package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/paging"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/style"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
	reposettings "github.com/vriesdemichael/bitbucket-server-cli/internal/services/reposettings"
)

// permissionLookupLimit caps the existing-entry lookup a dry run does to
// predict create vs update vs no-op.
//
// Not a caller-visible flag: the prediction reads state rather than listing it,
// so there is nothing for a caller to page through. It matches the default of
// the list commands so a dry run predicts over the same window a caller sees.
const permissionLookupLimit = 100

// permissionEntry is one row of a repository permission listing, flattened so
// the user and group shapes render through the same code. Groups have no
// display name distinct from their name, so display mirrors name there.
type permissionEntry struct {
	name       string
	display    string
	permission string
}

// permissionSubject is the only thing that differs between the user-facing and
// group-facing halves of the repository permission commands.
//
// grant, revoke and list were written out twice before this — once per subject
// — which is two copies free to drift. They are now built once and registered
// three times: under the canonical deep path for users, for groups, and under
// the shallow `bb repo permissions` alias where --group picks the subject at
// run time instead of the path doing it.
type permissionSubject struct {
	// noun is "user" or "group". It carries the whole human and machine
	// vocabulary: help text, dry-run intents, and the JSON key naming the
	// subject.
	noun string
	// tableLabel prefixes the subject in success lines. Granting to a group
	// reads "to group ops"; granting to a user just reads "to alice", because
	// a bare name is understood to be a person.
	tableLabel string
	list       func(context.Context, *reposettings.Service, reposettings.RepositoryRef, int) ([]permissionEntry, error)
	grant      func(context.Context, *reposettings.Service, reposettings.RepositoryRef, string, string) error
	revoke     func(context.Context, *reposettings.Service, reposettings.RepositoryRef, string) error
}

