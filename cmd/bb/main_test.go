package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"syscall"
	"testing"

	"github.com/spf13/cobra"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
)

func TestExecuteRootCommandWithoutDiagnostics(t *testing.T) {
	t.Setenv("BB_LOG_LEVEL", "")
	t.Setenv("BB_LOG_FORMAT", "")

	cmd := &cobra.Command{Use: "test", RunE: func(command *cobra.Command, args []string) error {
		return apperrors.New(apperrors.KindValidation, "invalid input", nil)
	}}

	buffer := &bytes.Buffer{}
	exitCode := executeRootCommand(cmd, nil, nil, buffer)
	if exitCode != 2 {
		t.Fatalf("expected exit code 2, got %d", exitCode)
	}

	output := strings.TrimSpace(buffer.String())
	if output != "validation: invalid input" {
		t.Fatalf("expected only plain error output, got %q", output)
	}
}

func TestExecuteRootCommandWithDiagnosticsJSONL(t *testing.T) {
	t.Setenv("BB_LOG_LEVEL", "error")
	t.Setenv("BB_LOG_FORMAT", "jsonl")

	cmd := &cobra.Command{Use: "test", RunE: func(command *cobra.Command, args []string) error {
		return apperrors.New(apperrors.KindValidation, "invalid input", nil)
	}}

	buffer := &bytes.Buffer{}
	exitCode := executeRootCommand(cmd, nil, nil, buffer)
	if exitCode != 2 {
		t.Fatalf("expected exit code 2, got %d", exitCode)
	}

	lines := strings.Split(strings.TrimSpace(buffer.String()), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected diagnostics + error line, got %q", buffer.String())
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &payload); err != nil {
		t.Fatalf("expected first line to be json diagnostics, got %q (%v)", lines[0], err)
	}

	if payload["level"] != "error" {
		t.Fatalf("expected diagnostics level error, got %v", payload["level"])
	}
	if payload["error_kind"] != "validation" {
		t.Fatalf("expected diagnostics error_kind validation, got %v", payload["error_kind"])
	}
	if _, ok := payload["correlation_id"].(string); !ok {
		t.Fatalf("expected correlation_id string, got %T", payload["correlation_id"])
	}

	if lines[len(lines)-1] != "validation: invalid input" {
		t.Fatalf("expected final user-facing error line, got %q", lines[len(lines)-1])
	}
}

func TestKindFallbackForPlainErrors(t *testing.T) {
	t.Setenv("BB_LOG_LEVEL", "error")
	t.Setenv("BB_LOG_FORMAT", "jsonl")

	cmd := &cobra.Command{Use: "test", RunE: func(command *cobra.Command, args []string) error {
		return errors.New("plain failure")
	}}

	buffer := &bytes.Buffer{}
	exitCode := executeRootCommand(cmd, nil, nil, buffer)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}

	lines := strings.Split(strings.TrimSpace(buffer.String()), "\n")
	var payload map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &payload); err != nil {
		t.Fatalf("expected first line json diagnostics, got %q (%v)", lines[0], err)
	}

	if payload["error_kind"] != "internal" {
		t.Fatalf("expected internal kind fallback, got %v", payload["error_kind"])
	}
}

func TestExecuteRootCommandSuccess(t *testing.T) {
	t.Setenv("BB_LOG_LEVEL", "")
	t.Setenv("BB_LOG_FORMAT", "")

	cmd := &cobra.Command{Use: "test", RunE: func(command *cobra.Command, args []string) error {
		return nil
	}}

	buffer := &bytes.Buffer{}
	exitCode := executeRootCommand(cmd, nil, nil, buffer)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	if buffer.String() != "" {
		t.Fatalf("expected no stderr output on success, got %q", buffer.String())
	}
}

