package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"

	"github.com/spf13/cobra"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
	hookservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/hook"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/style"
)

func newHookCommand(options *rootOptions) *cobra.Command {
	var projectKey string
	var repositorySelector string
	var limit int
	var start int

	hookCmd := &cobra.Command{
		Use:   "hook",
		Short: "Manage repository/project hooks",
	}

	hookCmd.PersistentFlags().StringVar(&projectKey, "project", "", "Project key")
	hookCmd.PersistentFlags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List hooks",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			service := hookservice.NewService(client)

			if repositorySelector != "" {
				repo, err := resolveRepositoryReference(repositorySelector, cfg)
				if err != nil {
					return err
				}
				hooks, err := service.ListRepositoryHooks(cmd.Context(), repo.ProjectKey, repo.Slug, limit, start)
				if err != nil {
					return err
				}
				if options.JSON {
					return writeJSON(cmd.OutOrStdout(), map[string]any{"hooks": hooks})
				}
				printHooks(cmd, hooks)
				return nil
			}

			if projectKey == "" {
				projectKey = cfg.ProjectKey
			}
			if projectKey == "" {
				return fmt.Errorf("project key is required (use --project or --repo)")
			}

			hooks, err := service.ListProjectHooks(cmd.Context(), projectKey, limit, start)
			if err != nil {
				return err
			}
			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"hooks": hooks})
			}
			printHooks(cmd, hooks)
			return nil
		},
	}
	listCmd.Flags().IntVar(&limit, "limit", 25, "Maximum number of hooks to list")
	listCmd.Flags().IntVar(&start, "start", 0, "Start offset for list operations")
	hookCmd.AddCommand(listCmd)

	enableCmd := &cobra.Command{
		Use:   "enable <hook-key>",
		Short: "Enable a hook",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			service := hookservice.NewService(client)
			hookKey := args[0]

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

					hooks, err := service.ListRepositoryHooks(cmd.Context(), repo.ProjectKey, repo.Slug, 100, 0)
					if err != nil {
						return err
					}
					predicted := "blocked"
					reason := "hook not found in repository scope"
					blockingReasons := []string{"hook not found"}
					if hook, found := findHookByKey(hooks, hookKey); found {
						blockingReasons = nil
						if hook.Enabled != nil && *hook.Enabled {
							predicted = "no-op"
							reason = "hook already enabled"
						} else {
							predicted = "update"
							reason = "hook will be enabled"
						}
					}

					preview := dryRunPreview{
						DryRun:       true,
						PlanningMode: planningModeStateful,
						Capability:   capabilityFull,
						Items: []dryRunItem{{
							Intent:          "hook.enable",
							Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "hook_key": hookKey},
							Action:          "update",
							PredictedAction: predicted,
							Supported:       true,
							Reason:          reason,
							Confidence:      capabilityFull,
							RequiredState:   []string{"repository hooks list"},
							BlockingReasons: blockingReasons,
						}},
						Summary: dryRunSummary{Total: 1, Supported: 1},
					}
					applyDryRunSummaryPredicted(&preview.Summary, predicted)
					return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
				}
				hook, err := service.EnableRepositoryHook(cmd.Context(), repo.ProjectKey, repo.Slug, hookKey)
				if err != nil {
					return err
				}
				if options.JSON {
					return writeJSON(cmd.OutOrStdout(), map[string]any{"hook": hook})
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s %s for repository %s\n", style.Success.Render("Enabled hook"), style.Resource.Render(hookKey), style.Resource.Render(repo.ProjectKey+"/"+repo.Slug))
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

				hooks, err := service.ListProjectHooks(cmd.Context(), projectKey, 100, 0)
				if err != nil {
					return err
				}
				predicted := "blocked"
				reason := "hook not found in project scope"
				blockingReasons := []string{"hook not found"}
				if hook, found := findHookByKey(hooks, hookKey); found {
					blockingReasons = nil
					if hook.Enabled != nil && *hook.Enabled {
						predicted = "no-op"
						reason = "hook already enabled"
					} else {
						predicted = "update"
						reason = "hook will be enabled"
					}
				}
				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "hook.enable",
						Target:          map[string]any{"project": projectKey, "hook_key": hookKey},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"project hooks list"},
						BlockingReasons: blockingReasons,
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1},
				}
				applyDryRunSummaryPredicted(&preview.Summary, predicted)
				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}

			hook, err := service.EnableProjectHook(cmd.Context(), projectKey, hookKey)
			if err != nil {
				return err
			}
			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"hook": hook})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s for project %s\n", style.Success.Render("Enabled hook"), style.Resource.Render(hookKey), projectKey)
			return nil
		},
	}
	hookCmd.AddCommand(enableCmd)

	disableCmd := &cobra.Command{
		Use:   "disable <hook-key>",
		Short: "Disable a hook",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			service := hookservice.NewService(client)
			hookKey := args[0]

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

					hooks, err := service.ListRepositoryHooks(cmd.Context(), repo.ProjectKey, repo.Slug, 100, 0)
					if err != nil {
						return err
					}
					predicted := "blocked"
					reason := "hook not found in repository scope"
					blockingReasons := []string{"hook not found"}
					if hook, found := findHookByKey(hooks, hookKey); found {
						blockingReasons = nil
						if hook.Enabled != nil && !*hook.Enabled {
							predicted = "no-op"
							reason = "hook already disabled"
						} else {
							predicted = "update"
							reason = "hook will be disabled"
						}
					}

					preview := dryRunPreview{
						DryRun:       true,
						PlanningMode: planningModeStateful,
						Capability:   capabilityFull,
						Items: []dryRunItem{{
							Intent:          "hook.disable",
							Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "hook_key": hookKey},
							Action:          "update",
							PredictedAction: predicted,
							Supported:       true,
							Reason:          reason,
							Confidence:      capabilityFull,
							RequiredState:   []string{"repository hooks list"},
							BlockingReasons: blockingReasons,
						}},
						Summary: dryRunSummary{Total: 1, Supported: 1},
					}
					applyDryRunSummaryPredicted(&preview.Summary, predicted)
					return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
				}
				if err := service.DisableRepositoryHook(cmd.Context(), repo.ProjectKey, repo.Slug, hookKey); err != nil {
					return err
				}
				if options.JSON {
					return writeJSON(cmd.OutOrStdout(), map[string]string{"status": "ok"})
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s %s for repository %s\n", style.Deleted.Render("Disabled hook"), style.Resource.Render(hookKey), style.Resource.Render(repo.ProjectKey+"/"+repo.Slug))
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

				hooks, err := service.ListProjectHooks(cmd.Context(), projectKey, 100, 0)
				if err != nil {
					return err
				}
				predicted := "blocked"
				reason := "hook not found in project scope"
				blockingReasons := []string{"hook not found"}
				if hook, found := findHookByKey(hooks, hookKey); found {
					blockingReasons = nil
					if hook.Enabled != nil && !*hook.Enabled {
						predicted = "no-op"
						reason = "hook already disabled"
					} else {
						predicted = "update"
						reason = "hook will be disabled"
					}
				}
				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "hook.disable",
						Target:          map[string]any{"project": projectKey, "hook_key": hookKey},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"project hooks list"},
						BlockingReasons: blockingReasons,
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1},
				}
				applyDryRunSummaryPredicted(&preview.Summary, predicted)
				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}

			if err := service.DisableProjectHook(cmd.Context(), projectKey, hookKey); err != nil {
				return err
			}
			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]string{"status": "ok"})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s for project %s\n", style.Deleted.Render("Disabled hook"), style.Resource.Render(hookKey), projectKey)
			return nil
		},
	}
	hookCmd.AddCommand(disableCmd)

	var configFile string
	configureCmd := &cobra.Command{
		Use:   "configure <hook-key> [settings-json]",
		Short: "Configure hook settings",
		Long:  "Configure hook settings using JSON from argument, file (--config-file), or stdin (-)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			service := hookservice.NewService(client)
			if len(args) < 1 {
				return fmt.Errorf("hook key is required")
			}
			hookKey := args[0]

			var settingsData []byte
			hasArgSettings := len(args) > 1
			hasConfigFile := configFile != ""

			if hasArgSettings && hasConfigFile {
				return fmt.Errorf("cannot provide settings as both an argument and via --config-file; please use only one")
			}

			if hasArgSettings {
				if args[1] == "-" {
					settingsData, err = io.ReadAll(cmd.InOrStdin())
					if err != nil {
						return fmt.Errorf("failed to read stdin: %w", err)
					}
				} else {
					settingsData = []byte(args[1])
				}
			} else if hasConfigFile {
				settingsData, err = os.ReadFile(configFile)
				if err != nil {
					return fmt.Errorf("failed to read config file: %w", err)
				}
			} else {
				// No settings provided, just get current settings
				if repositorySelector != "" {
					repo, err := resolveRepositoryReference(repositorySelector, cfg)
					if err != nil {
						return err
					}
					settings, err := service.GetRepositoryHookSettings(cmd.Context(), repo.ProjectKey, repo.Slug, hookKey)
					if err != nil {
						return err
					}
					if options.JSON {
						return writeJSON(cmd.OutOrStdout(), settings)
					}
					pretty, err := json.MarshalIndent(settings, "", "  ")
					if err != nil {
						fmt.Fprintf(cmd.OutOrStdout(), "%+v\n", settings)
						return nil
					}
					fmt.Fprintln(cmd.OutOrStdout(), string(pretty))
					return nil
				}
				if projectKey == "" {
					projectKey = cfg.ProjectKey
				}
				if projectKey == "" {
					return fmt.Errorf("project key is required (use --project or --repo)")
				}
				settings, err := service.GetProjectHookSettings(cmd.Context(), projectKey, hookKey)
				if err != nil {
					return err
				}
				if options.JSON {
					return writeJSON(cmd.OutOrStdout(), settings)
				}
				pretty, err := json.MarshalIndent(settings, "", "  ")
				if err != nil {
					fmt.Fprintf(cmd.OutOrStdout(), "%+v\n", settings)
					return nil
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(pretty))
				return nil
			}

			var settings map[string]any
			if err := json.Unmarshal(settingsData, &settings); err != nil {
				return fmt.Errorf("invalid settings JSON: %w", err)
			}
			if settings == nil {
				return fmt.Errorf("invalid settings JSON: settings must be a JSON object, not null")
			}

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

					hooks, err := service.ListRepositoryHooks(cmd.Context(), repo.ProjectKey, repo.Slug, 100, 0)
					if err != nil {
						return err
					}
					predicted := "blocked"
					reason := "hook not found in repository scope"
					blockingReasons := []string{"hook not found"}
					requiredState := []string{"repository hooks list"}
					if _, found := findHookByKey(hooks, hookKey); found {
						currentSettings, err := service.GetRepositoryHookSettings(cmd.Context(), repo.ProjectKey, repo.Slug, hookKey)
						if err != nil {
							return err
						}
						requiredState = append(requiredState, "repository hook settings")
						blockingReasons = nil
						if reflect.DeepEqual(normalizeJSONShape(currentSettings), normalizeJSONShape(settings)) {
							predicted = "no-op"
							reason = "hook settings already match requested configuration"
						} else {
							predicted = "update"
							reason = "hook settings will be updated"
						}
					}
					preview := dryRunPreview{
						DryRun:       true,
						PlanningMode: planningModeStateful,
						Capability:   capabilityFull,
						Items: []dryRunItem{{
							Intent:          "hook.configure",
							Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "hook_key": hookKey},
							Action:          "update",
							PredictedAction: predicted,
							Supported:       true,
							Reason:          reason,
							Confidence:      capabilityFull,
							RequiredState:   requiredState,
							BlockingReasons: blockingReasons,
						}},
						Summary: dryRunSummary{Total: 1, Supported: 1},
					}
					applyDryRunSummaryPredicted(&preview.Summary, predicted)
					return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
				}
				result, err := service.SetRepositoryHookSettings(cmd.Context(), repo.ProjectKey, repo.Slug, hookKey, settings)
				if err != nil {
					return err
				}
				if options.JSON {
					return writeJSON(cmd.OutOrStdout(), result)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s %s for repository %s\n", style.Updated.Render("Configured hook"), style.Resource.Render(hookKey), style.Resource.Render(repo.ProjectKey+"/"+repo.Slug))
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

				hooks, err := service.ListProjectHooks(cmd.Context(), projectKey, 100, 0)
				if err != nil {
					return err
				}
				predicted := "blocked"
				reason := "hook not found in project scope"
				blockingReasons := []string{"hook not found"}
				requiredState := []string{"project hooks list"}
				if _, found := findHookByKey(hooks, hookKey); found {
					currentSettings, err := service.GetProjectHookSettings(cmd.Context(), projectKey, hookKey)
					if err != nil {
						return err
					}
					requiredState = append(requiredState, "project hook settings")
					blockingReasons = nil
					if reflect.DeepEqual(normalizeJSONShape(currentSettings), normalizeJSONShape(settings)) {
						predicted = "no-op"
						reason = "hook settings already match requested configuration"
					} else {
						predicted = "update"
						reason = "hook settings will be updated"
					}
				}
				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "hook.configure",
						Target:          map[string]any{"project": projectKey, "hook_key": hookKey},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   requiredState,
						BlockingReasons: blockingReasons,
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1},
				}
				applyDryRunSummaryPredicted(&preview.Summary, predicted)
				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}
			result, err := service.SetProjectHookSettings(cmd.Context(), projectKey, hookKey, settings)
			if err != nil {
				return err
			}
			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), result)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s for project %s\n", style.Updated.Render("Configured hook"), style.Resource.Render(hookKey), projectKey)
			return nil
		},
	}
	configureCmd.Flags().StringVar(&configFile, "config-file", "", "JSON file containing hook settings")
	hookCmd.AddCommand(configureCmd)

	return hookCmd
}

