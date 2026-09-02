package auth

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/git"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

type usersClient interface {
	GetUsers2WithResponse(ctx context.Context, params *openapigenerated.GetUsers2Params, reqEditors ...openapigenerated.RequestEditorFn) (*openapigenerated.GetUsers2Response, error)
	GetUserWithResponse(ctx context.Context, userSlug string, reqEditors ...openapigenerated.RequestEditorFn) (*openapigenerated.GetUserResponse, error)
}

type repositoriesClient interface {
	GetRepositoriesRecentlyAccessedWithResponse(ctx context.Context, params *openapigenerated.GetRepositoriesRecentlyAccessedParams, reqEditors ...openapigenerated.RequestEditorFn) (*openapigenerated.GetRepositoriesRecentlyAccessedResponse, error)
	GetRepositories1WithResponse(ctx context.Context, params *openapigenerated.GetRepositories1Params, reqEditors ...openapigenerated.RequestEditorFn) (*openapigenerated.GetRepositories1Response, error)
}

type Dependencies struct {
	JSONEnabled func() bool
	LoadConfig  func() (config.AppConfig, error)
	// RuntimeOverrides carries the global flags, for the values auth reads
	// directly rather than through a config load.
	RuntimeOverrides func() config.Overrides

	// LoadConfigWithOverrides steers resolution for the commands that take a
	// --host flag. They used to publish the value with os.Setenv, which
	// outlived the command and destroyed a real BITBUCKET_URL rather than
	// outranking it for one invocation (ADR-021, issue #458).
	//
	// Optional only for a caller that never passes a host: loadWith falls back
	// to LoadConfig when none is given, and reports an internal error when one
	// is given and this is nil, rather than resolving the wrong instance.
	LoadConfigWithOverrides func(config.Overrides) (config.AppConfig, error)
	WriteJSON               func(io.Writer, any) error
	NewUsersClient          func(config.AppConfig) (usersClient, error)
	NewReposClient          func(config.AppConfig) (repositoriesClient, error)
	// ConfigureGitCredentialHelper writes the git configuration that points git
	// at bb for credentials. Injected so setup-git can be tested without
	// mutating the developer's real git configuration.
	ConfigureGitCredentialHelper func(ctx context.Context, key, value string, global, force bool) error
	// GitBackend reads git configuration for the status checks. Injected for
	// the same reason as the writer above: without it, running auth status in a
	// test shells out to real git and reads whatever global configuration the
	// machine happens to have, so the result depends on the developer rather
	// than on the code.
	GitBackend func() git.Backend
}

