package repocmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/dryrunpreview"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/paging"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/style"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
	reposettings "github.com/vriesdemichael/bitbucket-server-cli/internal/services/reposettings"
)

const permissionLookupLimit = 100

type permissionEntry struct {
	name       string
	display    string
	permission string
}

type permissionSubject struct {
	noun       string
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

type permissionSubjectResolver func() permissionSubject

func fixedPermissionSubject(subject permissionSubject) permissionSubjectResolver {
	return func() permissionSubject { return subject }
}

func newRepoPermissionListCommand(deps Dependencies, repositorySelector *string, subjectFor permissionSubjectResolver) *cobra.Command {
	var listPaging paging.Options

	command := &cobra.Command{
		Use: "list",
		RunE: func(cmd *cobra.Command, args []string) error {
			subject := subjectFor()

			cfg, client, err := deps.LoadConfigAndClient()
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

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), GrantedPermissions{
					Repository: settingsRepositoryOf(repo),
					Subject:    subject.noun,
					Entries:    permissionEntriesFrom(entries),
				})
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

func newRepoPermissionGrantCommand(deps Dependencies, repositorySelector *string, subjectFor permissionSubjectResolver) *cobra.Command {
	return &cobra.Command{
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			subject := subjectFor()

			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveRepositorySettingsReference(*repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := reposettings.NewService(client)
			permission := strings.ToUpper(strings.TrimSpace(args[1]))
			if deps.DryRunEnabled() {
				return runPermissionGrantDryRun(cmd, deps, client, service, subject, repo, args[0], permission)
			}

			if err := subject.grant(cmd.Context(), service, repo, args[0], permission); err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), PermissionGrant{Status: result.OK(), Repository: settingsRepositoryOf(repo), Subject: subject.noun, Name: args[0], Permission: permission})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s to %s%s\n", style.Success.Render("Granted"), permission, subject.tableLabel, style.Resource.Render(args[0]))
			return nil
		},
	}
}

func newRepoPermissionRevokeCommand(deps Dependencies, repositorySelector *string, subjectFor permissionSubjectResolver) *cobra.Command {
	return &cobra.Command{
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			subject := subjectFor()

			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveRepositorySettingsReference(*repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := reposettings.NewService(client)
			if deps.DryRunEnabled() {
				return runPermissionRevokeDryRun(cmd, deps, client, service, subject, repo, args[0])
			}

			if err := subject.revoke(cmd.Context(), service, repo, args[0]); err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), PermissionRevocation{Status: result.OK(), Repository: settingsRepositoryOf(repo), Subject: subject.noun, Name: args[0]})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s for %s%s\n", style.Deleted.Render("Revoked permissions"), subject.tableLabel, style.Resource.Render(args[0]))
			return nil
		},
	}
}

func runPermissionGrantDryRun(
	cmd *cobra.Command,
	deps Dependencies,
	client *openapigenerated.ClientWithResponses,
	service *reposettings.Service,
	subject permissionSubject,
	repo reposettings.RepositoryRef,
	name string,
	permission string,
) error {
	if deps.PermissionChecker != nil {
		checker := deps.PermissionChecker(client)
		if checker != nil {
			if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapi.RepoAdmin); err != nil {
				return err
			}
		}
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

	preview := dryrunpreview.Preview{
		DryRun:       true,
		PlanningMode: dryrunpreview.PlanningModeStateful,
		Capability:   dryrunpreview.CapabilityFull,
		Items: []dryrunpreview.Item{{
			Intent: fmt.Sprintf("repo.permission.%s.grant", subject.noun),
			Target: map[string]any{
				"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug),
				"subject":    subject.noun,
				"name":       name,
				"permission": permission,
			},
			Action:          "update",
			PredictedAction: predicted,
			Supported:       true,
			Reason:          reason,
			Confidence:      dryrunpreview.CapabilityFull,
			RequiredState:   []string{fmt.Sprintf("repository permission %ss list", subject.noun)},
		}},
		Summary: dryrunpreview.Summary{Total: 1, Supported: 1},
	}
	applyPermissionDryRunSummary(&preview, predicted)

	return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
}

