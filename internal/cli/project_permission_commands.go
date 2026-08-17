package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/paging"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/style"
	projectservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/project"
)

// projectPermissionSubject is the project-level twin of permissionSubject.
//
// Deliberately a twin rather than a shared abstraction: the two trees differ in
// how they name their target (a positional project key against a --repo flag),
// which service they call, which permission they pre-check, and what their JSON
// and human output say. Folding them together would mean a struct where most
// fields exist to be different, which reads worse than two parallel ones that
// each collapse the duplication that actually existed — the user and group
// halves, written out twice per tree.
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

func newProjectPermissionListCommand(options *rootOptions, subjectFor projectPermissionSubjectResolver) *cobra.Command {
	var listPaging paging.Options

	command := &cobra.Command{
		Use:  "list <key>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			subject := subjectFor()

			_, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			service := projectservice.NewService(client)
			entries, err := subject.list(cmd.Context(), service, args[0], listPaging.ServiceLimit())
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{subject.noun + "s": projectPermissionEntriesPayload(subject, entries)})
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

// projectPermissionEntriesPayload rebuilds the service-shaped structs for JSON.
// The flattened permissionEntry is a rendering detail; the JSON contract
// predates it and is what callers parse.
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

func newProjectPermissionGrantCommand(options *rootOptions, subjectFor projectPermissionSubjectResolver) *cobra.Command {
	return &cobra.Command{
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			subject := subjectFor()

			_, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			service := projectservice.NewService(client)
			projectKey := args[0]
			name := args[1]
			permission := strings.ToUpper(strings.TrimSpace(args[2]))

			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckProjectAdmin(cmd.Context(), projectKey); err != nil {
					return err
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

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
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
						Confidence:      capabilityFull,
						RequiredState:   []string{fmt.Sprintf("project permission %ss list", subject.noun)},
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1},
				}
				applyPermissionDryRunSummary(&preview, predicted)

				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}

			if err := subject.grant(cmd.Context(), service, projectKey, name, permission); err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{
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

func newProjectPermissionRevokeCommand(options *rootOptions, subjectFor projectPermissionSubjectResolver) *cobra.Command {
	return &cobra.Command{
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			subject := subjectFor()

			_, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			service := projectservice.NewService(client)
			projectKey := args[0]
			name := args[1]

			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckProjectAdmin(cmd.Context(), projectKey); err != nil {
					return err
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

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent: fmt.Sprintf("project.permission.%s.revoke", subject.noun),
						Target: map[string]any{
							"project":                projectKey,
							subject.jsonSubjectKey(): name,
						},
						Action:          "delete",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{fmt.Sprintf("project permission %ss list", subject.noun)},
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1},
				}
				applyPermissionDryRunSummary(&preview, predicted)

				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}

			if err := subject.revoke(cmd.Context(), service, projectKey, name); err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{
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

// newProjectPermissionSubjectCommand builds one canonical deep group —
// `bb project permissions users` or `... groups`.
func newProjectPermissionSubjectCommand(options *rootOptions, subject projectPermissionSubject) *cobra.Command {
	resolver := fixedProjectPermissionSubject(subject)
	shallow := "bb project permissions"

	group := &cobra.Command{
		Use:   subject.noun + "s",
		Short: strings.ToUpper(subject.noun[:1]) + subject.noun[1:] + " permissions",
	}

	listCommand := newProjectPermissionListCommand(options, resolver)
	listCommand.Short = fmt.Sprintf("List %ss with project permissions", subject.noun)
	listCommand.Long = fmt.Sprintf("List %ss with project permissions.\n\n%s", subject.noun, alsoAvailableAs(shallow+" list", subject.noun))

	grantCommand := newProjectPermissionGrantCommand(options, resolver)
	grantCommand.Use = fmt.Sprintf("grant <key> <%s> <permission>", subject.argPlaceholder())
	grantCommand.Short = fmt.Sprintf("Grant a project permission to a %s", subject.noun)
	grantCommand.Long = fmt.Sprintf("Grant a project permission to a %s.\n\n%s", subject.noun, alsoAvailableAs(shallow+" grant", subject.noun))

	revokeCommand := newProjectPermissionRevokeCommand(options, resolver)
	revokeCommand.Use = fmt.Sprintf("revoke <key> <%s>", subject.argPlaceholder())
	revokeCommand.Short = fmt.Sprintf("Revoke a project permission from a %s", subject.noun)
	revokeCommand.Long = fmt.Sprintf("Revoke a project permission from a %s.\n\n%s", subject.noun, alsoAvailableAs(shallow+" revoke", subject.noun))

	group.AddCommand(listCommand)
	group.AddCommand(grantCommand)
	group.AddCommand(revokeCommand)

	return group
}

// addProjectPermissionAliases registers the shallow spelling on
// `bb project permissions`, next to the `show` already there.
func addProjectPermissionAliases(parent *cobra.Command, options *rootOptions) {
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

	listCommand := newProjectPermissionListCommand(options, subjectFrom(&listGroups))
	listCommand.Short = "List users or groups with project permissions"
	listCommand.Long = "List users with project permissions, or groups with --group.\n\n" +
		"Shallow alias for " + deep + " list."
	listCommand.Flags().BoolVar(&listGroups, "group", false, "List groups instead of users")

	grantCommand := newProjectPermissionGrantCommand(options, subjectFrom(&grantGroup))
	grantCommand.Use = "grant <key> <user-or-group> <permission>"
	grantCommand.Short = "Grant a project permission to a user or group"
	grantCommand.Long = "Grant a project permission to a user, or to a group with --group.\n\n" +
		"Shallow alias for " + deep + " grant."
	grantCommand.Flags().BoolVar(&grantGroup, "group", false, "Treat the argument as a group rather than a user")

	revokeCommand := newProjectPermissionRevokeCommand(options, subjectFrom(&revokeGroup))
	revokeCommand.Use = "revoke <key> <user-or-group>"
	revokeCommand.Short = "Revoke a project permission from a user or group"
	revokeCommand.Long = "Revoke a project permission from a user, or from a group with --group.\n\n" +
		"Shallow alias for " + deep + " revoke."
	revokeCommand.Flags().BoolVar(&revokeGroup, "group", false, "Treat the argument as a group rather than a user")

	parent.AddCommand(listCommand)
	parent.AddCommand(grantCommand)
	parent.AddCommand(revokeCommand)
}

// alsoAvailableAs is the pointer from a canonical command to its shallow alias.
//
// It exists because the alias already names its canonical path but not the
// reverse, so a reader who arrives at the canonical page — which is where the
// docs send them — has no way to learn the shorter spelling exists. Cross
// references are only useful when they work in both directions.
func alsoAvailableAs(shallowPath string, noun string) string {
	flag := ""
	if noun == "group" {
		flag = " --group"
	}

	return fmt.Sprintf("Also available as %s%s, one level shallower.", shallowPath, flag)
}
