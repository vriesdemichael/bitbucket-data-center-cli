package cli

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/style"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
	commentservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/comment"
	reposettings "github.com/vriesdemichael/bitbucket-server-cli/internal/services/reposettings"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/services/repository"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/transport/httpclient"
)

func newRepoCommand(options *rootOptions) *cobra.Command {
	repoCmd := &cobra.Command{
		Use:   "repo",
		Short: "Repository commands",
	}

	var limit int
	var projectKey string
	repoCmd.PersistentFlags().IntVar(&limit, "limit", 25, "Maximum number of results to return")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List repositories",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			client := httpclient.NewFromConfig(cfg)
			service := repository.NewService(client)

			listOptions := repository.ListOptions{Limit: limit}

			var repos []repository.Repository
			if projectKey != "" {
				repos, err = service.ListByProject(cmd.Context(), projectKey, listOptions)
			} else {
				repos, err = service.List(cmd.Context(), listOptions)
			}
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), repos)
			}

			if len(repos) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No repositories found"))
				return nil
			}

			rows := make([][]string, len(repos))
			for i, repo := range repos {
				rows[i] = []string{style.Resource.Render(repo.ProjectKey + "/" + repo.Slug), repo.Name}
			}
			style.WriteTable(cmd.OutOrStdout(), rows)

			return nil
		},
	}
	listCmd.Flags().StringVar(&projectKey, "project", "", "Filter by project key")
	repoCmd.AddCommand(listCmd)

	repoCmd.AddCommand(newRepoSettingsCommand(options))
	repoCmd.AddCommand(newRepoCommentCommand(options))
	repoCmd.AddCommand(newRepoBrowseCommand(options))
	repoCmd.AddCommand(newRepoCloneCommand(options))
	repoCmd.AddCommand(newRepoAdminCommand(options))
	repoCmd.AddCommand(newRepoPermissionsCommand(options))
	repoCmd.AddCommand(newRepoLabelCommand(options))
	repoCmd.AddCommand(newRepoWatchCommand(options))
	repoCmd.AddCommand(newRepoUnwatchCommand(options))
	repoCmd.AddCommand(newRepoDefaultTaskCommand(options))
	repoCmd.AddCommand(newRepoSshKeyCommand(options))

	return repoCmd
}

func newRepoCommentCommand(options *rootOptions) *cobra.Command {
	var repositorySelector string
	var commitID string
	var pullRequestID string

	commentCmd := &cobra.Command{
		Use:   "comment",
		Short: "Comment commands for commits and pull requests",
	}

	commentCmd.PersistentFlags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug (defaults to BITBUCKET_PROJECT_KEY + BITBUCKET_REPO_SLUG)")
	commentCmd.PersistentFlags().StringVar(&commitID, "commit", "", "Commit ID context")
	commentCmd.PersistentFlags().StringVar(&pullRequestID, "pr", "", "Pull request ID context")

	var listLimit int
	var listPath string
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List comments",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			target, err := resolveCommentTarget(repositorySelector, commitID, pullRequestID, cfg)
			if err != nil {
				return err
			}

			service := commentservice.NewService(client)
			comments, err := service.List(cmd.Context(), target, listPath, listLimit)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"context": target.Context(), "comments": comments})
			}

			if len(comments) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No comments found"))
				return nil
			}

			for _, comment := range comments {
				fmt.Fprintln(cmd.OutOrStdout(), formatCommentSummary(comment))
			}

			return nil
		},
	}
	listCmd.Flags().StringVar(&listPath, "path", "", "File path for comment listing scope")
	listCmd.Flags().IntVar(&listLimit, "limit", 25, "Page size for Bitbucket comment list operations")
	_ = listCmd.MarkFlagRequired("path")
	commentCmd.AddCommand(listCmd)

	var createText string
	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a comment",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			target, err := resolveCommentTarget(repositorySelector, commitID, pullRequestID, cfg)
			if err != nil {
				return err
			}

			service := commentservice.NewService(client)
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), target.Repository.ProjectKey, target.Repository.Slug, openapigenerated.REPOREAD); err != nil {
					return err
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityPartial,
					Items: []dryRunItem{{
						Intent:          "repo.comment.create",
						Target:          map[string]any{"context": target.Context(), "text": createText},
						Action:          "create",
						PredictedAction: "create",
						Supported:       true,
						Reason:          "comment will be created",
						Confidence:      capabilityPartial,
						RequiredState:   []string{"comment target context"},
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1, CreateCount: 1},
				}
				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}

			created, err := service.Create(cmd.Context(), target, createText)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"context": target.Context(), "comment": created})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Success.Render("Created comment"), style.Secondary.Render(commentIDString(created)))
			return nil
		},
	}
	createCmd.Flags().StringVar(&createText, "text", "", "Comment text")
	_ = createCmd.MarkFlagRequired("text")
	commentCmd.AddCommand(createCmd)

	var updateCommentID string
	var updateText string
	var updateVersion int32
	updateCmd := &cobra.Command{
		Use:   "update",
		Short: "Update a comment",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			target, err := resolveCommentTarget(repositorySelector, commitID, pullRequestID, cfg)
			if err != nil {
				return err
			}

			service := commentservice.NewService(client)
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), target.Repository.ProjectKey, target.Repository.Slug, openapigenerated.REPOREAD); err != nil {
					return err
				}

				current, err := service.Get(cmd.Context(), target, updateCommentID)
				if err != nil {
					return err
				}
				currentUser := strings.TrimSpace(cfg.BitbucketUsername)

				predicted := "update"
				reason := "comment will be updated"
				blocking := []string{}
				if strings.EqualFold(strings.TrimSpace(safeString(current.Text)), strings.TrimSpace(updateText)) {
					predicted = "no-op"
					reason = "comment text already matches requested value"
				} else if currentUser != "" && !commentOwnedByUser(current, currentUser) {
					predicted = "blocked"
					reason = "comment is owned by another user"
					blocking = []string{"comment owned by another user"}
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityPartial,
					Items: []dryRunItem{{
						Intent:          "repo.comment.update",
						Target:          map[string]any{"context": target.Context(), "id": updateCommentID, "text": updateText},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityPartial,
						RequiredState:   []string{"comment get"},
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

			var version *int32
			if cmd.Flags().Changed("version") {
				version = &updateVersion
			}

			updated, err := service.Update(cmd.Context(), target, updateCommentID, updateText, version)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"context": target.Context(), "comment": updated})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Updated.Render("Updated comment"), style.Secondary.Render(commentIDString(updated)))
			return nil
		},
	}
	updateCmd.Flags().StringVar(&updateCommentID, "id", "", "Comment ID")
	updateCmd.Flags().StringVar(&updateText, "text", "", "Comment text")
	updateCmd.Flags().Int32Var(&updateVersion, "version", 0, "Expected comment version")
	_ = updateCmd.MarkFlagRequired("id")
	_ = updateCmd.MarkFlagRequired("text")
	commentCmd.AddCommand(updateCmd)

	var deleteCommentID string
	var deleteVersion int32
	deleteCmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete a comment",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			target, err := resolveCommentTarget(repositorySelector, commitID, pullRequestID, cfg)
			if err != nil {
				return err
			}

			service := commentservice.NewService(client)
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), target.Repository.ProjectKey, target.Repository.Slug, openapigenerated.REPOREAD); err != nil {
					return err
				}

				current, err := service.Get(cmd.Context(), target, deleteCommentID)
				currentUser := strings.TrimSpace(cfg.BitbucketUsername)
				predicted := "delete"
				reason := "comment will be deleted"
				blocking := []string{}
				if err != nil {
					if apperrors.ExitCode(err) == 4 {
						predicted = "no-op"
						reason = "comment was not found"
					} else {
						return err
					}
				} else if currentUser != "" {
					if !commentOwnedByUser(current, currentUser) {
						predicted = "blocked"
						reason = "comment is owned by another user"
						blocking = []string{"comment owned by another user"}
					}
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityPartial,
					Items: []dryRunItem{{
						Intent:          "repo.comment.delete",
						Target:          map[string]any{"context": target.Context(), "id": deleteCommentID},
						Action:          "delete",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityPartial,
						RequiredState:   []string{"comment get"},
						BlockingReasons: blocking,
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1},
				}
				if predicted == "delete" {
					preview.Summary.DeleteCount = 1
				} else if predicted == "no-op" {
					preview.Summary.NoopCount = 1
				} else {
					preview.Summary.UnknownCount = 1
				}

				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}

			var version *int32
			if cmd.Flags().Changed("version") {
				version = &deleteVersion
			}

			resolvedVersion, err := service.Delete(cmd.Context(), target, deleteCommentID, version)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{
					"context": target.Context(),
					"deleted": map[string]any{"id": deleteCommentID, "version": resolvedVersion},
				})
			}

			if resolvedVersion == nil {
				fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Deleted.Render("Deleted comment"), style.Secondary.Render(strings.TrimSpace(deleteCommentID)))
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s %s\n", style.Deleted.Render("Deleted comment"), style.Secondary.Render(strings.TrimSpace(deleteCommentID)), style.Secondary.Render(fmt.Sprintf("(version=%d)", *resolvedVersion)))
			return nil
		},
	}
	deleteCmd.Flags().StringVar(&deleteCommentID, "id", "", "Comment ID")
	deleteCmd.Flags().Int32Var(&deleteVersion, "version", 0, "Expected comment version")
	_ = deleteCmd.MarkFlagRequired("id")
	commentCmd.AddCommand(deleteCmd)

	return commentCmd
}

