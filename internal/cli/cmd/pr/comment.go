package prcmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/dryrunpreview"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/enumflag"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/paging"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/preflight"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/prsel"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	commentservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/comment"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/commentanchor"
	pullrequestactivityservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequestactivity"
)

func newPullRequestCommentCommand(deps Dependencies, repository *string) *cobra.Command {
	commentCmd := &cobra.Command{Use: "comment", Short: "Add, list, resolve and react to pull request comments"}

	var commentPath string
	var commentPaging paging.Options
	var commentBlocker bool
	var commentState string
	var commentUnresolved bool
	var commentTasksOnly bool
	var commentWithReplies bool
	var commentFull bool
	commentListCmd := &cobra.Command{
		Use:   "list <pr-id>",
		Short: "List comment threads for a pull request, unresolved first",
		Example: `  # Every comment thread and task, unresolved first
  bb pr comment list 42 --repo PROJ/repo

  # Only what is still waiting on someone
  bb pr comment list 42 --repo PROJ/repo --unresolved

  # The open tasks
  bb pr comment list 42 --repo PROJ/repo --tasks-only --state open

  # The comments on one file, with every reply
  bb pr comment list 42 --repo PROJ/repo --path src/main.go --with-replies`,
		Long: "List pull request comment threads. Bitbucket models a task as a blocker comment, so this " +
			"returns reviewer comments and tasks in one view, each with its resolution state, anchor and reply count.\n\n" +
			"Without --path this uses the pull request activity timeline to return the aggregate comment view. " +
			"With --path it uses the path-scoped comments endpoint. With --blocker it lists blocker comments.\n\n" +
			"Use --unresolved to show only threads still waiting on someone. Use --full to add every comment " +
			"ungrouped, alongside the thread view rather than in place of it.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			trimmedCommentPath := strings.TrimSpace(commentPath)

			if commentUnresolved && cmd.Flags().Changed("state") && !strings.EqualFold(strings.TrimSpace(commentState), "open") {
				return apperrors.New(apperrors.KindValidation, "--unresolved cannot be combined with a --state other than open", nil)
			}
			if commentUnresolved {
				commentState = "open"
			}
			normalizedState, err := pullrequestactivityservice.NormalizeThreadState(commentState)
			if err != nil {
				return apperrors.New(apperrors.KindValidation, err.Error(), nil)
			}

			target, err := prsel.Resolve(cmd.Context(), args[0], *repository, cfg, nil)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			threadOptions := pullrequestactivityservice.ThreadOptions{
				State:         normalizedState,
				TasksOnly:     commentTasksOnly,
				WithReplies:   commentWithReplies,
				BaseURL:       cfg.BitbucketURL,
				ProjectKey:    repo.ProjectKey,
				Slug:          repo.Slug,
				PullRequestID: target.PullRequestID,
			}

			source := "comments"
			var comments []openapigenerated.RestComment
			var threads []pullrequestactivityservice.Thread
			var summary pullrequestactivityservice.Summary

			// Every branch reads the whole set and the cap lands on the threads
			// below, because the thing being listed here is threads and the
			// grouping happens after the fetch.
			//
			// It had landed on the fetch, where it did nothing: all three
			// services took it as a page size and read to the last page anyway,
			// so --limit changed neither the requests' outcome nor the output.
			// Moving it to the fetch properly would have been worse than
			// leaving it broken -- a comment and its replies arrive separately,
			// so a truncated window ends mid-thread and the summary reports
			// fewer open threads than the pull request has.
			if commentBlocker {
				source = "blocker_comments"
				service := commentservice.NewService(client)
				comments, err = service.List(cmd.Context(), commentservice.Target{
					Repository:    commentservice.RepositoryRef{ProjectKey: repo.ProjectKey, Slug: repo.Slug},
					PullRequestID: target.PullRequestID,
					Blocker:       true,
				}, "", commentservice.AllResults)
				if err != nil {
					return err
				}
				threads, summary = pullrequestactivityservice.ThreadsFromComments(comments, threadOptions)
			} else if trimmedCommentPath == "" {
				source = "activities"
				activityService := pullrequestactivityservice.NewService(client)
				activities, listErr := activityService.List(cmd.Context(), pullrequestactivityservice.RepositoryRef{ProjectKey: repo.ProjectKey, Slug: repo.Slug}, target.PullRequestID,
					pullrequestactivityservice.ListOptions{MaxResults: pullrequestactivityservice.AllResults})
				if listErr != nil {
					return listErr
				}
				comments = pullrequestactivityservice.ExtractComments(activities)
				threads, summary = pullrequestactivityservice.ExtractThreads(activities, threadOptions)
			} else {
				service := commentservice.NewService(client)
				comments, err = service.List(cmd.Context(), commentservice.Target{
					Repository:    commentservice.RepositoryRef{ProjectKey: repo.ProjectKey, Slug: repo.Slug},
					PullRequestID: target.PullRequestID,
				}, trimmedCommentPath, commentservice.AllResults)
				if err != nil {
					return err
				}
				threads, summary = pullrequestactivityservice.ThreadsFromComments(comments, threadOptions)
			}

			// The summary above counts the whole pull request and keeps doing
			// so: it is what says how much the listing is not showing.
			threads = paging.Truncate(commentPaging, threads)

			if commentFull {
				// --full asks for the same comments ungrouped, not for a
				// different set of them. The payload carried state next to a
				// list that ignored it, so a caller passing --state resolved
				// and reading comments got every comment on the pull request
				// with nothing saying the filter had been dropped.
				comments = commentsInThreads(comments, threads)

				// Flattened for the same reason bb repo comment list is: a reply
				// nests under its root, and an ungrouped list that carried only
				// roots was not the ungrouped list -- it was the roots with the
				// replies counted and thrown away.
				ungrouped := result.FlattenComments(comments)

				if deps.JSONEnabled() {
					// Present even when empty: its absence is what says --full
					// was not passed, so an empty file must still carry the key.
					return deps.WriteJSONList(cmd.OutOrStdout(), CommentThreads{
						Repository:    repositoryOf(repo),
						PullRequestID: target.PullRequestID,
						Source:        source,
						Path:          trimmedCommentPath,
						State:         normalizedState,
						Summary:       threadSummaryFrom(summary),
						Threads:       threadsFrom(threads),
						Comments:      &ungrouped,
					}, paging.LimitReached(commentPaging, len(threads)))
				}

				if len(ungrouped) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "No comments found")
					return nil
				}
				for _, comment := range ungrouped {
					fmt.Fprintln(cmd.OutOrStdout(), result.FormatComment(comment))
				}
				paging.Hint(cmd.ErrOrStderr(), commentPaging, len(threads))

				return nil
			}

			if deps.JSONEnabled() {
				return deps.WriteJSONList(cmd.OutOrStdout(), CommentThreads{
					Repository:    repositoryOf(repo),
					PullRequestID: target.PullRequestID,
					Source:        source,
					Path:          trimmedCommentPath,
					State:         normalizedState,
					Summary:       threadSummaryFrom(summary),
					Threads:       threadsFrom(threads),
				}, paging.LimitReached(commentPaging, len(threads)))
			}

			if summary.TotalThreads == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No comments found")
				return nil
			}

			fmt.Fprintln(cmd.OutOrStdout(), formatThreadCounts(summary))

			if len(threads) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No comments match the current filter")
				return nil
			}

			fmt.Fprintln(cmd.OutOrStdout())
			for _, thread := range threads {
				fmt.Fprintln(cmd.OutOrStdout(), formatThread(thread))
			}
			paging.Hint(cmd.ErrOrStderr(), commentPaging, len(threads))

			return nil
		},
	}
	commentListCmd.Flags().StringVar(&commentPath, "path", "", "Optional file path for path-scoped pull request comment listing")
	commentPaging.Register(commentListCmd, 25)
	commentListCmd.Flags().BoolVar(&commentBlocker, "blocker", false, "List pull request blocker comments")
	enumflag.Register(commentListCmd.Flags(), &commentState, "state", "all", threadStates, "Filter threads by resolution state")
	commentListCmd.Flags().BoolVar(&commentUnresolved, "unresolved", false, "Show only unresolved threads (shorthand for --state open)")
	commentListCmd.Flags().BoolVar(&commentTasksOnly, "tasks-only", false, "Show only threads Bitbucket tracks as tasks (blocker comments)")
	commentListCmd.Flags().BoolVar(&commentWithReplies, "with-replies", false, "Include the full text of every reply instead of only the most recent one")
	commentListCmd.Flags().BoolVar(&commentFull, "full", false, "Add every comment ungrouped, alongside the thread view")
	commentCmd.AddCommand(commentListCmd)

	commentGetCmd := &cobra.Command{
		Use:   "get <pr-id> <comment-id>",
		Short: "Get a pull request comment",
		Example: `  # One comment in full, by the id bb pr comment list prints
  bb pr comment get 42 1389396 --repo PROJ/repo`,
		Long: "Get a single pull request comment by id. This is the authoritative single-comment view and is better suited than list output when you need the full rendered comment payload.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			target, err := prsel.Resolve(cmd.Context(), args[0], *repository, cfg, nil)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			service := commentservice.NewService(client)
			comment, err := service.Get(cmd.Context(), commentservice.Target{
				Repository:    commentservice.RepositoryRef{ProjectKey: repo.ProjectKey, Slug: repo.Slug},
				PullRequestID: target.PullRequestID,
			}, args[1])
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), SingleComment{Repository: repositoryOf(repo), PullRequestID: target.PullRequestID, Comment: result.CommentFrom(comment)})
			}

			fmt.Fprintln(cmd.OutOrStdout(), formatCommentDetail(comment))
			return nil
		},
	}
	commentCmd.AddCommand(commentGetCmd)

	var commentAddText string
	var commentAddBlocker bool
	var commentAddPending bool
	var commentAddPath string
	var commentAddLine int
	var commentAddLineType string
	var commentAddParentID int64
	commentAddCmd := &cobra.Command{
		Use:   "add <pr-id>",
		Short: "Add a comment to a pull request",
		Long: `Add a comment to a pull request.

Pass --path and --line to anchor the comment to a file and line, or --parent-id
to reply to an existing comment. Bitbucket only accepts an anchor on a line that
appears in the pull request diff, so the line has to be inside a changed hunk and
--line-type has to match the side it is on.`,
		Example: `  # A pull-request-level comment
  bb pr comment add 49 --repo PROJ/repo --text "Looks good overall."

  # Anchored to a line in the diff
  bb pr comment add 49 --repo PROJ/repo --path app/core/runner.py --line 157 \
    --text "This raises after the execution is recorded."

  # A line that only exists in the original file
  bb pr comment add 49 --repo PROJ/repo --path app/core/runner.py --line 88 \
    --line-type REMOVED --text "Why was this dropped?"

  # Reply to an existing comment
  bb pr comment add 49 --repo PROJ/repo --parent-id 1389396 --text "Agreed, fixed."

  # A task: Bitbucket models one as a blocker comment
  bb pr comment add 49 --repo PROJ/repo --blocker --text "Add a test for the empty case."

  # A draft, which bb pr review complete publishes with the rest of your review
  bb pr comment add 49 --repo PROJ/repo --pending --text "Naming: prefer retryCount."`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Validated up front, in terms of the flags the caller typed, so a
			// bad anchor fails before any network call. The comment service
			// applies the same rules for callers that do not come through here.
			if err := commentanchor.Validate(commentanchor.Options{
				Path:     commentAddPath,
				Line:     commentAddLine,
				LineType: commentAddLineType,
				ParentID: commentAddParentID,
				Blocker:  commentAddBlocker,
			}, commentanchor.CLINames); err != nil {
				return err
			}

			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			target, err := prsel.Resolve(cmd.Context(), args[0], *repository, cfg, nil)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			cmtTarget := commentservice.Target{
				Repository:    commentservice.RepositoryRef{ProjectKey: repo.ProjectKey, Slug: repo.Slug},
				PullRequestID: target.PullRequestID,
				Blocker:       commentAddBlocker,
				Pending:       commentAddPending,
				Path:          commentAddPath,
				Line:          commentAddLine,
				LineType:      commentAddLineType,
				ParentID:      commentAddParentID,
			}

			if deps.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), deps.PermissionChecker, client, repo.ProjectKey, repo.Slug, openapi.RepoRead); err != nil {
					return err
				}

				targetMap := map[string]any{
					"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug),
					"id":         target.PullRequestID,
					"text":       commentAddText,
					"blocker":    commentAddBlocker,
					"pending":    commentAddPending,
				}
				if commentAddPath != "" {
					targetMap["path"] = commentAddPath
				}
				if commentAddLine > 0 {
					targetMap["line"] = commentAddLine
				}
				if commentAddLineType != "" {
					targetMap["line_type"] = commentAddLineType
				}
				if commentAddParentID > 0 {
					targetMap["parent_id"] = commentAddParentID
				}

				preview := dryrunpreview.New(dryrunpreview.Item{
					Intent:          "pr.comment.add",
					Target:          targetMap,
					Action:          "create",
					PredictedAction: "create",
					Reason:          "pull request comment will be created",
				})
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			service := commentservice.NewService(client)
			created, err := service.Create(cmd.Context(), cmtTarget, commentAddText)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), AddedComment{
					Repository:    repositoryOf(repo),
					PullRequestID: target.PullRequestID,
					Comment:       result.CommentFrom(created),
					Blocker:       commentAddBlocker,
					Pending:       commentAddPending,
					Path:          commentAddPath,
					Line:          commentAddLine,
					LineType:      commentAddLineType,
					ParentID:      commentAddParentID,
				})
			}

			commentID := ""
			if created.Id != nil {
				commentID = strconv.Itoa(int(*created.Id))
			}
			blockerStr := ""
			if commentAddBlocker {
				blockerStr = " blocker"
			}
			pendingStr := ""
			if commentAddPending {
				pendingStr = " pending"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created%s%s comment %s\n", blockerStr, pendingStr, commentID)
			return nil
		},
	}
	commentAddCmd.Flags().StringVar(&commentAddText, "text", "", "Comment text")
	commentAddCmd.Flags().BoolVar(&commentAddBlocker, "blocker", false, "Mark the comment as a blocker")
	commentAddCmd.Flags().BoolVar(&commentAddPending, "pending", false, "Mark the comment as pending (draft)")
	commentAddCmd.Flags().StringVar(&commentAddPath, "path", "", "File path for an inline comment")
	commentAddCmd.Flags().IntVar(&commentAddLine, "line", 0, "Line number for an inline comment")
	enumflag.Register(commentAddCmd.Flags(), &commentAddLineType, "line-type", "", openapi.DiffLineTypes, "Line type for an inline comment, ADDED when not given")
	commentAddCmd.Flags().Int64Var(&commentAddParentID, "parent-id", 0, "Parent comment ID to reply to")
	_ = commentAddCmd.MarkFlagRequired("text")
	commentCmd.AddCommand(commentAddCmd)

	var commentReactRemove bool
	commentReactCmd := &cobra.Command{
		Use:   "react <pr-id> <comment-id> <emoji>",
		Short: "Add or remove a reaction on a pull request comment",
		Example: `  # Add a thumbs-up to a comment
  bb pr comment react 42 1389396 thumbsup --repo PROJ/repo

  # Take it back
  bb pr comment react 42 1389396 thumbsup --repo PROJ/repo --remove`,
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			target, err := prsel.Resolve(cmd.Context(), args[0], *repository, cfg, nil)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			prID := target.PullRequestID
			commentID := args[1]
			emoticon := normalizeEmoticon(args[2])

			if deps.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), deps.PermissionChecker, client, repo.ProjectKey, repo.Slug, openapi.RepoRead); err != nil {
					return err
				}

				action := "update"
				predicted := "update"
				intent := "pr.comment.react"
				reason := "reaction will be added"
				if commentReactRemove {
					action = "delete"
					predicted = "delete"
					reason = "reaction will be removed"
				}

				preview := dryrunpreview.New(dryrunpreview.Item{
					Intent:          intent,
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "prId": prID, "commentId": commentID, "emoticon": emoticon},
					Action:          action,
					PredictedAction: predicted,
					// Only the permission is checked: whether the comment is there,
					// and whether the reaction already is, is not (ADR-078).
					Tier:   dryrunpreview.TierPredicted,
					Reason: reason,
				})
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			service := commentservice.NewService(client)
			if commentReactRemove {
				err = service.UnReact(cmd.Context(), commentservice.RepositoryRef{ProjectKey: repo.ProjectKey, Slug: repo.Slug}, prID, commentID, emoticon)
				if err != nil {
					return err
				}

				if deps.JSONEnabled() {
					return deps.WriteJSON(cmd.OutOrStdout(), Reaction{Status: result.OK(), Action: "removed", Repository: repositoryOf(repo), PullRequestID: prID, CommentID: commentID, Emoticon: emoticon})
				}

				fmt.Fprintf(cmd.OutOrStdout(), "Removed reaction :%s: from comment %s\n", emoticon, commentID)
				return nil
			}

			_, err = service.React(cmd.Context(), commentservice.RepositoryRef{ProjectKey: repo.ProjectKey, Slug: repo.Slug}, prID, commentID, emoticon)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), Reaction{Status: result.OK(), Action: "added", Repository: repositoryOf(repo), PullRequestID: prID, CommentID: commentID, Emoticon: emoticon})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Added reaction :%s: to comment %s\n", emoticon, commentID)
			return nil
		},
	}
	commentReactCmd.Flags().BoolVar(&commentReactRemove, "remove", false, "Remove the reaction instead of adding it")
	commentCmd.AddCommand(commentReactCmd)

	for _, stateCommand := range newPullRequestCommentStateCommands(deps, repository) {
		commentCmd.AddCommand(stateCommand)
	}
	commentCmd.AddCommand(newPullRequestCommentDeleteCommand(deps, repository))

	var commentSuggestionMsg string
	var commentSuggestionIdx int32
	var commentSuggestionCommentVer int32
	var commentSuggestionPrVer int32
	commentApplySuggestionCmd := &cobra.Command{
		Use:   "apply-suggestion <pr-id> <comment-id>",
		Short: "Apply a suggested change from a comment",
		Example: `  # Commit the change a reviewer suggested in a comment
  bb pr comment apply-suggestion 42 1389396 --repo PROJ/repo

  # The second suggestion in the comment, with a commit message of your own
  bb pr comment apply-suggestion 42 1389396 --repo PROJ/repo --index 1 \
    --commit-message "Use the shared retry helper"`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			target, err := prsel.Resolve(cmd.Context(), args[0], *repository, cfg, nil)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			prID := target.PullRequestID
			commentID := args[1]

			message := fmt.Sprintf("Apply suggestion from comment %s", commentID)
			if strings.TrimSpace(commentSuggestionMsg) != "" {
				message = commentSuggestionMsg
			}
			req := openapigenerated.RestApplySuggestionRequest{Message: &message}
			if cmd.Flags().Changed("index") {
				req.SuggestionIndex = commentSuggestionIdx
			}
			if cmd.Flags().Changed("comment-version") {
				req.CommentVersion = commentSuggestionCommentVer
			}
			if cmd.Flags().Changed("pr-version") {
				req.PullRequestVersion = commentSuggestionPrVer
			}

			if deps.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), deps.PermissionChecker, client, repo.ProjectKey, repo.Slug, openapi.RepoWrite); err != nil {
					return err
				}

				preview := dryrunpreview.New(dryrunpreview.Item{
					Intent:          "pr.comment.apply-suggestion",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "prId": prID, "commentId": commentID, "suggestionIndex": commentSuggestionIdx},
					Action:          "update",
					PredictedAction: "update",
					Reason:          "comment suggestion will be applied",
				})
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			service := commentservice.NewService(client)
			err = service.ApplySuggestion(cmd.Context(), commentservice.RepositoryRef{ProjectKey: repo.ProjectKey, Slug: repo.Slug}, prID, commentID, req)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), AppliedSuggestion{Status: result.OK(), Repository: repositoryOf(repo), PullRequestID: prID, CommentID: commentID})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Applied suggestion on comment %s for pull request %s\n", commentID, prID)
			return nil
		},
	}
	commentApplySuggestionCmd.Flags().StringVar(&commentSuggestionMsg, "commit-message", "", "Commit message for the applied suggestion (default: names the comment)")
	commentApplySuggestionCmd.Flags().Int32Var(&commentSuggestionIdx, "index", 0, "Optional index of the suggestion in the comment (default 0)")
	commentApplySuggestionCmd.Flags().Int32Var(&commentSuggestionCommentVer, "comment-version", 0, "Optional expected version of the comment")
	commentApplySuggestionCmd.Flags().Int32Var(&commentSuggestionPrVer, "pr-version", 0, "Optional expected version of the pull request")
	commentCmd.AddCommand(commentApplySuggestionCmd)
	return commentCmd
}
