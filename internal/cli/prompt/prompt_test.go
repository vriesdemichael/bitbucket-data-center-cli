package prompt

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/interactive"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
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

// allowPrompting makes the gate permit, so the prompting half of this package
// can be exercised. Under `go test` no stream is a terminal, so without this
// every path past gate is unreachable.
//
// Not parallel-safe: it replaces package state.
func allowPrompting(t *testing.T) {
	t.Helper()
	original := decide
	t.Cleanup(func() { decide = original })
	decide = func(interactive.Options) interactive.Decision {
		return interactive.Decision{Allowed: true}
	}
}

// TestConfirmDestructiveAsksWhenSomeoneIsThere covers the path a person takes.
func TestConfirmDestructiveAsksWhenSomeoneIsThere(t *testing.T) {
	allowPrompting(t)

	cases := []struct {
		name     string
		typed    string
		proceeds bool
	}{
		{name: "the name typed back", typed: "PRJ/demo\n", proceeds: true},
		{name: "a bare yes is not enough", typed: "y\n"},
		{name: "a different repository", typed: "PRJ/other\n"},
		{name: "nothing at all", typed: "\n"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			out := &bytes.Buffer{}
			err := ConfirmDestructive(Request{
				In:             strings.NewReader(testCase.typed),
				Out:            out,
				TargetExplicit: true,
				Resource:       "PRJ/demo",
				Flag:           "--yes",
			})

			if testCase.proceeds && err != nil {
				t.Fatalf("a correct answer was rejected: %v", err)
			}
			if !testCase.proceeds && err == nil {
				t.Fatalf("%q was accepted as confirmation", testCase.typed)
			}
			if !strings.Contains(out.String(), "PRJ/demo") {
				t.Errorf("the question did not name what would be deleted: %q", out.String())
			}
		})
	}
}

// TestConfirmActionAsksWhenSomeoneIsThere covers the yes-or-no path end to end.
func TestConfirmActionAsksWhenSomeoneIsThere(t *testing.T) {
	allowPrompting(t)

	for _, testCase := range []struct {
		typed    string
		proceeds bool
	}{
		{typed: "y\n", proceeds: true},
		{typed: "yes\n", proceeds: true},
		{typed: "n\n"},
		{typed: "\n"},
	} {
		t.Run(strings.TrimSpace(testCase.typed)+"_", func(t *testing.T) {
			err := ConfirmAction(Request{
				In:   strings.NewReader(testCase.typed),
				Out:  &bytes.Buffer{},
				Flag: "--yes",
			}, "clear all GPG keys")

			if testCase.proceeds && err != nil {
				t.Errorf("%q was rejected: %v", testCase.typed, err)
			}
			if !testCase.proceeds && err == nil {
				t.Errorf("%q was accepted", testCase.typed)
			}
		})
	}
}

