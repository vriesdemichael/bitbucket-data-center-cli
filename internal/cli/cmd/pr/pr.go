package prcmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/diffoutput"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/prsel"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/git"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/git/execgit"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	codeowners "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/codeowners"
	diffservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/diff"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
	reviewerservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/reviewer"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
)

type PermissionChecker interface {
	CheckRepoPermission(ctx context.Context, projectKey, repoSlug string, permission openapigenerated.GetRepositories1ParamsPermission) error
}

type nopPermissionChecker struct{}

func (nopPermissionChecker) CheckRepoPermission(ctx context.Context, projectKey, repoSlug string, permission openapigenerated.GetRepositories1ParamsPermission) error {
	return nil
}

type Dependencies struct {
	JSONEnabled         func() bool
	DryRunEnabled       func() bool
	LoadConfig          func() (config.AppConfig, error)
	LoadConfigAndClient func() (config.AppConfig, *openapigenerated.ClientWithResponses, error)
	WriteJSON           func(io.Writer, any) error
	WriteJSONList       func(io.Writer, any, bool) error
	GitBackend          func() git.Backend
	PermissionChecker   func(*openapigenerated.ClientWithResponses) PermissionChecker
	// RepositoryWasInferred reports that --repo was filled in from the git
	// remote of the checkout rather than named by the caller. pr create takes
	// the checked-out branch as its source unattended only then. Optional: nil
	// reads as not inferred.
	RepositoryWasInferred func() bool
	// InferredRemote is the git remote the repository was inferred from, for
	// a message that tells someone to push to it. Optional: nil or empty
	// reads as unknown.
	InferredRemote func() string
}

func New(deps Dependencies) *cobra.Command {
	if deps.JSONEnabled == nil {
		deps.JSONEnabled = func() bool { return false }
	}
	if deps.DryRunEnabled == nil {
		deps.DryRunEnabled = func() bool { return false }
	}
	if deps.LoadConfig == nil {
		deps.LoadConfig = config.LoadFromEnv
	}
	if deps.LoadConfigAndClient == nil {
		deps.LoadConfigAndClient = func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
			cfg, err := deps.LoadConfig()
			if err != nil {
				return config.AppConfig{}, nil, err
			}
			client, err := openapi.NewClientWithResponsesFromConfig(cfg)
			if err != nil {
				return config.AppConfig{}, nil, apperrors.New(apperrors.KindInternal, "failed to initialize API client", err)
			}
			return cfg, client, nil
		}
	}
	if deps.WriteJSON == nil {
		deps.WriteJSON = jsonoutput.Write
	}
	if deps.WriteJSONList == nil {
		deps.WriteJSONList = jsonoutput.WriteList
	}
	if deps.GitBackend == nil {
		deps.GitBackend = func() git.Backend { return execgit.New() }
	}
	if deps.PermissionChecker == nil {
		deps.PermissionChecker = func(c *openapigenerated.ClientWithResponses) PermissionChecker {
			return nopPermissionChecker{}
		}
	}

	var repository string

	prCmd := &cobra.Command{
		Use:   "pr",
		Short: "Create, review, merge and inspect pull requests",
		Long: `Create, review, merge and inspect pull requests.

A command that takes <pr-id> takes a pull request in any of three forms: its
number, as 42 or #42; the branch it comes from, which picks that branch's open
pull request when it has one; or its URL, which names the repository too, so
--repo is not needed with it.`,
	}
	prCmd.PersistentFlags().StringVar(&repository, "repo", "", "Repository as PROJECT/slug (defaults to the repository of the git checkout you are in, then BITBUCKET_PROJECT_KEY + BITBUCKET_REPO_SLUG)")

	prCmd.AddCommand(newPullRequestDiffAlias(deps, &repository))
	prCmd.AddCommand(newPullRequestStatusCommand(deps, &repository))
	prCmd.AddCommand(newPullRequestCheckoutCommand(deps, &repository))
	prCmd.AddCommand(newPullRequestReadyCommand(deps, &repository))
	prCmd.AddCommand(newPullRequestListCommand(deps, &repository))
	prCmd.AddCommand(newPullRequestGetCommand(deps, &repository))
	prCmd.AddCommand(newPullRequestCommitsCommand(deps, &repository))
	prCmd.AddCommand(newPullRequestFilesCommand(deps, &repository))
	prCmd.AddCommand(newPullRequestMergeBaseCommand(deps, &repository))
	prCmd.AddCommand(newPullRequestCreateCommand(deps, &repository))
	prCmd.AddCommand(newPullRequestUpdateCommand(deps, &repository))
	prCmd.AddCommand(newPullRequestTransitionCommands(deps, &repository)...)
	prCmd.AddCommand(newPullRequestReviewCommand(deps, &repository))
	prCmd.AddCommand(newPullRequestJiraCommand(deps, &repository))
	prCmd.AddCommand(newPullRequestCommentCommand(deps, &repository))
	prCmd.AddCommand(newPullRequestActivityCommand(deps, &repository))
	prCmd.AddCommand(newPullRequestBuildCommands(deps, &repository)...)
	prCmd.AddCommand(newPullRequestAutoMergeCommand(deps, &repository))
	prCmd.AddCommand(newPullRequestWatchCommands(deps, &repository)...)
	prCmd.AddCommand(newPullRequestRebaseCommand(deps, &repository))
	prCmd.AddCommand(newPullRequestParticipantsCommand(deps, &repository))
	prCmd.AddCommand(newPullRequestDefaultReviewersCommand(deps, &repository))

	for _, path := range commandsReadingTheCurrentPullRequest {
		takeTheCurrentPullRequest(prCmd, path, deps, &repository)
	}

	return prCmd
}

