package searchcmd

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/enumflag"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/paging"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/reposel"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/style"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	commitservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/commit"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
	repositoryservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/repository"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
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
				MaxResults: listPaging.ServiceLimit(),
				Start:      start,
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

			reported := result.RepositorySummariesFrom(repos)

			if deps.JSONEnabled() {
				return deps.WriteJSONList(cmd.OutOrStdout(), reported, paging.LimitReached(listPaging, len(reported)))
			}

			if len(reported) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No repositories found"))
				return nil
			}

			rows := make([][]string, len(reported))
			for i, repo := range reported {
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
				MaxResults: listPaging.ServiceLimit(),
				Start:      start,
				Path:       path,
				Since:      since,
				Until:      until,
				Merges:     merges,
			}

			commits, err := service.List(cmd.Context(), repo, opts)
			if err != nil {
				return err
			}

			reported := Commits{
				Repository: result.Repository{ProjectKey: repo.ProjectKey, Slug: repo.Slug},
				Commits:    result.CommitsFrom(commits),
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), reported)
			}

			if len(reported.Commits) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No commits found"))
				return nil
			}

			rows := make([][]string, len(reported.Commits))
			for i, commit := range reported.Commits {
				rows[i] = []string{style.Secondary.Render(commit.DisplayID), commit.Subject()}
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
	enumflag.Register(cmd.Flags(), &merges, "merges", "", mergeFilters, "Filter merge commits")

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
			// The repository pull-requests endpoint has no role parameter, so
			// Bitbucket drops one that is sent and answers with every open pull
			// request in the repository. Dropping the flag here instead was
			// honest in the help text and silent at the terminal: the caller
			// asked for their own and got everybody's, with nothing to say the
			// filter had not been applied (#540).
			if repositorySelector != "" && role != "" {
				return apperrors.New(apperrors.KindValidation,
					"--role cannot be combined with --repo: Bitbucket applies a role only on the dashboard, so drop --repo to use it", nil)
			}

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
					MaxResults: listPaging.ServiceLimit(),
					Start:      start,
					State:      state,
				}
				prs, err = service.List(cmd.Context(), repo, opts)
			} else {
				opts := pullrequestservice.DashboardListOptions{
					MaxResults: listPaging.ServiceLimit(),
					Start:      start,
					State:      state,
					Role:       role,
				}
				prs, err = service.ListDashboard(cmd.Context(), opts)
			}

			if err != nil {
				return err
			}

			reported := PullRequests{PullRequests: result.PullRequestsFrom(prs)}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), reported)
			}

			if len(reported.PullRequests) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No pull requests found"))
				return nil
			}

			rows := make([][]string, len(reported.PullRequests))
			for i, pr := range reported.PullRequests {
				repoStr := ""
				if pr.Repository.ProjectKey != "" {
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
	enumflag.Register(cmd.Flags(), &state, "state", "open", openapi.PullRequestStateFilters, "Filter by state")
	enumflag.Register(cmd.Flags(), &role, "role", "", participantRoles, "Filter by role; dashboard only, so it cannot be combined with --repo")

	return cmd
}

// mergeFilters are how a commit search treats merge commits, and
// participantRoles are the roles a pull request search can filter on. Both are
// sent lower-case; the service upper-cases the role on its way out, so either
// spelling works on the command line.
var (
	mergeFilters     = []string{"exclude", "include", "only"}
	participantRoles = []string{"author", "reviewer", "participant"}
)
