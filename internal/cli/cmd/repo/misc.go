package repocmd

import (
	"context"
	"errors"
	"fmt"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/safederef"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/dryrunpreview"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/enumflag"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/inherited"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/outwriter"
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
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/download"
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
				preview := dryrunpreview.New(dryrunpreview.Item{
					Intent:          "repo.label.add",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "label": args[0]},
					Action:          "create",
					PredictedAction: "create",
					Reason:          "label will be added to the repository",
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
				preview := dryrunpreview.New(dryrunpreview.Item{
					Intent:          "repo.label.remove",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "label": args[0]},
					Action:          "delete",
					PredictedAction: "delete",
					Reason:          "label will be removed from the repository",
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
				preview := dryrunpreview.New(dryrunpreview.Item{
					Intent:          "repo.watch",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug)},
					Action:          "update",
					PredictedAction: "update",
					Reason:          "user will watch repository",
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
				preview := dryrunpreview.New(dryrunpreview.Item{
					Intent:          "repo.unwatch",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug)},
					Action:          "delete",
					PredictedAction: "delete",
					Reason:          "user will unwatch repository",
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
		Long: `List the repository's default checklist tasks, and those it inherits from its
project, which are marked as inherited. bb project default-task changes an
inherited one.`,
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
				rows[i] = []string{
					style.Secondary.Render(idStr), style.Resource.Render(desc), src, tgt,
					style.Secondary.Render(inherited.Label(defaultTaskScope(t), repo.ProjectKey)),
				}
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
				preview := dryrunpreview.New(dryrunpreview.Item{
					Intent:          "repo.default-task.create",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "description": args[0], "sourceRef": src, "targetRef": tgt},
					Action:          "create",
					PredictedAction: "create",
					Reason:          "default task will be created",
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
		Use:   "update <task-id>",
		Short: "Update a default checklist task",
		Long: `Update one of the repository's default checklist tasks.

A task the repository inherits from its project is refused; bb project
default-task update changes it there, for every repository in the project.`,
		Args: cobra.ExactArgs(1),
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
				predicted, reason := "update", "default task will be updated"
				var blocking []string
				if err := ownDefaultTask(cmd.Context(), service, repo, args[0], "update"); err != nil {
					if !apperrors.IsKind(err, apperrors.KindNotFound) {
						return err
					}
					predicted, reason, blocking = "blocked", "default task not found in repository", []string{"default task not found"}
				}
				preview := dryrunpreview.New(dryrunpreview.Item{
					Intent:          "repo.default-task.update",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": args[0], "description": updateDesc, "sourceRef": src, "targetRef": tgt},
					Action:          "update",
					PredictedAction: predicted,
					Tier:            dryrunpreview.TierPreconditionsChecked,
					Reason:          reason,
					BlockingReasons: blocking,
					// The one refusal predicted here is a task that is not there,
					// which the real run meets in ownDefaultTask as not_found.
					Fails: apperrors.KindNotFound,
				})
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}
			if err := ownDefaultTask(cmd.Context(), service, repo, args[0], "update"); err != nil {
				return err
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
		Use:   "delete <task-id>",
		Short: "Delete a default checklist task",
		Long: `Delete one of the repository's default checklist tasks.

A task the repository inherits from its project is refused; bb project
default-task delete deletes it there, for every repository in the project.`,
		Args: cobra.ExactArgs(1),
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
				predicted, reason := "delete", "default task will be deleted"
				if err := ownDefaultTask(cmd.Context(), service, repo, args[0], "delete"); err != nil {
					if !apperrors.IsKind(err, apperrors.KindNotFound) {
						return err
					}
					predicted, reason = "no-op", "default task was not found"
				}
				preview := dryrunpreview.New(dryrunpreview.Item{
					Intent:          "repo.default-task.delete",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": args[0]},
					Action:          "delete",
					PredictedAction: predicted,
					Tier:            dryrunpreview.TierPreconditionsChecked,
					Reason:          reason,
				})
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}
			// Looked up first: a task inherited from the project is refused,
			// because the route answers 204 for it and leaves it in place, and
			// one that is not there is reported as such.
			if err := ownDefaultTask(cmd.Context(), service, repo, args[0], "delete"); err != nil {
				return err
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

// ownDefaultTask looks a task up in the repository's listing, which holds the
// tasks it inherits from its project as well, and refuses one of those: the
// repository's route answers 204 for it and leaves it where it is (#657). A
// task that is not there is not found. change is the subcommand the caller
// ran, which does the same on the project.
// archiveRequestURL is the address of a repository's archive, built by the
// generated client so the path and every parameter are encoded as the
// specification has them.
//
// The generated builders resolve their path relative to the server URL, and
// RFC 3986 replaces a base URL's last segment unless it ends in a slash: given
// .../rest, they address .../api/latest/..., which Bitbucket answers with its
// web UI's 404 page. The generated client adds that slash itself; a builder
// called directly has to be given it.
func archiveRequestURL(bitbucketURL, projectKey, slug string, params *openapigenerated.GetArchiveParams) (string, error) {
	request, err := openapigenerated.NewGetArchiveRequest(strings.TrimRight(bitbucketURL, "/")+"/rest/", projectKey, slug, params)
	if err != nil {
		return "", err
	}

	return request.URL.String(), nil
}

func ownDefaultTask(ctx context.Context, service *reposettings.Service, repo reposettings.RepositoryRef, id, change string) error {
	task, err := service.GetDefaultTask(ctx, repo, id)
	if err != nil {
		return err
	}

	if inherited.FromProject(defaultTaskScope(task)) {
		trimmed := strings.TrimSpace(id)
		return inherited.Refusal("default task", trimmed, repo.ProjectKey, change,
			fmt.Sprintf("bb project default-task %s %s %s", change, repo.ProjectKey, trimmed))
	}

	return nil
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
				preview := dryrunpreview.New(dryrunpreview.Item{
					Intent:          "repo.sync.trigger",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug)},
					Action:          "update",
					PredictedAction: "update",
					Reason:          "manual synchronization will be triggered",
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
	// syncActions, the slice the output schema's enum is declared from: the flag
	// listed REBASE inline and the schema did not, so a successful rebase sync
	// emitted a document its own schema rejected (#577).
	enumflag.Register(syncCmd.Flags(), &syncAction, "action", "MERGE", syncActions, "How to reconcile the ref")
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
				preview := dryrunpreview.New(dryrunpreview.Item{
					Intent:          "repo.sync.enable",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "enabled": true},
					Action:          "update",
					PredictedAction: "update",
					Reason:          "automatic synchronization will be enabled",
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
				preview := dryrunpreview.New(dryrunpreview.Item{
					Intent:          "repo.sync.disable",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "enabled": false},
					Action:          "update",
					PredictedAction: "update",
					Reason:          "automatic synchronization will be disabled",
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
		Long:  rawFileHelp("Output the raw content of a file over REST."),
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

			return writeRawFile(cmd, deps, service, repo, args[0], at)
		},
	}

	cmd.Flags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug")
	cmd.Flags().StringVar(&at, "at", "", "Commit ID or ref to cat")

	return cmd
}

// rawFileHelp is the help of the two commands that write a file's bytes, which
// behave alike: the limit under --json is theirs.
func rawFileHelp(summary string) string {
	return summary + fmt.Sprintf(`

The file is written to stdout as it arrives, byte for byte, whatever its size.
With --json it is returned inside the document instead -- as text, or as base64
when it is not text -- which holds it in memory, so a file larger than %d MiB is
refused under --json.`, maxHeldFileBytes>>20)
}

// maxHeldFileBytes is the most of a file --json holds in memory. The document
// wraps the file -- as base64 when it is not text, and then encoded again as
// JSON -- so it is held several times over. Without --json the bytes go to
// stdout as they arrive, and nothing is held.
const maxHeldFileBytes = 64 << 20

// writeRawFile writes a file's bytes, for bb repo cat and bb repo browse raw:
// to stdout as they arrive, or under --json held and wrapped in the document
// ADR-014 promises.
func writeRawFile(cmd *cobra.Command, deps Dependencies, service *browseservice.Service, repo browseservice.RepositoryRef, path, at string) error {
	if deps.JSONEnabled() {
		var held download.Memory
		if err := service.RawTo(cmd.Context(), repo, path, at, &held, maxHeldFileBytes); err != nil {
			return tooLargeForJSON(err, path)
		}

		return deps.WriteJSON(cmd.OutOrStdout(), rawFileFrom(browseRepositoryOf(repo), path, at, held.Bytes()))
	}

	return streamed(service.RawTo(cmd.Context(), repo, path, at, download.To(cmd.OutOrStdout()), 0))
}

// tooLargeForJSON says what to do about a file over what --json holds: drop the
// flag, and the file streams to stdout whatever its size.
//
// Validation, exit 2: the invocation cannot work for this file, and changing
// it is the remedy.
func tooLargeForJSON(err error, path string) error {
	var limit *download.LimitError
	if !errors.As(err, &limit) {
		return err
	}

	return apperrors.New(apperrors.KindValidation, fmt.Sprintf(
		"%s is larger than %d MiB, the most --json holds in memory; drop --json to stream the file to stdout", path, limit.Limit>>20), nil)
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

				preview := dryrunpreview.New(dryrunpreview.Item{
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
					Reason:          "file will be edited",
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
		Long: `Compare commits or branches: what is reachable from <from> but not from
<to>. Pass the ref you are asking about first; git log's range puts it last.

  bb repo compare feature/x main        # what feature/x adds
  bb repo compare main feature/x        # nothing, unless main has moved`,
		Args: cobra.ExactArgs(2),
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
				text, err := service.ComparePatch(cmd.Context(), repo, from, to)
				if err != nil {
					return err
				}
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

			changes, err := service.CompareChanges(cmd.Context(), repo, from, to, diffservice.AllResults)
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
			// Refused rather than ignored. The archive and the envelope both
			// want stdout and only one can have it, so this used to resolve
			// itself by writing the archive and dropping the envelope in
			// silence -- and a caller could not tell that from a command that
			// had failed to produce one. Worse here than elsewhere, because
			// `repo archive --describe` publishes a schema, so the contract
			// promised a document this path never emitted.
			//
			// Checked before anything is fetched: erroring after streaming an
			// archive would be both wasteful and too late to act on.
			if output == "-" && deps.JSONEnabled() {
				return apperrors.New(apperrors.KindValidation,
					"--json cannot be combined with --output - because the archive itself is written to stdout; "+
						"drop --json to stream the archive, or give --output a filename to get the envelope", nil)
			}

			cfg, _, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			repoRef, err := resolveRepoReference(repositorySelector, cfg)
			if err != nil {
				return err
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
			// Always set: --format is an enum flag defaulting to zip, and an
			// empty value resets it to that default, so it is never blank here.
			formatParam := &format

			params := &openapigenerated.GetArchiveParams{
				Path:   pathParam,
				At:     atParam,
				Prefix: prefixParam,
				Format: formatParam,
			}

			// Sent through the downloader: an archive is as large as the
			// repository, and one that had to arrive within the request timeout
			// could not be larger than the link carries in that time.
			archiveURL, err := archiveRequestURL(cfg.BitbucketURL, repoRef.ProjectKey, repoRef.Slug, params)
			if err != nil {
				return apperrors.New(apperrors.KindValidation, "failed to build the repository archive request", err)
			}
			source := httpclient.RequestOptions{Path: archiveURL}
			client := httpclient.NewFromConfig(cfg)

			// Streaming to stdout reports nothing on stdout: the archive is
			// already there, and a success line appended to it would corrupt the
			// file the caller is redirecting. The --json half of this is refused
			// above; what is left is the human line, which has the same problem
			// for the same reason.
			//
			// No cap, here or for a file: the archive is held nowhere in memory,
			// and how large a repository may be is not bb's to judge.
			if output == "-" {
				_, err := client.Download(cmd.Context(), source, download.To(cmd.OutOrStdout()), 0)

				return archiveFailure(streamed(err))
			}

			filename := output
			if filename == "" {
				filename = fmt.Sprintf("%s.%s", repoRef.Slug, format)
			}

			// A temporary file beside the target takes the archive, and is
			// renamed over it only once the download is complete. Written into
			// place, a failed download left a truncated archive there -- and
			// had already destroyed whatever file was there before.
			file, err := download.CreateFile(filename)
			if err != nil {
				return err
			}
			defer file.Discard()

			if _, err := client.Download(cmd.Context(), source, file, 0); err != nil {
				return archiveFailure(err)
			}
			if err := file.Commit(); err != nil {
				return err
			}

			targetMsg, _ := filepath.Abs(filename)
			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), Archive{Status: result.OK(), Repository: repositoryOf(repoRef), File: targetMsg})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Successfully downloaded repository archive to %s\n", targetMsg)

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
			paging.Hint(cmd.ErrOrStderr(), listPaging, len(keys))
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
		Use:   "remove <access-key-id>",
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

// archiveFailure says what failed when an archive download does. Bitbucket's
// own refusal -- a repository that is not there -- goes back as it is, as every
// command reports one; anything else is wrapped with the classification the
// transport gave it (#478).
func archiveFailure(err error) error {
	if err == nil {
		return nil
	}
	if _, answered := apperrors.DetailsOf(err)["upstreamStatus"]; answered {
		return err
	}

	return apperrors.Transport("failed to stream the repository archive", err)
}

// streamed is the failure of a download written to standard output. A reader
// that stopped reading -- `bb repo archive -o - | tar t | head` -- has what it
// wanted, and ends the command as it ends any other output (outwriter).
func streamed(err error) error {
	if err != nil && outwriter.ReaderGone(err) {
		return nil
	}

	return err
}
