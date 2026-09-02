package repocmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/dryrunpreview"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/preflight"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/reposel"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/style"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	reposettings "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/reposettings"
)

func resolveRepositorySettingsReference(selector string, cfg config.AppConfig) (reposettings.RepositoryRef, error) {
	projectKey, slug, err := reposel.Resolve(selector, cfg)
	if err != nil {
		return reposettings.RepositoryRef{}, err
	}
	return reposettings.RepositoryRef{ProjectKey: projectKey, Slug: slug}, nil
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

func newRepoSettingsCommand(deps Dependencies) *cobra.Command {
	var repositorySelector string

	settingsCmd := &cobra.Command{
		Use:   "settings",
		Short: "Repository settings commands",
	}
	settingsCmd.PersistentFlags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug (defaults to BITBUCKET_PROJECT_KEY + BITBUCKET_REPO_SLUG)")

	securityCmd := &cobra.Command{Use: "security", Short: "Security settings"}
	permissionsCmd := &cobra.Command{Use: "permissions", Short: "Repository permissions"}
	permissionsCmd.AddCommand(newRepoPermissionSubjectCommand(deps, &repositorySelector, userPermissionSubject()))
	permissionsCmd.AddCommand(newRepoPermissionSubjectCommand(deps, &repositorySelector, groupPermissionSubject()))

	securityCmd.AddCommand(permissionsCmd)
	settingsCmd.AddCommand(securityCmd)

	workflowCmd := &cobra.Command{Use: "workflow", Short: "Workflow settings"}
	webhooksCmd := &cobra.Command{Use: "webhooks", Short: "Repository webhooks"}
	webhooksListCmd := &cobra.Command{
		Use:   "list",
		Short: "List repository webhooks",
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
			webhooks, err := service.ListRepositoryWebhooks(cmd.Context(), repo)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), Webhooks{Repository: settingsRepositoryOf(repo), Count: webhooks.Count, Webhooks: result.WebhooksFrom(webhooks.Payload)})
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

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          "repo.webhook.create",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "name": args[0], "url": args[1], "events": webhookEvents, "active": webhookActive},
						Action:          "create",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      dryrunpreview.CapabilityFull,
						RequiredState:   []string{"repository webhooks list"},
						BlockingReasons: blocking,
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1},
				}
				switch predicted {
				case "create":
					preview.Summary.CreateCount = 1
				case "conflict":
					preview.Summary.UnknownCount = 1
				default:
					preview.Summary.UnknownCount = 1
				}

				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
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

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), WebhookChange{Status: result.OK(), Repository: settingsRepositoryOf(repo), Webhook: result.WebhookFrom(payload)})
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

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          "repo.webhook.delete",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "webhookId": args[0]},
						Action:          "delete",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      dryrunpreview.CapabilityFull,
						RequiredState:   []string{"repository webhooks list"},
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1},
				}
				switch predicted {
				case "delete":
					preview.Summary.DeleteCount = 1
				case "no-op":
					preview.Summary.NoopCount = 1
				default:
					preview.Summary.UnknownCount = 1
				}

				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			if err := service.DeleteRepositoryWebhook(cmd.Context(), repo, args[0]); err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), WebhookDeletion{Status: result.OK(), Repository: settingsRepositoryOf(repo), WebhookID: args[0]})
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
			cfg, client, err := deps.LoadConfigAndClient()
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

			// Both renderings read one value. The human path used to pick the
			// same fields out of the raw map a second time, which is two
			// descriptions of one payload and the thing this whole change
			// exists to stop.
			published := pullRequestSettingsFrom(settingsRepositoryOf(repo), settings)
			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), published)
			}

			// "not reported" rather than a default, for the same reason the
			// payload omits the key: an instance that did not answer the
			// question is not an instance that answered no.
			requiredApprovals := "not reported"
			switch {
			case published.RequiredApproversEnabled == nil:
			case !*published.RequiredApproversEnabled:
				requiredApprovals = "disabled"
			case published.RequiredApprovers != nil:
				requiredApprovals = strconv.Itoa(*published.RequiredApprovers)
			}

			requiredTasks := "not reported"
			if published.RequiredAllTasksComplete != nil {
				requiredTasks = strconv.FormatBool(*published.RequiredAllTasksComplete)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Label.Render("Required tasks complete:"), requiredTasks)
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Label.Render("Required approvers:"), requiredApprovals)

			if published.MergeStrategies != nil && len(*published.MergeStrategies) > 0 {
				strategies := *published.MergeStrategies
				fmt.Fprintf(cmd.OutOrStdout(), "%s %d\n", style.Label.Render("Available merge strategies:"), len(strategies))
				for _, strategy := range strategies {
					marker := ""
					if strategy.Enabled {
						marker = "*"
					}
					if published.DefaultMergeStrategy != nil && strategy.ID == *published.DefaultMergeStrategy {
						marker += " (default)"
					}
					fmt.Fprintf(cmd.OutOrStdout(), "- %s%s (%s)\n", strategy.ID, marker, strategy.Name)
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
			cfg, client, err := deps.LoadConfigAndClient()
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

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), MergeChecks{Repository: settingsRepositoryOf(repo), Checks: result.RequiredBuildChecksFromAny(checks)})
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

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          "repo.pull-request-settings.update",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "requiredAllTasksComplete": requiredAllTasksComplete},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      dryrunpreview.CapabilityFull,
						RequiredState:   []string{"repository pull-request settings"},
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1},
				}
				switch predicted {
				case "update":
					preview.Summary.UpdateCount = 1
				case "no-op":
					preview.Summary.NoopCount = 1
				default:
					preview.Summary.UnknownCount = 1
				}

				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			settings, err := service.UpdateRepositoryPullRequestRequiredAllTasks(cmd.Context(), repo, requiredAllTasksComplete)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), pullRequestSettingsFrom(settingsRepositoryOf(repo), settings))
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

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          "repo.pull-request-settings.update-approvers",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "count": requiredApproversCount},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      dryrunpreview.CapabilityFull,
						RequiredState:   []string{"repository pull-request settings"},
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1},
				}
				switch predicted {
				case "update":
					preview.Summary.UpdateCount = 1
				case "no-op":
					preview.Summary.NoopCount = 1
				default:
					preview.Summary.UnknownCount = 1
				}

				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			settings, err := service.UpdateRepositoryPullRequestRequiredApproversCount(cmd.Context(), repo, requiredApproversCount)
			if err != nil {
				return err
			}

			// Both renderings read the same model. Printing the requested count
			// here said the update had taken a value nothing had confirmed:
			// an instance that clamps or ignores it answers differently, and
			// only the JSON path would have shown that.
			updatedApprovers := pullRequestSettingsFrom(settingsRepositoryOf(repo), settings)
			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), updatedApprovers)
			}
			if updatedApprovers.RequiredApprovers == nil {
				fmt.Fprintln(cmd.OutOrStdout(), "Updated pull-request settings: requiredApprovers not reported")
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Updated pull-request settings: requiredApprovers=%d\n", *updatedApprovers.RequiredApprovers)
			return nil
		},
	}
	pullRequestsUpdateApproversCmd.Flags().IntVar(&requiredApproversCount, "count", 2, "Required approvers count (0 disables check)")

	pullRequestsSetStrategyCmd := &cobra.Command{
		Use:   "set-strategy <strategy-id>",
		Short: "Set default merge strategy",
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
			mergeStrategyID := args[0]
			if deps.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), deps.PermissionChecker, client, repo.ProjectKey, repo.Slug, openapi.RepoAdmin); err != nil {
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

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          "repo.pull-request-settings.set-strategy",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "strategyId": mergeStrategyID},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      dryrunpreview.CapabilityFull,
						RequiredState:   []string{"repository pull-request settings"},
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1},
				}
				switch predicted {
				case "update":
					preview.Summary.UpdateCount = 1
				case "no-op":
					preview.Summary.NoopCount = 1
				default:
					preview.Summary.UnknownCount = 1
				}

				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
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

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), pullRequestSettingsFrom(settingsRepositoryOf(repo), updated))
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
	settingsCmd.AddCommand(newRepoSettingsAutoMergeCommand(deps))
	settingsCmd.AddCommand(newRepoSettingsAutoDeclineCommand(deps))

	return settingsCmd
}

