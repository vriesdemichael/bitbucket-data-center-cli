package prcmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/paging"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/prsel"
	pullrequestactivityservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequestactivity"
)

func newPullRequestActivityCommand(deps Dependencies, repository *string) *cobra.Command {
	activityCmd := &cobra.Command{
		Use:   "activity",
		Short: "List what happened on a pull request",
		Long:  "Pull request activity commands. This is an explicit exception to the stable versioned API and is intended only for AI ingestion and debugging.",
	}

	var activityPaging paging.Options
	activityListCmd := &cobra.Command{
		Use:   "list <pr-id>",
		Short: "List raw pull request activity items",
		Example: `  # Everything that happened on a pull request, as Bitbucket records it
  bb pr activity list 42 --repo PROJ/repo --all`,
		Long: "List raw pull request activity items. This output is an explicit exception to the stable versioned API and is intended only for AI ingestion and debugging.",
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

			service := pullrequestactivityservice.NewService(client)
			activities, err := service.List(cmd.Context(), pullrequestactivityservice.RepositoryRef{ProjectKey: repo.ProjectKey, Slug: repo.Slug}, target.PullRequestID, pullrequestactivityservice.ListOptions{MaxResults: activityPaging.ServiceLimit()})
			if err != nil {
				return err
			}

			// The service already stops at the cap (ADR-074); this keeps --limit
			// honest if one ever does not. A no-op under --all.
			activities = paging.Truncate(activityPaging, activities)

			if deps.JSONEnabled() {
				return deps.WriteJSONList(cmd.OutOrStdout(), Activities{Repository: repositoryOf(repo), PullRequestID: target.PullRequestID, Activities: activitiesFrom(activities)}, paging.LimitReached(activityPaging, len(activities)))
			}

			if len(activities) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No activities found")
				return nil
			}

			for _, activity := range activities {
				fmt.Fprintln(cmd.OutOrStdout(), formatPullRequestActivitySummary(activity))
			}
			paging.Hint(cmd.ErrOrStderr(), activityPaging, len(activities))

			return nil
		},
	}
	activityPaging.Register(activityListCmd, 25)
	activityCmd.AddCommand(activityListCmd)
	return activityCmd
}
