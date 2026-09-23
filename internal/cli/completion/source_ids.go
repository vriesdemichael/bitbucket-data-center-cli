package completion

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/safederef"
	branchservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/branch"
	commentservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/comment"
	projectservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/project"
	pullrequestactivityservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequestactivity"
	qualityservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/quality"
	reposettingsservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/reposettings"
	reviewerservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/reviewer"
	sshkeyservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/sshkey"
)

// This file answers the slots that take the identifier of something living
// inside a repository or a project: a comment, a webhook, a branch
// restriction, a default task, a reviewer condition, a required build, a label
// or an access key.
//
// None of these is a value anybody remembers. 1389396 is a comment and 7 is a
// webhook, and neither says which -- so for every kind here the description is
// half the answer, and a candidate that arrives without one has not completed
// anything. That is why each source reads the object rather than a list of
// ids: a webhook's name and URL, a restriction's matcher, the first line of a
// comment.
//
// Several of these objects exist at both levels, configured through two routes
// -- `bb webhook delete <webhook-id>` is the repository's and `bb project
// webhook delete <project-key> <webhook-id>` is that project's. Offering the
// wrong scope's ids is worse than offering none, because every candidate is a
// 404 the person was handed, so the command being completed decides.
func init() {
	register(KindPRComment, pullRequestCommentSource)
	register(KindRepoComment, repositoryCommentSource)
	register(KindWebhook, webhookSource)
	register(KindRestriction, restrictionSource)
	register(KindDefaultTask, defaultTaskSource)
	register(KindReviewerCondition, reviewerConditionSource)
	register(KindRequiredBuild, requiredBuildSource)
	register(KindLabel, labelSource)
	register(KindAccessKey, accessKeySource)
}

const (
	// commentPageSize is how much of a timeline one press reads.
	//
	// The activity feed carries an entry per action rather than per comment,
	// so this is not a hundred comments -- it is the recent end of the
	// conversation, which is where the comment somebody is addressing is.
	commentPageSize = 100

	// commitPathBudget is how many of a commit's files are asked for comments.
	//
	// Bitbucket refuses to list a commit's comments without a path -- "The
	// path query parameter is required when retrieving comments" -- so the
	// files it touched are the only way in, one request each. A tab press has
	// no budget for a hundred of them, and a commit with more than a handful
	// of commented files is not the one anybody is completing.
	commitPathBudget = 8

	// suggestionFence opens the block `bb pr comment apply-suggestion` acts
	// on. A comment without one is a value that command has nothing to apply.
	suggestionFence = "```suggestion"
)

// pullRequestCommentSource offers the comments of the pull request on the line.
//
// Through the activity timeline rather than the comments endpoint, for the
// reason `bb pr comment list` uses it: the path-scoped listing needs a path,
// and a slot taking a comment id has none to give. The timeline carries every
// comment on the pull request, anchored or not, with its replies nested
// underneath -- which is why the whole tree is flattened rather than only its
// roots. A reply is a comment `react`, `resolve` and `--parent-id` all accept.
//
// Newest first, and kept that way: comment ids sorted as text put 1389396
// above 98, and the comment being answered is nearly always the last one
// written.
func pullRequestCommentSource(ctx context.Context, environment *Environment, request Request) (Result, error) {
	repository, err := environment.Repository(ctx)
	if err != nil {
		return Result{}, err
	}

	pullRequestID, err := environment.PullRequest(ctx)
	if err != nil {
		return Result{}, err
	}

	client, err := environment.APIClient(ctx)
	if err != nil {
		return Result{}, err
	}

	activities, err := pullrequestactivityservice.NewService(client).List(
		ctx,
		pullrequestactivityservice.RepositoryRef{ProjectKey: repository.ProjectKey, Slug: repository.Slug},
		pullRequestID,
		pullrequestactivityservice.ListOptions{MaxResults: commentPageSize},
	)
	if err != nil {
		return Result{}, err
	}

	comments := result.FlattenComments(pullrequestactivityservice.ExtractComments(activities))

	return commentResult(comments, commentFilterFor(commandPath(request.Command))), nil
}

