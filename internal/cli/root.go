package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	admincmd "github.com/vriesdemichael/bitbucket-server-cli/internal/cli/cmd/admin"
	aicmd "github.com/vriesdemichael/bitbucket-server-cli/internal/cli/cmd/ai"
	apicmd "github.com/vriesdemichael/bitbucket-server-cli/internal/cli/cmd/api"
	authcmd "github.com/vriesdemichael/bitbucket-server-cli/internal/cli/cmd/auth"
	branchcmd "github.com/vriesdemichael/bitbucket-server-cli/internal/cli/cmd/branch"
	browsecmd "github.com/vriesdemichael/bitbucket-server-cli/internal/cli/cmd/browse"
	buildcmd "github.com/vriesdemichael/bitbucket-server-cli/internal/cli/cmd/build"
	bulkcmd "github.com/vriesdemichael/bitbucket-server-cli/internal/cli/cmd/bulk"
	commitcmd "github.com/vriesdemichael/bitbucket-server-cli/internal/cli/cmd/commit"
	deploymentcmd "github.com/vriesdemichael/bitbucket-server-cli/internal/cli/cmd/deployment"
	diffcmd "github.com/vriesdemichael/bitbucket-server-cli/internal/cli/cmd/diff"
	insightscmd "github.com/vriesdemichael/bitbucket-server-cli/internal/cli/cmd/insights"
	prcmd "github.com/vriesdemichael/bitbucket-server-cli/internal/cli/cmd/pr"
	projectcmd "github.com/vriesdemichael/bitbucket-server-cli/internal/cli/cmd/project"
	refcmd "github.com/vriesdemichael/bitbucket-server-cli/internal/cli/cmd/ref"
	repocmd "github.com/vriesdemichael/bitbucket-server-cli/internal/cli/cmd/repo"
	reviewercmd "github.com/vriesdemichael/bitbucket-server-cli/internal/cli/cmd/reviewer"
	reviewergroupcmd "github.com/vriesdemichael/bitbucket-server-cli/internal/cli/cmd/reviewergroup"
	searchcmd "github.com/vriesdemichael/bitbucket-server-cli/internal/cli/cmd/search"
	sshkeycmd "github.com/vriesdemichael/bitbucket-server-cli/internal/cli/cmd/sshkey"
	tagcmd "github.com/vriesdemichael/bitbucket-server-cli/internal/cli/cmd/tag"
	updatecmd "github.com/vriesdemichael/bitbucket-server-cli/internal/cli/cmd/update"
	webhookcmd "github.com/vriesdemichael/bitbucket-server-cli/internal/cli/cmd/webhook"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/style"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/diagnostics"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
	qualityservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/quality"
)