// TestYesSkipsTheQuestionEntirely pins that --yes does not read stdin.
//
// If it did, a scripted caller passing --yes with nothing on stdin would block
// on a CI runner, which is the whole failure this package exists to prevent.
func TestYesSkipsTheQuestionEntirely(t *testing.T) {
	allowPrompting(t)

	out := &bytes.Buffer{}
	err := ConfirmAction(Request{
		In:   iotest.ErrReader(errors.New("stdin must not be read when --yes is given")),
		Out:  out,
		Yes:  true,
		Flag: "--yes",
	}, "clear all GPG keys")

	if err != nil {
		t.Fatalf("--yes did not skip the question: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("a question was printed despite --yes: %q", out.String())
	}
}

// TestRequestForReadsTheNoInputFlag pins the wiring that was missing.
func TestRequestForReadsTheNoInputFlag(t *testing.T) {
	t.Parallel()

	command := &cobra.Command{Use: "x"}
	command.Flags().Bool(noInputFlag, false, "")

	if request := RequestFor(command, false); request.Disabled {
		t.Error("Disabled was set without --no-input")
	}

	if err := command.Flags().Set(noInputFlag, "true"); err != nil {
		t.Fatalf("set flag: %v", err)
	}
	request := RequestFor(command, true)
	if !request.Disabled {
		t.Error("--no-input was passed and Disabled is false; the flag is not wired")
	}
	if !request.MachineOutput {
		t.Error("MachineOutput was not carried through")
	}
}

// TestACommandWithoutTheFlagStillWorks covers the GetBool error path.
func TestACommandWithoutTheFlagStillWorks(t *testing.T) {
	t.Parallel()

	request := RequestFor(&cobra.Command{Use: "x"}, false)
	if request.Disabled {
		t.Error("Disabled was set for a command that has no --no-input flag")
	}
}

// TestFillMissingAsksForEachAbsentValue covers the prompting half.
//
// Under `go test` the gate always refuses, so without the decide seam every
// line below the refusal is unreachable -- and unreachable code in the one
// place that asks a person for input is exactly what should not go unchecked.
func TestFillMissingAsksForEachAbsentValue(t *testing.T) {
	allowPrompting(t)

	var fromRef, toRef, title string
	out := &bytes.Buffer{}

	err := FillMissing(Request{
		In:  strings.NewReader("feature\nmain\nMy change\n"),
		Out: out,
	}, []Missing{
		{Flag: "--from-ref", Question: "Source branch", Value: &fromRef},
		{Flag: "--to-ref", Question: "Target branch", Value: &toRef},
		{Flag: "--title", Question: "Title", Value: &title},
	})
	if err != nil {
		t.Fatalf("filling from answers failed: %v", err)
	}

	for _, got := range []struct{ name, value, want string }{
		{"from-ref", fromRef, "feature"},
		{"to-ref", toRef, "main"},
		{"title", title, "My change"},
	} {
		if got.value != got.want {
			t.Errorf("%s = %q, want %q", got.name, got.value, got.want)
		}
	}

	// Each question is asked, in order, naming what it wants.
	asked := out.String()
	for _, question := range []string{"Source branch", "Target branch", "Title"} {
		if !strings.Contains(asked, question) {
			t.Errorf("never asked for %q: %q", question, asked)
		}
	}
}

// TestFillMissingOnlyAsksForWhatIsAbsent guards against re-asking.
func TestFillMissingOnlyAsksForWhatIsAbsent(t *testing.T) {
	allowPrompting(t)

	supplied := "already given"
	var title string
	out := &bytes.Buffer{}

	if err := FillMissing(Request{In: strings.NewReader("My change\n"), Out: out}, []Missing{
		{Flag: "--from-ref", Question: "Source branch", Value: &supplied},
		{Flag: "--title", Question: "Title", Value: &title},
	}); err != nil {
		t.Fatalf("filling failed: %v", err)
	}

	if supplied != "already given" {
		t.Errorf("a supplied value was overwritten with %q", supplied)
	}
	if title != "My change" {
		t.Errorf("title = %q, want the typed answer", title)
	}
	if strings.Contains(out.String(), "Source branch") {
		t.Errorf("asked for a value that was already supplied: %q", out.String())
	}
}

// TestFillMissingRefusesAnEmptyAnswer is ADR-073's rule that refusing to ask is
// not permission to guess -- and neither is being answered with nothing.
func TestFillMissingRefusesAnEmptyAnswer(t *testing.T) {
	allowPrompting(t)

	var title string
	err := FillMissing(Request{In: strings.NewReader("\n"), Out: &bytes.Buffer{}}, []Missing{
		{Flag: "--title", Question: "Title", Value: &title},
	})
	if err == nil {
		t.Fatal("an empty answer was accepted as a value")
	}
	if !strings.Contains(err.Error(), "--title") {
		t.Errorf("error = %q, want it to name the flag", err.Error())
	}
	if title != "" {
		t.Errorf("title = %q, want it left alone", title)
	}
}

// TestFillMissingWithNothingAbsentAsksNothing covers the early return.
func TestFillMissingWithNothingAbsentAsksNothing(t *testing.T) {
	t.Parallel()

	value := "given"
	out := &bytes.Buffer{}

	// No seam: with nothing missing this must not reach the gate at all, or a
	// complete invocation would fail wherever nobody can answer.
	if err := FillMissing(Request{
		In:  iotest.ErrReader(errors.New("stdin must not be read when nothing is missing")),
		Out: out,
	}, []Missing{{Flag: "--title", Question: "Title", Value: &value}}); err != nil {
		t.Fatalf("a complete invocation was refused: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("a question was asked with nothing missing: %q", out.String())
	}
}

// TestReadsThatFailAreReported covers the error branches of both readers.
func TestReadsThatFailAreReported(t *testing.T) {
	allowPrompting(t)

	broken := func() io.Reader { return iotest.ErrReader(errors.New("stdin exploded")) }

	if err := confirmYesNo(broken(), &bytes.Buffer{}, "do the thing"); err == nil {
		t.Error("a failed read was treated as a yes")
	}
	if err := confirmDeletion(broken(), &bytes.Buffer{}, "PRJ/demo"); err == nil {
		t.Error("a failed read was treated as a confirmation")
	}

	var title string
	if err := FillMissing(Request{In: broken(), Out: &bytes.Buffer{}}, []Missing{
		{Flag: "--title", Question: "Title", Value: &title},
	}); err == nil {
		t.Error("a failed read was treated as an answer")
	}
}
