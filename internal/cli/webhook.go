package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/style"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
	reposettings "github.com/vriesdemichael/bitbucket-server-cli/internal/services/reposettings"
)

func newWebhookCommand(options *rootOptions) *cobra.Command {
	var repositorySelector string

	webhookCmd := &cobra.Command{
		Use:   "webhook",
		Short: "Manage repository webhooks",
	}
	webhookCmd.PersistentFlags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug (defaults to BITBUCKET_PROJECT_KEY + BITBUCKET_REPO_SLUG)")

	getCmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get a repository webhook by ID",
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
			hook, err := service.GetWebhook(cmd.Context(), repo, args[0])
			if err != nil {
				return err
			}
			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), hook)
			}
			pretty, err := json.MarshalIndent(hook, "", "  ")
			if err != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "%+v\n", hook)
				return nil
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(pretty))
			return nil
		},
	}

	var name string
	var url string
	var events []string
	var activeVal string
	updateCmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update a repository webhook",
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
			var active *bool
			if cmd.Flags().Changed("active") {
				val := strings.ToLower(strings.TrimSpace(activeVal))
				if val == "true" {
					active = boolPtr(true)
				} else if val == "false" {
					active = boolPtr(false)
				} else {
					return apperrors.New(apperrors.KindValidation, "active must be true or false", nil)
				}
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
						Intent:          "repo.webhook.update",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "webhook_id": args[0], "name": name, "url": url, "events": events, "active": active},
						Action:          "update",
						PredictedAction: "update",
						Supported:       true,
						Reason:          "webhook will be updated",
						Confidence:      capabilityFull,
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1, UpdateCount: 1},
				}
				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}
			updated, err := service.UpdateWebhook(cmd.Context(), repo, args[0], name, url, events, active)
			if err != nil {
				return err
			}
			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), updated)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Updated.Render("Updated webhook:"), style.Secondary.Render(args[0]))
			return nil
		},
	}
	updateCmd.Flags().StringVar(&name, "name", "", "New name of the webhook")
	updateCmd.Flags().StringVar(&url, "url", "", "New URL of the webhook")
	updateCmd.Flags().StringSliceVar(&events, "event", nil, "New list of webhook events to subscribe to")
	updateCmd.Flags().StringVar(&activeVal, "active", "", "Active status (true or false)")

	testCmd := &cobra.Command{
		Use:   "test <id>",
		Short: "Test connection to repository webhook URL by sending a ping event",
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
						Intent:          "repo.webhook.test",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "webhook_id": args[0]},
						Action:          "update",
						PredictedAction: "update",
						Supported:       true,
						Reason:          "webhook connection test will be triggered",
						Confidence:      capabilityFull,
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1, UpdateCount: 1},
				}
				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}
			res, err := service.TestWebhook(cmd.Context(), repo, args[0])
			if err != nil {
				return err
			}
			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), res)
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

	var summary bool
	statsCmd := &cobra.Command{
		Use:   "stats <id>",
		Short: "Get repository webhook statistics",
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
			var res any
			if summary {
				res, err = service.GetWebhookStatisticsSummary(cmd.Context(), repo, args[0])
			} else {
				res, err = service.GetWebhookStatistics(cmd.Context(), repo, args[0])
			}
			if err != nil {
				return err
			}
			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), res)
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
	statsCmd.Flags().BoolVar(&summary, "summary", false, "Get statistics summary instead of detailed stats")

	var listLimit int
	var listStart int
	listCmd := &cobra.Command{
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
			res, err := service.ListRepositoryWebhooks(cmd.Context(), repo)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), res.Payload)
			}

			var webhooks []WebhookModel
			if res.Payload != nil {
				raw, err := json.Marshal(res.Payload)
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

			if listStart < 0 {
				listStart = 0
			}
			if listStart >= len(webhooks) {
				webhooks = []WebhookModel{}
			} else {
				end := listStart + listLimit
				if end > len(webhooks) {
					end = len(webhooks)
				}
				webhooks = webhooks[listStart:end]
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
	listCmd.Flags().IntVar(&listLimit, "limit", 25, "Maximum number of webhooks to list")
	listCmd.Flags().IntVar(&listStart, "start", 0, "Start index for webhooks listing")

	webhookCmd.AddCommand(getCmd)
	webhookCmd.AddCommand(listCmd)
	webhookCmd.AddCommand(updateCmd)
	webhookCmd.AddCommand(testCmd)
	webhookCmd.AddCommand(statsCmd)
	return webhookCmd
}

func boolPtr(v bool) *bool {
	return &v
}