// repositoryCommentSource offers the comments of the commit on the line.
//
// `bb repo comment` addresses a commit or a pull request -- exactly one of
// --commit and --pr -- and completes whichever the line names, because the
// other is a comment the command would refuse to touch.
//
// A commit's comments cost more than a pull request's. Bitbucket will not list
// them without a path, so the commit's own changed files are read first and
// asked one at a time, up to commitPathBudget of them. A --path already on the
// line -- `bb repo comment create --path x --parent <tab>` -- is taken as the
// answer to that question and nothing is discovered.
//
// The consequence worth knowing: a comment left on the commit as a whole,
// anchored to no file, is not listable through any endpoint Bitbucket offers,
// so it cannot be offered here either.
func repositoryCommentSource(ctx context.Context, environment *Environment, request Request) (Result, error) {
	repository, err := environment.Repository(ctx)
	if err != nil {
		return Result{}, err
	}

	client, err := environment.APIClient(ctx)
	if err != nil {
		return Result{}, err
	}

	commitID, commitErr := environment.Commit(ctx)
	if commitErr != nil {
		// No commit means the invocation is the --pr form, whose comments the
		// pull request source already knows how to read.
		return pullRequestCommentSource(ctx, environment, request)
	}

	target := commentservice.Target{
		Repository: commentservice.RepositoryRef{ProjectKey: repository.ProjectKey, Slug: repository.Slug},
		CommitID:   commitID,
	}

	paths := []string{}
	if typed := strings.TrimSpace(environment.Flag("path")); typed != "" {
		paths = append(paths, typed)
	} else {
		discovered, err := commitPaths(ctx, client, repository, commitID)
		if err != nil {
			return Result{}, err
		}
		paths = discovered
	}

	service := commentservice.NewService(client)
	comments := make([]openapigenerated.RestComment, 0, len(paths))

	var listErr error
	for _, path := range paths {
		page, err := service.List(ctx, target, path, commentPageSize)
		if err != nil {
			// One unreadable file is not an unreadable commit: a path that has
			// been deleted and re-added answers differently from its
			// neighbours, and the neighbours are still worth offering.
			if listErr == nil {
				listErr = err
			}

			continue
		}

		comments = append(comments, page...)
		if len(comments) >= maxCandidates {
			break
		}
	}

	if len(comments) == 0 && listErr != nil {
		return Result{}, listErr
	}

	return commentResult(result.FlattenComments(comments), nil), nil
}