func NewRootCommand() *cobra.Command {
	options := &rootOptions{}

	rootCmd := &cobra.Command{
		Use:   "bb",
		Short: "Bitbucket Data Center CLI (live-behavior first)",
		Long: `bb — Bitbucket Data Center CLI (live-behavior first)

A CLI for interacting with recent versions of Bitbucket for Data Center APIs.

AGENT NOTICE
If you are an AI agent or automated tool using this CLI and you encounter
unexpected behavior, missing functionality, or errors that do not match
what you expected, please report it so it can be investigated.

You can create an issue at:
  https://github.com/vriesdemichael/bitbucket-data-center-cli/issues/new

If you cannot open the URL directly, ask the user to file the issue on
your behalf using the link above.`,
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			diagnostics.SetOutputWriter(cmd.ErrOrStderr())
			if err := applyRuntimeFlagOverrides(cmd); err != nil {
				return err
			}
			style.Init(options.NoColor)
			return applyInferredRepositoryContext(cmd, options.JSON)
		},
	}

	rootCmd.PersistentFlags().BoolVar(&options.JSON, "json", false, "Output as JSON")
	rootCmd.PersistentFlags().BoolVar(&options.DryRun, "dry-run", false, "Preview server mutations without applying them")
	rootCmd.PersistentFlags().BoolVar(&options.NoColor, "no-color", false, "Disable colored output")
	rootCmd.PersistentFlags().Bool("no-input", false, "Never prompt; fail instead when a value is missing")
	rootCmd.PersistentFlags().String("ca-file", "", "Path to PEM CA bundle for TLS trust")
	rootCmd.PersistentFlags().Bool("insecure-skip-verify", false, "Disable TLS certificate verification (unsafe; local/dev only)")
	rootCmd.PersistentFlags().String("client-cert", "", "Path to PEM client certificate for mTLS")
	rootCmd.PersistentFlags().String("client-key", "", "Path to PEM client key for mTLS")
	rootCmd.PersistentFlags().String("request-timeout", "", "HTTP request timeout (Go duration, e.g. 20s)")
	rootCmd.PersistentFlags().Int("retry-count", -1, "HTTP retry attempts for transient errors")
	rootCmd.PersistentFlags().String("retry-backoff", "", "Base retry backoff duration (e.g. 250ms)")
	rootCmd.PersistentFlags().String("log-level", "", "Diagnostics verbosity: error, warn, info, debug")
	rootCmd.PersistentFlags().String("log-format", "", "Diagnostics format: text or jsonl")

	rootCmd.AddCommand(aicmd.New(aicmd.Dependencies{
		Version:    func() string { return rootCmd.Version },
		LoadConfig: loadConfigWithOverrides,
		WriteJSON:  writeJSON,
	}))
	rootCmd.AddCommand(apicmd.New(apicmd.Dependencies{
		JSONEnabled:   func() bool { return options.JSON },
		DryRunEnabled: func() bool { return options.DryRun },
		LoadConfig:    loadConfigWithOverrides,
		WriteJSON:     writeJSON,
	}))
	rootCmd.AddCommand(authcmd.New(authcmd.Dependencies{
		JSONEnabled:             func() bool { return options.JSON },
		LoadConfig:              loadConfig,
		LoadConfigWithOverrides: loadConfigWithOverrides,
		WriteJSON:               writeJSON,
	}))
	rootCmd.AddCommand(bulkcmd.New(bulkcmd.Dependencies{
		JSONEnabled: func() bool { return options.JSON },
		LoadConfig:  loadConfig,
		WriteJSON:   writeJSON,
	}))
	rootCmd.AddCommand(repocmd.New(repocmd.Dependencies{
		JSONEnabled:         func() bool { return options.JSON },
		DryRunEnabled:       func() bool { return options.DryRun },
		LoadConfig:          loadConfig,
		LoadConfigAndClient: loadConfigAndClient,
		WriteJSON:           writeJSON,
		WriteJSONList:       writeJSONList,
		PermissionChecker: func(client *openapigenerated.ClientWithResponses) repocmd.PermissionChecker {
			return options.permissionCheckerFor(client)
		},
	}))
	rootCmd.AddCommand(repocmd.NewClone(repocmd.Dependencies{
		JSONEnabled:         func() bool { return options.JSON },
		DryRunEnabled:       func() bool { return options.DryRun },
		LoadConfig:          loadConfig,
		LoadConfigAndClient: loadConfigAndClient,
		WriteJSON:           writeJSON,
		WriteJSONList:       writeJSONList,
	}))
	rootCmd.AddCommand(tagcmd.New(tagcmd.Dependencies{
		JSONEnabled:         func() bool { return options.JSON },
		DryRunEnabled:       func() bool { return options.DryRun },
		LoadConfig:          loadConfig,
		LoadConfigAndClient: loadConfigAndClient,
		WriteJSON:           writeJSON,
		WriteJSONList:       writeJSONList,
		PermissionChecker: func(client *openapigenerated.ClientWithResponses) tagcmd.PermissionChecker {
			return options.permissionCheckerFor(client)
		},
	}))
	rootCmd.AddCommand(branchcmd.New(branchcmd.Dependencies{
		JSONEnabled:         func() bool { return options.JSON },
		DryRunEnabled:       func() bool { return options.DryRun },
		LoadConfig:          loadConfig,
		LoadConfigAndClient: loadConfigAndClient,
		WriteJSON:           writeJSON,
		WriteJSONList:       writeJSONList,
		PermissionChecker: func(client *openapigenerated.ClientWithResponses) branchcmd.PermissionChecker {
			return options.permissionCheckerFor(client)
		},
	}))
	rootCmd.AddCommand(diffcmd.New(diffcmd.Dependencies{
		JSONEnabled:         func() bool { return options.JSON },
		LoadConfig:          loadConfig,
		LoadConfigAndClient: loadConfigAndClient,
		WriteJSON:           writeJSON,
	}))
	rootCmd.AddCommand(buildcmd.New(buildcmd.Dependencies{
		JSONEnabled:         func() bool { return options.JSON },
		DryRunEnabled:       func() bool { return options.DryRun },
		LoadConfig:          loadConfig,
		LoadConfigAndClient: loadConfigAndClient,
		WriteJSON:           writeJSON,
		WriteJSONList:       writeJSONList,
		PermissionChecker: func(client *openapigenerated.ClientWithResponses) buildcmd.PermissionChecker {
			return options.permissionCheckerFor(client)
		},
	}))
	rootCmd.AddCommand(deploymentcmd.New(deploymentcmd.Dependencies{
		JSONEnabled:         func() bool { return options.JSON },
		DryRunEnabled:       func() bool { return options.DryRun },
		LoadConfig:          loadConfig,
		LoadConfigAndClient: loadConfigAndClient,
		WriteJSON:           writeJSON,
		PermissionChecker: func(client *openapigenerated.ClientWithResponses) deploymentcmd.PermissionChecker {
			return options.permissionCheckerFor(client)
		},
	}))
	rootCmd.AddCommand(insightscmd.New(insightscmd.Dependencies{
		JSONEnabled:         func() bool { return options.JSON },
		DryRunEnabled:       func() bool { return options.DryRun },
		LoadConfig:          loadConfig,
		LoadConfigAndClient: loadConfigAndClient,
		WriteJSON:           writeJSON,
		WriteJSONList:       writeJSONList,
		PermissionChecker: func(client *openapigenerated.ClientWithResponses) insightscmd.PermissionChecker {
			return options.permissionCheckerFor(client)
		},
	}))
	rootCmd.AddCommand(prcmd.New(prcmd.Dependencies{
		JSONEnabled:         func() bool { return options.JSON },
		DryRunEnabled:       func() bool { return options.DryRun },
		LoadConfig:          loadConfig,
		LoadConfigAndClient: loadConfigAndClient,
		WriteJSON:           writeJSON,
		WriteJSONList:       writeJSONList,
		GitBackend:          gitBackendFactory,
		PermissionChecker: func(client *openapigenerated.ClientWithResponses) prcmd.PermissionChecker {
			return options.permissionCheckerFor(client)
		},
	}))
	rootCmd.AddCommand(admincmd.New(admincmd.Dependencies{
		JSONEnabled: func() bool { return options.JSON },
		LoadConfig:  loadConfig,
		WriteJSON:   writeJSON,
	}))
	rootCmd.AddCommand(commitcmd.New(commitcmd.Dependencies{
		JSONEnabled:         func() bool { return options.JSON },
		LoadConfig:          loadConfig,
		LoadConfigAndClient: loadConfigAndClient,
		WriteJSON:           writeJSON,
		WriteJSONList:       writeJSONList,
	}))
	rootCmd.AddCommand(refcmd.New(refcmd.Dependencies{
		JSONEnabled:         func() bool { return options.JSON },
		LoadConfig:          loadConfig,
		LoadConfigAndClient: loadConfigAndClient,
		WriteJSON:           writeJSON,
	}))
	rootCmd.AddCommand(projectcmd.New(projectcmd.Dependencies{
		JSONEnabled:         func() bool { return options.JSON },
		DryRunEnabled:       func() bool { return options.DryRun },
		LoadConfig:          loadConfig,
		LoadConfigAndClient: loadConfigAndClient,
		WriteJSON:           writeJSON,
		WriteJSONList:       writeJSONList,
		PermissionChecker: func(client *openapigenerated.ClientWithResponses) projectcmd.PermissionChecker {
			return options.permissionCheckerFor(client)
		},
	}))
	rootCmd.AddCommand(reviewercmd.New(reviewercmd.Dependencies{
		JSONEnabled:         func() bool { return options.JSON },
		DryRunEnabled:       func() bool { return options.DryRun },
		LoadConfig:          loadConfig,
		LoadConfigAndClient: loadConfigAndClient,
		WriteJSON:           writeJSON,
		PermissionChecker: func(client *openapigenerated.ClientWithResponses) reviewercmd.PermissionChecker {
			return options.permissionCheckerFor(client)
		},
	}))
	rootCmd.AddCommand(reviewergroupcmd.New(reviewergroupcmd.Dependencies{
		JSONEnabled:         func() bool { return options.JSON },
		DryRunEnabled:       func() bool { return options.DryRun },
		LoadConfig:          loadConfig,
		LoadConfigAndClient: loadConfigAndClient,
		WriteJSON:           writeJSON,
		PermissionChecker: func(client *openapigenerated.ClientWithResponses) reviewergroupcmd.PermissionChecker {
			return options.permissionCheckerFor(client)
		},
	}))
	rootCmd.AddCommand(webhookcmd.New(webhookcmd.Dependencies{
		JSONEnabled:         func() bool { return options.JSON },
		DryRunEnabled:       func() bool { return options.DryRun },
		LoadConfig:          loadConfig,
		LoadConfigAndClient: loadConfigAndClient,
		WriteJSON:           writeJSON,
		PermissionChecker: func(client *openapigenerated.ClientWithResponses) webhookcmd.PermissionChecker {
			return options.permissionCheckerFor(client)
		},
	}))
	rootCmd.AddCommand(browsecmd.New(browsecmd.Dependencies{
		JSONEnabled: func() bool { return options.JSON },
		LoadConfig:  loadConfig,
		WriteJSON:   writeJSON,
	}))
	rootCmd.AddCommand(searchcmd.New(searchcmd.Dependencies{
		JSONEnabled:         func() bool { return options.JSON },
		LoadConfig:          loadConfig,
		LoadConfigAndClient: loadConfigAndClient,
		WriteJSON:           writeJSON,
		WriteJSONList:       writeJSONList,
	}))
	rootCmd.AddCommand(updatecmd.New(updatecmd.Dependencies{
		JSONEnabled:   func() bool { return options.JSON },
		DryRunEnabled: func() bool { return options.DryRun },
		WriteJSON:     writeJSON,
	}))
	rootCmd.AddCommand(sshkeycmd.New(sshkeycmd.Dependencies{
		JSONEnabled:         func() bool { return options.JSON },
		LoadConfig:          loadConfig,
		LoadConfigAndClient: loadConfigAndClient,
		WriteJSON:           writeJSON,
		WriteJSONList:       writeJSONList,
	}))

	registerGlobalDryRunInterceptors(rootCmd, options)
	enforceNoArgsDefaults(rootCmd)

	return rootCmd
}

