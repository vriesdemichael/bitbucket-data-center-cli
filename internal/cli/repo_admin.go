package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/style"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
	reposervice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/repository"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/transport/httpclient"
)

func newRepoAdminCommand(options *rootOptions) *cobra.Command {
	var repositorySelector string

	repoAdminCmd := &cobra.Command{
		Use:   "admin",
		Short: "Repository administration commands (create/fork/update/delete)",
	}

	repoAdminCmd.PersistentFlags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug (defaults to BITBUCKET_PROJECT_KEY + BITBUCKET_REPO_SLUG)")

	var createProject string
	var createName string
	var createDesc string
	var createForkable bool
	var createDefaultBranch string
	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			if strings.TrimSpace(createProject) == "" {
				return fmt.Errorf("project key is required")
			}

			service := reposervice.NewAdminService(client)
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckProjectWrite(cmd.Context(), createProject); err != nil {
					return err
				}

				repoQueryService := reposervice.NewService(httpclient.NewFromConfig(cfg))
				existing, err := repoQueryService.ListByProject(cmd.Context(), createProject, reposervice.ListOptions{Limit: 200, Name: createName})
				if err != nil {
					return err
				}

				predicted := "create"
				reason := "repository will be created"
				for _, repo := range existing {
					if strings.EqualFold(strings.TrimSpace(repo.Name), strings.TrimSpace(createName)) {
						predicted = "conflict"
						reason = "repository with the same name already exists in project"
						break
					}
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "repo.admin.create",
						Target:          map[string]any{"project": createProject, "name": createName, "default_branch": createDefaultBranch},
						Action:          "create",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"project repositories list (name filtered)"},
						BlockingReasons: func() []string {
							if predicted == "conflict" {
								return []string{"repository already exists"}
							}
							return nil
						}(),
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1},
				}
				if predicted == "create" {
					preview.Summary.CreateCount = 1
				} else {
					preview.Summary.UnknownCount = 1
				}

				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}

			created, err := service.Create(cmd.Context(), createProject, reposervice.CreateInput{
				Name:          createName,
				Description:   createDesc,
				Forkable:      createForkable,
				DefaultBranch: createDefaultBranch,
			})
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"repository": created})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Success.Render("Created repository"), style.Resource.Render(createProject+"/"+safeString(created.Name)))
			return nil
		},
	}
	createCmd.Flags().StringVar(&createProject, "project", "", "Project key")
	createCmd.Flags().StringVar(&createName, "name", "", "Repository name")
	createCmd.Flags().StringVar(&createDesc, "description", "", "Repository description")
	createCmd.Flags().BoolVar(&createForkable, "forkable", true, "Repository forkable")
	createCmd.Flags().StringVar(&createDefaultBranch, "default-branch", "", "Repository default branch")
	_ = createCmd.MarkFlagRequired("project")
	_ = createCmd.MarkFlagRequired("name")
	repoAdminCmd.AddCommand(createCmd)

	var forkName string
	var forkProject string
	forkCmd := &cobra.Command{
		Use:   "fork",
		Short: "Fork a repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repoRef, err := resolveRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			repo := reposervice.RepositoryRef{ProjectKey: repoRef.ProjectKey, Slug: repoRef.Slug}
			service := reposervice.NewAdminService(client)
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOREAD); err != nil {
					return err
				}
				if forkProject != "" {
					if err := checker.CheckProjectWrite(cmd.Context(), forkProject); err != nil {
						return err
					}
				}

				predicted := "create"
				reason := "repository fork will be created"
				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityPartial,
					Items: []dryRunItem{{
						Intent:          "repo.admin.fork",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "name": forkName, "project": forkProject},
						Action:          "create",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityPartial,
						RequiredState:   []string{"source repository reference"},
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1, CreateCount: 1},
				}
				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}

			forked, err := service.Fork(cmd.Context(), repo, reposervice.ForkInput{
				Name:    forkName,
				Project: forkProject,
			})
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"repository": forked})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Success.Render("Forked repository to"), style.Resource.Render(safeString(forked.Name)))
			return nil
		},
	}
	forkCmd.Flags().StringVar(&forkName, "name", "", "Name of the new fork")
	forkCmd.Flags().StringVar(&forkProject, "project", "", "Project key of the new fork")
	repoAdminCmd.AddCommand(forkCmd)

	var updateName string
	var updateDesc string
	var updateDefaultBranch string
	updateCmd := &cobra.Command{
		Use:   "update",
		Short: "Update repository metadata",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repoRef, err := resolveRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			repo := reposervice.RepositoryRef{ProjectKey: repoRef.ProjectKey, Slug: repoRef.Slug}
			service := reposervice.NewAdminService(client)
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOADMIN); err != nil {
					return err
				}

				predicted := "update"
				reason := "repository metadata will be updated"
				if strings.TrimSpace(updateName) == "" && strings.TrimSpace(updateDesc) == "" && strings.TrimSpace(updateDefaultBranch) == "" {
					predicted = "no-op"
					reason = "no update fields provided"
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityPartial,
					Items: []dryRunItem{{
						Intent:          "repo.admin.update",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "name": updateName, "description": updateDesc, "default_branch": updateDefaultBranch},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityPartial,
						RequiredState:   []string{"repository reference"},
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1},
				}
				if predicted == "update" {
					preview.Summary.UpdateCount = 1
				} else {
					preview.Summary.NoopCount = 1
				}
				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}

			updated, err := service.Update(cmd.Context(), repo, reposervice.UpdateInput{
				Name:          updateName,
				Description:   updateDesc,
				DefaultBranch: updateDefaultBranch,
			})
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"repository": updated})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Updated.Render("Updated repository"), style.Resource.Render(safeString(updated.Name)))
			return nil
		},
	}
	updateCmd.Flags().StringVar(&updateName, "name", "", "Repository name")
	updateCmd.Flags().StringVar(&updateDesc, "description", "", "Repository description")
	updateCmd.Flags().StringVar(&updateDefaultBranch, "default-branch", "", "Repository default branch")
	repoAdminCmd.AddCommand(updateCmd)

	deleteCmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete a repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repoRef, err := resolveRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			repo := reposervice.RepositoryRef{ProjectKey: repoRef.ProjectKey, Slug: repoRef.Slug}
			service := reposervice.NewAdminService(client)
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOADMIN); err != nil {
					return err
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityPartial,
					Items: []dryRunItem{{
						Intent:          "repo.admin.delete",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug)},
						Action:          "delete",
						PredictedAction: "delete",
						Supported:       true,
						Reason:          "repository delete will be attempted",
						Confidence:      capabilityPartial,
						RequiredState:   []string{"repository reference"},
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1, DeleteCount: 1},
				}
				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}

			if err := service.Delete(cmd.Context(), repo); err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]string{"status": "ok", "repository": repoRef.ProjectKey + "/" + repoRef.Slug})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Deleted.Render("Deleted repository"), style.Resource.Render(repoRef.ProjectKey+"/"+repoRef.Slug))
			return nil
		},
	}
	repoAdminCmd.AddCommand(deleteCmd)

	return repoAdminCmd
}