func userPermissionSubject() permissionSubject {
	return permissionSubject{
		noun:       "user",
		tableLabel: "",
		list: func(ctx context.Context, service *reposettings.Service, repo reposettings.RepositoryRef, limit int) ([]permissionEntry, error) {
			users, err := service.ListRepositoryPermissionUsers(ctx, repo, limit)
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
		grant: func(ctx context.Context, service *reposettings.Service, repo reposettings.RepositoryRef, subject string, permission string) error {
			return service.GrantRepositoryUserPermission(ctx, repo, subject, permission)
		},
		revoke: func(ctx context.Context, service *reposettings.Service, repo reposettings.RepositoryRef, subject string) error {
			return service.RevokeRepositoryUserPermission(ctx, repo, subject)
		},
	}
}

func groupPermissionSubject() permissionSubject {
	return permissionSubject{
		noun:       "group",
		tableLabel: "group ",
		list: func(ctx context.Context, service *reposettings.Service, repo reposettings.RepositoryRef, limit int) ([]permissionEntry, error) {
			groups, err := service.ListRepositoryPermissionGroups(ctx, repo, limit)
			if err != nil {
				return nil, err
			}

			entries := make([]permissionEntry, len(groups))
			for index, group := range groups {
				entries[index] = permissionEntry{name: group.Name, display: group.Name, permission: group.Permission}
			}

			return entries, nil
		},
		grant: func(ctx context.Context, service *reposettings.Service, repo reposettings.RepositoryRef, subject string, permission string) error {
			return service.GrantRepositoryGroupPermission(ctx, repo, subject, permission)
		},
		revoke: func(ctx context.Context, service *reposettings.Service, repo reposettings.RepositoryRef, subject string) error {
			return service.RevokeRepositoryGroupPermission(ctx, repo, subject)
		},
	}
}

// permissionSubjectResolver picks the subject for one invocation.
//
// The deep commands know it from their path and return a constant. The shallow
// alias reads its --group flag, which is the trade the shallow spelling makes:
// one fewer path segment in exchange for the subject living in a flag.
type permissionSubjectResolver func() permissionSubject

func fixedPermissionSubject(subject permissionSubject) permissionSubjectResolver {
	return func() permissionSubject { return subject }
}

func newRepoPermissionListCommand(options *rootOptions, repositorySelector *string, subjectFor permissionSubjectResolver) *cobra.Command {
	var listPaging paging.Options

	command := &cobra.Command{
		Use: "list",
		RunE: func(cmd *cobra.Command, args []string) error {
			subject := subjectFor()

			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveRepositorySettingsReference(*repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := reposettings.NewService(client)
			entries, err := subject.list(cmd.Context(), service, repo, listPaging.ServiceLimit())
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{subject.noun + "s": permissionEntriesPayload(subject, entries)})
			}
			if len(entries) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render(fmt.Sprintf("No %ss with repository permissions found", subject.noun)))
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

// permissionEntriesPayload rebuilds the service-shaped structs for JSON.
//
// The flattened permissionEntry exists for rendering; the JSON contract
// predates it and is what callers parse, so it is reproduced exactly rather
// than replaced by the internal shape.
func permissionEntriesPayload(subject permissionSubject, entries []permissionEntry) any {
	if subject.noun == "group" {
		groups := make([]reposettings.PermissionGroup, len(entries))
		for index, entry := range entries {
			groups[index] = reposettings.PermissionGroup{Name: entry.name, Permission: entry.permission}
		}
		return groups
	}

	users := make([]reposettings.PermissionUser, len(entries))
	for index, entry := range entries {
		display := entry.display
		if display == entry.name {
			display = ""
		}
		users[index] = reposettings.PermissionUser{Name: entry.name, Display: display, Permission: entry.permission}
	}

	return users
}

func newRepoPermissionGrantCommand(options *rootOptions, repositorySelector *string, subjectFor permissionSubjectResolver) *cobra.Command {
	return &cobra.Command{
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			subject := subjectFor()

			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveRepositorySettingsReference(*repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := reposettings.NewService(client)
			permission := strings.ToUpper(strings.TrimSpace(args[1]))
			if options.DryRun {
				return runPermissionGrantDryRun(cmd, options, client, service, subject, repo, args[0], permission)
			}

			if err := subject.grant(cmd.Context(), service, repo, args[0], permission); err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"status": "ok", subject.jsonSubjectKey(): args[0], "permission": permission})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s to %s%s\n", style.Success.Render("Granted"), permission, subject.tableLabel, style.Resource.Render(args[0]))
			return nil
		},
	}
}

func newRepoPermissionRevokeCommand(options *rootOptions, repositorySelector *string, subjectFor permissionSubjectResolver) *cobra.Command {
	return &cobra.Command{
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			subject := subjectFor()

			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveRepositorySettingsReference(*repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := reposettings.NewService(client)
			if options.DryRun {
				return runPermissionRevokeDryRun(cmd, options, client, service, subject, repo, args[0])
			}

			if err := subject.revoke(cmd.Context(), service, repo, args[0]); err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"status": "ok", subject.jsonSubjectKey(): args[0]})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s for %s%s\n", style.Deleted.Render("Revoked permissions"), subject.tableLabel, style.Resource.Render(args[0]))
			return nil
		},
	}
}

// jsonSubjectKey names the subject in machine output. Users are "username"
// rather than "user" because that is what the field has always been called.
func (subject permissionSubject) jsonSubjectKey() string {
	if subject.noun == "user" {
		return "username"
	}

	return subject.noun
}

