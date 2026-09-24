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
	commitcmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/commit"
	deploymentcmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/deployment"
	diffcmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/diff"
	doctorcmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/doctor"
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
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/completionsetup"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/diagnostics"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	qualityservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/quality"
)

// Run hooks traverse the tree, root first.
//
// Cobra runs only the nearest PersistentPreRun by default, so a subcommand
// group with a hook of its own silently replaces the root's, and none of what
// the root sets up before a command -- --full-error-body among it -- reaches
// the group's commands (#574). A deprecated group warning its users from a hook
// of its own is the case that found it. Set once, at package load: it is
// process-wide, and assigning it per invocation would race in the parallel
// command tests.
func init() {
	cobra.EnableTraverseRunHooks = true
}

func NewRootCommand() *cobra.Command {
	return NewRootCommandWithOverrides(config.Overrides{})
}

// NewRootCommandWithOverrides builds the root command with values already
// supplied, ahead of the environment and the stored configuration.
//
// The global flags write into the same place during PersistentPreRunE, so a
// flag still outranks what is passed here for the settings a flag can name.
// What this reaches that a flag cannot is the credential: bb takes a password
// from stdin or the environment and never as a flag value (ADR-047), so a
// caller holding one has nowhere to put it except the process -- which is
// process-wide, and therefore something two concurrent callers take from each
// other. The live suite runs as sixteen different restricted users at once and
// needs each command told who it is rather than the process being told.
func NewRootCommandWithOverrides(supplied config.Overrides) *cobra.Command {
	options := &rootOptions{runtime: supplied}

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

			// The two flags ask for the same document in two encodings, so one
			// of them has to be dropped rather than one silently winning.
			if options.JSON && options.YAML {
				return apperrors.New(apperrors.KindValidation,
					"--json and --yaml print the same document in two encodings; pass one of them", nil)
			}

			// Every document the command writes goes through its output writer,
			// so what the flags decided about it travels with it: the format
			// (ADR-095), and the member and the command (ADR-096).
			root := cmd.Root()
			settings := options.outputSettings(cmd)

			// A command that does not take --dry-run is not dry-run: passing it
			// is an invalid invocation, reported as itself rather than as a
			// verdict on a run that would never happen.
			if options.DryRun && !options.Describe && classifyCommand(dryRunCommandPath(cmd)) == classificationWithoutDryRun {
				settings.Mode = jsonoutput.ModeRun
				root.SetOut(jsonoutput.Bind(root.OutOrStdout(), settings))
				return dryRunUnsupportedError(dryRunCommandPath(cmd))
			}

			root.SetOut(jsonoutput.Bind(root.OutOrStdout(), settings))

			// --describe answers from schemas compiled into this binary, so it
			// must not need configuration, a server, or a git checkout. Running
			// the rest of this would make asking what a command returns fail in
			// exactly the situations where the answer is most wanted.
			// Before the --describe bail, so it applies to every run.
			openapi.SetFullUpstreamBodies(options.FullErrorBody)

			if options.Describe {
				style.Init(options.NoColor)
				return nil
			}

			if err := options.applyRuntimeFlagOverrides(cmd); err != nil {
				return err
			}
			style.Init(options.NoColor)
			return options.applyInferredRepositoryContext(cmd, options.machineOutput())
		},
	}

	// Cobra consults the nearest ancestor's FlagErrorFunc, so this one covers
	// every subcommand. It classifies a flag error where it is raised rather
	// than at the top of main: enumflag's rejections are pflag errors, and
	// anything driving the command tree without going through main -- the
	// tests, for one -- was seeing them as kind=internal and exit 1.
	rootCmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return ClassifyUsageError(err)
	})

	rootCmd.PersistentFlags().BoolVar(&options.JSON, "json", false, "Print the output as one JSON document")
	rootCmd.PersistentFlags().BoolVar(&options.YAML, "yaml", false, "Print the output as one YAML document: the same document as --json")
	rootCmd.PersistentFlags().BoolVar(&options.DryRun, "dry-run", false, "Say what the real run would do, without doing it; each command's help says what it checks")
	rootCmd.PersistentFlags().BoolVar(&options.NoColor, "no-color", false, "Disable colored output")
	rootCmd.PersistentFlags().BoolVar(&options.FullErrorBody, "full-error-body", false,
		"Print the whole upstream response body in an error instead of a summary")
	rootCmd.PersistentFlags().Bool("no-input", false, "Never prompt; fail instead when a value is missing")
	rootCmd.PersistentFlags().BoolVar(&options.Describe, describeFlag, false, "Print the JSON Schema of the command's --json data payload instead of running it")
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
		JSONEnabled: options.machineOutput,
		LoadConfig:  options.loadConfigWithOverrides,
		WriteJSON:   writeJSON,
	}))
	rootCmd.AddCommand(apicmd.New(apicmd.Dependencies{
		JSONEnabled:   options.machineOutput,
		DryRunEnabled: func() bool { return options.DryRun },
		LoadConfig:    options.loadConfigWithOverrides,
		WriteJSON:     writeJSON,
	}))
	rootCmd.AddCommand(authcmd.New(authcmd.Dependencies{
		JSONEnabled:             options.machineOutput,
		LoadConfig:              options.loadConfig,
		LoadConfigWithOverrides: options.loadConfigWithOverrides,
		RuntimeOverrides:        func() config.Overrides { return options.runtime },
		WriteJSON:               writeJSON,
		WriteJSONList:           writeJSONList,
	}))
	rootCmd.AddCommand(repocmd.New(repocmd.Dependencies{
		JSONEnabled:         options.machineOutput,
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
		JSONEnabled:           options.machineOutput,
		DryRunEnabled:         func() bool { return options.DryRun },
		LoadConfig:            options.loadConfig,
		LoadConfigAndClient:   options.loadConfigAndClient,
		WriteJSON:             writeJSON,
		WriteJSONList:         writeJSONList,
		RepositoryWasInferred: func() bool { return options.repositoryInferred },
	}))
	rootCmd.AddCommand(tagcmd.New(tagcmd.Dependencies{
		JSONEnabled:         options.machineOutput,
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
		JSONEnabled:         options.machineOutput,
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
		JSONEnabled:         options.machineOutput,
		LoadConfig:          options.loadConfig,
		LoadConfigAndClient: options.loadConfigAndClient,
		WriteJSON:           writeJSON,
	}))
	rootCmd.AddCommand(buildcmd.New(buildcmd.Dependencies{
		JSONEnabled:         options.machineOutput,
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
		JSONEnabled:         options.machineOutput,
		DryRunEnabled:       func() bool { return options.DryRun },
		LoadConfig:          options.loadConfig,
		LoadConfigAndClient: options.loadConfigAndClient,
		WriteJSON:           writeJSON,
		PermissionChecker: func(client *openapigenerated.ClientWithResponses) deploymentcmd.PermissionChecker {
			return options.permissionCheckerFor(client)
		},
	}))
	rootCmd.AddCommand(insightscmd.New(insightscmd.Dependencies{
		JSONEnabled:         options.machineOutput,
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
		JSONEnabled:         options.machineOutput,
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
		JSONEnabled: options.machineOutput,
		LoadConfig:  options.loadConfig,
		WriteJSON:   writeJSON,
	}))
	rootCmd.AddCommand(commitcmd.New(commitcmd.Dependencies{
		JSONEnabled:         options.machineOutput,
		LoadConfig:          options.loadConfig,
		LoadConfigAndClient: options.loadConfigAndClient,
		WriteJSON:           writeJSON,
		WriteJSONList:       writeJSONList,
	}))
	rootCmd.AddCommand(refcmd.New(refcmd.Dependencies{
		JSONEnabled:         options.machineOutput,
		LoadConfig:          options.loadConfig,
		LoadConfigAndClient: options.loadConfigAndClient,
		WriteJSON:           writeJSON,
	}))
	rootCmd.AddCommand(projectcmd.New(projectcmd.Dependencies{
		JSONEnabled:         options.machineOutput,
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
		JSONEnabled:         options.machineOutput,
		DryRunEnabled:       func() bool { return options.DryRun },
		LoadConfig:          options.loadConfig,
		LoadConfigAndClient: options.loadConfigAndClient,
		WriteJSON:           writeJSON,
		PermissionChecker: func(client *openapigenerated.ClientWithResponses) reviewercmd.PermissionChecker {
			return options.permissionCheckerFor(client)
		},
	}))
	rootCmd.AddCommand(reviewergroupcmd.New(reviewergroupcmd.Dependencies{
		JSONEnabled:         options.machineOutput,
		DryRunEnabled:       func() bool { return options.DryRun },
		LoadConfig:          options.loadConfig,
		LoadConfigAndClient: options.loadConfigAndClient,
		WriteJSON:           writeJSON,
		PermissionChecker: func(client *openapigenerated.ClientWithResponses) reviewergroupcmd.PermissionChecker {
			return options.permissionCheckerFor(client)
		},
	}))
	rootCmd.AddCommand(webhookcmd.New(webhookcmd.Dependencies{
		JSONEnabled:         options.machineOutput,
		DryRunEnabled:       func() bool { return options.DryRun },
		LoadConfig:          options.loadConfig,
		LoadConfigAndClient: options.loadConfigAndClient,
		WriteJSON:           writeJSON,
		WriteJSONList:       writeJSONList,
		PermissionChecker: func(client *openapigenerated.ClientWithResponses) webhookcmd.PermissionChecker {
			return options.permissionCheckerFor(client)
		},
	}))
	rootCmd.AddCommand(browsecmd.New(browsecmd.Dependencies{
		JSONEnabled: options.machineOutput,
		LoadConfig:  options.loadConfig,
		WriteJSON:   writeJSON,
	}))
	rootCmd.AddCommand(searchcmd.New(searchcmd.Dependencies{
		JSONEnabled:         options.machineOutput,
		LoadConfig:          options.loadConfig,
		LoadConfigAndClient: options.loadConfigAndClient,
		WriteJSON:           writeJSON,
		WriteJSONList:       writeJSONList,
	}))
	rootCmd.AddCommand(updatecmd.New(updatecmd.Dependencies{
		JSONEnabled:      options.machineOutput,
		DryRunEnabled:    func() bool { return options.DryRun },
		WriteJSON:        writeJSON,
		RuntimeOverrides: func() config.Overrides { return options.runtime },
	}))
	rootCmd.AddCommand(doctorcmd.New(doctorcmd.Dependencies{
		JSONEnabled:      options.machineOutput,
		WriteJSON:        writeJSON,
		RuntimeOverrides: func() config.Overrides { return options.runtime },
		Version:          func() string { return rootCmd.Version },
		CompletionScript: func(shell completionsetup.Shell, withDescriptions bool) (string, error) {
			return completionScript(rootCmd, string(shell), withDescriptions)
		},
	}))
	rootCmd.AddCommand(sshkeycmd.New(sshkeycmd.Dependencies{
		JSONEnabled:         options.machineOutput,
		LoadConfig:          options.loadConfig,
		LoadConfigAndClient: options.loadConfigAndClient,
		WriteJSON:           writeJSON,
		WriteJSONList:       writeJSONList,
	}))

	// Before the walks, so the two commands it adds under Cobra's completion
	// command get what every command that writes a file gets.
	addCompletionSetup(rootCmd, options)

	registerGlobalDryRunInterceptors(rootCmd, options)
	installDryRunHelp(rootCmd)

	// The same walk, for the same reason: a destructive command written
	// tomorrow inherits its --yes and its question by being named delete,
	// rather than by its author remembering ADR-073.
	registerDestructiveConfirmations(rootCmd, options)
	enforceNoArgsDefaults(rootCmd)

	// After the defaults, because it wraps whatever validator a command ended
	// up with -- including the NoArgs just installed above.
	nameTheMissingArgument(rootCmd)
	sendFailingGroupHelpToStderr(rootCmd)

	// Over the finished tree, and for the same reason as the walks above: what
	// a command accepts is readable from the command, so reading it beats
	// asking every author to remember a registration call.
	installCompletions(rootCmd, options)
	fixPowerShellCompletion(rootCmd)

	// Installed last, over the finished tree, because it wraps every runnable
	// command it finds. Anything added after this point would not answer
	// --describe.
	installDescribe(rootCmd, &options.Describe)

	return rootCmd
}