// commitPaths is the files a commit touched, which is the only way to ask for
// its comments.
func commitPaths(
	ctx context.Context,
	client *openapigenerated.ClientWithResponses,
	repository Repository,
	commitID string,
) ([]string, error) {
	limit := float32(maxCandidates)

	response, err := client.GetChangesWithResponse(ctx, repository.ProjectKey, repository.Slug, commitID,
		&openapigenerated.GetChangesParams{Limit: &limit})
	if err != nil {
		return nil, apperrors.Transport("failed to list the commit's changed files", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return nil, err
	}

	page := response.ApplicationjsonCharsetUTF8200
	if page == nil || page.Values == nil {
		return nil, nil
	}

	paths := make([]string, 0, commitPathBudget)
	for _, change := range *page.Values {
		if change.Path == nil {
			continue
		}

		path := strings.Join(safederef.StringSlice(change.Path.Components), "/")
		if strings.TrimSpace(path) == "" {
			continue
		}

		paths = append(paths, path)
		if len(paths) == commitPathBudget {
			break
		}
	}

	return paths, nil
}

// commentResult turns comments into candidates, newest first.
//
// The sort is done here rather than trusted from the server because the two
// endpoints behind these sources order differently, and the order is what the
// KeepOrder directive then asks the shell to preserve.
func commentResult(comments []result.Comment, keep func(result.Comment) bool) Result {
	ordered := make([]result.Comment, 0, len(comments))
	for _, comment := range comments {
		if comment.ID == 0 {
			continue
		}
		if keep != nil && !keep(comment) {
			continue
		}

		ordered = append(ordered, comment)
	}

	sort.SliceStable(ordered, func(first, second int) bool {
		return ordered[first].CreatedDate > ordered[second].CreatedDate
	})

	candidates := make([]Candidate, 0, len(ordered))
	for _, comment := range ordered {
		candidates = append(candidates, Candidate{
			Value:       strconv.FormatInt(comment.ID, 10),
			Description: describeComment(comment.Author.DisplayName, comment.Author.Name, comment.Text),
		})
	}

	return Result{Candidates: candidates, KeepOrder: true}
}

// commentFilterFor is the comments a verb can act on.
//
// The same rule `bb pr reopen` follows: a value the command will refuse is
// not a suggestion. Resolving is the one asymmetric case -- Bitbucket takes
// OPEN for a comment that carries no state at all, so anything not already
// resolved is offered.
func commentFilterFor(path string) func(result.Comment) bool {
	switch path {
	case "pr comment reopen":
		return resolvedComment
	case "pr comment resolve":
		return func(comment result.Comment) bool { return !resolvedComment(comment) }
	case "pr comment apply-suggestion":
		return func(comment result.Comment) bool { return carriesSuggestion(comment.Text) }
	default:
		return nil
	}
}

func resolvedComment(comment result.Comment) bool {
	return strings.EqualFold(strings.TrimSpace(comment.State), "RESOLVED")
}

// carriesSuggestion reports the fenced block `apply-suggestion` rewrites the
// file from. It is Bitbucket's own spelling, and the only thing that endpoint
// can act on.
func carriesSuggestion(text string) bool {
	return strings.Contains(strings.ToLower(text), suggestionFence)
}

// webhookSource offers the webhooks of whichever scope the command addresses.
func webhookSource(ctx context.Context, environment *Environment, request Request) (Result, error) {
	client, err := environment.APIClient(ctx)
	if err != nil {
		return Result{}, err
	}

	var payload any

	if projectScoped(commandPath(request.Command), environment.Flag("repo"), environment.Flag("project")) {
		projectKey, err := environment.Project(ctx)
		if err != nil {
			return Result{}, err
		}

		payload, err = projectservice.NewService(client).ListProjectWebhooks(ctx, projectKey)
		if err != nil {
			return Result{}, err
		}
	} else {
		repository, err := environment.Repository(ctx)
		if err != nil {
			return Result{}, err
		}

		listed, err := reposettingsservice.NewService(client).ListRepositoryWebhooks(ctx,
			reposettingsservice.RepositoryRef{ProjectKey: repository.ProjectKey, Slug: repository.Slug})
		if err != nil {
			return Result{}, err
		}

		payload = listed.Payload
	}

	webhooks := result.WebhooksFrom(payload)
	candidates := make([]Candidate, 0, len(webhooks))
	for _, webhook := range webhooks {
		if webhook.ID == 0 {
			continue
		}

		candidates = append(candidates, Candidate{
			Value:       strconv.Itoa(webhook.ID),
			Description: describeWebhook(webhook.Name, webhook.URL, webhook.Active),
		})
	}

	return Result{Candidates: candidates}, nil
}

// restrictionSource offers the branch restrictions of the scope in hand.
//
// A repository's listing carries the project's restrictions as well, marked by
// their scope, and they have to be left out. Not because the command would
// refuse them -- it does not, which is the problem. `bb branch restriction
// delete` handed a project restriction's id deletes it, project-wide, from
// every repository the project holds; verified against a live instance, which
// answered 204 and then had no such restriction left at project scope. A
// completion that offers it is a completion that deletes it.
func restrictionSource(ctx context.Context, environment *Environment, request Request) (Result, error) {
	client, err := environment.APIClient(ctx)
	if err != nil {
		return Result{}, err
	}

	var restrictions []openapigenerated.RestRefRestriction

	wantProject := projectScoped(commandPath(request.Command), environment.Flag("repo"), environment.Flag("project"))
	if wantProject {
		projectKey, err := environment.Project(ctx)
		if err != nil {
			return Result{}, err
		}

		restrictions, err = projectservice.NewService(client).ListRestrictions(ctx, projectKey,
			projectservice.RestrictionListOptions{MaxResults: maxCandidates})
		if err != nil {
			return Result{}, err
		}
	} else {
		repository, err := environment.Repository(ctx)
		if err != nil {
			return Result{}, err
		}

		restrictions, err = branchservice.NewService(client).ListRestrictions(ctx,
			branchservice.RepositoryRef{ProjectKey: repository.ProjectKey, Slug: repository.Slug},
			branchservice.RestrictionListOptions{MaxResults: maxCandidates})
		if err != nil {
			return Result{}, err
		}
	}

	candidates := make([]Candidate, 0, len(restrictions))
	for _, restriction := range restrictions {
		if restriction.Id == nil {
			continue
		}

		scope := ""
		if restriction.Scope != nil {
			scope = string(restriction.Scope.Type)
		}
		if !ownScope(scope, wantProject) {
			continue
		}

		matcher := refMatcher{}
		if restriction.Matcher != nil {
			matcher = refMatcher{
				ID:        safederef.String(restriction.Matcher.Id),
				DisplayID: safederef.String(restriction.Matcher.DisplayId),
			}
		}

		candidates = append(candidates, Candidate{
			Value:       strconv.FormatInt(int64(*restriction.Id), 10),
			Description: describeRestriction(safederef.String(restriction.Type), matcherRef(matcher)),
		})
	}

	return Result{Candidates: candidates}, nil
}

// defaultTask is one entry of a default-task listing, read for the three
// fields a completion has anything to do with.
//
// Read from one page of the response rather than through either service, whose
// listing walks every page: a completion has a second to answer in, and offers
// a page of candidates at most. scope is what says whether the repository's
// listing is showing the repository's task or the project's, inherited.
type defaultTask struct {
	ID          int64  `json:"id"`
	Description string `json:"description"`
	Scope       struct {
		Type string `json:"type"`
	} `json:"scope"`
	SourceMatcher refMatcher `json:"sourceMatcher"`
	TargetMatcher refMatcher `json:"targetMatcher"`
}

// refMatcher is the ref a default task is scoped to, in the one shape both
// endpoints answer with.
type refMatcher struct {
	ID        string `json:"id"`
	DisplayID string `json:"displayId"`
}

// defaultTaskSource offers the default checklist tasks of the scope in hand.
func defaultTaskSource(ctx context.Context, environment *Environment, request Request) (Result, error) {
	client, err := environment.APIClient(ctx)
	if err != nil {
		return Result{}, err
	}

	limit := float32(maxCandidates)

	var body []byte
	var status int

	wantProject := projectScoped(commandPath(request.Command), environment.Flag("repo"), environment.Flag("project"))
	if wantProject {
		projectKey, err := environment.Project(ctx)
		if err != nil {
			return Result{}, err
		}

		response, err := client.GetDefaultTasksWithResponse(ctx, projectKey,
			&openapigenerated.GetDefaultTasksParams{Limit: &limit})
		if err != nil {
			return Result{}, apperrors.Transport("failed to list project default tasks", err)
		}

		body, status = response.Body, response.StatusCode()
	} else {
		repository, err := environment.Repository(ctx)
		if err != nil {
			return Result{}, err
		}

		response, err := client.GetDefaultTasks1WithResponse(ctx, repository.ProjectKey, repository.Slug,
			&openapigenerated.GetDefaultTasks1Params{Limit: &limit})
		if err != nil {
			return Result{}, apperrors.Transport("failed to list default tasks", err)
		}

		body, status = response.Body, response.StatusCode()
	}

	if err := openapi.MapStatusError(status, body); err != nil {
		return Result{}, err
	}

	var page struct {
		Values []defaultTask `json:"values"`
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &page); err != nil {
			return Result{}, apperrors.New(apperrors.KindPermanent, "failed to decode default tasks list", err)
		}
	}

	candidates := make([]Candidate, 0, len(page.Values))
	for _, task := range page.Values {
		if task.ID == 0 || !ownScope(task.Scope.Type, wantProject) {
			continue
		}

		candidates = append(candidates, Candidate{
			Value: strconv.FormatInt(task.ID, 10),
			Description: describeDefaultTask(task.Description,
				matcherRef(task.SourceMatcher), matcherRef(task.TargetMatcher)),
		})
	}

	return Result{Candidates: candidates}, nil
}