func New(deps Dependencies) *cobra.Command {
	if deps.LoadConfig == nil {
		deps.LoadConfig = func() (config.AppConfig, error) {
			return config.AppConfig{}, apperrors.New(apperrors.KindInternal, "auth command dependency LoadConfig is not configured", nil)
		}
	}

	if deps.WriteJSON == nil {
		deps.WriteJSON = func(io.Writer, any) error {
			return apperrors.New(apperrors.KindInternal, "auth command dependency WriteJSON is not configured", nil)
		}
	}

	if deps.GitBackend == nil {
		deps.GitBackend = defaultGitBackend
	}

	if deps.NewUsersClient == nil {
		deps.NewUsersClient = func(cfg config.AppConfig) (usersClient, error) {
			return openapi.NewClientWithResponsesFromConfig(cfg)
		}
	}

	if deps.NewReposClient == nil {
		deps.NewReposClient = func(cfg config.AppConfig) (repositoriesClient, error) {
			return openapi.NewClientWithResponsesFromConfig(cfg)
		}
	}

	if deps.ConfigureGitCredentialHelper == nil {
		deps.ConfigureGitCredentialHelper = defaultConfigureGitCredentialHelper
	}

	authCmd := &cobra.Command{
		Use:   "auth",
		Short: "Authentication commands",
	}

	isJSON := func() bool {
		if deps.JSONEnabled == nil {
			return false
		}
		return deps.JSONEnabled()
	}

	var statusHost string
	var statusCheckExit bool
	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show the configured target and verify it works",
		Long: `Show the configured target and verify it works.

Reports the resolved host, how the credential is stored, whether that credential
still authenticates, and whether git is set up to authenticate through bb.

Reporting alone is not enough to be useful: an expired token, an unreachable
host and a working setup used to produce the same confident output. Each line
now says which it is, and what to do when it is not the last one.

Lines marked ! are advisory: they report something worth knowing that does not
mean the setup is broken. The git credential helper is one — it is needed to git
push and irrelevant to anything that only calls the API — so it is reported but
never fails the command.

Exit status is unchanged by default, so existing scripts keep working. Pass
--check to exit non-zero when a non-advisory check fails, which is the form
worth putting in CI.

Under --json the exit status is always zero and the verdict is the "ok" field.
Machine output is a single document on stdout, so a failing exit would replace
the findings with an error envelope — losing exactly the detail that was asked
for.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := deps.loadWith(statusHost)
			if err != nil {
				return err
			}

			checks := []statusCheck{
				identityState(cmd.Context(), cfg, deps.NewUsersClient),
				gitCredentialHelperState(cmd.Context(), deps.GitBackend(), cfg.BitbucketURL),
			}

			// Advisory failures are reported but do not make the setup unhealthy.
			// See the note on statusCheck.Advisory.
			allOK := true
			for _, check := range checks {
				if !check.OK && !check.Advisory {
					allOK = false
				}
			}

			if isJSON() {
				return deps.WriteJSON(cmd.OutOrStdout(), Status{
					OK:                     allOK,
					BitbucketURL:           cfg.BitbucketURL,
					BitbucketVersionTarget: cfg.BitbucketVersionTarget,
					AuthMode:               cfg.AuthMode(),
					AuthSource:             cfg.AuthSource,
					// Reported here as well as at login, so an operator auditing
					// an existing machine can see how its credentials are held
					// without having to grep the config file.
					CredentialStorage: cfg.CredentialStorage(),
					Checks:            checksFrom(checks),
				})
			}

			// The expected version is only reported when the operator pinned
			// one; the project itself does not claim a supported version.
			details := fmt.Sprintf("auth=%s, source=%s", cfg.AuthMode(), cfg.AuthSource)
			if version := strings.TrimSpace(cfg.BitbucketVersionTarget); version != "" {
				details = fmt.Sprintf("expected version %s, %s", version, details)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Target Bitbucket: %s (%s)\n", cfg.BitbucketURL, details)

			// status is where an operator comes for detail, so it names the file
			// rather than repeating the generic warning loadConfig already put on
			// stderr for every command.
			fmt.Fprintf(cmd.OutOrStdout(), "Credential storage: %s\n", describeCredentialStorage(cfg))

			for _, check := range checks {
				// An advisory miss reads as a suggestion rather than breakage,
				// because for an API-only setup that is exactly what it is.
				var marker string
				switch {
				case check.OK:
					marker = "-"
				case check.Advisory:
					marker = "!"
				default:
					marker = "x"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s %s: %s\n", marker, check.Name, check.Detail)
				if check.Remedy != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "    %s\n", check.Remedy)
				}
			}

			if statusCheckExit && !allOK {
				return apperrors.New(apperrors.KindAuthentication, "one or more authentication checks failed", nil)
			}

			return nil
		},
	}
	statusCmd.Flags().StringVar(&statusHost, "host", "", "Override host for this status check")
	statusCmd.Flags().BoolVar(&statusCheckExit, "check", false, "Exit non-zero when a check fails (for CI)")
	authCmd.AddCommand(statusCmd)

	var loginToken string
	var loginTokenStdin bool
	var loginUsername string
	var loginPassword string
	var loginPasswordStdin bool
	var loginClientCert string
	var loginClientKey string
	var loginSetDefault bool
	var loginDiscoverAliases bool
	var loginRequireKeyring bool
	loginCmd := &cobra.Command{
		Use:   "login <host>",
		Short: "Store credentials for a Bitbucket host",
		Long: `Store credentials for a Bitbucket host.

Prefer the stdin forms. A secret passed as a flag value appears in the process
argument list, where any local user can read it through ps or /proc, and where
the shell records it in history:

  printf '%s' "$BITBUCKET_TOKEN" | bb auth login https://bitbucket.example.com --token-stdin

Credentials are stored in the OS keyring. Where no keyring is available — headless
servers, most containers, WSL without gnome-keyring — bb falls back to the config
file in plaintext and says so. Pass --require-keyring (or set BB_REQUIRE_KEYRING=1)
to fail instead of falling back.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolvedHost := strings.TrimSpace(args[0])

			if loginTokenStdin && loginPasswordStdin {
				return apperrors.New(apperrors.KindValidation, "--token-stdin and --password-stdin cannot both be used: stdin carries one secret", nil)
			}

			token, err := resolveLoginSecret(loginToken, loginTokenStdin, cmd.InOrStdin(), "--token", "--token-stdin")
			if err != nil {
				return err
			}
			password, err := resolveLoginSecret(loginPassword, loginPasswordStdin, cmd.InOrStdin(), "--password", "--password-stdin")
			if err != nil {
				return err
			}

			warnAboutSecretsOnTheCommandLine(cmd)

			// The subcommand's own flag wins, then the global --client-cert,
			// then the variable. The middle layer used to arrive as the
			// variable, because the global flag was written into it; reading
			// the environment alone stopped seeing it once flags became values.
			globals := deps.runtimeOverrides()
			clientCert := firstNonBlank(loginClientCert, derefString(globals.ClientCert), os.Getenv("BB_CLIENT_CERT"))
			clientKey := firstNonBlank(loginClientKey, derefString(globals.ClientKey), os.Getenv("BB_CLIENT_KEY"))

			aliases := []string(nil)
			if loginDiscoverAliases {
				probeCfg := config.AppConfig{
					BitbucketURL:      resolvedHost,
					BitbucketToken:    token,
					BitbucketUsername: strings.TrimSpace(loginUsername),
					BitbucketPassword: password,
					ClientCertFile:    clientCert,
					ClientKeyFile:     clientKey,
				}
				discoveredAliases, err := discoverAliases(cmd.Context(), probeCfg, deps.NewReposClient)
				if err == nil {
					aliases = discoveredAliases
				}
			}

			stored, err := config.SaveLogin(config.LoginInput{
				Host:           resolvedHost,
				Aliases:        aliases,
				Username:       loginUsername,
				Password:       password,
				Token:          token,
				ClientCert:     clientCert,
				ClientKey:      clientKey,
				SetDefault:     loginSetDefault,
				RequireKeyring: loginRequireKeyring,
			})
			if err != nil {
				return err
			}

			// The warning goes to stderr in both modes: under --json stdout is a
			// machine contract, and prose there would make the envelope
			// unparseable.
			if stored.UsedInsecureStorage {
				reportInsecureStorage(cmd.ErrOrStderr(), stored.Host)
			}

			if isJSON() {
				return deps.WriteJSON(cmd.OutOrStdout(), Login{
					Host:                stored.Host,
					Aliases:             listOrEmpty(stored.Aliases),
					AuthMode:            stored.AuthMode,
					UsedInsecureStorage: stored.UsedInsecureStorage,
				})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Stored credentials for %s (mode=%s)\n", stored.Host, stored.AuthMode)
			if len(stored.Aliases) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Discovered aliases: %s\n", strings.Join(stored.Aliases, ", "))
			}
			return nil
		},
	}
	loginCmd.Flags().StringVar(&loginToken, "token", "", "Access token (visible in the process list; prefer --token-stdin)")
	loginCmd.Flags().BoolVar(&loginTokenStdin, "token-stdin", false, "Read the access token from stdin")
	loginCmd.Flags().StringVar(&loginUsername, "username", "", "Username for basic auth")
	loginCmd.Flags().StringVar(&loginPassword, "password", "", "Password for basic auth (visible in the process list; prefer --password-stdin)")
	loginCmd.Flags().BoolVar(&loginPasswordStdin, "password-stdin", false, "Read the basic-auth password from stdin")
	loginCmd.Flags().StringVar(&loginClientCert, "client-cert", "", "Path to PEM client certificate for mTLS")
	loginCmd.Flags().StringVar(&loginClientKey, "client-key", "", "Path to PEM client key for mTLS")
	loginCmd.Flags().BoolVar(&loginSetDefault, "set-default", true, "Set host as default target")
	loginCmd.Flags().BoolVar(&loginDiscoverAliases, "discover-aliases", true, "Discover host aliases from the first accessible repository clone links")
	loginCmd.Flags().BoolVar(&loginRequireKeyring, "require-keyring", false, "Fail if the OS keyring is unavailable instead of storing credentials in plaintext")
	authCmd.AddCommand(loginCmd)

	var identityHost string
	identityCmd := &cobra.Command{
		Use:     "identity",
		Aliases: []string{"whoami"},
		Short:   "Show authenticated user identity",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := deps.loadWith(identityHost)
			if err != nil {
				return err
			}

			identity, err := resolveIdentity(cmd.Context(), cfg, deps.NewUsersClient)
			if err != nil {
				return err
			}

			if isJSON() {
				return deps.WriteJSON(cmd.OutOrStdout(), Identity{BitbucketURL: cfg.BitbucketURL, User: identity})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Authenticated user: %s\n", identityHumanSummary(identity))
			return nil
		},
	}
	identityCmd.Flags().StringVar(&identityHost, "host", "", "Override host for this identity check")
	authCmd.AddCommand(identityCmd)

	var tokenHost string
	tokenCmd := &cobra.Command{
		Use:   "token-url",
		Short: "Show personal access token creation URL",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load config once. If --host is provided, override the URL so the identity
			// lookup targets the same server as the PAT URL being generated.
			cfg, err := deps.LoadConfig()
			if err != nil {
				return err
			}

			resolvedHost := strings.TrimSpace(tokenHost)
			if resolvedHost == "" {
				resolvedHost = cfg.BitbucketURL
			} else {
				// Apply --host override so identity resolution targets the right server.
				cfg.BitbucketURL = resolvedHost
			}

			// Attempt to resolve the current user slug for a per-user PAT URL.
			// If credentials are not configured, fall back to the generic URL.
			var userSlug string
			if cfg.AuthMode() != "none" {
				if identity, err := resolveIdentity(cmd.Context(), cfg, deps.NewUsersClient); err == nil {
					userSlug = identity.Slug
				}
			}

			patURL, err := personalAccessTokenURL(resolvedHost, userSlug)
			if err != nil {
				return err
			}

			if isJSON() {
				return deps.WriteJSON(cmd.OutOrStdout(), TokenURL{BitbucketURL: resolvedHost, TokenURL: patURL})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Create a personal access token at:\n%s\n", patURL)
			return nil
		},
	}
	tokenCmd.Flags().StringVar(&tokenHost, "host", "", "Bitbucket host URL")
	authCmd.AddCommand(tokenCmd)

	var logoutHost string
	logoutCmd := &cobra.Command{
		Use:   "logout",
		Short: "Remove stored credentials for a Bitbucket host",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := config.Logout(logoutHost); err != nil {
				return err
			}

			if isJSON() {
				return deps.WriteJSON(cmd.OutOrStdout(), result.OK())
			}

			fmt.Fprintln(cmd.OutOrStdout(), "Stored credentials removed")
			return nil
		},
	}
	logoutCmd.Flags().StringVar(&logoutHost, "host", "", "Bitbucket host URL (defaults to stored default host)")
	authCmd.AddCommand(logoutCmd)

	serverCmd := &cobra.Command{
		Use:   "server",
		Short: "Manage server contexts",
	}

	serverListCmd := &cobra.Command{
		Use:   "list",
		Short: "List stored server contexts",
		RunE: func(cmd *cobra.Command, args []string) error {
			contexts, err := config.ListServerContexts()
			if err != nil {
				return err
			}

			if isJSON() {
				return deps.WriteJSON(cmd.OutOrStdout(), ServerContexts{Servers: serverContextsFrom(contexts)})
			}

			if len(contexts) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No stored server contexts")
				return nil
			}

			for _, context := range contexts {
				marker := " "
				if context.IsDefault {
					marker = "*"
				}

				if context.Username != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "%s %s (auth=%s, user=%s)\n", marker, context.Host, context.AuthMode, context.Username)
					continue
				}

				fmt.Fprintf(cmd.OutOrStdout(), "%s %s (auth=%s)\n", marker, context.Host, context.AuthMode)
			}

			return nil
		},
	}
	serverCmd.AddCommand(serverListCmd)

	var serverUseHost string
	serverUseCmd := &cobra.Command{
		Use:   "use [host]",
		Short: "Set the active default server context",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(serverUseHost) == "" && len(args) > 0 {
				serverUseHost = args[0]
			}

			resolvedHost, err := config.SetDefaultHost(serverUseHost)
			if err != nil {
				return err
			}

			if isJSON() {
				return deps.WriteJSON(cmd.OutOrStdout(), DefaultServer{Status: result.OK(), DefaultHost: resolvedHost})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Active server set to %s\n", resolvedHost)
			return nil
		},
	}
	serverUseCmd.Flags().StringVar(&serverUseHost, "host", "", "Bitbucket host URL")
	serverCmd.AddCommand(serverUseCmd)

	aliasCmd := &cobra.Command{
		Use:   "alias",
		Short: "Manage host aliases for a stored server context",
	}

	var aliasHost string
	aliasListCmd := &cobra.Command{
		Use:   "list",
		Short: "List aliases for a stored server context",
		RunE: func(cmd *cobra.Command, args []string) error {
			aliases, host, err := config.ListHostAliases(aliasHost)
			if err != nil {
				return err
			}

			if isJSON() {
				return deps.WriteJSON(cmd.OutOrStdout(), Aliases{Host: host, Aliases: listOrEmpty(aliases)})
			}

			if len(aliases) == 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "No aliases configured for %s\n", host)
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Aliases for %s:\n", host)
			for _, alias := range aliases {
				fmt.Fprintln(cmd.OutOrStdout(), alias)
			}
			return nil
		},
	}
	aliasListCmd.Flags().StringVar(&aliasHost, "host", "", "Bitbucket host URL")
	aliasCmd.AddCommand(aliasListCmd)

	var aliasAddHost string
	aliasAddCmd := &cobra.Command{
		Use:   "add <alias> [alias...]",
		Short: "Add aliases to a stored server context",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			aliases, host, err := config.AddHostAliases(aliasAddHost, args)
			if err != nil {
				return err
			}
			if isJSON() {
				return deps.WriteJSON(cmd.OutOrStdout(), Aliases{Host: host, Aliases: listOrEmpty(aliases)})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Aliases updated: %s\n", strings.Join(aliases, ", "))
			return nil
		},
	}
	aliasAddCmd.Flags().StringVar(&aliasAddHost, "host", "", "Bitbucket host URL")
	aliasCmd.AddCommand(aliasAddCmd)

	var aliasRemoveHost string
	aliasRemoveCmd := &cobra.Command{
		Use:   "remove <alias>",
		Short: "Remove an alias from a stored server context",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			aliases, host, err := config.RemoveHostAlias(aliasRemoveHost, args[0])
			if err != nil {
				return err
			}
			if isJSON() {
				return deps.WriteJSON(cmd.OutOrStdout(), Aliases{Host: host, Aliases: listOrEmpty(aliases)})
			}
			if len(aliases) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "Alias removed; no aliases remain")
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Remaining aliases: %s\n", strings.Join(aliases, ", "))
			return nil
		},
	}
	aliasRemoveCmd.Flags().StringVar(&aliasRemoveHost, "host", "", "Bitbucket host URL")
	aliasCmd.AddCommand(aliasRemoveCmd)

	var aliasDiscoverHost string
	var aliasDiscoverReplace bool
	aliasDiscoverCmd := &cobra.Command{
		Use:   "discover",
		Short: "Discover aliases from the first accessible repository clone links",
		Long: "Discover aliases from the first accessible repository clone links.\n\n" +
			"Discovered aliases are added to the ones already stored. Aliases added by hand are " +
			"kept, because discovery cannot find every alias -- an instance whose SSH clone host " +
			"differs from its web URL is the documented case for adding one manually, and it would " +
			"be undone by every later discovery run.\n\n" +
			"Pass --replace to store only what was discovered. Anything dropped is named in the " +
			"output.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := deps.loadWith(aliasDiscoverHost)
			if err != nil {
				return err
			}

			discovered, err := discoverAliases(cmd.Context(), cfg, deps.NewReposClient)
			if err != nil {
				return err
			}

			// Read what is stored before writing, so a removal can be named rather
			// than just happening.
			existing, _, err := config.ListHostAliases(cfg.BitbucketURL)
			if err != nil {
				return err
			}

			var aliases []string
			if aliasDiscoverReplace {
				aliases, err = config.SetHostAliases(cfg.BitbucketURL, discovered)
			} else {
				aliases, _, err = config.AddHostAliases(cfg.BitbucketURL, discovered)
			}
			if err != nil {
				return err
			}

			removed := aliasesMissingFrom(existing, aliases)

			if isJSON() {
				return deps.WriteJSON(cmd.OutOrStdout(), DiscoveredAliases{
					Host:       cfg.BitbucketURL,
					Aliases:    listOrEmpty(aliases),
					Discovered: listOrEmpty(discovered),
					Removed:    listOrEmpty(removed),
				})
			}
			if len(discovered) == 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "No aliases discovered for %s\n", cfg.BitbucketURL)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "Discovered aliases for %s: %s\n", cfg.BitbucketURL, strings.Join(discovered, ", "))
			}
			// Removals are the part worth saying out loud: they are configuration
			// the user put there, and losing them silently is what made this a bug.
			if len(removed) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Removed aliases: %s\n", strings.Join(removed, ", "))
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Stored aliases for %s: %s\n", cfg.BitbucketURL, strings.Join(aliases, ", "))
			return nil
		},
	}
	aliasDiscoverCmd.Flags().StringVar(&aliasDiscoverHost, "host", "", "Bitbucket host URL")
	aliasDiscoverCmd.Flags().BoolVar(&aliasDiscoverReplace, "replace", false, "Store only the discovered aliases, dropping any others")
	aliasCmd.AddCommand(aliasDiscoverCmd)

	authCmd.AddCommand(aliasCmd)

	authCmd.AddCommand(serverCmd)

	authCmd.AddCommand(newTokenCommand(deps))
	authCmd.AddCommand(newGpgKeyCommand(deps))
	authCmd.AddCommand(newGitCredentialCommand())
	authCmd.AddCommand(newSetupGitCommand(deps))

	return authCmd
}