func newRepoSettingsCommand(options *rootOptions) *cobra.Command {
	var repositorySelector string

	settingsCmd := &cobra.Command{
		Use:   "settings",
		Short: "Repository settings commands",
	}
	settingsCmd.PersistentFlags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug (defaults to BITBUCKET_PROJECT_KEY + BITBUCKET_REPO_SLUG)")

	securityCmd := &cobra.Command{Use: "security", Short: "Security settings"}
	permissionsCmd := &cobra.Command{Use: "permissions", Short: "Repository permissions"}
	permissionsUsersCmd := &cobra.Command{Use: "users", Short: "User permissions"}
	var permissionsLimit int
	permissionsUsersListCmd := &cobra.Command{
		Use:   "list",
		Short: "List users with repository permissions",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveRepositorySettingsReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := reposettings.NewService(client)
			users, err := service.ListRepositoryPermissionUsers(cmd.Context(), repo, permissionsLimit)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"users": users})
			}
			if len(users) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No users with repository permissions found"))
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
	permissionsUsersListCmd.Flags().IntVar(&permissionsLimit, "limit", 100, "Page size for listing permission users")
	permissionsUsersGrantCmd := &cobra.Command{
		Use:   "grant <username> <permission>",
		Short: "Grant a repository permission to a user",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveRepositorySettingsReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := reposettings.NewService(client)
			permission := strings.ToUpper(strings.TrimSpace(args[1]))
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOADMIN); err != nil {
					return err
				}

				users, err := service.ListRepositoryPermissionUsers(cmd.Context(), repo, permissionsLimit)
				if err != nil {
					return err
				}

				predicted := "create"
				reason := "permission grant will create user permission entry"
				for _, user := range users {
					if strings.EqualFold(strings.TrimSpace(user.Name), strings.TrimSpace(args[0])) {
						if strings.EqualFold(strings.TrimSpace(user.Permission), permission) {
							predicted = "no-op"
							reason = "user already has requested repository permission"
						} else {
							predicted = "update"
							reason = "user permission will be updated"
						}
						break
					}
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "repo.permission.user.grant",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "username": args[0], "permission": permission},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"repository permission users list"},
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

			if err := service.GrantRepositoryUserPermission(cmd.Context(), repo, args[0], permission); err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"status": "ok", "username": args[0], "permission": permission})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s to %s\n", style.Success.Render("Granted"), permission, style.Resource.Render(args[0]))
			return nil
		},
	}
	permissionsUsersRevokeCmd := &cobra.Command{
		Use:   "revoke <username>",
		Short: "Revoke a repository permission from a user",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveRepositorySettingsReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := reposettings.NewService(client)
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOADMIN); err != nil {
					return err
				}

				users, err := service.ListRepositoryPermissionUsers(cmd.Context(), repo, permissionsLimit)
				if err != nil {
					return err
				}

				predicted := "no-op"
				reason := "user does not currently have repository permission entry"
				for _, user := range users {
					if strings.EqualFold(strings.TrimSpace(user.Name), strings.TrimSpace(args[0])) {
						predicted = "delete"
						reason = "user repository permission entry will be removed"
						break
					}
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "repo.permission.user.revoke",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "username": args[0]},
						Action:          "delete",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"repository permission users list"},
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

			if err := service.RevokeRepositoryUserPermission(cmd.Context(), repo, args[0]); err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"status": "ok", "username": args[0]})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s for %s\n", style.Deleted.Render("Revoked permissions"), style.Resource.Render(args[0]))
			return nil
		},
	}

	permissionsGroupsCmd := &cobra.Command{Use: "groups", Short: "Group permissions"}
	permissionsGroupsListCmd := &cobra.Command{
		Use:   "list",
		Short: "List groups with repository permissions",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveRepositorySettingsReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := reposettings.NewService(client)
			groups, err := service.ListRepositoryPermissionGroups(cmd.Context(), repo, permissionsLimit)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"groups": groups})
			}
			if len(groups) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No groups with repository permissions found"))
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
	permissionsGroupsListCmd.Flags().IntVar(&permissionsLimit, "limit", 100, "Page size for listing permission groups")

	permissionsGroupsGrantCmd := &cobra.Command{
		Use:   "grant <group> <permission>",
		Short: "Grant a repository permission to a group",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveRepositorySettingsReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := reposettings.NewService(client)
			permission := strings.ToUpper(strings.TrimSpace(args[1]))
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOADMIN); err != nil {
					return err
				}

				groups, err := service.ListRepositoryPermissionGroups(cmd.Context(), repo, permissionsLimit)
				if err != nil {
					return err
				}

				predicted := "create"
				reason := "permission grant will create group permission entry"
				for _, group := range groups {
					if strings.EqualFold(strings.TrimSpace(group.Name), strings.TrimSpace(args[0])) {
						if strings.EqualFold(strings.TrimSpace(group.Permission), permission) {
							predicted = "no-op"
							reason = "group already has requested repository permission"
						} else {
							predicted = "update"
							reason = "group permission will be updated"
						}
						break
					}
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "repo.permission.group.grant",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "group": args[0], "permission": permission},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"repository permission groups list"},
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

			if err := service.GrantRepositoryGroupPermission(cmd.Context(), repo, args[0], permission); err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"status": "ok", "group": args[0], "permission": permission})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s to group %s\n", style.Success.Render("Granted"), permission, style.Resource.Render(args[0]))
			return nil
		},
	}

	permissionsGroupsRevokeCmd := &cobra.Command{
		Use:   "revoke <group>",
		Short: "Revoke a repository permission from a group",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveRepositorySettingsReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := reposettings.NewService(client)
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOADMIN); err != nil {
					return err
				}

				groups, err := service.ListRepositoryPermissionGroups(cmd.Context(), repo, permissionsLimit)
				if err != nil {
					return err
				}

				predicted := "no-op"
				reason := "group does not currently have repository permission entry"
				if slices.ContainsFunc(groups, func(group reposettings.PermissionGroup) bool {
					return strings.EqualFold(strings.TrimSpace(group.Name), strings.TrimSpace(args[0]))
				}) {
					predicted = "delete"
					reason = "group repository permission entry will be removed"
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "repo.permission.group.revoke",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "group": args[0]},
						Action:          "delete",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"repository permission groups list"},
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

			if err := service.RevokeRepositoryGroupPermission(cmd.Context(), repo, args[0]); err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"status": "ok", "group": args[0]})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s for group %s\n", style.Deleted.Render("Revoked permissions"), style.Resource.Render(args[0]))
			return nil
		},
	}

	permissionsUsersCmd.AddCommand(permissionsUsersListCmd)
	permissionsUsersCmd.AddCommand(permissionsUsersGrantCmd)
	permissionsUsersCmd.AddCommand(permissionsUsersRevokeCmd)
	permissionsCmd.AddCommand(permissionsUsersCmd)

	permissionsGroupsCmd.AddCommand(permissionsGroupsListCmd)
	permissionsGroupsCmd.AddCommand(permissionsGroupsGrantCmd)
	permissionsGroupsCmd.AddCommand(permissionsGroupsRevokeCmd)
	permissionsCmd.AddCommand(permissionsGroupsCmd)

	securityCmd.AddCommand(permissionsCmd)
	settingsCmd.AddCommand(securityCmd)

	workflowCmd := &cobra.Command{Use: "workflow", Short: "Workflow settings"}
	webhooksCmd := &cobra.Command{Use: "webhooks", Short: "Repository webhooks"}
	webhooksListCmd := &cobra.Command{
		Use:   "list",
		Short: "List repository webhooks",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveRepositorySettingsReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := reposettings.NewService(client)
			webhooks, err := service.ListRepositoryWebhooks(cmd.Context(), repo)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"webhooks": webhooks.Payload})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %d\n", style.Label.Render("Webhooks configured:"), webhooks.Count)
			return nil
		},
	}
	var webhookEvents []string
	var webhookActive bool
	webhooksCreateCmd := &cobra.Command{
		Use:   "create <name> <url>",
		Short: "Create a repository webhook",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveRepositorySettingsReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := reposettings.NewService(client)
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOADMIN); err != nil {
					return err
				}

				webhooks, err := service.ListRepositoryWebhooks(cmd.Context(), repo)
				if err != nil {
					return err
				}

				predicted := "create"
				reason := "webhook will be created"
				blocking := []string{}
				if webhookExistsByNameAndURL(webhooks.Payload, args[0], args[1]) {
					predicted = "conflict"
					reason = "webhook with the same name and URL already exists"
					blocking = []string{"webhook already exists"}
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "repo.webhook.create",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "name": args[0], "url": args[1], "events": webhookEvents, "active": webhookActive},
						Action:          "create",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"repository webhooks list"},
						BlockingReasons: blocking,
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1},
				}
				switch predicted {
				case "create":
					preview.Summary.CreateCount = 1
				case "conflict":
					preview.Summary.UnknownCount = 1
				default:
					preview.Summary.UnknownCount = 1
				}

				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}

			payload, err := service.CreateRepositoryWebhook(cmd.Context(), repo, reposettings.WebhookCreateInput{
				Name:   args[0],
				URL:    args[1],
				Events: webhookEvents,
				Active: webhookActive,
			})
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"status": "ok", "webhook": payload})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Success.Render("Webhook created:"), style.Resource.Render(args[0]))
			return nil
		},
	}
	webhooksCreateCmd.Flags().StringSliceVar(&webhookEvents, "event", []string{"repo:refs_changed"}, "Webhook event(s) to subscribe to")
	webhooksCreateCmd.Flags().BoolVar(&webhookActive, "active", true, "Whether the webhook is active")
	webhooksDeleteCmd := &cobra.Command{
		Use:   "delete <webhook-id>",
		Short: "Delete a repository webhook",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveRepositorySettingsReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := reposettings.NewService(client)
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOADMIN); err != nil {
					return err
				}

				webhooks, err := service.ListRepositoryWebhooks(cmd.Context(), repo)
				if err != nil {
					return err
				}

				predicted := "no-op"
				reason := "webhook id was not found in repository"
				if webhookExistsByID(webhooks.Payload, args[0]) {
					predicted = "delete"
					reason = "webhook will be deleted"
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "repo.webhook.delete",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "webhook_id": args[0]},
						Action:          "delete",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"repository webhooks list"},
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

			if err := service.DeleteRepositoryWebhook(cmd.Context(), repo, args[0]); err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"status": "ok", "webhook_id": args[0]})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Deleted.Render("Webhook deleted:"), style.Resource.Render(args[0]))
			return nil
		},
	}
	webhooksCmd.AddCommand(webhooksListCmd)
	webhooksCmd.AddCommand(webhooksCreateCmd)
	webhooksCmd.AddCommand(webhooksDeleteCmd)
	workflowCmd.AddCommand(webhooksCmd)
	settingsCmd.AddCommand(workflowCmd)

	pullRequestsCmd := &cobra.Command{Use: "pull-requests", Short: "Pull request settings"}
	pullRequestsGetCmd := &cobra.Command{
		Use:   "get",
		Short: "Get repository pull-request settings",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveRepositorySettingsReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := reposettings.NewService(client)
			settings, err := service.GetRepositoryPullRequestSettings(cmd.Context(), repo)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"pull_request_settings": settings})
			}

			requiredTasks := false
			if value, ok := settings["requiredAllTasksComplete"].(bool); ok {
				requiredTasks = value
			}
			requiredApprovals := "disabled"
			if section, ok := settings["requiredApprovers"].(map[string]any); ok {
				enabled, _ := section["enabled"].(bool)
				if enabled {
					switch count := section["count"].(type) {
					case string:
						requiredApprovals = count
					case float64:
						requiredApprovals = fmt.Sprintf("%.0f", count)
					default:
						requiredApprovals = "enabled"
					}
				}
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %t\n", style.Label.Render("Required tasks complete:"), requiredTasks)
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Label.Render("Required approvers:"), requiredApprovals)

			if mergeConfig, ok := settings["mergeConfig"].(map[string]any); ok {
				if strategies, ok := mergeConfig["strategies"].([]any); ok {
					fmt.Fprintf(cmd.OutOrStdout(), "%s %d\n", style.Label.Render("Available merge strategies:"), len(strategies))
					for _, s := range strategies {
						if sm, ok := s.(map[string]any); ok {
							enabled := ""
							if en, ok := sm["enabled"].(bool); ok && en {
								enabled = "*"
							}
							fmt.Fprintf(cmd.OutOrStdout(), "- %s%s (%s)\n", sm["id"], enabled, sm["name"])
						}
					}
				}
			}
			return nil
		},
	}

	mergeChecksCmd := &cobra.Command{
		Use:   "merge-checks",
		Short: "Manage repository merge checks",
	}
	mergeChecksListCmd := &cobra.Command{
		Use:   "list",
		Short: "List configured merge checks",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveRepositorySettingsReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := reposettings.NewService(client)
			checks, err := service.ListRequiredBuildsMergeChecks(cmd.Context(), repo)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"merge_checks": checks})
			}

			fmt.Fprintln(cmd.OutOrStdout(), "Configured merge checks:")
			if checksMap, ok := checks.(map[string]any); ok {
				if values, ok := checksMap["values"].([]any); ok {
					for _, check := range values {
						fmt.Fprintf(cmd.OutOrStdout(), "- %v\n", check)
					}
					return nil
				}
			}

			if checksArr, ok := checks.([]any); ok {
				for _, check := range checksArr {
					fmt.Fprintf(cmd.OutOrStdout(), "- %v\n", check)
				}
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "- %v\n", checks)
			}
			return nil
		},
	}
	mergeChecksCmd.AddCommand(mergeChecksListCmd)
	pullRequestsCmd.AddCommand(mergeChecksCmd)
	var requiredAllTasksComplete bool
	var requiredApproversCount int
	pullRequestsUpdateCmd := &cobra.Command{
		Use:   "update",
		Short: "Update repository pull-request settings",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveRepositorySettingsReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := reposettings.NewService(client)
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOADMIN); err != nil {
					return err
				}

				currentSettings, err := service.GetRepositoryPullRequestSettings(cmd.Context(), repo)
				if err != nil {
					return err
				}
				current := false
				if value, ok := currentSettings["requiredAllTasksComplete"].(bool); ok {
					current = value
				}

				predicted := "update"
				reason := "required-all-tasks-complete setting will be updated"
				if current == requiredAllTasksComplete {
					predicted = "no-op"
					reason = "required-all-tasks-complete setting already has requested value"
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "repo.pull-request-settings.update",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "required_all_tasks_complete": requiredAllTasksComplete},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"repository pull-request settings"},
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1},
				}
				switch predicted {
				case "update":
					preview.Summary.UpdateCount = 1
				case "no-op":
					preview.Summary.NoopCount = 1
				default:
					preview.Summary.UnknownCount = 1
				}

				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}

			settings, err := service.UpdateRepositoryPullRequestRequiredAllTasks(cmd.Context(), repo, requiredAllTasksComplete)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"status": "ok", "pull_request_settings": settings})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Updated pull-request settings: requiredAllTasksComplete=%t\n", requiredAllTasksComplete)
			return nil
		},
	}
	pullRequestsUpdateCmd.Flags().BoolVar(&requiredAllTasksComplete, "required-all-tasks-complete", false, "Require all pull-request tasks to be completed before merge")
	pullRequestsUpdateApproversCmd := &cobra.Command{
		Use:   "update-approvers",
		Short: "Update required approvers count",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveRepositorySettingsReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := reposettings.NewService(client)
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOADMIN); err != nil {
					return err
				}

				currentSettings, err := service.GetRepositoryPullRequestSettings(cmd.Context(), repo)
				if err != nil {
					return err
				}

				currentCount := -1
				if section, ok := currentSettings["requiredApprovers"].(map[string]any); ok {
					enabled, _ := section["enabled"].(bool)
					if enabled {
						switch count := section["count"].(type) {
						case string:
							if value, convErr := strconv.Atoi(strings.TrimSpace(count)); convErr == nil {
								currentCount = value
							}
						case float64:
							currentCount = int(count)
						}
					} else {
						currentCount = 0
					}
				}

				predicted := "update"
				reason := "required approvers setting will be updated"
				if currentCount == requiredApproversCount {
					predicted = "no-op"
					reason = "required approvers setting already has requested value"
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "repo.pull-request-settings.update-approvers",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "count": requiredApproversCount},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"repository pull-request settings"},
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1},
				}
				switch predicted {
				case "update":
					preview.Summary.UpdateCount = 1
				case "no-op":
					preview.Summary.NoopCount = 1
				default:
					preview.Summary.UnknownCount = 1
				}

				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}

			settings, err := service.UpdateRepositoryPullRequestRequiredApproversCount(cmd.Context(), repo, requiredApproversCount)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"status": "ok", "pull_request_settings": settings})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Updated pull-request settings: requiredApprovers=%d\n", requiredApproversCount)
			return nil
		},
	}
	pullRequestsUpdateApproversCmd.Flags().IntVar(&requiredApproversCount, "count", 2, "Required approvers count (0 disables check)")
	pullRequestsSetStrategyCmd := &cobra.Command{
		Use:   "set-strategy <strategy-id>",
		Short: "Set default merge strategy",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveRepositorySettingsReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := reposettings.NewService(client)
			mergeStrategyID := args[0]
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOADMIN); err != nil {
					return err
				}

				currentSettings, err := service.GetRepositoryPullRequestSettings(cmd.Context(), repo)
				if err != nil {
					return err
				}

				currentStrategyID := ""
				if mergeConfig, ok := currentSettings["mergeConfig"].(map[string]any); ok {
					if defaultStrategy, ok := mergeConfig["defaultStrategy"].(map[string]any); ok {
						if value, ok := defaultStrategy["id"].(string); ok {
							currentStrategyID = strings.TrimSpace(value)
						}
					}
				}

				predicted := "update"
				reason := "default merge strategy will be updated"
				if strings.EqualFold(currentStrategyID, mergeStrategyID) {
					predicted = "no-op"
					reason = "default merge strategy already matches requested strategy"
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "repo.pull-request-settings.set-strategy",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "strategy_id": mergeStrategyID},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"repository pull-request settings"},
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1},
				}
				switch predicted {
				case "update":
					preview.Summary.UpdateCount = 1
				case "no-op":
					preview.Summary.NoopCount = 1
				default:
					preview.Summary.UnknownCount = 1
				}

				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}

			settings := map[string]any{
				"mergeConfig": map[string]any{
					"defaultStrategy": map[string]any{
						"id": mergeStrategyID,
					},
				},
			}

			updated, err := service.UpdateRepositoryPullRequestSettings(cmd.Context(), repo, settings)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"status": "ok", "pull_request_settings": updated})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Updated default merge strategy to %s\n", mergeStrategyID)
			return nil
		},
	}
	pullRequestsCmd.AddCommand(pullRequestsSetStrategyCmd)

	pullRequestsCmd.AddCommand(pullRequestsGetCmd)
	pullRequestsCmd.AddCommand(pullRequestsUpdateCmd)
	pullRequestsCmd.AddCommand(pullRequestsUpdateApproversCmd)
	settingsCmd.AddCommand(pullRequestsCmd)
	settingsCmd.AddCommand(newRepoSettingsAutoMergeCommand(options))
	settingsCmd.AddCommand(newRepoSettingsAutoDeclineCommand(options))

	return settingsCmd
}

