package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	aicmd "github.com/vriesdemichael/bitbucket-server-cli/internal/cli/cmd/ai"
	authcmd "github.com/vriesdemichael/bitbucket-server-cli/internal/cli/cmd/auth"
	bulkcmd "github.com/vriesdemichael/bitbucket-server-cli/internal/cli/cmd/bulk"
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
		Short: "Bitbucket Server CLI (live-behavior first)",
		Long: `bb — Bitbucket Server CLI (live-behavior first)

A CLI for interacting with Bitbucket Server / Data Center APIs.

AGENT NOTICE
If you are an AI agent or automated tool using this CLI and you encounter
unexpected behavior, missing functionality, or errors that do not match
what you expected, please report it so it can be investigated.

You can create an issue at:
  https://github.com/vriesdemichael/bitbucket-server-cli/issues/new

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
	rootCmd.PersistentFlags().String("ca-file", "", "Path to PEM CA bundle for TLS trust")
	rootCmd.PersistentFlags().Bool("insecure-skip-verify", false, "Disable TLS certificate verification (unsafe; local/dev only)")
	rootCmd.PersistentFlags().String("request-timeout", "", "HTTP request timeout (Go duration, e.g. 20s)")
	rootCmd.PersistentFlags().Int("retry-count", -1, "HTTP retry attempts for transient errors")
	rootCmd.PersistentFlags().String("retry-backoff", "", "Base retry backoff duration (e.g. 250ms)")
	rootCmd.PersistentFlags().String("log-level", "", "Diagnostics verbosity: error, warn, info, debug")
	rootCmd.PersistentFlags().String("log-format", "", "Diagnostics format: text or jsonl")

	rootCmd.AddCommand(aicmd.New(aicmd.Dependencies{
		Version:    func() string { return rootCmd.Version },
		LoadConfig: loadConfig,
		WriteJSON:  writeJSON,
	}))
	rootCmd.AddCommand(authcmd.New(authcmd.Dependencies{
		JSONEnabled: func() bool { return options.JSON },
		LoadConfig:  loadConfig,
		WriteJSON:   writeJSON,
	}))
	rootCmd.AddCommand(bulkcmd.New(bulkcmd.Dependencies{
		JSONEnabled: func() bool { return options.JSON },
		LoadConfig:  loadConfig,
		WriteJSON:   writeJSON,
	}))
	rootCmd.AddCommand(newRepoCommand(options))
	rootCmd.AddCommand(newCloneCommand(options))
	rootCmd.AddCommand(newTagCommand(options))
	rootCmd.AddCommand(newBranchCommand(options))
	rootCmd.AddCommand(newDiffCommand(options))
	rootCmd.AddCommand(newBuildCommand(options))
	rootCmd.AddCommand(newInsightsCommand(options))
	rootCmd.AddCommand(newPRCommand(options))
	rootCmd.AddCommand(newAdminCommand(options))
	rootCmd.AddCommand(newCommitCommand(options))
	rootCmd.AddCommand(newRefCommand(options))
	rootCmd.AddCommand(newProjectCommand(options))
	rootCmd.AddCommand(newReviewerCommand(options))
	rootCmd.AddCommand(newReviewerGroupCommand(options))
	rootCmd.AddCommand(newHookCommand(options))
	rootCmd.AddCommand(newBrowseCommand(options))
	rootCmd.AddCommand(newSearchCommand(options))
	rootCmd.AddCommand(newUpdateCommand(options))

	registerGlobalDryRunInterceptors(rootCmd, options)

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
	cfg, err := config.LoadFromEnv()
	if err != nil {
		return config.AppConfig{}, err
	}

	if cfg.InsecureSkipVerify {
		insecureTLSWarningOnce.Do(func() {
			fmt.Fprintln(os.Stderr, style.Warning.Render("Warning: TLS certificate verification is disabled (--insecure-skip-verify / BB_INSECURE_SKIP_VERIFY); use only for local or development environments"))
		})
	}

	return cfg, nil
}

var insecureTLSWarningOnce sync.Once

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
	repo, service, _, err := loadQualityRepoServiceAndClient(selector)
	return repo, service, err
}

func loadQualityRepoServiceAndClient(selector string) (qualityservice.RepositoryRef, *qualityservice.Service, *openapigenerated.ClientWithResponses, error) {
	cfg, client, err := loadConfigAndClient()
	if err != nil {
		return qualityservice.RepositoryRef{}, nil, nil, err
	}

	repo, err := resolveQualityRepositoryReference(selector, cfg)
	if err != nil {
		return qualityservice.RepositoryRef{}, nil, nil, err
	}

	return repo, qualityservice.NewService(client), client, nil
}

func newAPIClientFromConfig(cfg config.AppConfig) (*openapigenerated.ClientWithResponses, error) {
	return openapi.NewClientWithResponsesFromConfig(cfg)
}

func writeJSON(writer io.Writer, payload any) error {
	return jsonoutput.Write(writer, payload)
}