type rootOptions struct {
	JSON              bool
	DryRun            bool
	NoColor           bool
	permissionChecker *PermissionChecker
}

func (options *rootOptions) permissionCheckerFor(client *openapigenerated.ClientWithResponses) *PermissionChecker {
	if options == nil || client == nil {
		return nil
	}
	if options.permissionChecker == nil {
		options.permissionChecker = NewPermissionChecker(client)
	}
	return options.permissionChecker
}

func loadConfig() (config.AppConfig, error) {
	return loadConfigWithOverrides(config.Overrides{})
}

// loadConfigWithOverrides is loadConfig for the commands that take a --host or
// --token flag, which have to steer the resolution rather than inherit it.
func loadConfigWithOverrides(overrides config.Overrides) (config.AppConfig, error) {
	cfg, err := config.LoadWithOverrides(overrides)
	if err != nil {
		return config.AppConfig{}, err
	}

	if cfg.InsecureSkipVerify {
		insecureTLSWarningOnce.Do(func() {
			fmt.Fprintln(os.Stderr, style.Warning.Render("Warning: TLS certificate verification is disabled (--insecure-skip-verify / BB_INSECURE_SKIP_VERIFY); use only for local or development environments"))
		})
	}

	// Warned on use, not only at login. A warning that fires once when the
	// credential is stored is invisible to whoever inherits the machine, and
	// plaintext storage is a standing condition rather than a one-off event.
	if cfg.UsedInsecureStorage {
		insecureStorageWarningOnce.Do(func() {
			fmt.Fprintln(os.Stderr, style.Warning.Render("Warning: credentials for this host are stored in plaintext because no OS keyring was available; run 'bb auth status' for details, or set BB_REQUIRE_KEYRING=1 to refuse plaintext storage"))
		})
	}

	return cfg, nil
}