func commentOwnedByUser(comment openapigenerated.RestComment, username string) bool {
	trimmed := strings.TrimSpace(username)
	if trimmed == "" || comment.Author == nil {
		return false
	}
	if comment.Author.Name != nil && strings.EqualFold(strings.TrimSpace(*comment.Author.Name), trimmed) {
		return true
	}
	if comment.Author.Slug != nil && strings.EqualFold(strings.TrimSpace(*comment.Author.Slug), trimmed) {
		return true
	}
	return false
}

func webhookEntries(payload any) []map[string]any {
	entries := make([]map[string]any, 0)
	appendEntry := func(value any) {
		if object, ok := value.(map[string]any); ok {
			entries = append(entries, object)
		}
	}

	switch typed := payload.(type) {
	case []any:
		for _, value := range typed {
			appendEntry(value)
		}
	case map[string]any:
		if values, ok := typed["values"].([]any); ok {
			for _, value := range values {
				appendEntry(value)
			}
		} else {
			appendEntry(typed)
		}
	}

	return entries
}

func webhookExistsByNameAndURL(payload any, name, url string) bool {
	trimmedName := strings.TrimSpace(name)
	trimmedURL := strings.TrimSpace(url)
	for _, entry := range webhookEntries(payload) {
		entryName, _ := entry["name"].(string)
		entryURL, _ := entry["url"].(string)
		if strings.EqualFold(strings.TrimSpace(entryName), trimmedName) && strings.EqualFold(strings.TrimSpace(entryURL), trimmedURL) {
			return true
		}
	}

	return false
}

