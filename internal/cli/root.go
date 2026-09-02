package cli

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	admincmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/admin"
	aicmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/ai"
	apicmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/api"
	authcmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/auth"
	branchcmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/branch"
	browsecmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/browse"
	buildcmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/build"
	bulkcmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/bulk"
	commitcmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/commit"
	deploymentcmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/deployment"
	diffcmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/diff"
	insightscmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/insights"
	prcmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/pr"
	projectcmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/project"
	refcmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/ref"
	repocmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/repo"
	reviewercmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/reviewer"
	reviewergroupcmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/reviewergroup"
	searchcmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/search"
	sshkeycmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/sshkey"
	tagcmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/tag"
	updatecmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/update"
	webhookcmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/webhook"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/style"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/diagnostics"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	qualityservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/quality"
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

			// --describe answers from schemas compiled into this binary, so it
			// must not need configuration, a server, or a git checkout. Running
			// the rest of this would make asking what a command returns fail in
			// exactly the situations where the answer is most wanted.
			if options.Describe {
				style.Init(options.NoColor)
				return nil
			}

			if err := options.applyRuntimeFlagOverrides(cmd); err != nil {
				return err
			}
			style.Init(options.NoColor)
			return options.applyInferredRepositoryContext(cmd, options.JSON)
		},
	}

	rootCmd.PersistentFlags().BoolVar(&options.JSON, "json", false, "Output as JSON")
	rootCmd.PersistentFlags().BoolVar(&options.DryRun, "dry-run", false, "Preview server mutations without applying them")
	rootCmd.PersistentFlags().BoolVar(&options.NoColor, "no-color", false, "Disable colored output")
	rootCmd.PersistentFlags().Bool("no-input", false, "Never prompt; fail instead when a value is missing")
	rootCmd.PersistentFlags().BoolVar(&options.Describe, describeFlag, false, "Print the command's output schema instead of running it")
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
		Version:     func() string { return rootCmd.Version },
		JSONEnabled: func() bool { return options.JSON },
		LoadConfig:  options.loadConfigWithOverrides,
		WriteJSON:   writeJSON,
	}))
	rootCmd.AddCommand(apicmd.New(apicmd.Dependencies{
		JSONEnabled:   func() bool { return options.JSON },
		DryRunEnabled: func() bool { return options.DryRun },
		LoadConfig:    options.loadConfigWithOverrides,
		WriteJSON:     writeJSON,
	}))
	rootCmd.AddCommand(authcmd.New(authcmd.Dependencies{
		JSONEnabled:             func() bool { return options.JSON },
		LoadConfig:              options.loadConfig,
		LoadConfigWithOverrides: options.loadConfigWithOverrides,
		RuntimeOverrides:        func() config.Overrides { return options.runtime },
		WriteJSON:               writeJSON,
	}))
	rootCmd.AddCommand(bulkcmd.New(bulkcmd.Dependencies{
		JSONEnabled: func() bool { return options.JSON },
		LoadConfig:  options.loadConfig,
		WriteJSON:   writeJSON,
	}))
	rootCmd.AddCommand(repocmd.New(repocmd.Dependencies{
		JSONEnabled:         func() bool { return options.JSON },
		DryRunEnabled:       func() bool { return options.DryRun },
		LoadConfig:          options.loadConfig,
		LoadConfigAndClient: options.loadConfigAndClient,
		WriteJSON:           writeJSON,
		WriteJSONList:       writeJSONList,
		PermissionChecker: func(client *openapigenerated.ClientWithResponses) repocmd.PermissionChecker {
			return options.permissionCheckerFor(client)
		},
		RepositoryWasInferred: func() bool { return options.repositoryInferred },
	}))
	rootCmd.AddCommand(repocmd.NewClone(repocmd.Dependencies{
		JSONEnabled:           func() bool { return options.JSON },
		DryRunEnabled:         func() bool { return options.DryRun },
		LoadConfig:            options.loadConfig,
		LoadConfigAndClient:   options.loadConfigAndClient,
		WriteJSON:             writeJSON,
		WriteJSONList:         writeJSONList,
		RepositoryWasInferred: func() bool { return options.repositoryInferred },
	}))
	rootCmd.AddCommand(tagcmd.New(tagcmd.Dependencies{
		JSONEnabled:         func() bool { return options.JSON },
		DryRunEnabled:       func() bool { return options.DryRun },
		LoadConfig:          options.loadConfig,
		LoadConfigAndClient: options.loadConfigAndClient,
		WriteJSON:           writeJSON,
		WriteJSONList:       writeJSONList,
		PermissionChecker: func(client *openapigenerated.ClientWithResponses) tagcmd.PermissionChecker {
			return options.permissionCheckerFor(client)
		},
	}))
	rootCmd.AddCommand(branchcmd.New(branchcmd.Dependencies{
		JSONEnabled:         func() bool { return options.JSON },
		DryRunEnabled:       func() bool { return options.DryRun },
		LoadConfig:          options.loadConfig,
		LoadConfigAndClient: options.loadConfigAndClient,
		WriteJSON:           writeJSON,
		WriteJSONList:       writeJSONList,
		PermissionChecker: func(client *openapigenerated.ClientWithResponses) branchcmd.PermissionChecker {
			return options.permissionCheckerFor(client)
		},
	}))
	rootCmd.AddCommand(diffcmd.New(diffcmd.Dependencies{
		JSONEnabled:         func() bool { return options.JSON },
		LoadConfig:          options.loadConfig,
		LoadConfigAndClient: options.loadConfigAndClient,
		WriteJSON:           writeJSON,
	}))
	rootCmd.AddCommand(buildcmd.New(buildcmd.Dependencies{
		JSONEnabled:         func() bool { return options.JSON },
		DryRunEnabled:       func() bool { return options.DryRun },
		LoadConfig:          options.loadConfig,
		LoadConfigAndClient: options.loadConfigAndClient,
		WriteJSON:           writeJSON,
		WriteJSONList:       writeJSONList,
		PermissionChecker: func(client *openapigenerated.ClientWithResponses) buildcmd.PermissionChecker {
			return options.permissionCheckerFor(client)
		},
	}))
	rootCmd.AddCommand(deploymentcmd.New(deploymentcmd.Dependencies{
		JSONEnabled:         func() bool { return options.JSON },
		DryRunEnabled:       func() bool { return options.DryRun },
		LoadConfig:          options.loadConfig,
		LoadConfigAndClient: options.loadConfigAndClient,
		WriteJSON:           writeJSON,
		PermissionChecker: func(client *openapigenerated.ClientWithResponses) deploymentcmd.PermissionChecker {
			return options.permissionCheckerFor(client)
		},
	}))
	rootCmd.AddCommand(insightscmd.New(insightscmd.Dependencies{
		JSONEnabled:         func() bool { return options.JSON },
		DryRunEnabled:       func() bool { return options.DryRun },
		LoadConfig:          options.loadConfig,
		LoadConfigAndClient: options.loadConfigAndClient,
		WriteJSON:           writeJSON,
		WriteJSONList:       writeJSONList,
		PermissionChecker: func(client *openapigenerated.ClientWithResponses) insightscmd.PermissionChecker {
			return options.permissionCheckerFor(client)
		},
	}))
	rootCmd.AddCommand(prcmd.New(prcmd.Dependencies{
		JSONEnabled:         func() bool { return options.JSON },
		DryRunEnabled:       func() bool { return options.DryRun },
		LoadConfig:          options.loadConfig,
		LoadConfigAndClient: options.loadConfigAndClient,
		WriteJSON:           writeJSON,
		WriteJSONList:       writeJSONList,
		GitBackend:          gitBackendFactory,
		PermissionChecker: func(client *openapigenerated.ClientWithResponses) prcmd.PermissionChecker {
			return options.permissionCheckerFor(client)
		},
	}))
	rootCmd.AddCommand(admincmd.New(admincmd.Dependencies{
		JSONEnabled: func() bool { return options.JSON },
		LoadConfig:  options.loadConfig,
		WriteJSON:   writeJSON,
	}))
	rootCmd.AddCommand(commitcmd.New(commitcmd.Dependencies{
		JSONEnabled:         func() bool { return options.JSON },
		LoadConfig:          options.loadConfig,
		LoadConfigAndClient: options.loadConfigAndClient,
		WriteJSON:           writeJSON,
		WriteJSONList:       writeJSONList,
	}))
	rootCmd.AddCommand(refcmd.New(refcmd.Dependencies{
		JSONEnabled:         func() bool { return options.JSON },
		LoadConfig:          options.loadConfig,
		LoadConfigAndClient: options.loadConfigAndClient,
		WriteJSON:           writeJSON,
	}))
	rootCmd.AddCommand(projectcmd.New(projectcmd.Dependencies{
		JSONEnabled:         func() bool { return options.JSON },
		DryRunEnabled:       func() bool { return options.DryRun },
		LoadConfig:          options.loadConfig,
		LoadConfigAndClient: options.loadConfigAndClient,
		WriteJSON:           writeJSON,
		WriteJSONList:       writeJSONList,
		PermissionChecker: func(client *openapigenerated.ClientWithResponses) projectcmd.PermissionChecker {
			return options.permissionCheckerFor(client)
		},
	}))
	rootCmd.AddCommand(reviewercmd.New(reviewercmd.Dependencies{
		JSONEnabled:         func() bool { return options.JSON },
		DryRunEnabled:       func() bool { return options.DryRun },
		LoadConfig:          options.loadConfig,
		LoadConfigAndClient: options.loadConfigAndClient,
		WriteJSON:           writeJSON,
		PermissionChecker: func(client *openapigenerated.ClientWithResponses) reviewercmd.PermissionChecker {
			return options.permissionCheckerFor(client)
		},
	}))
	rootCmd.AddCommand(reviewergroupcmd.New(reviewergroupcmd.Dependencies{
		JSONEnabled:         func() bool { return options.JSON },
		DryRunEnabled:       func() bool { return options.DryRun },
		LoadConfig:          options.loadConfig,
		LoadConfigAndClient: options.loadConfigAndClient,
		WriteJSON:           writeJSON,
		PermissionChecker: func(client *openapigenerated.ClientWithResponses) reviewergroupcmd.PermissionChecker {
			return options.permissionCheckerFor(client)
		},
	}))
	rootCmd.AddCommand(webhookcmd.New(webhookcmd.Dependencies{
		JSONEnabled:         func() bool { return options.JSON },
		DryRunEnabled:       func() bool { return options.DryRun },
		LoadConfig:          options.loadConfig,
		LoadConfigAndClient: options.loadConfigAndClient,
		WriteJSON:           writeJSON,
		PermissionChecker: func(client *openapigenerated.ClientWithResponses) webhookcmd.PermissionChecker {
			return options.permissionCheckerFor(client)
		},
	}))
	rootCmd.AddCommand(browsecmd.New(browsecmd.Dependencies{
		JSONEnabled: func() bool { return options.JSON },
		LoadConfig:  options.loadConfig,
		WriteJSON:   writeJSON,
	}))
	rootCmd.AddCommand(searchcmd.New(searchcmd.Dependencies{
		JSONEnabled:         func() bool { return options.JSON },
		LoadConfig:          options.loadConfig,
		LoadConfigAndClient: options.loadConfigAndClient,
		WriteJSON:           writeJSON,
		WriteJSONList:       writeJSONList,
	}))
	rootCmd.AddCommand(updatecmd.New(updatecmd.Dependencies{
		JSONEnabled:      func() bool { return options.JSON },
		DryRunEnabled:    func() bool { return options.DryRun },
		WriteJSON:        writeJSON,
		RuntimeOverrides: func() config.Overrides { return options.runtime },
	}))
	rootCmd.AddCommand(sshkeycmd.New(sshkeycmd.Dependencies{
		JSONEnabled:         func() bool { return options.JSON },
		LoadConfig:          options.loadConfig,
		LoadConfigAndClient: options.loadConfigAndClient,
		WriteJSON:           writeJSON,
		WriteJSONList:       writeJSONList,
	}))

	registerGlobalDryRunInterceptors(rootCmd, options)
	enforceNoArgsDefaults(rootCmd)

	// Installed last, over the finished tree, because it wraps every runnable
	// command it finds. Anything added after this point would not answer
	// --describe.
	installDescribe(rootCmd, &options.Describe)

	return rootCmd
}

