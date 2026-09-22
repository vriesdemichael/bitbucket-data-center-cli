package completion

import (
	"context"
	"strings"
	"sync"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/reposel"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/git/execgit"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/safederef"
	branchservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/branch"
	commitservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/commit"
	tagservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/tag"
)

func init() {
	register(KindBranch, branchSource)
	register(KindTag, tagSource)
	register(KindRef, refSource)
	register(KindCommit, commitSource)
}

const (
	// refPageSize is how many references one press asks the server for.
	//
	// Small because the prefix is sent with the query: what comes back is
	// already the branches whose names contain what was typed, and fifty of
	// those is more than a shell can usefully show.
	refPageSize = 50

	// commitPageSize is how far back a commit slot looks. A commit nobody has
	// touched in two hundred pushes is one you paste rather than complete.
	commitPageSize = 20

	// localRefLimit bounds the checkout's own listing. A clone of a busy
	// monorepo carries thousands of remote-tracking refs, and the five
	// hundred most recently written are the ones anybody is completing.
	localRefLimit = 500

	// modificationOrder asks Bitbucket for most-recently-written first, which
	// is the order KeepOrder then protects from the shell's sort.
	modificationOrder = "MODIFICATION"
)

// headPatterns, tagPatterns and everyPattern are what a checkout is asked for.
// Remote-tracking references are included with the local branches because a
// branch somebody else pushed is a branch of the repository in scope, whether
// or not this checkout has ever had it checked out.
var (
	headPatterns  = []string{"refs/heads", "refs/remotes"}
	tagPatterns   = []string{"refs/tags"}
	everyPatterns = []string{"refs/heads", "refs/remotes", "refs/tags"}
)

// refScope is the repository a ref slot names, and the checkout that can
// answer for it without a request.
type refScope struct {
	repository Repository
	// directory is the checkout whose own references describe this
	// repository, empty when they describe a different one.
	directory string
}

// branchSource offers the branches of the repository in scope.
func branchSource(ctx context.Context, environment *Environment, request Request) (Result, error) {
	scope, err := scopeFor(ctx, environment, request)
	if err != nil {
		return Result{}, err
	}

	if refs := localRefs(ctx, scope, headPatterns...); len(refs) > 0 {
		return Result{Candidates: rankBranches(refs, scope.repository.RemoteName), KeepOrder: true}, nil
	}

	return remoteBranches(ctx, environment, scope, request.ToComplete)
}

// tagSource offers the repository's tags.
func tagSource(ctx context.Context, environment *Environment, request Request) (Result, error) {
	scope, err := scopeFor(ctx, environment, request)
	if err != nil {
		return Result{}, err
	}

	if refs := localRefs(ctx, scope, tagPatterns...); len(refs) > 0 {
		return Result{Candidates: localTags(refs), KeepOrder: true}, nil
	}

	client, err := environment.APIClient(ctx)
	if err != nil {
		return Result{}, err
	}

	tags, err := tagservice.NewService(client).List(
		ctx,
		tagservice.RepositoryRef{ProjectKey: scope.repository.ProjectKey, Slug: scope.repository.Slug},
		tagservice.ListOptions{
			FilterText: strings.TrimSpace(request.ToComplete),
			MaxResults: refPageSize,
			OrderBy:    modificationOrder,
		},
	)
	if err != nil {
		return Result{}, err
	}

	candidates := make([]Candidate, 0, len(tags))
	for _, tag := range tags {
		name := nameOf(tag.DisplayId, tag.Id)
		if name == "" {
			continue
		}

		candidates = append(candidates, Candidate{Value: name, Description: describeTag(safederef.String(tag.LatestCommit))})
	}

	return Result{Candidates: candidates, KeepOrder: true}, nil
}

