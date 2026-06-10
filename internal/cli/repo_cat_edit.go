package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
	browseservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/browse"
)

func newRepoCatCommand(options *rootOptions) *cobra.Command {
	var repositorySelector string
	var at string

	cmd := &cobra.Command{
		Use:   "cat <path>",
		Short: "Output the raw content of a file over REST",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repoRef, err := resolveRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			repo := browseservice.RepositoryRef{ProjectKey: repoRef.ProjectKey, Slug: repoRef.Slug}
			service := browseservice.NewService(client)

			content, err := service.Raw(cmd.Context(), repo, args[0], at)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{
					"content": string(content),
					"path":    args[0],
					"at":      at,
				})
			}

			_, _ = cmd.OutOrStdout().Write(content)
			return nil
		},
	}

	cmd.Flags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug")
	cmd.Flags().StringVar(&at, "at", "", "Commit ID or ref to cat")

	return cmd
}

func newRepoEditCommand(options *rootOptions) *cobra.Command {
	var repositorySelector string
	var branch string
	var message string
	var content string
	var sourceBranch string
	var sourceCommitId string

	cmd := &cobra.Command{
		Use:   "edit <path>",
		Short: "Edit a file's content over REST",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repoRef, err := resolveRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			repo := browseservice.RepositoryRef{ProjectKey: repoRef.ProjectKey, Slug: repoRef.Slug}
			service := browseservice.NewService(client)

			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOWRITE); err != nil {
					return err
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityPartial,
					Items: []dryRunItem{{
						Intent: "repo.edit",
						Target: map[string]any{
							"repository":     fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug),
							"path":           args[0],
							"branch":         branch,
							"message":        message,
							"sourceBranch":   sourceBranch,
							"sourceCommitId": sourceCommitId,
						},
						Action:          "update",
						PredictedAction: "update",
						Supported:       true,
						Reason:          "file will be edited",
						Confidence:      capabilityPartial,
						RequiredState:   []string{"repository write access"},
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1, UpdateCount: 1},
				}
				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}

			commit, err := service.Edit(cmd.Context(), repo, args[0], browseservice.EditInput{
				Branch:         branch,
				Message:        message,
				Content:        content,
				SourceBranch:   sourceBranch,
				SourceCommitId: sourceCommitId,
			})
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), commit)
			}

			commitID := ""
			if commit.Id != nil {
				commitID = *commit.Id
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Successfully edited %s in commit %s\n", args[0], commitID)
			return nil
		},
	}

	cmd.Flags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug")
	cmd.Flags().StringVar(&branch, "branch", "", "The branch on which the file should be modified or created")
	cmd.Flags().StringVar(&message, "message", "", "Commit message")
	cmd.Flags().StringVar(&content, "content", "", "The full content of the file")
	cmd.Flags().StringVar(&sourceBranch, "source-branch", "", "Starting point branch")
	cmd.Flags().StringVar(&sourceCommitId, "source-commit", "", "Commit ID before editing")

	return cmd
}
