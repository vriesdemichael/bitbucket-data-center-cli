package prompt

import (
	"bytes"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
)

// noEnvironment is a Lookup that reports nothing set, so a test is not
// affected by the harness it runs in. Without it CLAUDECODE or CI decides the
// outcome, which is a real property of the helper and a nuisance in a table.
func noEnvironment(string) (string, bool) { return "", false }

func TestConfirmDestructive(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		request Request
		typed   string
		wantErr string
	}{
		{
			name: "an explicit target with --yes proceeds",
			request: Request{
				Yes: true, TargetExplicit: true, Resource: "PRJ/demo", Flag: "--yes",
			},
		},
		{
			name: "--yes is inert when the target was inferred",
			request: Request{
				Yes: true, TargetExplicit: false, Resource: "PRJ/demo", Flag: "--yes",
			},
			wantErr: "--yes only applies when the target is named explicitly",
		},
		{
			name: "no person and no --yes refuses, naming the flag",
			request: Request{
				TargetExplicit: true, Resource: "PRJ/demo", Flag: "--yes", Lookup: noEnvironment,
			},
			wantErr: "--yes is required to delete PRJ/demo",
		},
		{
			name: "machine output refuses like anything else",
			request: Request{
				TargetExplicit: true, Resource: "PRJ/demo", Flag: "--yes", MachineOutput: true, Lookup: noEnvironment,
			},
			wantErr: "--yes is required to delete PRJ/demo",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			request := testCase.request
			request.In = strings.NewReader(testCase.typed)
			request.Out = &bytes.Buffer{}

			err := ConfirmDestructive(request)

			if testCase.wantErr == "" {
				if err != nil {
					t.Fatalf("proceeding was refused: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("the command was allowed to proceed; expected a refusal mentioning %q", testCase.wantErr)
			}
			if !strings.Contains(err.Error(), testCase.wantErr) {
				t.Errorf("error = %q, want it to mention %q", err.Error(), testCase.wantErr)
			}
			if kind := apperrors.KindOf(err); kind != apperrors.KindValidation {
				t.Errorf("kind = %v, want KindValidation so the exit code is the usage one", kind)
			}
		})
	}
}

// TestTheConfirmationMustNameTheResource covers the prompt itself.
//
// The environment rules and the terminal check both refuse under `go test`, so
// confirmDeletion is exercised directly. That is the honest seam: what is being
// asserted here is what a typed answer does, not when the question is asked.
func TestTheConfirmationMustNameTheResource(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		typed    string
		resource string
		accepted bool
	}{
		{name: "the exact name", typed: "PRJ/demo\n", resource: "PRJ/demo", accepted: true},
		{name: "surrounding space is forgiven", typed: "  PRJ/demo  \n", resource: "PRJ/demo", accepted: true},
		{name: "no trailing newline", typed: "PRJ/demo", resource: "PRJ/demo", accepted: true},
		{name: "a bare yes is not enough", typed: "y\n", resource: "PRJ/demo"},
		{name: "empty is not enough", typed: "\n", resource: "PRJ/demo"},
		{name: "a different repository", typed: "PRJ/other\n", resource: "PRJ/demo"},
		{name: "wrong case", typed: "prj/demo\n", resource: "PRJ/demo"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			out := &bytes.Buffer{}
			err := confirmDeletion(strings.NewReader(testCase.typed), out, testCase.resource)

			if testCase.accepted {
				if err != nil {
					t.Fatalf("a correct answer was rejected: %v", err)
				}
			} else if err == nil {
				t.Fatalf("%q was accepted as confirmation of %q", testCase.typed, testCase.resource)
			}

			if !strings.Contains(out.String(), testCase.resource) {
				t.Errorf("the question did not name what would be deleted: %q", out.String())
			}
		})
	}
}

// TestAnEmptyTargetIsRefused closes a shape that reads as safe and is not.
//
// With no resource name the typed-name confirmation is satisfied by pressing
// return, which is the keystroke this design exists to reject. The test was
// written first and failed, which is how the check below came to exist.
func TestAnEmptyTargetIsRefused(t *testing.T) {
	t.Parallel()

	for _, resource := range []string{"", "   "} {
		if err := confirmDeletion(strings.NewReader("\n"), &bytes.Buffer{}, resource); err == nil {
			t.Errorf("a bare return confirmed the deletion of %q", resource)
		}
	}
}

// TestConfirmActionReadsAYesOrNo covers the question with no target to type.
//
// bb auth gpg-key clear has nothing to name, so the confirmation is weaker by
// design and the flag carries the safety instead. This lives here rather than
// beside the command because the environment is injectable here; in the command
// the real CLAUDECODE or CI refuses before the read is ever reached.
func TestConfirmActionReadsAYesOrNo(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		typed    string
		proceeds bool
	}{
		{name: "y", typed: "y\n", proceeds: true},
		{name: "yes", typed: "yes\n", proceeds: true},
		{name: "upper case", typed: "Y\n", proceeds: true},
		{name: "return alone declines", typed: "\n"},
		{name: "n declines", typed: "n\n"},
		{name: "anything else declines", typed: "sure\n"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			out := &bytes.Buffer{}
			err := confirmYesNo(strings.NewReader(testCase.typed), out, "clear all GPG keys")

			if testCase.proceeds && err != nil {
				t.Errorf("%q was rejected: %v", testCase.typed, err)
			}
			if !testCase.proceeds && err == nil {
				t.Errorf("%q was accepted as confirmation", testCase.typed)
			}
			if !strings.Contains(out.String(), "clear all GPG keys") {
				t.Errorf("the question did not say what it would do: %q", out.String())
			}
		})
	}
}

// TestConfirmActionSaysWhatItWillDo keeps the refusal actionable.
func TestConfirmActionSaysWhatItWillDo(t *testing.T) {
	t.Parallel()

	err := ConfirmAction(Request{
		In:     strings.NewReader(""),
		Out:    &bytes.Buffer{},
		Flag:   "--yes",
		Lookup: func(string) (string, bool) { return "", false },
	}, "clear all GPG keys")

	if err == nil {
		t.Fatal("the action proceeded with nobody to confirm it")
	}
	for _, expected := range []string{"--yes", "clear all GPG keys"} {
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("error = %q, want it to mention %q", err.Error(), expected)
		}
	}
}
