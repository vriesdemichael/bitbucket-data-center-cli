package cli

import (
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	projectservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/project"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/style"
)

func newProjectCommand(options *rootOptions) *cobra.Command {
	var limit int

	projectCmd := &cobra.Command{
		Use:   "project",
		Short: "Project administration commands",
	}

	projectCmd.PersistentFlags().IntVar(&limit, "limit", 25, "Page size for list operations")

	var listName string
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List projects",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			service := projectservice.NewService(client)
			projects, err := service.List(cmd.Context(), projectservice.ListOptions{
				Limit: limit,
				Name:  listName,
			})
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"projects": projects})
			}

			if len(projects) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No projects found"))
				return nil
			}

			rows := make([][]string, len(projects))
			for i, p := range projects {
				rows[i] = []string{style.Resource.Render(safeString(p.Key)), safeString(p.Name)}
			}
			style.WriteTable(cmd.OutOrStdout(), rows)

			return nil
		},
	}
	listCmd.Flags().StringVar(&listName, "name", "", "Filter projects by name")
	projectCmd.AddCommand(listCmd)

	getCmd := &cobra.Command{
		Use:   "get <key>",
		Short: "Get project details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			service := projectservice.NewService(client)
			project, err := service.Get(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"project": project})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Label.Render("Key:"), safeString(project.Key))
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Label.Render("Name:"), safeString(project.Name))
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Label.Render("Description:"), safeString(project.Description))
			return nil
		},
	}
	projectCmd.AddCommand(getCmd)

	var createName string
	var createDesc string
	createCmd := &cobra.Command{
		Use:   "create <key>",
		Short: "Create a new project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			service := projectservice.NewService(client)
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckProjectCreate(cmd.Context()); err != nil {
					return err
				}

				_, err := service.Get(cmd.Context(), args[0])
				predicted := "create"
				reason := "project will be created"
				if err == nil {
					predicted = "conflict"
					reason = "project key already exists"
				} else if apperrors.ExitCode(err) != 4 {
					return err
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "project.create",
						Target:          map[string]any{"project": args[0], "name": createName, "description": createDesc},
						Action:          "create",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"project get"},
						BlockingReasons: func() []string {
							if predicted == "conflict" {
								return []string{"project key exists"}
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

			created, err := service.Create(cmd.Context(), projectservice.CreateInput{
				Key:         args[0],
				Name:        createName,
				Description: createDesc,
			})
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"project": created})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Success.Render("Created project"), style.Resource.Render(safeString(created.Key)))
			return nil
		},
	}
	createCmd.Flags().StringVar(&createName, "name", "", "Project name (required)")
	createCmd.Flags().StringVar(&createDesc, "description", "", "Project description")
	_ = createCmd.MarkFlagRequired("name")
	projectCmd.AddCommand(createCmd)

	var updateName string
	var updateDesc string
	updateCmd := &cobra.Command{
		Use:   "update <key>",
		Short: "Update project details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			service := projectservice.NewService(client)
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckProjectAdmin(cmd.Context(), args[0]); err != nil {
					return err
				}

				current, err := service.Get(cmd.Context(), args[0])
				if err != nil {
					return err
				}

				predicted := "update"
				reason := "project details will be updated"
				currentName := strings.TrimSpace(safeString(current.Name))
				currentDesc := strings.TrimSpace(safeString(current.Description))
				if strings.EqualFold(currentName, strings.TrimSpace(updateName)) && strings.EqualFold(currentDesc, strings.TrimSpace(updateDesc)) {
					predicted = "no-op"
					reason = "project already matches requested values"
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "project.update",
						Target:          map[string]any{"project": args[0], "name": updateName, "description": updateDesc},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"project get"},
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

			updated, err := service.Update(cmd.Context(), args[0], projectservice.UpdateInput{
				Name:        updateName,
				Description: updateDesc,
			})
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"project": updated})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Updated.Render("Updated project"), style.Resource.Render(safeString(updated.Key)))
			return nil
		},
	}
	updateCmd.Flags().StringVar(&updateName, "name", "", "Project name")
	updateCmd.Flags().StringVar(&updateDesc, "description", "", "Project description")
	projectCmd.AddCommand(updateCmd)

	deleteCmd := &cobra.Command{
		Use:   "delete <key>",
		Short: "Delete a project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			service := projectservice.NewService(client)
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckProjectAdmin(cmd.Context(), args[0]); err != nil {
					return err
				}

				_, err := service.Get(cmd.Context(), args[0])
				predicted := "delete"
				reason := "project will be deleted"
				if err != nil {
					if apperrors.ExitCode(err) == 4 {
						predicted = "no-op"
						reason = "project was not found"
					} else {
						return err
					}
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "project.delete",
						Target:          map[string]any{"project": args[0]},
						Action:          "delete",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"project get"},
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

			if err := service.Delete(cmd.Context(), args[0]); err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]string{"status": "ok", "project_key": args[0]})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Deleted.Render("Deleted project"), style.Resource.Render(args[0]))
			return nil
		},
	}
	projectCmd.AddCommand(deleteCmd)

	var projectPermissionsLimit int
	permissionsCmd := &cobra.Command{Use: "permissions", Short: "Project permissions"}

	permissionsUsersCmd := &cobra.Command{Use: "users", Short: "User permissions"}
	permissionsUsersListCmd := &cobra.Command{
		Use:   "list <key>",
		Short: "List users with project permissions",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			service := projectservice.NewService(client)
			users, err := service.ListProjectPermissionUsers(cmd.Context(), args[0], projectPermissionsLimit)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"users": users})
			}
			if len(users) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No users with project permissions found"))
				return nil
			}
			rows := make([][]string, len(users))
			for i, user := range users {
				display := user.Display
				if strings.TrimSpace(display) == "" {
					display = user.Name
				}
				rows[i] = []string{style.Resource.Render(display), user.Permission}
			}
			style.WriteTable(cmd.OutOrStdout(), rows)

			return nil
		},
	}
	permissionsUsersListCmd.Flags().IntVar(&projectPermissionsLimit, "limit", 100, "Page size for listing permission users")

	permissionsUsersGrantCmd := &cobra.Command{
		Use:   "grant <key> <username> <permission>",
		Short: "Grant a project permission to a user",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			service := projectservice.NewService(client)
			permission := strings.ToUpper(strings.TrimSpace(args[2]))
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckProjectAdmin(cmd.Context(), args[0]); err != nil {
					return err
				}

				users, err := service.ListProjectPermissionUsers(cmd.Context(), args[0], projectPermissionsLimit)
				if err != nil {
					return err
				}

				predicted := "create"
				reason := "permission grant will create project user permission entry"
				for _, user := range users {
					if strings.EqualFold(strings.TrimSpace(user.Name), strings.TrimSpace(args[1])) {
						if strings.EqualFold(strings.TrimSpace(user.Permission), permission) {
							predicted = "no-op"
							reason = "user already has requested project permission"
						} else {
							predicted = "update"
							reason = "user project permission will be updated"
						}
						break
					}
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "project.permission.user.grant",
						Target:          map[string]any{"project": args[0], "username": args[1], "permission": permission},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"project permission users list"},
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1},
				}
				switch predicted {
				case "create":
					preview.Summary.CreateCount = 1
				case "update":
					preview.Summary.UpdateCount = 1
				case "no-op":
					preview.Summary.NoopCount = 1
				default:
					preview.Summary.UnknownCount = 1
				}

				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}

			if err := service.GrantProjectUserPermission(cmd.Context(), args[0], args[1], permission); err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"status": "ok", "project": args[0], "username": args[1], "permission": permission})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s to %s for project %s\n", style.Success.Render("Granted"), permission, style.Resource.Render(args[1]), args[0])
			return nil
		},
	}

	permissionsUsersRevokeCmd := &cobra.Command{
		Use:   "revoke <key> <username>",
		Short: "Revoke a project permission from a user",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			service := projectservice.NewService(client)
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckProjectAdmin(cmd.Context(), args[0]); err != nil {
					return err
				}

				users, err := service.ListProjectPermissionUsers(cmd.Context(), args[0], projectPermissionsLimit)
				if err != nil {
					return err
				}

				predicted := "no-op"
				reason := "user does not currently have project permission entry"
				if slices.ContainsFunc(users, func(user projectservice.PermissionUser) bool {
					return strings.EqualFold(strings.TrimSpace(user.Name), strings.TrimSpace(args[1]))
				}) {
					predicted = "delete"
					reason = "user project permission entry will be removed"
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "project.permission.user.revoke",
						Target:          map[string]any{"project": args[0], "username": args[1]},
						Action:          "delete",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"project permission users list"},
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1},
				}
				switch predicted {
				case "delete":
					preview.Summary.DeleteCount = 1
				case "no-op":
					preview.Summary.NoopCount = 1
				default:
					preview.Summary.UnknownCount = 1
				}

				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}

			if err := service.RevokeProjectUserPermission(cmd.Context(), args[0], args[1]); err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"status": "ok", "project": args[0], "username": args[1]})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s for %s on project %s\n", style.Deleted.Render("Revoked permissions"), style.Resource.Render(args[1]), args[0])
			return nil
		},
	}

	permissionsUsersCmd.AddCommand(permissionsUsersListCmd)
	permissionsUsersCmd.AddCommand(permissionsUsersGrantCmd)
	permissionsUsersCmd.AddCommand(permissionsUsersRevokeCmd)
	permissionsCmd.AddCommand(permissionsUsersCmd)

	permissionsGroupsCmd := &cobra.Command{Use: "groups", Short: "Group permissions"}
	permissionsGroupsListCmd := &cobra.Command{
		Use:   "list <key>",
		Short: "List groups with project permissions",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			service := projectservice.NewService(client)
			groups, err := service.ListProjectPermissionGroups(cmd.Context(), args[0], projectPermissionsLimit)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"groups": groups})
			}
			if len(groups) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No groups with project permissions found"))
				return nil
			}
			rows := make([][]string, len(groups))
			for i, group := range groups {
				rows[i] = []string{style.Resource.Render(group.Name), group.Permission}
			}
			style.WriteTable(cmd.OutOrStdout(), rows)

			return nil
		},
	}
	permissionsGroupsListCmd.Flags().IntVar(&projectPermissionsLimit, "limit", 100, "Page size for listing permission groups")

	permissionsGroupsGrantCmd := &cobra.Command{
		Use:   "grant <key> <group> <permission>",
		Short: "Grant a project permission to a group",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			service := projectservice.NewService(client)
			permission := strings.ToUpper(strings.TrimSpace(args[2]))
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckProjectAdmin(cmd.Context(), args[0]); err != nil {
					return err
				}

				groups, err := service.ListProjectPermissionGroups(cmd.Context(), args[0], projectPermissionsLimit)
				if err != nil {
					return err
				}

				predicted := "create"
				reason := "permission grant will create project group permission entry"
				for _, group := range groups {
					if strings.EqualFold(strings.TrimSpace(group.Name), strings.TrimSpace(args[1])) {
						if strings.EqualFold(strings.TrimSpace(group.Permission), permission) {
							predicted = "no-op"
							reason = "group already has requested project permission"
						} else {
							predicted = "update"
							reason = "group project permission will be updated"
						}
						break
					}
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "project.permission.group.grant",
						Target:          map[string]any{"project": args[0], "group": args[1], "permission": permission},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"project permission groups list"},
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1},
				}
				switch predicted {
				case "create":
					preview.Summary.CreateCount = 1
				case "update":
					preview.Summary.UpdateCount = 1
				case "no-op":
					preview.Summary.NoopCount = 1
				default:
					preview.Summary.UnknownCount = 1
				}

				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}

			if err := service.GrantProjectGroupPermission(cmd.Context(), args[0], args[1], permission); err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"status": "ok", "project": args[0], "group": args[1], "permission": permission})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s to group %s for project %s\n", style.Success.Render("Granted"), permission, style.Resource.Render(args[1]), args[0])
			return nil
		},
	}

	permissionsGroupsRevokeCmd := &cobra.Command{
		Use:   "revoke <key> <group>",
		Short: "Revoke a project permission from a group",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			service := projectservice.NewService(client)
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckProjectAdmin(cmd.Context(), args[0]); err != nil {
					return err
				}

				groups, err := service.ListProjectPermissionGroups(cmd.Context(), args[0], projectPermissionsLimit)
				if err != nil {
					return err
				}

				predicted := "no-op"
				reason := "group does not currently have project permission entry"
				if slices.ContainsFunc(groups, func(group projectservice.PermissionGroup) bool {
					return strings.EqualFold(strings.TrimSpace(group.Name), strings.TrimSpace(args[1]))
				}) {
					predicted = "delete"
					reason = "group project permission entry will be removed"
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "project.permission.group.revoke",
						Target:          map[string]any{"project": args[0], "group": args[1]},
						Action:          "delete",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"project permission groups list"},
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1},
				}
				switch predicted {
				case "delete":
					preview.Summary.DeleteCount = 1
				case "no-op":
					preview.Summary.NoopCount = 1
				default:
					preview.Summary.UnknownCount = 1
				}

				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}

			if err := service.RevokeProjectGroupPermission(cmd.Context(), args[0], args[1]); err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"status": "ok", "project": args[0], "group": args[1]})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s for group %s on project %s\n", style.Deleted.Render("Revoked permissions"), style.Resource.Render(args[1]), args[0])
			return nil
		},
	}

	permissionsGroupsCmd.AddCommand(permissionsGroupsListCmd)
	permissionsGroupsCmd.AddCommand(permissionsGroupsGrantCmd)
	permissionsGroupsCmd.AddCommand(permissionsGroupsRevokeCmd)
	permissionsCmd.AddCommand(permissionsGroupsCmd)

	permissionsShowCmd := &cobra.Command{
		Use:   "show <key>",
		Short: "Show the caller's effective permissions on a project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			checker := options.permissionCheckerFor(client)
			perms, err := checker.InspectProjectPermissions(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{
					"project_key": args[0],
					"permissions": perms,
				})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Label.Render("Project:"), args[0])
			for _, level := range []string{"PROJECT_READ", "PROJECT_WRITE", "PROJECT_ADMIN"} {
				fmt.Fprintf(cmd.OutOrStdout(), "%s %t\n", style.Label.Render(level+":"), perms[level])
			}
			return nil
		},
	}
	permissionsCmd.AddCommand(permissionsShowCmd)

	projectCmd.AddCommand(permissionsCmd)
	projectCmd.AddCommand(newProjectWebhookCommand(options))
	projectCmd.AddCommand(newProjectBranchRestrictionCommand(options))
	projectCmd.AddCommand(newProjectDefaultTaskCommand(options))

	return projectCmd
}
