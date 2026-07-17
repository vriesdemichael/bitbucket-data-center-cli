package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
	reviewerservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/reviewer"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/style"
)

func newReviewerGroupCommand(options *rootOptions) *cobra.Command {
	var projectKey string
	var repositorySelector string
	var nameFlag string
	var descriptionFlag string

	reviewerGroupCmd := &cobra.Command{
		Use:   "reviewer-group",
		Short: "Manage reviewer groups",
	}

	reviewerGroupCmd.PersistentFlags().StringVar(&projectKey, "project", "", "Project key")
	reviewerGroupCmd.PersistentFlags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List reviewer groups",
		RunE: func(cmd *cobra.Command, args []string) error {
			if projectKey != "" && repositorySelector != "" {
				return fmt.Errorf("cannot specify both --project and --repo")
			}

			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			service := reviewerservice.NewService(client)

			if repositorySelector != "" {
				repo, err := resolveRepositoryReference(repositorySelector, cfg)
				if err != nil {
					return err
				}
				groups, err := service.ListRepositoryReviewerGroups(cmd.Context(), repo.ProjectKey, repo.Slug)
				if err != nil {
					return err
				}
				if options.JSON {
					return writeJSON(cmd.OutOrStdout(), map[string]any{"reviewer_groups": groups})
				}
				printReviewerGroups(cmd, groups)
				return nil
			}

			if projectKey == "" {
				projectKey = cfg.ProjectKey
			}
			if projectKey == "" {
				return fmt.Errorf("project key is required (use --project or --repo)")
			}

			groups, err := service.ListProjectReviewerGroups(cmd.Context(), projectKey)
			if err != nil {
				return err
			}
			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"reviewer_groups": groups})
			}
			printReviewerGroups(cmd, groups)
			return nil
		},
	}
	reviewerGroupCmd.AddCommand(listCmd)

	createCmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a reviewer group",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if projectKey != "" && repositorySelector != "" {
				return fmt.Errorf("cannot specify both --project and --repo")
			}

			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			service := reviewerservice.NewService(client)
			name := args[0]

			if repositorySelector != "" {
				repo, err := resolveRepositoryReference(repositorySelector, cfg)
				if err != nil {
					return err
				}

				if options.DryRun {
					checker := options.permissionCheckerFor(client)
					if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOADMIN); err != nil {
						return err
					}

					groups, err := service.ListRepositoryReviewerGroups(cmd.Context(), repo.ProjectKey, repo.Slug)
					if err != nil {
						return err
					}

					predicted := "create"
					reason := "reviewer group will be created"
					var blocking []string
					if reviewerGroupExistsByName(groups, name) {
						predicted = "conflict"
						reason = fmt.Sprintf("reviewer group with name %q already exists", name)
						blocking = []string{"reviewer group already exists"}
					}

					preview := dryRunPreview{
						DryRun:       true,
						PlanningMode: planningModeStateful,
						Capability:   capabilityFull,
						Items: []dryRunItem{{
							Intent:          "reviewer-group.create",
							Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "name": name},
							Action:          "create",
							PredictedAction: predicted,
							Supported:       true,
							Reason:          reason,
							Confidence:      capabilityFull,
							RequiredState:   []string{"reviewer groups list"},
							BlockingReasons: blocking,
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

				group, err := service.CreateRepositoryReviewerGroup(cmd.Context(), repo.ProjectKey, repo.Slug, name, descriptionFlag)
				if err != nil {
					return err
				}
				if options.JSON {
					return writeJSON(cmd.OutOrStdout(), group)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s %s for repository %s\n", style.Success.Render("Created reviewer group"), style.Resource.Render(name), style.Resource.Render(repo.ProjectKey+"/"+repo.Slug))
				return nil
			}

			if projectKey == "" {
				projectKey = cfg.ProjectKey
			}
			if projectKey == "" {
				return fmt.Errorf("project key is required (use --project or --repo)")
			}

			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckProjectAdmin(cmd.Context(), projectKey); err != nil {
					return err
				}

				groups, err := service.ListProjectReviewerGroups(cmd.Context(), projectKey)
				if err != nil {
					return err
				}

				predicted := "create"
				reason := "reviewer group will be created"
				var blocking []string
				if reviewerGroupExistsByName(groups, name) {
					predicted = "conflict"
					reason = fmt.Sprintf("reviewer group with name %q already exists", name)
					blocking = []string{"reviewer group already exists"}
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "reviewer-group.create",
						Target:          map[string]any{"project": projectKey, "name": name},
						Action:          "create",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"reviewer groups list"},
						BlockingReasons: blocking,
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

			group, err := service.CreateProjectReviewerGroup(cmd.Context(), projectKey, name, descriptionFlag)
			if err != nil {
				return err
			}
			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), group)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s for project %s\n", style.Success.Render("Created reviewer group"), style.Resource.Render(name), projectKey)
			return nil
		},
	}
	createCmd.Flags().StringVar(&descriptionFlag, "description", "", "Description of the reviewer group")
	reviewerGroupCmd.AddCommand(createCmd)

	updateCmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update a reviewer group",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if projectKey != "" && repositorySelector != "" {
				return fmt.Errorf("cannot specify both --project and --repo")
			}

			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			service := reviewerservice.NewService(client)
			id := args[0]

			if repositorySelector != "" {
				repo, err := resolveRepositoryReference(repositorySelector, cfg)
				if err != nil {
					return err
				}

				if options.DryRun {
					checker := options.permissionCheckerFor(client)
					if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOADMIN); err != nil {
						return err
					}

					groups, err := service.ListRepositoryReviewerGroups(cmd.Context(), repo.ProjectKey, repo.Slug)
					if err != nil {
						return err
					}

					predicted := "blocked"
					reason := "reviewer group not found"
					blocking := []string{"reviewer group not found"}
					if existing, found := findReviewerGroupByID(groups, id); found {
						blocking = nil
						predicted = "update"
						reason = "reviewer group will be updated"
						nameMatch := nameFlag == "" || (existing.Name != nil && *existing.Name == nameFlag)
						descMatch := descriptionFlag == "" || (existing.Description != nil && *existing.Description == descriptionFlag)
						if nameMatch && descMatch {
							predicted = "no-op"
							reason = "reviewer group configuration already matches requested update"
						}
					}

					preview := dryRunPreview{
						DryRun:       true,
						PlanningMode: planningModeStateful,
						Capability:   capabilityFull,
						Items: []dryRunItem{{
							Intent:          "reviewer-group.update",
							Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": id},
							Action:          "update",
							PredictedAction: predicted,
							Supported:       true,
							Reason:          reason,
							Confidence:      capabilityFull,
							RequiredState:   []string{"reviewer groups list"},
							BlockingReasons: blocking,
						}},
						Summary: dryRunSummary{Total: 1, Supported: 1},
					}
					if predicted == "update" {
						preview.Summary.UpdateCount = 1
					} else if predicted == "no-op" {
						preview.Summary.NoopCount = 1
					} else {
						preview.Summary.UnknownCount = 1
					}
					return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
				}

				group, err := service.UpdateRepositoryReviewerGroup(cmd.Context(), repo.ProjectKey, repo.Slug, id, nameFlag, descriptionFlag)
				if err != nil {
					return err
				}
				if options.JSON {
					return writeJSON(cmd.OutOrStdout(), group)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s %s for repository %s\n", style.Updated.Render("Updated reviewer group"), style.Resource.Render(id), style.Resource.Render(repo.ProjectKey+"/"+repo.Slug))
				return nil
			}

			if projectKey == "" {
				projectKey = cfg.ProjectKey
			}
			if projectKey == "" {
				return fmt.Errorf("project key is required (use --project or --repo)")
			}

			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckProjectAdmin(cmd.Context(), projectKey); err != nil {
					return err
				}

				groups, err := service.ListProjectReviewerGroups(cmd.Context(), projectKey)
				if err != nil {
					return err
				}

				predicted := "blocked"
				reason := "reviewer group not found"
				blocking := []string{"reviewer group not found"}
				if existing, found := findReviewerGroupByID(groups, id); found {
					blocking = nil
					predicted = "update"
					reason = "reviewer group will be updated"
					nameMatch := nameFlag == "" || (existing.Name != nil && *existing.Name == nameFlag)
					descMatch := descriptionFlag == "" || (existing.Description != nil && *existing.Description == descriptionFlag)
					if nameMatch && descMatch {
						predicted = "no-op"
						reason = "reviewer group configuration already matches requested update"
					}
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "reviewer-group.update",
						Target:          map[string]any{"project": projectKey, "id": id},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"reviewer groups list"},
						BlockingReasons: blocking,
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1},
				}
				if predicted == "update" {
					preview.Summary.UpdateCount = 1
				} else if predicted == "no-op" {
					preview.Summary.NoopCount = 1
				} else {
					preview.Summary.UnknownCount = 1
				}
				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}

			group, err := service.UpdateProjectReviewerGroup(cmd.Context(), projectKey, id, nameFlag, descriptionFlag)
			if err != nil {
				return err
			}
			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), group)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s for project %s\n", style.Updated.Render("Updated reviewer group"), style.Resource.Render(id), projectKey)
			return nil
		},
	}
	updateCmd.Flags().StringVar(&nameFlag, "name", "", "New name of the reviewer group")
	updateCmd.Flags().StringVar(&descriptionFlag, "description", "", "New description of the reviewer group")
	reviewerGroupCmd.AddCommand(updateCmd)

	deleteCmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a reviewer group",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if projectKey != "" && repositorySelector != "" {
				return fmt.Errorf("cannot specify both --project and --repo")
			}

			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			service := reviewerservice.NewService(client)
			id := args[0]

			if repositorySelector != "" {
				repo, err := resolveRepositoryReference(repositorySelector, cfg)
				if err != nil {
					return err
				}

				if options.DryRun {
					checker := options.permissionCheckerFor(client)
					if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOADMIN); err != nil {
						return err
					}

					groups, err := service.ListRepositoryReviewerGroups(cmd.Context(), repo.ProjectKey, repo.Slug)
					if err != nil {
						return err
					}

					predicted := "no-op"
					reason := "reviewer group not found"
					if reviewerGroupExistsByID(groups, id) {
						predicted = "delete"
						reason = "reviewer group will be deleted"
					}

					preview := dryRunPreview{
						DryRun:       true,
						PlanningMode: planningModeStateful,
						Capability:   capabilityFull,
						Items: []dryRunItem{{
							Intent:          "reviewer-group.delete",
							Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": id},
							Action:          "delete",
							PredictedAction: predicted,
							Supported:       true,
							Reason:          reason,
							Confidence:      capabilityFull,
							RequiredState:   []string{"reviewer groups list"},
						}},
						Summary: dryRunSummary{Total: 1, Supported: 1},
					}
					if predicted == "delete" {
						preview.Summary.DeleteCount = 1
					} else {
						preview.Summary.NoopCount = 1
					}
					return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
				}

				if err := service.DeleteRepositoryReviewerGroup(cmd.Context(), repo.ProjectKey, repo.Slug, id); err != nil {
					return err
				}
				if options.JSON {
					return writeJSON(cmd.OutOrStdout(), map[string]string{"status": "ok", "id": id})
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s %s for repository %s\n", style.Deleted.Render("Deleted reviewer group"), style.Resource.Render(id), style.Resource.Render(repo.ProjectKey+"/"+repo.Slug))
				return nil
			}

			if projectKey == "" {
				projectKey = cfg.ProjectKey
			}
			if projectKey == "" {
				return fmt.Errorf("project key is required (use --project or --repo)")
			}

			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckProjectAdmin(cmd.Context(), projectKey); err != nil {
					return err
				}

				groups, err := service.ListProjectReviewerGroups(cmd.Context(), projectKey)
				if err != nil {
					return err
				}

				predicted := "no-op"
				reason := "reviewer group not found"
				if reviewerGroupExistsByID(groups, id) {
					predicted = "delete"
					reason = "reviewer group will be deleted"
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "reviewer-group.delete",
						Target:          map[string]any{"project": projectKey, "id": id},
						Action:          "delete",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"reviewer groups list"},
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1},
				}
				if predicted == "delete" {
					preview.Summary.DeleteCount = 1
				} else {
					preview.Summary.NoopCount = 1
				}
				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}

			if err := service.DeleteProjectReviewerGroup(cmd.Context(), projectKey, id); err != nil {
				return err
			}
			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]string{"status": "ok", "id": id})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s for project %s\n", style.Deleted.Render("Deleted reviewer group"), style.Resource.Render(id), projectKey)
			return nil
		},
	}
	reviewerGroupCmd.AddCommand(deleteCmd)

	usersCmd := &cobra.Command{
		Use:   "users <id>",
		Short: "List users in a repository reviewer group",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if projectKey != "" {
				return fmt.Errorf("users command is only supported at repository scope (use --repo instead of --project)")
			}

			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			service := reviewerservice.NewService(client)
			id := args[0]

			repo, err := resolveRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			users, err := service.ListRepositoryReviewerGroupUsers(cmd.Context(), repo.ProjectKey, repo.Slug, id)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"users": users})
			}

			printUsers(cmd, users)
			return nil
		},
	}
	reviewerGroupCmd.AddCommand(usersCmd)

	return reviewerGroupCmd
}

