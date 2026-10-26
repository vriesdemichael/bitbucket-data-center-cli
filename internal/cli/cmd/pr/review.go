package prcmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/dryrunpreview"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/enumflag"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/preflight"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/prsel"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
	pullrequestactivityservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequestactivity"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
)

func newPullRequestReviewCommand(deps Dependencies, repository *string) *cobra.Command {
	reviewCmd := &cobra.Command{Use: "review", Short: "Approve or request changes, and publish or discard a review"}

	reviewApproveCmd := &cobra.Command{
		Use:   "approve <pr-id>",
		Short: "Approve a pull request",
		Example: `  # Approve a pull request
  bb pr review approve 42 --repo PROJ/repo`,
		Long: `Approve a pull request.

Shorthand for ` + "`bb pr review set <pr-id> APPROVED`" + `. A participant holds one
status, so approving replaces a request for changes rather than joining it.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, apiClient, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
			target, err := prsel.Resolve(cmd.Context(), args[0], *repository, cfg, service)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			if deps.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), deps.PermissionChecker, apiClient, repo.ProjectKey, repo.Slug, openapi.RepoRead); err != nil {
					return err
				}

				current, err := service.Get(cmd.Context(), repo, target.PullRequestID)
				if err != nil {
					return err
				}
				currentUser := resolveCurrentUsername(cmd.Context(), httpclient.NewFromConfig(cfg), cfg)
				predicted := "update"
				reason := "pull request approval will be added"
				if currentUser != "" && reviewerApprovedByUser(current.Reviewers, currentUser) {
					predicted = "no-op"
					reason = "current user has already approved this pull request"
				}

				preview := dryrunpreview.New(dryrunpreview.Item{
					Intent:          "pr.review.approve",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": target.PullRequestID},
					Action:          "update",
					PredictedAction: predicted,
					Tier:            dryrunpreview.TierPreconditionsChecked,
					Reason:          reason,
				})

				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}
			pullRequest, err := service.Approve(cmd.Context(), repo, target.PullRequestID)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), PullRequestChange{Repository: repositoryOf(repo), PullRequest: result.PullRequestFrom(pullRequest)})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Approved pull request #%d\n", pullRequest.ID)
			return nil
		},
	}
	reviewCmd.AddCommand(reviewApproveCmd)

	reviewUnapproveCmd := &cobra.Command{
		Use:   "unapprove <pr-id>",
		Short: "Clear your review status on a pull request",
		Example: `  # Withdraw your approval, or your request for changes
  bb pr review unapprove 42 --repo PROJ/repo`,
		Long: `Clear your review status on a pull request.

Shorthand for ` + "`bb pr review set <pr-id> UNAPPROVED`" + `, and it does more than the
name suggests: a participant holds one status, so this clears a request for
changes as readily as an approval. There is no separate verb for withdrawing
NEEDS_WORK.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, apiClient, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
			target, err := prsel.Resolve(cmd.Context(), args[0], *repository, cfg, service)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			if deps.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), deps.PermissionChecker, apiClient, repo.ProjectKey, repo.Slug, openapi.RepoRead); err != nil {
					return err
				}

				current, err := service.Get(cmd.Context(), repo, target.PullRequestID)
				if err != nil {
					return err
				}
				// A participant holds one status, and this command clears
				// whichever it is. Asking whether they had *approved* answered
				// the wrong question: a reviewer holding NEEDS_WORK has not
				// approved, so the preview called it a no-op while the command
				// went on to clear the request for changes -- exactly the
				// contradiction the reviewer-group dry runs had.
				currentUser := resolveCurrentUsername(cmd.Context(), httpclient.NewFromConfig(cfg), cfg)
				predicted := "update"
				reason := "review status will be cleared"
				if held := reviewerStatusForUser(current.Reviewers, currentUser); held == "" || held == "UNAPPROVED" {
					predicted = "no-op"
					reason = "current user holds no review status to clear"
				}

				preview := dryrunpreview.New(dryrunpreview.Item{
					Intent:          "pr.review.unapprove",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": target.PullRequestID},
					Action:          "update",
					PredictedAction: predicted,
					Tier:            dryrunpreview.TierPreconditionsChecked,
					Reason:          reason,
				})

				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}
			pullRequest, err := service.Unapprove(cmd.Context(), repo, target.PullRequestID)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), PullRequestChange{Repository: repositoryOf(repo), PullRequest: result.PullRequestFrom(pullRequest)})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Removed approval for pull request #%d\n", pullRequest.ID)
			return nil
		},
	}
	reviewCmd.AddCommand(reviewUnapproveCmd)

	reviewSetStatusCmd := &cobra.Command{
		Use:   "set <pr-id> <status>",
		Short: "Set your review status on a pull request",
		Long: `Set your review status on a pull request.

A participant holds exactly one status. APPROVED and NEEDS_WORK are mutually
exclusive, and UNAPPROVED is neither: requesting changes replaces an approval
rather than joining it, and vice versa. Setting the status you already hold is
a no-op.

` + "`approve`" + ` and ` + "`unapprove`" + ` are shorthands for two of these three values.
` + "`unapprove`" + ` clears whichever status you hold, so it withdraws a request for
changes as readily as an approval, which its name does not suggest.`,
		Example: `  # Request changes
  bb pr review set 42 NEEDS_WORK --repo PROJ/repo

  # Approve
  bb pr review set 42 APPROVED --repo PROJ/repo

  # Withdraw whichever status you hold
  bb pr review set 42 UNAPPROVED --repo PROJ/repo`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Validated before anything is loaded or resolved, so a bad value
			// costs no round trip and the message names what was allowed.
			status, err := enumflag.Value("status", args[1], reviewStatuses)
			if err != nil {
				return err
			}

			cfg, apiClient, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
			target, err := prsel.Resolve(cmd.Context(), args[0], *repository, cfg, service)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			if deps.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), deps.PermissionChecker, apiClient, repo.ProjectKey, repo.Slug, openapi.RepoRead); err != nil {
					return err
				}

				current, err := service.Get(cmd.Context(), repo, target.PullRequestID)
				if err != nil {
					return err
				}

				predicted := "update"
				reason := fmt.Sprintf("review status will be set to %s", status)
				currentUser := resolveCurrentUsername(cmd.Context(), httpclient.NewFromConfig(cfg), cfg)
				if held := reviewerStatusForUser(current.Reviewers, currentUser); held == status {
					predicted = "no-op"
					reason = fmt.Sprintf("review status is already %s", status)
				}

				preview := dryrunpreview.New(dryrunpreview.Item{
					Intent:          "pr.review.set",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": target.PullRequestID, "status": status},
					Action:          "update",
					PredictedAction: predicted,
					Tier:            dryrunpreview.TierPreconditionsChecked,
					Reason:          reason,
				})

				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			var pullRequest pullrequestservice.PullRequest
			switch status {
			case "APPROVED":
				pullRequest, err = service.Approve(cmd.Context(), repo, target.PullRequestID)
			case "NEEDS_WORK":
				pullRequest, err = service.NeedsWork(cmd.Context(), repo, target.PullRequestID)
			default:
				pullRequest, err = service.Unapprove(cmd.Context(), repo, target.PullRequestID)
			}
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), PullRequestChange{Repository: repositoryOf(repo), PullRequest: result.PullRequestFrom(pullRequest)})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Review status on pull request #%d is now %s\n", pullRequest.ID, status)

			return nil
		},
	}
	reviewCmd.AddCommand(reviewSetStatusCmd)

	reviewCmd.AddCommand(newPullRequestReviewerCommand(deps, repository))

	reviewGetCmd := &cobra.Command{
		Use:   "get <pr-id>",
		Short: "Retrieve current draft review details",
		Example: `  # The draft comments of a review you have not published yet
  bb pr review get 42 --repo PROJ/repo`,
		Args: cobra.ExactArgs(1),
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

			comments, err := pullrequestactivityservice.NewService(client).Review(cmd.Context(),
				pullrequestactivityservice.RepositoryRef{ProjectKey: repo.ProjectKey, Slug: repo.Slug}, target.PullRequestID,
				pullrequestactivityservice.ListOptions{MaxResults: pullrequestactivityservice.AllResults})
			if err != nil {
				return err
			}

			// Flattened like every other comment listing: a draft reply to a
			// draft comment is one of the unpublished comments this command
			// exists to show.
			drafts := result.FlattenComments(comments)

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), DraftReview{Repository: repositoryOf(repo), PullRequestID: target.PullRequestID, Comments: drafts})
			}

			if len(drafts) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No draft comments found in review")
				return nil
			}

			for _, comment := range drafts {
				fmt.Fprintln(cmd.OutOrStdout(), result.FormatComment(comment))
			}
			return nil
		},
	}
	reviewCmd.AddCommand(reviewGetCmd)

	var reviewCompleteStatus string
	var reviewCompleteComment string
	reviewCompleteCmd := &cobra.Command{
		Use:   "complete <pr-id>",
		Short: "Publish draft comments and optionally submit a status change",
		Long: `Publish your draft review on a pull request: the comments you added with
` + "`bb pr comment add --pending`" + `, together with the status and summary comment
given by --status and --comment.

Bitbucket completes only a review that was started, so this needs at least one
draft comment. Without one it fails and changes nothing. To set a status on its
own, use ` + "`bb pr review set`" + `; to post a comment on its own, use ` + "`bb pr comment add`" + `.`,
		Example: `  # Publish your draft comments
  bb pr review complete 42 --repo PROJ/repo

  # Publish them with a request for changes and a summary comment
  bb pr review complete 42 --repo PROJ/repo --status NEEDS_WORK --comment "Unit tests fail"`,
		Args: cobra.ExactArgs(1),
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

			var body openapigenerated.RestPullRequestFinishReviewRequest
			if reviewCompleteStatus != "" {
				s := strings.ToUpper(reviewCompleteStatus)
				body.ParticipantStatus = &s
			}
			if reviewCompleteComment != "" {
				c := reviewCompleteComment
				body.CommentText = &c
			}

			if deps.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), deps.PermissionChecker, client, repo.ProjectKey, repo.Slug, openapi.RepoWrite); err != nil {
					return err
				}

				started, err := hasDraftReview(cmd.Context(), client, target)
				if err != nil {
					return err
				}
				predicted := dryrunpreview.PredictedUpdate
				reason := "pull request review will be completed"
				var blocking []string
				if !started {
					predicted = dryrunpreview.PredictedBlocked
					reason = noDraftReviewReason(target, reviewCompleteStatus, reviewCompleteComment)
					// The whole reason, since what stops a run is what a verdict
					// prints: it names the command for each part of what was
					// asked (#621), which a shorter line dropped.
					blocking = []string{reason}
				}

				preview := dryrunpreview.New(dryrunpreview.Item{
					Intent:          "pr.review.complete",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": target.PullRequestID, "status": reviewCompleteStatus, "comment": reviewCompleteComment},
					Action:          "update",
					PredictedAction: predicted,
					Tier:            dryrunpreview.TierPreconditionsChecked,
					Reason:          reason,
					BlockingReasons: blocking,
					// The real run meets Bitbucket's 404 for a review that was
					// never started, and noDraftReviewError keeps it not_found.
					Fails: apperrors.KindNotFound,
				})
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			response, err := client.FinishReviewWithResponse(cmd.Context(), repo.ProjectKey, repo.Slug, target.PullRequestID, nil, body)
			if err != nil {
				return err
			}
			if isNoDraftReview(response.StatusCode(), response.Body) {
				return noDraftReviewError(target, reviewCompleteStatus, reviewCompleteComment)
			}
			if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), ReviewChange{Status: result.OK(), Repository: repositoryOf(repo), PullRequestID: target.PullRequestID, Review: "completed"})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Completed review for pull request #%s\n", target.PullRequestID)
			return nil
		},
	}
	enumflag.Register(reviewCompleteCmd.Flags(), &reviewCompleteStatus, "status", "", reviewStatuses, "Pull request status change")
	reviewCompleteCmd.Flags().StringVar(&reviewCompleteComment, "comment", "", "Review completion comment text")
	reviewCmd.AddCommand(reviewCompleteCmd)

	reviewDiscardCmd := &cobra.Command{
		Use:   "discard <pr-id>",
		Short: "Discard all draft comments and cancel review",
		Example: `  # Throw away the draft comments of a review you have not published
  bb pr review discard 42 --repo PROJ/repo`,
		Args: cobra.ExactArgs(1),
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

			if deps.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), deps.PermissionChecker, client, repo.ProjectKey, repo.Slug, openapi.RepoWrite); err != nil {
					return err
				}

				preview := dryrunpreview.New(dryrunpreview.Item{
					Intent:          "pr.review.discard",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": target.PullRequestID},
					Action:          "delete",
					PredictedAction: "delete",
					Reason:          "pull request review will be discarded",
				})
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			response, err := client.DiscardReviewWithResponse(cmd.Context(), repo.ProjectKey, repo.Slug, target.PullRequestID)
			if err != nil {
				return err
			}
			if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), ReviewChange{Status: result.OK(), Repository: repositoryOf(repo), PullRequestID: target.PullRequestID, Review: "discarded"})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Discarded review for pull request #%s\n", target.PullRequestID)
			return nil
		},
	}
	reviewCmd.AddCommand(reviewDiscardCmd)
	return reviewCmd
}
