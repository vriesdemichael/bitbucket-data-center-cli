package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli"
)

// groupPaths returns every command that exists only to hold subcommands.
func groupPaths(root *cobra.Command) []string {
	var paths []string

	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		if !cmd.Runnable() && cmd.HasSubCommands() && cmd != root {
			path := strings.TrimPrefix(cmd.CommandPath(), root.Name()+" ")
			paths = append(paths, path)
		}
		for _, child := range cmd.Commands() {
			walk(child)
		}
	}
	walk(root)

	return paths
}

// A group handed something that is not one of its subcommands fails, and says so
// on the stream the caller is reading.
//
// Cobra answers any command with no RunE by printing help and returning nil, so
// this exited 0 in 64 of 65 groups -- and under --json a caller reading stdout
// got help prose rather than an envelope. The v4 release notes promised exit 2
// for invalid arguments, and a misspelled subcommand is the mistake a pipeline
// or an agent is most likely to make.
func TestUnknownSubcommandFailsInEveryGroup(t *testing.T) {
	t.Parallel()

	paths := groupPaths(cli.NewRootCommand())
	if len(paths) < 50 {
		t.Fatalf("found only %d command groups; the walk is not seeing the tree", len(paths))
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			stdout := &bytes.Buffer{}
			stderr := &bytes.Buffer{}
			args := append([]string{"--json", "--no-input"}, strings.Fields(path)...)
			args = append(args, "definitely-not-a-subcommand")

			root := cli.NewRootCommand()
			root.SetArgs(args)
			root.SetErr(stderr)

			if code := executeRootCommand(root, args, stdout, stderr); code != 2 {
				t.Fatalf("exit code %d, want 2 (validation)", code)
			}

			var envelope struct {
				Error *struct {
					Kind    string `json:"kind"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &envelope); err != nil {
				t.Fatalf("stdout is not an envelope under --json: %v\nstdout: %s", err, stdout.String())
			}
			if envelope.Error == nil || envelope.Error.Kind != "validation" {
				t.Fatalf("expected a validation error envelope, got: %s", stdout.String())
			}
			if !strings.Contains(envelope.Error.Message, "definitely-not-a-subcommand") {
				t.Fatalf("the message does not name the unknown command: %s", envelope.Error.Message)
			}
		})
	}
}

// A group with no arguments still prints its help and succeeds. That is how the
// command tree is browsed, so the fix above must not reach it.
func TestGroupWithNoArgumentsStillPrintsHelp(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"pr", "repo", "auth", "auth server"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			stdout := &bytes.Buffer{}
			stderr := &bytes.Buffer{}
			args := strings.Fields(path)

			root := cli.NewRootCommand()
			root.SetArgs(args)
			root.SetErr(stderr)

			if code := executeRootCommand(root, args, stdout, stderr); code != 0 {
				t.Fatalf("exit code %d, want 0", code)
			}
			if !strings.Contains(stdout.String(), "Usage:") {
				t.Fatalf("expected help on stdout, got: %s", stdout.String())
			}
		})
	}
}
