package prcmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/dryrunpreview"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/preflight"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/prsel"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	codeowners "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/codeowners"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
	reviewerservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/reviewer"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
)

func newPullRequestReviewerCommand(deps Dependencies, repository *string) *cobra.Command {
	reviewerCmd := &cobra.Command{Use: "reviewer", Short: "Add or remove a pull request's reviewers"}
	var reviewerUsers []string
	var reviewerGroups []string
	var reviewerDefaultReviewers bool
	var reviewerCodeOwners bool
	reviewerAddCmd := &cobra.Command{
		Use:   "add <pr-id>",
		Short: "Add reviewers to a pull request",
		Example: "  # Add a single reviewer\n" +
			"  bb pr review reviewer add 42 --repo PROJ/repo --user alice\n\n" +
			"  # Add multiple reviewers (repeatable or comma-separated)\n" +
			"  bb pr review reviewer add 42 --repo PROJ/repo --user alice --user bob\n" +
			"  bb pr review reviewer add 42 --repo PROJ/repo --users alice,bob\n\n" +
			"  # Add a reviewer group\n" +
			"  bb pr review reviewer add 42 --repo PROJ/repo --reviewer-group core-team\n" +
			"  bb pr review reviewer add 42 --repo PROJ/repo --user @core-team\n\n" +
			"  # Add default reviewers configured on repository/project\n" +
			"  bb pr review reviewer add 42 --repo PROJ/repo --default-reviewers\n\n" +
			"  # Add reviewers matching .bitbucket/CODEOWNERS\n" +
			"  bb pr review reviewer add 42 --repo PROJ/repo --codeowners",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(reviewerUsers) == 0 && len(reviewerGroups) == 0 && !reviewerDefaultReviewers && !reviewerCodeOwners {
				return apperrors.New(apperrors.KindValidation, "at least one reviewer (--user), reviewer group (--reviewer-group), --default-reviewers, or --codeowners is required", nil)
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
				if err := preflight.RepoPermission(cmd.Context(), deps.PermissionChecker, apiClient, repo.ProjectKey, repo.Slug, openapi.RepoWrite); err != nil {
					return err
				}
			}

			current, err := service.Get(cmd.Context(), repo, target.PullRequestID)
			if err != nil {
				return err
			}

			author := current.AuthorUsername
			if author == "" {
				author = current.Author
			}

			reviewerSvc := reviewerservice.NewService(apiClient)

			var allUsers []string
			var allGroups []string
			allUsers = append(allUsers, reviewerUsers...)
			allGroups = append(allGroups, reviewerGroups...)

			if reviewerDefaultReviewers {
				query := reviewerservice.DefaultReviewerQuery{
					SourceRef: current.SourceBranch,
					TargetRef: current.TargetBranch,
				}
				// A fork pull request has its source branch elsewhere, and the
				// conditions are matched against that repository.
				if current.SourceRepository != nil {
					query.SourceProjectKey = current.SourceRepository.ProjectKey
					query.SourceSlug = current.SourceRepository.Slug
				}

				defaults, err := reviewerSvc.ResolveDefaultReviewers(cmd.Context(), repo.ProjectKey, repo.Slug, query)
				if err != nil {
					return err
				}
				allUsers = append(allUsers, defaults...)
			}

			if reviewerCodeOwners {
				// A fork pull request has its source branch in another
				// repository, and the endpoint needs to be told which.
				var codeOwnersSource *codeowners.RepositoryRef
				if current.SourceRepository != nil {
					codeOwnersSource = &codeowners.RepositoryRef{
						ProjectKey: current.SourceRepository.ProjectKey,
						Slug:       current.SourceRepository.Slug,
					}
				}

				codeOwners, err := resolveCodeOwnersReviewers(
					cmd.Context(),
					cfg,
					codeowners.RepositoryRef{ProjectKey: repo.ProjectKey, Slug: repo.Slug},
					codeOwnersSource,
					current.SourceBranch, current.TargetBranch,
					author,
				)
				if err != nil {
					return err
				}
				allUsers = append(allUsers, codeOwners...)
			}

			resolvedReviewers, err := resolveReviewersAndGroups(
				cmd.Context(),
				reviewerSvc,
				repo.ProjectKey, repo.Slug,
				allUsers,
				allGroups,
				author,
			)
			if err != nil {
				return err
			}

			if deps.DryRunEnabled() {
				if len(resolvedReviewers) == 0 {
					preview := dryrunpreview.New(dryrunpreview.Item{
						Intent:          "pr.review.reviewer.add",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": target.PullRequestID},
						Action:          "update",
						PredictedAction: "no-op",
						Reason:          "no eligible reviewers to add",
					})
					return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
				}

				var items []dryrunpreview.Item

				for _, u := range resolvedReviewers {
					predicted := "update"
					reason := "reviewer will be added"
					if isAuthor(current.Author, current.AuthorUsername, u) {
						predicted = "no-op"
						reason = "pull request author cannot be reviewer"
					} else if hasReviewer(current.Reviewers, u) {
						predicted = "no-op"
						reason = "reviewer already present"
					}

					items = append(items, dryrunpreview.Item{
						Intent:          "pr.review.reviewer.add",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": target.PullRequestID, "user": u},
						Action:          "update",
						PredictedAction: predicted,
						Reason:          reason,
					})
				}

				preview := dryrunpreview.New(items...)
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			var addedReviewers []string
			var skippedAuthor []string
			var alreadyPresent []string
			var failedReviewers []string
			var addErrs []error

			for _, u := range resolvedReviewers {
				if isAuthor(current.Author, current.AuthorUsername, u) {
					skippedAuthor = append(skippedAuthor, u)
					continue
				}
				if hasReviewer(current.Reviewers, u) {
					alreadyPresent = append(alreadyPresent, u)
					continue
				}
				// Reviewers are added one request at a time, so a failure partway
				// through leaves earlier reviewers on the pull request. Keep going
				// and report the whole outcome rather than aborting and hiding
				// what already succeeded.
				if _, err := service.AddReviewer(cmd.Context(), repo, target.PullRequestID, u); err != nil {
					failedReviewers = append(failedReviewers, u)
					addErrs = append(addErrs, fmt.Errorf("%s: %w", u, err))
					continue
				}
				addedReviewers = append(addedReviewers, u)
			}

			// Adding a participant returns the participant, not the pull request,
			// so the response cannot stand in for the pull request. Report the one
			// that was actually fetched.
			latestPR := current

			if deps.JSONEnabled() {
				// Under --json stdout carries exactly one document: a success
				// envelope, or the failure envelope the entry point writes when
				// this returns an error. Emitting both would produce two.
				// So on a partial failure the outcome goes into the error itself.
				if len(addErrs) > 0 {
					return partialReviewerAddError(addedReviewers, failedReviewers, addErrs)
				}

				return deps.WriteJSON(cmd.OutOrStdout(), ReviewerAddition{
					Repository:     repositoryOf(repo),
					PullRequest:    result.PullRequestFrom(latestPR),
					Added:          addedReviewers,
					SkippedAuthor:  skippedAuthor,
					AlreadyPresent: alreadyPresent,
				})
			}

			if len(addedReviewers) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Added %s to pull request #%d\n", formatReviewerList(addedReviewers), latestPR.ID)
			}
			if len(skippedAuthor) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Skipped %s (pull request author)\n", strings.Join(skippedAuthor, ", "))
			}
			if len(alreadyPresent) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Reviewers already present: %s\n", strings.Join(alreadyPresent, ", "))
			}
			if len(addErrs) > 0 {
				return partialReviewerAddError(addedReviewers, failedReviewers, addErrs)
			}
			if len(addedReviewers) == 0 && len(skippedAuthor) == 0 && len(alreadyPresent) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No eligible reviewers to add")
			}
			return nil
		},
	}
	reviewerAddCmd.Flags().StringSliceVar(&reviewerUsers, "user", nil, "Reviewer username(s) (repeatable or comma-separated, accepts @group syntax; aliases --users, --reviewers)")
	reviewerAddCmd.Flags().StringSliceVar(&reviewerGroups, "reviewer-group", nil, "Reviewer group name(s) to expand and add (repeatable or comma-separated; a leading @ and a reviewer-group/ prefix are both accepted; alias --reviewer-groups)")
	reviewerAddCmd.Flags().SetNormalizeFunc(reviewerAddFlagAliases)
	reviewerAddCmd.Flags().BoolVar(&reviewerDefaultReviewers, "default-reviewers", false, "Assign default reviewers configured on repository/project for this pull request")
	reviewerAddCmd.Flags().BoolVar(&reviewerCodeOwners, "codeowners", false, "Assign the code owners Bitbucket reports for the change, the same ones the web interface offers; the CODEOWNERS syntax and its meaning are the server's")
	reviewerCmd.AddCommand(reviewerAddCmd)

	var removeReviewerUsername string
	reviewerRemoveCmd := &cobra.Command{
		Use:   "remove <pr-id>",
		Short: "Remove a reviewer",
		Example: `  # Take someone off a pull request's reviewers
  bb pr review reviewer remove 42 --user alice --repo PROJ/repo`,
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
				if err := preflight.RepoPermission(cmd.Context(), deps.PermissionChecker, apiClient, repo.ProjectKey, repo.Slug, openapi.RepoWrite); err != nil {
					return err
				}

				current, err := service.Get(cmd.Context(), repo, target.PullRequestID)
				if err != nil {
					return err
				}
				predicted := "delete"
				reason := "reviewer will be removed"
				fails := apperrors.KindConflict
				if !hasReviewer(current.Reviewers, removeReviewerUsername) {
					// Removing a user who is not a reviewer goes through and
					// changes nothing -- except for the author, refused with a
					// 409, and a user who does not exist, refused with a 404,
					// which one lookup tells apart.
					predicted = "no-op"
					reason = "reviewer is not present"
					if isAuthor(current.Author, current.AuthorUsername, removeReviewerUsername) {
						predicted = "blocked"
						reason = "the author of a pull request may not be unassigned from their role"
					} else {
						exists, err := userExists(cmd.Context(), apiClient, removeReviewerUsername)
						if err != nil {
							return err
						}
						if !exists {
							predicted = "blocked"
							reason = fmt.Sprintf("there is no user named %s", removeReviewerUsername)
							fails = apperrors.KindNotFound
						}
					}
				}

				preview := dryrunpreview.New(dryrunpreview.Item{
					Intent:          "pr.review.reviewer.remove",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": target.PullRequestID, "user": removeReviewerUsername},
					Action:          "delete",
					PredictedAction: predicted,
					Tier:            dryrunpreview.TierPreconditionsChecked,
					Reason:          reason,
					Fails:           fails,
				})

				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}
			pullRequest, err := service.RemoveReviewer(cmd.Context(), repo, target.PullRequestID, removeReviewerUsername)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), PullRequestChange{Repository: repositoryOf(repo), PullRequest: result.PullRequestFrom(pullRequest)})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Removed reviewer %s from pull request #%d\n", removeReviewerUsername, pullRequest.ID)
			return nil
		},
	}
	reviewerRemoveCmd.Flags().StringVar(&removeReviewerUsername, "user", "", "Reviewer username")
	_ = reviewerRemoveCmd.MarkFlagRequired("user")
	reviewerCmd.AddCommand(reviewerRemoveCmd)
	return reviewerCmd
}

