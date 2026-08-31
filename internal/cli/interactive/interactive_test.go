package interactive

import (
	"bytes"
	"os"
	"testing"
)

// env builds a Lookup from a map, so a test never needs t.Setenv and can
// therefore run in parallel.
func env(pairs map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, ok := pairs[name]
		return value, ok
	}
}

func TestDetectRefusesAndSaysWhy(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		options Options
		reason  string
	}{
		{
			name:    "the no-input flag",
			options: Options{Disabled: true},
			reason:  "--no-input was given",
		},
		{
			name:    "machine output",
			options: Options{MachineOutput: true},
			reason:  "machine-readable output was requested",
		},
		{
			name:    "the disable variable",
			options: Options{Lookup: env(map[string]string{"BB_NO_PROMPT": "1"})},
			reason:  "BB_NO_PROMPT is set",
		},
		{
			name:    "a hosted runner",
			options: Options{Lookup: env(map[string]string{"CI": "true"})},
			reason:  "CI is set",
		},
		{
			name:    "debian packaging",
			options: Options{Lookup: env(map[string]string{"DEBIAN_FRONTEND": "noninteractive"})},
			reason:  "DEBIAN_FRONTEND is set",
		},
		{
			name:    "homebrew",
			options: Options{Lookup: env(map[string]string{"NONINTERACTIVE": "1"})},
			reason:  "NONINTERACTIVE is set",
		},
		{
			name:    "a coding harness",
			options: Options{Lookup: env(map[string]string{"CLAUDECODE": "1"})},
			reason:  "CLAUDECODE is set",
		},
		{
			name:    "a terminal that says it cannot",
			options: Options{Lookup: env(map[string]string{"TERM": "dumb"}), Stdin: &bytes.Buffer{}, Stdout: &bytes.Buffer{}},
			reason:  "TERM is dumb",
		},
		{
			name:    "stdin is not a terminal",
			options: Options{Lookup: env(nil), Stdin: &bytes.Buffer{}, Stdout: os.Stdout},
			reason:  "standard input is not a terminal",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			decision := Detect(testCase.options)
			if decision.Allowed {
				t.Fatalf("prompting was permitted; expected a refusal because %s", testCase.reason)
			}
			if decision.Reason != testCase.reason {
				t.Errorf("reason = %q, want %q", decision.Reason, testCase.reason)
			}
		})
	}
}

// TestAnOperatorCanNameTheirOwnVariable is the extension point.
//
// A harness bb has never heard of is the expected case, not the exotic one, so
// this is the mechanism that has to work without waiting for a release.
func TestAnOperatorCanNameTheirOwnVariable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		vars   map[string]string
		reason string
		refuse bool
	}{
		{
			name:   "a single name",
			vars:   map[string]string{"BB_NO_PROMPT_VARS": "FUTURE_HARNESS", "FUTURE_HARNESS": "1"},
			reason: "FUTURE_HARNESS is set, named by BB_NO_PROMPT_VARS",
			refuse: true,
		},
		{
			name:   "several names, spaces tolerated",
			vars:   map[string]string{"BB_NO_PROMPT_VARS": "ONE, TWO , THREE", "TWO": "yes"},
			reason: "TWO is set, named by BB_NO_PROMPT_VARS",
			refuse: true,
		},
		{
			name:   "a name that is not set does not refuse",
			vars:   map[string]string{"BB_NO_PROMPT_VARS": "ABSENT"},
			refuse: false,
		},
		{
			name:   "a named variable set to false does not refuse",
			vars:   map[string]string{"BB_NO_PROMPT_VARS": "MAYBE", "MAYBE": "false"},
			refuse: false,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			// Streams are buffers, so a non-refusing case still stops at the
			// terminal check rather than reporting Allowed.
			decision := Detect(Options{
				Lookup: env(testCase.vars),
				Stdin:  &bytes.Buffer{},
				Stdout: &bytes.Buffer{},
			})

			if testCase.refuse {
				if decision.Reason != testCase.reason {
					t.Errorf("reason = %q, want %q", decision.Reason, testCase.reason)
				}
				return
			}
			if decision.Reason != "standard input is not a terminal" {
				t.Errorf("reason = %q; the extension list should not have refused", decision.Reason)
			}
		})
	}
}

