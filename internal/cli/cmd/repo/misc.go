package repocmd

import (
	"fmt"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/safederef"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/dryrunpreview"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/enumflag"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/paging"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/preflight"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/style"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	branchservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/branch"
	browseservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/browse"
	diffservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/diff"
	forksync "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/forksync"
	reposettings "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/reposettings"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/sshkey"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
)

func newRepoLabelCommand(deps Dependencies) *cobra.Command {
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
			cfg, client, err := deps.LoadConfigAndClient()
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
			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), Labels{Repository: settingsRepositoryOf(repo), Labels: labels})
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
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}
			repo, err := resolveRepositorySettingsReference(repositorySelector, cfg)
			if err != nil {
				return err
			}
			service := reposettings.NewService(client)
			if deps.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), deps.PermissionChecker, client, repo.ProjectKey, repo.Slug, openapi.RepoWrite); err != nil {
					return err
				}
				preview := dryrunpreview.New(dryrunpreview.PlanningModeStateful, dryrunpreview.CapabilityFull, dryrunpreview.Item{
					Intent:          "repo.label.add",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "label": args[0]},
					Action:          "create",
					PredictedAction: "create",
					Supported:       true,
					Reason:          "label will be added to the repository",
					Confidence:      dryrunpreview.CapabilityFull,
				})
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}
			err = service.AddRepositoryLabel(cmd.Context(), repo, args[0])
			if err != nil {
				return err
			}
			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), LabelChange{Status: result.OK(), Repository: settingsRepositoryOf(repo), Label: args[0]})
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
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}
			repo, err := resolveRepositorySettingsReference(repositorySelector, cfg)
			if err != nil {
				return err
			}
			service := reposettings.NewService(client)
			if deps.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), deps.PermissionChecker, client, repo.ProjectKey, repo.Slug, openapi.RepoWrite); err != nil {
					return err
				}
				preview := dryrunpreview.New(dryrunpreview.PlanningModeStateful, dryrunpreview.CapabilityFull, dryrunpreview.Item{
					Intent:          "repo.label.remove",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "label": args[0]},
					Action:          "delete",
					PredictedAction: "delete",
					Supported:       true,
					Reason:          "label will be removed from the repository",
					Confidence:      dryrunpreview.CapabilityFull,
				})
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}
			err = service.RemoveRepositoryLabel(cmd.Context(), repo, args[0])
			if err != nil {
				return err
			}
			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), LabelChange{Status: result.OK(), Repository: settingsRepositoryOf(repo), Label: args[0]})
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

func newRepoWatchCommand(deps Dependencies) *cobra.Command {
	var repositorySelector string

	watchCmd := &cobra.Command{
		Use:   "watch",
		Short: "Watch repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}
			repo, err := resolveRepositorySettingsReference(repositorySelector, cfg)
			if err != nil {
				return err
			}
			service := reposettings.NewService(client)
			if deps.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), deps.PermissionChecker, client, repo.ProjectKey, repo.Slug, openapi.RepoRead); err != nil {
					return err
				}
				preview := dryrunpreview.New(dryrunpreview.PlanningModeStateful, dryrunpreview.CapabilityFull, dryrunpreview.Item{
					Intent:          "repo.watch",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug)},
					Action:          "update",
					PredictedAction: "update",
					Supported:       true,
					Reason:          "user will watch repository",
					Confidence:      dryrunpreview.CapabilityFull,
				})
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}
			err = service.WatchRepository(cmd.Context(), repo)
			if err != nil {
				return err
			}
			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), WatchState{Status: result.OK(), Repository: settingsRepositoryOf(repo), Watching: true})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Watching repository %s/%s\n", repo.ProjectKey, repo.Slug)
			return nil
		},
	}
	watchCmd.Flags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug (defaults to BITBUCKET_PROJECT_KEY + BITBUCKET_REPO_SLUG)")
	return watchCmd
}

