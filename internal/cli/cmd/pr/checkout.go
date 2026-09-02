package prcmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/giturl"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/prsel"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/style"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/git"
	pullrequestservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/pullrequest"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/transport/httpclient"
)

type checkoutPlan struct {
	repositoryRoot   string
	remoteName       string
	remoteURL        string
	remoteIsNew      bool
	pullRequest      int64
	sourceBranch     string
	sourceRepository string
	localBranch      string
	branchExists     bool
	fork             bool
	credentials      *git.Credentials
}

func newPullRequestCheckoutCommand(deps Dependencies, repositorySelector *string) *cobra.Command {
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
			"The fetch uses the credentials bb is already authenticated with, so no git credential setup " +
			"is needed first. Pushing afterwards is plain git and does need one: run bb auth setup-git once.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg)).WithAPIClient(client)
			target, err := prsel.Resolve(cmd.Context(), args[0], *repositorySelector, cfg, service)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			pullRequest, err := service.Get(cmd.Context(), repo, target.PullRequestID)
			if err != nil {
				return err
			}

			backend := deps.GitBackend()
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

			outcome, err := applyPullRequestCheckout(cmd.Context(), backend, plan, force, detach)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), checkoutFrom(outcome))
			}

			writeCheckoutResult(cmd, pullRequest, outcome)

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
	FastForwarded    bool   `json:"fast_forwarded"`
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
		if giturl.IsNonRepositoryError(err) {
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
		repositoryRoot:   repositoryRoot,
		remoteName:       remoteName,
		remoteURL:        remoteURL,
		remoteIsNew:      remoteIsNew,
		pullRequest:      pullRequest.ID,
		sourceBranch:     sourceBranch,
		sourceRepository: sourceRepository.ProjectKey + "/" + sourceRepository.Slug,
		fork:             fork,
		credentials:      checkoutCredentials(cfg, remoteURL),
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

func defaultCheckoutBranchName(sourceBranch string, sourceRepository pullrequestservice.RepositoryRef, fork bool) string {
	if !fork {
		return sourceBranch
	}

	owner := strings.TrimPrefix(strings.TrimSpace(sourceRepository.ProjectKey), "~")
	if owner == "" {
		return sourceBranch
	}

	return strings.ToLower(owner) + "/" + sourceBranch
}

func checkoutCredentials(cfg config.AppConfig, remoteURL string) *git.Credentials {
	credentials := git.Credentials{
		URL:      remoteURL,
		Token:    strings.TrimSpace(cfg.BitbucketToken),
		Username: strings.TrimSpace(cfg.BitbucketUsername),
		Password: cfg.BitbucketPassword,
	}

	if credentials.Token == "" && (credentials.Username == "" || credentials.Password == "") {
		return nil
	}

	return &credentials
}

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
		_, projectKey, slug, ok := giturl.ParseBitbucketRemote(remote.URL)
		if !ok {
			continue
		}
		if strings.EqualFold(projectKey, sourceRepository.ProjectKey) && strings.EqualFold(slug, sourceRepository.Slug) {
			return remote.Name, remote.URL, false, nil
		}
	}

	cloneURL, err := giturl.BuildBitbucketCloneURL(giturl.NormalizeHTTPCloneHost(cfg.BitbucketURL), sourceRepository.ProjectKey, sourceRepository.Slug)
	if err != nil {
		return "", "", false, err
	}

	name := checkoutRemoteName(sourceRepository, remotes)

	return name, cloneURL, true, nil
}

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
		PullRequest:      plan.pullRequest,
		Branch:           plan.localBranch,
		Detached:         detach,
		Remote:           plan.remoteName,
		RemoteURL:        plan.remoteURL,
		RemoteAdded:      plan.remoteIsNew,
		SourceBranch:     plan.sourceBranch,
		SourceRepository: plan.sourceRepository,
		Fork:             plan.fork,
	}

	if plan.remoteIsNew {
		if err := backend.AddRemote(ctx, plan.repositoryRoot, git.Remote{Name: plan.remoteName, URL: plan.remoteURL}); err != nil {
			return checkoutResult{}, err
		}
	}

	remoteTrackingRef := fmt.Sprintf("refs/remotes/%s/%s", plan.remoteName, plan.sourceBranch)
	refspec := fmt.Sprintf("+refs/heads/%s:%s", plan.sourceBranch, remoteTrackingRef)
	if err := backend.Fetch(ctx, plan.repositoryRoot, git.FetchOptions{
		Remote:      plan.remoteName,
		Refspecs:    []string{refspec},
		Credentials: plan.credentials,
	}); err != nil {
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