// TestAVariableSetToNoDoesNotCount guards the case that would silence
// prompting for the one person who took the trouble to say otherwise.
func TestAVariableSetToNoDoesNotCount(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"", "0", "false", "FALSE", " false "} {
		t.Run("CI="+value, func(t *testing.T) {
			t.Parallel()

			decision := Detect(Options{
				Lookup: env(map[string]string{"CI": value}),
				Stdin:  &bytes.Buffer{},
				Stdout: &bytes.Buffer{},
			})
			if decision.Reason == "CI is set" {
				t.Errorf("CI=%q was read as a hosted runner", value)
			}
		})
	}
}

// TestEveryRuleCanOnlyRefuse is the property that makes this safe to extend.
//
// If a later rule could permit what an earlier one refused, adding one would
// widen the set of situations in which bb prompts, and reviewing a new rule
// would stop being a local question.
func TestEveryRuleCanOnlyRefuse(t *testing.T) {
	t.Parallel()

	everything := Options{
		Disabled:      true,
		MachineOutput: true,
		Lookup: env(map[string]string{
			"BB_NO_PROMPT":      "1",
			"BB_NO_PROMPT_VARS": "EXTRA",
			"EXTRA":             "1",
			"CI":                "true",
			"TERM":              "dumb",
		}),
		Stdin:  &bytes.Buffer{},
		Stdout: &bytes.Buffer{},
	}

	if decision := Detect(everything); decision.Allowed {
		t.Fatal("prompting was permitted with every refusal present")
	}

	// Dropping refusals one at a time must never permit prompting while any
	// refusal remains.
	stripped := everything
	stripped.Disabled = false
	if decision := Detect(stripped); decision.Allowed {
		t.Error("dropping --no-input permitted prompting while other refusals stood")
	}
	stripped.MachineOutput = false
	if decision := Detect(stripped); decision.Allowed {
		t.Error("dropping machine output permitted prompting while other refusals stood")
	}
}

// TestACleanEnvironmentReachesTheTerminalCheck proves the environment rules do
// not refuse on their own.
//
// The Allowed branch itself cannot be reached under `go test`, where stdin is
// never a terminal. What can be asserted is that a clean environment gets past
// every rule above the terminal check, which is what makes the check the
// deciding one for a real person at a real terminal.
func TestACleanEnvironmentReachesTheTerminalCheck(t *testing.T) {
	t.Parallel()

	decision := Detect(Options{
		Lookup: env(map[string]string{}),
		Stdin:  &bytes.Buffer{},
		Stdout: &bytes.Buffer{},
	})

	if decision.Reason != "standard input is not a terminal" {
		t.Errorf("stopped at %q; a clean environment should reach the terminal check", decision.Reason)
	}
}

// TestStdoutMustBeATerminalToo covers the branch the table above cannot reach.
//
// Under `go test` stdin is never a terminal, so Detect always refuses there
// first. Substituting the check is the only way to ask what happens when a
// keyboard is attached but the output is piped -- which is a real shape: a
// person running `bb ... | less` is not watching for a question.
//
// Not parallel: it replaces package state.
func TestStdoutMustBeATerminalToo(t *testing.T) {
	original := terminalCheck
	t.Cleanup(func() { terminalCheck = original })

	stdout := &bytes.Buffer{}
	terminalCheck = func(stream any) bool { return stream != any(stdout) }

	decision := Detect(Options{Lookup: env(nil), Stdin: os.Stdin, Stdout: stdout})
	if decision.Allowed {
		t.Fatal("prompting was permitted with output piped")
	}
	if decision.Reason != "standard output is not a terminal" {
		t.Errorf("reason = %q, want %q", decision.Reason, "standard output is not a terminal")
	}
}

// TestBothTerminalsPermitPrompting is the only path that returns Allowed.
//
// Not parallel: it replaces package state.
func TestBothTerminalsPermitPrompting(t *testing.T) {
	original := terminalCheck
	t.Cleanup(func() { terminalCheck = original })
	terminalCheck = func(any) bool { return true }

	decision := Detect(Options{Lookup: env(nil), Stdin: os.Stdin, Stdout: os.Stdout})
	if !decision.Allowed {
		t.Fatalf("prompting was refused because %q; a clean environment on two terminals should permit it", decision.Reason)
	}
}
