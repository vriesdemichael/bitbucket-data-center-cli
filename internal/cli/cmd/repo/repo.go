package repocmd

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/paging"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/reposel"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/style"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/git"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/repository"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
)

type PermissionChecker interface {
	CheckRepoPermission(ctx context.Context, projectKey, repoSlug string, permission openapigenerated.GetRepositories1ParamsPermission) error
	CheckProjectAdmin(ctx context.Context, projectKey string) error
	CheckProjectWrite(ctx context.Context, projectKey string) error
	InspectRepoPermissions(ctx context.Context, projectKey, repoSlug string) (map[string]bool, error)
}

type Dependencies struct {
	JSONEnabled         func() bool
	DryRunEnabled       func() bool
	LoadConfig          func() (config.AppConfig, error)
	LoadConfigAndClient func() (config.AppConfig, *openapigenerated.ClientWithResponses, error)
	WriteJSON           func(io.Writer, any) error
	WriteJSONList       func(io.Writer, any, bool) error
	PermissionChecker   func(*openapigenerated.ClientWithResponses) PermissionChecker
	// RepositoryWasInferred reports that --repo was filled in from the git
	// remote rather than named by the caller.
	//
	// Only the destructive commands need the difference. Inference marks the
	// flag Changed so every command can resolve a target, which made "the
	// caller named it" and "we guessed it" indistinguishable -- and that is
	// what makes --yes dangerous on a repository you merely happen to be
	// standing in (#472).
	//
	// Optional: nil reads as not inferred, which is right for a caller that
	// does no inference at all.
	RepositoryWasInferred func() bool

	// GitBackend builds the git backend a clone runs through, and
	// CanPromptForCloneLogin decides whether a clone may ask for credentials.
	//
	// Fields rather than the package-level variables they replace. Those were
	// swapped and restored by 24 tests, which is why every test in this package
	// had to run on its own: two of them substituting a backend at the same
	// time would each see the other's. Passing them in is what lets those tests
	// run together, and it costs the production path nothing -- withDefaults
	// supplies exactly what the variables held.
	GitBackend             func() git.Backend
	CanPromptForCloneLogin func(io.Reader, io.Writer) bool
}

func (deps *Dependencies) withDefaults() Dependencies {
	d := *deps
	if d.JSONEnabled == nil {
		d.JSONEnabled = func() bool { return false }
	}
	if d.DryRunEnabled == nil {
		d.DryRunEnabled = func() bool { return false }
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
	if d.GitBackend == nil {
		d.GitBackend = gitBackendFactory
	}
	if d.CanPromptForCloneLogin == nil {
		d.CanPromptForCloneLogin = canPromptForCloneLogin
	}
	return d
}

func resolveRepoReference(selector string, cfg config.AppConfig) (repository.RepositoryRef, error) {
	projectKey, slug, err := reposel.Resolve(selector, cfg)
	if err != nil {
		return repository.RepositoryRef{}, err
	}
	return repository.RepositoryRef{ProjectKey: projectKey, Slug: slug}, nil
}

func New(deps Dependencies) *cobra.Command {
	d := deps.withDefaults()

	repoCmd := &cobra.Command{
		Use:   "repo",
		Short: "Repository commands",
	}

	var listPaging paging.Options
	var start int
	var projectKey string

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List repositories",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := d.LoadConfig()
			if err != nil {
				return err
			}

			client := httpclient.NewFromConfig(cfg)
			service := repository.NewService(client)

			listOptions := repository.ListOptions{MaxResults: listPaging.ServiceLimit(), Start: start}

			var repos []repository.Repository
			if projectKey != "" {
				repos, err = service.ListByProject(cmd.Context(), projectKey, listOptions)
			} else {
				repos, err = service.List(cmd.Context(), listOptions)
			}
			if err != nil {
				return err
			}

			if d.JSONEnabled() {
				return d.WriteJSONList(cmd.OutOrStdout(), result.RepositorySummariesFrom(repos), paging.LimitReached(listPaging, len(repos)))
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
	listCmd.Flags().StringVar(&projectKey, "project", "", "Filter by project key")
	// On the leaf, not the parent: every other repoCmd subcommand advertised
	// these and ignored them (#476).
	listPaging.Register(listCmd, 25)
	listCmd.Flags().IntVar(&start, "start", 0, "Start offset for list operations")
	repoCmd.AddCommand(listCmd)
	repoCmd.AddCommand(newRepoCreateCommand(d, false))
	repoCmd.AddCommand(newRepoForkCommand(d, nil, false))
	repoCmd.AddCommand(newRepoDeleteCommand(d, nil, false))

	repoCmd.AddCommand(newRepoSettingsCommand(d))
	repoCmd.AddCommand(newRepoCommentCommand(d))
	repoCmd.AddCommand(newRepoBrowseCommand(d))
	repoCmd.AddCommand(newRepoCloneCommand(d))
	repoCmd.AddCommand(newRepoAdminCommand(d))
	repoCmd.AddCommand(newRepoPermissionsCommand(d))
	repoCmd.AddCommand(newRepoLabelCommand(d))
	repoCmd.AddCommand(newRepoWatchCommand(d))
	repoCmd.AddCommand(newRepoUnwatchCommand(d))
	repoCmd.AddCommand(newRepoDefaultTaskCommand(d))
	repoCmd.AddCommand(newRepoSshKeyCommand(d))
	repoCmd.AddCommand(newRepoCatCommand(d))
	repoCmd.AddCommand(newRepoEditCommand(d))
	repoCmd.AddCommand(newRepoCompareCommand(d))
	repoCmd.AddCommand(newRepoArchiveCommand(d))
	repoCmd.AddCommand(newRepoSyncCommand(d))

	return repoCmd
}

func NewClone(deps Dependencies) *cobra.Command {
	return newCloneCommand(deps.withDefaults())
}