func resolveIdentity(ctx context.Context, cfg config.AppConfig, newUsersClient func(config.AppConfig) (usersClient, error)) (result.User, error) {
	client, err := newUsersClient(cfg)
	if err != nil {
		return result.User{}, apperrors.New(apperrors.KindInternal, "failed to initialize API client", err)
	}

	response, err := client.GetUsers2WithResponse(ctx, nil)
	if err != nil {
		return result.User{}, apperrors.New(apperrors.KindTransient, "identity lookup failed", err)
	}

	if response.StatusCode() < 200 || response.StatusCode() >= 300 {
		return result.User{}, openapi.MapStatusError(response.StatusCode(), response.Body)
	}

	// Try to get authenticated username from the X-AUSERNAME header
	var username string
	if response.HTTPResponse != nil {
		username = strings.TrimSpace(response.HTTPResponse.Header.Get("X-AUSERNAME"))
	}

	if username != "" {
		userResponse, err := client.GetUserWithResponse(ctx, username)
		if err == nil && userResponse.StatusCode() == 200 && userResponse.ApplicationjsonCharsetUTF8200 != nil {
			user := userResponse.ApplicationjsonCharsetUTF8200
			return result.User{
				Name:         strings.TrimSpace(safeString(user.Name)),
				Slug:         strings.TrimSpace(safeString(user.Slug)),
				DisplayName:  strings.TrimSpace(safeString(user.DisplayName)),
				EmailAddress: strings.TrimSpace(safeString(user.EmailAddress)),
				ID:           safeInt32(user.Id),
				Type:         strings.TrimSpace(safeStringFromEnum(user.Type)),
				Active:       safeBool(user.Active),
			}, nil
		}
	}

	// Fallback to the old logic (parsing response directly as RestApplicationUser, which works for some mocks/servers)
	if response.ApplicationjsonCharsetUTF8200 != nil {
		user := response.ApplicationjsonCharsetUTF8200
		return result.User{
			Name:         strings.TrimSpace(safeString(user.Name)),
			Slug:         strings.TrimSpace(safeString(user.Slug)),
			DisplayName:  strings.TrimSpace(safeString(user.DisplayName)),
			EmailAddress: strings.TrimSpace(safeString(user.EmailAddress)),
			ID:           safeInt32(user.Id),
			Type:         strings.TrimSpace(safeStringFromEnum(user.Type)),
			Active:       safeBool(user.Active),
		}, nil
	}

	return result.User{}, apperrors.New(apperrors.KindAuthentication, "failed to resolve identity: authenticated username not found", nil)
}

