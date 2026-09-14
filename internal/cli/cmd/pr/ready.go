package prcmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/dryrunpreview"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/preflight"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/prsel"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
)

// newPullRequestReadyCommand is `bb pr ready`, the gh verb for the draft flag.
//
// bb pr update --draft sets the same field. This exists because update requires
// --version, and a caller marking a draft ready has no reason to know it: the
// version is read here, and a stale read is retried once (#598).
//
// It takes no --version of its own. A caller who wants the lock has bb pr
// update, and a second command offering it would be a second spelling of that
// one rather than of gh's.
func newPullRequestReadyCommand(deps Dependencies, repositorySelector *string) *cobra.Command {
	var undo bool

	cmd := &cobra.Command{
		Use:   "ready <id>",
		Short: "Mark a draft pull request ready for review, or turn it back into a draft",
		Long: "Mark a draft pull request ready for review. With --undo, turn an open pull request back into a draft.\n\n" +
			"No --version is needed: bb reads the pull request's current version itself. If the pull request " +
			"changes between that read and the update, so that Bitbucket refuses the version as out of date, " +
			"bb reads it again and retries once.\n\n" +
			"A pull request already in the requested state is left as it is, and the command succeeds saying " +
			"nothing changed. A merged or declined pull request is refused: only an open one can be marked " +
			"ready or turned into a draft.\n\n" +
			"bb pr update --draft sets the same flag alongside other fields, and takes --version for a caller " +
			"who wants the change refused if the pull request has moved on since they read it.",
		Example: "  # Mark a draft pull request ready for review\n" +
			"  bb pr ready 42 --repo PROJ/repo\n\n" +
			"  # Turn it back into a draft\n" +
			"  bb pr ready 42 --repo PROJ/repo --undo",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, apiClient, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
			target, err := prsel.Resolve(cmd.Context(), args[0], *repositorySelector, cfg, service)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()
			draft := undo

			if deps.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), deps.PermissionChecker, apiClient, repo.ProjectKey, repo.Slug, openapi.RepoWrite); err != nil {
					return err
				}

				current, err := service.Get(cmd.Context(), repo, target.PullRequestID)
				if err != nil {
					return err
				}

				preview := dryrunpreview.New(dryrunpreview.PlanningModeStateful, dryrunpreview.CapabilityFull,
					readyPreviewItem(repo, target.PullRequestID, current, draft))

				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			updated, changed, err := service.SetDraft(cmd.Context(), repo, target.PullRequestID, draft)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), DraftStateChange{
					Repository:  repositoryOf(repo),
					PullRequest: result.PullRequestFrom(updated),
					Changed:     changed,
				})
			}

			fmt.Fprintln(cmd.OutOrStdout(), readyMessage(target.PullRequestID, draft, changed))

			return nil
		},
	}
	cmd.Flags().BoolVar(&undo, "undo", false, "Turn the pull request back into a draft instead")

	return cmd
}

// readyPreviewItem predicts bb pr ready from the pull request as it stands, by
// the rules SetDraft applies.
func readyPreviewItem(repo pullrequestservice.RepositoryRef, pullRequestID string, current pullrequestservice.PullRequest, draft bool) dryrunpreview.Item {
	item := dryrunpreview.Item{
		Intent:          "pr.ready",
		Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": pullRequestID, "draft": draft},
		Action:          "update",
		PredictedAction: "update",
		Tier:            dryrunpreview.TierPreconditionsChecked,
		Supported:       true,
		Reason:          "pull request will be marked ready for review",
		RequiredState:   []string{"pull request"},
	}
	if draft {
		item.Reason = "pull request will be turned into a draft"
	}

	if refusal := pullrequestservice.DraftChangeRefusal(current); refusal != nil {
		item.PredictedAction = "blocked"
		item.Reason = refusal.Error()
		item.BlockingReasons = []string{refusal.Error()}

		return item
	}

	if current.Draft == draft {
		item.PredictedAction = "no-op"
		item.Reason = "pull request is already ready for review"
		if draft {
			item.Reason = "pull request is already a draft"
		}
	}

	return item
}

// readyMessage is what bb pr ready tells a person.
func readyMessage(pullRequestID string, draft, changed bool) string {
	switch {
	case draft && changed:
		return fmt.Sprintf("Pull request #%s is now a draft", pullRequestID)
	case draft:
		return fmt.Sprintf("Pull request #%s is already a draft; nothing changed", pullRequestID)
	case changed:
		return fmt.Sprintf("Pull request #%s is ready for review", pullRequestID)
	default:
		return fmt.Sprintf("Pull request #%s is already ready for review; nothing changed", pullRequestID)
	}
}
