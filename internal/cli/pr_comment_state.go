package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/style"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
	commentservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/comment"
)

// newPullRequestCommentStateCommands builds `pr comment resolve` and
// `pr comment reopen`.
//
// These are what replaced marking a pull request task done. Bitbucket removed
// pull request tasks in 8.0 and folded them into comments carrying a blocker
// severity, so resolving the comment is what closing the task became. `bb`
// could already create one (`pr comment add --blocker`) and list them
// (`pr comment list --tasks-only`) but not close one, which left the workflow
// with no ending.
func newPullRequestCommentStateCommands(options *rootOptions, repository *string) []*cobra.Command {
	type stateCommand struct {
		use      string
		short    string
		intent   string
		reason   string
		state    commentservice.CommentState
		doneWord string
	}

	specs := []stateCommand{
		{
			use:      "resolve <pr-id> <comment-id>",
			short:    "Resolve a pull request comment, closing it as a task",
			intent:   "pr.comment.resolve",
			reason:   "comment will be resolved",
			state:    commentservice.CommentStateResolved,
			doneWord: "Resolved",
		},
		{
			use:      "reopen <pr-id> <comment-id>",
			short:    "Reopen a resolved pull request comment",
			intent:   "pr.comment.reopen",
			reason:   "comment will be reopened",
			state:    commentservice.CommentStateOpen,
			doneWord: "Reopened",
		},
	}

	commands := make([]*cobra.Command, 0, len(specs))
	for _, spec := range specs {
		commands = append(commands, newCommentStateCommand(options, repository, spec.use, spec.short, spec.intent, spec.reason, spec.state, spec.doneWord))
	}

	return commands
}

func newCommentStateCommand(
	options *rootOptions,
	repository *string,
	use string,
	short string,
	intent string,
	reason string,
	state commentservice.CommentState,
	doneWord string,
) *cobra.Command {
	command := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolvePullRequestRepositoryReference(*repository, cfg)
			if err != nil {
				return err
			}

			prID := args[0]
			commentID := args[1]

			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOREAD); err != nil {
					return err
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          intent,
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "pr_id": prID, "comment_id": commentID, "state": string(state)},
						Action:          "update",
						PredictedAction: "update",
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"pull request comment"},
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1, UpdateCount: 1},
				}
				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}

			target := commentservice.Target{
				Repository:    commentservice.RepositoryRef{ProjectKey: repo.ProjectKey, Slug: repo.Slug},
				PullRequestID: prID,
			}

			updated, err := commentservice.NewService(client).SetState(cmd.Context(), target, commentID, state, nil)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "pull_request_id": prID, "comment": updated})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s comment %s\n", style.Success.Render(doneWord), style.Resource.Render(commentID))
			return nil
		},
	}

	return command
}
