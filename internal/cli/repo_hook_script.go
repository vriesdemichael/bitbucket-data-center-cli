package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/style"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
	hookservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/hook"
)

func newRepoHookScriptCommand(options *rootOptions) *cobra.Command {
	var repositorySelector string

	hookScriptCmd := &cobra.Command{
		Use:   "hook-script",
		Short: "Manage repository hook scripts",
	}

	hookScriptCmd.PersistentFlags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List configured hook scripts on a repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repoRef, err := resolveRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := hookservice.NewService(client)
			scripts, err := service.ListHookScripts(cmd.Context(), repoRef.ProjectKey, repoRef.Slug, 100)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), scripts)
			}

			if len(scripts) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No hook scripts configured"))
				return nil
			}

			rows := make([][]string, len(scripts))
			for i, s := range scripts {
				idStr := ""
				nameStr := ""
				descStr := ""
				if s.Script != nil {
					if s.Script.Id != nil {
						idStr = strconv.FormatInt(*s.Script.Id, 10)
					}
					if s.Script.Name != nil {
						nameStr = *s.Script.Name
					}
					if s.Script.Description != nil {
						descStr = *s.Script.Description
					}
				}
				triggers := ""
				if s.TriggerIds != nil {
					triggers = strings.Join(*s.TriggerIds, ", ")
				}
				rows[i] = []string{style.Resource.Render(idStr), nameStr, descStr, triggers}
			}
			style.WriteTable(cmd.OutOrStdout(), rows)

			return nil
		},
	}

	var triggerIds []string
	setCmd := &cobra.Command{
		Use:   "set <script-id>",
		Short: "Configure or update triggers for a hook script on a repository",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repoRef, err := resolveRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			scriptID := args[0]
			service := hookservice.NewService(client)

			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repoRef.ProjectKey, repoRef.Slug, openapigenerated.REPOADMIN); err != nil {
					return err
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityPartial,
					Items: []dryRunItem{{
						Intent: "repo.hook-script.set",
						Target: map[string]any{
							"repository": fmt.Sprintf("%s/%s", repoRef.ProjectKey, repoRef.Slug),
							"script_id":  scriptID,
							"triggers":   triggerIds,
						},
						Action:          "update",
						PredictedAction: "update",
						Supported:       true,
						Reason:          "hook script will be set/updated",
						Confidence:      capabilityPartial,
						RequiredState:   []string{"repository admin access"},
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1, UpdateCount: 1},
				}
				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}

			err = service.SetHookScript(cmd.Context(), repoRef.ProjectKey, repoRef.Slug, scriptID, triggerIds)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]string{
					"status":    "success",
					"script_id": scriptID,
				})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Successfully configured hook script %s on %s/%s\n", scriptID, repoRef.ProjectKey, repoRef.Slug)
			return nil
		},
	}
	setCmd.Flags().StringSliceVar(&triggerIds, "trigger", nil, "Trigger IDs to configure for this hook script")

	removeCmd := &cobra.Command{
		Use:   "remove <script-id>",
		Short: "Remove a hook script configuration from a repository",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repoRef, err := resolveRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			scriptID := args[0]
			service := hookservice.NewService(client)

			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repoRef.ProjectKey, repoRef.Slug, openapigenerated.REPOADMIN); err != nil {
					return err
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityPartial,
					Items: []dryRunItem{{
						Intent: "repo.hook-script.remove",
						Target: map[string]any{
							"repository": fmt.Sprintf("%s/%s", repoRef.ProjectKey, repoRef.Slug),
							"script_id":  scriptID,
						},
						Action:          "delete",
						PredictedAction: "delete",
						Supported:       true,
						Reason:          "hook script configuration will be removed",
						Confidence:      capabilityPartial,
						RequiredState:   []string{"repository admin access"},
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1, DeleteCount: 1},
				}
				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}

			err = service.RemoveHookScript(cmd.Context(), repoRef.ProjectKey, repoRef.Slug, scriptID)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]string{
					"status":    "success",
					"script_id": scriptID,
				})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Successfully removed hook script %s from %s/%s\n", scriptID, repoRef.ProjectKey, repoRef.Slug)
			return nil
		},
	}

	hookScriptCmd.AddCommand(listCmd)
	hookScriptCmd.AddCommand(setCmd)
	hookScriptCmd.AddCommand(removeCmd)

	return hookScriptCmd
}