func TestLoggerFromEnvironmentBranches(t *testing.T) {
	t.Run("partial config uses defaults", func(t *testing.T) {
		t.Setenv("BB_LOG_LEVEL", "")
		t.Setenv("BB_LOG_FORMAT", "jsonl")

		buffer := &bytes.Buffer{}
		logger, enabled := loggerFromEnvironment(buffer)
		if !enabled {
			t.Fatal("expected logger to be enabled")
		}

		logger.Error("test", map[string]any{"k": "v"})
		if !strings.Contains(buffer.String(), "\"level\":\"error\"") {
			t.Fatalf("expected default error level in json output, got %q", buffer.String())
		}
	})

	t.Run("invalid level disables logger", func(t *testing.T) {
		t.Setenv("BB_LOG_LEVEL", "trace")
		t.Setenv("BB_LOG_FORMAT", "jsonl")

		buffer := &bytes.Buffer{}
		logger, enabled := loggerFromEnvironment(buffer)
		if enabled {
			t.Fatal("expected logger to be disabled for invalid level")
		}

		logger.Error("ignored", map[string]any{"k": "v"})
		if buffer.String() != "" {
			t.Fatalf("expected no output from disabled logger, got %q", buffer.String())
		}
	})

	t.Run("invalid format disables logger", func(t *testing.T) {
		t.Setenv("BB_LOG_LEVEL", "error")
		t.Setenv("BB_LOG_FORMAT", "yaml")

		buffer := &bytes.Buffer{}
		_, enabled := loggerFromEnvironment(buffer)
		if enabled {
			t.Fatal("expected logger to be disabled for invalid format")
		}
	})
}

// newJSONRootCommand builds a root command carrying the same persistent --json
// flag the real root has, so the flag-lookup path is exercised rather than only
// the raw-argument fallback.
func newJSONRootCommand(t *testing.T, runErr error) *cobra.Command {
	t.Helper()

	cmd := &cobra.Command{
		Use:           "test",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(command *cobra.Command, args []string) error {
			return runErr
		},
	}
	cmd.PersistentFlags().Bool("json", false, "Output as JSON")

	return cmd
}

func TestExecuteRootCommandEmitsErrorEnvelopeUnderJSON(t *testing.T) {
	t.Setenv("BB_LOG_LEVEL", "")
	t.Setenv("BB_LOG_FORMAT", "")

	cmd := newJSONRootCommand(t, apperrors.New(apperrors.KindNotFound, "repository does not exist", nil))
	cmd.SetArgs([]string{"--json"})

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	exitCode := executeRootCommand(cmd, []string{"--json"}, stdout, stderr)

	if exitCode != 4 {
		t.Fatalf("expected exit code 4, got %d", exitCode)
	}

	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("expected parseable stdout, got %q (%v)", stdout.String(), err)
	}

	if _, present := envelope["version"]; present {
		t.Fatalf("the failure envelope still carries a contract version (ADR-064): %v", envelope)
	}
	if meta, _ := envelope["meta"].(map[string]any); meta["bbVersion"] == nil {
		t.Fatalf("the failure envelope carries no meta.bbVersion: %v", envelope)
	}
	if _, present := envelope["data"]; present {
		t.Fatal("expected no data key on the failure envelope")
	}

	meta, ok := envelope["meta"].(map[string]any)
	if !ok || meta["bbVersion"] == nil || meta["bbVersion"] == "" {
		t.Fatalf("the failure envelope carries no meta.bbVersion, got %v", envelope["meta"])
	}

	payload, ok := envelope["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got %T", envelope["error"])
	}
	if payload["kind"] != "not_found" {
		t.Fatalf("expected kind not_found, got %v", payload["kind"])
	}
	if payload["message"] != "repository does not exist" {
		t.Fatalf("expected message without kind prefix, got %v", payload["message"])
	}
	if payload["exitCode"] != float64(4) {
		t.Fatalf("expected exitCode 4, got %v", payload["exitCode"])
	}

	// The human-readable line stays on stderr regardless of --json.
	if !strings.Contains(stderr.String(), "not_found: repository does not exist") {
		t.Fatalf("expected human error on stderr, got %q", stderr.String())
	}
}