// reviewerConditionSource offers the default reviewer conditions of the scope
// in hand.
//
// Scoped the same way branch restrictions are, and for the same reason: the
// repository's listing includes the project's conditions, and deleting one
// through the repository route removes it from the project -- 200, and the
// project has one condition fewer.
func reviewerConditionSource(ctx context.Context, environment *Environment, request Request) (Result, error) {
	client, err := environment.APIClient(ctx)
	if err != nil {
		return Result{}, err
	}

	service := reviewerservice.NewService(client)

	var conditions []openapigenerated.RestPullRequestCondition

	wantProject := projectScoped(commandPath(request.Command), environment.Flag("repo"), environment.Flag("project"))
	if wantProject {
		projectKey, err := environment.Project(ctx)
		if err != nil {
			return Result{}, err
		}

		conditions, err = service.ListProjectConditions(ctx, projectKey)
		if err != nil {
			return Result{}, err
		}
	} else {
		repository, err := environment.Repository(ctx)
		if err != nil {
			return Result{}, err
		}

		conditions, err = service.ListRepositoryConditions(ctx, repository.ProjectKey, repository.Slug)
		if err != nil {
			return Result{}, err
		}
	}

	candidates := make([]Candidate, 0, len(conditions))
	for _, condition := range conditions {
		if condition.Id == nil {
			continue
		}

		scope := ""
		if condition.Scope != nil {
			scope = string(condition.Scope.Type)
		}
		if !ownScope(scope, wantProject) {
			continue
		}

		source, target := "", ""
		if condition.SourceRefMatcher != nil {
			source = matcherRef(refMatcher{
				ID:        safederef.String(condition.SourceRefMatcher.Id),
				DisplayID: safederef.String(condition.SourceRefMatcher.DisplayId),
			})
		}
		if condition.TargetRefMatcher != nil {
			target = matcherRef(refMatcher{
				ID:        safederef.String(condition.TargetRefMatcher.Id),
				DisplayID: safederef.String(condition.TargetRefMatcher.DisplayId),
			})
		}

		candidates = append(candidates, Candidate{
			Value:       strconv.FormatInt(int64(*condition.Id), 10),
			Description: describeReviewerCondition(source, target, safederef.Int32(condition.RequiredApprovals)),
		})
	}

	return Result{Candidates: candidates}, nil
}