func printHooks(cmd *cobra.Command, hooks []openapigenerated.RestRepositoryHook) {
	if len(hooks) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No hooks found"))
		return
	}
	rows := make([][]string, len(hooks))
	for i, h := range hooks {
		enabledStr := "disabled"
		if h.Enabled != nil && *h.Enabled {
			enabledStr = "enabled"
		}
		name := ""
		if h.Details != nil && h.Details.Name != nil {
			name = *h.Details.Name
		}
		key := ""
		if h.Details != nil && h.Details.Key != nil {
			key = *h.Details.Key
		}
		rows[i] = []string{style.Resource.Render(key), style.ActionStyle(enabledStr).Render(enabledStr), name}
	}
	style.WriteTable(cmd.OutOrStdout(), rows)
}

func findHookByKey(hooks []openapigenerated.RestRepositoryHook, hookKey string) (openapigenerated.RestRepositoryHook, bool) {
	trimmedHookKey := strings.TrimSpace(hookKey)
	if trimmedHookKey == "" {
		return openapigenerated.RestRepositoryHook{}, false
	}

	for _, hook := range hooks {
		if hook.Details == nil || hook.Details.Key == nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(*hook.Details.Key), trimmedHookKey) {
			return hook, true
		}
	}

	return openapigenerated.RestRepositoryHook{}, false
}

func applyDryRunSummaryPredicted(summary *dryRunSummary, predicted string) {
	if summary == nil {
		return
	}

	switch predicted {
	case "no-op":
		summary.NoopCount = 1
	case "create":
		summary.CreateCount = 1
	case "update":
		summary.UpdateCount = 1
	case "delete":
		summary.DeleteCount = 1
	default:
		summary.UnknownCount = 1
	}
}

func normalizeJSONShape(value any) any {
	raw, err := json.Marshal(value)
	if err != nil {
		return value
	}

	var normalized any
	if err := json.Unmarshal(raw, &normalized); err != nil {
		return value
	}

	return normalized
}
