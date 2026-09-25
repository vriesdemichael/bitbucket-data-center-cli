package repocmd

import (
	"fmt"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/enumflag"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/safederef"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/dryrunpreview"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/paging"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/preflight"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/reposel"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/style"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	commentservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/comment"
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

			// The service already stops at the cap (ADR-074); this keeps --limit
			// honest if one ever does not. A no-op under --all.
			comments = paging.Truncate(listPaging, comments)

			// Flattened, so replies are reachable. Bitbucket nests them under
			// their root, and this command has no thread view to reach them
			// through -- listing only the roots discarded every reply body on a
			// commit comment, leaving a count as the only trace.
			listed := result.FlattenComments(comments)

			// --limit counts the top-level comments Truncate cut, not the flattened
			// list, which carries their replies too: three comments with 25 replies
			// between them reached no limit.

			if deps.JSONEnabled() {
				return deps.WriteJSONList(cmd.OutOrStdout(), Comments{Context: commentContextFrom(target.Context()), Comments: listed}, paging.LimitReached(listPaging, len(comments)))
			}

			if len(listed) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No comments found"))
				return nil
			}

			for _, comment := range listed {
				fmt.Fprintln(cmd.OutOrStdout(), result.FormatComment(comment))
			}
			paging.Hint(cmd.ErrOrStderr(), listPaging, len(comments))

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
				if err := preflight.RepoPermission(cmd.Context(), deps.PermissionChecker, client, target.Repository.ProjectKey, target.Repository.Slug, openapi.RepoRead); err != nil {
					return err
				}

				preview := dryrunpreview.New(dryrunpreview.Item{
					Intent:          "repo.comment.create",
					Target:          createTargetPreview(target, createText, createParentID),
					Action:          "create",
					PredictedAction: "create",
					Reason:          "comment will be created",
				})
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
	enumflag.Register(createCmd.Flags(), &createLineType, "line-type", "", openapi.DiffLineTypes, "Which side of the diff --line refers to")
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
				if err := preflight.RepoPermission(cmd.Context(), deps.PermissionChecker, client, target.Repository.ProjectKey, target.Repository.Slug, openapi.RepoRead); err != nil {
					return err
				}

				current, err := service.Get(cmd.Context(), target, updateCommentID)
				if err != nil {
					return err
				}
				currentUser := strings.TrimSpace(cfg.BitbucketUsername)

				predicted := "update"
				reason := "comment will be updated"
				blocking := []string{}
				if strings.EqualFold(strings.TrimSpace(safederef.String(current.Text)), strings.TrimSpace(updateText)) {
					predicted = "no-op"
					reason = "comment text already matches requested value"
				} else if currentUser != "" && !commentOwnedByUser(current, currentUser) {
					predicted = "blocked"
					reason = "comment is owned by another user"
					blocking = []string{"comment owned by another user"}
				}

				preview := dryrunpreview.New(dryrunpreview.Item{
					Intent:          "repo.comment.update",
					Target:          map[string]any{"context": target.Context(), "id": updateCommentID, "text": updateText},
					Action:          "update",
					PredictedAction: predicted,
					Tier:            dryrunpreview.TierPreconditionsChecked,
					Reason:          reason,
					BlockingReasons: blocking,
					// Only its author may edit a comment. Bitbucket refuses
					// anybody else, a repository admin too, with a 401
					// AuthorisationException, which is authorization.
					Fails: apperrors.KindAuthorization,
				})

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
				if err := preflight.RepoPermission(cmd.Context(), deps.PermissionChecker, client, target.Repository.ProjectKey, target.Repository.Slug, openapi.RepoRead); err != nil {
					return err
				}

				current, err := service.Get(cmd.Context(), target, deleteCommentID)
				currentUser := strings.TrimSpace(cfg.BitbucketUsername)
				predicted := "delete"
				reason := "comment will be deleted"
				blocking := []string{}
				fails := apperrors.KindConflict
				if err != nil {
					if apperrors.ExitCode(err) == 4 {
						// Deleting a comment that is not there is a 404.
						predicted = "blocked"
						reason = "comment was not found"
						fails = apperrors.KindNotFound
					} else {
						return err
					}
				} else {
					// Bitbucket lets the author delete a comment, and anyone who
					// administers the repository: a repository admin deleting
					// somebody else's comment succeeds, so being somebody else
					// is only a refusal without admin. It asks that first, and
					// only then refuses a comment that still has replies.
					if currentUser != "" && !commentOwnedByUser(current, currentUser) {
						adminErr := preflight.RepoPermission(cmd.Context(), deps.PermissionChecker, client, target.Repository.ProjectKey, target.Repository.Slug, openapi.RepoAdmin)
						switch {
						case adminErr == nil:
						case apperrors.IsKind(adminErr, apperrors.KindAuthorization):
							predicted = "blocked"
							reason = "comment is owned by another user, and only its author or a repository admin may delete it"
							blocking = []string{reason}
							fails = apperrors.KindAuthorization
						default:
							return adminErr
						}
					}
					if predicted == "delete" && current.Comments != nil && len(*current.Comments) > 0 {
						predicted = "blocked"
						reason = "comment has replies, which have to be deleted first"
						blocking = []string{reason}
					}
				}

				preview := dryrunpreview.New(dryrunpreview.Item{
					Intent:          "repo.comment.delete",
					Target:          map[string]any{"context": target.Context(), "id": deleteCommentID},
					Action:          "delete",
					PredictedAction: predicted,
					Tier:            dryrunpreview.TierPreconditionsChecked,
					Reason:          reason,
					BlockingReasons: blocking,
					// authorization for somebody else's comment, which Bitbucket
					// refuses with a 401 AuthorisationException; conflict, its
					// 409 CommentDeletionException, for one with replies.
					Fails: fails,
				})

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