func runPermissionGrantDryRun(
	cmd *cobra.Command,
	options *rootOptions,
	client *openapigenerated.ClientWithResponses,
	service *reposettings.Service,
	subject permissionSubject,
	repo reposettings.RepositoryRef,
	name string,
	permission string,
) error {
	checker := options.permissionCheckerFor(client)
	if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOADMIN); err != nil {
		return err
	}

	entries, err := subject.list(cmd.Context(), service, repo, permissionLookupLimit)
	if err != nil {
		return err
	}

	predicted := "create"
	reason := fmt.Sprintf("permission grant will create %s permission entry", subject.noun)
	for _, entry := range entries {
		if !strings.EqualFold(strings.TrimSpace(entry.name), strings.TrimSpace(name)) {
			continue
		}

		if strings.EqualFold(strings.TrimSpace(entry.permission), permission) {
			predicted = "no-op"
			reason = fmt.Sprintf("%s already has requested repository permission", subject.noun)
		} else {
			predicted = "update"
			reason = fmt.Sprintf("%s permission will be updated", subject.noun)
		}
		break
	}

	preview := dryRunPreview{
		DryRun:       true,
		PlanningMode: planningModeStateful,
		Capability:   capabilityFull,
		Items: []dryRunItem{{
			Intent: fmt.Sprintf("repo.permission.%s.grant", subject.noun),
			Target: map[string]any{
				"repository":             fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug),
				subject.jsonSubjectKey(): name,
				"permission":             permission,
			},
			Action:          "update",
			PredictedAction: predicted,
			Supported:       true,
			Reason:          reason,
			Confidence:      capabilityFull,
			RequiredState:   []string{fmt.Sprintf("repository permission %ss list", subject.noun)},
		}},
		Summary: dryRunSummary{Total: 1, Supported: 1},
	}
	applyPermissionDryRunSummary(&preview, predicted)

	return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
}

func runPermissionRevokeDryRun(
	cmd *cobra.Command,
	options *rootOptions,
	client *openapigenerated.ClientWithResponses,
	service *reposettings.Service,
	subject permissionSubject,
	repo reposettings.RepositoryRef,
	name string,
) error {
	checker := options.permissionCheckerFor(client)
	if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOADMIN); err != nil {
		return err
	}

	entries, err := subject.list(cmd.Context(), service, repo, permissionLookupLimit)
	if err != nil {
		return err
	}

	predicted := "no-op"
	reason := fmt.Sprintf("%s does not currently have repository permission entry", subject.noun)
	for _, entry := range entries {
		if strings.EqualFold(strings.TrimSpace(entry.name), strings.TrimSpace(name)) {
			predicted = "delete"
			reason = fmt.Sprintf("%s repository permission entry will be removed", subject.noun)
			break
		}
	}

	preview := dryRunPreview{
		DryRun:       true,
		PlanningMode: planningModeStateful,
		Capability:   capabilityFull,
		Items: []dryRunItem{{
			Intent: fmt.Sprintf("repo.permission.%s.revoke", subject.noun),
			Target: map[string]any{
				"repository":             fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug),
				subject.jsonSubjectKey(): name,
			},
			Action:          "delete",
			PredictedAction: predicted,
			Supported:       true,
			Reason:          reason,
			Confidence:      capabilityFull,
			RequiredState:   []string{fmt.Sprintf("repository permission %ss list", subject.noun)},
		}},
		Summary: dryRunSummary{Total: 1, Supported: 1},
	}
	applyPermissionDryRunSummary(&preview, predicted)

	return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
}

