package projectcmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/dryrunpreview"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/paging"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/style"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	projectservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/project"
)

type WebhookModel struct {
	Id     *int     `json:"id,omitempty"`
	Name   *string  `json:"name,omitempty"`
	Url    *string  `json:"url,omitempty"`
	Active *bool    `json:"active,omitempty"`
	Events []string `json:"events,omitempty"`
}

func newProjectWebhookCommand(deps Dependencies) *cobra.Command {
	webhookCmd := &cobra.Command{
		Use:   "webhook",
		Short: "Manage project webhooks",
	}

	var listPaging paging.Options
	var start int
	listCmd := &cobra.Command{
		Use:   "list <project-key>",
		Short: "List all webhooks configured for the project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := projectservice.NewService(client)
			payload, err := service.ListProjectWebhooks(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), Webhooks{Project: args[0], Webhooks: result.WebhooksFrom(payload)})
			}

			var webhooks []WebhookModel
			if payload != nil {
				raw, err := json.Marshal(payload)
				if err == nil {
					_ = json.Unmarshal(raw, &webhooks)
					if len(webhooks) == 0 {
						var paginated struct {
							Values []WebhookModel `json:"values"`
						}
						_ = json.Unmarshal(raw, &paginated)
						webhooks = paginated.Values
					}
				}
			}

			if start < 0 {
				start = 0
			}
			if start >= len(webhooks) {
				webhooks = []WebhookModel{}
			} else {
				end := start + listPaging.ServiceLimit()
				if end > len(webhooks) {
					end = len(webhooks)
				}
				webhooks = webhooks[start:end]
			}

			if len(webhooks) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No webhooks found"))
				return nil
			}

			rows := make([][]string, len(webhooks))
			for i, h := range webhooks {
				idStr := ""
				if h.Id != nil {
					idStr = fmt.Sprintf("%d", *h.Id)
				}
				nameStr := safeString(h.Name)
				urlStr := safeString(h.Url)
				activeStr := "false"
				if h.Active != nil && *h.Active {
					activeStr = "true"
				}
				eventsStr := strings.Join(h.Events, ", ")
				rows[i] = []string{
					style.Secondary.Render(idStr),
					nameStr,
					urlStr,
					activeStr,
					eventsStr,
				}
			}
			style.WriteTable(cmd.OutOrStdout(), rows)
			return nil
		},
	}
	listPaging.Register(listCmd, 25)
	listCmd.Flags().IntVar(&start, "start", 0, "Start index for webhooks listing")
	webhookCmd.AddCommand(listCmd)

	var createEvents []string
	var createActive bool
	createCmd := &cobra.Command{
		Use:   "create <project-key> <name> <url>",
		Short: "Create a new project-level webhook",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := projectservice.NewService(client)
			if deps.DryRunEnabled() {
				if deps.PermissionChecker != nil {
					checker := deps.PermissionChecker(client)
					if checker != nil {
						if err := checker.CheckProjectAdmin(cmd.Context(), args[0]); err != nil {
							return err
						}
					}
				}

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          "project.webhook.create",
						Target:          map[string]any{"project": args[0], "name": args[1], "url": args[2], "events": createEvents, "active": createActive},
						Action:          "create",
						PredictedAction: "create",
						Supported:       true,
						Reason:          "webhook will be created",
						Confidence:      dryrunpreview.CapabilityFull,
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1, CreateCount: 1},
				}
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			created, err := service.CreateProjectWebhook(cmd.Context(), args[0], args[1], args[2], createEvents, createActive)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), WebhookChange{Status: result.OK(), Project: args[0], Webhook: result.WebhookFrom(created)})
			}

			var hook WebhookModel
			if created != nil {
				raw, err := json.Marshal(created)
				if err == nil {
					_ = json.Unmarshal(raw, &hook)
				}
			}

			idStr := ""
			if hook.Id != nil {
				idStr = fmt.Sprintf("%d", *hook.Id)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Success.Render("Created webhook:"), style.Secondary.Render(idStr))
			return nil
		},
	}
	createCmd.Flags().StringSliceVar(&createEvents, "event", []string{"repo:refs_changed"}, "Webhook events to subscribe to")
	createCmd.Flags().BoolVar(&createActive, "active", true, "Whether the webhook is active")
	webhookCmd.AddCommand(createCmd)

	var updateName string
	var updateURL string
	var updateEvents []string
	var updateActiveVal string
	updateCmd := &cobra.Command{
		Use:   "update <project-key> <webhook-id>",
		Short: "Update a project webhook",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			var active *bool
			if cmd.Flags().Changed("active") {
				val := strings.ToLower(strings.TrimSpace(updateActiveVal))
				if val == "true" {
					active = boolPtr(true)
				} else if val == "false" {
					active = boolPtr(false)
				} else {
					return apperrors.New(apperrors.KindValidation, "active must be true or false", nil)
				}
			}

			service := projectservice.NewService(client)
			if deps.DryRunEnabled() {
				if deps.PermissionChecker != nil {
					checker := deps.PermissionChecker(client)
					if checker != nil {
						if err := checker.CheckProjectAdmin(cmd.Context(), args[0]); err != nil {
							return err
						}
					}
				}

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          "project.webhook.update",
						Target:          map[string]any{"project": args[0], "webhook_id": args[1], "name": updateName, "url": updateURL, "events": updateEvents, "active": active},
						Action:          "update",
						PredictedAction: "update",
						Supported:       true,
						Reason:          "webhook will be updated",
						Confidence:      dryrunpreview.CapabilityFull,
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1, UpdateCount: 1},
				}
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			updated, err := service.UpdateProjectWebhook(cmd.Context(), args[0], args[1], updateName, updateURL, updateEvents, active)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), WebhookChange{Status: result.OK(), Project: args[0], Webhook: result.WebhookFrom(updated)})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Updated.Render("Updated webhook:"), style.Secondary.Render(args[1]))
			return nil
		},
	}
	updateCmd.Flags().StringVar(&updateName, "name", "", "New name")
	updateCmd.Flags().StringVar(&updateURL, "url", "", "New URL")
	updateCmd.Flags().StringSliceVar(&updateEvents, "event", nil, "New list of webhook events")
	updateCmd.Flags().StringVar(&updateActiveVal, "active", "", "Active status (true or false)")
	webhookCmd.AddCommand(updateCmd)

	deleteCmd := &cobra.Command{
		Use:   "delete <project-key> <webhook-id>",
		Short: "Delete a project webhook",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := projectservice.NewService(client)
			if deps.DryRunEnabled() {
				if deps.PermissionChecker != nil {
					checker := deps.PermissionChecker(client)
					if checker != nil {
						if err := checker.CheckProjectAdmin(cmd.Context(), args[0]); err != nil {
							return err
						}
					}
				}

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          "project.webhook.delete",
						Target:          map[string]any{"project": args[0], "webhook_id": args[1]},
						Action:          "delete",
						PredictedAction: "delete",
						Supported:       true,
						Reason:          "webhook will be deleted",
						Confidence:      dryrunpreview.CapabilityFull,
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1, DeleteCount: 1},
				}
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			if err := service.DeleteProjectWebhook(cmd.Context(), args[0], args[1]); err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), WebhookDeletion{Status: result.OK(), Project: args[0], WebhookID: args[1]})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Deleted.Render("Deleted webhook:"), style.Secondary.Render(args[1]))
			return nil
		},
	}
	webhookCmd.AddCommand(deleteCmd)

	var projectWebhookTestURL string
	testCmd := &cobra.Command{
		Use:   "test <project-key> <webhook-id>",
		Short: "Trigger a connection test ping",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := projectservice.NewService(client)
			if deps.DryRunEnabled() {
				if deps.PermissionChecker != nil {
					checker := deps.PermissionChecker(client)
					if checker != nil {
						if err := checker.CheckProjectAdmin(cmd.Context(), args[0]); err != nil {
							return err
						}
					}
				}

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          "project.webhook.test",
						Target:          map[string]any{"project": args[0], "webhook_id": args[1]},
						Action:          "update",
						PredictedAction: "update",
						Supported:       true,
						Reason:          "webhook connection test will be triggered",
						Confidence:      dryrunpreview.CapabilityFull,
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1, UpdateCount: 1},
				}
				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			res, err := service.TestProjectWebhook(cmd.Context(), args[0], args[1], projectWebhookTestURL)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), res)
			}

			pretty, err := json.MarshalIndent(res, "", "  ")
			if err != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "%+v\n", res)
				return nil
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(pretty))
			return nil
		},
	}
	testCmd.Flags().StringVar(&projectWebhookTestURL, "url", "", "Test this URL instead of the webhook's configured one")
	webhookCmd.AddCommand(testCmd)

	var summary bool
	statsCmd := &cobra.Command{
		Use:   "stats <project-key> <webhook-id>",
		Short: "Retrieve execution statistics",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := projectservice.NewService(client)
			var res any
			if summary {
				res, err = service.GetProjectWebhookStatisticsSummary(cmd.Context(), args[0], args[1])
			} else {
				res, err = service.GetProjectWebhookStatistics(cmd.Context(), args[0], args[1])
			}
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), res)
			}

			pretty, err := json.MarshalIndent(res, "", "  ")
			if err != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "%+v\n", res)
				return nil
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(pretty))
			return nil
		},
	}
	statsCmd.Flags().BoolVar(&summary, "summary", false, "Get statistics summary instead of detailed logs")
	webhookCmd.AddCommand(statsCmd)

	return webhookCmd
}

func boolPtr(b bool) *bool {
	return &b
}
