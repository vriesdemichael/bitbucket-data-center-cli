package cli

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/enumflag"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// executeForUsageError drives a real command tree so the error under test is the
// one Cobra and pflag actually produce, not a hand-written copy of it.
func executeForUsageError(t *testing.T, configure func(root *cobra.Command), args ...string) error {
	t.Helper()

	root := &cobra.Command{
		Use:           "bb",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)

	if configure != nil {
		configure(root)
	}

	root.SetArgs(args)

	err := root.Execute()
	if err == nil {
		t.Fatalf("expected %v to fail", args)
	}

	return err
}

// TestClassifyUsageErrorMatchesCobrasRealMessages is the guard on the text
// matching in cobraUsageErrorMarkers.
//
// Cobra raises its own usage errors as plain fmt.Errorf values, so recognising
// them depends on their wording. If a Cobra upgrade rewords one, this fails
// rather than silently reclassifying malformed invocations back to internal.
func TestClassifyUsageErrorMatchesCobrasRealMessages(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		configure func(root *cobra.Command)
		args      []string
	}{
		{
			name: "unknown flag",
			args: []string{"--nonexistent-flag"},
		},
		{
			name: "unknown shorthand flag",
			args: []string{"-Z"},
		},
		{
			name: "flag needs an argument",
			configure: func(root *cobra.Command) {
				root.Flags().String("host", "", "")
			},
			args: []string{"--host"},
		},
		{
			name: "invalid value for a typed flag",
			configure: func(root *cobra.Command) {
				root.Flags().Int("limit", 0, "")
			},
			args: []string{"--limit", "not-a-number"},
		},
		{
			// The most common usage error of all, and the one the marker list
			// was missing: sixteen commands reported a forgotten flag as a
			// defect in bb rather than a mistake in the invocation (#475).
			name: "missing required flag",
			configure: func(root *cobra.Command) {
				root.Flags().String("title", "", "")
				_ = root.MarkFlagRequired("title")
			},
			args: []string{},
		},
		{
			name: "unknown command",
			configure: func(root *cobra.Command) {
				root.AddCommand(&cobra.Command{Use: "repo", RunE: func(*cobra.Command, []string) error { return nil }})
			},
			args: []string{"nosuchcommand"},
		},
		{
			name: "too many arguments",
			configure: func(root *cobra.Command) {
				root.Args = cobra.ExactArgs(1)
			},
			args: []string{"one", "two"},
		},
		{
			name: "too few arguments",
			configure: func(root *cobra.Command) {
				root.Args = cobra.MinimumNArgs(2)
			},
			args: []string{"one"},
		},
		{
			name: "mutually exclusive flags",
			configure: func(root *cobra.Command) {
				root.Flags().Bool("all", false, "")
				root.Flags().Int("limit", 25, "")
				root.MarkFlagsMutuallyExclusive("all", "limit")
			},
			args: []string{"--all", "--limit", "5"},
		},
		{
			// enumflag returns a plain error so pflag's wrapper supplies the
			// flag name; this is what makes that error reach the caller as
			// validation and exit 2 rather than internal and exit 1.
			name: "value outside an enum flag's set",
			configure: func(root *cobra.Command) {
				var severity string
				enumflag.Register(root.Flags(), &severity, "severity", "", []string{"LOW", "MEDIUM", "HIGH"}, "Annotation severity")
			},
			args: []string{"--severity", "CRITICAL"},
		},
		{
			name: "argument outside the valid set",
			configure: func(root *cobra.Command) {
				root.Args = cobra.OnlyValidArgs
				root.ValidArgs = []string{"allowed"}
			},
			args: []string{"disallowed"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			raw := executeForUsageError(t, testCase.configure, testCase.args...)

			// Precondition: without classification these fall through to
			// internal, which is the defect being fixed.
			if apperrors.KindOf(raw) != apperrors.KindInternal {
				t.Fatalf("expected an unclassified error, got kind %q", apperrors.KindOf(raw))
			}

			classified := ClassifyUsageError(raw)
			if apperrors.KindOf(classified) != apperrors.KindValidation {
				t.Fatalf("expected validation for %q, got kind %q", raw, apperrors.KindOf(classified))
			}
			if apperrors.ExitCode(classified) != 2 {
				t.Fatalf("expected exit code 2, got %d", apperrors.ExitCode(classified))
			}
			if apperrors.MessageOf(classified) != raw.Error() {
				t.Fatalf("classification changed the message: %q became %q", raw, apperrors.MessageOf(classified))
			}
		})
	}
}

