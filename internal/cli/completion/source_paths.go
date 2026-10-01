package completion

import (
	"context"
	"strings"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/git/execgit"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/listing"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
)

func init() {
	register(KindRepoPath, repoPathSource)
	register(KindPRDiffPath, pullRequestPathSource)
}

const (
	// directoryPageSize is how many entries of one directory a press asks for:
	// listing.DirectoryEntries, which says why it is large.
	directoryPageSize = listing.DirectoryEntries

	// changedFilePageSize is how far into a pull request's changes a press
	// looks.
	//
	// The paging window is twenty-five, so a pull request of the usual size
	// costs one round trip and only a large one costs a second -- which is the
	// right way round for a budget that allows about one round trip in total.
	changedFilePageSize = 50
)

// repoPathSource offers a path inside the repository in scope, one segment at
// a time.
//
// A segment at a time because the alternative does not work: a repository with
// twenty thousand files cannot answer a keystroke with all of them, and a
// shell that printed them would have told the user nothing they could not have
// got by typing another letter. So what is offered is the contents of the
// directory being typed -- the top level for an empty word, what is inside
// internal/ once that has been typed -- with a directory marked by the trailing
// slash that makes the next press continue into it. That is what a shell's own
// path completion does, and it is the only shape that stays usable.
//
// The path is read at the commit the line names, because a path exists at a
// commit rather than in the abstract: `bb repo browse file --at release/2.1`
// is asking about that branch's tree. With nothing named, the repository's
// default branch is what the command itself would read.
func repoPathSource(ctx context.Context, environment *Environment, request Request) (Result, error) {
	scope, err := scopeFor(ctx, environment, request)
	if err != nil {
		return Result{}, err
	}

	// Nothing on the line naming a commit is the ordinary case, not a failure:
	// the listing then falls to the default branch, which is where both the
	// checkout and the server look when they are given no ref.
	at, err := environment.Commit(ctx)
	if err != nil {
		at = ""
	}

	directory, _ := splitPath(request.ToComplete)

	if entries := localEntries(ctx, scope, at, directory); len(entries) > 0 {
		return pathResult(directory, entries, request.ToComplete), nil
	}

	entries, err := remoteEntries(ctx, environment, scope.repository, at, directory)
	if err != nil {
		return Result{}, err
	}

	return pathResult(directory, entries, request.ToComplete), nil
}

// pullRequestPathSource offers the files a pull request changes.
//
// An inline comment is anchored to a line of a file in the diff; a path the
// pull request does not touch is a value Bitbucket refuses, whether or not the
// repository has it. So the listing is the diff rather than the tree, which
// also makes this the one path slot worth offering whole: the changed files
// are few, and which files a pull request touches is most of what the person
// pressing tab is asking.
func pullRequestPathSource(ctx context.Context, environment *Environment, _ Request) (Result, error) {
	repository, err := environment.Repository(ctx)
	if err != nil {
		return Result{}, err
	}

	pullRequestID, err := environment.PullRequest(ctx)
	if err != nil {
		return Result{}, err
	}

	client, err := environment.HTTPClient(ctx)
	if err != nil {
		return Result{}, err
	}

	changes, err := pullrequestservice.NewService(client).ListChanges(
		ctx,
		pullrequestservice.RepositoryRef{ProjectKey: repository.ProjectKey, Slug: repository.Slug},
		pullRequestID,
		pullrequestservice.PageOptions{MaxResults: changedFilePageSize},
	)
	if err != nil {
		return Result{}, err
	}

	return Result{Candidates: changeCandidates(changes)}, nil
}

// changeCandidates turns a pull request's changes into paths that say what the
// pull request did to each one.
//
// Which matters for the slot: a comment cannot be anchored to the new side of
// a file the pull request deleted, and the description is the only warning
// before Bitbucket refuses it.
func changeCandidates(changes []pullrequestservice.Change) []Candidate {
	candidates := make([]Candidate, 0, len(changes))

	for _, change := range changes {
		path := strings.TrimSpace(change.Path)
		if path == "" {
			continue
		}

		candidates = append(candidates, Candidate{Value: path, Description: describeChange(change)})
	}

	return candidates
}

func describeChange(change pullrequestservice.Change) string {
	kind := strings.ToLower(strings.TrimSpace(change.Type))
	source := strings.TrimSpace(change.SrcPath)

	switch {
	case kind == "" && source == "":
		return ""
	case source == "" || source == strings.TrimSpace(change.Path):
		return kind
	case kind == "":
		return "from " + source
	default:
		return kind + " from " + source
	}
}

// entry is one child of a directory: its name within that directory, and
// whether there is anything underneath it.
type entry struct {
	name      string
	directory bool
}