var insecureTLSWarningOnce sync.Once

var insecureStorageWarningOnce sync.Once

func applyRuntimeFlagOverrides(cmd *cobra.Command) error {
	if cmd == nil {
		return nil
	}

	lookupFlag := func(flagName string) *pflag.Flag {
		if flag := cmd.Flags().Lookup(flagName); flag != nil {
			return flag
		}
		return cmd.PersistentFlags().Lookup(flagName)
	}

	setIfChanged := func(flagName, envKey string) {
		flag := lookupFlag(flagName)
		if flag == nil || !flag.Changed {
			return
		}

		value := strings.TrimSpace(flag.Value.String())

		if value == "" {
			_ = os.Unsetenv(envKey)
			return
		}

		_ = os.Setenv(envKey, value)
	}

	overrides := []struct {
		flagName string
		envKey   string
	}{
		{flagName: "ca-file", envKey: "BB_CA_FILE"},
		{flagName: "insecure-skip-verify", envKey: "BB_INSECURE_SKIP_VERIFY"},
		{flagName: "client-cert", envKey: "BB_CLIENT_CERT"},
		{flagName: "client-key", envKey: "BB_CLIENT_KEY"},
		{flagName: "request-timeout", envKey: "BB_REQUEST_TIMEOUT"},
		{flagName: "retry-count", envKey: "BB_RETRY_COUNT"},
		{flagName: "retry-backoff", envKey: "BB_RETRY_BACKOFF"},
		{flagName: "log-level", envKey: "BB_LOG_LEVEL"},
		{flagName: "log-format", envKey: "BB_LOG_FORMAT"},
	}

	for _, override := range overrides {
		setIfChanged(override.flagName, override.envKey)
	}

	return nil
}

