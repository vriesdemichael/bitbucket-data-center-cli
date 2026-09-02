package repocmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/dryrunpreview"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/paging"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/reposel"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/style"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
	commentservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/comment"
)

func resolveCommentTarget(selector string, commitID string, pullRequestID string, cfg config.AppConfig) (commentservice.Target, error) {
	projectKey, slug, err := reposel.Resolve(selector, cfg)
	if err != nil {
		return commentservice.Target{}, err
	}

	trimmedCommitID := strings.TrimSpace(commitID)
	trimmedPullRequestID := strings.TrimSpace(pullRequestID)
	hasCommit := trimmedCommitID != ""
	hasPullRequest := trimmedPullRequestID != ""

	if hasCommit == hasPullRequest {
		return commentservice.Target{}, apperrors.New(apperrors.KindValidation, "exactly one of --commit or --pr is required", nil)
	}

	return commentservice.Target{
		Repository:    commentservice.RepositoryRef{ProjectKey: projectKey, Slug: slug},
		CommitID:      trimmedCommitID,
		PullRequestID: trimmedPullRequestID,
	}, nil
}

func commentOwnedByUser(comment openapigenerated.RestComment, username string) bool {
	trimmed := strings.TrimSpace(username)
	if trimmed == "" || comment.Author == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(comment.Author.Name), trimmed) {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(comment.Author.Slug), trimmed) {
		return true
	}
	return false
}

func commentIDString(comment openapigenerated.RestComment) string {
	if comment.Id == nil {
		return "?"
	}
	return strconv.FormatInt(*comment.Id, 10)
}

