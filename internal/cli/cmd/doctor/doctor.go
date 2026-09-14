// Package doctorcmd implements `bb doctor`, which checks the configuration bb
// would load without needing a host, a network, or a configuration that loads.
package doctorcmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
)

// Dependencies are what the command needs from the root.
type Dependencies struct {
	JSONEnabled      func() bool
	WriteJSON        func(io.Writer, any) error
	RuntimeOverrides func() config.Overrides
	// Diagnose is config.Diagnose outside a test.
	Diagnose func(config.DiagnoseInput) config.Diagnosis
}

func (deps Dependencies) withDefaults() Dependencies {
	if deps.JSONEnabled == nil {
		deps.JSONEnabled = func() bool { return false }
	}
	if deps.WriteJSON == nil {
		deps.WriteJSON = jsonoutput.Write
	}
	if deps.RuntimeOverrides == nil {
		deps.RuntimeOverrides = func() config.Overrides { return config.Overrides{} }
	}
	if deps.Diagnose == nil {
		deps.Diagnose = config.Diagnose
	}

	return deps
}

// New builds `bb doctor`.
func New(deps Dependencies) *cobra.Command {
	d := deps.withDefaults()

	return &cobra.Command{
		Use:   "doctor",
		Short: "Check the configuration bb would load",
		Long: `Check the configuration bb would load, and report everything wrong with it at once.

A command that loads the configuration stops at the first file it cannot use.
bb doctor reads the stored, workspace and system configuration files on their
own and reports, for each, where it is, whether it parses, and every key the
configuration schema rejects, with its line. It lists keys that are valid but
not read from the file they are in: policy set in your own file mandates
nothing.

It then shows where each effective setting comes from -- a flag, an environment
variable, a .env file, a configuration file, the Windows registry or the
built-in default -- and what it overrides. When keyring-backed storage is
required, it checks that the OS keyring can be reached.

It needs no configured host, never contacts Bitbucket, and never prints a
secret: a token or password is reported as configured, with where it is held.

Exit status is 0 only when there is nothing to fix. Any issue the report shows
-- an invalid file, a key its file never reads, a setting a command would
refuse, a required keyring that cannot be reached -- exits 1. Under --json a
run with issues writes the failure envelope instead of the report: its message
summarises the issues, and error.details names each one under its own key,
file/<file>, violation/<file>/<key path>, ignored/<file>/<key>, setting/<name>
or keyring, with the key path written as a JSON Pointer.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			diagnosis := d.Diagnose(config.DiagnoseInput{
				Overrides:    d.RuntimeOverrides(),
				ChangedFlags: changedFlags(cmd, "log-level", "log-format"),
			})
			issues := issuesIn(diagnosis)
			failure := failureFor(diagnosis, issues)

			// One document on stdout (ADR-075): the report when there is
			// nothing to fix, and otherwise only the failure envelope, which
			// cmd/bb writes from the returned error.
			if d.JSONEnabled() {
				if failure != nil {
					return failure
				}
				return d.WriteJSON(cmd.OutOrStdout(), reportFrom(diagnosis))
			}

			writeReport(cmd.OutOrStdout(), diagnosis, len(issues))

			return failure
		},
	}
}

func changedFlags(cmd *cobra.Command, names ...string) map[string]bool {
	changed := map[string]bool{}
	for _, name := range names {
		if flag := cmd.Flags().Lookup(name); flag != nil && flag.Changed {
			changed[name] = true
		}
	}

	return changed
}

func writeReport(w io.Writer, diagnosis config.Diagnosis, issues int) {
	fmt.Fprintln(w, "Configuration files")
	invalid := false
	for _, file := range diagnosis.Files {
		writeFile(w, file)
		invalid = invalid || !file.Valid()
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Settings")
	if invalid {
		fmt.Fprintln(w, "  Resolved from the files that parse. bb runs no command until every file is valid.")
	}
	width := 0
	for _, setting := range diagnosis.Settings {
		width = max(width, len(setting.Name))
	}
	for _, setting := range diagnosis.Settings {
		writeSetting(w, setting, width)
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Keyring")
	switch keyring := diagnosis.Keyring; {
	case !keyring.Required:
		fmt.Fprintln(w, "  not required, so not checked")
	case keyring.Reachable:
		fmt.Fprintf(w, "  required by %s; reachable\n", describeSource(keyring.RequiredBy))
	default:
		fmt.Fprintf(w, "  required by %s; %s\n", describeSource(keyring.RequiredBy), keyring.Problem)
	}

	fmt.Fprintln(w)
	if issues == 0 {
		fmt.Fprintln(w, "No issues found.")
	} else {
		fmt.Fprintf(w, "%d %s to fix.\n", issues, plural(issues, "issue", "issues"))
	}
}

const fileIndent = "             "

func writeFile(w io.Writer, file config.DiagnosedFile) {
	location := file.Path
	switch {
	case location == "" && file.Tier == config.TierWorkspace:
		location = "none found above the working directory"
	case location == "":
		location = "no location"
	case file.PathFrom != "default" && file.PathFrom != "machine" && file.PathFrom != "search":
		location += " (" + file.PathFrom + ")"
	}
	fmt.Fprintf(w, "  %-10s %s\n", file.Tier, location)

	switch {
	case !file.Read:
		fmt.Fprintf(w, "%snot read: %s\n", fileIndent, file.NotRead)
	case file.Problem != "":
		fmt.Fprintf(w, "%sinvalid: %s\n", fileIndent, file.Problem)
	case !file.Exists:
		if file.Path != "" {
			fmt.Fprintf(w, "%snot present\n", fileIndent)
		}
	case len(file.Violations) > 0:
		fmt.Fprintf(w, "%sinvalid: the schema rejects %d %s\n", fileIndent, len(file.Violations), plural(len(file.Violations), "key", "keys"))
	default:
		fmt.Fprintf(w, "%svalid\n", fileIndent)
	}

	for _, violation := range file.Violations {
		key := violation.Key
		if key == "" {
			key = "(the file)"
		}
		if violation.Line > 0 {
			fmt.Fprintf(w, "%sline %d: %s: %s\n", fileIndent, violation.Line, key, violation.Problem)
		} else {
			fmt.Fprintf(w, "%s%s: %s\n", fileIndent, key, violation.Problem)
		}
	}
	for _, ignored := range file.Ignored {
		fmt.Fprintf(w, "%signored: %s is read only from the %s configuration\n", fileIndent, ignored.Key, strings.Join(ignored.ReadFrom, " or "))
	}
	for _, secret := range file.Secrets {
		held := []string{}
		if secret.Token {
			held = append(held, "a token")
		}
		if secret.Password {
			held = append(held, "a password")
		}
		fmt.Fprintf(w, "%splaintext: %s for %s\n", fileIndent, strings.Join(held, " and "), secret.Host)
	}
}

func writeSetting(w io.Writer, setting config.DiagnosedSetting, width int) {
	indent := strings.Repeat(" ", width+4)

	switch {
	case !setting.Configured && setting.Value == "":
		fmt.Fprintf(w, "  %-*s  not set\n", width, setting.Name)
	case !setting.Configured:
		fmt.Fprintf(w, "  %-*s  %s (default)\n", width, setting.Name, setting.Value)
	case setting.Secret:
		fmt.Fprintf(w, "  %-*s  configured, from %s\n", width, setting.Name, describeSource(setting.Source))
	case setting.Value == "":
		fmt.Fprintf(w, "  %-*s  empty, from %s\n", width, setting.Name, describeSource(setting.Source))
	default:
		fmt.Fprintf(w, "  %-*s  %s, from %s\n", width, setting.Name, setting.Value, describeSource(setting.Source))
	}

	for _, shadowed := range setting.Shadowed {
		fmt.Fprintf(w, "%soverrides %s\n", indent, describeSource(shadowed))
	}
	if setting.Problem != "" {
		fmt.Fprintf(w, "%sproblem: %s\n", indent, setting.Problem)
	}
}

func describeSource(source config.SettingSource) string {
	switch source.Kind {
	case config.SourceFlag:
		return "the " + source.Name + " flag"
	case config.SourceEnvironment:
		return source.Name + " in the environment"
	case config.SourceDotenv:
		return source.Name + " in " + source.Path
	case config.SourceOverride:
		return "the program running bb"
	case config.SourceKeyring:
		return "the OS keyring"
	case config.SourceRegistry:
		return source.Name + " in " + source.Path
	case config.SourceDefault:
		return "the default"
	}

	return source.Name + " in the " + source.Kind + " configuration " + source.Path
}

func plural(count int, one, many string) string {
	if count == 1 {
		return one
	}

	return many
}
