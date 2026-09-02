package cli

import (
	"errors"
	"io"
	"testing"

	"github.com/spf13/cobra"
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
	if err := ClassifyUsageError(nil); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}
