package webhookcmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/dryrunpreview"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/enumflag"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/paging"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/preflight"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/reposel"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/style"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/webhookflags"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	reposettings "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/reposettings"
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
	WriteJSONList       func(io.Writer, any, bool) error
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
	if d.WriteJSONList == nil {
		d.WriteJSONList = jsonoutput.WriteList
	}
	return d
}

func New(deps Dependencies) *cobra.Command {
	d := deps.withDefaults()

	var repositorySelector string

	webhookCmd := &cobra.Command{
		Use:   "webhook",
		Short: "Manage repository webhooks",
	}
	webhookCmd.PersistentFlags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug (defaults to BITBUCKET_PROJECT_KEY + BITBUCKET_REPO_SLUG)")

	var getRevealSecret bool
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
			// Both renderings go through the model. The human one used to
			// pretty-print the payload Bitbucket sent, and Bitbucket sends the
			// shared secret back in plaintext on every read, so `bb webhook
			// get` wrote a credential to stdout.
			published := result.WebhookFrom(hook)
			if getRevealSecret {
				if published.Secret = result.WebhookSecretFrom(hook); published.Secret != "" {
					webhookflags.WarnRevealed(cmd.ErrOrStderr(), "the webhook's shared secret")
				}
			}
			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), SingleWebhook{Webhook: published})
			}
			writeWebhookDetail(cmd.OutOrStdout(), published)
			return nil
		},
	}
	webhookflags.RegisterReveal(getCmd, &getRevealSecret, "the webhook's shared secret")

	var name string
	var url string
	var events []string
	var activeVal string
	var updateFields webhookflags.Fields
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
				// enumflag has already refused anything but true or false, at
				// parse time, and normalised the case.
				active = boolPtr(activeVal == "true")
			}
			// Before the dry-run branch: a refusal that only fires on the real
			// run makes the preview predict something the command cannot do.
			input, err := updateFields.UpdateInput(cmd)
			if err != nil {
				return err
			}
			input.Name, input.URL, input.Events, input.Active = name, url, events, active

			service := reposettings.NewService(client)
			if d.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), d.PermissionChecker, client, repo.ProjectKey, repo.Slug, openapi.RepoAdmin); err != nil {
					return err
				}
				target := map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "webhookId": args[0], "name": name, "url": url, "events": events, "active": active}
				webhookflags.Describe(target, input, updateFields.Origins())
				preview := dryrunpreview.New(dryrunpreview.PlanningModeStateful, dryrunpreview.CapabilityFull, dryrunpreview.Item{
					Intent:          "repo.webhook.update",
					Target:          target,
					Action:          "update",
					PredictedAction: "update",
					Supported:       true,
					Reason:          "webhook will be updated",
				})
				return dryrunpreview.Write(cmd.OutOrStdout(), d.JSONEnabled(), preview)
			}
			updated, err := service.UpdateWebhook(cmd.Context(), repo, args[0], input)
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
	// Strict, and for the same reason as its project-scoped twin: an empty
	// --active used to be an error, and reading it as "leave it alone" would
	// silently disable a webhook.
	enumflag.RegisterStrict(updateCmd.Flags(), &activeVal, "active", "", []string{"true", "false"}, "Active status, unchanged when omitted")
	updateFields.RegisterUpdate(updateCmd)

	var webhookTestURL string
	var testRevealSecret bool
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
				if err := preflight.RepoPermission(cmd.Context(), d.PermissionChecker, client, repo.ProjectKey, repo.Slug, openapi.RepoAdmin); err != nil {
					return err
				}
				preview := dryrunpreview.New(dryrunpreview.PlanningModeStateful, dryrunpreview.CapabilityFull, dryrunpreview.Item{
					Intent:          "repo.webhook.test",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "webhookId": args[0]},
					Action:          "update",
					PredictedAction: "update",
					Supported:       true,
					Reason:          "webhook connection test will be triggered",
				})
				return dryrunpreview.Write(cmd.OutOrStdout(), d.JSONEnabled(), preview)
			}
			res, err := service.TestWebhook(cmd.Context(), repo, args[0], webhookTestURL)
			if err != nil {
				return err
			}
			// The delivery record carries the request headers Bitbucket sent,
			// and the endpoint's basic-auth credentials are one of them.
			if testRevealSecret {
				webhookflags.WarnRevealed(cmd.ErrOrStderr(), "the endpoint credentials in the delivery record")
			} else {
				res = result.RedactedDelivery(res)
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

			// Paged before either rendering, not only before the human one.
			// --start and --limit used to narrow the table while --json returned
			// every webhook, so the two answered differently to the same flags.
			webhooks := result.PageOfWebhooks(result.WebhooksFrom(res.Payload), listStart, listPaging.ServiceLimit())

			// WriteJSONList, not WriteJSON: --limit truncates the listing here,
			// and an envelope without meta.limitReached tells a caller nothing
			// was left behind when something was.
			if d.JSONEnabled() {
				return d.WriteJSONList(cmd.OutOrStdout(), Webhooks{Webhooks: webhooks},
					paging.LimitReached(listPaging, len(webhooks)))
			}

			if len(webhooks) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No webhooks found"))
				return nil
			}

			rows := make([][]string, len(webhooks))
			for i, hook := range webhooks {
				rows[i] = []string{
					style.Secondary.Render(strconv.Itoa(hook.ID)),
					hook.Name,
					hook.URL,
					strconv.FormatBool(hook.Active),
					strings.Join(hook.Events, ", "),
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
	var createFields webhookflags.Fields
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

			// Before the dry-run branch, so a preview cannot promise a create
			// that the real run would refuse for the secret it was handed.
			input, err := createFields.CreateInput(cmd)
			if err != nil {
				return err
			}
			input.Name, input.URL, input.Events, input.Active = args[0], args[1], createEvents, createActive

			service := reposettings.NewService(client)
			if d.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), d.PermissionChecker, client, repo.ProjectKey, repo.Slug, openapi.RepoAdmin); err != nil {
					return err
				}

				target := map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "name": args[0], "url": args[1], "events": createEvents, "active": createActive}
				webhookflags.DescribeCreate(target, input, createFields.Origins())
				preview := dryrunpreview.New(dryrunpreview.PlanningModeStateful, dryrunpreview.CapabilityFull, dryrunpreview.Item{
					Intent:          "repo.webhook.create",
					Target:          target,
					Action:          "create",
					PredictedAction: "create",
					Supported:       true,
					Reason:          "webhook will be created",
				})
				return dryrunpreview.Write(cmd.OutOrStdout(), d.JSONEnabled(), preview)
			}

			payload, err := service.CreateRepositoryWebhook(cmd.Context(), repo, input)
			if err != nil {
				return err
			}

			hook := result.WebhookFrom(payload)
			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), Change{
					Status:     result.OK(),
					Repository: result.Repository{ProjectKey: repo.ProjectKey, Slug: repo.Slug},
					Webhook:    hook,
				})
			}

			// Report the name when the server did not send an id back, rather
			// than printing the name where an id belongs: `bb webhook delete`
			// takes the id, so a name shown in its place reads as one.
			if hook.ID != 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Success.Render("Created webhook:"), style.Secondary.Render(strconv.Itoa(hook.ID)))
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Success.Render("Created webhook"), style.Secondary.Render(args[0]))
			return nil
		},
	}
	createCmd.Flags().StringSliceVar(&createEvents, "event", []string{"repo:refs_changed"}, "Webhook event(s) to subscribe to")
	createCmd.Flags().BoolVar(&createActive, "active", true, "Whether the new webhook is active")
	createFields.RegisterCreate(createCmd)

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
				if err := preflight.RepoPermission(cmd.Context(), d.PermissionChecker, client, repo.ProjectKey, repo.Slug, openapi.RepoAdmin); err != nil {
					return err
				}

				preview := dryrunpreview.New(dryrunpreview.PlanningModeStateful, dryrunpreview.CapabilityFull, dryrunpreview.Item{
					Intent:          "repo.webhook.delete",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "webhookId": args[0]},
					Action:          "delete",
					PredictedAction: "delete",
					Supported:       true,
					Reason:          "webhook will be deleted",
				})
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
	webhookflags.RegisterReveal(testCmd, &testRevealSecret, "the endpoint credentials Bitbucket sent")
	webhookCmd.AddCommand(testCmd)
	webhookCmd.AddCommand(statsCmd)
	return webhookCmd
}