func webhookExistsByID(payload any, webhookID string) bool {
	trimmedID := strings.TrimSpace(webhookID)
	for _, entry := range webhookEntries(payload) {
		switch value := entry["id"].(type) {
		case string:
			if strings.EqualFold(strings.TrimSpace(value), trimmedID) {
				return true
			}
		case float64:
			if strings.EqualFold(strconv.FormatInt(int64(value), 10), trimmedID) {
				return true
			}
		}
	}

	return false
}

func newRepoPermissionsCommand(options *rootOptions) *cobra.Command {
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
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveRepositorySettingsReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			checker := options.permissionCheckerFor(client)
			perms, err := checker.InspectRepoPermissions(cmd.Context(), repo.ProjectKey, repo.Slug)
			if err != nil {
				return err
			}

			repoID := fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug)

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{
					"repository":  repoID,
					"permissions": perms,
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
	return permissionsCmd
}

func newRepoSettingsAutoMergeCommand(options *rootOptions) *cobra.Command {
	var repositorySelector string

	autoMergeCmd := &cobra.Command{
		Use:   "auto-merge",
		Short: "Manage repository auto-merge settings",
	}
	autoMergeCmd.PersistentFlags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug (defaults to BITBUCKET_PROJECT_KEY + BITBUCKET_REPO_SLUG)")

	getCmd := &cobra.Command{
		Use:   "get",
		Short: "Get repository auto-merge settings",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}
			repo, err := resolveRepositorySettingsReference(repositorySelector, cfg)
			if err != nil {
				return err
			}
			service := reposettings.NewService(client)
			res, err := service.GetRepositoryAutoMergeSettings(cmd.Context(), repo)
			if err != nil {
				return err
			}
			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), res)
			}
			enabled := false
			if res != nil && res.Enabled != nil {
				enabled = *res.Enabled
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Auto-merge enabled: %t\n", enabled)
			return nil
		},
	}

	var setEnabled bool
	setCmd := &cobra.Command{
		Use:   "set",
		Short: "Set repository auto-merge settings",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}
			repo, err := resolveRepositorySettingsReference(repositorySelector, cfg)
			if err != nil {
				return err
			}
			service := reposettings.NewService(client)
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOADMIN); err != nil {
					return err
				}
				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "repo.settings.auto-merge.set",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "enabled": setEnabled},
						Action:          "update",
						PredictedAction: "update",
						Supported:       true,
						Reason:          "auto-merge settings will be updated",
						Confidence:      capabilityFull,
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1, UpdateCount: 1},
				}
				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}
			res, err := service.UpdateRepositoryAutoMergeSettings(cmd.Context(), repo, setEnabled)
			if err != nil {
				return err
			}
			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), res)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Updated auto-merge settings: enabled=%t\n", setEnabled)
			return nil
		},
	}
	setCmd.Flags().BoolVar(&setEnabled, "enabled", false, "Enable or disable auto-merge")
	_ = setCmd.MarkFlagRequired("enabled")

	deleteCmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete repository auto-merge settings",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}
			repo, err := resolveRepositorySettingsReference(repositorySelector, cfg)
			if err != nil {
				return err
			}
			service := reposettings.NewService(client)
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOADMIN); err != nil {
					return err
				}
				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "repo.settings.auto-merge.delete",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug)},
						Action:          "delete",
						PredictedAction: "delete",
						Supported:       true,
						Reason:          "auto-merge settings will be deleted",
						Confidence:      capabilityFull,
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1, DeleteCount: 1},
				}
				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}
			err = service.DeleteRepositoryAutoMergeSettings(cmd.Context(), repo)
			if err != nil {
				return err
			}
			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"status": "deleted"})
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Deleted auto-merge settings")
			return nil
		},
	}

	autoMergeCmd.AddCommand(getCmd)
	autoMergeCmd.AddCommand(setCmd)
	autoMergeCmd.AddCommand(deleteCmd)
	return autoMergeCmd
}

