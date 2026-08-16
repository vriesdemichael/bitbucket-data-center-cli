package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/diagnostics"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
)

// Version is set at build time via -ldflags "-X main.Version=<semver>".
var Version = "dev"

func main() {
	cmd := cli.NewRootCommand()
	cmd.Version = Version
	os.Exit(executeRootCommand(cmd, os.Args[1:], os.Stdout, os.Stderr))
}

func executeRootCommand(rootCmd *cobra.Command, args []string, stdout, stderr io.Writer) int {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}

	if err := cli.ClassifyUsageError(rootCmd.Execute()); err != nil {
		emitCommandFailureDiagnostic(err, stderr)

		// Under --json, stdout is a machine contract, and a failure that leaves
		// it empty is indistinguishable from a command that produced malformed
		// output. Emit the classified failure there; stderr keeps the same
		// human-readable line either way.
		if jsonRequested(rootCmd, args) {
			if writeErr := jsonoutput.WriteError(stdout, err); writeErr != nil {
				fmt.Fprintln(stderr, writeErr.Error())
			}
		}

		fmt.Fprintln(stderr, err.Error())
		return apperrors.ExitCode(err)
	}

	return 0
}

// jsonRequested reports whether machine output was asked for.
//
// The parsed flag is authoritative when parsing reached it. It does not always:
// `bb --bogus --json` fails before pflag sees --json, and an unknown flag is
// exactly the case where a script most needs a parseable answer. Fall back to
// the raw arguments there.
func jsonRequested(rootCmd *cobra.Command, args []string) bool {
	if rootCmd != nil {
		if flag := rootCmd.PersistentFlags().Lookup("json"); flag != nil && flag.Value.String() == "true" {
			return true
		}
	}

	return argsRequestJSON(args)
}

func argsRequestJSON(args []string) bool {
	requested := false

	for _, arg := range args {
		// Everything after -- is a positional argument, not a flag.
		if arg == "--" {
			break
		}

		switch arg {
		case "--json", "--json=true":
			requested = true
		case "--json=false":
			requested = false
		}
	}

	return requested
}

func emitCommandFailureDiagnostic(err error, stderr io.Writer) {
	logger, enabled := loggerFromEnvironment(stderr)
	if !enabled {
		return
	}

	logger.Error("command execution failed", map[string]any{
		"correlation_id": newCorrelationID(),
		"error_kind":     apperrors.KindOf(err),
		"exit_code":      apperrors.ExitCode(err),
		"error":          err.Error(),
	})
}

func loggerFromEnvironment(stderr io.Writer) (*diagnostics.Logger, bool) {
	rawLevel := strings.TrimSpace(os.Getenv("BB_LOG_LEVEL"))
	rawFormat := strings.TrimSpace(os.Getenv("BB_LOG_FORMAT"))
	enabled := rawLevel != "" || rawFormat != ""
	if !enabled {
		return diagnostics.NewLogger(diagnostics.Config{}, io.Discard), false
	}

	level := rawLevel
	if level == "" {
		level = string(diagnostics.LevelError)
	}
	parsedLevel, levelErr := diagnostics.ParseLevel(level)
	if levelErr != nil {
		return diagnostics.NewLogger(diagnostics.Config{}, io.Discard), false
	}

	format := rawFormat
	if format == "" {
		format = string(diagnostics.FormatText)
	}
	parsedFormat, formatErr := diagnostics.ParseFormat(format)
	if formatErr != nil {
		return diagnostics.NewLogger(diagnostics.Config{}, io.Discard), false
	}

	return diagnostics.NewLogger(diagnostics.Config{Level: parsedLevel, Format: parsedFormat}, stderr), true
}

func newCorrelationID() string {
	buffer := make([]byte, 8)
	if _, err := rand.Read(buffer); err != nil {
		return ""
	}

	return hex.EncodeToString(buffer)
}
