package prcmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/dryrunpreview"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/preflight"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/prsel"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/style"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	commentservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/comment"
)

// newPullRequestCommentDeleteCommand deletes a pull request comment, or a task,
// which Bitbucket keeps as a comment (#785). It reads the comment's version
// unless --version names the one expected, and its dry run predicts what bb
// repo comment delete predicts, through the same function.
func newPullRequestCommentDeleteCommand(deps Dependencies, repository *string) *cobra.Command {
	var version int32

	command := &cobra.Command{
		Use:   "delete <pr-id> <comment-id>",
		Short: "Delete a pull request comment or task",
		Example: `  # A comment, or a task, by the id bb pr comment list prints
  bb pr comment delete 42 1389396 --repo PROJ/repo

  # Refused if the comment changed since you read version 3
  bb pr comment delete 42 1389396 --repo PROJ/repo --version 3`,
		Long: "Delete a pull request comment, or a task, which Bitbucket keeps as a comment.\n\n" +
			"Bitbucket lets the comment's author delete it, and anyone who administers the repository. " +
			"It refuses a comment that has replies: delete the replies first. The comment's current " +
			"version is read unless --version names the one you expect, and then a comment that changed " +
			"since is refused rather than deleted.",
		Args: cobra.ExactArgs(2),
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
			commentID := args[1]
			target := commentservice.Target{
				Repository:    commentservice.RepositoryRef{ProjectKey: repo.ProjectKey, Slug: repo.Slug},
				PullRequestID: prTarget.PullRequestID,
			}
			service := commentservice.NewService(client)

			if deps.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), deps.PermissionChecker, client, repo.ProjectKey, repo.Slug, openapi.RepoRead); err != nil {
					return err
				}

				prediction, err := preflight.PredictCommentDeletion(cmd.Context(), deps.PermissionChecker, client, service, target, commentID, cfg.BitbucketUsername)
				if err != nil {
					return err
				}

				preview := dryrunpreview.New(dryrunpreview.Item{
					Intent:          "pr.comment.delete",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "prId": prTarget.PullRequestID, "commentId": commentID},
					Action:          "delete",
					PredictedAction: prediction.Predicted,
					Tier:            dryrunpreview.TierPreconditionsChecked,
					Reason:          prediction.Reason,
					BlockingReasons: prediction.Blocking,
					Fails:           prediction.Fails,
				})

				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			var expected *int32
			if cmd.Flags().Changed("version") {
				expected = &version
			}

			deletedAt, err := service.Delete(cmd.Context(), target, commentID, expected)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				deletion := CommentDeletion{
					Status:        result.OK(),
					Repository:    repositoryOf(repo),
					PullRequestID: prTarget.PullRequestID,
					CommentID:     commentID,
				}
				if deletedAt != nil {
					value := int(*deletedAt)
					deletion.Version = &value
				}

				return deps.WriteJSON(cmd.OutOrStdout(), deletion)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s on pull request %s\n",
				style.Deleted.Render("Deleted comment"), style.Resource.Render(commentID), style.Resource.Render(prTarget.PullRequestID))
			return nil
		},
	}
	command.Flags().Int32Var(&version, "version", 0, "Expected comment version; a comment that changed since is refused")

	return command
}
