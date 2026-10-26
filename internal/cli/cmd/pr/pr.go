package prcmd

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/git"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/git/execgit"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	codeowners "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/codeowners"
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