// requiredBuildSource offers the repository's required-build merge checks.
//
// Repository only: Bitbucket configures these per repository, and no bb
// command spells a project-level one.
func requiredBuildSource(ctx context.Context, environment *Environment, _ Request) (Result, error) {
	repository, err := environment.Repository(ctx)
	if err != nil {
		return Result{}, err
	}

	client, err := environment.APIClient(ctx)
	if err != nil {
		return Result{}, err
	}

	checks, err := qualityservice.NewService(client).ListRequiredBuildChecks(ctx,
		qualityservice.RepositoryRef{ProjectKey: repository.ProjectKey, Slug: repository.Slug}, maxCandidates)
	if err != nil {
		return Result{}, err
	}

	converted := result.RequiredBuildChecksFrom(checks)
	candidates := make([]Candidate, 0, len(converted))
	for _, check := range converted {
		if check.ID == 0 {
			continue
		}

		candidates = append(candidates, Candidate{
			Value: strconv.FormatInt(check.ID, 10),
			Description: describeRequiredBuild(
				firstNonEmpty(check.RefMatcher.DisplayID, check.RefMatcher.ID),
				check.BuildParentKeys,
			),
		})
	}

	return Result{Candidates: candidates}, nil
}

// labelSource offers the labels this repository actually carries.
//
// Only `bb repo label remove` reaches here. `add` names a label that may not
// exist yet and is declared free, which is what keeps this from suggesting
// exactly the values that command has nothing to do with.
func labelSource(ctx context.Context, environment *Environment, _ Request) (Result, error) {
	repository, err := environment.Repository(ctx)
	if err != nil {
		return Result{}, err
	}

	client, err := environment.APIClient(ctx)
	if err != nil {
		return Result{}, err
	}

	labels, err := reposettingsservice.NewService(client).ListRepositoryLabels(ctx,
		reposettingsservice.RepositoryRef{ProjectKey: repository.ProjectKey, Slug: repository.Slug})
	if err != nil {
		return Result{}, err
	}

	candidates := make([]Candidate, 0, len(labels))
	for _, label := range labels {
		if strings.TrimSpace(label) == "" {
			continue
		}

		// No description: a label is its own name, and Bitbucket stores
		// nothing else beside it.
		candidates = append(candidates, Candidate{Value: label})
	}

	return Result{Candidates: candidates}, nil
}

