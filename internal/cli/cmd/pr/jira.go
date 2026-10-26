package prcmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/prsel"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/style"
	jiraservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/jira"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
)

func newPullRequestJiraCommand(deps Dependencies, repository *string) *cobra.Command {
	jiraCmd := &cobra.Command{
		Use:   "jira <pr-id>",
		Short: "List Jira issues associated with a pull request",
		Example: `  # The Jira issues a pull request refers to
  bb pr jira 42 --repo PROJ/repo`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := deps.LoadConfig()
			if err != nil {
				return err
			}

			target, err := prsel.Resolve(cmd.Context(), args[0], *repository, cfg, nil)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			client := httpclient.NewFromConfig(cfg)
			jiraService := jiraservice.NewService(client)
			issues, err := jiraService.GetPRIssues(cmd.Context(), jiraservice.RepositoryRef{ProjectKey: repo.ProjectKey, Slug: repo.Slug}, target.PullRequestID)
			if err != nil {
				return err
			}

			// The Jira endpoint answers 200 with [] for a pull request that does
			// not exist, where every other endpoint answers 404. Reporting "no
			// linked issues" for a pull request nobody can open is a wrong
			// answer wearing a fact's clothes, and an agent has no way to tell
			// the two apart.
			//
			// Empty is the only ambiguous answer, so it is the only one that
			// pays for the extra request.
			if len(issues) == 0 {
				if _, err := pullrequestservice.NewService(client).Get(cmd.Context(), repo, target.PullRequestID); err != nil {
					return err
				}
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), LinkedIssues{Repository: repositoryOf(repo), PullRequestID: target.PullRequestID, Issues: issuesFrom(issues)})
			}

			if len(issues) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No Jira issues associated with pull request")
				return nil
			}

			rows := make([][]string, len(issues))
			for i, issue := range issues {
				rows[i] = []string{style.Secondary.Render(issue.Key), issue.URL}
			}
			style.WriteTable(cmd.OutOrStdout(), rows)

			return nil
		},
	}
	return jiraCmd
}