func newRepoUnwatchCommand(deps Dependencies) *cobra.Command {
	var repositorySelector string

	unwatchCmd := &cobra.Command{
		Use:   "unwatch",
		Short: "Unwatch repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}
			repo, err := resolveRepositorySettingsReference(repositorySelector, cfg)
			if err != nil {
				return err
			}
			service := reposettings.NewService(client)
			if deps.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), deps.PermissionChecker, client, repo.ProjectKey, repo.Slug, openapi.RepoRead); err != nil {
					return err
				}
				preview := dryrunpreview.New(dryrunpreview.PlanningModeStateful, dryrunpreview.CapabilityFull, dryrunpreview.Item{
					Intent:          "repo.unwatch",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug)},
					Action:          "delete",
					PredictedAction: "delete",
					Supported:       true,
					Reason:          "user will unwatch repository",
					Confidence:      dryrunpreview.CapabilityFull,
				})
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}
			err = service.UnwatchRepository(cmd.Context(), repo)
			if err != nil {
				return err
			}
			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), WatchState{Status: result.OK(), Repository: settingsRepositoryOf(repo), Watching: false})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Unwatched repository %s/%s\n", repo.ProjectKey, repo.Slug)
			return nil
		},
	}
	unwatchCmd.Flags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug (defaults to BITBUCKET_PROJECT_KEY + BITBUCKET_REPO_SLUG)")
	return unwatchCmd
}

func newRepoDefaultTaskCommand(deps Dependencies) *cobra.Command {
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
			cfg, client, err := deps.LoadConfigAndClient()
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

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), DefaultTasks{Repository: settingsRepositoryOf(repo), Tasks: defaultTasksFrom(tasks)})
			}
			if len(tasks) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No default checklist tasks found"))
				return nil
			}
			rows := make([][]string, len(tasks))
			for i, t := range tasks {
				idStr := ""
				if t.Id != nil {
					idStr = strconv.FormatInt(*t.Id, 10)
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
			cfg, client, err := deps.LoadConfigAndClient()
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
			if deps.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), deps.PermissionChecker, client, repo.ProjectKey, repo.Slug, openapi.RepoAdmin); err != nil {
					return err
				}
				preview := dryrunpreview.New(dryrunpreview.PlanningModeStateful, dryrunpreview.CapabilityFull, dryrunpreview.Item{
					Intent:          "repo.default-task.create",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "description": args[0], "sourceRef": src, "targetRef": tgt},
					Action:          "create",
					PredictedAction: "create",
					Supported:       true,
					Reason:          "default task will be created",
					Confidence:      dryrunpreview.CapabilityFull,
				})
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}
			task, err := service.AddDefaultTask(cmd.Context(), repo, args[0], src, tgt)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), SingleDefaultTask{Repository: settingsRepositoryOf(repo), Task: defaultTaskValue(task)})
			}
			idStr := ""
			if task != nil && task.Id != nil {
				idStr = strconv.FormatInt(*task.Id, 10)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Success.Render("Created default task:"), style.Secondary.Render(idStr))
			return nil
		},
	}
	addCmd.Flags().StringVar(&sourceRef, "source-ref", "", "Source ref to match; a glob matches as a pattern, anything else as a branch (default: any ref)")
	addCmd.Flags().StringVar(&targetRef, "target-ref", "", "Target ref to match; a glob matches as a pattern, anything else as a branch (default: any ref)")

	var updateDesc string
	updateCmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update a default checklist task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
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
			if deps.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), deps.PermissionChecker, client, repo.ProjectKey, repo.Slug, openapi.RepoAdmin); err != nil {
					return err
				}
				preview := dryrunpreview.New(dryrunpreview.PlanningModeStateful, dryrunpreview.CapabilityFull, dryrunpreview.Item{
					Intent:          "repo.default-task.update",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": args[0], "description": updateDesc, "sourceRef": src, "targetRef": tgt},
					Action:          "update",
					PredictedAction: "update",
					Supported:       true,
					Reason:          "default task will be updated",
					Confidence:      dryrunpreview.CapabilityFull,
				})
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}
			task, err := service.UpdateDefaultTask(cmd.Context(), repo, args[0], updateDesc, src, tgt)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), SingleDefaultTask{Repository: settingsRepositoryOf(repo), Task: defaultTaskValue(task)})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Updated.Render("Updated default task:"), style.Secondary.Render(args[0]))
			return nil
		},
	}
	updateCmd.Flags().StringVar(&updateDesc, "description", "", "New task description")
	updateCmd.Flags().StringVar(&sourceRef, "source-ref", "", "New source ref to match; a glob matches as a pattern, anything else as a branch (default: any ref)")
	updateCmd.Flags().StringVar(&targetRef, "target-ref", "", "New target ref to match; a glob matches as a pattern, anything else as a branch (default: any ref)")
	_ = updateCmd.MarkFlagRequired("description")

	deleteCmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a default checklist task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}
			repo, err := resolveRepositorySettingsReference(repositorySelector, cfg)
			if err != nil {
				return err
			}
			service := reposettings.NewService(client)
			if deps.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), deps.PermissionChecker, client, repo.ProjectKey, repo.Slug, openapi.RepoAdmin); err != nil {
					return err
				}
				preview := dryrunpreview.New(dryrunpreview.PlanningModeStateful, dryrunpreview.CapabilityFull, dryrunpreview.Item{
					Intent:          "repo.default-task.delete",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": args[0]},
					Action:          "delete",
					PredictedAction: "delete",
					Supported:       true,
					Reason:          "default task will be deleted",
					Confidence:      dryrunpreview.CapabilityFull,
				})
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}
			err = service.DeleteDefaultTask(cmd.Context(), repo, args[0])
			if err != nil {
				return err
			}
			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), DefaultTaskDeletion{Status: result.OK(), Repository: settingsRepositoryOf(repo), ID: args[0]})
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