func newRepoSettingsAutoMergeCommand(deps Dependencies) *cobra.Command {
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
			cfg, client, err := deps.LoadConfigAndClient()
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

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), autoMergeSettingsFrom(settingsRepositoryOf(repo), res))
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
				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          "repo.settings.auto-merge.set",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "enabled": setEnabled},
						Action:          "update",
						PredictedAction: "update",
						Supported:       true,
						Reason:          "auto-merge settings will be updated",
						Confidence:      dryrunpreview.CapabilityFull,
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1, UpdateCount: 1},
				}
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}
			res, err := service.UpdateRepositoryAutoMergeSettings(cmd.Context(), repo, setEnabled)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), autoMergeSettingsFrom(settingsRepositoryOf(repo), res))
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
				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          "repo.settings.auto-merge.delete",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug)},
						Action:          "delete",
						PredictedAction: "delete",
						Supported:       true,
						Reason:          "auto-merge settings will be deleted",
						Confidence:      dryrunpreview.CapabilityFull,
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1, DeleteCount: 1},
				}
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}
			err = service.DeleteRepositoryAutoMergeSettings(cmd.Context(), repo)
			if err != nil {
				return err
			}
			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), SettingsDeletion{Status: result.Status{Status: "deleted"}, Repository: settingsRepositoryOf(repo), Setting: "autoMerge"})
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

func newRepoSettingsAutoDeclineCommand(deps Dependencies) *cobra.Command {
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
			cfg, client, err := deps.LoadConfigAndClient()
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

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), autoDeclineSettingsFrom(settingsRepositoryOf(repo), res))
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
			cfg, client, err := deps.LoadConfigAndClient()
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
			if deps.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), deps.PermissionChecker, client, repo.ProjectKey, repo.Slug, openapi.RepoAdmin); err != nil {
					return err
				}
				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          "repo.settings.auto-decline.set",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "enabled": setEnabled, "inactivityWeeks": inactivityWeeks},
						Action:          "update",
						PredictedAction: "update",
						Supported:       true,
						Reason:          "auto-decline settings will be updated",
						Confidence:      dryrunpreview.CapabilityFull,
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1, UpdateCount: 1},
				}
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}
			res, err := service.UpdateRepositoryAutoDeclineSettings(cmd.Context(), repo, setEnabled, inactivityWeeks)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), autoDeclineSettingsFrom(settingsRepositoryOf(repo), res))
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
				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          "repo.settings.auto-decline.delete",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug)},
						Action:          "delete",
						PredictedAction: "delete",
						Supported:       true,
						Reason:          "auto-decline settings will be deleted",
						Confidence:      dryrunpreview.CapabilityFull,
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1, DeleteCount: 1},
				}
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}
			err = service.DeleteRepositoryAutoDeclineSettings(cmd.Context(), repo)
			if err != nil {
				return err
			}
			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), SettingsDeletion{Status: result.Status{Status: "deleted"}, Repository: settingsRepositoryOf(repo), Setting: "autoDecline"})
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
