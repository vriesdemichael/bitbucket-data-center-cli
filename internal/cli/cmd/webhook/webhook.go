package webhookcmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/dryrunpreview"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/paging"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/reposel"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/style"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
	reposettings "github.com/vriesdemichael/bitbucket-server-cli/internal/services/reposettings"
)

type PermissionChecker interface {
	CheckRepoPermission(ctx context.Context, projectKey, repoSlug string, permission openapigenerated.GetRepositories1ParamsPermission) error
}

type Dependencies struct {
	JSONEnabled         func() bool
	DryRunEnabled       func() bool
	LoadConfig          func() (config.AppConfig, error)
	LoadConfigAndClient func() (config.AppConfig, *openapigenerated.ClientWithResponses, error)
	WriteJSON           func(io.Writer, any) error
	PermissionChecker   func(*openapigenerated.ClientWithResponses) PermissionChecker
}

func (d Dependencies) withDefaults() Dependencies {
	if d.JSONEnabled == nil {
		d.JSONEnabled = func() bool { return false }
	}
	if d.DryRunEnabled == nil {
		d.DryRunEnabled = func() bool { return false }
	}
	if d.LoadConfig == nil {
		d.LoadConfig = func() (config.AppConfig, error) {
			return config.LoadFromEnv()
		}
	}
	if d.LoadConfigAndClient == nil {
		d.LoadConfigAndClient = func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
			cfg, err := d.LoadConfig()
			if err != nil {
				return config.AppConfig{}, nil, err
			}
			client, err := openapi.NewClientWithResponsesFromConfig(cfg)
			if err != nil {
				return config.AppConfig{}, nil, err
			}
			return cfg, client, nil
		}
	}
	if d.WriteJSON == nil {
		d.WriteJSON = func(w io.Writer, v any) error {
			return jsonoutput.Write(w, v)
		}
	}
	return d
}

type WebhookModel struct {
	Id     *int     `json:"id,omitempty"`
	Name   *string  `json:"name,omitempty"`
	Url    *string  `json:"url,omitempty"`
	Active *bool    `json:"active,omitempty"`
	Events []string `json:"events,omitempty"`
}