func newRepoSyncCommand(deps Dependencies) *cobra.Command {
	var repositorySelector string
	var syncRefID string
	var syncAction string

	syncCmd := &cobra.Command{
		Use:   "sync",
		Short: "Manage repository fork synchronization",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}
			repo, err := resolveRepositorySettingsReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := forksync.NewService(client)
			if deps.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), deps.PermissionChecker, client, repo.ProjectKey, repo.Slug, openapi.RepoAdmin); err != nil {
					return err
				}
				preview := dryrunpreview.New(dryrunpreview.PlanningModeStateful, dryrunpreview.CapabilityFull, dryrunpreview.Item{
					Intent:          "repo.sync.trigger",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug)},
					Action:          "update",
					PredictedAction: "update",
					Supported:       true,
					Reason:          "manual synchronization will be triggered",
					Confidence:      dryrunpreview.CapabilityFull,
				})
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			syncRef := strings.TrimSpace(syncRefID)
			if syncRef == "" {
				defaultRef, err := branchservice.NewService(client).GetDefault(cmd.Context(),
					branchservice.RepositoryRef{ProjectKey: repo.ProjectKey, Slug: repo.Slug})
				if err != nil {
					return err
				}
				if defaultRef.Id != nil {
					syncRef = *defaultRef.Id
				}
			}

			if err := service.Synchronize(cmd.Context(), repo.ProjectKey, repo.Slug, syncRef, syncAction); err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), SyncTriggered{Status: result.OK(), Repository: settingsRepositoryOf(repo), Ref: syncRef, Action: strings.ToUpper(syncAction)})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Synchronization triggered for fork %s/%s on %s from upstream\n", repo.ProjectKey, repo.Slug, syncRef)
			return nil
		},
	}
	syncCmd.Flags().StringVar(&syncRefID, "ref", "", "Ref to synchronize (defaults to the repository default branch)")
	enumflag.Register(syncCmd.Flags(), &syncAction, "action", "MERGE", []string{"MERGE", "DISCARD", "REBASE"}, "How to reconcile the ref")
	syncCmd.PersistentFlags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug (defaults to BITBUCKET_PROJECT_KEY + BITBUCKET_REPO_SLUG)")

	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Query synchronization status, divergence, and settings",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}
			repo, err := resolveRepositorySettingsReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := forksync.NewService(client)
			status, err := service.GetSyncStatus(cmd.Context(), repo.ProjectKey, repo.Slug)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), syncStatusFrom(settingsRepositoryOf(repo), status))
			}

			enabled := false
			if status.Enabled != nil {
				enabled = *status.Enabled
			}
			available := false
			if status.Available != nil {
				available = *status.Available
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Auto-sync enabled: %t\n", enabled)
			fmt.Fprintf(cmd.OutOrStdout(), "Auto-sync available: %t\n", available)
			return nil
		},
	}

	enableCmd := &cobra.Command{
		Use:   "enable",
		Short: "Enable automatic background synchronization",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}
			repo, err := resolveRepositorySettingsReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := forksync.NewService(client)
			if deps.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), deps.PermissionChecker, client, repo.ProjectKey, repo.Slug, openapi.RepoAdmin); err != nil {
					return err
				}
				preview := dryrunpreview.New(dryrunpreview.PlanningModeStateful, dryrunpreview.CapabilityFull, dryrunpreview.Item{
					Intent:          "repo.sync.enable",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "enabled": true},
					Action:          "update",
					PredictedAction: "update",
					Supported:       true,
					Reason:          "automatic synchronization will be enabled",
					Confidence:      dryrunpreview.CapabilityFull,
				})
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			status, err := service.SetEnabled(cmd.Context(), repo.ProjectKey, repo.Slug, true)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), syncStatusFrom(settingsRepositoryOf(repo), status))
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Automatic synchronization enabled for fork %s/%s\n", repo.ProjectKey, repo.Slug)
			return nil
		},
	}

	disableCmd := &cobra.Command{
		Use:   "disable",
		Short: "Disable automatic background synchronization",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}
			repo, err := resolveRepositorySettingsReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := forksync.NewService(client)
			if deps.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), deps.PermissionChecker, client, repo.ProjectKey, repo.Slug, openapi.RepoAdmin); err != nil {
					return err
				}
				preview := dryrunpreview.New(dryrunpreview.PlanningModeStateful, dryrunpreview.CapabilityFull, dryrunpreview.Item{
					Intent:          "repo.sync.disable",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "enabled": false},
					Action:          "update",
					PredictedAction: "update",
					Supported:       true,
					Reason:          "automatic synchronization will be disabled",
					Confidence:      dryrunpreview.CapabilityFull,
				})
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			status, err := service.SetEnabled(cmd.Context(), repo.ProjectKey, repo.Slug, false)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), syncStatusFrom(settingsRepositoryOf(repo), status))
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Automatic synchronization disabled for fork %s/%s\n", repo.ProjectKey, repo.Slug)
			return nil
		},
	}

	syncCmd.AddCommand(statusCmd)
	syncCmd.AddCommand(enableCmd)
	syncCmd.AddCommand(disableCmd)
	return syncCmd
}

