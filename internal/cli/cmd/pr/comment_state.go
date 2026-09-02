package prcmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/dryrunpreview"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/prsel"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/style"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	commentservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/comment"
)

func newPullRequestCommentStateCommands(deps Dependencies, repository *string) []*cobra.Command {
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
		commands = append(commands, newCommentStateCommand(deps, repository, spec.use, spec.short, spec.intent, spec.reason, spec.state, spec.doneWord))
	}

	return commands
}

func newCommentStateCommand(
	deps Dependencies,
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
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			prTarget, err := prsel.Resolve(cmd.Context(), args[0], *repository, cfg, nil)
			if err != nil {
				return err
			}
			repo := prTarget.RepositoryRef()
			prID := prTarget.PullRequestID
			commentID := args[1]

			if deps.DryRunEnabled() {
				checker := deps.PermissionChecker(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapi.RepoRead); err != nil {
					return err
				}

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          intent,
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "prId": prID, "commentId": commentID, "state": string(state)},
						Action:          "update",
						PredictedAction: "update",
						Supported:       true,
						Reason:          reason,
						Confidence:      dryrunpreview.CapabilityFull,
						RequiredState:   []string{"pull request comment"},
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1, UpdateCount: 1},
				}
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			target := commentservice.Target{
				Repository:    commentservice.RepositoryRef{ProjectKey: repo.ProjectKey, Slug: repo.Slug},
				PullRequestID: prID,
			}

			updated, err := commentservice.NewService(client).SetState(cmd.Context(), target, commentID, state, nil)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), SingleComment{Repository: result.Repository{ProjectKey: repo.ProjectKey, Slug: repo.Slug}, PullRequestID: prID, Comment: commentFrom(updated)})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s comment %s\n", style.Success.Render(doneWord), style.Resource.Render(commentID))
			return nil
		},
	}

	return command
}