func identityHumanSummary(identity result.User) string {
	parts := make([]string, 0, 6)
	if identity.DisplayName != "" {
		parts = append(parts, identity.DisplayName)
	} else if identity.Name != "" {
		parts = append(parts, identity.Name)
	} else if identity.Slug != "" {
		parts = append(parts, identity.Slug)
	} else {
		parts = append(parts, "unknown")
	}

	if identity.Name != "" {
		parts = append(parts, "name="+identity.Name)
	}
	if identity.Slug != "" {
		parts = append(parts, "slug="+identity.Slug)
	}
	if identity.EmailAddress != "" {
		parts = append(parts, "email="+identity.EmailAddress)
	}
	if identity.Type != "" {
		parts = append(parts, "type="+identity.Type)
	}
	if identity.ID > 0 {
		parts = append(parts, fmt.Sprintf("id=%d", identity.ID))
	}

	return strings.Join(parts, ", ")
}

func safeString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func safeInt32(value *int32) int32 {
	if value == nil {
		return 0
	}
	return *value
}

func safeBool(value *bool) bool {
	if value == nil {
		return false
	}
	return *value
}

func safeStringFromEnum(value *openapigenerated.RestApplicationUserType) string {
	if value == nil {
		return ""
	}
	return string(*value)
}

func discoverAliases(ctx context.Context, cfg config.AppConfig, newReposClient func(config.AppConfig) (repositoriesClient, error)) ([]string, error) {
	client, err := newReposClient(cfg)
	if err != nil {
		return nil, apperrors.New(apperrors.KindInternal, "failed to initialize repository discovery client", err)
	}

	limit := float32(5)
	permission := "REPO_READ"
	recent, err := client.GetRepositoriesRecentlyAccessedWithResponse(ctx, &openapigenerated.GetRepositoriesRecentlyAccessedParams{
		Limit:      &limit,
		Permission: &permission,
	})
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "alias discovery failed", err)
	}

	aliases, found, err := discoverAliasesFromRepositoryPage(recent.StatusCode(), recent.Body, recent.ApplicationjsonCharsetUTF8200)
	if err != nil {
		return nil, err
	}
	if found {
		return aliases, nil
	}

	all, err := client.GetRepositories1WithResponse(ctx, &openapigenerated.GetRepositories1Params{Limit: &limit})
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "alias discovery failed", err)
	}

	aliases, _, err = discoverAliasesFromRepositoryPage(all.StatusCode(), all.Body, all.ApplicationjsonCharsetUTF8200)
	if err != nil {
		return nil, err
	}

	return aliases, nil
}