func newRepoCommentCommand(deps Dependencies) *cobra.Command {
	var repositorySelector string
	var commitID string
	var pullRequestID string

	commentCmd := &cobra.Command{
		Use:   "comment",
		Short: "Comment commands for commits and pull requests",
	}

	commentCmd.PersistentFlags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug (defaults to BITBUCKET_PROJECT_KEY + BITBUCKET_REPO_SLUG)")
	commentCmd.PersistentFlags().StringVar(&commitID, "commit", "", "Commit ID context")
	commentCmd.PersistentFlags().StringVar(&pullRequestID, "pr", "", "Pull request ID context")

	var listPaging paging.Options
	var listPath string
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List comments",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			target, err := resolveCommentTarget(repositorySelector, commitID, pullRequestID, cfg)
			if err != nil {
				return err
			}

			service := commentservice.NewService(client)
			comments, err := service.List(cmd.Context(), target, listPath, listPaging.ServiceLimit())
			if err != nil {
				return err
			}

			// Flattened, so replies are reachable. Bitbucket nests them under
			// their root, and this command has no thread view to reach them
			// through -- listing only the roots discarded every reply body on a
			// commit comment, leaving a count as the only trace.
			listed := result.FlattenComments(comments)

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), Comments{Context: commentContextFrom(target.Context()), Comments: listed})
			}

			if len(listed) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No comments found"))
				return nil
			}

			for _, comment := range listed {
				fmt.Fprintln(cmd.OutOrStdout(), result.FormatComment(comment))
			}

			return nil
		},
	}
	listCmd.Flags().StringVar(&listPath, "path", "", "File path to scope the listing to. Bitbucket requires it: a comment anchored to no file cannot be listed.")
	listPaging.Register(listCmd, 25)
	_ = listCmd.MarkFlagRequired("path")
	commentCmd.AddCommand(listCmd)

	var createText string
	var createParentID int64
	var createPath string
	var createLine int
	var createLineType string
	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a comment",
		Long: "Create a comment on a commit or a pull request.\n\n" +
			"Pass --parent to reply to an existing comment rather than start a new thread. " +
			"The id to pass is the one `bb repo comment list` reports; a reply carries reply and " +
			"parentId in that listing, so a thread can be walked back to its root.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			target, err := resolveCommentTarget(repositorySelector, commitID, pullRequestID, cfg)
			if err != nil {
				return err
			}
			target.ParentID = createParentID
			target.Path = createPath
			target.Line = createLine
			target.LineType = createLineType

			service := commentservice.NewService(client)
			if deps.DryRunEnabled() {
				if deps.PermissionChecker != nil {
					checker := deps.PermissionChecker(client)
					if checker != nil {
						if err := checker.CheckRepoPermission(cmd.Context(), target.Repository.ProjectKey, target.Repository.Slug, openapi.RepoRead); err != nil {
							return err
						}
					}
				}

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityPartial,
					Items: []dryrunpreview.Item{{
						Intent:          "repo.comment.create",
						Target:          createTargetPreview(target, createText, createParentID),
						Action:          "create",
						PredictedAction: "create",
						Supported:       true,
						Reason:          "comment will be created",
						Confidence:      dryrunpreview.CapabilityPartial,
						RequiredState:   []string{"comment target context"},
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1, CreateCount: 1},
				}
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			created, err := service.Create(cmd.Context(), target, createText)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), SingleComment{Context: commentContextFrom(target.Context()), Comment: result.CommentFrom(created)})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Success.Render("Created comment"), style.Secondary.Render(commentIDString(created)))
			return nil
		},
	}
	createCmd.Flags().StringVar(&createText, "text", "", "Comment text")
	createCmd.Flags().Int64Var(&createParentID, "parent", 0, "Reply to this comment id instead of starting a new thread")
	createCmd.Flags().StringVar(&createPath, "path", "", "Anchor the comment to this file, which is what makes it listable")
	createCmd.Flags().IntVar(&createLine, "line", 0, "Line within --path to anchor the comment to")
	createCmd.Flags().StringVar(&createLineType, "line-type", "", "Which side of the diff --line refers to: ADDED, REMOVED or CONTEXT")
	_ = createCmd.MarkFlagRequired("text")
	commentCmd.AddCommand(createCmd)

	var updateCommentID string
	var updateText string
	var updateVersion int32
	updateCmd := &cobra.Command{
		Use:   "update",
		Short: "Update a comment",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			target, err := resolveCommentTarget(repositorySelector, commitID, pullRequestID, cfg)
			if err != nil {
				return err
			}

			service := commentservice.NewService(client)
			if deps.DryRunEnabled() {
				if deps.PermissionChecker != nil {
					checker := deps.PermissionChecker(client)
					if checker != nil {
						if err := checker.CheckRepoPermission(cmd.Context(), target.Repository.ProjectKey, target.Repository.Slug, openapi.RepoRead); err != nil {
							return err
						}
					}
				}

				current, err := service.Get(cmd.Context(), target, updateCommentID)
				if err != nil {
					return err
				}
				currentUser := strings.TrimSpace(cfg.BitbucketUsername)

				predicted := "update"
				reason := "comment will be updated"
				blocking := []string{}
				if strings.EqualFold(strings.TrimSpace(safeString(current.Text)), strings.TrimSpace(updateText)) {
					predicted = "no-op"
					reason = "comment text already matches requested value"
				} else if currentUser != "" && !commentOwnedByUser(current, currentUser) {
					predicted = "blocked"
					reason = "comment is owned by another user"
					blocking = []string{"comment owned by another user"}
				}

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityPartial,
					Items: []dryrunpreview.Item{{
						Intent:          "repo.comment.update",
						Target:          map[string]any{"context": target.Context(), "id": updateCommentID, "text": updateText},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      dryrunpreview.CapabilityPartial,
						RequiredState:   []string{"comment get"},
						BlockingReasons: blocking,
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1},
				}
				if predicted == "update" {
					preview.Summary.UpdateCount = 1
				} else if predicted == "no-op" {
					preview.Summary.NoopCount = 1
				} else {
					preview.Summary.UnknownCount = 1
				}

				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			var version *int32
			if cmd.Flags().Changed("version") {
				version = &updateVersion
			}

			updated, err := service.Update(cmd.Context(), target, updateCommentID, updateText, version)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), SingleComment{Context: commentContextFrom(target.Context()), Comment: result.CommentFrom(updated)})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Updated.Render("Updated comment"), style.Secondary.Render(commentIDString(updated)))
			return nil
		},
	}
	updateCmd.Flags().StringVar(&updateCommentID, "id", "", "Comment ID")
	updateCmd.Flags().StringVar(&updateText, "text", "", "Comment text")
	updateCmd.Flags().Int32Var(&updateVersion, "version", 0, "Expected comment version")
	_ = updateCmd.MarkFlagRequired("id")
	_ = updateCmd.MarkFlagRequired("text")
	commentCmd.AddCommand(updateCmd)

	var deleteCommentID string
	var deleteVersion int32
	deleteCmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete a comment",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			target, err := resolveCommentTarget(repositorySelector, commitID, pullRequestID, cfg)
			if err != nil {
				return err
			}

			service := commentservice.NewService(client)
			if deps.DryRunEnabled() {
				if deps.PermissionChecker != nil {
					checker := deps.PermissionChecker(client)
					if checker != nil {
						if err := checker.CheckRepoPermission(cmd.Context(), target.Repository.ProjectKey, target.Repository.Slug, openapi.RepoRead); err != nil {
							return err
						}
					}
				}

				current, err := service.Get(cmd.Context(), target, deleteCommentID)
				currentUser := strings.TrimSpace(cfg.BitbucketUsername)
				predicted := "delete"
				reason := "comment will be deleted"
				blocking := []string{}
				if err != nil {
					if apperrors.ExitCode(err) == 4 {
						predicted = "no-op"
						reason = "comment was not found"
					} else {
						return err
					}
				} else if currentUser != "" {
					if !commentOwnedByUser(current, currentUser) {
						predicted = "blocked"
						reason = "comment is owned by another user"
						blocking = []string{"comment owned by another user"}
					}
				}

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityPartial,
					Items: []dryrunpreview.Item{{
						Intent:          "repo.comment.delete",
						Target:          map[string]any{"context": target.Context(), "id": deleteCommentID},
						Action:          "delete",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      dryrunpreview.CapabilityPartial,
						RequiredState:   []string{"comment get"},
						BlockingReasons: blocking,
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1},
				}
				if predicted == "delete" {
					preview.Summary.DeleteCount = 1
				} else if predicted == "no-op" {
					preview.Summary.NoopCount = 1
				} else {
					preview.Summary.UnknownCount = 1
				}

				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			var version *int32
			if cmd.Flags().Changed("version") {
				version = &deleteVersion
			}

			resolvedVersion, err := service.Delete(cmd.Context(), target, deleteCommentID, version)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), CommentDeletion{
					Status:  result.OK(),
					Context: commentContextFrom(target.Context()),
					ID:      deleteCommentID,
					Version: intPointer(resolvedVersion),
				})
			}

			if resolvedVersion == nil {
				fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Deleted.Render("Deleted comment"), style.Secondary.Render(strings.TrimSpace(deleteCommentID)))
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s %s\n", style.Deleted.Render("Deleted comment"), style.Secondary.Render(strings.TrimSpace(deleteCommentID)), style.Secondary.Render(fmt.Sprintf("(version=%d)", *resolvedVersion)))
			return nil
		},
	}
	deleteCmd.Flags().StringVar(&deleteCommentID, "id", "", "Comment ID")
	deleteCmd.Flags().Int32Var(&deleteVersion, "version", 0, "Expected comment version")
	_ = deleteCmd.MarkFlagRequired("id")
	commentCmd.AddCommand(deleteCmd)

	return commentCmd
}

// createTargetPreview describes what a create would do, for --dry-run.
//
// The parent is named only when there is one, so the preview reads as "a new
// comment" or "a reply to 42" rather than always carrying a zero that means
// neither.
func createTargetPreview(target commentservice.Target, text string, parentID int64) map[string]any {
	preview := map[string]any{"context": target.Context(), "text": text}
	if parentID != 0 {
		preview["parentId"] = parentID
	}

	return preview
}