func New(deps Dependencies) *cobra.Command {
	d := deps.withDefaults()

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
			cfg, client, err := d.LoadConfigAndClient()
			if err != nil {
				return err
			}
			projectKey, slug, err := reposel.Resolve(repositorySelector, cfg)
			if err != nil {
				return err
			}
			repo := reposettings.RepositoryRef{ProjectKey: projectKey, Slug: slug}

			service := reposettings.NewService(client)
			hook, err := service.GetWebhook(cmd.Context(), repo, args[0])
			if err != nil {
				return err
			}
			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), SingleWebhook{Webhook: result.WebhookFrom(hook)})
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
			cfg, client, err := d.LoadConfigAndClient()
			if err != nil {
				return err
			}
			projectKey, slug, err := reposel.Resolve(repositorySelector, cfg)
			if err != nil {
				return err
			}
			repo := reposettings.RepositoryRef{ProjectKey: projectKey, Slug: slug}

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
			if d.DryRunEnabled() {
				if d.PermissionChecker != nil {
					checker := d.PermissionChecker(client)
					if checker != nil {
						if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapi.RepoAdmin); err != nil {
							return err
						}
					}
				}
				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          "repo.webhook.update",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "webhookId": args[0], "name": name, "url": url, "events": events, "active": active},
						Action:          "update",
						PredictedAction: "update",
						Supported:       true,
						Reason:          "webhook will be updated",
						Confidence:      dryrunpreview.CapabilityFull,
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1, UpdateCount: 1},
				}
				return dryrunpreview.Write(cmd.OutOrStdout(), d.JSONEnabled(), preview)
			}
			updated, err := service.UpdateWebhook(cmd.Context(), repo, args[0], name, url, events, active)
			if err != nil {
				return err
			}
			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), Change{
					Status:     result.OK(),
					Repository: result.Repository{ProjectKey: repo.ProjectKey, Slug: repo.Slug},
					Webhook:    result.WebhookFrom(updated),
				})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Updated.Render("Updated webhook:"), style.Secondary.Render(args[0]))
			return nil
		},
	}
	updateCmd.Flags().StringVar(&name, "name", "", "New name of the webhook")
	updateCmd.Flags().StringVar(&url, "url", "", "New URL of the webhook")
	updateCmd.Flags().StringSliceVar(&events, "event", nil, "New list of webhook events to subscribe to")
	// A string rather than a bool because update has three states to express:
	// activate, deactivate, and leave the current setting alone. `create` takes
	// a bool for the same flag, where there is nothing to leave alone.
	updateCmd.Flags().StringVar(&activeVal, "active", "", "Active status (true or false); unchanged when omitted")

	var webhookTestURL string
	testCmd := &cobra.Command{
		Use:   "test <id>",
		Short: "Test connection to repository webhook URL by sending a ping event",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := d.LoadConfigAndClient()
			if err != nil {
				return err
			}
			projectKey, slug, err := reposel.Resolve(repositorySelector, cfg)
			if err != nil {
				return err
			}
			repo := reposettings.RepositoryRef{ProjectKey: projectKey, Slug: slug}

			service := reposettings.NewService(client)
			if d.DryRunEnabled() {
				if d.PermissionChecker != nil {
					checker := d.PermissionChecker(client)
					if checker != nil {
						if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapi.RepoAdmin); err != nil {
							return err
						}
					}
				}
				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          "repo.webhook.test",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "webhookId": args[0]},
						Action:          "update",
						PredictedAction: "update",
						Supported:       true,
						Reason:          "webhook connection test will be triggered",
						Confidence:      dryrunpreview.CapabilityFull,
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1, UpdateCount: 1},
				}
				return dryrunpreview.Write(cmd.OutOrStdout(), d.JSONEnabled(), preview)
			}
			res, err := service.TestWebhook(cmd.Context(), repo, args[0], webhookTestURL)
			if err != nil {
				return err
			}
			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), res)
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
			cfg, client, err := d.LoadConfigAndClient()
			if err != nil {
				return err
			}
			projectKey, slug, err := reposel.Resolve(repositorySelector, cfg)
			if err != nil {
				return err
			}
			repo := reposettings.RepositoryRef{ProjectKey: projectKey, Slug: slug}

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
			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), res)
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

	var listPaging paging.Options
	var listStart int
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List repository webhooks",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := d.LoadConfigAndClient()
			if err != nil {
				return err
			}
			projectKey, slug, err := reposel.Resolve(repositorySelector, cfg)
			if err != nil {
				return err
			}
			repo := reposettings.RepositoryRef{ProjectKey: projectKey, Slug: slug}

			service := reposettings.NewService(client)
			res, err := service.ListRepositoryWebhooks(cmd.Context(), repo)
			if err != nil {
				return err
			}

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), Webhooks{Webhooks: result.WebhooksFrom(res.Payload)})
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
				end := listStart + listPaging.ServiceLimit()
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
	listPaging.Register(listCmd, 25)
	listCmd.Flags().IntVar(&listStart, "start", 0, "Start index for webhooks listing")

	var createEvents []string
	var createActive bool
	createCmd := &cobra.Command{
		Use:   "create <name> <url>",
		Short: "Create a repository webhook",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := d.LoadConfigAndClient()
			if err != nil {
				return err
			}
			projectKey, slug, err := reposel.Resolve(repositorySelector, cfg)
			if err != nil {
				return err
			}
			repo := reposettings.RepositoryRef{ProjectKey: projectKey, Slug: slug}

			service := reposettings.NewService(client)
			if d.DryRunEnabled() {
				if d.PermissionChecker != nil {
					checker := d.PermissionChecker(client)
					if checker != nil {
						if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapi.RepoAdmin); err != nil {
							return err
						}
					}
				}

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          "repo.webhook.create",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "name": args[0], "url": args[1], "events": createEvents, "active": createActive},
						Action:          "create",
						PredictedAction: "create",
						Supported:       true,
						Reason:          "webhook will be created",
						Confidence:      dryrunpreview.CapabilityFull,
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1, CreateCount: 1},
				}
				return dryrunpreview.Write(cmd.OutOrStdout(), d.JSONEnabled(), preview)
			}

			payload, err := service.CreateRepositoryWebhook(cmd.Context(), repo, reposettings.WebhookCreateInput{
				Name:   args[0],
				URL:    args[1],
				Events: createEvents,
				Active: createActive,
			})
			if err != nil {
				return err
			}

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), Change{
					Status:     result.OK(),
					Repository: result.Repository{ProjectKey: repo.ProjectKey, Slug: repo.Slug},
					Webhook:    result.WebhookFrom(payload),
				})
			}

			var hook WebhookModel
			if payload != nil {
				raw, err := json.Marshal(payload)
				if err == nil {
					_ = json.Unmarshal(raw, &hook)
				}
			}

			// Report the name when the server did not send an id back, rather
			// than printing the name where an id belongs: `bb webhook delete`
			// takes the id, so a name shown in its place reads as one.
			if hook.Id != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Success.Render("Created webhook:"), style.Secondary.Render(fmt.Sprintf("%d", *hook.Id)))
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Success.Render("Created webhook"), style.Secondary.Render(args[0]))
			return nil
		},
	}
	createCmd.Flags().StringSliceVar(&createEvents, "event", []string{"repo:refs_changed"}, "Webhook event(s) to subscribe to")
	createCmd.Flags().BoolVar(&createActive, "active", true, "Whether the new webhook is active")

	deleteCmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a repository webhook",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := d.LoadConfigAndClient()
			if err != nil {
				return err
			}
			projectKey, slug, err := reposel.Resolve(repositorySelector, cfg)
			if err != nil {
				return err
			}
			repo := reposettings.RepositoryRef{ProjectKey: projectKey, Slug: slug}

			service := reposettings.NewService(client)
			if d.DryRunEnabled() {
				if d.PermissionChecker != nil {
					checker := d.PermissionChecker(client)
					if checker != nil {
						if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapi.RepoAdmin); err != nil {
							return err
						}
					}
				}

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          "repo.webhook.delete",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "webhookId": args[0]},
						Action:          "delete",
						PredictedAction: "delete",
						Supported:       true,
						Reason:          "webhook will be deleted",
						Confidence:      dryrunpreview.CapabilityFull,
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1, DeleteCount: 1},
				}
				return dryrunpreview.Write(cmd.OutOrStdout(), d.JSONEnabled(), preview)
			}

			if err := service.DeleteRepositoryWebhook(cmd.Context(), repo, args[0]); err != nil {
				return err
			}

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), Deletion{
					Status:     result.OK(),
					Repository: result.Repository{ProjectKey: repo.ProjectKey, Slug: repo.Slug},
					WebhookID:  args[0],
				})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Deleted.Render("Deleted webhook:"), style.Secondary.Render(args[0]))
			return nil
		},
	}

	webhookCmd.AddCommand(getCmd)
	webhookCmd.AddCommand(listCmd)
	webhookCmd.AddCommand(createCmd)
	webhookCmd.AddCommand(updateCmd)
	webhookCmd.AddCommand(deleteCmd)
	testCmd.Flags().StringVar(&webhookTestURL, "url", "", "Test this URL instead of the webhook's configured one")
	webhookCmd.AddCommand(testCmd)
	webhookCmd.AddCommand(statsCmd)
	return webhookCmd
}

func boolPtr(v bool) *bool {
	return &v
}

func safeString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