func printReviewerGroups(cmd *cobra.Command, groups []openapigenerated.RestReviewerGroup) {
	if len(groups) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No reviewer groups found"))
		return
	}
	rows := make([][]string, len(groups))
	for i, g := range groups {
		idStr := ""
		if g.Id != nil {
			idStr = fmt.Sprintf("%d", *g.Id)
		}
		name := ""
		if g.Name != nil {
			name = *g.Name
		}
		desc := ""
		if g.Description != nil {
			desc = *g.Description
		}
		rows[i] = []string{style.Secondary.Render(idStr), style.Resource.Render(name), desc}
	}
	style.WriteTable(cmd.OutOrStdout(), rows)
}

func printUsers(cmd *cobra.Command, users []openapigenerated.RestApplicationUser) {
	if len(users) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No users found"))
		return
	}
	rows := make([][]string, len(users))
	for i, u := range users {
		name := ""
		if u.Name != nil {
			name = *u.Name
		}
		displayName := ""
		if u.DisplayName != nil {
			displayName = *u.DisplayName
		}
		email := ""
		if u.EmailAddress != nil {
			email = *u.EmailAddress
		}
		activeStr := "active"
		if u.Active != nil && !*u.Active {
			activeStr = "inactive"
		}
		rows[i] = []string{style.Resource.Render(name), displayName, email, activeStr}
	}
	style.WriteTable(cmd.OutOrStdout(), rows)
}

func reviewerGroupExistsByName(groups []openapigenerated.RestReviewerGroup, name string) bool {
	for _, g := range groups {
		if g.Name != nil && strings.EqualFold(*g.Name, name) {
			return true
		}
	}
	return false
}

func reviewerGroupExistsByID(groups []openapigenerated.RestReviewerGroup, id string) bool {
	_, found := findReviewerGroupByID(groups, id)
	return found
}

func findReviewerGroupByID(groups []openapigenerated.RestReviewerGroup, id string) (openapigenerated.RestReviewerGroup, bool) {
	trimmedID := strings.TrimSpace(id)
	for _, g := range groups {
		if g.Id != nil && fmt.Sprintf("%d", *g.Id) == trimmedID {
			return g, true
		}
	}
	return openapigenerated.RestReviewerGroup{}, false
}