func newRepoSettingsAutoDeclineCommand(options *rootOptions) *cobra.Command {
	var repositorySelector string

	autoDeclineCmd := &cobra.Command{
		Use:   "auto-decline",
		Short: "Manage repository auto-decline settings",
	}
	autoDeclineCmd.PersistentFlags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug (defaults to BITBUCKET_PROJECT_KEY + BITBUCKET_REPO_SLUG)")

	getCmd := &cobra.Command{
		Use:   "get",
		Short: "Get repository auto-decline settings",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}
			repo, err := resolveRepositorySettingsReference(repositorySelector, cfg)
			if err != nil {
				return err
			}
			service := reposettings.NewService(client)
			res, err := service.GetRepositoryAutoDeclineSettings(cmd.Context(), repo)
			if err != nil {
				return err
			}
			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), res)
			}
			enabled := false
			inactivityWeeks := int32(0)
			if res != nil {
				if res.Enabled != nil {
					enabled = *res.Enabled
				}
				if res.InactivityWeeks != nil {
					inactivityWeeks = *res.InactivityWeeks
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Auto-decline enabled: %t\n", enabled)
			if enabled {
				fmt.Fprintf(cmd.OutOrStdout(), "Inactivity weeks: %d\n", inactivityWeeks)
			}
			return nil
		},
	}

	var setEnabled bool
	var inactivityWeeks int32
	setCmd := &cobra.Command{
		Use:   "set",
		Short: "Set repository auto-decline settings",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}
			repo, err := resolveRepositorySettingsReference(repositorySelector, cfg)
			if err != nil {
				return err
			}
			if setEnabled && inactivityWeeks <= 0 {
				return apperrors.New(apperrors.KindValidation, "inactivity weeks must be > 0 when enabled is true", nil)
			}
			service := reposettings.NewService(client)
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOADMIN); err != nil {
					return err
				}
				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "repo.settings.auto-decline.set",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "enabled": setEnabled, "inactivityWeeks": inactivityWeeks},
						Action:          "update",
						PredictedAction: "update",
						Supported:       true,
						Reason:          "auto-decline settings will be updated",
						Confidence:      capabilityFull,
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1, UpdateCount: 1},
				}
				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}
			res, err := service.UpdateRepositoryAutoDeclineSettings(cmd.Context(), repo, setEnabled, inactivityWeeks)
			if err != nil {
				return err
			}
			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), res)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Updated auto-decline settings: enabled=%t inactivityWeeks=%d\n", setEnabled, inactivityWeeks)
			return nil
		},
	}
	setCmd.Flags().BoolVar(&setEnabled, "enabled", false, "Enable or disable auto-decline")
	setCmd.Flags().Int32Var(&inactivityWeeks, "inactivity-weeks", 0, "Number of inactivity weeks before auto-decline")
	_ = setCmd.MarkFlagRequired("enabled")

	deleteCmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete repository auto-decline settings",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}
			repo, err := resolveRepositorySettingsReference(repositorySelector, cfg)
			if err != nil {
				return err
			}
			service := reposettings.NewService(client)
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOADMIN); err != nil {
					return err
				}
				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "repo.settings.auto-decline.delete",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug)},
						Action:          "delete",
						PredictedAction: "delete",
						Supported:       true,
						Reason:          "auto-decline settings will be deleted",
						Confidence:      capabilityFull,
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1, DeleteCount: 1},
				}
				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}
			err = service.DeleteRepositoryAutoDeclineSettings(cmd.Context(), repo)
			if err != nil {
				return err
			}
			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"status": "deleted"})
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Deleted auto-decline settings")
			return nil
		},
	}

	autoDeclineCmd.AddCommand(getCmd)
	autoDeclineCmd.AddCommand(setCmd)
	autoDeclineCmd.AddCommand(deleteCmd)
	return autoDeclineCmd
}