// splitPath separates the directory a half-typed path names from the leaf
// being typed inside it.
//
// `internal/cli/comp` is a prefix inside internal/cli, so internal/cli is the
// only directory a listing has to read. The separator stays with the
// directory, because every candidate is rebuilt from it: a shell replaces the
// whole word, so offering `completion/` for that press would leave the line
// reading `bb repo cat completion/`.
func splitPath(word string) (directory, leaf string) {
	cut := strings.LastIndex(word, "/")
	if cut < 0 {
		return "", word
	}

	return word[:cut+1], word[cut+1:]
}

// pathResult turns one directory's entries into the press's answer.
//
// The prefix filter is applied here as well as in run.go, and deliberately:
// NoSpace is a property of the whole answer rather than of one candidate, so
// deciding it from entries the shell will never be shown would glue the cursor
// to a completed file name because some other entry of the same directory
// happened to be a directory.
func pathResult(directory string, entries []entry, word string) Result {
	lowered := strings.ToLower(word)
	candidates := make([]Candidate, 0, len(entries))
	intoDirectory := false

	for _, item := range entries {
		name := strings.TrimSpace(item.name)
		if name == "" {
			continue
		}

		value := directory + name
		if item.directory {
			value += "/"
		}
		if !strings.HasPrefix(strings.ToLower(value), lowered) {
			continue
		}

		if item.directory {
			intoDirectory = true
		}

		candidates = append(candidates, Candidate{Value: value})

		if len(candidates) == maxCandidates {
			break
		}
	}

	// No KeepOrder: a directory listing carries no ranking worth protecting
	// from the shell's own sort, and alphabetical is what a person expects of
	// a path.
	return Result{Candidates: candidates, NoSpace: intoDirectory}
}

// localEntries reads the directory out of the checkout, and answers nothing
// when it cannot.
//
// Nothing rather than an error, for the reason localRefs gives: a directory
// that is not a checkout, a ref this clone has never fetched and a path that
// does not exist at that ref all mean the same thing to the caller, which is
// that the server has to be asked instead.
func localEntries(ctx context.Context, scope refScope, at, directory string) []entry {
	if scope.directory == "" {
		return nil
	}

	// execgit's tree reader directly, for the reasons localRefs gives.
	backend := execgit.New()

	for _, ref := range checkoutRefs(at, scope.repository.RemoteName) {
		listed, err := backend.ListTree(ctx, scope.directory, ref, directory, directoryPageSize)
		if err != nil {
			debugf("local tree at %s: %v", ref, err)

			continue
		}
		if len(listed) == 0 {
			continue
		}

		entries := make([]entry, 0, len(listed))
		for _, item := range listed {
			// git prints the path from the repository root; the directory is
			// already carried by the candidate's prefix.
			entries = append(entries, entry{
				name:      strings.TrimPrefix(item.Path, directory),
				directory: item.Directory,
			})
		}

		return entries
	}

	return nil
}

// checkoutRefs is what the checkout can be asked for a path at, in the order
// worth trying.
//
// A ref named on the line goes first as it was typed and then as this remote's
// copy of it, because the branch source offers branches the checkout has never
// had checked out: `--at a-colleagues-branch` names something that exists here
// only as refs/remotes/origin/a-colleagues-branch.
//
// With nothing named it is the remote's recorded HEAD, which is the one place
// a checkout writes down which branch the server considers the default -- the
// same branch the request would have fallen back to. Guessing anything else,
// the commit this checkout happens to be standing on above all, would offer
// paths from a branch nobody named.
func checkoutRefs(at, remote string) []string {
	at = strings.TrimSpace(at)
	remote = strings.TrimSpace(remote)

	if at == "" {
		if remote == "" {
			return nil
		}

		return []string{"refs/remotes/" + remote + "/HEAD"}
	}

	refs := []string{at}
	if remote != "" {
		refs = append(refs, "refs/remotes/"+remote+"/"+at)
	}

	return refs
}

// remoteEntries asks Bitbucket for one directory, through the listing both
// the shell and the MCP server complete a path with.
func remoteEntries(
	ctx context.Context,
	environment *Environment,
	repository Repository,
	at string,
	directory string,
) ([]entry, error) {
	client, err := environment.HTTPClient(ctx)
	if err != nil {
		return nil, err
	}

	children, err := listing.Directory(ctx, client, repository.ProjectKey, repository.Slug, at, directory, directoryPageSize)
	if err != nil {
		return nil, err
	}

	entries := make([]entry, 0, len(children))
	for _, child := range children {
		entries = append(entries, entry{name: child.Name, directory: child.Directory})
	}

	return entries, nil
}
