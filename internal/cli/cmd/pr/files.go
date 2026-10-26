package prcmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/paging"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/prsel"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
)

func newPullRequestFilesCommand(deps Dependencies, repository *string) *cobra.Command {
	var filesPaging paging.Options
	var filesStart int
	filesCmd := &cobra.Command{
		Use:     "files <pr-id>",
		Aliases: []string{"changes"},
		Short:   "List the files changed in a pull request",
		Example: `  # The files a pull request changes
  bb pr files 42 --repo PROJ/repo`,
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

			changes, err := service.ListChanges(cmd.Context(), repo, target.PullRequestID, pullrequestservice.PageOptions{MaxResults: filesPaging.ServiceLimit(), Start: filesStart})
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSONList(cmd.OutOrStdout(), PullRequestChanges{Repository: repositoryOf(repo), PullRequestID: target.PullRequestID, Changes: changesFrom(changes)}, paging.LimitReached(filesPaging, len(changes)))
			}

			if len(changes) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No changes found")
				return nil
			}
			for _, change := range changes {
				changeType := change.Type
				if changeType == "" {
					changeType = "MODIFY"
				}
				line := fmt.Sprintf("%s\t%s", changeType, change.Path)
				if change.SrcPath != "" && change.SrcPath != change.Path {
					line += fmt.Sprintf(" (from %s)", change.SrcPath)
				}
				fmt.Fprintln(cmd.OutOrStdout(), line)
			}
			paging.Hint(cmd.ErrOrStderr(), filesPaging, len(changes))
			return nil
		},
	}
	filesPaging.Register(filesCmd, 25)
	paging.RegisterStart(filesCmd, &filesStart)
	return filesCmd
}