func applyPermissionDryRunSummary(preview *dryRunPreview, predicted string) {
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

// newRepoPermissionSubjectCommand builds one of the canonical deep groups —
// `... permissions users` or `... permissions groups`.
func newRepoPermissionSubjectCommand(options *rootOptions, repositorySelector *string, subject permissionSubject) *cobra.Command {
	resolver := fixedPermissionSubject(subject)

	group := &cobra.Command{
		Use:   subject.noun + "s",
		Short: strings.ToUpper(subject.noun[:1]) + subject.noun[1:] + " permissions",
	}

	shallow := "bb repo permissions"

	listCommand := newRepoPermissionListCommand(options, repositorySelector, resolver)
	listCommand.Short = fmt.Sprintf("List %ss with repository permissions", subject.noun)
	listCommand.Long = fmt.Sprintf("List %ss with repository permissions.\n\n%s", subject.noun, alsoAvailableAs(shallow+" list", subject.noun))

	grantCommand := newRepoPermissionGrantCommand(options, repositorySelector, resolver)
	grantCommand.Use = fmt.Sprintf("grant <%s> <permission>", subject.argPlaceholder())
	grantCommand.Short = fmt.Sprintf("Grant a repository permission to a %s", subject.noun)
	grantCommand.Long = fmt.Sprintf("Grant a repository permission to a %s.\n\n%s", subject.noun, alsoAvailableAs(shallow+" grant", subject.noun))

	revokeCommand := newRepoPermissionRevokeCommand(options, repositorySelector, resolver)
	revokeCommand.Use = fmt.Sprintf("revoke <%s>", subject.argPlaceholder())
	revokeCommand.Short = fmt.Sprintf("Revoke a repository permission from a %s", subject.noun)
	revokeCommand.Long = fmt.Sprintf("Revoke a repository permission from a %s.\n\n%s", subject.noun, alsoAvailableAs(shallow+" revoke", subject.noun))

	group.AddCommand(listCommand)
	group.AddCommand(grantCommand)
	group.AddCommand(revokeCommand)

	return group
}

func (subject permissionSubject) argPlaceholder() string {
	if subject.noun == "user" {
		return "username"
	}

	return subject.noun
}

// addRepoPermissionAliases registers the shallow spelling of grant, revoke and
// list on `bb repo permissions`, alongside the `show` that already lives there.
//
// The canonical path is six levels deep. gh caps at three, and depth is
// specifically hostile to agents: every level is a --help round trip to
// discover and an intermediate name to guess wrong. See issue #338.
//
// --group replaces the users/groups path segment. That is the cost of the
// shallower spelling, and it is why the deep paths stay: they name the subject
// unambiguously and remain what the generated reference documents.
func addRepoPermissionAliases(parent *cobra.Command, options *rootOptions, repositorySelector *string) {
	var listGroups bool
	var grantGroup bool
	var revokeGroup bool

	subjectFrom := func(useGroup *bool) permissionSubjectResolver {
		return func() permissionSubject {
			if *useGroup {
				return groupPermissionSubject()
			}

			return userPermissionSubject()
		}
	}

	listCommand := newRepoPermissionListCommand(options, repositorySelector, subjectFrom(&listGroups))
	listCommand.Short = "List users or groups with repository permissions"
	listCommand.Long = "List users with repository permissions, or groups with --group.\n\n" +
		"Shallow alias for bb repo settings security permissions {users,groups} list."
	listCommand.Flags().BoolVar(&listGroups, "group", false, "List groups instead of users")

	grantCommand := newRepoPermissionGrantCommand(options, repositorySelector, subjectFrom(&grantGroup))
	grantCommand.Use = "grant <user-or-group> <permission>"
	grantCommand.Short = "Grant a repository permission to a user or group"
	grantCommand.Long = "Grant a repository permission to a user, or to a group with --group.\n\n" +
		"Shallow alias for bb repo settings security permissions {users,groups} grant."
	grantCommand.Flags().BoolVar(&grantGroup, "group", false, "Treat the argument as a group rather than a user")

	revokeCommand := newRepoPermissionRevokeCommand(options, repositorySelector, subjectFrom(&revokeGroup))
	revokeCommand.Use = "revoke <user-or-group>"
	revokeCommand.Short = "Revoke a repository permission from a user or group"
	revokeCommand.Long = "Revoke a repository permission from a user, or from a group with --group.\n\n" +
		"Shallow alias for bb repo settings security permissions {users,groups} revoke."
	revokeCommand.Flags().BoolVar(&revokeGroup, "group", false, "Treat the argument as a group rather than a user")

	parent.AddCommand(listCommand)
	parent.AddCommand(grantCommand)
	parent.AddCommand(revokeCommand)
}