func newPullRequestDiffAlias(deps Dependencies, repositorySelector *string) *cobra.Command {
	var patch bool
	var stat bool
	var nameOnly bool

	command := &cobra.Command{
		Use:   "diff <pr-id>",
		Short: "Diff a pull request (alias for bb diff pr)",
		Example: `  # The diff of a pull request
  bb pr diff 42 --repo PROJ/repo

  # Only the names of the files it changes
  bb pr diff 42 --repo PROJ/repo --name-only

  # As a patch, saved to a file
  bb pr diff 42 --repo PROJ/repo --patch > pr-42.patch`,
		Long: "Diff a pull request.\n\nAlias for bb diff pr, which is where the command reference documents it.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			target, err := prsel.Resolve(cmd.Context(), args[0], *repositorySelector, cfg, nil)
			if err != nil {
				return err
			}
			repo := diffservice.RepositoryRef{ProjectKey: target.ProjectKey, Slug: target.RepoSlug}

			service := diffservice.NewService(client)
			outputMode, err := diffoutput.ResolveOutputMode(patch, stat, nameOnly)
			if err != nil {
				return err
			}

			diffed, err := service.DiffPR(cmd.Context(), diffservice.DiffPRInput{
				Repository:    repo,
				PullRequestID: target.PullRequestID,
				Output:        outputMode,
			})
			if err != nil {
				return err
			}

			return diffoutput.Write(cmd.OutOrStdout(), deps.JSONEnabled(), result.Repository{ProjectKey: repo.ProjectKey, Slug: repo.Slug}, outputMode, diffed, deps.WriteJSON)
		},
	}

	command.Flags().BoolVar(&patch, "patch", false, "Output unified patch stream")
	command.Flags().BoolVar(&stat, "stat", false, "Output structured diff stats")
	command.Flags().BoolVar(&nameOnly, "name-only", false, "Output only changed file names")

	return command
}

// Registering an alias as a second flag bound to the same slice does not work:
// pflag tracks "has this flag been set" per flag, so the first use of each
// spelling replaces the slice instead of appending to it, and
// "--user alice --reviewers bob" silently drops alice. Normalizing the name
// during parsing routes every spelling to one flag, which appends correctly.

// createReviewerFlagAliases resolves the alias spellings accepted by `pr create`,
// where the canonical reviewer flag is --reviewers.
func createReviewerFlagAliases(_ *pflag.FlagSet, name string) pflag.NormalizedName {
	if name == "reviewer-groups" {
		name = "reviewer-group"
	}
	return pflag.NormalizedName(name)
}

// reviewerAddFlagAliases resolves the alias spellings accepted by
// `pr review reviewer add`, where the canonical reviewer flag is --user.
func reviewerAddFlagAliases(_ *pflag.FlagSet, name string) pflag.NormalizedName {
	switch name {
	case "users", "reviewers":
		name = "user"
	case "reviewer-groups":
		name = "reviewer-group"
	}
	return pflag.NormalizedName(name)
}

