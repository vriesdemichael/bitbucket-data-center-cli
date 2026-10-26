package prcmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/dryrunpreview"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/preflight"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/prsel"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
)

func newPullRequestRebaseCommand(deps Dependencies, repository *string) *cobra.Command {
	var rebaseVersion int
	rebaseCmd := &cobra.Command{
		Use:   "rebase <pr-id>",
		Short: "Rebase a pull request",
		Example: `  # Rebase a pull request's branch onto its target
  bb pr rebase 42 --repo PROJ/repo`,
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
				if err := preflight.RepoPermission(cmd.Context(), deps.PermissionChecker, apiClient, repo.ProjectKey, repo.Slug, openapi.RepoWrite); err != nil {
					return err
				}

				// The state as well as the rebase check. Bitbucket's check answers
				// a declined pull request with canRebase false and no vetoes, and
				// the generated model carries only the vetoes, so read alone it
				// passed a rebase the real run is refused with a 409.
				current, err := service.Get(cmd.Context(), repo, target.PullRequestID)
				if err != nil {
					return err
				}

				rebaseability, err := service.CanRebase(cmd.Context(), repo, target.PullRequestID)
				if err != nil {
					return err
				}

				predicted := "update"
				reason := "pull request will be rebased"
				blocking := []string{}
				fails := apperrors.KindConflict
				switch {
				case !current.Open:
					predicted = "blocked"
					reason = fmt.Sprintf("pull request is %s; only an open pull request can be rebased", current.State)
					blocking = []string{reason}
				case rebaseability != nil && rebaseability.Vetoes != nil && len(*rebaseability.Vetoes) > 0:
					// What Bitbucket vetoes here it does not refuse up front: the
					// rebase runs, and the update of the source branch is then
					// vetoed with a 400 RepositoryHookVetoedException, which is
					// validation. Seen with branch permissions on the source
					// branch, the veto a caller meets most.
					fails = apperrors.KindValidation
					predicted = "blocked"
					reason = "rebase is vetoed"
					for _, veto := range *rebaseability.Vetoes {
						msg := ""
						if veto.SummaryMessage != nil {
							msg = *veto.SummaryMessage
						}
						if veto.DetailedMessage != nil {
							if msg != "" {
								msg += ": "
							}
							msg += *veto.DetailedMessage
						}
						if msg != "" {
							blocking = append(blocking, msg)
						}
					}
					if len(blocking) > 0 {
						reason = strings.Join(blocking, "; ")
					}
				}

				// Bitbucket's rebase check reports vetoes, not the file
				// conflicts that refuse a rebase too. A refusal it reports is
				// checked; a rebase it does not refuse is only predicted to go
				// through (ADR-078).
				tier := dryrunpreview.TierPredicted
				if predicted == "blocked" {
					tier = dryrunpreview.TierPreconditionsChecked
				}

				preview := dryrunpreview.New(dryrunpreview.Item{
					Intent:          "pr.rebase",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": target.PullRequestID},
					Action:          "update",
					PredictedAction: predicted,
					Tier:            tier,
					Reason:          reason,
					BlockingReasons: blocking,
					Fails:           fails,
				})

				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			var version *int
			if cmd.Flags().Changed("version") {
				version = &rebaseVersion
			}

			rebased, err := service.Rebase(cmd.Context(), repo, target.PullRequestID, version)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), rebaseResultFrom(repositoryOf(repo), rebased))
			}

			// A branch already on its target's tip is rebased in the sense that
			// matters, and saying so is different from claiming commits moved.
			if rebased == nil || rebased.RefChange == nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Pull request #%s is already on top of its target; nothing to rebase\n", target.PullRequestID)

				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Rebased pull request #%s\n", target.PullRequestID)

			return nil
		},
	}
	rebaseCmd.Flags().IntVar(&rebaseVersion, "version", 0, "Expected pull request version")
	return rebaseCmd
}
