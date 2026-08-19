package projectcmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/dryrunpreview"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/paging"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/style"
	projectservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/project"
)

const permissionLookupLimit = 100

type permissionEntry struct {
	name       string
	display    string
	permission string
}

type projectPermissionSubject struct {
	noun       string
	tableLabel string
	list       func(context.Context, *projectservice.Service, string, int) ([]permissionEntry, error)
	grant      func(context.Context, *projectservice.Service, string, string, string) error
	revoke     func(context.Context, *projectservice.Service, string, string) error
}

func userProjectPermissionSubject() projectPermissionSubject {
	return projectPermissionSubject{
		noun:       "user",
		tableLabel: "",
		list: func(ctx context.Context, service *projectservice.Service, projectKey string, limit int) ([]permissionEntry, error) {
			users, err := service.ListProjectPermissionUsers(ctx, projectKey, limit)
			if err != nil {
				return nil, err
			}

			entries := make([]permissionEntry, len(users))
			for index, user := range users {
				display := user.Display
				if strings.TrimSpace(display) == "" {
					display = user.Name
				}
				entries[index] = permissionEntry{name: user.Name, display: display, permission: user.Permission}
			}

			return entries, nil
		},
		grant: func(ctx context.Context, service *projectservice.Service, projectKey string, subject string, permission string) error {
			return service.GrantProjectUserPermission(ctx, projectKey, subject, permission)
		},
		revoke: func(ctx context.Context, service *projectservice.Service, projectKey string, subject string) error {
			return service.RevokeProjectUserPermission(ctx, projectKey, subject)
		},
	}
}

func groupProjectPermissionSubject() projectPermissionSubject {
	return projectPermissionSubject{
		noun:       "group",
		tableLabel: "group ",
		list: func(ctx context.Context, service *projectservice.Service, projectKey string, limit int) ([]permissionEntry, error) {
			groups, err := service.ListProjectPermissionGroups(ctx, projectKey, limit)
			if err != nil {
				return nil, err
			}

			entries := make([]permissionEntry, len(groups))
			for index, group := range groups {
				entries[index] = permissionEntry{name: group.Name, display: group.Name, permission: group.Permission}
			}

			return entries, nil
		},
		grant: func(ctx context.Context, service *projectservice.Service, projectKey string, subject string, permission string) error {
			return service.GrantProjectGroupPermission(ctx, projectKey, subject, permission)
		},
		revoke: func(ctx context.Context, service *projectservice.Service, projectKey string, subject string) error {
			return service.RevokeProjectGroupPermission(ctx, projectKey, subject)
		},
	}
}

type projectPermissionSubjectResolver func() projectPermissionSubject

func fixedProjectPermissionSubject(subject projectPermissionSubject) projectPermissionSubjectResolver {
	return func() projectPermissionSubject { return subject }
}

func (subject projectPermissionSubject) jsonSubjectKey() string {
	if subject.noun == "user" {
		return "username"
	}

	return subject.noun
}

func (subject projectPermissionSubject) argPlaceholder() string {
	if subject.noun == "user" {
		return "username"
	}

	return subject.noun
}

func newProjectPermissionListCommand(deps Dependencies, subjectFor projectPermissionSubjectResolver) *cobra.Command {
	var listPaging paging.Options

	command := &cobra.Command{
		Use:  "list <key>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			subject := subjectFor()

			_, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := projectservice.NewService(client)
			entries, err := subject.list(cmd.Context(), service, args[0], listPaging.ServiceLimit())
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]any{subject.noun + "s": projectPermissionEntriesPayload(subject, entries)})
			}
			if len(entries) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render(fmt.Sprintf("No %ss with project permissions found", subject.noun)))
				return nil
			}

			rows := make([][]string, len(entries))
			for index, entry := range entries {
				rows[index] = []string{style.Resource.Render(entry.display), entry.permission}
			}
			style.WriteTable(cmd.OutOrStdout(), rows)

			return nil
		},
	}
	listPaging.Register(command, permissionLookupLimit)

	return command
}

func projectPermissionEntriesPayload(subject projectPermissionSubject, entries []permissionEntry) any {
	if subject.noun == "group" {
		groups := make([]projectservice.PermissionGroup, len(entries))
		for index, entry := range entries {
			groups[index] = projectservice.PermissionGroup{Name: entry.name, Permission: entry.permission}
		}
		return groups
	}

	users := make([]projectservice.PermissionUser, len(entries))
	for index, entry := range entries {
		display := entry.display
		if display == entry.name {
			display = ""
		}
		users[index] = projectservice.PermissionUser{Name: entry.name, Display: display, Permission: entry.permission}
	}

	return users
}