// accessKeySource offers the SSH access keys of the scope in hand.
//
// Two slots reach it: the key `bb repo ssh-key remove` takes away, and the
// keys a branch restriction lets past. Both are configured at either level,
// and `bb repo ssh-key` says which with --project or --repo rather than by
// being a different command.
func accessKeySource(ctx context.Context, environment *Environment, request Request) (Result, error) {
	client, err := environment.APIClient(ctx)
	if err != nil {
		return Result{}, err
	}

	service := sshkeyservice.NewService(client)

	var keys []openapigenerated.RestSshAccessKey

	if projectScoped(commandPath(request.Command), environment.Flag("repo"), environment.Flag("project")) {
		projectKey, err := environment.Project(ctx)
		if err != nil {
			return Result{}, err
		}

		keys, err = service.ListProjectKeys(ctx, projectKey, maxCandidates)
		if err != nil {
			return Result{}, err
		}
	} else {
		repository, err := environment.Repository(ctx)
		if err != nil {
			return Result{}, err
		}

		keys, err = service.ListRepoKeys(ctx, repository.ProjectKey, repository.Slug, maxCandidates)
		if err != nil {
			return Result{}, err
		}
	}

	candidates := make([]Candidate, 0, len(keys))
	for _, key := range keys {
		if key.Key == nil || key.Key.Id == nil {
			continue
		}

		permission := ""
		if key.Permission != nil {
			permission = string(*key.Permission)
		}

		candidates = append(candidates, Candidate{
			Value: strconv.FormatInt(int64(*key.Key.Id), 10),
			Description: describeAccessKey(
				safederef.String(key.Key.Label),
				permission,
				safederef.String(key.Key.Fingerprint),
			),
		})
	}

	return Result{Candidates: candidates}, nil
}

// projectScoped reports whether the invocation being completed addresses a
// project's copy of an object rather than a repository's.
//
// Three shapes, because the tree spells the choice three ways and a source
// that guessed one of them would hand out ids from the other -- every one of
// which is a 404.
//
//   - Everything under `bb project` is that project's by construction, and the
//     project key is an argument the Environment has already bound.
//   - `bb reviewer condition` takes both: a --repo names the repository's
//     conditions, and anything else -- --project, or the project the checkout
//     implies -- names the project's.
//   - `bb repo ssh-key` is the same choice again, spelled the other way round:
//     the keys are a repository's unless --project says otherwise. The command
//     refuses both flags together, so the check need not.
func projectScoped(commandPath, repoFlag, projectFlag string) bool {
	named := func(value string) bool { return strings.TrimSpace(value) != "" }

	switch {
	case strings.HasPrefix(commandPath, "project "):
		return true
	case strings.HasPrefix(commandPath, "reviewer condition "):
		return !named(repoFlag)
	case strings.HasPrefix(commandPath, "repo ssh-key "):
		return named(projectFlag) && !named(repoFlag)
	default:
		return false
	}
}

// describeComment is what a shell shows beside a comment id.
//
// The author and the first line, because that is how a person recognises a
// comment they are about to resolve or delete. The rest of the body is left
// out on purpose: a comment is paragraphs long and the line has room for one.
func describeComment(displayName, username, text string) string {
	author := firstNonEmpty(strings.TrimSpace(displayName), strings.TrimSpace(username))
	opening := firstLine(text)

	switch {
	case author != "" && opening != "":
		return author + ": " + opening
	case author != "":
		return author
	default:
		return opening
	}
}

// describeWebhook names the webhook and where it posts.
//
// Both, because neither alone identifies one: several webhooks point at the
// same CI server under different names, and several are called "build" and
// point at different hosts. An inactive one says so, since that is the
// difference between the webhook that is failing and its disabled twin.
func describeWebhook(name, url string, active bool) string {
	description := strings.TrimSpace(name)

	if trimmedURL := strings.TrimSpace(url); trimmedURL != "" {
		if description == "" {
			description = trimmedURL
		} else {
			description += " → " + trimmedURL
		}
	}

	if !active && description != "" {
		return description + " (inactive)"
	}

	return description
}

