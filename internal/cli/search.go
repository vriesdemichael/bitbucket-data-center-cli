package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/style"
	commitservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/commit"
	pullrequestservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/pullrequest"
	repositoryservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/repository"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/transport/httpclient"
)

func newSearchCommand(options *rootOptions) *cobra.Command {
	searchCmd := &cobra.Command{
		Use:   "search",
		Short: "Search for repositories, commits, and pull requests",
	}

	searchCmd.AddCommand(newSearchReposCommand(options))
	searchCmd.AddCommand(newSearchCommitsCommand(options))
	searchCmd.AddCommand(newSearchPRsCommand(options))

	return searchCmd
}

func newSearchReposCommand(options *rootOptions) *cobra.Command {
	var limit int
	var start int
	var projectKey string

	cmd := &cobra.Command{
		Use:   "repos [name]",
		Short: "Search for repositories",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			client := httpclient.NewFromConfig(cfg)
			service := repositoryservice.NewService(client)

			opts := repositoryservice.ListOptions{
				Limit: limit,
				Start: start,
			}
			if len(args) > 0 {
				opts.Name = args[0]
			}
			var repos []repositoryservice.Repository
			if projectKey != "" {
				repos, err = service.ListByProject(cmd.Context(), projectKey, opts)
			} else {
				repos, err = service.List(cmd.Context(), opts)
			}
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), repos)
			}

			if len(repos) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No repositories found"))
				return nil
			}

			rows := make([][]string, len(repos))
			for i, repo := range repos {
				rows[i] = []string{style.Resource.Render(repo.ProjectKey + "/" + repo.Slug), repo.Name}
			}
			style.WriteTable(cmd.OutOrStdout(), rows)

			return nil
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 25, "Page size")
	cmd.Flags().IntVar(&start, "start", 0, "Pagination start index")
	cmd.Flags().StringVar(&projectKey, "project", "", "Filter by project key")

	return cmd
}

func newSearchCommitsCommand(options *rootOptions) *cobra.Command {
	var limit int
	var start int
	var repositorySelector string
	var path string
	var since string
	var until string
	var merges string

	cmd := &cobra.Command{
		Use:   "commits",
		Short: "Search for commits within a repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repoRef, err := resolveRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			repo := commitservice.RepositoryRef{ProjectKey: repoRef.ProjectKey, Slug: repoRef.Slug}
			service := commitservice.NewService(client)

			opts := commitservice.ListOptions{
				Limit:  limit,
				Start:  start,
				Path:   path,
				Since:  since,
				Until:  until,
				Merges: merges,
			}

			commits, err := service.List(cmd.Context(), repo, opts)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "commits": commits})
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

	cmd.Flags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug (required)")
	cmd.Flags().IntVar(&limit, "limit", 25, "Page size")
	cmd.Flags().IntVar(&start, "start", 0, "Pagination start index")
	cmd.Flags().StringVar(&path, "path", "", "Filter by file path")
	cmd.Flags().StringVar(&since, "since", "", "Commit ID or ref to search after (exclusive)")
	cmd.Flags().StringVar(&until, "until", "", "Commit ID or ref to search before (inclusive)")
	cmd.Flags().StringVar(&merges, "merges", "", "Filter merge commits (exclude, include, only)")

	_ = cmd.MarkFlagRequired("repo")

	return cmd
}

func newSearchPRsCommand(options *rootOptions) *cobra.Command {
	var limit int
	var start int
	var repositorySelector string
	var state string
	var role string

	cmd := &cobra.Command{
		Use:   "prs",
		Short: "Search for pull requests globally or within a repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			client := httpclient.NewFromConfig(cfg)
			service := pullrequestservice.NewService(client)

			var prs []pullrequestservice.PullRequest

			if repositorySelector != "" {
				repoRef, err := resolveRepositoryReference(repositorySelector, cfg)
				if err != nil {
					return err
				}
				repo := pullrequestservice.RepositoryRef{ProjectKey: repoRef.ProjectKey, Slug: repoRef.Slug}
				opts := pullrequestservice.ListOptions{
					Limit: limit,
					Start: start,
					State: state,
				}
				prs, err = service.List(cmd.Context(), repo, opts)
			} else {
				opts := pullrequestservice.DashboardListOptions{
					Limit: limit,
					Start: start,
					State: state,
					Role:  role,
				}
				prs, err = service.ListDashboard(cmd.Context(), opts)
			}

			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"pull_requests": prs})
			}

			if len(prs) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No pull requests found"))
				return nil
			}

			rows := make([][]string, len(prs))
			for i, pr := range prs {
				repoStr := ""
				if pr.Repository != nil && pr.Repository.ProjectKey != "" {
					repoStr = fmt.Sprintf("[%s/%s] ", pr.Repository.ProjectKey, pr.Repository.Slug)
				}
				rows[i] = []string{style.Resource.Render(fmt.Sprintf("%s#%d", repoStr, pr.ID)), style.ActionStyle(pr.State).Render(pr.State), pr.Title}
			}
			style.WriteTable(cmd.OutOrStdout(), rows)

			return nil
		},
	}

	cmd.Flags().StringVar(&repositorySelector, "repo", "", "Optional repository as PROJECT/slug to scope search")
	cmd.Flags().IntVar(&limit, "limit", 25, "Page size")
	cmd.Flags().IntVar(&start, "start", 0, "Pagination start index")
	cmd.Flags().StringVar(&state, "state", "open", "Filter by state (open, closed, all)")
	cmd.Flags().StringVar(&role, "role", "", "Filter by role (author, reviewer, participant) - only applies when --repo is not used")

	return cmd
}
