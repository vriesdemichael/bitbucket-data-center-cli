package commitcmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/paging"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/reposel"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/style"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
	commitservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/commit"
	jiraservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/jira"
	pullrequestservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/pullrequest"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/transport/httpclient"
)

type Dependencies struct {
	JSONEnabled         func() bool
	LoadConfig          func() (config.AppConfig, error)
	LoadConfigAndClient func() (config.AppConfig, *openapigenerated.ClientWithResponses, error)
	WriteJSON           func(io.Writer, any) error
	WriteJSONList       func(io.Writer, any, bool) error
}

func (deps *Dependencies) withDefaults() Dependencies {
	d := *deps
	if d.JSONEnabled == nil {
		d.JSONEnabled = func() bool { return false }
	}
	if d.LoadConfig == nil {
		d.LoadConfig = func() (config.AppConfig, error) {
			return config.LoadFromEnv()
		}
	}
	if d.LoadConfigAndClient == nil {
		d.LoadConfigAndClient = func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
			cfg, err := d.LoadConfig()
			if err != nil {
				return config.AppConfig{}, nil, err
			}
			client, err := openapi.NewClientWithResponsesFromConfig(cfg)
			if err != nil {
				return config.AppConfig{}, nil, err
			}
			return cfg, client, nil
		}
	}
	if d.WriteJSON == nil {
		d.WriteJSON = jsonoutput.Write
	}
	if d.WriteJSONList == nil {
		d.WriteJSONList = jsonoutput.WriteList
	}
	return d
}

func resolveCommitRepositoryReference(selector string, cfg config.AppConfig) (commitservice.RepositoryRef, error) {
	projectKey, slug, err := reposel.Resolve(selector, cfg)
	if err != nil {
		return commitservice.RepositoryRef{}, err
	}
	return commitservice.RepositoryRef{ProjectKey: projectKey, Slug: slug}, nil
}

func safeString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func New(deps Dependencies) *cobra.Command {
	d := deps.withDefaults()

	var repositorySelector string
	var listPaging paging.Options
	var start int

	commitCmd := &cobra.Command{
		Use:   "commit",
		Short: "Commit inspection and compare commands",
	}

	commitCmd.PersistentFlags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug (defaults to BITBUCKET_PROJECT_KEY + BITBUCKET_REPO_SLUG)")
	listPaging.RegisterPersistent(commitCmd, 25)
	commitCmd.PersistentFlags().IntVar(&start, "start", 0, "Start offset for list operations")

	var listPath string
	var listJira string
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List repository commits",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := d.LoadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveCommitRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			var commits []openapigenerated.RestCommit
			if strings.TrimSpace(listJira) != "" {
				jiraService := jiraservice.NewService(httpclient.NewFromConfig(cfg))
				commits, err = jiraService.GetIssueCommits(cmd.Context(), strings.TrimSpace(listJira), listPaging.ServiceLimit())
				if err != nil {
					return err
				}
			} else {
				service := commitservice.NewService(client)
				commits, err = service.List(cmd.Context(), repo, commitservice.ListOptions{MaxResults: listPaging.ServiceLimit(), Start: start, Path: listPath})
				if err != nil {
					return err
				}
			}

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "commits": commits})
			}

			if len(commits) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No commits found"))
				return nil
			}

			rows := make([][]string, len(commits))
			for i, commit := range commits {
				rows[i] = []string{style.Secondary.Render(safeString(commit.DisplayId)), strings.Split(safeString(commit.Message), "\n")[0]}
			}
			style.WriteTable(cmd.OutOrStdout(), rows)

			return nil
		},
	}
	listCmd.Flags().StringVar(&listPath, "path", "", "Filter commits by file path")
	listCmd.Flags().StringVar(&listJira, "jira", "", "List commits associated with a Jira issue key")
	commitCmd.AddCommand(listCmd)

	getCmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get a specific commit",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := d.LoadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveCommitRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := commitservice.NewService(client)
			commit, err := service.Get(cmd.Context(), repo, args[0])
			if err != nil {
				return err
			}

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "commit": commit})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Label.Render("Commit:"), style.Secondary.Render(safeString(commit.Id)))
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Label.Render("Message:"), safeString(commit.Message))
			return nil
		},
	}
	commitCmd.AddCommand(getCmd)

	compareCmd := &cobra.Command{
		Use:   "compare <from> <to>",
		Short: "Compare two commits or refs",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := d.LoadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveCommitRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := commitservice.NewService(client)
			commits, err := service.Compare(cmd.Context(), repo, commitservice.CompareOptions{
				From:       args[0],
				To:         args[1],
				MaxResults: listPaging.ServiceLimit(),
			})
			if err != nil {
				return err
			}

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "commits": commits})
			}

			if len(commits) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No commits found between refs"))
				return nil
			}

			rows := make([][]string, len(commits))
			for i, commit := range commits {
				rows[i] = []string{style.Secondary.Render(safeString(commit.DisplayId)), strings.Split(safeString(commit.Message), "\n")[0]}
			}
			style.WriteTable(cmd.OutOrStdout(), rows)

			return nil
		},
	}
	commitCmd.AddCommand(compareCmd)

	prsCmd := &cobra.Command{
		Use:   "prs <commitId>",
		Short: "List pull requests containing a commit",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, apiClient, err := d.LoadConfigAndClient()
			if err != nil {
				return err
			}

			projectKey, slug, err := reposel.Resolve(repositorySelector, cfg)
			if err != nil {
				return err
			}

			repo := pullrequestservice.RepositoryRef{ProjectKey: projectKey, Slug: slug}
			service := pullrequestservice.NewService(nil).WithAPIClient(apiClient)

			prs, err := service.ListPullRequestsContainingCommit(cmd.Context(), repo, args[0])
			if err != nil {
				return err
			}

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "pull_requests": prs})
			}

			if len(prs) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No pull requests containing commit found"))
				return nil
			}

			rows := make([][]string, len(prs))
			for i, pr := range prs {
				idStr := fmt.Sprintf("#%d", pr.ID)
				rows[i] = []string{style.Secondary.Render(idStr), pr.Title, pr.State}
			}
			style.WriteTable(cmd.OutOrStdout(), rows)

			return nil
		},
	}
	commitCmd.AddCommand(prsCmd)

	return commitCmd
}
