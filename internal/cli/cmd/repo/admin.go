package repocmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/dryrunpreview"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/prompt"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/style"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
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
				existing, err := repoQueryService.ListByProject(cmd.Context(), createProject, reposervice.ListOptions{MaxResults: 200, Name: createName})
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
				return deps.WriteJSON(cmd.OutOrStdout(), SingleRepository{Repository: result.RepositoryDetailFrom(created)})
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
						if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapi.RepoRead); err != nil {
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
				return deps.WriteJSON(cmd.OutOrStdout(), SingleRepository{Repository: result.RepositoryDetailFrom(forked)})
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
	var confirmed bool

	shortDesc := "Delete a repository"
	longDesc := "Delete a repository.\n\nName the repository as a PROJECT/slug argument or with --repo. Without one it is\n" +
		"inferred from the git remote, and --yes does not apply to an inferred target:\n" +
		"a safety flag that works on a target you did not name is not one. At a terminal\n" +
		"the confirmation names what will be deleted.\n\nAlso available as bb repo admin delete."
	if isAlias {
		longDesc = "Delete a repository.\n\nAlias for bb repo delete."
	}

	deleteCmd := &cobra.Command{
		Use:   "delete [PROJECT/slug]",
		Short: shortDesc,
		Long:  longDesc,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			selector := localRepoSelector
			if repositorySelector != nil && *repositorySelector != "" {
				selector = *repositorySelector
			}

			// A named target is the one the caller wrote down. The environment
			// carries an inferred one too -- applyInferredRepositoryContext
			// fills BITBUCKET_PROJECT_KEY and BITBUCKET_REPO_SLUG from the git
			// remote -- and that is indistinguishable from an operator setting
			// them, so neither counts as naming the repository here (ADR-073).
			// Changed alone is not "the caller named it": inference sets the
			// flag and marks it Changed so every command can resolve a target,
			// which silently made an inferred repository count as explicit and
			// let --yes apply to the one you were standing in. That is the
			// hazard #472 reports, reintroduced by the fix for it.
			targetExplicit := cmd.Flags().Changed("repo") &&
				!(deps.RepositoryWasInferred != nil && deps.RepositoryWasInferred())
			if len(args) == 1 {
				// Two named targets that disagree is not something to resolve
				// by precedence. The caller believes one of them is about to be
				// deleted and the other is; for an irreversible command that is
				// worth stopping for, even though every other command here
				// silently prefers the argument.
				if targetExplicit && strings.TrimSpace(args[0]) != strings.TrimSpace(selector) {
					return apperrors.New(
						apperrors.KindValidation,
						fmt.Sprintf("two different repositories named: %q as an argument and %q with --repo; pass one", args[0], selector),
						nil,
					)
				}
				selector = args[0]
				targetExplicit = true
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
						if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapi.RepoAdmin); err != nil {
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

			fullName := repoRef.ProjectKey + "/" + repoRef.Slug
			request := prompt.RequestFor(cmd, deps.JSONEnabled())
			request.Yes = confirmed
			request.TargetExplicit = targetExplicit
			request.Resource = fullName
			request.Flag = "--yes"
			if err := prompt.ConfirmDestructive(request); err != nil {
				return err
			}

			if err := service.Delete(cmd.Context(), repo); err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), RepositoryDeletion{Status: result.OK(), Repository: repositoryOf(repo)})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Deleted.Render("Deleted repository"), style.Resource.Render(repoRef.ProjectKey+"/"+repoRef.Slug))
			return nil
		},
	}
	if repositorySelector == nil {
		deleteCmd.Flags().StringVar(&localRepoSelector, "repo", "", "Repository as PROJECT/slug (defaults to BITBUCKET_PROJECT_KEY + BITBUCKET_REPO_SLUG)")
	}
	deleteCmd.Flags().BoolVar(&confirmed, "yes", false, "Skip the confirmation. Only applies when the repository is named explicitly.")
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
						if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapi.RepoAdmin); err != nil {
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
				return deps.WriteJSON(cmd.OutOrStdout(), SingleRepository{Repository: result.RepositoryDetailFrom(updated)})
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
