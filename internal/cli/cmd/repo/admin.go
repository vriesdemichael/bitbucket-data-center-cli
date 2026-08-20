package repocmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/dryrunpreview"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/style"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
	reposervice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/repository"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/transport/httpclient"
)

func newRepoCreateCommand(deps Dependencies, isAlias bool) *cobra.Command {
	var createProject string
	var createName string
	var createDesc string
	var createForkable bool
	var createDefaultBranch string

	shortDesc := "Create a new repository"
	longDesc := "Create a new repository.\n\nAlso available as bb repo admin create."
	if isAlias {
		longDesc = "Create a new repository.\n\nAlias for bb repo create."
	}

	createCmd := &cobra.Command{
		Use:   "create",
		Short: shortDesc,
		Long:  longDesc,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			if strings.TrimSpace(createProject) == "" {
				return fmt.Errorf("project key is required")
			}

			service := reposervice.NewAdminService(client)
			if deps.DryRunEnabled() {
				if deps.PermissionChecker != nil {
					checker := deps.PermissionChecker(client)
					if checker != nil {
						if err := checker.CheckProjectWrite(cmd.Context(), createProject); err != nil {
							return err
						}
					}
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

				intent := "repo.create"
				if isAlias {
					intent = "repo.admin.create"
				}

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          intent,
						Target:          map[string]any{"project": createProject, "name": createName, "default_branch": createDefaultBranch},
						Action:          "create",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      dryrunpreview.CapabilityFull,
						RequiredState:   []string{"project repositories list (name filtered)"},
						BlockingReasons: func() []string {
							if predicted == "conflict" {
								return []string{"repository already exists"}
							}
							return nil
						}(),
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1},
				}
				if predicted == "create" {
					preview.Summary.CreateCount = 1
				} else {
					preview.Summary.UnknownCount = 1
				}

				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
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

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]any{"repository": created})
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
	return createCmd
}

func newRepoForkCommand(deps Dependencies, repositorySelector *string, isAlias bool) *cobra.Command {
	var forkName string
	var forkProject string
	var localRepoSelector string

	shortDesc := "Fork a repository"
	longDesc := "Fork a repository.\n\nAlso available as bb repo admin fork."
	if isAlias {
		longDesc = "Fork a repository.\n\nAlias for bb repo fork."
	}

	forkCmd := &cobra.Command{
		Use:   "fork",
		Short: shortDesc,
		Long:  longDesc,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			selector := localRepoSelector
			if repositorySelector != nil && *repositorySelector != "" {
				selector = *repositorySelector
			}
			repoRef, err := resolveRepoReference(selector, cfg)
			if err != nil {
				return err
			}

			repo := reposervice.RepositoryRef{ProjectKey: repoRef.ProjectKey, Slug: repoRef.Slug}
			service := reposervice.NewAdminService(client)
			if deps.DryRunEnabled() {
				if deps.PermissionChecker != nil {
					checker := deps.PermissionChecker(client)
					if checker != nil {
						if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOREAD); err != nil {
							return err
						}
						if forkProject != "" {
							if err := checker.CheckProjectWrite(cmd.Context(), forkProject); err != nil {
								return err
							}
						}
					}
				}

				predicted := "create"
				reason := "repository fork will be created"
				intent := "repo.fork"
				if isAlias {
					intent = "repo.admin.fork"
				}

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityPartial,
					Items: []dryrunpreview.Item{{
						Intent:          intent,
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "name": forkName, "project": forkProject},
						Action:          "create",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      dryrunpreview.CapabilityPartial,
						RequiredState:   []string{"source repository reference"},
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1, CreateCount: 1},
				}
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			forked, err := service.Fork(cmd.Context(), repo, reposervice.ForkInput{
				Name:    forkName,
				Project: forkProject,
			})
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]any{"repository": forked})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Success.Render("Forked repository to"), style.Resource.Render(safeString(forked.Name)))
			return nil
		},
	}
	forkCmd.Flags().StringVar(&forkName, "name", "", "Name of the new fork")
	forkCmd.Flags().StringVar(&forkProject, "project", "", "Project key of the new fork")
	if repositorySelector == nil {
		forkCmd.Flags().StringVar(&localRepoSelector, "repo", "", "Repository as PROJECT/slug (defaults to BITBUCKET_PROJECT_KEY + BITBUCKET_REPO_SLUG)")
	}
	return forkCmd
}

