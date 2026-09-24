package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/dryrunpreview"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/outwriter"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/diagnostics"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	updateworkflow "github.com/vriesdemichael/bitbucket-data-center-cli/internal/workflows/update"
)

// Version is set at build time via -ldflags "-X main.Version=<semver>".
var Version = "dev"

func main() {
	// bb update on Windows sets the binary it replaced aside, because Windows
	// will not delete a file a process is running from, and the helper of
	// earlier releases left files of its own there. Each run deletes what it
	// can of those, in the background and silently, so that it can neither
	// slow a command down nor write into its output.
	go updateworkflow.RemoveUpdateLeftovers()

	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is main short of exiting, so a test reaches everything main sets up.
func run(args []string, stdout, stderr io.Writer) int {
	cmd := cli.NewRootCommand()
	cmd.Version = Version

	// Machine output reports which binary produced it (ADR-064). Set here, once,
	// because the version is stamped into this package at build time and the
	// ~250 sites that write an envelope have no reason to know it.
	jsonoutput.SetReleaseVersion(Version)

	// An interrupt cancels the command's context instead of killing the
	// process, so a request in flight ends through the transport and is
	// reported as what it was: cancelled, exit 12, or unknown_outcome for a
	// mutation that had already reached the server (#574). Only the first is
	// caught. stop restores the default, so a second interrupt still ends a
	// command that is not listening to its context.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	finished := make(chan struct{})
	defer close(finished)
	go superviseInterrupt(ctx, finished, interruptGrace, stop, args, stdout, stderr)

	cmd.SetContext(ctx)
	cmd.SetArgs(args)

	// A completion request has nobody to read stderr, and a terminal to write
	// it over. Cobra ends every one with "Completion ended with directive:
	// ...", which bash, zsh and fish discard because their generated scripts
	// redirect it -- and PowerShell's does not, so the line lands on the
	// prompt the user is typing at. There is nothing else worth saying on the
	// stderr of a tab press; BB_COMPLETION_DEBUG writes to the real one
	// directly, for running __complete by hand.
	if completionRequest(args) {
		stderr = io.Discard
		cmd.SetErr(io.Discard)
	}

	return executeRootCommand(cmd, args, stdout, stderr)
}

// completionRequest reports the hidden commands a shell calls on a tab press.
func completionRequest(args []string) bool {
	if len(args) == 0 {
		return false
	}

	return args[0] == cobra.ShellCompRequestCmd || args[0] == cobra.ShellCompNoDescRequestCmd
}

// interruptGrace is how long an interrupted command has to answer for itself
// before main answers for it. A test shortens it.
var interruptGrace = 2 * time.Second

// superviseInterrupt restores default signal handling on the first interrupt,
// and answers for a command that the cancellation does not reach.
//
// Cancelling the context is the whole mechanism for a command that watches it.
// A command blocked on a read does not: `bb api --input -`, `auth login
// --token-stdin` and the git credential helper all wait on stdin, and a reader
// there does not notice a cancelled context. v4.0.0 had no handler at all, so
// the first interrupt ended the process; keeping the terminal waiting for a
// second one that prints nothing is worse than either.
func superviseInterrupt(ctx context.Context, finished <-chan struct{}, grace time.Duration, stop func(), args []string, stdout, stderr io.Writer) {
	select {
	case <-ctx.Done():
	case <-finished:
		return
	}

	stop()

	timer := time.NewTimer(grace)
	defer timer.Stop()

	select {
	case <-finished:
	case <-timer.C:
		interruptAnswer(args, stdout, stderr)
	}
}

// interruptAnswer ends the process once the answer is written. A test replaces
// it, because os.Exit would take the test binary with it.
var interruptAnswer = reportInterrupt

func reportInterrupt(args []string, stdout, stderr io.Writer) {
	os.Exit(writeInterruptAnswer(args, stdout, stderr))
}

// writeInterruptAnswer writes what executeRootCommand would have written for a
// cancelled run, and returns its exit code, because what the command is waiting
// for is not going to return.
//
// The raw arguments decide whether stdout is a machine contract. The parsed
// flag belongs to a command that is still running, and reading it from here
// would be a data race.
func writeInterruptAnswer(args []string, stdout, stderr io.Writer) int {
	err := apperrors.New(apperrors.KindCancelled, "interrupted", nil)

	if settings := argsRequestOutput(args); settings.Machine {
		if writeErr := jsonoutput.WriteError(jsonoutput.Bind(stdout, settings), err); writeErr != nil {
			fmt.Fprintln(stderr, writeErr.Error())
		}
	}

	fmt.Fprintln(stderr, err.Error())

	return apperrors.ExitCode(err)
}

func executeRootCommand(rootCmd *cobra.Command, args []string, stdout, stderr io.Writer) int {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}

	// Command output goes through a recorder so a failed write is not lost.
	// Individually those writes cannot be checked -- a command has no way to
	// report that stdout is broken -- but here, after the command has run and
	// an exit code is still available, it can be.
	output := outwriter.New(stdout)
	rootCmd.SetOut(output)

	// Execute answers a group given an unknown subcommand with help and no error,
	// so the result is inspected rather than trusted. Checked here rather than
	// inside the tree because Cobra has to have parsed the group's flags first.
	executed, executeErr := rootCmd.ExecuteC()
	if executeErr == nil {
		executeErr = cli.UnknownSubcommandError(rootCmd, args)
	}
	executeErr = interrupted(rootCmd, executeErr)

	// A command reporting the state it read through the exit status has not
	// failed (ADR-091): its output is written, so it is checked like any
	// other success, and only the code and the reason differ.
	var state *apperrors.StateExit
	if errors.As(executeErr, &state) {
		executeErr = nil
	}

	if err := cli.ClassifyUsageError(executeErr); err != nil {
		err = cli.HintGHFieldList(err, args, executed)
		emitCommandFailureDiagnostic(err, stderr)

		settings := invocationOutput(rootCmd, executed, args)

		// Under --dry-run a failure that is an answer about the real run is
		// the verdict (ADR-096): a document that exits 0 since the verdict is
		// in it, or, as text, the verdict on stdout and the exit code the real
		// run would have.
		if settings.Mode == jsonoutput.ModeDryRun && jsonoutput.IsVerdict(err) {
			return reportVerdict(err, settings, stdout, stderr)
		}

		// Under --json or --yaml, stdout is a machine contract, and a failure
		// that leaves it empty is indistinguishable from a command that
		// produced malformed output. Emit the classified failure there; stderr
		// keeps the same human-readable line either way.
		if settings.Machine {
			if writeErr := jsonoutput.WriteError(jsonoutput.Bind(stdout, settings), err); writeErr != nil {
				fmt.Fprintln(stderr, writeErr.Error())
			}
		}

		fmt.Fprintln(stderr, err.Error())
		return apperrors.ExitCode(err)
	}

	// The command believes it succeeded. If its output never reached the
	// destination, saying so is the whole point of recording it: truncated
	// output reported as success is indistinguishable from complete output.
	if writeErr := output.Err(); writeErr != nil {
		failure := apperrors.New(apperrors.KindInternal, "failed to write command output", writeErr)
		emitCommandFailureDiagnostic(failure, stderr)
		fmt.Fprintln(stderr, failure.Error())
		return apperrors.ExitCode(failure)
	}

	if state != nil {
		// A dry run predicting failure says why on stdout, so it has no line
		// of its own to add here.
		if state.Reason != "" {
			fmt.Fprintln(stderr, state.Reason)
		}
		return state.Code
	}

	return 0
}