func newRepoLabelCommand(options *rootOptions) *cobra.Command {
	var repositorySelector string

	labelCmd := &cobra.Command{
		Use:   "label",
		Short: "Manage repository labels",
	}
	labelCmd.PersistentFlags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug (defaults to BITBUCKET_PROJECT_KEY + BITBUCKET_REPO_SLUG)")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List repository labels",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}
			repo, err := resolveRepositorySettingsReference(repositorySelector, cfg)
			if err != nil {
				return err
			}
			service := reposettings.NewService(client)
			labels, err := service.ListRepositoryLabels(cmd.Context(), repo)
			if err != nil {
				return err
			}
			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"labels": labels})
			}
			if len(labels) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No labels found"))
				return nil
			}
			for _, label := range labels {
				fmt.Fprintln(cmd.OutOrStdout(), style.Resource.Render(label))
			}
			return nil
		},
	}

	addCmd := &cobra.Command{
		Use:   "add <label>",
		Short: "Add a repository label",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}
			repo, err := resolveRepositorySettingsReference(repositorySelector, cfg)
			if err != nil {
				return err
			}
			service := reposettings.NewService(client)
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOWRITE); err != nil {
					return err
				}
				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "repo.label.add",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "label": args[0]},
						Action:          "create",
						PredictedAction: "create",
						Supported:       true,
						Reason:          "label will be added to the repository",
						Confidence:      capabilityFull,
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1, CreateCount: 1},
				}
				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}
			err = service.AddRepositoryLabel(cmd.Context(), repo, args[0])
			if err != nil {
				return err
			}
			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"status": "ok", "label": args[0]})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Success.Render("Added label:"), style.Resource.Render(args[0]))
			return nil
		},
	}

	removeCmd := &cobra.Command{
		Use:   "remove <label>",
		Short: "Remove a repository label",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}
			repo, err := resolveRepositorySettingsReference(repositorySelector, cfg)
			if err != nil {
				return err
			}
			service := reposettings.NewService(client)
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOWRITE); err != nil {
					return err
				}
				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "repo.label.remove",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "label": args[0]},
						Action:          "delete",
						PredictedAction: "delete",
						Supported:       true,
						Reason:          "label will be removed from the repository",
						Confidence:      capabilityFull,
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1, DeleteCount: 1},
				}
				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}
			err = service.RemoveRepositoryLabel(cmd.Context(), repo, args[0])
			if err != nil {
				return err
			}
			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"status": "ok", "label": args[0]})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Deleted.Render("Removed label:"), style.Resource.Render(args[0]))
			return nil
		},
	}

	labelCmd.AddCommand(listCmd)
	labelCmd.AddCommand(addCmd)
	labelCmd.AddCommand(removeCmd)
	return labelCmd
}