// describeRestriction says what the restriction forbids and where.
//
// The matcher is the half that distinguishes two restrictions of the same
// type, and the type is the half that distinguishes two restrictions on the
// same branch -- so neither is optional.
func describeRestriction(restrictionType, matcher string) string {
	kind := strings.TrimSpace(restrictionType)
	where := strings.TrimSpace(matcher)

	switch {
	case kind != "" && where != "":
		return kind + " on " + where
	case kind != "":
		return kind + " on any ref"
	default:
		return where
	}
}

// describeDefaultTask is the task's own text, with the refs it applies to when
// it does not apply to all of them.
func describeDefaultTask(description, sourceRef, targetRef string) string {
	text := firstLine(description)

	scope := []string{}
	if trimmed := strings.TrimSpace(sourceRef); trimmed != "" {
		scope = append(scope, "from "+trimmed)
	}
	if trimmed := strings.TrimSpace(targetRef); trimmed != "" {
		scope = append(scope, "to "+trimmed)
	}

	if len(scope) == 0 {
		return text
	}
	if text == "" {
		return strings.Join(scope, " ")
	}

	return text + " (" + strings.Join(scope, " ") + ")"
}

// describeReviewerCondition says which pull requests the condition covers.
//
// A condition has no name, so the refs it matches are the only thing telling
// two of them apart; the approval count is what tells apart two that cover the
// same refs.
func describeReviewerCondition(sourceRef, targetRef string, requiredApprovals int32) string {
	source := firstNonEmpty(strings.TrimSpace(sourceRef), "any")
	target := firstNonEmpty(strings.TrimSpace(targetRef), "any")

	description := source + " → " + target
	if requiredApprovals > 0 {
		description += ", " + strconv.FormatInt(int64(requiredApprovals), 10) + " approvals"
	}

	return description
}

// describeRequiredBuild says which branches the check guards and what has to
// be green on them.
func describeRequiredBuild(refPattern string, buildKeys []string) string {
	pattern := firstNonEmpty(strings.TrimSpace(refPattern), "any ref")

	keys := make([]string, 0, len(buildKeys))
	for _, key := range buildKeys {
		if trimmed := strings.TrimSpace(key); trimmed != "" {
			keys = append(keys, trimmed)
		}
	}

	if len(keys) == 0 {
		return pattern
	}

	return pattern + ": " + strings.Join(keys, ", ")
}

// describeAccessKey is the label somebody gave the key and what it may do.
//
// The fingerprint stands in for a key nobody labelled, which is the case where
// an id alone says least: two unlabelled deploy keys are told apart by nothing
// else.
func describeAccessKey(label, permission, fingerprint string) string {
	description := firstNonEmpty(strings.TrimSpace(label), strings.TrimSpace(fingerprint))

	if trimmedPermission := strings.TrimSpace(permission); trimmedPermission != "" {
		if description == "" {
			return trimmedPermission
		}

		return description + " (" + trimmedPermission + ")"
	}

	return description
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}

	return ""
}

// ownScope reports an object configured at the level being completed rather
// than inherited from the other one.
//
// The repository listings of branch restrictions, reviewer conditions and
// default tasks all include the project's, marked by scope.type, and Bitbucket
// does not refuse the repository route for one of them -- it acts on the
// project. Anything with no scope at all is kept: an endpoint that does not
// say is answering about the level it was asked about.
func ownScope(scopeType string, wantProject bool) bool {
	scope := strings.ToUpper(strings.TrimSpace(scopeType))

	switch scope {
	case "":
		return true
	case "PROJECT":
		return wantProject
	default:
		return !wantProject
	}
}

// anyRefMatcherIDs are how Bitbucket spells "this matches every ref".
//
// Two spellings for one thing: ANY_REF goes out on a create and
// ANY_REF_MATCHER_ID comes back on every read. Neither belongs in a
// description -- a rule that applies to everything is one with no refs worth
// naming, and printing the constant reads like a branch called
// ANY_REF_MATCHER_ID.
var anyRefMatcherIDs = map[string]bool{"ANY_REF": true, "ANY_REF_MATCHER_ID": true}

// matcherRef is the ref a matcher names, empty when it names all of them.
func matcherRef(matcher refMatcher) string {
	ref := firstNonEmpty(strings.TrimSpace(matcher.DisplayID), strings.TrimSpace(matcher.ID))
	if anyRefMatcherIDs[strings.ToUpper(ref)] {
		return ""
	}

	return ref
}