func newProjectPermissionGrantCommand(deps Dependencies, subjectFor projectPermissionSubjectResolver) *cobra.Command {
	return &cobra.Command{
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			subject := subjectFor()

			_, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := projectservice.NewService(client)
			projectKey := args[0]
			name := args[1]
			permission := strings.ToUpper(strings.TrimSpace(args[2]))

			if deps.DryRunEnabled() {
				if deps.PermissionChecker != nil {
					checker := deps.PermissionChecker(client)
					if checker != nil {
						if err := checker.CheckProjectAdmin(cmd.Context(), projectKey); err != nil {
							return err
						}
					}
				}

				entries, err := subject.list(cmd.Context(), service, projectKey, permissionLookupLimit)
				if err != nil {
					return err
				}

				predicted := "create"
				reason := fmt.Sprintf("permission grant will create project %s permission entry", subject.noun)
				for _, entry := range entries {
					if !strings.EqualFold(strings.TrimSpace(entry.name), strings.TrimSpace(name)) {
						continue
					}

					if strings.EqualFold(strings.TrimSpace(entry.permission), permission) {
						predicted = "no-op"
						reason = fmt.Sprintf("%s already has requested project permission", subject.noun)
					} else {
						predicted = "update"
						reason = fmt.Sprintf("%s project permission will be updated", subject.noun)
					}
					break
				}

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent: fmt.Sprintf("project.permission.%s.grant", subject.noun),
						Target: map[string]any{
							"project":                projectKey,
							subject.jsonSubjectKey(): name,
							"permission":             permission,
						},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      dryrunpreview.CapabilityFull,
						RequiredState:   []string{fmt.Sprintf("project permission %ss list", subject.noun)},
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1},
				}
				applyPermissionDryRunSummary(&preview, predicted)

				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			if err := subject.grant(cmd.Context(), service, projectKey, name, permission); err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]any{
					"status":                 "ok",
					"project":                projectKey,
					subject.jsonSubjectKey(): name,
					"permission":             permission,
				})
			}
			fmt.Fprintf(
				cmd.OutOrStdout(),
				"%s %s to %s%s for project %s\n",
				style.Success.Render("Granted"),
				permission,
				subject.tableLabel,
				style.Resource.Render(name),
				projectKey,
			)
			return nil
		},
	}
}

func newProjectPermissionRevokeCommand(deps Dependencies, subjectFor projectPermissionSubjectResolver) *cobra.Command {
	return &cobra.Command{
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			subject := subjectFor()

			_, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := projectservice.NewService(client)
			projectKey := args[0]
			name := args[1]

			if deps.DryRunEnabled() {
				if deps.PermissionChecker != nil {
					checker := deps.PermissionChecker(client)
					if checker != nil {
						if err := checker.CheckProjectAdmin(cmd.Context(), projectKey); err != nil {
							return err
						}
					}
				}

				entries, err := subject.list(cmd.Context(), service, projectKey, permissionLookupLimit)
				if err != nil {
					return err
				}

				predicted := "no-op"
				reason := fmt.Sprintf("%s does not currently have project permission entry", subject.noun)
				for _, entry := range entries {
					if strings.EqualFold(strings.TrimSpace(entry.name), strings.TrimSpace(name)) {
						predicted = "delete"
						reason = fmt.Sprintf("%s project permission entry will be removed", subject.noun)
						break
					}
				}

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent: fmt.Sprintf("project.permission.%s.revoke", subject.noun),
						Target: map[string]any{
							"project":                projectKey,
							subject.jsonSubjectKey(): name,
						},
						Action:          "delete",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      dryrunpreview.CapabilityFull,
						RequiredState:   []string{fmt.Sprintf("project permission %ss list", subject.noun)},
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1},
				}
				applyPermissionDryRunSummary(&preview, predicted)

				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			if err := subject.revoke(cmd.Context(), service, projectKey, name); err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]any{
					"status":                 "ok",
					"project":                projectKey,
					subject.jsonSubjectKey(): name,
				})
			}
			fmt.Fprintf(
				cmd.OutOrStdout(),
				"%s for %s%s on project %s\n",
				style.Deleted.Render("Revoked permissions"),
				subject.tableLabel,
				style.Resource.Render(name),
				projectKey,
			)
			return nil
		},
	}
}