// refSource offers branches and tags together, for the slot that takes either.
func refSource(ctx context.Context, environment *Environment, request Request) (Result, error) {
	scope, err := scopeFor(ctx, environment, request)
	if err != nil {
		return Result{}, err
	}

	if refs := localRefs(ctx, scope, everyPatterns...); len(refs) > 0 {
		return Result{Candidates: localNamedRefs(refs, scope.repository.RemoteName), KeepOrder: true}, nil
	}

	client, err := environment.APIClient(ctx)
	if err != nil {
		return Result{}, err
	}

	// One call rather than two services: it sends the typed prefix to both
	// listings as the filter Bitbucket applies itself.
	named, err := commitservice.NewService(client).ListTagsAndBranches(
		ctx,
		commitservice.RepositoryRef{ProjectKey: scope.repository.ProjectKey, Slug: scope.repository.Slug},
		strings.TrimSpace(request.ToComplete),
	)
	if err != nil {
		return Result{}, err
	}

	return Result{Candidates: namedRefCandidates(named), KeepOrder: true}, nil
}

// commitSource offers a commit-ish: the commits themselves, and the names that
// resolve to one.
//
// Both, because the slots spelled <commit> take either -- `bb branch create
// --start-point` is nearly always given a branch name, and `bb commit get` a
// hash -- and a source that offered only hashes would be silent for the
// commoner of the two.
func commitSource(ctx context.Context, environment *Environment, request Request) (Result, error) {
	scope, err := scopeFor(ctx, environment, request)
	if err != nil {
		return Result{}, err
	}

	if candidates := localCommitish(ctx, scope); len(candidates) > 0 {
		return Result{Candidates: candidates, KeepOrder: true}, nil
	}

	client, err := environment.APIClient(ctx)
	if err != nil {
		return Result{}, err
	}

	service := commitservice.NewService(client)
	repo := commitservice.RepositoryRef{ProjectKey: scope.repository.ProjectKey, Slug: scope.repository.Slug}

	var (
		named     []openapigenerated.RestMinimalRef
		namedErr  error
		commits   []openapigenerated.RestCommit
		commitErr error
	)

	inParallel(
		func() {
			named, namedErr = service.ListTagsAndBranches(ctx, repo, strings.TrimSpace(request.ToComplete))
		},
		func() {
			// No prefix to send: Bitbucket's commit listing takes a path and a
			// range and nothing that narrows by hash, so this is the one
			// listing here that run.go filters rather than the server.
			commits, commitErr = service.List(ctx, repo, commitservice.ListOptions{MaxResults: commitPageSize})
		},
	)

	// One of the two failing still leaves an answer worth giving: a commit
	// slot with the branch names but no hashes is most of what it is for.
	// Both failing is the press having nothing to say.
	if namedErr != nil && commitErr != nil {
		return Result{}, namedErr
	}
	if namedErr != nil {
		debugf("commit-ish names: %v", namedErr)
	}
	if commitErr != nil {
		debugf("commit-ish commits: %v", commitErr)
	}

	candidates := namedRefCandidates(named)
	for _, commit := range commits {
		value := nameOf(commit.DisplayId, commit.Id)
		if value == "" {
			continue
		}

		candidates = append(candidates, Candidate{
			Value:       value,
			Description: describeCommit(safederef.String(commit.Message), authorOf(commit)),
		})
	}

	return Result{Candidates: candidates, KeepOrder: true}, nil
}

// scopeFor is the repository whose references this slot names.
//
// Normally the one the command would act on, which the Environment resolves
// the way the command path does. The exception is the source side of a fork
// pull request, which is a different repository named on the same line.
func scopeFor(ctx context.Context, environment *Environment, request Request) (refScope, error) {
	repository, err := environment.Repository(ctx)
	if err != nil {
		return refScope{}, err
	}

	elsewhere, moved, err := forkScope(request.Flag, environment.Flag("from-repo"), repository)
	if err != nil {
		return refScope{}, err
	}

	scope := refScope{repository: elsewhere}

	// The checkout answers only for the repository its own remote names. Its
	// references describe that repository and no other, so a --repo naming
	// something else, or a fork named by --from-repo, has to be asked over
	// the wire even while standing in a checkout.
	if !moved && elsewhere.Inferred() {
		scope.directory = "."
	}

	return scope, nil
}

