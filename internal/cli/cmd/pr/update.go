package prcmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/dryrunpreview"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/preflight"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/prsel"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
	reviewerservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/reviewer"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
)

func newPullRequestUpdateCommand(deps Dependencies, repository *string) *cobra.Command {
	var updateTitle string
	var updateDescription string
	var updateVersion int
	var updateDraft bool
	var updateReviewers []string
	updateCmd := &cobra.Command{
		Use:     "update <pr-id>",
		Aliases: []string{"edit"},
		Short:   "Update pull request metadata",
		Long: "Update a pull request's title, description, draft state or reviewers.\n\n" +
			"Bitbucket refuses an update carrying a version the pull request has moved on from, rather than " +
			"overwriting someone else's change. bb reads that version, so --version is only needed to assert a " +
			"particular one: pass it and a pull request that has changed since is reported instead of updated.\n\n" +
			"To mark a draft ready for review, or turn a pull request back into a draft, use bb pr ready.",
		Example: "  # Update title and description\n" +
			"  bb pr update 42 --repo PROJ/repo --title \"New title\" \\\n" +
			"    --description \"What changed since the first review.\"\n\n" +
			"  # Replace the reviewers with two people and a reviewer group\n" +
			"  bb pr update 42 --repo PROJ/repo --reviewers alice,bob,@backend-team\n\n" +
			"  # Mark a draft PR as ready for review\n" +
			"  bb pr update 42 --repo PROJ/repo --draft=false\n\n" +
			"  # Refuse the update if the pull request has changed since version 1\n" +
			"  bb pr update 42 --repo PROJ/repo --version 1 --draft",
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

			var draft *bool
			if cmd.Flags().Changed("draft") {
				draft = &updateDraft
			}

			// Nil is "whatever version it is now", which is what the service
			// reads for a caller who did not assert one.
			var version *int
			if cmd.Flags().Changed("version") {
				version = &updateVersion
			}

			if deps.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), deps.PermissionChecker, apiClient, repo.ProjectKey, repo.Slug, openapi.RepoWrite); err != nil {
					return err
				}

				current, err := service.Get(cmd.Context(), repo, target.PullRequestID)
				if err != nil {
					return err
				}

				// Only the fields the caller named are compared, because only
				// those travel: buildUpdatePayload drops an empty title and an
				// empty description rather than sending them. Comparing them
				// anyway made `pr update --draft=false` on a pull request that
				// is already not a draft predict an update, because the title
				// nobody passed did not match the one the pull request has --
				// a preview promising a change the command would not make.
				changed := false
				if cmd.Flags().Changed("title") &&
					!strings.EqualFold(strings.TrimSpace(current.Title), strings.TrimSpace(updateTitle)) {
					changed = true
				}
				if cmd.Flags().Changed("description") &&
					!strings.EqualFold(strings.TrimSpace(current.Description), strings.TrimSpace(updateDescription)) {
					changed = true
				}
				if draft != nil && current.Draft != *draft {
					changed = true
				}
				if cmd.Flags().Changed("reviewers") {
					changed = true
				}

				predicted := "update"
				reason := "pull request metadata will be updated"
				if !changed {
					predicted = "no-op"
					reason = "pull request already matches requested metadata"
				}

				preview := dryrunpreview.New(dryrunpreview.Item{
					Intent:          "pr.update",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": target.PullRequestID, "title": updateTitle, "description": updateDescription, "version": version, "draft": draft},
					Action:          "update",
					PredictedAction: predicted,
					Tier:            dryrunpreview.TierPreconditionsChecked,
					Reason:          reason,
				})

				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			// Left nil, the service echoes the pull request's current reviewers
			// back, because Bitbucket reads an absent key as "no reviewers"
			// (#511). --reviewers is the way to say something else on purpose,
			// and --reviewers "" is how to clear the list.
			var reviewers *[]string
			if cmd.Flags().Changed("reviewers") {
				resolved, err := resolveReviewersAndGroups(
					cmd.Context(),
					reviewerservice.NewService(apiClient),
					repo.ProjectKey, repo.Slug,
					updateReviewers,
					nil,
					"",
				)
				if err != nil {
					return err
				}
				if resolved == nil {
					resolved = []string{}
				}
				reviewers = &resolved
			}

			updated, err := service.Update(cmd.Context(), repo, target.PullRequestID, pullrequestservice.UpdateInput{
				Title:       updateTitle,
				Description: updateDescription,
				Version:     version,
				Draft:       draft,
				Reviewers:   reviewers,
			})
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), PullRequestChange{Repository: repositoryOf(repo), PullRequest: result.PullRequestFrom(updated)})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Updated pull request #%d\n", updated.ID)
			return nil
		},
	}
	updateCmd.Flags().StringVar(&updateTitle, "title", "", "Updated pull request title")
	updateCmd.Flags().StringVar(&updateDescription, "description", "", "Updated pull request description")
	updateCmd.Flags().IntVar(&updateVersion, "version", 0, "Expected pull request version; omit to act on whatever version is current")
	updateCmd.Flags().BoolVar(&updateDraft, "draft", false, "Set draft state: --draft to mark as draft, --draft=false to mark as ready for review")
	updateCmd.Flags().StringSliceVar(&updateReviewers, "reviewers", nil, "Replace the reviewers (repeatable or comma-separated, accepts @group syntax); omit to keep the current reviewers, pass \"\" to clear them")
	return updateCmd
}
