package prcmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/paging"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/prsel"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
)

func newPullRequestCommitsCommand(deps Dependencies, repository *string) *cobra.Command {
	var commitsPaging paging.Options
	var commitsStart int
	commitsCmd := &cobra.Command{
		Use:   "commits <pr-id>",
		Short: "List the commits in a pull request",
		Example: `  # The commits a pull request would merge
  bb pr commits 42 --repo PROJ/repo`,
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

			commits, err := service.ListCommits(cmd.Context(), repo, target.PullRequestID, pullrequestservice.PageOptions{MaxResults: commitsPaging.ServiceLimit(), Start: commitsStart})
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSONList(cmd.OutOrStdout(), PullRequestCommits{Repository: repositoryOf(repo), PullRequestID: target.PullRequestID, Commits: commitsFrom(commits)}, paging.LimitReached(commitsPaging, len(commits)))
			}

			if len(commits) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No commits found")
				return nil
			}
			for _, commit := range commits {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", shortCommitID(commit), firstMessageLine(commit.Message))
			}
			paging.Hint(cmd.ErrOrStderr(), commitsPaging, len(commits))
			return nil
		},
	}
	commitsPaging.Register(commitsCmd, 25)
	paging.RegisterStart(commitsCmd, &commitsStart)
	return commitsCmd
}