func newRepoWatchCommand(options *rootOptions) *cobra.Command {
	var repositorySelector string

	watchCmd := &cobra.Command{
		Use:   "watch",
		Short: "Watch repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}
			repo, err := resolveRepositorySettingsReference(repositorySelector, cfg)
			if err != nil {
				return err
			}
			service := reposettings.NewService(client)
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOREAD); err != nil {
					return err
				}
				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "repo.watch",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug)},
						Action:          "update",
						PredictedAction: "update",
						Supported:       true,
						Reason:          "user will watch repository",
						Confidence:      capabilityFull,
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1, UpdateCount: 1},
				}
				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}
			err = service.WatchRepository(cmd.Context(), repo)
			if err != nil {
				return err
			}
			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"status": "ok", "watching": true})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Watching repository %s/%s\n", repo.ProjectKey, repo.Slug)
			return nil
		},
	}
	watchCmd.Flags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug (defaults to BITBUCKET_PROJECT_KEY + BITBUCKET_REPO_SLUG)")
	return watchCmd
}

func newRepoUnwatchCommand(options *rootOptions) *cobra.Command {
	var repositorySelector string

	unwatchCmd := &cobra.Command{
		Use:   "unwatch",
		Short: "Unwatch repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}
			repo, err := resolveRepositorySettingsReference(repositorySelector, cfg)
			if err != nil {
				return err
			}
			service := reposettings.NewService(client)
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOREAD); err != nil {
					return err
				}
				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "repo.unwatch",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug)},
						Action:          "delete",
						PredictedAction: "delete",
						Supported:       true,
						Reason:          "user will unwatch repository",
						Confidence:      capabilityFull,
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1, DeleteCount: 1},
				}
				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}
			err = service.UnwatchRepository(cmd.Context(), repo)
			if err != nil {
				return err
			}
			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"status": "ok", "watching": false})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Unwatched repository %s/%s\n", repo.ProjectKey, repo.Slug)
			return nil
		},
	}
	unwatchCmd.Flags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug (defaults to BITBUCKET_PROJECT_KEY + BITBUCKET_REPO_SLUG)")
	return unwatchCmd
}