// forkScope is the repository a --from-ref names when --from-repo puts it in
// another one.
//
// `bb pr create --from-repo FORK/service --from-ref <tab>` is completing a
// branch of the fork, not of the repository the pull request targets. Only
// --from-ref moves: --to-ref names a branch of the target, and so does every
// other reference on the line.
//
// A --from-repo that names the target anyway is the same-repository case
// spelled out, and moves nothing -- which matters, because treating it as a
// move would throw away the checkout those branches could have come from.
func forkScope(flag, fromRepo string, target Repository) (Repository, bool, error) {
	if flag != "from-ref" || strings.TrimSpace(fromRepo) == "" {
		return target, false, nil
	}

	projectKey, slug, err := reposel.Parse(fromRepo)
	if err != nil {
		return Repository{}, false, err
	}

	if strings.EqualFold(projectKey, target.ProjectKey) && strings.EqualFold(slug, target.Slug) {
		return target, false, nil
	}

	return Repository{Host: target.Host, ProjectKey: projectKey, Slug: slug}, true, nil
}

// localRefs reads the checkout's own references, and answers nothing when it
// cannot.
//
// Nothing rather than an error, because every way this fails -- a directory
// that is no longer a checkout, a git too old for one of the format atoms, a
// repository in the middle of a rebase -- means the same thing to the caller,
// which is that the server has to be asked instead.
func localRefs(ctx context.Context, scope refScope, patterns ...string) []execgit.Ref {
	if scope.directory == "" {
		return nil
	}

	refs, err := execgit.New().ListRefs(ctx, scope.directory, localRefLimit, patterns...)
	if err != nil {
		debugf("local refs: %v", err)

		return nil
	}

	return refs
}

// localCommitish is a commit slot answered from the checkout: the names it
// knows, then the commits it is standing on.
func localCommitish(ctx context.Context, scope refScope) []Candidate {
	if scope.directory == "" {
		return nil
	}

	var (
		refs    []execgit.Ref
		commits []execgit.Commit
	)

	inParallel(
		func() { refs = localRefs(ctx, scope, everyPatterns...) },
		func() {
			listed, err := execgit.New().ListCommits(ctx, scope.directory, commitPageSize)
			if err != nil {
				debugf("local commits: %v", err)

				return
			}

			commits = listed
		},
	)

	if len(refs) == 0 && len(commits) == 0 {
		return nil
	}

	return append(localNamedRefs(refs, scope.repository.RemoteName), localCommitCandidates(commits)...)
}

// remoteBranches asks Bitbucket for the branches, and for which one is the
// default beside it.
//
// Beside rather than after: the default branch is a second endpoint, because
// the listing's own flag for it does not survive the generated client -- the
// model reads `default` and Bitbucket sends `isDefault`, so RestBranch.Default
// is nil for every branch including the default one. Two sequential round
// trips would be most of the press.
func remoteBranches(ctx context.Context, environment *Environment, scope refScope, prefix string) (Result, error) {
	client, err := environment.APIClient(ctx)
	if err != nil {
		return Result{}, err
	}

	service := branchservice.NewService(client)
	repo := branchservice.RepositoryRef{ProjectKey: scope.repository.ProjectKey, Slug: scope.repository.Slug}

	var (
		branches      []openapigenerated.RestBranch
		listErr       error
		defaultBranch string
	)

	inParallel(
		func() {
			branches, listErr = service.List(ctx, repo, branchservice.ListOptions{
				FilterText: strings.TrimSpace(prefix),
				MaxResults: refPageSize,
				OrderBy:    modificationOrder,
			})
		},
		func() {
			reference, err := service.GetDefault(ctx, repo)
			if err != nil {
				// A repository with no commits has no default branch, and a
				// reader without admin rights may not be told which it is.
				// Neither is a reason to complete nothing.
				debugf("default branch: %v", err)

				return
			}

			defaultBranch = nameOf(reference.DisplayId, reference.Id)
		},
	)

	if listErr != nil {
		return Result{}, listErr
	}

	candidates := make([]Candidate, 0, len(branches))
	for _, branch := range branches {
		name := nameOf(branch.DisplayId, branch.Id)
		if name == "" {
			continue
		}

		candidates = append(candidates, Candidate{Value: name, Description: describeBranch(name, defaultBranch)})
	}

	return Result{Candidates: hoist(candidates, defaultBranch), KeepOrder: true}, nil
}

