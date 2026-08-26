package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestAllRunnableCommandsDeclareArgsPolicy(t *testing.T) {
	root := NewRootCommand()
	var visit func(*cobra.Command)

	visit = func(cmd *cobra.Command) {
		if cmd.Hidden || cmd.Name() == "help" || cmd.Name() == "completion" {
			return
		}
		if cmd.Runnable() {
			if cmd.Args == nil {
				t.Errorf("Command %q is runnable but declares no Args policy (cmd.Args is nil). Every runnable command must explicitly declare its positional argument contract or be covered by enforceNoArgsDefaults.", cmd.CommandPath())
			}
		}
		for _, child := range cmd.Commands() {
			visit(child)
		}
	}
	visit(root)
}

func TestZeroArgCommandsRejectPositionalArguments(t *testing.T) {
	testCases := []struct {
		name string
		args []string
	}{
		{
			name: "auth logout rejects positional url",
			args: []string{"auth", "logout", "https://unexpected-host.example.com"},
		},
		{
			name: "auth status rejects positional arg",
			args: []string{"auth", "status", "extra-arg"},
		},
		{
			name: "auth setup-git rejects positional arg",
			args: []string{"auth", "setup-git", "extra-arg"},
		},
		{
			name: "pr list rejects positional arg",
			args: []string{"pr", "list", "extra-arg"},
		},
		{
			name: "repo list rejects positional arg",
			args: []string{"repo", "list", "extra-arg"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			root := NewRootCommand()
			buf := new(bytes.Buffer)
			root.SetOut(buf)
			root.SetErr(buf)
			root.SetArgs(tc.args)

			err := root.Execute()
			if err == nil {
				t.Fatalf("expected error executing %v, got nil", tc.args)
			}
			if !strings.Contains(err.Error(), "unknown command") && !strings.Contains(err.Error(), "accepts 0 arg(s)") {
				t.Errorf("expected arity error message for %v, got: %v", tc.args, err)
			}
		})
	}
}

func TestHasPositionalPlaceholder(t *testing.T) {
	testCases := []struct {
		use      string
		expected bool
	}{
		{"logout", false},
		{"get <id>", true},
		{"list [filter]", true},
		{"view", false},
		{"clone <repo> [dir]", true},
		{"status", false},
		{"cmd flag-only", false},
	}
	for _, tc := range testCases {
		if got := hasPositionalPlaceholder(tc.use); got != tc.expected {
			t.Errorf("hasPositionalPlaceholder(%q) = %v, want %v", tc.use, got, tc.expected)
		}
	}
}

func TestEnforceNoArgsDefaults(t *testing.T) {
	cmdWithPos := &cobra.Command{
		Use: "foo <bar>",
		Run: func(*cobra.Command, []string) {},
	}
	cmdZero := &cobra.Command{
		Use: "bar",
		Run: func(*cobra.Command, []string) {},
	}
	parent := &cobra.Command{Use: "root"}
	parent.AddCommand(cmdWithPos, cmdZero)
	enforceNoArgsDefaults(parent)

	if cmdZero.Args == nil {
		t.Errorf("expected cmdZero.Args to be set to NoArgs")
	}
	if cmdWithPos.Args != nil {
		t.Errorf("expected cmdWithPos.Args to remain nil")
	}
}
