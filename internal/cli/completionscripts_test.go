package cli

import (
	"bytes"
	"strings"
	"testing"
)

// TestThePowerShellScriptCompletesNothingWithoutThrowing covers the one line
// bb rewrites in a script Cobra generates.
//
// Measured in PowerShell 7.6, against the unpatched script: asking for a
// completion that has nothing to offer -- `bb pr merge <tab>` in a repository
// with no open pull requests -- raised "Cannot process argument because the
// value of argument completionText is null" at the prompt, because the script
// returns a bare empty string and PowerShell will not make a CompletionResult
// from one. With the rewrite it returns the word being completed, and the same
// press does nothing at all.
func TestThePowerShellScriptCompletesNothingWithoutThrowing(t *testing.T) {
	t.Parallel()

	command := NewRootCommand()
	script := &bytes.Buffer{}
	command.SetOut(script)
	command.SetErr(script)
	command.SetArgs([]string{"completion", "powershell"})

	if err := command.Execute(); err != nil {
		t.Fatalf("generating the PowerShell completion script failed: %v", err)
	}

	generated := script.String()
	if strings.Contains(generated, `            ""`+"\n            return") {
		t.Error("the generated script still returns a bare empty string, which PowerShell rejects")
	}
	if !strings.Contains(generated, "$WordToComplete -ne ''") {
		t.Error("the generated script does not carry bb's empty-completion correction")
	}
}

// TestTheCobraScriptIsCheckedBeforeItIsRewritten is the half that matters when
// the dependency moves.
//
// A rewrite that silently stops matching would leave every PowerShell user
// with the broken script and nothing to say so. Finding no anchor is an error,
// not a pass-through.
//
// Sabotage that proved it guards: changing one character of
// brokenEmptyCompletion makes `bb completion powershell` fail with this
// message instead of printing a script.
func TestTheCobraScriptIsCheckedBeforeItIsRewritten(t *testing.T) {
	t.Parallel()

	if _, err := withWorkingEmptyCompletion("a script that has moved on"); err == nil {
		t.Fatal("rewriting a script without the expected branch returned no error")
	}
}

// TestTheFishScriptKeepsTheCursorAgainstAValue covers the other line bb
// rewrites.
//
// Found by driving fish for real (scripts/completion-shells): `bb ai mcp serve
// --tools list_t<tab>` left "list_tags " with a space, because Cobra's check
// for when to keep the cursor against a value read the last character of the
// description as well as the value, and "List tags in a repository." ends in a
// full stop.
func TestTheFishScriptKeepsTheCursorAgainstAValue(t *testing.T) {
	t.Parallel()

	command := NewRootCommand()
	script := &bytes.Buffer{}
	command.SetOut(script)
	command.SetErr(script)
	command.SetArgs([]string{"completion", "fish"})

	if err := command.Execute(); err != nil {
		t.Fatalf("generating the fish completion script failed: %v", err)
	}

	if strings.Contains(script.String(), brokenFishNoSpace) {
		t.Error("the generated fish script still reads the description's last character")
	}
	if !strings.Contains(script.String(), workingFishNoSpace) {
		t.Error("the generated fish script does not carry bb's no-space correction")
	}

	if _, err := withWorkingFishNoSpace("a script that has moved on"); err == nil {
		t.Error("rewriting a fish script without the expected line returned no error")
	}
}

// TestEveryShellScriptGoesToTheCallersWriter guards the reason bb replaces the
// body of all four generators rather than only PowerShell's.
//
// Cobra builds these commands with the writer the root had at the time, so a
// caller that sets one afterwards -- every test here, and cmd/bb, which wraps
// stdout to notice a failed write -- was not the one being written to.
func TestEveryShellScriptGoesToTheCallersWriter(t *testing.T) {
	t.Parallel()

	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		t.Run(shell, func(t *testing.T) {
			t.Parallel()

			command := NewRootCommand()
			script := &bytes.Buffer{}
			command.SetOut(script)
			command.SetErr(script)
			command.SetArgs([]string{"completion", shell})

			if err := command.Execute(); err != nil {
				t.Fatalf("generating the %s completion script failed: %v", shell, err)
			}

			if !strings.Contains(script.String(), "__bb_") && !strings.Contains(script.String(), "__bb") {
				t.Errorf("the %s script did not reach the writer the caller set: %q", shell, truncate(script.String()))
			}
		})
	}
}

// TestAScriptWithoutDescriptionsIsStillGenerated covers the flag Cobra puts on
// each of these commands.
//
// --no-descriptions asks for a different generator, not a different rendering,
// so replacing the body of these commands has to honour it -- otherwise the
// flag would parse, print a script, and be ignored.
func TestAScriptWithoutDescriptionsIsStillGenerated(t *testing.T) {
	t.Parallel()

	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		t.Run(shell, func(t *testing.T) {
			t.Parallel()

			command := NewRootCommand()
			script := &bytes.Buffer{}
			command.SetOut(script)
			command.SetErr(script)
			command.SetArgs([]string{"completion", shell, "--no-descriptions"})

			if err := command.Execute(); err != nil {
				t.Fatalf("generating the %s completion script without descriptions failed: %v", shell, err)
			}

			// __completeNoDesc is the request a script makes when it was asked
			// for no descriptions, and the one thing that says which generator
			// ran.
			if !strings.Contains(script.String(), "__completeNoDesc") {
				t.Errorf("the %s script still asks for descriptions: %q", shell, truncate(script.String()))
			}
		})
	}
}

func truncate(value string) string {
	if len(value) <= 120 {
		return value
	}

	return value[:120] + "…"
}
