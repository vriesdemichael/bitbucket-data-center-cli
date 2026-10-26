package prcmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/dryrunpreview"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/preflight"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/prsel"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
)

func newPullRequestWatchCommands(deps Dependencies, repository *string) []*cobra.Command {
	watchCmd := &cobra.Command{
		Use:   "watch <pr-id>",
		Short: "Watch a pull request",
		Example: `  # Get notified of changes to a pull request
  bb pr watch 42 --repo PROJ/repo`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, apiClient, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg)).WithAPIClient(apiClient)
			target, err := prsel.Resolve(cmd.Context(), args[0], *repository, cfg, service)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			if deps.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), deps.PermissionChecker, apiClient, repo.ProjectKey, repo.Slug, openapi.RepoRead); err != nil {
					return err
				}

				if _, err := service.Get(cmd.Context(), repo, target.PullRequestID); err != nil {
					return err
				}

				// Every effect says why (ADR-096). This one also says what it
				// did not look at, since that is why it is only predicted.
				preview := dryrunpreview.New(dryrunpreview.Item{
					Intent:          "pr.watch",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": target.PullRequestID},
					Action:          "update",
					PredictedAction: "update",
					Reason:          "you will watch the pull request; whether you already do is not checked",
				})
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			err = service.Watch(cmd.Context(), repo, target.PullRequestID)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), WatchState{Repository: repositoryOf(repo), PullRequestID: target.PullRequestID, Watched: true})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Watching pull request #%s\n", target.PullRequestID)
			return nil
		},
	}

	unwatchCmd := &cobra.Command{
		Use:   "unwatch <pr-id>",
		Short: "Unwatch a pull request",
		Example: `  # Stop notifications for a pull request
  bb pr unwatch 42 --repo PROJ/repo`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, apiClient, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg)).WithAPIClient(apiClient)
			target, err := prsel.Resolve(cmd.Context(), args[0], *repository, cfg, service)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			if deps.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), deps.PermissionChecker, apiClient, repo.ProjectKey, repo.Slug, openapi.RepoRead); err != nil {
					return err
				}

				if _, err := service.Get(cmd.Context(), repo, target.PullRequestID); err != nil {
					return err
				}

				preview := dryrunpreview.New(dryrunpreview.Item{
					Intent:          "pr.unwatch",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": target.PullRequestID},
					Action:          "delete",
					PredictedAction: "delete",
					Reason:          "you will stop watching the pull request; whether you watch it now is not checked",
				})
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			err = service.Unwatch(cmd.Context(), repo, target.PullRequestID)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), WatchState{Repository: repositoryOf(repo), PullRequestID: target.PullRequestID, Watched: false})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Unwatching pull request #%s\n", target.PullRequestID)
			return nil
		},
	}
	return []*cobra.Command{watchCmd, unwatchCmd}
}