type rootOptions struct {
	JSON    bool
	DryRun  bool
	NoColor bool
	// Describe makes a command print its own output contract instead of running
	// it. A pointer to this is handed to installDescribe, so the wrappers see the
	// parsed value rather than the value at construction time.
	Describe bool
	// runtime carries the values the global flags supplied, resolved once in
	// PersistentPreRunE. It lives here rather than in the environment so a flag
	// outranks BB_* for this invocation instead of destroying it, and so the
	// value does not outlive the command -- which matters for bb ai mcp serve
	// (issue #458).
	runtime config.Overrides
	// repositoryInferred reports that --repo was filled in from the git remote
	// rather than named by the caller. A destructive command needs the
	// difference; see applyInferredRepositoryContext.
	repositoryInferred bool
	permissionChecker  *PermissionChecker
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

func (options *rootOptions) loadConfig() (config.AppConfig, error) {
	return options.loadConfigWithOverrides(config.Overrides{})
}

// loadConfigWithOverrides is loadConfig for the commands that take a --host or
// --token flag, which have to steer the resolution rather than inherit it.
func (options *rootOptions) loadConfigWithOverrides(overrides config.Overrides) (config.AppConfig, error) {
	cfg, err := config.LoadWithOverrides(options.merge(overrides))
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

func (options *rootOptions) applyRuntimeFlagOverrides(cmd *cobra.Command) error {
	if cmd == nil {
		return nil
	}

	lookupFlag := func(flagName string) *pflag.Flag {
		if flag := cmd.Flags().Lookup(flagName); flag != nil {
			return flag
		}
		return cmd.PersistentFlags().Lookup(flagName)
	}

	// changedString returns the flag's value, or nil when it was not passed.
	//
	// The distinction is the whole point: nil leaves the environment its own
	// precedence slot, where writing the flag into BB_* destroyed whatever the
	// user had set. A flag passed empty is still a decision and is carried as
	// an empty string rather than as nil.
	changedString := func(flagName string) *string {
		flag := lookupFlag(flagName)
		if flag == nil || !flag.Changed {
			return nil
		}
		value := strings.TrimSpace(flag.Value.String())
		return &value
	}

	options.runtime.CAFile = changedString("ca-file")
	options.runtime.ClientCert = changedString("client-cert")
	options.runtime.ClientKey = changedString("client-key")
	options.runtime.RequestTimeout = changedString("request-timeout")
	options.runtime.RetryBackoff = changedString("retry-backoff")

	if raw := changedString("insecure-skip-verify"); raw != nil {
		value := strings.EqualFold(*raw, "true")
		options.runtime.InsecureSkipVerify = &value
	}
	if raw := changedString("retry-count"); raw != nil {
		// Cobra parsed this as an Int flag, so Value.String() is always a valid
		// integer and the error branch is unreachable. A parse failure here would
		// mean the flag type changed, which the config layer would then reject.
		if value, err := strconv.Atoi(*raw); err == nil {
			options.runtime.RetryCount = &value
		}
	}

	// Diagnostics is still read from the environment: it is consumed by
	// package-level state in internal/diagnostics rather than through
	// config.AppConfig, so it has no override to carry. Left for its own change.
	for _, diagnostic := range []struct{ flagName, envKey string }{
		{"log-level", "BB_LOG_LEVEL"},
		{"log-format", "BB_LOG_FORMAT"},
	} {
		value := changedString(diagnostic.flagName)
		if value == nil {
			continue
		}
		// Empty is a decision, not an absence -- the same rule the overrides
		// above follow. Unsetting is how it is expressed here, because this
		// still travels through the environment.
		if *value == "" {
			_ = os.Unsetenv(diagnostic.envKey)
			continue
		}
		_ = os.Setenv(diagnostic.envKey, *value)
	}

	return nil
}

func (options *rootOptions) loadConfigAndClient() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
	cfg, err := options.loadConfig()
	if err != nil {
		return config.AppConfig{}, nil, err
	}

	client, err := newAPIClientFromConfig(cfg)
	if err != nil {
		return config.AppConfig{}, nil, apperrors.New(apperrors.KindInternal, "failed to initialize API client", err)
	}

	return cfg, client, nil
}

func (options *rootOptions) loadQualityRepoAndService(selector string) (qualityservice.RepositoryRef, *qualityservice.Service, error) {
	cfg, client, err := options.loadConfigAndClient()
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

// merge layers a command's own overrides on top of the invocation's flags.
//
// A command that takes --host or --token steers resolution for itself; the
// global flags apply to every command. Both are per-invocation values now, so
// the two compose here rather than racing to write the same environment
// variable.
func (options *rootOptions) merge(command config.Overrides) config.Overrides {
	if options == nil {
		return command
	}

	// Start from the command's own overrides so every field it set survives,
	// including ones added to config.Overrides later. Copying a chosen few was
	// how the previous version silently dropped anything it had not been taught
	// about, with no signal to the caller.
	merged := command

	if merged.Host == "" {
		merged.Host = options.runtime.Host
	}
	if merged.Token == "" {
		merged.Token = options.runtime.Token
	}
	if merged.ProjectKey == "" {
		merged.ProjectKey = options.runtime.ProjectKey
	}
	if merged.RepoSlug == "" {
		merged.RepoSlug = options.runtime.RepoSlug
	}

	// The runtime settings are pointers, so nil is "the command said nothing"
	// and the global flag applies.
	if merged.CAFile == nil {
		merged.CAFile = options.runtime.CAFile
	}
	if merged.InsecureSkipVerify == nil {
		merged.InsecureSkipVerify = options.runtime.InsecureSkipVerify
	}
	if merged.ClientCert == nil {
		merged.ClientCert = options.runtime.ClientCert
	}
	if merged.ClientKey == nil {
		merged.ClientKey = options.runtime.ClientKey
	}
	if merged.RequestTimeout == nil {
		merged.RequestTimeout = options.runtime.RequestTimeout
	}
	if merged.RetryCount == nil {
		merged.RetryCount = options.runtime.RetryCount
	}
	if merged.RetryBackoff == nil {
		merged.RetryBackoff = options.runtime.RetryBackoff
	}

	return merged
}