func newRepoDefaultTaskCommand(options *rootOptions) *cobra.Command {
	var repositorySelector string

	defaultTaskCmd := &cobra.Command{
		Use:   "default-task",
		Short: "Manage repository default checklist tasks",
	}
	defaultTaskCmd.PersistentFlags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug (defaults to BITBUCKET_PROJECT_KEY + BITBUCKET_REPO_SLUG)")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List default checklist tasks",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}
			repo, err := resolveRepositorySettingsReference(repositorySelector, cfg)
			if err != nil {
				return err
			}
			service := reposettings.NewService(client)
			tasks, err := service.ListDefaultTasks(cmd.Context(), repo)
			if err != nil {
				return err
			}
			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), tasks)
			}
			if len(tasks) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No default checklist tasks found"))
				return nil
			}
			rows := make([][]string, len(tasks))
			for i, t := range tasks {
				idStr := ""
				if t.Id != nil {
					idStr = strconv.FormatInt(int64(*t.Id), 10)
				}
				desc := ""
				if t.Description != nil {
					desc = *t.Description
				}
				src := "ANY"
				if t.SourceMatcher != nil && t.SourceMatcher.Id != nil {
					src = *t.SourceMatcher.Id
				}
				tgt := "ANY"
				if t.TargetMatcher != nil && t.TargetMatcher.Id != nil {
					tgt = *t.TargetMatcher.Id
				}
				rows[i] = []string{style.Secondary.Render(idStr), style.Resource.Render(desc), src, tgt}
			}
			style.WriteTable(cmd.OutOrStdout(), rows)
			return nil
		},
	}

	var sourceRef string
	var targetRef string
	addCmd := &cobra.Command{
		Use:   "add <description>",
		Short: "Add a default checklist task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}
			repo, err := resolveRepositorySettingsReference(repositorySelector, cfg)
			if err != nil {
				return err
			}
			service := reposettings.NewService(client)
			var src *string
			if cmd.Flags().Changed("source-ref") {
				src = &sourceRef
			}
			var tgt *string
			if cmd.Flags().Changed("target-ref") {
				tgt = &targetRef
			}
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOADMIN); err != nil {
					return err
				}
				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "repo.default-task.create",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "description": args[0], "source_ref": src, "target_ref": tgt},
						Action:          "create",
						PredictedAction: "create",
						Supported:       true,
						Reason:          "default task will be created",
						Confidence:      capabilityFull,
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1, CreateCount: 1},
				}
				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}
			task, err := service.AddDefaultTask(cmd.Context(), repo, args[0], src, tgt)
			if err != nil {
				return err
			}
			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), task)
			}
			idStr := ""
			if task != nil && task.Id != nil {
				idStr = strconv.FormatInt(int64(*task.Id), 10)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Success.Render("Created default task:"), style.Secondary.Render(idStr))
			return nil
		},
	}
	addCmd.Flags().StringVar(&sourceRef, "source-ref", "", "Source ref matcher (e.g. refs/heads/feature/*)")
	addCmd.Flags().StringVar(&targetRef, "target-ref", "", "Target ref matcher (e.g. refs/heads/master)")

	var updateDesc string
	updateCmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update a default checklist task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}
			repo, err := resolveRepositorySettingsReference(repositorySelector, cfg)
			if err != nil {
				return err
			}
			service := reposettings.NewService(client)
			var src *string
			if cmd.Flags().Changed("source-ref") {
				src = &sourceRef
			}
			var tgt *string
			if cmd.Flags().Changed("target-ref") {
				tgt = &targetRef
			}
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOADMIN); err != nil {
					return err
				}
				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "repo.default-task.update",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": args[0], "description": updateDesc, "source_ref": src, "target_ref": tgt},
						Action:          "update",
						PredictedAction: "update",
						Supported:       true,
						Reason:          "default task will be updated",
						Confidence:      capabilityFull,
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1, UpdateCount: 1},
				}
				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}
			task, err := service.UpdateDefaultTask(cmd.Context(), repo, args[0], updateDesc, src, tgt)
			if err != nil {
				return err
			}
			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), task)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Updated.Render("Updated default task:"), style.Secondary.Render(args[0]))
			return nil
		},
	}
	updateCmd.Flags().StringVar(&updateDesc, "description", "", "New task description")
	updateCmd.Flags().StringVar(&sourceRef, "source-ref", "", "New source ref matcher")
	updateCmd.Flags().StringVar(&targetRef, "target-ref", "", "New target ref matcher")
	_ = updateCmd.MarkFlagRequired("description")

	deleteCmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a default checklist task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}
			repo, err := resolveRepositorySettingsReference(repositorySelector, cfg)
			if err != nil {
				return err
			}
			service := reposettings.NewService(client)
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOADMIN); err != nil {
					return err
				}
				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "repo.default-task.delete",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": args[0]},
						Action:          "delete",
						PredictedAction: "delete",
						Supported:       true,
						Reason:          "default task will be deleted",
						Confidence:      capabilityFull,
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1, DeleteCount: 1},
				}
				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}
			err = service.DeleteDefaultTask(cmd.Context(), repo, args[0])
			if err != nil {
				return err
			}
			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"status": "ok", "id": args[0]})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Deleted.Render("Deleted default task:"), style.Secondary.Render(args[0]))
			return nil
		},
	}

	defaultTaskCmd.AddCommand(listCmd)
	defaultTaskCmd.AddCommand(addCmd)
	defaultTaskCmd.AddCommand(updateCmd)
	defaultTaskCmd.AddCommand(deleteCmd)
	return defaultTaskCmd
}