func discoverAliasesFromRepositoryPage(statusCode int, body []byte, page *struct {
	IsLastPage    *bool                              `json:"isLastPage,omitempty"`
	Limit         *float32                           `json:"limit,omitempty"`
	NextPageStart *int32                             `json:"nextPageStart,omitempty"`
	Size          *float32                           `json:"size,omitempty"`
	Start         *int32                             `json:"start,omitempty"`
	Values        *[]openapigenerated.RestRepository `json:"values,omitempty"`
}) ([]string, bool, error) {
	if statusCode < 200 || statusCode >= 300 || page == nil {
		return nil, false, openapi.MapStatusError(statusCode, body)
	}
	if page.Values == nil {
		return nil, false, nil
	}

	for _, repository := range *page.Values {
		aliases := extractRepositoryCloneAliases(repository)
		if len(aliases) > 0 {
			return aliases, true, nil
		}
	}

	return nil, false, nil
}

func extractRepositoryCloneAliases(repository openapigenerated.RestRepository) []string {
	if repository.Links == nil {
		return nil
	}

	rawCloneLinks, ok := (*repository.Links)["clone"]
	if !ok {
		return nil
	}

	items, ok := rawCloneLinks.([]any)
	if !ok {
		return nil
	}

	aliases := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		href, _ := entry["href"].(string)
		name, _ := entry["name"].(string)
		if strings.TrimSpace(href) == "" {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(name), "ssh") && !strings.HasPrefix(strings.TrimSpace(href), "git@") {
			continue
		}
		alias, err := normalizeCloneEndpoint(href)
		if err != nil {
			continue
		}
		if _, exists := seen[alias]; exists {
			continue
		}
		seen[alias] = struct{}{}
		aliases = append(aliases, alias)
	}

	return aliases
}

