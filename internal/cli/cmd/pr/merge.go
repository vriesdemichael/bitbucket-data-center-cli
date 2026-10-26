package prcmd

import (
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/dryrunpreview"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/enumflag"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/preflight"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/prsel"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
)

func newPullRequestTransitionCommands(deps Dependencies, repository *string) []*cobra.Command {
	var transitionVersion int
	var mergeStrategy string
	mergeCmd := &cobra.Command{
		Use:   "merge <pr-id>",
		Short: "Merge a pull request",
		Example: `  # Merge with the repository's default strategy
  bb pr merge 42 --repo PROJ/repo

  # Squash, and only if nobody changed it since the version you read
  bb pr merge 42 --repo PROJ/repo --strategy squash --version 3`,
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

				// State alone was the whole prediction, so any open pull request
				// was reported as "will be merged" at confidence full -- with an
				// empty blockingReasons -- however many vetoes stood against it.
				// Merging is the irreversible pull request operation, so this was
				// the weakest prediction in the tool making the strongest claim
				// the contract offers (#479).
				//
				// service.Get already fetches mergeability on this same call. It
				// was fetched and then not read.
				predicted := "update"
				reason := "pull request will be merged"
				blocking := []string{}
				tier := dryrunpreview.TierPreconditionsChecked

				switch {
				case strings.EqualFold(strings.TrimSpace(current.State), "MERGED"):
					// Bitbucket does not merge it again: it refuses, with a 409.
					predicted = "blocked"
					reason = "pull request is already merged"
					blocking = []string{reason}
				case !strings.EqualFold(strings.TrimSpace(current.State), "OPEN"):
					predicted = "blocked"
					reason = "pull request is not open"
					blocking = []string{"pull request is not open"}
				case current.Draft:
					// Before mergeability, which does not know about drafts: on a
					// draft it answers canMerge with no vetoes, and the merge is
					// then refused with a 409.
					predicted = "blocked"
					reason = "pull request is a draft; mark it ready for review with bb pr ready before merging it"
					blocking = []string{reason}
				case current.Mergeability == nil:
					// Asked and not answered. Saying "will be merged" here would
					// be a guess wearing the same label as a checked answer, so
					// the tier drops to the one that cannot report full.
					reason = "pull request is open; bitbucket did not report whether it can be merged"
					tier = dryrunpreview.TierPredicted
				case !current.Mergeability.Mergeable:
					predicted = "blocked"
					reason = "pull request cannot be merged"
					blocking = mergeBlockingReasons(*current.Mergeability)
				}

				// A strategy the repository has not enabled is refused when the
				// merge is sent, whatever the mergeability check said, so it is
				// looked up rather than left for the real run to find.
				// Bitbucket answers it 400, which is a validation error rather than
				// the conflict the refusals above are.
				var fails apperrors.Kind
				if mergeStrategy != "" && predicted != "blocked" {
					enabled, err := service.EnabledMergeStrategies(cmd.Context(), repo)
					switch {
					case err != nil:
						reason = fmt.Sprintf("pull request is open; whether the %s merge strategy is enabled could not be read", mergeStrategy)
						tier = dryrunpreview.TierPredicted
					case !slices.Contains(enabled, mergeStrategy):
						predicted = "blocked"
						reason = fmt.Sprintf("the %s merge strategy is not enabled for %s/%s; enabled: %s",
							mergeStrategy, repo.ProjectKey, repo.Slug, strings.Join(enabled, ", "))
						blocking = []string{reason}
						fails = apperrors.KindValidation
					}
				}

				preview := dryrunpreview.New(dryrunpreview.Item{
					Intent:          "pr.merge",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": target.PullRequestID},
					Action:          "update",
					PredictedAction: predicted,
					Tier:            tier,
					Reason:          reason,
					BlockingReasons: blocking,
					Fails:           fails,
				})

				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			var version *int
			if cmd.Flags().Changed("version") {
				version = &transitionVersion
			}

			merged, err := service.MergeWith(cmd.Context(), repo, target.PullRequestID,
				pullrequestservice.MergeOptions{Version: version, StrategyID: mergeStrategy})
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), PullRequestChange{Repository: repositoryOf(repo), PullRequest: result.PullRequestFrom(merged)})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Merged pull request #%d\n", merged.ID)
			return nil
		},
	}
	mergeCmd.Flags().IntVar(&transitionVersion, "version", 0, "Expected pull request version; omit to act on whatever version is current")
	enumflag.Register(mergeCmd.Flags(), &mergeStrategy, "strategy", "", openapi.MergeStrategies, "How to merge; omit for the repository's default. Bitbucket refuses one the repository has not enabled")

	declineCmd := &cobra.Command{
		Use:     "decline <pr-id>",
		Aliases: []string{"close"},
		Short:   "Decline a pull request",
		Example: `  # Decline a pull request
  bb pr decline 42 --repo PROJ/repo

  # Only if nobody changed it since the version you read
  bb pr decline 42 --repo PROJ/repo --version 3`,
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

				predicted := "update"
				reason := "pull request will be declined"
				// Declining a declined pull request is refused with a 409, not
				// accepted as nothing to do.
				if strings.EqualFold(strings.TrimSpace(current.State), "DECLINED") {
					predicted = "blocked"
					reason = "pull request is already declined"
				}

				preview := dryrunpreview.New(dryrunpreview.Item{
					Intent:          "pr.decline",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": target.PullRequestID},
					Action:          "update",
					PredictedAction: predicted,
					Tier:            dryrunpreview.TierPreconditionsChecked,
					Reason:          reason,
				})

				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			var version *int
			if cmd.Flags().Changed("version") {
				version = &transitionVersion
			}

			declined, err := service.Decline(cmd.Context(), repo, target.PullRequestID, version)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), PullRequestChange{Repository: repositoryOf(repo), PullRequest: result.PullRequestFrom(declined)})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Declined pull request #%d\n", declined.ID)
			return nil
		},
	}
	declineCmd.Flags().IntVar(&transitionVersion, "version", 0, "Expected pull request version; omit to act on whatever version is current")

	reopenCmd := &cobra.Command{
		Use:   "reopen <pr-id>",
		Short: "Reopen a pull request",
		Example: `  # Reopen a declined pull request
  bb pr reopen 42 --repo PROJ/repo`,
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

				predicted := "update"
				reason := "pull request will be reopened"
				// Reopening an open pull request is refused with a 409.
				if strings.EqualFold(strings.TrimSpace(current.State), "OPEN") {
					predicted = "blocked"
					reason = "pull request is already open"
				}

				preview := dryrunpreview.New(dryrunpreview.Item{
					Intent:          "pr.reopen",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": target.PullRequestID},
					Action:          "update",
					PredictedAction: predicted,
					Tier:            dryrunpreview.TierPreconditionsChecked,
					Reason:          reason,
				})

				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			var version *int
			if cmd.Flags().Changed("version") {
				version = &transitionVersion
			}

			reopened, err := service.Reopen(cmd.Context(), repo, target.PullRequestID, version)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), PullRequestChange{Repository: repositoryOf(repo), PullRequest: result.PullRequestFrom(reopened)})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Reopened pull request #%d\n", reopened.ID)
			return nil
		},
	}
	reopenCmd.Flags().IntVar(&transitionVersion, "version", 0, "Expected pull request version; omit to act on whatever version is current")
	return []*cobra.Command{mergeCmd, declineCmd, reopenCmd}
}