// rankBranches turns a checkout's references into branch candidates: the
// branch you are standing on first, then the default one, then the rest in
// the order git gave them.
//
// The order is why Result.KeepOrder exists. Sorted as text, the branch you
// made this morning sits below one abandoned last spring, and `feature/`
// buries thirty of them under whichever came first alphabetically.
func rankBranches(refs []execgit.Ref, remote string) []Candidate {
	defaultBranch := defaultBranchFrom(refs, remote)

	current := ""
	names := make([]string, 0, len(refs))
	seen := make(map[string]bool, len(refs))

	for _, ref := range refs {
		name, ok := branchFromRef(ref.Name, remote)
		if !ok {
			continue
		}
		if ref.Checked {
			current = name
		}
		if seen[name] {
			continue
		}

		seen[name] = true
		names = append(names, name)
	}

	candidates := make([]Candidate, 0, len(names))
	for _, name := range names {
		candidates = append(candidates, Candidate{Value: name, Description: describeBranch(name, defaultBranch)})
	}

	return hoist(hoist(candidates, defaultBranch), current)
}

// localTags is the checkout's tags, newest first, each saying what it marks.
func localTags(refs []execgit.Ref) []Candidate {
	candidates := make([]Candidate, 0, len(refs))

	for _, ref := range refs {
		name, ok := tagFromRef(ref.Name)
		if !ok {
			continue
		}

		candidates = append(candidates, Candidate{Value: name, Description: describeTag(ref.Object)})
	}

	return candidates
}

// localNamedRefs is every name the checkout knows, branches before tags.
func localNamedRefs(refs []execgit.Ref, remote string) []Candidate {
	branches := rankBranches(refs, remote)
	for index := range branches {
		if branches[index].Description == "" {
			// In a slot that takes either, which of the two this is says more
			// than nothing does.
			branches[index].Description = "branch"
		}
	}

	return append(branches, localTags(refs)...)
}

// localCommitCandidates keeps git's order, newest first, and says which one is
// HEAD -- the hash of the commit you are standing on looks like any other.
func localCommitCandidates(commits []execgit.Commit) []Candidate {
	candidates := make([]Candidate, 0, len(commits))

	for index, commit := range commits {
		description := describeCommit(commit.Subject, commit.Author)
		if index == 0 {
			if description == "" {
				description = "HEAD"
			} else {
				description = "HEAD: " + description
			}
		}

		candidates = append(candidates, Candidate{Value: commit.ID, Description: description})
	}

	return candidates
}

// namedRefCandidates turns Bitbucket's combined branch-and-tag listing into
// candidates that say which of the two each one is.
func namedRefCandidates(refs []openapigenerated.RestMinimalRef) []Candidate {
	candidates := make([]Candidate, 0, len(refs))

	for _, ref := range refs {
		name := nameOf(ref.DisplayId, ref.Id)
		if name == "" {
			continue
		}

		kind := ""
		if ref.Type != nil {
			kind = strings.ToLower(strings.TrimSpace(string(*ref.Type)))
		}

		candidates = append(candidates, Candidate{Value: name, Description: kind})
	}

	return candidates
}

// branchFromRef is the branch a reference in a checkout names.
//
// A local branch names itself. A remote-tracking reference names the branch it
// tracks, but only on the remote the repository in scope was inferred from:
// refs/remotes/fork/main is a branch of somebody else's repository, and
// offering it for a command that acts on this one offers a value that may not
// exist there.
func branchFromRef(name, remote string) (string, bool) {
	if branch, ok := strings.CutPrefix(name, "refs/heads/"); ok {
		return branch, branch != ""
	}

	if strings.TrimSpace(remote) == "" {
		return "", false
	}

	tracked, ok := strings.CutPrefix(name, "refs/remotes/"+remote+"/")
	if !ok || tracked == "" || tracked == "HEAD" {
		// refs/remotes/<remote>/HEAD is the remote's default branch recorded
		// as a symbolic reference, not a branch called HEAD.
		return "", false
	}

	return tracked, true
}

// tagFromRef is the tag a reference names.
func tagFromRef(name string) (string, bool) {
	tag, ok := strings.CutPrefix(name, "refs/tags/")

	return tag, ok && tag != ""
}

