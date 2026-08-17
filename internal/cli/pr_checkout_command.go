package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/style"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/git"
	pullrequestservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/pullrequest"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/transport/httpclient"
)

// checkoutPlan is what the command works out before it touches the repository.
//
// Resolving everything first means the failure modes that are knowable in
// advance — no repository, dirty tree, a fork we cannot address — are reported
// before any fetch, remote or branch has been created. A half-applied checkout
// is worse than a refused one.
type checkoutPlan struct {
	repositoryRoot string
	remoteName     string
	remoteURL      string
	remoteIsNew    bool
	sourceBranch   string
	localBranch    string
	branchExists   bool
	fork           bool
}

func newPullRequestCheckoutCommand(options *rootOptions, repositorySelector *string) *cobra.Command {
	var localBranchName string
	var force bool
	var detach bool

	command := &cobra.Command{
		Use:   "checkout <id>",
		Short: "Check out a pull request branch locally",
		Long: "Check out the source branch of a pull request in the current git repository.\n\n" +
			"Same-repository pull requests are checked out from the remote that already points at the " +
			"repository. Pull requests from a fork fetch from the fork, adding a remote for it when one " +
			"is not configured yet, so a later git push goes back to the fork branch the pull request is " +
			"built from.\n\n" +
			"Authentication uses whatever git is already configured to use for the host. Run " +
			"bb auth setup-git once if fetching prompts for credentials.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolvePullRequestRepositoryReference(*repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg)).WithAPIClient(client)
			pullRequest, err := service.Get(cmd.Context(), repo, strings.TrimSpace(args[0]))
			if err != nil {
				return err
			}

			backend := gitBackendFactory()
			if backend == nil {
				return apperrors.New(apperrors.KindInternal, "no git backend available", nil)
			}

			plan, err := planPullRequestCheckout(cmd.Context(), backend, cfg, repo, pullRequest, localBranchName, detach)
			if err != nil {
				return err
			}

			if !force {
				if err := refuseDirtyWorkingTree(cmd.Context(), backend, plan.repositoryRoot); err != nil {
					return err
				}
			}

			result, err := applyPullRequestCheckout(cmd.Context(), backend, plan, force, detach)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), result)
			}

			writeCheckoutResult(cmd, pullRequest, result)

			return nil
		},
	}

	command.Flags().StringVar(&localBranchName, "branch", "", "Local branch name to use (defaults to the pull request source branch)")
	command.Flags().BoolVar(&force, "force", false, "Check out even when tracked files have uncommitted changes, discarding them")
	command.Flags().BoolVar(&detach, "detach", false, "Check out the source commit without creating or moving a local branch")
	command.MarkFlagsMutuallyExclusive("branch", "detach")

	return command
}

type checkoutResult struct {
	PullRequest      int64  `json:"pull_request"`
	Branch           string `json:"branch,omitempty"`
	Detached         bool   `json:"detached"`
	Remote           string `json:"remote"`
	RemoteURL        string `json:"remote_url"`
	RemoteAdded      bool   `json:"remote_added"`
	SourceBranch     string `json:"source_branch"`
	SourceRepository string `json:"source_repository"`
	Fork             bool   `json:"fork"`
	// FastForwarded records that an existing local branch was advanced rather
	// than created, which is the difference between "this is the pull request"
	// and "this was the pull request when you last ran it".
	FastForwarded bool `json:"fast_forwarded"`
}