func newRepoDeleteCommand(deps Dependencies, repositorySelector *string, isAlias bool) *cobra.Command {
	var localRepoSelector string

	shortDesc := "Delete a repository"
	longDesc := "Delete a repository.\n\nAlso available as bb repo admin delete."
	if isAlias {
		longDesc = "Delete a repository.\n\nAlias for bb repo delete."
	}

	deleteCmd := &cobra.Command{
		Use:   "delete",
		Short: shortDesc,
		Long:  longDesc,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			selector := localRepoSelector
			if repositorySelector != nil && *repositorySelector != "" {
				selector = *repositorySelector
			}
			repoRef, err := resolveRepoReference(selector, cfg)
			if err != nil {
				return err
			}

			repo := reposervice.RepositoryRef{ProjectKey: repoRef.ProjectKey, Slug: repoRef.Slug}
			service := reposervice.NewAdminService(client)
			if deps.DryRunEnabled() {
				if deps.PermissionChecker != nil {
					checker := deps.PermissionChecker(client)
					if checker != nil {
						if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOADMIN); err != nil {
							return err
						}
					}
				}

				intent := "repo.delete"
				if isAlias {
					intent = "repo.admin.delete"
				}

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityPartial,
					Items: []dryrunpreview.Item{{
						Intent:          intent,
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug)},
						Action:          "delete",
						PredictedAction: "delete",
						Supported:       true,
						Reason:          "repository delete will be attempted",
						Confidence:      dryrunpreview.CapabilityPartial,
						RequiredState:   []string{"repository reference"},
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1, DeleteCount: 1},
				}
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			if err := service.Delete(cmd.Context(), repo); err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]string{"status": "ok", "repository": repoRef.ProjectKey + "/" + repoRef.Slug})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Deleted.Render("Deleted repository"), style.Resource.Render(repoRef.ProjectKey+"/"+repoRef.Slug))
			return nil
		},
	}
	if repositorySelector == nil {
		deleteCmd.Flags().StringVar(&localRepoSelector, "repo", "", "Repository as PROJECT/slug (defaults to BITBUCKET_PROJECT_KEY + BITBUCKET_REPO_SLUG)")
	}
	return deleteCmd
}

func newRepoAdminCommand(deps Dependencies) *cobra.Command {
	var repositorySelector string

	repoAdminCmd := &cobra.Command{
		Use:   "admin",
		Short: "Repository administration commands (create/fork/update/delete)",
	}

	repoAdminCmd.PersistentFlags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug (defaults to BITBUCKET_PROJECT_KEY + BITBUCKET_REPO_SLUG)")

	repoAdminCmd.AddCommand(newRepoCreateCommand(deps, true))
	repoAdminCmd.AddCommand(newRepoForkCommand(deps, &repositorySelector, true))

	var updateName string
	var updateDesc string
	var updateDefaultBranch string
	updateCmd := &cobra.Command{
		Use:   "update",
		Short: "Update repository metadata",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			repoRef, err := resolveRepoReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			repo := reposervice.RepositoryRef{ProjectKey: repoRef.ProjectKey, Slug: repoRef.Slug}
			service := reposervice.NewAdminService(client)
			if deps.DryRunEnabled() {
				if deps.PermissionChecker != nil {
					checker := deps.PermissionChecker(client)
					if checker != nil {
						if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOADMIN); err != nil {
							return err
						}
					}
				}

				predicted := "update"
				reason := "repository metadata will be updated"
				if strings.TrimSpace(updateName) == "" && strings.TrimSpace(updateDesc) == "" && strings.TrimSpace(updateDefaultBranch) == "" {
					predicted = "no-op"
					reason = "no update fields provided"
				}

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityPartial,
					Items: []dryrunpreview.Item{{
						Intent:          "repo.admin.update",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "name": updateName, "description": updateDesc, "default_branch": updateDefaultBranch},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      dryrunpreview.CapabilityPartial,
						RequiredState:   []string{"repository reference"},
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1},
				}
				if predicted == "update" {
					preview.Summary.UpdateCount = 1
				} else {
					preview.Summary.NoopCount = 1
				}
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			updated, err := service.Update(cmd.Context(), repo, reposervice.UpdateInput{
				Name:          updateName,
				Description:   updateDesc,
				DefaultBranch: updateDefaultBranch,
			})
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]any{"repository": updated})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Updated.Render("Updated repository"), style.Resource.Render(safeString(updated.Name)))
			return nil
		},
	}
	updateCmd.Flags().StringVar(&updateName, "name", "", "Repository name")
	updateCmd.Flags().StringVar(&updateDesc, "description", "", "Repository description")
	updateCmd.Flags().StringVar(&updateDefaultBranch, "default-branch", "", "Repository default branch")
	repoAdminCmd.AddCommand(updateCmd)

	repoAdminCmd.AddCommand(newRepoDeleteCommand(deps, &repositorySelector, true))

	return repoAdminCmd
}