// defaultBranchFrom is the branch refs/remotes/<remote>/HEAD points at.
//
// That symbolic reference is the only place a checkout records which branch
// the server considers the default, and it is written by the clone -- so a
// repository cloned before the default moved names the old one. Wrong there
// costs a misplaced row rather than a wrong value, which is the trade the
// whole local path makes.
func defaultBranchFrom(refs []execgit.Ref, remote string) string {
	if strings.TrimSpace(remote) == "" {
		return ""
	}

	head := "refs/remotes/" + remote + "/HEAD"
	for _, ref := range refs {
		if ref.Name != head || ref.Target == "" {
			continue
		}

		if branch, ok := branchFromRef(ref.Target, remote); ok {
			return branch
		}
	}

	return ""
}

// hoist moves one value to the front, leaving the rest as they were.
func hoist(candidates []Candidate, first string) []Candidate {
	if strings.TrimSpace(first) == "" {
		return candidates
	}

	for index, candidate := range candidates {
		if candidate.Value != first {
			continue
		}
		if index == 0 {
			return candidates
		}

		ordered := make([]Candidate, 0, len(candidates))
		ordered = append(ordered, candidate)
		ordered = append(ordered, candidates[:index]...)

		return append(ordered, candidates[index+1:]...)
	}

	return candidates
}

// describeBranch says which one is the default.
//
// It is the branch most of these slots mean -- the one a pull request targets,
// the one a delete will be refused for -- and among forty names it is
// otherwise indistinguishable.
func describeBranch(name, defaultBranch string) string {
	if defaultBranch != "" && name == defaultBranch {
		return "default branch"
	}

	return ""
}

// describeTag says what the tag marks. Its name says what it is for and
// nothing about where, and where is what tells two release candidates apart.
func describeTag(commitID string) string {
	short := shorten(commitID)
	if short == "" {
		return "tag"
	}

	return "at " + short
}

// describeCommit is the short message and the author, which is how a person
// recognises a commit. A column of hashes is a column of nothing.
func describeCommit(message, author string) string {
	subject := strings.TrimSpace(firstLine(message))
	author = strings.TrimSpace(author)

	switch {
	case subject == "" && author == "":
		return ""
	case author == "":
		return subject
	case subject == "":
		return "(" + author + ")"
	default:
		return subject + " (" + author + ")"
	}
}

// authorOf prefers the author over the committer: a rebased or cherry-picked
// commit is still recognised by whoever wrote it.
func authorOf(commit openapigenerated.RestCommit) string {
	if commit.Author != nil && strings.TrimSpace(commit.Author.Name) != "" {
		return commit.Author.Name
	}

	if commit.Committer != nil {
		return commit.Committer.Name
	}

	return ""
}

// nameOf is what to show a reference as: the display id, or the full name with
// its refs/ prefix taken off when the server left the display id out.
func nameOf(displayID, id *string) string {
	if display := strings.TrimSpace(safederef.String(displayID)); display != "" {
		return display
	}

	full := strings.TrimSpace(safederef.String(id))
	for _, prefix := range []string{"refs/heads/", "refs/tags/"} {
		if trimmed, ok := strings.CutPrefix(full, prefix); ok {
			return trimmed
		}
	}

	return full
}

func firstLine(message string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(message), "\n")

	return line
}

// shortCommitLength is git's own abbreviation, which is what a person reading
// a description expects to see and long enough to be unambiguous.
const shortCommitLength = 7

func shorten(commitID string) string {
	trimmed := strings.TrimSpace(commitID)
	if len(trimmed) <= shortCommitLength {
		return trimmed
	}

	return trimmed[:shortCommitLength]
}

// inParallel runs the listings of one press at the same time.
//
// A source needing two of them would otherwise spend two round trips out of a
// budget that allows about one. The recover is here rather than inherited:
// run.go guards the source's own goroutine, and a panic in one this starts
// would take the process down with a stack trace the shell has nowhere to put.
func inParallel(work ...func()) {
	var wait sync.WaitGroup

	for _, task := range work {
		wait.Add(1)

		go func() {
			defer wait.Done()
			defer func() {
				if recovered := recover(); recovered != nil {
					debugf("parallel listing failed: %v", recovered)
				}
			}()

			task()
		}()
	}

	wait.Wait()
}