func newRepoCatCommand(deps Dependencies) *cobra.Command {
	var repositorySelector string
	var at string

	cmd := &cobra.Command{
		Use:   "cat <path>",
		Short: "Output the raw content of a file over REST",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			repoRef, err := resolveRepoReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			repo := browseservice.RepositoryRef{ProjectKey: repoRef.ProjectKey, Slug: repoRef.Slug}
			service := browseservice.NewService(client, httpclient.NewFromConfig(cfg))

			content, err := service.Raw(cmd.Context(), repo, args[0], at)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), rawFileFrom(browseRepositoryOf(repo), args[0], at, content))
			}

			_, _ = cmd.OutOrStdout().Write(content)
			return nil
		},
	}

	cmd.Flags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug")
	cmd.Flags().StringVar(&at, "at", "", "Commit ID or ref to cat")

	return cmd
}

func newRepoEditCommand(deps Dependencies) *cobra.Command {
	var repositorySelector string
	var branch string
	var message string
	var content string
	var sourceBranch string
	var sourceCommitId string

	cmd := &cobra.Command{
		Use:   "edit <path>",
		Short: "Edit a file's content over REST",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			repoRef, err := resolveRepoReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			repo := browseservice.RepositoryRef{ProjectKey: repoRef.ProjectKey, Slug: repoRef.Slug}
			service := browseservice.NewService(client, httpclient.NewFromConfig(cfg))

			if deps.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), deps.PermissionChecker, client, repo.ProjectKey, repo.Slug, openapi.RepoWrite); err != nil {
					return err
				}

				preview := dryrunpreview.New(dryrunpreview.PlanningModeStateful, dryrunpreview.CapabilityPartial, dryrunpreview.Item{
					Intent: "repo.edit",
					Target: map[string]any{
						"repository":     fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug),
						"path":           args[0],
						"branch":         branch,
						"message":        message,
						"sourceBranch":   sourceBranch,
						"sourceCommitId": sourceCommitId,
					},
					Action:          "update",
					PredictedAction: "update",
					Supported:       true,
					Reason:          "file will be edited",
					Confidence:      dryrunpreview.CapabilityPartial,
					RequiredState:   []string{"repository write access"},
				})
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			// Reading stdin requires --content -, never an empty --content.
			// The implicit fallback blocked forever when stdin was an open pipe
			// with nothing coming -- the shape a CI runner provides -- and under
			// an agent it returned nothing instead, committing an empty file
			// over the real one. Both are ADR-073's reason for the rule.
			editContent := content
			if content == "-" {
				inBytes, err := io.ReadAll(cmd.InOrStdin())
				if err != nil {
					return err
				}
				editContent = string(inBytes)
			} else if content == "" {
				return apperrors.New(apperrors.KindValidation,
					"no content given: pass --content, or --content - to read the file body from standard input", nil)
			}

			res, err := service.Edit(cmd.Context(), repo, args[0], browseservice.EditInput{
				Branch:         branch,
				Content:        editContent,
				Message:        message,
				SourceBranch:   sourceBranch,
				SourceCommitId: sourceCommitId,
			})
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), fileEditFrom(browseRepositoryOf(repo), args[0], branch, res))
			}

			commitID := safederef.String(res.Id)
			fmt.Fprintf(cmd.OutOrStdout(), "Successfully edited %s in commit %s\n", args[0], commitID)
			return nil
		},
	}

	cmd.Flags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug")
	cmd.Flags().StringVar(&branch, "branch", "", "The branch on which the file should be modified or created")
	cmd.Flags().StringVar(&message, "message", "", "Commit message")
	cmd.Flags().StringVar(&content, "content", "", "The full content of the file")
	cmd.Flags().StringVar(&sourceBranch, "source-branch", "", "Starting point branch")
	cmd.Flags().StringVar(&sourceCommitId, "source-commit", "", "Commit ID before editing")

	return cmd
}