// interrupted reports a failure that followed an interrupt as the interrupt.
//
// The transport classifies a request an interrupt cut short, but a command
// can fail somewhere else -- a git subprocess, a prompt, the gap between two
// requests -- and what it returns then is a consequence of the interrupt, not
// a failure of its own. unknown_outcome is kept: it is the more specific
// answer, and the one that says to check before running the command again.
func interrupted(rootCmd *cobra.Command, err error) error {
	if err == nil {
		return nil
	}

	ctx := rootCmd.Context()
	if ctx == nil || !errors.Is(ctx.Err(), context.Canceled) {
		return err
	}

	switch apperrors.KindOf(err) {
	case apperrors.KindCancelled, apperrors.KindUnknownOutcome:
		return err
	default:
		return apperrors.New(apperrors.KindCancelled, "interrupted", err)
	}
}

// reportVerdict answers a failure under --dry-run that is a verdict on the real
// run, and returns the exit code.
func reportVerdict(err error, settings jsonoutput.Settings, stdout, stderr io.Writer) int {
	if settings.Machine {
		if writeErr := jsonoutput.WriteError(jsonoutput.Bind(stdout, settings), err); writeErr != nil {
			fmt.Fprintln(stderr, writeErr.Error())
			return apperrors.ExitCode(err)
		}

		return 0
	}

	failure := jsonoutput.EnvelopeErrorOf(err)
	verdict := jsonoutput.Preview{Tier: jsonoutput.TierOfFailure(err), Effects: []jsonoutput.Effect{}, Error: &failure}
	if writeErr := dryrunpreview.WriteText(stdout, settings.Command, verdict); writeErr != nil {
		fmt.Fprintln(stderr, err.Error())
	}

	return apperrors.ExitCode(err)
}