func newProjectPermissionSubjectCommand(deps Dependencies, subject projectPermissionSubject) *cobra.Command {
	resolver := fixedProjectPermissionSubject(subject)
	shallow := "bb project permissions"

	group := &cobra.Command{
		Use:   subject.noun + "s",
		Short: strings.ToUpper(subject.noun[:1]) + subject.noun[1:] + " permissions",
	}

	listCommand := newProjectPermissionListCommand(deps, resolver)
	listCommand.Short = fmt.Sprintf("List %ss with project permissions", subject.noun)
	listCommand.Long = fmt.Sprintf("List %ss with project permissions.\n\n%s", subject.noun, alsoAvailableAs(shallow+" list", subject.noun))

	grantCommand := newProjectPermissionGrantCommand(deps, resolver)
	grantCommand.Use = fmt.Sprintf("grant <key> <%s> <permission>", subject.argPlaceholder())
	grantCommand.Short = fmt.Sprintf("Grant a project permission to a %s", subject.noun)
	grantCommand.Long = fmt.Sprintf("Grant a project permission to a %s.\n\n%s", subject.noun, alsoAvailableAs(shallow+" grant", subject.noun))

	revokeCommand := newProjectPermissionRevokeCommand(deps, resolver)
	revokeCommand.Use = fmt.Sprintf("revoke <key> <%s>", subject.argPlaceholder())
	revokeCommand.Short = fmt.Sprintf("Revoke a project permission from a %s", subject.noun)
	revokeCommand.Long = fmt.Sprintf("Revoke a project permission from a %s.\n\n%s", subject.noun, alsoAvailableAs(shallow+" revoke", subject.noun))

	group.AddCommand(listCommand)
	group.AddCommand(grantCommand)
	group.AddCommand(revokeCommand)

	return group
}

func addProjectPermissionAliases(parent *cobra.Command, deps Dependencies) {
	var listGroups bool
	var grantGroup bool
	var revokeGroup bool

	subjectFrom := func(useGroup *bool) projectPermissionSubjectResolver {
		return func() projectPermissionSubject {
			if *useGroup {
				return groupProjectPermissionSubject()
			}

			return userProjectPermissionSubject()
		}
	}

	deep := "bb project permissions {users,groups}"

	listCommand := newProjectPermissionListCommand(deps, subjectFrom(&listGroups))
	listCommand.Short = "List users or groups with project permissions"
	listCommand.Long = "List users with project permissions, or groups with --group.\n\n" +
		"Shallow alias for " + deep + " list."
	listCommand.Flags().BoolVar(&listGroups, "group", false, "List groups instead of users")

	grantCommand := newProjectPermissionGrantCommand(deps, subjectFrom(&grantGroup))
	grantCommand.Use = "grant <key> <user-or-group> <permission>"
	grantCommand.Short = "Grant a project permission to a user or group"
	grantCommand.Long = "Grant a project permission to a user, or to a group with --group.\n\n" +
		"Shallow alias for " + deep + " grant."
	grantCommand.Flags().BoolVar(&grantGroup, "group", false, "Treat the argument as a group rather than a user")

	revokeCommand := newProjectPermissionRevokeCommand(deps, subjectFrom(&revokeGroup))
	revokeCommand.Use = "revoke <key> <user-or-group>"
	revokeCommand.Short = "Revoke a project permission from a user or group"
	revokeCommand.Long = "Revoke a project permission from a user, or from a group with --group.\n\n" +
		"Shallow alias for " + deep + " revoke."
	revokeCommand.Flags().BoolVar(&revokeGroup, "group", false, "Treat the argument as a group rather than a user")

	parent.AddCommand(listCommand)
	parent.AddCommand(grantCommand)
	parent.AddCommand(revokeCommand)
}

func alsoAvailableAs(shallowPath string, noun string) string {
	flag := ""
	if noun == "group" {
		flag = " --group"
	}

	return fmt.Sprintf("Also available as %s%s, one level shallower.", shallowPath, flag)
}

func applyPermissionDryRunSummary(preview *dryrunpreview.Preview, predicted string) {
	switch predicted {
	case "create":
		preview.Summary.CreateCount = 1
	case "update":
		preview.Summary.UpdateCount = 1
	case "delete":
		preview.Summary.DeleteCount = 1
	case "no-op":
		preview.Summary.NoopCount = 1
	default:
		preview.Summary.UnknownCount = 1
	}
}

func newProjectPermissionsCommand(deps Dependencies) *cobra.Command {
	permissionsCmd := &cobra.Command{Use: "permissions", Short: "Project permissions"}
	permissionsCmd.AddCommand(newProjectPermissionSubjectCommand(deps, userProjectPermissionSubject()))
	permissionsCmd.AddCommand(newProjectPermissionSubjectCommand(deps, groupProjectPermissionSubject()))
	addProjectPermissionAliases(permissionsCmd, deps)

	permissionsShowCmd := &cobra.Command{
		Use:   "show <key>",
		Short: "Show the caller's effective permissions on a project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			if deps.PermissionChecker == nil {
				return fmt.Errorf("permission checker is not configured")
			}
			checker := deps.PermissionChecker(client)
			if checker == nil {
				return fmt.Errorf("permission checker is not configured")
			}

			perms, err := checker.InspectProjectPermissions(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), map[string]any{
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

	return permissionsCmd
}