func loadConfigAndClient() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
	cfg, err := loadConfig()
	if err != nil {
		return config.AppConfig{}, nil, err
	}

	client, err := newAPIClientFromConfig(cfg)
	if err != nil {
		return config.AppConfig{}, nil, apperrors.New(apperrors.KindInternal, "failed to initialize API client", err)
	}

	return cfg, client, nil
}

func loadQualityRepoAndService(selector string) (qualityservice.RepositoryRef, *qualityservice.Service, error) {
	cfg, client, err := loadConfigAndClient()
	if err != nil {
		return qualityservice.RepositoryRef{}, nil, err
	}

	repo, err := resolveQualityRepositoryReference(selector, cfg)
	if err != nil {
		return qualityservice.RepositoryRef{}, nil, err
	}

	return repo, qualityservice.NewService(client), nil
}

func newAPIClientFromConfig(cfg config.AppConfig) (*openapigenerated.ClientWithResponses, error) {
	return openapi.NewClientWithResponsesFromConfig(cfg)
}

func writeJSON(writer io.Writer, payload any) error {
	return jsonoutput.Write(writer, payload)
}

// writeJSONList is writeJSON for a bounded result set, recording in the
// envelope meta whether the result came back at --limit.
func writeJSONList(writer io.Writer, payload any, limitReached bool) error {
	return jsonoutput.WriteList(writer, payload, limitReached)
}

func enforceNoArgsDefaults(root *cobra.Command) {
	var visit func(*cobra.Command)
	visit = func(cmd *cobra.Command) {
		if cmd.Runnable() && cmd.Args == nil {
			if !hasPositionalPlaceholder(cmd.Use) {
				cmd.Args = cobra.NoArgs
			}
		}
		for _, child := range cmd.Commands() {
			visit(child)
		}
	}
	visit(root)
}

func hasPositionalPlaceholder(use string) bool {
	parts := strings.Fields(use)
	if len(parts) <= 1 {
		return false
	}
	for _, p := range parts[1:] {
		if strings.HasPrefix(p, "<") || strings.HasPrefix(p, "[") {
			return true
		}
	}
	return false
}