func newRepoCompareCommand(deps Dependencies) *cobra.Command {
	var repositorySelector string
	var diff bool

	cmd := &cobra.Command{
		Use:   "compare <from> <to>",
		Short: "Compare commits or branches",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			repoRef, err := resolveRepoReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			repo := diffservice.RepositoryRef{ProjectKey: repoRef.ProjectKey, Slug: repoRef.Slug}
			service := diffservice.NewService(client)

			from := args[0]
			to := args[1]

			if diff {
				diffResult, err := service.CompareDiff(cmd.Context(), repo, from, to)
				if err != nil {
					return err
				}

				text := diffservice.FormatRestDiff(diffResult)
				if deps.JSONEnabled() {
					return deps.WriteJSON(cmd.OutOrStdout(), Comparison{
						Repository: result.Repository{ProjectKey: repo.ProjectKey, Slug: repo.Slug},
						From:       from,
						To:         to,
						Changes:    []Change{},
						Patch:      text,
					})
				}

				fmt.Fprint(cmd.OutOrStdout(), text)
				return nil
			}

			changes, err := service.CompareChanges(cmd.Context(), repo, from, to, 1000)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), Comparison{
					Repository: result.Repository{ProjectKey: repo.ProjectKey, Slug: repo.Slug},
					From:       from,
					To:         to,
					Changes:    changesFrom(changes),
				})
			}

			if len(changes) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No changes found"))
				return nil
			}

			rows := make([][]string, len(changes))
			for i, change := range changes {
				pathStr := ""
				if change.Path != nil && change.Path.Components != nil {
					pathStr = strings.Join(*change.Path.Components, "/")
				}
				changeType := ""
				if change.Type != nil {
					changeType = string(*change.Type)
				}
				rows[i] = []string{style.Resource.Render(pathStr), changeType}
			}
			style.WriteTable(cmd.OutOrStdout(), rows)

			return nil
		},
	}

	cmd.Flags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug")
	cmd.Flags().BoolVar(&diff, "diff", false, "Show the unified diff of the changes")

	return cmd
}

