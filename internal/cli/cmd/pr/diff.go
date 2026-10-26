package prcmd

import (
	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/diffoutput"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/prsel"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	diffservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/diff"
)

func newPullRequestDiffAlias(deps Dependencies, repositorySelector *string) *cobra.Command {
	var patch bool
	var stat bool
	var nameOnly bool

	command := &cobra.Command{
		Use:   "diff <pr-id>",
		Short: "Diff a pull request (alias for bb diff pr)",
		Example: `  # The diff of a pull request
  bb pr diff 42 --repo PROJ/repo

  # Only the names of the files it changes
  bb pr diff 42 --repo PROJ/repo --name-only

  # As a patch, saved to a file
  bb pr diff 42 --repo PROJ/repo --patch > pr-42.patch`,
		Long: "Diff a pull request.\n\nAlias for bb diff pr, which is where the command reference documents it.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			target, err := prsel.Resolve(cmd.Context(), args[0], *repositorySelector, cfg, nil)
			if err != nil {
				return err
			}
			repo := diffservice.RepositoryRef{ProjectKey: target.ProjectKey, Slug: target.RepoSlug}

			service := diffservice.NewService(client)
			outputMode, err := diffoutput.ResolveOutputMode(patch, stat, nameOnly)
			if err != nil {
				return err
			}

			diffed, err := service.DiffPR(cmd.Context(), diffservice.DiffPRInput{
				Repository:    repo,
				PullRequestID: target.PullRequestID,
				Output:        outputMode,
			})
			if err != nil {
				return err
			}

			return diffoutput.Write(cmd.OutOrStdout(), deps.JSONEnabled(), result.Repository{ProjectKey: repo.ProjectKey, Slug: repo.Slug}, outputMode, diffed, deps.WriteJSON)
		},
	}

	command.Flags().BoolVar(&patch, "patch", false, "Output unified patch stream")
	command.Flags().BoolVar(&stat, "stat", false, "Output structured diff stats")
	command.Flags().BoolVar(&nameOnly, "name-only", false, "Output only changed file names")

	return command
}