// resolveReviewersAndGroups expands individual reviewer usernames and reviewer group names into a deduplicated
// slice of reviewer usernames, excluding the PR author from expanded groups.
func resolveReviewersAndGroups(
	ctx context.Context,
	reviewerSvc *reviewerservice.Service,
	projectKey, repoSlug string,
	reviewers []string,
	reviewerGroups []string,
	author string,
) ([]string, error) {
	var directUsers []string
	var groupsToResolve []string
	explicitGroups := make(map[string]bool)

	for _, r := range reviewers {
		trimmed := strings.TrimSpace(r)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "@") {
			groupsToResolve = append(groupsToResolve, strings.TrimPrefix(trimmed, "@"))
		} else {
			directUsers = append(directUsers, trimmed)
		}
	}

	for _, g := range reviewerGroups {
		trimmed := strings.TrimPrefix(strings.TrimSpace(g), "@")
		if trimmed != "" {
			groupsToResolve = append(groupsToResolve, trimmed)
			explicitGroups[trimmed] = true
		}
	}

	seenUsers := make(map[string]bool)
	var finalReviewers []string

	// Direct users
	for _, u := range directUsers {
		lower := strings.ToLower(u)
		if !seenUsers[lower] {
			seenUsers[lower] = true
			finalReviewers = append(finalReviewers, u)
		}
	}

	// Group members (or @username fallback): exclude author
	for _, groupName := range groupsToResolve {
		members, err := reviewerSvc.ResolveReviewerGroupUsers(ctx, projectKey, repoSlug, groupName)
		if err != nil {
			// --reviewer-group names a group explicitly, so any failure to
			// resolve it is fatal. The "@name" shorthand is ambiguous, but only
			// a genuine "no such group" justifies rereading it as a username;
			// a transport or permission failure must not be papered over.
			if explicitGroups[groupName] || !isMissingResource(err) {
				return nil, err
			}
			if author != "" && strings.EqualFold(groupName, strings.TrimSpace(author)) {
				continue
			}
			lower := strings.ToLower(groupName)
			if !seenUsers[lower] {
				seenUsers[lower] = true
				finalReviewers = append(finalReviewers, groupName)
			}
			continue
		}
		for _, member := range members {
			trimmedMember := strings.TrimSpace(member)
			if trimmedMember == "" {
				continue
			}
			if author != "" && strings.EqualFold(trimmedMember, strings.TrimSpace(author)) {
				continue
			}
			lower := strings.ToLower(trimmedMember)
			if !seenUsers[lower] {
				seenUsers[lower] = true
				finalReviewers = append(finalReviewers, trimmedMember)
			}
		}
	}

	return finalReviewers, nil
}

// resolveCodeOwnersReviewers asks Bitbucket who owns the change.
//
// bb used to answer this itself: read .bitbucket/CODEOWNERS out of the target
// ref (or the working copy), match it against the diff, expand the groups and
// apply the selection strategies. Bitbucket does all of that in its bundled
// code-owners module, which is what the "add code owners" button in the pull
// request UI calls -- and the two disagreed. Bitbucket wants an at-sign before
// a user and "reviewer-group/" before a reviewer group; bb took a bare name as
// a user and a bare "@name" as a reviewer group, so one file assigned two
// people here and nobody there.
//
// The author is dropped because Bitbucket rejects a pull request whose author
// is also a reviewer, and the endpoint has no reason to know who is opening it.
func resolveCodeOwnersReviewers(
	ctx context.Context,
	cfg config.AppConfig,
	repository codeowners.RepositoryRef,
	sourceRepository *codeowners.RepositoryRef,
	sourceRef, targetRef string,
	author string,
) ([]string, error) {
	owners, err := codeowners.NewService(httpclient.NewFromConfig(cfg)).
		Owners(ctx, repository, sourceRef, targetRef, sourceRepository)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	reviewers := make([]string, 0, len(owners))
	for _, owner := range owners {
		trimmed := strings.TrimSpace(owner)
		if trimmed == "" {
			continue
		}
		if author != "" && strings.EqualFold(trimmed, strings.TrimSpace(author)) {
			continue
		}
		lower := strings.ToLower(trimmed)
		if seen[lower] {
			continue
		}
		seen[lower] = true
		reviewers = append(reviewers, trimmed)
	}

	return reviewers, nil
}