func newRepoArchiveCommand(deps Dependencies) *cobra.Command {
	var repositorySelector string
	var format string
	var output string
	var at string
	var prefix string
	var path string

	cmd := &cobra.Command{
		Use:   "archive",
		Short: "Download repository archive",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			repoRef, err := resolveRepoReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			if format == "" {
				format = "zip"
			}

			var pathParam *string
			if path != "" {
				pathParam = &path
			}
			var atParam *string
			if at != "" {
				atParam = &at
			}
			var prefixParam *string
			if prefix != "" {
				prefixParam = &prefix
			}
			var formatParam *string
			if format != "" {
				formatParam = &format
			}

			params := &openapigenerated.GetArchiveParams{
				Path:   pathParam,
				At:     atParam,
				Prefix: prefixParam,
				Format: formatParam,
			}

			resp, err := client.GetArchive(cmd.Context(), repoRef.ProjectKey, repoRef.Slug, params)
			if err != nil {
				return err
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode >= 400 {
				bodyBytes, _ := io.ReadAll(resp.Body)
				return openapi.MapStatusError(resp.StatusCode, bodyBytes)
			}

			var writer io.Writer
			// Non-nil only when writing to a file rather than stdout; the file is
			// closed explicitly before success is reported.
			var archiveFile io.WriteCloser
			var targetMsg string

			if output == "-" {
				writer = cmd.OutOrStdout()
				targetMsg = "stdout"
			} else {
				filename := output
				if filename == "" {
					filename = fmt.Sprintf("%s.%s", repoRef.Slug, format)
				}
				file, err := createArchiveFile(filename)
				if err != nil {
					return err
				}
				// finishArchiveFile closes it before success is reported; this
				// only covers the paths that return early.
				defer func() { _ = file.Close() }()
				archiveFile = file
				writer = file
				absPath, _ := filepath.Abs(filename)
				targetMsg = absPath
			}

			_, err = io.Copy(writer, resp.Body)
			if err != nil {
				return err
			}

			if err := finishArchiveFile(archiveFile, targetMsg); err != nil {
				return err
			}

			if output != "-" {
				if deps.JSONEnabled() {
					return deps.WriteJSON(cmd.OutOrStdout(), Archive{Status: result.OK(), Repository: repositoryOf(repoRef), File: targetMsg})
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Successfully downloaded repository archive to %s\n", targetMsg)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug")
	enumflag.Register(cmd.Flags(), &format, "format", "zip", []string{"zip", "tar", "tar.gz", "tgz"}, "The format to stream the archive in")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output filename (use '-' for stdout, defaults to <repo-slug>.<format>)")
	cmd.Flags().StringVar(&at, "at", "", "The commit to stream an archive of")
	cmd.Flags().StringVar(&prefix, "prefix", "", "A prefix to apply to all entries in the streamed archive")
	cmd.Flags().StringVar(&path, "path", "", "Paths to include in the streamed archive")

	return cmd
}

func readPublicKey(arg string) (string, error) {
	if _, err := os.Stat(arg); err == nil {
		content, err := os.ReadFile(arg)
		if err != nil {
			return "", apperrors.New(apperrors.KindValidation, fmt.Sprintf("failed to read key file %s", arg), err)
		}
		return strings.TrimSpace(string(content)), nil
	}
	return arg, nil
}

func resolveRepoSshKeyScope(projectFlag, repoFlag string) (string, string, bool, error) {
	if projectFlag != "" && repoFlag != "" {
		return "", "", false, apperrors.New(apperrors.KindValidation, "only one of --project or --repo can be specified", nil)
	}
	if projectFlag != "" {
		return projectFlag, "", true, nil
	}
	if repoFlag != "" {
		parts := strings.Split(strings.TrimSpace(repoFlag), "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return "", "", false, apperrors.New(apperrors.KindValidation, "--repo must be in projectKey/repositorySlug format", nil)
		}
		return parts[0], parts[1], false, nil
	}
	return "", "", false, apperrors.New(apperrors.KindValidation, "either --project or --repo is required", nil)
}

func newRepoSshKeyCommand(deps Dependencies) *cobra.Command {
	repoSshCmd := &cobra.Command{
		Use:   "ssh-key",
		Short: "Manage project or repository SSH access keys",
	}

	var projectFlag string
	var repoFlag string
	var listPaging paging.Options

	repoSshCmd.PersistentFlags().StringVar(&projectFlag, "project", "", "Project key for project-level SSH keys")
	repoSshCmd.PersistentFlags().StringVar(&repoFlag, "repo", "", "Repository reference (projectKey/repositorySlug) for repository-level SSH keys")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List project or repository SSH access keys",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}
			svc := sshkey.NewService(client)

			proj, repo, isProj, err := resolveRepoSshKeyScope(projectFlag, repoFlag)
			if err != nil {
				return err
			}

			var keys []openapigenerated.RestSshAccessKey
			if isProj {
				keys, err = svc.ListProjectKeys(cmd.Context(), proj, listPaging.ServiceLimit())
			} else {
				keys, err = svc.ListRepoKeys(cmd.Context(), proj, repo, listPaging.ServiceLimit())
			}
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSONList(cmd.OutOrStdout(), SSHKeys{Keys: sshKeysFrom(keys)}, paging.LimitReached(listPaging, len(keys)))
			}

			if len(keys) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No SSH access keys found")
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%-8s %-30s %-15s %-50s\n", "ID", "LABEL", "PERMISSION", "FINGERPRINT")
			for _, k := range keys {
				id := ""
				label := ""
				fingerprint := ""
				if k.Key != nil {
					if k.Key.Id != nil {
						id = fmt.Sprintf("%d", *k.Key.Id)
					}
					if k.Key.Label != nil {
						label = *k.Key.Label
					}
					if k.Key.Fingerprint != nil {
						fingerprint = *k.Key.Fingerprint
					}
				}
				permission := ""
				if k.Permission != nil {
					permission = string(*k.Permission)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%-8s %-30s %-15s %-50s\n", id, label, permission, fingerprint)
			}
			return nil
		},
	}
	listPaging.Register(listCmd, 25)
	repoSshCmd.AddCommand(listCmd)

	var labelFlag string
	var permissionFlag string
	var readOnlyFlag bool
	var readWriteFlag bool

	addCmd := &cobra.Command{
		Use:   "add <key-file-or-text>",
		Short: "Add a project or repository SSH access key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}
			svc := sshkey.NewService(client)

			proj, repo, isProj, err := resolveRepoSshKeyScope(projectFlag, repoFlag)
			if err != nil {
				return err
			}

			keyContent, err := readPublicKey(args[0])
			if err != nil {
				return err
			}

			permission := ""
			if readWriteFlag {
				if isProj {
					permission = "PROJECT_WRITE"
				} else {
					permission = "REPO_WRITE"
				}
			} else if readOnlyFlag || strings.ToLower(permissionFlag) == "read-only" {
				if isProj {
					permission = "PROJECT_READ"
				} else {
					permission = "REPO_READ"
				}
			} else if strings.ToLower(permissionFlag) == "read-write" {
				if isProj {
					permission = "PROJECT_WRITE"
				} else {
					permission = "REPO_WRITE"
				}
			} else {
				if isProj {
					permission = "PROJECT_READ"
				} else {
					permission = "REPO_READ"
				}
			}

			var added openapigenerated.RestSshAccessKey
			if isProj {
				added, err = svc.AddProjectKey(cmd.Context(), proj, labelFlag, keyContent, permission)
			} else {
				added, err = svc.AddRepoKey(cmd.Context(), proj, repo, labelFlag, keyContent, permission)
			}
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), AddedSSHKey{Key: sshKeyFrom(added)})
			}

			id := 0
			lbl := ""
			if added.Key != nil {
				if added.Key.Id != nil {
					id = int(*added.Key.Id)
				}
				if added.Key.Label != nil {
					lbl = *added.Key.Label
				}
			}
			perm := ""
			if added.Permission != nil {
				perm = string(*added.Permission)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "SSH access key %d (%s) with permission %s added successfully\n", id, lbl, perm)
			return nil
		},
	}
	addCmd.Flags().StringVar(&labelFlag, "label", "", "Label/comment for the SSH key")
	enumflag.Register(addCmd.Flags(), &permissionFlag, "permission", "read-only", []string{"read-only", "read-write"}, "Permission level")
	addCmd.Flags().BoolVar(&readOnlyFlag, "read-only", false, "Add as read-only access key")
	addCmd.Flags().BoolVar(&readWriteFlag, "read-write", false, "Add as read-write access key")
	repoSshCmd.AddCommand(addCmd)

	removeCmd := &cobra.Command{
		Use:   "remove <key-id>",
		Short: "Remove a project or repository SSH access key by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}
			svc := sshkey.NewService(client)

			proj, repo, isProj, err := resolveRepoSshKeyScope(projectFlag, repoFlag)
			if err != nil {
				return err
			}

			if isProj {
				err = svc.RemoveProjectKey(cmd.Context(), proj, args[0])
			} else {
				err = svc.RemoveRepoKey(cmd.Context(), proj, repo, args[0])
			}
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), result.OK())
			}

			fmt.Fprintf(cmd.OutOrStdout(), "SSH access key %s removed successfully\n", args[0])
			return nil
		},
	}
	repoSshCmd.AddCommand(removeCmd)

	return repoSshCmd
}

// createArchiveFile is the seam the archive download writes through.
//
// A close failure is the thing worth testing here and it cannot be provoked
// through os.Create: a real file closes cleanly, and one closed early fails the
// write instead. It returns io.WriteCloser rather than *os.File so a test can
// supply something that writes fine and fails only on Close, which is the
// branch that used to report a truncated download as a success.
var createArchiveFile = func(name string) (io.WriteCloser, error) { return os.Create(name) }

// finishArchiveFile closes the downloaded archive and reports a close failure
// as an error rather than as a successful download.
//
// io.Copy returning nil does not mean the bytes reached the disk. A Close that
// fails — a full disk, a network filesystem — leaves a truncated archive, and
// this command used to print "Successfully downloaded" and exit 0 over exactly
// that. It is a function rather than three lines at the call site so the
// failure path can be exercised: closing an already-closed *os.File returns an
// error on every platform, which no amount of mocking around os.Create would.
func finishArchiveFile(file io.WriteCloser, target string) error {
	if file == nil {
		return nil
	}
	if err := file.Close(); err != nil {
		return apperrors.New(
			apperrors.KindInternal,
			fmt.Sprintf("failed to finish writing repository archive to %s", target),
			err,
		)
	}
	return nil
}