func planPullRequestCheckout(
	ctx context.Context,
	backend git.Backend,
	cfg config.AppConfig,
	repo pullrequestservice.RepositoryRef,
	pullRequest pullrequestservice.PullRequest,
	requestedBranch string,
	detach bool,
) (checkoutPlan, error) {
	sourceBranch := strings.TrimSpace(pullRequest.SourceBranch)
	if sourceBranch == "" {
		return checkoutPlan{}, apperrors.New(
			apperrors.KindNotFound,
			fmt.Sprintf("pull request %d does not report a source branch to check out", pullRequest.ID),
			nil,
		)
	}

	workingDirectory, err := os.Getwd()
	if err != nil {
		return checkoutPlan{}, apperrors.New(apperrors.KindInternal, "failed to determine the working directory", err)
	}

	repositoryRoot, err := backend.RepositoryRoot(ctx, workingDirectory)
	if err != nil {
		if isNonRepositoryError(err) {
			return checkoutPlan{}, apperrors.New(
				apperrors.KindValidation,
				"bb pr checkout needs a git repository: run it inside a clone of the target repository, or clone it first with bb repo clone",
				nil,
			)
		}
		return checkoutPlan{}, err
	}

	sourceRepository := pullRequest.SourceRepository
	if sourceRepository == nil {
		// Bitbucket always reports fromRef.repository, so an absent one means a
		// payload we do not understand. Assuming same-repository would fetch
		// from the wrong place, which is worse than saying so.
		return checkoutPlan{}, apperrors.New(
			apperrors.KindNotFound,
			fmt.Sprintf("pull request %d does not report a source repository to fetch from", pullRequest.ID),
			nil,
		)
	}

	fork := !strings.EqualFold(strings.TrimSpace(sourceRepository.ProjectKey), strings.TrimSpace(repo.ProjectKey)) ||
		!strings.EqualFold(strings.TrimSpace(sourceRepository.Slug), strings.TrimSpace(repo.Slug))

	remoteName, remoteURL, remoteIsNew, err := resolveCheckoutRemote(ctx, backend, cfg, repositoryRoot, *sourceRepository)
	if err != nil {
		return checkoutPlan{}, err
	}

	plan := checkoutPlan{
		repositoryRoot: repositoryRoot,
		remoteName:     remoteName,
		remoteURL:      remoteURL,
		remoteIsNew:    remoteIsNew,
		sourceBranch:   sourceBranch,
		fork:           fork,
	}

	if detach {
		return plan, nil
	}

	plan.localBranch = strings.TrimSpace(requestedBranch)
	if plan.localBranch == "" {
		plan.localBranch = defaultCheckoutBranchName(sourceBranch, *sourceRepository, fork)
	}

	plan.branchExists, err = backend.BranchExists(ctx, repositoryRoot, plan.localBranch)
	if err != nil {
		return checkoutPlan{}, err
	}

	return plan, nil
}

// defaultCheckoutBranchName keeps a fork's branch from colliding with a local
// branch of the same name.
//
// Two contributors both working on "fix/login" in their own forks is ordinary,
// and so is having a local "fix/login" of your own. Prefixing with the fork's
// project key is what gh does with the fork owner, and it keeps the result
// readable rather than unique-but-opaque.
func defaultCheckoutBranchName(sourceBranch string, sourceRepository pullrequestservice.RepositoryRef, fork bool) string {
	if !fork {
		return sourceBranch
	}

	// Personal forks live under ~username, and a leading tilde in a branch name
	// is legal but reads like a typo.
	owner := strings.TrimPrefix(strings.TrimSpace(sourceRepository.ProjectKey), "~")
	if owner == "" {
		return sourceBranch
	}

	return strings.ToLower(owner) + "/" + sourceBranch
}

// resolveCheckoutRemote finds the remote that already points at the source
// repository, or names a new one for it.
//
// Reusing an existing remote matters more than it looks: adding a second remote
// for a repository the caller already tracks would leave two names for one
// place, and a later push could go to whichever one git resolved.
func resolveCheckoutRemote(
	ctx context.Context,
	backend git.Backend,
	cfg config.AppConfig,
	repositoryRoot string,
	sourceRepository pullrequestservice.RepositoryRef,
) (string, string, bool, error) {
	remotes, err := backend.ListRemotes(ctx, repositoryRoot)
	if err != nil {
		return "", "", false, err
	}

	for _, remote := range remotes {
		_, projectKey, slug, ok := parseBitbucketRemote(remote.URL)
		if !ok {
			continue
		}
		if strings.EqualFold(projectKey, sourceRepository.ProjectKey) && strings.EqualFold(slug, sourceRepository.Slug) {
			return remote.Name, remote.URL, false, nil
		}
	}

	cloneURL, err := buildBitbucketCloneURL(normalizeHTTPCloneHost(cfg.BitbucketURL), sourceRepository.ProjectKey, sourceRepository.Slug)
	if err != nil {
		return "", "", false, err
	}

	name := checkoutRemoteName(sourceRepository, remotes)

	return name, cloneURL, true, nil
}

// checkoutRemoteName picks a name for a fork remote that is not already taken.
func checkoutRemoteName(sourceRepository pullrequestservice.RepositoryRef, remotes []git.Remote) string {
	base := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(sourceRepository.ProjectKey), "~"))
	if base == "" {
		base = "fork"
	}

	taken := make(map[string]struct{}, len(remotes))
	for _, remote := range remotes {
		taken[strings.ToLower(strings.TrimSpace(remote.Name))] = struct{}{}
	}

	candidate := base
	for suffix := 2; ; suffix++ {
		if _, exists := taken[candidate]; !exists {
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d", base, suffix)
	}
}

func refuseDirtyWorkingTree(ctx context.Context, backend git.Backend, repositoryRoot string) error {
	status, err := backend.WorkingTreeState(ctx, repositoryRoot)
	if err != nil {
		return err
	}
	if !status.Dirty {
		return nil
	}

	preview := status.Entries
	if len(preview) > 5 {
		preview = append(preview[:5:5], fmt.Sprintf("... and %d more", len(status.Entries)-5))
	}

	return apperrors.New(
		apperrors.KindValidation,
		fmt.Sprintf(
			"working tree has uncommitted changes to tracked files; commit or stash them, or pass --force to discard them:\n  %s",
			strings.Join(preview, "\n  "),
		),
		nil,
	)
}

