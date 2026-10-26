package prcmd

import (
	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/reposel"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	reviewerservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/reviewer"
)

func newPullRequestDefaultReviewersCommand(deps Dependencies, repository *string) *cobra.Command {
	var defaultReviewersSourceRepoId string
	var defaultReviewersTargetRepoId string
	var defaultReviewersSourceRef string
	var defaultReviewersTargetRef string

	defaultReviewersCmd := &cobra.Command{
		Use:   "default-reviewers",
		Short: "List default reviewers and matching conditions for repository",
		Example: `  # Who Bitbucket would add as reviewers to a pull request from feature/x into main.
  # The ids are the repository's own, which bb repo get --json prints.
  bb pr default-reviewers --repo PROJ/repo \
    --source-ref refs/heads/feature/x --target-ref refs/heads/main \
    --source-repo-id 128 --target-repo-id 128`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			repoProj, repoSlug, err := reposel.Resolve(*repository, cfg)
			if err != nil {
				return err
			}

			service := reviewerservice.NewService(client)

			var sourceRepoIdPtr *string
			var targetRepoIdPtr *string
			var sourceRefPtr *string
			var targetRefPtr *string

			if defaultReviewersSourceRepoId != "" {
				sourceRepoIdPtr = &defaultReviewersSourceRepoId
			}
			if defaultReviewersTargetRepoId != "" {
				targetRepoIdPtr = &defaultReviewersTargetRepoId
			}
			if defaultReviewersSourceRef != "" {
				sourceRefPtr = &defaultReviewersSourceRef
			}
			if defaultReviewersTargetRef != "" {
				targetRefPtr = &defaultReviewersTargetRef
			}

			conditions, err := service.GetDefaultReviewers(cmd.Context(), repoProj, repoSlug, sourceRepoIdPtr, targetRepoIdPtr, sourceRefPtr, targetRefPtr)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), DefaultReviewers{DefaultReviewers: result.ConditionsFrom(conditions)})
			}

			printDefaultReviewers(cmd, conditions)
			return nil
		},
	}

	defaultReviewersCmd.Flags().StringVar(&defaultReviewersSourceRepoId, "source-repo-id", "", "The ID of the repository in which the source ref exists")
	defaultReviewersCmd.Flags().StringVar(&defaultReviewersTargetRepoId, "target-repo-id", "", "The ID of the repository in which the target ref exists")
	defaultReviewersCmd.Flags().StringVar(&defaultReviewersSourceRef, "source-ref", "", "The ID of the source ref (e.g. refs/heads/feature)")
	defaultReviewersCmd.Flags().StringVar(&defaultReviewersTargetRef, "target-ref", "", "The ID of the target ref (e.g. refs/heads/master)")
	return defaultReviewersCmd
}