// isMissingResource reports whether an error means "this does not exist here"
// as opposed to "the request failed". Both a Bitbucket 404 envelope and an
// unrouted endpoint on an older server count as missing.
func isMissingResource(err error) bool {
	return err != nil && (apperrors.IsKind(err, apperrors.KindNotFound) || openapi.IsRouteMissing(err))
}

// writeWarning emits a non-fatal diagnostic. Warnings go to stderr so they never
// contaminate --json output on stdout.
func writeWarning(warn io.Writer, message string) {
	if warn == nil {
		return
	}
	fmt.Fprintf(warn, "warning: %s\n", message)
}

// partialReviewerAddError describes the outcome of a reviewer add that only
// partly succeeded.
//
// Reviewers are attached one request at a time, so the reviewers added before
// the failure stay on the pull request. The message names them, because under
// --json the failure envelope carries no payload and this message is the only
// place that record can live.
func partialReviewerAddError(added, failed []string, causes []error) error {
	var detail strings.Builder
	detail.WriteString("failed to add ")
	detail.WriteString(strings.Join(failed, ", "))
	if len(added) > 0 {
		detail.WriteString("; already added ")
		detail.WriteString(strings.Join(added, ", "))
	}
	detail.WriteString(": ")
	detail.WriteString(errors.Join(causes...).Error())

	return apperrors.New(apperrors.KindOf(causes[0]), detail.String(), errors.Join(causes...))
}

// resolveCurrentUsername determines the username Bitbucket will record for the
// caller: the author it stamps on a pull request, and the participant whose
// review status is the caller's own.
//
// The server's own name for the caller is preferred over the configured
// username: the latter is whatever the user typed when configuring the CLI and
// may be an email address or differ in case, and a mismatch means the author
// survives the reviewer filter and Bitbucket rejects the whole create with
// "author cannot be a reviewer". It is the login name, which reviewers are named
// by, and not the slug, which differs from it for a name with an @ or a + in it
// (#807).
//
// Under a token there may be no configured username at all, which is why the
// review dry runs ask here rather than reading the config directly. Without it
// they cannot find the caller among the reviewers, so they predict an update
// where the status is already what was asked for. Safe, but wrong, and the
// server will say so for free: X-AUSERNAME rides on any authenticated response.
func resolveCurrentUsername(ctx context.Context, client *httpclient.Client, cfg config.AppConfig) string {
	if name, err := client.CurrentUserName(ctx); err == nil {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			return trimmed
		}
	}

	return strings.TrimSpace(cfg.BitbucketUsername)
}

func formatReviewerList(users []string) string {
	if len(users) == 1 {
		return fmt.Sprintf("reviewer %s", users[0])
	}
	return fmt.Sprintf("reviewers %s", strings.Join(users, ", "))
}

func isAuthor(author, authorUsername, username string) bool {
	u := strings.TrimSpace(username)
	if u == "" {
		return false
	}
	return (author != "" && strings.EqualFold(u, strings.TrimSpace(author))) ||
		(authorUsername != "" && strings.EqualFold(u, strings.TrimSpace(authorUsername)))
}

// reviewStatuses are the review outcomes a participant can record, and
// threadStates are the comment thread filters. "unresolved" is a synonym for
// "open" that NormalizeThreadState has always accepted; naming it here makes
// the flag advertise what it already does rather than leaving it undocumented.
var (
	reviewStatuses = []string{"APPROVED", "NEEDS_WORK", "UNAPPROVED"}
	threadStates   = []string{"open", "unresolved", "resolved", "pending", "all"}

	// echoedThreadStates are the filters pr comment list writes back: every
	// one --state takes, except the synonym it normalises to open.
	echoedThreadStates = []string{"open", "resolved", "pending", "all"}
)

