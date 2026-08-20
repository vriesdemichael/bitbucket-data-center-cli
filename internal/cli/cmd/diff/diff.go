package diffcmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/prsel"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/reposel"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
	diffservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/diff"
)

type Dependencies struct {
	JSONEnabled         func() bool
	LoadConfig          func() (config.AppConfig, error)
	LoadConfigAndClient func() (config.AppConfig, *openapigenerated.ClientWithResponses, error)
	WriteJSON           func(io.Writer, any) error
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
	return d
}

func resolveDiffRepositoryReference(selector string, cfg config.AppConfig) (diffservice.RepositoryRef, error) {
	projectKey, slug, err := reposel.Resolve(selector, cfg)
	if err != nil {
		return diffservice.RepositoryRef{}, err
	}
	return diffservice.RepositoryRef{ProjectKey: projectKey, Slug: slug}, nil
}

func resolveDiffOutputMode(patch, stat, nameOnly bool) (diffservice.OutputKind, error) {
	selected := 0
	mode := diffservice.OutputKindRaw
	if patch {
		selected++
		mode = diffservice.OutputKindPatch
	}
	if stat {
		selected++
		mode = diffservice.OutputKindStat
	}
	if nameOnly {
		selected++
		mode = diffservice.OutputKindNameOnly
	}
	if selected > 1 {
		return "", apperrors.New(apperrors.KindValidation, "only one of --patch, --stat, or --name-only may be specified", nil)
	}
	return mode, nil
}

func writeDiffResult(writer io.Writer, asJSON bool, mode diffservice.OutputKind, result diffservice.Result, writeJSON func(io.Writer, any) error) error {
	if asJSON {
		switch mode {
		case diffservice.OutputKindNameOnly:
			return writeJSON(writer, map[string]any{"names": result.Names})
		case diffservice.OutputKindStat:
			return writeJSON(writer, map[string]any{"stats": result.Stats})
		default:
			return writeJSON(writer, map[string]any{"patch": result.Patch})
		}
	}

	switch mode {
	case diffservice.OutputKindNameOnly:
		for _, name := range result.Names {
			fmt.Fprintln(writer, name)
		}
		return nil
	case diffservice.OutputKindStat:
		return writeJSON(writer, result.Stats)
	default:
		fmt.Fprint(writer, result.Patch)
		if result.Patch != "" && !strings.HasSuffix(result.Patch, "\n") {
			fmt.Fprintln(writer)
		}
		return nil
	}
}

func New(deps Dependencies) *cobra.Command {
	d := deps.withDefaults()

	var repositorySelector string

	diffCmd := &cobra.Command{
		Use:   "diff",
		Short: "Diff and patch commands",
	}

	diffCmd.PersistentFlags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug (defaults to BITBUCKET_PROJECT_KEY + BITBUCKET_REPO_SLUG)")

	var refsPath string
	var refsPatch bool
	var refsStat bool
	var refsNameOnly bool

	refsCmd := &cobra.Command{
		Use:   "refs <from> <to>",
		Short: "Diff two refs or commits",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := d.LoadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveDiffRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := diffservice.NewService(client)
			outputMode, err := resolveDiffOutputMode(refsPatch, refsStat, refsNameOnly)
			if err != nil {
				return err
			}

			result, err := service.DiffRefs(cmd.Context(), diffservice.DiffRefsInput{
				Repository: repo,
				From:       args[0],
				To:         args[1],
				Path:       refsPath,
				Output:     outputMode,
			})
			if err != nil {
				return err
			}

			return writeDiffResult(cmd.OutOrStdout(), d.JSONEnabled(), outputMode, result, d.WriteJSON)
		},
	}
	refsCmd.Flags().StringVar(&refsPath, "path", "", "Optional file path for file-scoped diff")
	refsCmd.Flags().BoolVar(&refsPatch, "patch", false, "Output unified patch stream")
	refsCmd.Flags().BoolVar(&refsStat, "stat", false, "Output structured diff stats")
	refsCmd.Flags().BoolVar(&refsNameOnly, "name-only", false, "Output only changed file names")
	diffCmd.AddCommand(refsCmd)

	diffCmd.AddCommand(NewDiffPullRequestCommand(d, &repositorySelector))

	var commitPath string
	commitCmd := &cobra.Command{
		Use:   "commit <sha>",
		Short: "Diff a commit against its parent",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := d.LoadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveDiffRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := diffservice.NewService(client)
			result, err := service.DiffCommit(cmd.Context(), diffservice.DiffCommitInput{
				Repository: repo,
				CommitID:   args[0],
				Path:       commitPath,
			})
			if err != nil {
				return err
			}

			return writeDiffResult(cmd.OutOrStdout(), d.JSONEnabled(), diffservice.OutputKindRaw, result, d.WriteJSON)
		},
	}
	commitCmd.Flags().StringVar(&commitPath, "path", "", "Optional file path for file-scoped diff")
	diffCmd.AddCommand(commitCmd)

	return diffCmd
}

func NewDiffPullRequestCommand(deps Dependencies, repositorySelector *string) *cobra.Command {
	d := deps.withDefaults()

	var patch bool
	var stat bool
	var nameOnly bool

	command := &cobra.Command{
		Use:   "pr <id>",
		Short: "Diff a pull request",
		Long:  "Diff a pull request.\n\nAlso available as bb pr diff, which is the gh spelling.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := d.LoadConfigAndClient()
			if err != nil {
				return err
			}

			repoSelector := ""
			if repositorySelector != nil {
				repoSelector = *repositorySelector
			}
			target, err := prsel.Resolve(cmd.Context(), args[0], repoSelector, cfg, nil)
			if err != nil {
				return err
			}
			repo := diffservice.RepositoryRef{ProjectKey: target.ProjectKey, Slug: target.RepoSlug}

			service := diffservice.NewService(client)
			outputMode, err := resolveDiffOutputMode(patch, stat, nameOnly)
			if err != nil {
				return err
			}

			result, err := service.DiffPR(cmd.Context(), diffservice.DiffPRInput{
				Repository:    repo,
				PullRequestID: target.PullRequestID,
				Output:        outputMode,
			})
			if err != nil {
				return err
			}

			return writeDiffResult(cmd.OutOrStdout(), d.JSONEnabled(), outputMode, result, d.WriteJSON)
		},
	}

	command.Flags().BoolVar(&patch, "patch", false, "Output unified patch stream")
	command.Flags().BoolVar(&stat, "stat", false, "Output structured diff stats")
	command.Flags().BoolVar(&nameOnly, "name-only", false, "Output only changed file names")

	return command
}