func applyPullRequestCheckout(
	ctx context.Context,
	backend git.Backend,
	plan checkoutPlan,
	force bool,
	detach bool,
) (checkoutResult, error) {
	result := checkoutResult{
		Branch:       plan.localBranch,
		Detached:     detach,
		Remote:       plan.remoteName,
		RemoteURL:    plan.remoteURL,
		RemoteAdded:  plan.remoteIsNew,
		SourceBranch: plan.sourceBranch,
		Fork:         plan.fork,
	}

	if plan.remoteIsNew {
		if err := backend.AddRemote(ctx, plan.repositoryRoot, git.Remote{Name: plan.remoteName, URL: plan.remoteURL}); err != nil {
			return checkoutResult{}, err
		}
	}

	// An explicit refspec rather than a bare fetch: the remote may have just
	// been added with no configured refspec, and naming the one branch avoids
	// pulling an entire fork to check out one pull request.
	remoteTrackingRef := fmt.Sprintf("refs/remotes/%s/%s", plan.remoteName, plan.sourceBranch)
	refspec := fmt.Sprintf("+refs/heads/%s:%s", plan.sourceBranch, remoteTrackingRef)
	if err := backend.Fetch(ctx, plan.repositoryRoot, git.FetchOptions{Remote: plan.remoteName, Refspecs: []string{refspec}}); err != nil {
		return checkoutResult{}, err
	}

	if detach {
		if err := backend.Checkout(ctx, plan.repositoryRoot, git.CheckoutOptions{Ref: remoteTrackingRef, Detach: true, Force: force}); err != nil {
			return checkoutResult{}, err
		}
		return result, nil
	}

	if plan.branchExists {
		if err := backend.Checkout(ctx, plan.repositoryRoot, git.CheckoutOptions{Ref: plan.localBranch, Force: force}); err != nil {
			return checkoutResult{}, err
		}
		// Checking out a branch created by an earlier run leaves it wherever it
		// was then. Without this the caller silently reviews an old revision.
		if err := backend.FastForward(ctx, plan.repositoryRoot, remoteTrackingRef); err != nil {
			return checkoutResult{}, err
		}
		result.FastForwarded = true
	} else if err := backend.Checkout(ctx, plan.repositoryRoot, git.CheckoutOptions{
		Ref:       remoteTrackingRef,
		NewBranch: plan.localBranch,
		Force:     force,
	}); err != nil {
		return checkoutResult{}, err
	}

	// Upstream is set explicitly rather than via checkout --track so it is the
	// same two keys whether the branch was created now or by an earlier run,
	// and so a fork branch pushes back to the fork rather than to the target
	// repository the caller happens to be standing in.
	if err := setBranchUpstream(ctx, backend, plan); err != nil {
		return checkoutResult{}, err
	}

	return result, nil
}

func setBranchUpstream(ctx context.Context, backend git.Backend, plan checkoutPlan) error {
	settings := []struct {
		key   string
		value string
	}{
		{key: fmt.Sprintf("branch.%s.remote", plan.localBranch), value: plan.remoteName},
		{key: fmt.Sprintf("branch.%s.merge", plan.localBranch), value: "refs/heads/" + plan.sourceBranch},
	}

	for _, setting := range settings {
		if err := backend.SetConfig(ctx, git.ConfigOptions{
			Directory: plan.repositoryRoot,
			Scope:     git.ConfigScopeLocal,
			Key:       setting.key,
			Value:     setting.value,
		}); err != nil {
			return err
		}
	}

	return nil
}

func writeCheckoutResult(cmd *cobra.Command, pullRequest pullrequestservice.PullRequest, result checkoutResult) {
	writer := cmd.OutOrStdout()

	if result.RemoteAdded {
		fmt.Fprintf(writer, "%s remote %s for %s\n", style.Success.Render("Added"), style.Resource.Render(result.Remote), result.RemoteURL)
	}

	if result.Detached {
		fmt.Fprintf(
			writer,
			"%s #%d at %s (detached HEAD)\n",
			style.Success.Render("Checked out"),
			pullRequest.ID,
			style.Resource.Render(result.Remote+"/"+result.SourceBranch),
		)
		return
	}

	verb := "Checked out"
	if result.FastForwarded {
		verb = "Updated"
	}

	fmt.Fprintf(
		writer,
		"%s #%d on branch %s tracking %s\n",
		style.Success.Render(verb),
		pullRequest.ID,
		style.Resource.Render(result.Branch),
		result.Remote+"/"+result.SourceBranch,
	)
}