func boolPtr(v bool) *bool {
	return &v
}

// writeWebhookDetail renders one webhook for a human.
//
// Every field the model carries, including the two that say whether a
// credential is configured without saying what it is. The secret and the
// endpoint password are the reason this exists rather than a JSON dump.
func writeWebhookDetail(writer io.Writer, hook result.Webhook) {
	fmt.Fprintf(writer, "%s %s\n", style.Label.Render("ID:"), style.Secondary.Render(strconv.Itoa(hook.ID)))
	fmt.Fprintf(writer, "%s %s\n", style.Label.Render("Name:"), hook.Name)
	fmt.Fprintf(writer, "%s %s\n", style.Label.Render("URL:"), hook.URL)
	fmt.Fprintf(writer, "%s %t\n", style.Label.Render("Active:"), hook.Active)
	fmt.Fprintf(writer, "%s %s\n", style.Label.Render("Events:"), strings.Join(hook.Events, ", "))
	if hook.ScopeType != "" {
		fmt.Fprintf(writer, "%s %s\n", style.Label.Render("Scope:"), hook.ScopeType)
	}
	// Absent is its own answer: the server did not say, which is not the same
	// as saying no.
	verification := "not reported"
	if hook.SSLVerificationRequired != nil {
		verification = strconv.FormatBool(*hook.SSLVerificationRequired)
	}
	fmt.Fprintf(writer, "%s %s\n", style.Label.Render("SSL verification required:"), verification)
	fmt.Fprintf(writer, "%s %t\n", style.Label.Render("Shared secret configured:"), hook.SecretConfigured)
	if hook.CredentialsUsername != "" {
		fmt.Fprintf(writer, "%s %s\n", style.Label.Render("Endpoint credentials username:"), hook.CredentialsUsername)
	}
}