func TestClassifyUsageErrorLeavesClassifiedErrorsAlone(t *testing.T) {
	t.Parallel()

	for _, kind := range apperrors.Kinds() {
		t.Run(string(kind), func(t *testing.T) {
			original := apperrors.New(kind, "already classified", nil)

			// Identity, not matching: the contract is that an already-classified
			// error comes back untouched. errors.Is would also accept a wrapper,
			// which is the thing being ruled out.
			//nolint:errorlint // deliberate identity comparison
			if classified := ClassifyUsageError(original); classified != error(original) {
				t.Fatalf("expected the original error back, got %v", classified)
			}
		})
	}
}

func TestClassifyUsageErrorLeavesGenuineFailuresAlone(t *testing.T) {
	t.Parallel()

	// A transport or server failure must keep falling through to internal.
	// Reclassifying it as validation would tell a caller to fix its invocation
	// when the right response is to retry or escalate.
	for _, message := range []string{
		"connection refused",
		"unexpected EOF",
		"server returned 500",
	} {
		t.Run(message, func(t *testing.T) {
			original := errors.New(message)

			classified := ClassifyUsageError(original)
			if apperrors.KindOf(classified) != apperrors.KindInternal {
				t.Fatalf("expected internal, got kind %q", apperrors.KindOf(classified))
			}
			if apperrors.ExitCode(classified) != 1 {
				t.Fatalf("expected exit code 1, got %d", apperrors.ExitCode(classified))
			}
		})
	}
}

func TestClassifyUsageErrorIgnoresNil(t *testing.T) {
	t.Parallel()

	if err := ClassifyUsageError(nil); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

// TestHintGHFieldListExplainsWhatBBDoesInstead covers the gh habit of naming
// fields after --json (ADR-095): the failure keeps its kind and gains the one
// thing that helps, the caller's own command with the list taken out.
func TestHintGHFieldListExplainsWhatBBDoesInstead(t *testing.T) {
	t.Parallel()

	list := &cobra.Command{Use: "list", RunE: func(*cobra.Command, []string) error { return nil }}
	group := &cobra.Command{Use: "pr"}
	group.AddCommand(list)
	root := &cobra.Command{Use: "bb"}
	root.AddCommand(group)

	unknown := apperrors.New(apperrors.KindValidation, `unknown command "number,title" for "bb pr list"`, nil)
	tooMany := apperrors.New(apperrors.KindValidation, "pr get takes <pr-id>; 2 arguments given", nil)

	testCases := []struct {
		name     string
		err      error
		args     []string
		command  *cobra.Command
		wantHint string
	}{
		{name: "a list the command took for a subcommand", err: unknown,
			args: []string{"pr", "list", "--json", "number,title"}, command: list,
			wantHint: "bb pr list --json | jq '.data', and see them with bb pr list --describe"},
		{name: "a list the command took for an argument too many", err: tooMany,
			args: []string{"pr", "view", "12", "--json", "number"}, command: list,
			wantHint: "bb pr view 12 --json | jq '.data'"},
		{name: "a list given as the flag's value", err: apperrors.New(apperrors.KindValidation,
			`invalid argument "number,title" for "--json" flag`, nil),
			args: []string{"pr", "list", "--json=number,title", "--repo", "P/r"}, command: list,
			wantHint: "bb pr list --json --repo P/r | jq '.data'"},
		{name: "a group names no command to describe", err: unknown,
			args: []string{"pr", "--json", "number,title"}, command: group,
			wantHint: "pick fields from .data with jq"},
		{name: "no list", err: unknown, args: []string{"pr", "list", "--json", "PROJ/repo"}, command: list},
		{name: "a failure about something else", err: apperrors.New(apperrors.KindValidation, "--repo is required", nil),
			args: []string{"pr", "list", "--json", "state"}, command: list},
		{name: "not a validation failure", err: apperrors.New(apperrors.KindNotFound, "unknown command", nil),
			args: []string{"pr", "list", "--json", "number"}, command: list},
		{name: "after the separator", err: unknown, args: []string{"pr", "list", "--", "--json", "number"}, command: list},
		{name: "a boolean value is not a list", err: tooMany, args: []string{"pr", "list", "--json=true"}, command: list},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := HintGHFieldList(testCase.err, testCase.args, testCase.command)
			if testCase.wantHint == "" {
				if got != testCase.err {
					t.Fatalf("hinted where nothing should be: %v", got)
				}
				return
			}

			if !apperrors.IsKind(got, apperrors.KindValidation) {
				t.Fatalf("the hint changed the kind: %v", got)
			}
			message := apperrors.MessageOf(got)
			if !strings.HasPrefix(message, apperrors.MessageOf(testCase.err)) || !strings.Contains(message, testCase.wantHint) {
				t.Fatalf("message = %q, want the original followed by %q", message, testCase.wantHint)
			}
		})
	}
}