// partialReviewerAddError describes the outcome of a reviewer add that only
// partly succeeded.
//
// Reviewers are attached one request at a time, so the reviewers added before
// the failure stay on the pull request. The message names them, because under
// --json the failure envelope carries no payload and this message is the only
// place that record can live.
func partialReviewerAddError(added, failed []string, causes []error) error {
	var detail strings.Builder
	detail.WriteString("failed to add ")
	detail.WriteString(strings.Join(failed, ", "))
	if len(added) > 0 {
		detail.WriteString("; already added ")
		detail.WriteString(strings.Join(added, ", "))
	}
	detail.WriteString(": ")
	detail.WriteString(errors.Join(causes...).Error())

	return apperrors.New(apperrors.KindOf(causes[0]), detail.String(), errors.Join(causes...))
}

func formatReviewerList(users []string) string {
	if len(users) == 1 {
		return fmt.Sprintf("reviewer %s", users[0])
	}
	return fmt.Sprintf("reviewers %s", strings.Join(users, ", "))
}

func isAuthor(author, authorUsername, username string) bool {
	u := strings.TrimSpace(username)
	if u == "" {
		return false
	}
	return (author != "" && strings.EqualFold(u, strings.TrimSpace(author))) ||
		(authorUsername != "" && strings.EqualFold(u, strings.TrimSpace(authorUsername)))
}

// userExists reports whether Bitbucket has a user by that name.
func userExists(ctx context.Context, client *openapigenerated.ClientWithResponses, username string) (bool, error) {
	response, err := client.GetUserWithResponse(ctx, strings.TrimSpace(username))
	if err != nil {
		return false, apperrors.Transport(fmt.Sprintf("failed to look up user %q", username), err)
	}
	if response.StatusCode() == http.StatusNotFound {
		return false, nil
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return false, err
	}

	return true, nil
}