func runPermissionRevokeDryRun(
	cmd *cobra.Command,
	deps Dependencies,
	client *openapigenerated.ClientWithResponses,
	service *reposettings.Service,
	subject permissionSubject,
	repo reposettings.RepositoryRef,
	name string,
) error {
	if deps.PermissionChecker != nil {
		checker := deps.PermissionChecker(client)
		if checker != nil {
			if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapi.RepoAdmin); err != nil {
				return err
			}
		}
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

	preview := dryrunpreview.Preview{
		DryRun:       true,
		PlanningMode: dryrunpreview.PlanningModeStateful,
		Capability:   dryrunpreview.CapabilityFull,
		Items: []dryrunpreview.Item{{
			Intent: fmt.Sprintf("repo.permission.%s.revoke", subject.noun),
			Target: map[string]any{
				"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug),
				"subject":    subject.noun,
				"name":       name,
			},
			Action:          "delete",
			PredictedAction: predicted,
			Supported:       true,
			Reason:          reason,
			Confidence:      dryrunpreview.CapabilityFull,
			RequiredState:   []string{fmt.Sprintf("repository permission %ss list", subject.noun)},
		}},
		Summary: dryrunpreview.Summary{Total: 1, Supported: 1},
	}
	applyPermissionDryRunSummary(&preview, predicted)

	return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
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

func newRepoPermissionSubjectCommand(deps Dependencies, repositorySelector *string, subject permissionSubject) *cobra.Command {
	resolver := fixedPermissionSubject(subject)

	group := &cobra.Command{
		Use:   subject.noun + "s",
		Short: strings.ToUpper(subject.noun[:1]) + subject.noun[1:] + " permissions",
	}

	shallow := "bb repo permissions"

	listCommand := newRepoPermissionListCommand(deps, repositorySelector, resolver)
	listCommand.Short = fmt.Sprintf("List %ss with repository permissions", subject.noun)
	listCommand.Long = fmt.Sprintf("List %ss with repository permissions.\n\n%s", subject.noun, alsoAvailableAs(shallow+" list", subject.noun))

	grantCommand := newRepoPermissionGrantCommand(deps, repositorySelector, resolver)
	grantCommand.Use = fmt.Sprintf("grant <%s> <permission>", subject.argPlaceholder())
	grantCommand.Short = fmt.Sprintf("Grant a repository permission to a %s", subject.noun)
	grantCommand.Long = fmt.Sprintf("Grant a repository permission to a %s.\n\n%s", subject.noun, alsoAvailableAs(shallow+" grant", subject.noun))

	revokeCommand := newRepoPermissionRevokeCommand(deps, repositorySelector, resolver)
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

func alsoAvailableAs(shallowPath string, noun string) string {
	flag := ""
	if noun == "group" {
		flag = " --group"
	}

	return fmt.Sprintf("Also available as %s%s, one level shallower.", shallowPath, flag)
}

func addRepoPermissionAliases(parent *cobra.Command, deps Dependencies, repositorySelector *string) {
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

	listCommand := newRepoPermissionListCommand(deps, repositorySelector, subjectFrom(&listGroups))
	listCommand.Short = "List users or groups with repository permissions"
	listCommand.Long = "List users with repository permissions, or groups with --group.\n\n" +
		"Shallow alias for bb repo settings security permissions {users,groups} list."
	listCommand.Flags().BoolVar(&listGroups, "group", false, "List groups instead of users")

	grantCommand := newRepoPermissionGrantCommand(deps, repositorySelector, subjectFrom(&grantGroup))
	grantCommand.Use = "grant <user-or-group> <permission>"
	grantCommand.Short = "Grant a repository permission to a user or group"
	grantCommand.Long = "Grant a repository permission to a user, or to a group with --group.\n\n" +
		"Shallow alias for bb repo settings security permissions {users,groups} grant."
	grantCommand.Flags().BoolVar(&grantGroup, "group", false, "Treat the argument as a group rather than a user")

	revokeCommand := newRepoPermissionRevokeCommand(deps, repositorySelector, subjectFrom(&revokeGroup))
	revokeCommand.Use = "revoke <user-or-group>"
	revokeCommand.Short = "Revoke a repository permission from a user or group"
	revokeCommand.Long = "Revoke a repository permission from a user, or from a group with --group.\n\n" +
		"Shallow alias for bb repo settings security permissions {users,groups} revoke."
	revokeCommand.Flags().BoolVar(&revokeGroup, "group", false, "Treat the argument as a group rather than a user")

	parent.AddCommand(listCommand)
	parent.AddCommand(grantCommand)
	parent.AddCommand(revokeCommand)
}

func newRepoPermissionsCommand(deps Dependencies) *cobra.Command {
	var repositorySelector string

	permissionsCmd := &cobra.Command{
		Use:   "permissions",
		Short: "Repository permission inspection commands",
	}
	permissionsCmd.PersistentFlags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug (defaults to BITBUCKET_PROJECT_KEY + BITBUCKET_REPO_SLUG)")

	showCmd := &cobra.Command{
		Use:   "show",
		Short: "Show the caller's effective permissions on a repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveRepositorySettingsReference(repositorySelector, cfg)
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

			perms, err := checker.InspectRepoPermissions(cmd.Context(), repo.ProjectKey, repo.Slug)
			if err != nil {
				return err
			}

			repoID := fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug)

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), EffectivePermissions{
					Repository:  settingsRepositoryOf(repo),
					Permissions: effectivePermissionsFrom(perms),
				})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Repository: %s\n", repoID)
			for _, level := range []string{"REPO_READ", "REPO_WRITE", "REPO_ADMIN"} {
				fmt.Fprintf(cmd.OutOrStdout(), "%-12s\t%t\n", level, perms[level])
			}
			return nil
		},
	}

	permissionsCmd.AddCommand(showCmd)
	addRepoPermissionAliases(permissionsCmd, deps, &repositorySelector)

	return permissionsCmd
}
