package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/style"
	diffservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/diff"
)

func newRepoCompareCommand(options *rootOptions) *cobra.Command {
	var repositorySelector string
	var diff bool

	cmd := &cobra.Command{
		Use:   "compare <from> <to>",
		Short: "Compare commits or branches",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repoRef, err := resolveRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			repo := diffservice.RepositoryRef{ProjectKey: repoRef.ProjectKey, Slug: repoRef.Slug}
			service := diffservice.NewService(client)

			from := args[0]
			to := args[1]

			if diff {
				diffResult, err := service.CompareDiff(cmd.Context(), repo, from, to)
				if err != nil {
					return err
				}

				if options.JSON {
					return writeJSON(cmd.OutOrStdout(), diffResult)
				}

				formatted := diffservice.FormatRestDiff(diffResult)
				_, _ = cmd.OutOrStdout().Write([]byte(formatted))
				return nil
			}

			changes, err := service.CompareChanges(cmd.Context(), repo, from, to, 1000)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{
					"from":    from,
					"to":      to,
					"changes": changes,
				})
			}

			if len(changes) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No changes found"))
				return nil
			}

			rows := make([][]string, len(changes))
			for i, change := range changes {
				pathStr := ""
				if change.Path != nil && change.Path.Components != nil {
					pathStr = strings.Join(*change.Path.Components, "/")
				}
				changeType := ""
				if change.Type != nil {
					changeType = string(*change.Type)
				}
				rows[i] = []string{style.Resource.Render(pathStr), changeType}
			}
			style.WriteTable(cmd.OutOrStdout(), rows)

			return nil
		},
	}

	cmd.Flags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug")
	cmd.Flags().BoolVar(&diff, "diff", false, "Show the unified diff of the changes")

	return cmd
}
