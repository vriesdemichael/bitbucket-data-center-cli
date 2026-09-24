package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestEveryCommandSaysWhatDryRunDoesForIt: the flag is global and its meaning
// is not, so every runnable command's help carries its own line, derived from
// its classification and tier rather than written beside it (ADR-096).
func TestEveryCommandSaysWhatDryRunDoesForIt(t *testing.T) {
	t.Parallel()

	root := NewRootCommand()
	counted := 0

	var visit func(*cobra.Command)
	visit = func(cmd *cobra.Command) {
		for _, child := range cmd.Commands() {
			visit(child)
		}
		if !cmd.Runnable() || cmd.Hidden || cmd.Name() == "help" {
			return
		}

		counted++
		line := dryRunHelpLine(cmd)
		if strings.TrimSpace(line) == "" {
			t.Errorf("%s has no --dry-run help line", dryRunCommandPath(cmd))
			return
		}

		var help bytes.Buffer
		cmd.SetOut(&help)
		if err := cmd.Usage(); err != nil {
			t.Errorf("%s: usage: %v", dryRunCommandPath(cmd), err)
			return
		}
		if !strings.Contains(help.String(), "Dry run:\n  "+line) {
			t.Errorf("%s: help lacks its Dry run section:\n%s", dryRunCommandPath(cmd), help.String())
		}
	}
	visit(root)

	if counted < 200 {
		t.Fatalf("checked only %d commands; the walk has stopped reaching the tree", counted)
	}
}

// TestDryRunHelpLinesFollowTheClassification pins one line per class, so the
// wording a person reads matches what the flag does.
func TestDryRunHelpLinesFollowTheClassification(t *testing.T) {
	t.Parallel()

	root := NewRootCommand()
	for path, want := range map[string]string{
		"pr list":           "Runs as usual",
		"auth logout":       "remove stored credentials",
		"ai mcp serve":      "Not accepted",
		"auth token create": "nothing is checked first (predicted)",
		"update":            "without installing it",
	} {
		cmd, _, err := root.Find(strings.Fields(path))
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if line := dryRunHelpLine(cmd); !strings.Contains(line, want) {
			t.Errorf("%s: help line %q lacks %q", path, line, want)
		}
	}
}

// TestTheDryRunTopicIsAHelpTopic: bb help dry-run explains the answer once,
// for every command's line to point at.
func TestTheDryRunTopicIsAHelpTopic(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&out)
	root.SetArgs([]string{"help", "dry-run"})
	if err := root.Execute(); err != nil {
		t.Fatalf("bb help dry-run: %v", err)
	}
	for _, want := range []string{"would-apply", "preconditions-checked", "stops at the check", "preview"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("the topic lacks %q:\n%s", want, out.String())
		}
	}
}
