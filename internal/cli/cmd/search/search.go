package searchcmd

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
	pullrequestservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/pullrequest"
	repositoryservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/repository"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/transport/httpclient"
)

type Dependencies struct {
	JSONEnabled         func() bool
	LoadConfig          func() (config.AppConfig, error)
	LoadConfigAndClient func() (config.AppConfig, *openapigenerated.ClientWithResponses, error)
	WriteJSON           func(io.Writer, any) error
	WriteJSONList       func(io.Writer, any, bool) error
}

func (d Dependencies) withDefaults() Dependencies {
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
		d.WriteJSON = func(w io.Writer, v any) error {
			return jsonoutput.Write(w, v)
		}
	}
	if d.WriteJSONList == nil {
		d.WriteJSONList = func(w io.Writer, v any, limitReached bool) error {
			return jsonoutput.WriteList(w, v, limitReached)
		}
	}
	return d
}

func New(deps Dependencies) *cobra.Command {
	d := deps.withDefaults()

	searchCmd := &cobra.Command{
		Use:   "search",
		Short: "Search for repositories, commits, and pull requests",
	}

	searchCmd.AddCommand(newSearchReposCommand(d))
	searchCmd.AddCommand(newSearchCommitsCommand(d))
	searchCmd.AddCommand(newSearchPRsCommand(d))

	return searchCmd
}

func newSearchReposCommand(deps Dependencies) *cobra.Command {
	var listPaging paging.Options
	var start int
	var projectKey string

	cmd := &cobra.Command{
		Use:   "repos [name]",
		Short: "Search for repositories",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := deps.LoadConfig()
			if err != nil {
				return err
			}

			client := httpclient.NewFromConfig(cfg)
			service := repositoryservice.NewService(client)

			opts := repositoryservice.ListOptions{
				Limit: listPaging.ServiceLimit(),
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

			if deps.JSONEnabled() {
				return deps.WriteJSONList(cmd.OutOrStdout(), repos, paging.LimitReached(listPaging, len(repos)))
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

	listPaging.Register(cmd, 25)
	cmd.Flags().IntVar(&start, "start", 0, "Pagination start index")
	cmd.Flags().StringVar(&projectKey, "project", "", "Filter by project key")

	return cmd
}

func newSearchCommitsCommand(deps Dependencies) *cobra.Command {
	var listPaging paging.Options
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
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			projectKey, slug, err := reposel.Resolve(repositorySelector, cfg)
			if err != nil {
				return err
			}

			repo := commitservice.RepositoryRef{ProjectKey: projectKey, Slug: slug}
			service := commitservice.NewService(client)

			opts := commitservice.ListOptions{
				Limit:  listPaging.ServiceLimit(),
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

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "commits": commits})
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
	listPaging.Register(cmd, 25)
	cmd.Flags().IntVar(&start, "start", 0, "Pagination start index")
	cmd.Flags().StringVar(&path, "path", "", "Filter by file path")
	cmd.Flags().StringVar(&since, "since", "", "Commit ID or ref to search after (exclusive)")
	cmd.Flags().StringVar(&until, "until", "", "Commit ID or ref to search before (inclusive)")
	cmd.Flags().StringVar(&merges, "merges", "", "Filter merge commits (exclude, include, only)")

	_ = cmd.MarkFlagRequired("repo")

	return cmd
}

func newSearchPRsCommand(deps Dependencies) *cobra.Command {
	var listPaging paging.Options
	var start int
	var repositorySelector string
	var state string
	var role string

	cmd := &cobra.Command{
		Use:   "prs",
		Short: "Search for pull requests globally or within a repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := deps.LoadConfig()
			if err != nil {
				return err
			}

			client := httpclient.NewFromConfig(cfg)
			service := pullrequestservice.NewService(client)

			var prs []pullrequestservice.PullRequest

			if repositorySelector != "" {
				// Named apart from err on purpose. This was `projectKey, slug,
				// err := ...`, which shadowed the outer err for the rest of the
				// branch -- so service.List below assigned its failure to the
				// shadow, the check after the if/else read the outer one, and a
				// failed search returned an empty list and exit 0.
				projectKey, slug, resolveErr := reposel.Resolve(repositorySelector, cfg)
				if resolveErr != nil {
					return resolveErr
				}
				repo := pullrequestservice.RepositoryRef{ProjectKey: projectKey, Slug: slug}
				opts := pullrequestservice.ListOptions{
					Limit: listPaging.ServiceLimit(),
					Start: start,
					State: state,
				}
				prs, err = service.List(cmd.Context(), repo, opts)
			} else {
				opts := pullrequestservice.DashboardListOptions{
					Limit: listPaging.ServiceLimit(),
					Start: start,
					State: state,
					Role:  role,
				}
				prs, err = service.ListDashboard(cmd.Context(), opts)
			}

			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]any{"pull_requests": prs})
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
	listPaging.Register(cmd, 25)
	cmd.Flags().IntVar(&start, "start", 0, "Pagination start index")
	cmd.Flags().StringVar(&state, "state", "open", "Filter by state (open, closed, all)")
	cmd.Flags().StringVar(&role, "role", "", "Filter by role (author, reviewer, participant) - only applies when --repo is not used")

	return cmd
}

func safeString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