// invocationOutput is what the invocation decided about its output.
//
// Once PersistentPreRunE has run, it is bound to the command's output writer,
// and that is the answer: it is also where a command that does not take
// --dry-run is recorded as a plain run. Before that -- an unknown flag, a
// missing argument -- it comes from the parsed flags, and from the raw
// arguments for what parsing did not reach: `bb --bogus --json` fails before
// pflag sees --json, and an unknown flag is exactly the case where a script
// most needs a parseable answer.
func invocationOutput(rootCmd, executed *cobra.Command, args []string) jsonoutput.Settings {
	if rootCmd != nil {
		if settings, bound := jsonoutput.SettingsOf(rootCmd.OutOrStdout()); bound {
			return settings
		}
	}

	requested := argsRequestOutput(args)
	if rootCmd == nil {
		return requested
	}

	parsed := cli.OutputSettingsFromFlags(rootCmd, executed)
	if !parsed.Machine && requested.Machine {
		parsed.Machine, parsed.Format = true, requested.Format
	}
	if parsed.Mode == jsonoutput.ModeRun && requested.Mode == jsonoutput.ModeDryRun {
		parsed.Mode = jsonoutput.ModeDryRun
	}

	return parsed
}

// argsRequestOutput reads --json, --yaml, --dry-run and --describe from the raw
// arguments. --json and --yaml together answer in JSON, since that combination
// is itself the failure being reported.
func argsRequestOutput(args []string) jsonoutput.Settings {
	json, yaml, dryRun, describe := false, false, false, false

	for _, arg := range args {
		// Everything after -- is a positional argument, not a flag.
		if arg == "--" {
			break
		}

		switch arg {
		case "--json", "--json=true":
			json = true
		case "--json=false":
			json = false
		case "--yaml", "--yaml=true":
			yaml = true
		case "--yaml=false":
			yaml = false
		case "--dry-run", "--dry-run=true":
			dryRun = true
		case "--dry-run=false":
			dryRun = false
		case "--describe", "--describe=true":
			describe = true
		case "--describe=false":
			describe = false
		}
	}

	settings := jsonoutput.Settings{Machine: json || yaml, Format: jsonoutput.FormatJSON}
	if yaml && !json {
		settings.Format = jsonoutput.FormatYAML
	}
	if dryRun && !describe {
		settings.Mode = jsonoutput.ModeDryRun
	}

	return settings
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