type rootOptions struct {
	JSON bool
	// YAML asks for the document --json prints, encoded as YAML (ADR-095).
	YAML    bool
	DryRun  bool
	NoColor bool
	// FullErrorBody turns off the summary of an upstream response body.
	//
	// One bad project key produced 18,414 characters of HTML in a single
	// error.message (#574), so the default is a summary. This is the way out
	// for somebody debugging a server that answers with something bb cannot
	// read.
	FullErrorBody bool
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

// machineOutput reports whether the invocation asked for the machine document,
// in either encoding. Everything that differs between text and machine output
// asks this; only the encoder asks which format.
func (options *rootOptions) machineOutput() bool {
	return options.JSON || options.YAML
}

// OutputSettingsFromFlags reads what the parsed flags decided about output, for
// a path that runs before PersistentPreRunE has bound it: a group's help
// function, or main reporting a failure. command is the command that ran, or
// nil when none resolved.
func OutputSettingsFromFlags(root, command *cobra.Command) jsonoutput.Settings {
	flagSet := func(name string) bool {
		value, _ := root.PersistentFlags().GetBool(name)
		return value
	}

	options := rootOptions{
		JSON:     flagSet("json"),
		YAML:     flagSet("yaml"),
		DryRun:   flagSet("dry-run"),
		Describe: flagSet(describeFlag),
	}

	return options.outputSettings(command)
}

// outputSettings is what the flags decided about output for command.
func (options *rootOptions) outputSettings(command *cobra.Command) jsonoutput.Settings {
	settings := jsonoutput.Settings{Machine: options.machineOutput(), Format: jsonoutput.FormatJSON}
	if options.YAML && !options.JSON {
		settings.Format = jsonoutput.FormatYAML
	}

	// --describe asks what a command returns whatever else is passed, so it
	// is not a dry run even beside --dry-run.
	if options.DryRun && !options.Describe {
		settings.Mode = jsonoutput.ModeDryRun
	}

	if command != nil && command.Runnable() {
		settings.Command = commandPathWithoutRoot(command)
	}

	return settings
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

	options.runtime = runtimeOverridesFromFlags(cmd, options.runtime)
	applyDiagnosticsFlags(cmd)

	return nil
}

// runtimeOverridesFromFlags reads the global runtime flags off an invocation.
//
// Separated from the hook that applies them because shell completion reads the
// same flags off the line it is completing: `bb --ca-file ./corp.pem pr merge
// <tab>` has to reach the instance that bundle is for, and Cobra does not run
// the hook for a completion request. Two readings of the same flags would
// drift, and the drift would be invisible -- completion silently offering
// nothing because it called a host the command would not have called.
func runtimeOverridesFromFlags(cmd *cobra.Command, base config.Overrides) config.Overrides {
	if cmd == nil {
		return base
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

	base.CAFile = changedString("ca-file")
	base.ClientCert = changedString("client-cert")
	base.ClientKey = changedString("client-key")
	base.RequestTimeout = changedString("request-timeout")
	base.RetryBackoff = changedString("retry-backoff")

	if raw := changedString("insecure-skip-verify"); raw != nil {
		value := strings.EqualFold(*raw, "true")
		base.InsecureSkipVerify = &value
	}
	if raw := changedString("retry-count"); raw != nil {
		// Cobra parsed this as an Int flag, so Value.String() is always a valid
		// integer and the error branch is unreachable. A parse failure here would
		// mean the flag type changed, which the config layer would then reject.
		if value, err := strconv.Atoi(*raw); err == nil {
			base.RetryCount = &value
		}
	}

	return base
}

// applyDiagnosticsFlags publishes --log-level and --log-format.
//
// Diagnostics is still read from the environment: it is consumed by
// package-level state in internal/diagnostics rather than through
// config.AppConfig, so it has no override to carry. Left for its own change.
//
// It is not part of runtimeOverridesFromFlags because this writes to the
// process, and a tab press has no business changing the environment of the
// shell that spawned it.
func applyDiagnosticsFlags(cmd *cobra.Command) {
	for _, diagnostic := range []struct{ flagName, envKey string }{
		{"log-level", "BB_LOG_LEVEL"},
		{"log-format", "BB_LOG_FORMAT"},
	} {
		flag := cmd.Flags().Lookup(diagnostic.flagName)
		if flag == nil {
			flag = cmd.PersistentFlags().Lookup(diagnostic.flagName)
		}
		if flag == nil || !flag.Changed {
			continue
		}

		// Empty is a decision, not an absence -- the same rule the overrides
		// follow. Unsetting is how it is expressed here, because this still
		// travels through the environment.
		value := strings.TrimSpace(flag.Value.String())
		if value == "" {
			_ = os.Unsetenv(diagnostic.envKey)

			continue
		}

		_ = os.Setenv(diagnostic.envKey, value)
	}
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
	if merged.Username == "" {
		merged.Username = options.runtime.Username
	}
	if merged.Password == "" {
		merged.Password = options.runtime.Password
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

// sendFailingGroupHelpToStderr keeps stdout clean for the invocation that is
// about to be reported as a failure.
//
// A group handed an unknown subcommand is an error (see UnknownSubcommandError),
// but Cobra has already answered it by printing the group's help before anything
// can intervene. On stdout that is the defect the exit code alone does not fix:
// under --json a caller reading stdout gets two kilobytes of prose, and now the
// failure envelope after it.
//
// The help itself is worth keeping -- it lists the subcommands that do exist,
// which is what the reader needs -- so it moves to stderr rather than being
// suppressed, and stdout carries only the envelope.
func sendFailingGroupHelpToStderr(root *cobra.Command) {
	defaultHelp := root.HelpFunc()
	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		unconsumed := cmd.Flags().Args()

		// A caller who asked for help is not the failing invocation: `bb pr
		// help` and `bb pr bogus --help` are answered, not refused, so their
		// help goes where help goes.
		if !cmd.Runnable() && cmd.HasSubCommands() && len(unconsumed) > 0 && !helpWasAskedFor(cmd, unconsumed) {
			restore := cmd.OutOrStdout()
			cmd.SetOut(cmd.ErrOrStderr())
			defer cmd.SetOut(restore)
		}

		defaultHelp(cmd, args)
	})
}
