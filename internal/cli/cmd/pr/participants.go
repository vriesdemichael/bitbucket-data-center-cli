package prcmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/reposel"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/style"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
)

func newPullRequestParticipantsCommand(deps Dependencies, repository *string) *cobra.Command {
	var searchFilter string
	participantsCmd := &cobra.Command{
		Use:   "participants",
		Short: "Search pull request participants across a repository",
		Example: `  # People who took part in the repository's pull requests, by name
  bb pr participants --search alice --repo PROJ/repo`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, apiClient, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}
			repoProj, repoSlug, err := reposel.Resolve(*repository, cfg)
			if err != nil {
				return err
			}
			repo := pullrequestservice.RepositoryRef{ProjectKey: repoProj, Slug: repoSlug}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg)).WithAPIClient(apiClient)
			participants, err := service.SearchParticipants(cmd.Context(), repo, searchFilter)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), Participants{Repository: repositoryOf(repo), Participants: participantsFrom(participants)})
			}

			if len(participants) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No participants found"))
				return nil
			}

			rows := make([][]string, len(participants))
			for i, p := range participants {
				activeStr := "active"
				if !p.Active {
					activeStr = "inactive"
				}
				rows[i] = []string{p.Name, p.DisplayName, p.EmailAddress, activeStr}
			}
			style.WriteTable(cmd.OutOrStdout(), rows)
			return nil
		},
	}
	participantsCmd.Flags().StringVar(&searchFilter, "search", "", "Query filter (checks username, name, or email)")
	_ = participantsCmd.MarkFlagRequired("search")
	return participantsCmd
}