func normalizeCloneEndpoint(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", apperrors.New(apperrors.KindValidation, "clone endpoint is required", nil)
	}

	if strings.HasPrefix(trimmed, "git@") {
		at := strings.LastIndex(trimmed, "@")
		colon := strings.Index(trimmed[at+1:], ":")
		if at >= 0 && colon >= 0 {
			return strings.ToLower(strings.TrimSpace(trimmed[at+1:at+1+colon]) + ":22"), nil
		}
	}

	parsed, err := url.Parse(trimmed)
	if err != nil || strings.TrimSpace(parsed.Hostname()) == "" {
		return "", apperrors.New(apperrors.KindValidation, fmt.Sprintf("clone endpoint %q is invalid", raw), err)
	}

	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	port := parsed.Port()
	if port == "" {
		switch strings.ToLower(parsed.Scheme) {
		case "http":
			port = "80"
		case "ssh":
			port = "22"
		default:
			port = "443"
		}
	}

	return host + ":" + port, nil
}

// personalAccessTokenURL returns the Bitbucket URL for managing personal access tokens.
// When userSlug is non-empty it returns the per-user URL (.../users/<slug>/manage);
// otherwise it returns the generic manage URL.
func personalAccessTokenURL(host string, userSlug string) (string, error) {
	trimmed := strings.TrimSpace(host)
	if trimmed == "" {
		return "", apperrors.New(apperrors.KindValidation, "bitbucket host is required (set --host or BITBUCKET_URL)", nil)
	}

	if !strings.Contains(trimmed, "://") {
		trimmed = "http://" + trimmed
	}

	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" {
		return "", apperrors.New(apperrors.KindValidation, "bitbucket host URL is invalid", err)
	}

	slug := strings.TrimSpace(userSlug)
	if slug != "" {
		parsed.Path = path.Join(parsed.Path, "/plugins/servlet/access-tokens/users/"+url.PathEscape(slug)+"/manage")
	} else {
		parsed.Path = path.Join(parsed.Path, "/plugins/servlet/access-tokens/manage")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""

	return parsed.String(), nil
}

