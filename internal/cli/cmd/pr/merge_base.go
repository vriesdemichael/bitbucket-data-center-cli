package prcmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/prsel"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
)

func newPullRequestMergeBaseCommand(deps Dependencies, repository *string) *cobra.Command {
	mergeBaseCmd := &cobra.Command{
		Use:   "merge-base <pr-id>",
		Short: "Show the common ancestor commit of a pull request's source and target branches",
		Example: `  # The commit a pull request's branches last had in common
  bb pr merge-base 42 --repo PROJ/repo`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := deps.LoadConfig()
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
			target, err := prsel.Resolve(cmd.Context(), args[0], *repository, cfg, service)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			commit, err := service.GetMergeBase(cmd.Context(), repo, target.PullRequestID)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), MergeBase{Repository: repositoryOf(repo), PullRequestID: target.PullRequestID, MergeBase: commitFrom(commit)})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", shortCommitID(commit), firstMessageLine(commit.Message))
			return nil
		},
	}
	return mergeBaseCmd
}