// mergeBlockerLines renders one line per veto for `bb pr get`.
//
// A veto carries a summary and a detail and either can be empty, so the line has
// to be built rather than read off one field: a veto with only a detail used to
// print an empty bullet. The two are joined only when they say different things,
// because Bitbucket repeats the summary as the detail for several of its own
// merge checks.
//
// It is deliberately not mergeBlockingReasons. That one feeds the dry-run
// preview's blockingReasons and joins the pair as "summary: detail" rather than
// "summary (detail)", and it adds a line for a conflict and a fallback for a
// veto-less refusal, neither of which belongs in a display list that only runs
// when there are vetoes. The two formats are a divergence worth removing, but
// blockingReasons is machine output and changing its shape is a contract change.
func mergeBlockerLines(blockers []pullrequestservice.MergeBlocker) []string {
	lines := make([]string, 0, len(blockers))

	for _, blocker := range blockers {
		summary := strings.TrimSpace(blocker.Summary)
		detail := strings.TrimSpace(blocker.Detail)

		switch {
		case summary != "" && detail != "" && !strings.EqualFold(summary, detail):
			lines = append(lines, fmt.Sprintf("%s (%s)", summary, detail))
		case summary != "":
			lines = append(lines, summary)
		case detail != "":
			lines = append(lines, detail)
		}
	}

	return lines
}

// mergeBlockingReasons turns Bitbucket's veto list into the preview's
// blockingReasons.
//
// Conflicts are named separately because Bitbucket reports a conflicted merge
// through its own flag rather than as a veto, so a conflicted pull request with
// no other veto would otherwise be blocked for no stated reason.
func mergeBlockingReasons(mergeability pullrequestservice.Mergeability) []string {
	reasons := []string{}

	if mergeability.Conflicted {
		reasons = append(reasons, "pull request has merge conflicts")
	}

	for _, blocker := range mergeability.Blockers {
		summary := strings.TrimSpace(blocker.Summary)
		detail := strings.TrimSpace(blocker.Detail)

		switch {
		case summary != "" && detail != "":
			reasons = append(reasons, summary+": "+detail)
		case summary != "":
			reasons = append(reasons, summary)
		case detail != "":
			reasons = append(reasons, detail)
		}
	}

	// Mergeable was false, so something refused it. Saying so without a reason
	// beats an empty list, which reads as "nothing is blocking".
	if len(reasons) == 0 {
		reasons = append(reasons, "bitbucket reported the pull request as not mergeable without naming a reason")
	}

	return reasons
}

// resolveSourceRepository turns --from-repo into the repository the source
// branch lives in.
//
// Nil when it was not given or names the target anyway, so the payload stays
// the same-repository shape it has always had and only a genuine fork to
// upstream pull request carries fromRef.repository (#506).
func resolveSourceRepository(selector string, target pullrequestservice.RepositoryRef) (*pullrequestservice.RepositoryRef, error) {
	trimmed := strings.TrimSpace(selector)
	if trimmed == "" {
		return nil, nil
	}

	projectKey, slug, found := strings.Cut(trimmed, "/")
	projectKey = strings.TrimSpace(projectKey)
	slug = strings.TrimSpace(slug)
	if !found || projectKey == "" || slug == "" {
		return nil, apperrors.New(apperrors.KindValidation,
			"--from-repo must be in PROJECT/slug form", nil)
	}

	if strings.EqualFold(projectKey, target.ProjectKey) && strings.EqualFold(slug, target.Slug) {
		return nil, nil
	}

	return &pullrequestservice.RepositoryRef{ProjectKey: projectKey, Slug: slug}, nil
}

// codeOwnersSourceRepository names the repository holding the source ref, and
// only when it is not the target: resolveSourceRepository returns nil for a
// same-repository pull request, which is also what the code-owners endpoint
// wants to be told nothing about.
func codeOwnersSourceRepository(source *pullrequestservice.RepositoryRef) *codeowners.RepositoryRef {
	if source == nil {
		return nil
	}

	return &codeowners.RepositoryRef{ProjectKey: source.ProjectKey, Slug: source.Slug}
}

// userExists reports whether Bitbucket has a user by that name.
func userExists(ctx context.Context, client *openapigenerated.ClientWithResponses, username string) (bool, error) {
	response, err := client.GetUserWithResponse(ctx, strings.TrimSpace(username))
	if err != nil {
		return false, apperrors.Transport(fmt.Sprintf("failed to look up user %q", username), err)
	}
	if response.StatusCode() == http.StatusNotFound {
		return false, nil
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return false, err
	}

	return true, nil
}