func TestExecuteRootCommandEveryKindRoundTrips(t *testing.T) {
	t.Setenv("BB_LOG_LEVEL", "")
	t.Setenv("BB_LOG_FORMAT", "")

	for _, kind := range apperrors.Kinds() {
		t.Run(string(kind), func(t *testing.T) {
			runErr := apperrors.New(kind, "boom", nil)

			cmd := newJSONRootCommand(t, runErr)
			cmd.SetArgs([]string{"--json"})

			stdout := &bytes.Buffer{}
			exitCode := executeRootCommand(cmd, []string{"--json"}, stdout, &bytes.Buffer{})

			var envelope struct {
				Error struct {
					Kind     string `json:"kind"`
					Message  string `json:"message"`
					ExitCode int    `json:"exitCode"`
				} `json:"error"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatalf("expected parseable stdout, got %q (%v)", stdout.String(), err)
			}

			if envelope.Error.Kind != string(kind) {
				t.Fatalf("expected kind %q, got %q", kind, envelope.Error.Kind)
			}
			if envelope.Error.Message != "boom" {
				t.Fatalf("expected message boom, got %q", envelope.Error.Message)
			}
			// The envelope's exit_code must agree with the process exit status,
			// or a script branching on the payload disagrees with $?.
			if envelope.Error.ExitCode != exitCode {
				t.Fatalf("envelope exitCode %d disagrees with process exit %d", envelope.Error.ExitCode, exitCode)
			}
			if exitCode != apperrors.ExitCode(runErr) {
				t.Fatalf("expected exit %d, got %d", apperrors.ExitCode(runErr), exitCode)
			}
		})
	}
}

func TestExecuteRootCommandLeavesStdoutEmptyWithoutJSON(t *testing.T) {
	t.Setenv("BB_LOG_LEVEL", "")
	t.Setenv("BB_LOG_FORMAT", "")

	cmd := newJSONRootCommand(t, apperrors.New(apperrors.KindValidation, "invalid input", nil))
	cmd.SetArgs(nil)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	if exitCode := executeRootCommand(cmd, nil, stdout, stderr); exitCode != 2 {
		t.Fatalf("expected exit code 2, got %d", exitCode)
	}

	if stdout.String() != "" {
		t.Fatalf("expected empty stdout without --json, got %q", stdout.String())
	}
	if strings.TrimSpace(stderr.String()) != "validation: invalid input" {
		t.Fatalf("expected plain stderr error, got %q", stderr.String())
	}
}

func TestExecuteRootCommandEmitsEnvelopeWhenFlagParsingFails(t *testing.T) {
	t.Setenv("BB_LOG_LEVEL", "")
	t.Setenv("BB_LOG_FORMAT", "")

	// pflag never reaches --json here, so only the raw arguments reveal that a
	// machine contract was requested — and an unknown flag is precisely when a
	// script needs a parseable answer.
	cmd := newJSONRootCommand(t, nil)
	cmd.SetArgs([]string{"--bogus", "--json"})

	stdout := &bytes.Buffer{}
	exitCode := executeRootCommand(cmd, []string{"--bogus", "--json"}, stdout, &bytes.Buffer{})

	// An unknown flag is the caller's mistake, so it reports validation and
	// exit 2 rather than falling through to internal and exit 1.
	if exitCode != 2 {
		t.Fatalf("expected exit code 2 for an unknown flag, got %d", exitCode)
	}

	var envelope struct {
		Error struct {
			Kind     string `json:"kind"`
			Message  string `json:"message"`
			ExitCode int    `json:"exitCode"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("expected parseable stdout, got %q (%v)", stdout.String(), err)
	}
	if envelope.Error.Kind != "validation" || envelope.Error.ExitCode != 2 {
		t.Fatalf("expected validation/2, got %+v", envelope.Error)
	}
	if !strings.Contains(envelope.Error.Message, "unknown flag") {
		t.Fatalf("expected the parser message preserved, got %q", envelope.Error.Message)
	}
}

func TestArgsRequestJSON(t *testing.T) {
	testCases := []struct {
		name     string
		args     []string
		expected bool
	}{
		{name: "absent", args: []string{"auth", "status"}, expected: false},
		{name: "bare flag", args: []string{"--json", "auth"}, expected: true},
		{name: "explicit true", args: []string{"--json=true"}, expected: true},
		{name: "explicit false", args: []string{"--json=false"}, expected: false},
		{name: "last occurrence wins", args: []string{"--json", "--json=false"}, expected: false},
		{name: "re-enabled", args: []string{"--json=false", "--json"}, expected: true},
		{name: "after the separator is positional", args: []string{"--", "--json"}, expected: false},
		// Known false positive: a flag value of literally --json reads as a
		// request. It costs an unwanted envelope on stdout on the error path
		// only, and the parsed flag wins whenever parsing succeeded.
		{name: "flag value that looks like the flag", args: []string{"--message", "--json"}, expected: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := argsRequestJSON(testCase.args); got != testCase.expected {
				t.Fatalf("argsRequestJSON(%v) = %v, want %v", testCase.args, got, testCase.expected)
			}
		})
	}
}

// unwritableStdout stands in for a closed or full stdout — a broken pipe when
// the consumer exits early, for instance.
type unwritableStdout struct{}

func (unwritableStdout) Write([]byte) (int, error) {
	return 0, errors.New("stdout is closed")
}

func TestExecuteRootCommandReportsEnvelopeWriteFailure(t *testing.T) {
	t.Setenv("BB_LOG_LEVEL", "")
	t.Setenv("BB_LOG_FORMAT", "")

	cmd := newJSONRootCommand(t, apperrors.New(apperrors.KindTransient, "upstream unavailable", nil))
	cmd.SetArgs([]string{"--json"})

	stderr := &bytes.Buffer{}
	exitCode := executeRootCommand(cmd, []string{"--json"}, unwritableStdout{}, stderr)

	// A failure to write the envelope must not change the exit code the
	// taxonomy dictates; the process still failed for the original reason.
	if exitCode != 10 {
		t.Fatalf("expected exit code 10, got %d", exitCode)
	}

	output := stderr.String()
	if !strings.Contains(output, "failed to write JSON error output") {
		t.Fatalf("expected the write failure reported on stderr, got %q", output)
	}
	if !strings.Contains(output, "transient: upstream unavailable") {
		t.Fatalf("expected the original error still reported, got %q", output)
	}
}

// stdoutFailure fails every write, standing in for a full disk or a read-only
// destination.
type stdoutFailure struct{ err error }

func (w stdoutFailure) Write([]byte) (int, error) { return 0, w.err }

// TestExecuteRootCommandFailsWhenOutputCannotBeWritten covers the case the
// output recorder exists for.
//
// The command succeeds and prints its result; the write never lands. Reporting
// success there is indistinguishable from having produced complete output,
// which is how `bb pr list > file` on a full disk used to exit 0 with a
// truncated file.
func TestExecuteRootCommandFailsWhenOutputCannotBeWritten(t *testing.T) {
	t.Setenv("BB_LOG_LEVEL", "")
	t.Setenv("BB_LOG_FORMAT", "")

	cmd := &cobra.Command{Use: "test", RunE: func(command *cobra.Command, args []string) error {
		fmt.Fprintln(command.OutOrStdout(), "the result the caller asked for")
		return nil
	}}

	stderr := &bytes.Buffer{}
	code := executeRootCommand(cmd, nil, stdoutFailure{err: errors.New("no space left on device")}, stderr)

	if code == 0 {
		t.Fatal("expected a failed write to fail the command")
	}
	if !strings.Contains(stderr.String(), "failed to write command output") {
		t.Fatalf("stderr does not explain the failure: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "no space left on device") {
		t.Fatalf("stderr does not name the underlying cause: %q", stderr.String())
	}
}

// TestExecuteRootCommandSucceedsOnAClosedPipe is the other half: `bb ... | head`
// closes the pipe early, and that must stay a success.
func TestExecuteRootCommandSucceedsOnAClosedPipe(t *testing.T) {
	t.Setenv("BB_LOG_LEVEL", "")
	t.Setenv("BB_LOG_FORMAT", "")

	cmd := &cobra.Command{Use: "test", RunE: func(command *cobra.Command, args []string) error {
		fmt.Fprintln(command.OutOrStdout(), "more output than the reader wants")
		return nil
	}}

	stderr := &bytes.Buffer{}
	code := executeRootCommand(cmd, nil, stdoutFailure{err: syscall.EPIPE}, stderr)

	if code != 0 {
		t.Fatalf("a closed pipe must not fail the command, got exit %d: %s", code, stderr.String())
	}
}