// aliasesMissingFrom returns the entries of before that are absent from after.
//
// Discovery adds by default, so this is normally empty; it is what --replace
// dropped, and reporting it is the difference between a removal and a silent
// loss.
func aliasesMissingFrom(before []string, after []string) []string {
	remaining := make(map[string]struct{}, len(after))
	for _, alias := range after {
		remaining[alias] = struct{}{}
	}

	missing := []string{}
	for _, alias := range before {
		if _, present := remaining[alias]; !present {
			missing = append(missing, alias)
		}
	}

	return missing
}

// loadWith resolves configuration with a host override, without publishing it.
//
// The override used to be an os.Setenv of BITBUCKET_URL. That outlived the
// command -- harmless for a one-shot invocation, not for `bb ai mcp serve`
// -- and it overwrote the user's own BITBUCKET_URL rather than outranking it,
// which is the flag layer ADR-021 describes and the implementation did not have.
func (deps Dependencies) loadWith(host string) (config.AppConfig, error) {
	if strings.TrimSpace(host) == "" {
		return deps.LoadConfig()
	}
	if deps.LoadConfigWithOverrides == nil {
		// A caller that did not wire the override cannot honour one. Say so
		// rather than silently resolving the wrong instance.
		return config.AppConfig{}, apperrors.New(apperrors.KindInternal,
			"a host override was given but this command was built without support for one", nil)
	}
	return deps.LoadConfigWithOverrides(config.Overrides{Host: host})
}

// runtimeOverrides is the global flags, or none when the caller wired nothing.
func (deps Dependencies) runtimeOverrides() config.Overrides {
	if deps.RuntimeOverrides == nil {
		return config.Overrides{}
	}
	return deps.RuntimeOverrides()
}

// firstNonBlank returns the first value that is not empty after trimming.
func firstNonBlank(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// derefString reads through an override pointer, where nil means "not passed".
func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
