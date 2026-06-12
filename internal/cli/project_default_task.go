package cli

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/style"
	projectservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/project"
)

func newProjectDefaultTaskCommand(options *rootOptions) *cobra.Command {
	defaultTaskCmd := &cobra.Command{
		Use:   "default-task",
		Short: "Manage project default checklist tasks",
	}

	listCmd := &cobra.Command{
		Use:   "list <project-key>",
		Short: "List all default checklist tasks for the project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			service := projectservice.NewService(client)
			tasks, err := service.ListDefaultTasks(cmd.Context(), args[0])
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
	defaultTaskCmd.AddCommand(listCmd)

	var sourceRef string
	var targetRef string
	addCmd := &cobra.Command{
		Use:   "add <project-key> <description>",
		Short: "Add a default checklist task",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			var src *string
			if cmd.Flags().Changed("source-ref") {
				src = &sourceRef
			}
			var tgt *string
			if cmd.Flags().Changed("target-ref") {
				tgt = &targetRef
			}

			service := projectservice.NewService(client)
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckProjectAdmin(cmd.Context(), args[0]); err != nil {
					return err
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "project.default-task.create",
						Target:          map[string]any{"project": args[0], "description": args[1], "source_ref": src, "target_ref": tgt},
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

			task, err := service.AddDefaultTask(cmd.Context(), args[0], args[1], src, tgt)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), task)
			}

			idStr := ""
			if task != nil && task.Id != nil {
				idStr = strconv.FormatInt(*task.Id, 10)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Success.Render("Created default task:"), style.Secondary.Render(idStr))
			return nil
		},
	}
	addCmd.Flags().StringVar(&sourceRef, "source-ref", "", "Source ref matcher (e.g. refs/heads/feature/*)")
	addCmd.Flags().StringVar(&targetRef, "target-ref", "", "Target ref matcher (e.g. refs/heads/master)")
	defaultTaskCmd.AddCommand(addCmd)

	var updateDesc string
	updateCmd := &cobra.Command{
		Use:   "update <project-key> <task-id>",
		Short: "Update a default checklist task",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			var src *string
			if cmd.Flags().Changed("source-ref") {
				src = &sourceRef
			}
			var tgt *string
			if cmd.Flags().Changed("target-ref") {
				tgt = &targetRef
			}

			service := projectservice.NewService(client)
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckProjectAdmin(cmd.Context(), args[0]); err != nil {
					return err
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "project.default-task.update",
						Target:          map[string]any{"project": args[0], "id": args[1], "description": updateDesc, "source_ref": src, "target_ref": tgt},
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

			task, err := service.UpdateDefaultTask(cmd.Context(), args[0], args[1], updateDesc, src, tgt)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), task)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Updated.Render("Updated default task:"), style.Secondary.Render(args[1]))
			return nil
		},
	}
	updateCmd.Flags().StringVar(&updateDesc, "description", "", "New task description")
	updateCmd.Flags().StringVar(&sourceRef, "source-ref", "", "New source ref matcher")
	updateCmd.Flags().StringVar(&targetRef, "target-ref", "", "New target ref matcher")
	_ = updateCmd.MarkFlagRequired("description")
	defaultTaskCmd.AddCommand(updateCmd)

	deleteCmd := &cobra.Command{
		Use:   "delete <project-key> <task-id>",
		Short: "Delete a default checklist task",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			service := projectservice.NewService(client)
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckProjectAdmin(cmd.Context(), args[0]); err != nil {
					return err
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "project.default-task.delete",
						Target:          map[string]any{"project": args[0], "id": args[1]},
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

			if err := service.DeleteDefaultTask(cmd.Context(), args[0], args[1]); err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"status": "ok", "id": args[1]})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Deleted.Render("Deleted default task:"), style.Secondary.Render(args[1]))
			return nil
		},
	}
	defaultTaskCmd.AddCommand(deleteCmd)

	return defaultTaskCmd
}
