package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestEveryLeafCommandUnderJSONWritesAnEnvelopeOrNothing walks the whole
// command tree and holds each leaf to the ADR-046 contract.
//
// It invokes every leaf with --json and no arguments, in a sealed environment:
// no configuration, so no host, so client construction fails before anything
// reaches the network. What each command does is therefore uninteresting --
// most report a validation or configuration failure. What is interesting is
// the shape of stdout, which under --json must be a bb.machine envelope or
// empty, never an English sentence.
//
// That is the property that was missed: `ai skill install` and `remove` printed
// "Skill installed: <path>" under --json, and 229 of 233 commands being correct
// is exactly the situation in which nobody looks.
func TestEveryLeafCommandUnderJSONWritesAnEnvelopeOrNothing(t *testing.T) {
	sealEnvironment(t)

	for _, path := range leafCommandPaths(t) {
		if _, exempt := commandsThatDoNotEmitJSON[path]; exempt {
			continue
		}

		t.Run(path, func(t *testing.T) {
			sealEnvironment(t)

			root := NewRootCommand()
			out := &bytes.Buffer{}
			root.SetOut(out)
			root.SetErr(&bytes.Buffer{})
			root.SetArgs(append([]string{"--json", "--no-input"}, strings.Fields(path)...))

			// The error is the command's business; stdout is ours.
			_ = root.Execute()

			stdout := bytes.TrimSpace(out.Bytes())
			if len(stdout) == 0 {
				return
			}

			var envelope struct {
				Meta struct {
					Contract string `json:"contract"`
				} `json:"meta"`
			}
			if err := json.Unmarshal(stdout, &envelope); err != nil {
				t.Fatalf("wrote non-JSON to stdout under --json: %v\n%s", err, stdout)
			}
			if envelope.Meta.Contract != "bb.machine" {
				t.Errorf("contract = %q, want bb.machine\n%s", envelope.Meta.Contract, stdout)
			}
		})
	}
}

// sealEnvironment points every path the CLI reads at a temporary directory, so
// invoking the tree cannot see or touch the developer's real configuration.
func sealEnvironment(t *testing.T) {
	t.Helper()

	directory := t.TempDir()
	t.Chdir(directory)
	t.Setenv("BB_CONFIG_PATH", directory+"/config.yaml")
	t.Setenv("HOME", directory)
	t.Setenv("USERPROFILE", directory)
	t.Setenv("XDG_CONFIG_HOME", directory)
	t.Setenv("APPDATA", directory)
	t.Setenv("BB_BULK_STATUS_DIR", directory+"/bulk")

	// Nothing may reach a server, and nothing may wait on a human.
	t.Setenv("BB_URL", "")
	t.Setenv("BB_TOKEN", "")
	t.Setenv("BB_NO_PROMPT", "1")
}

// leafCommandPaths returns the space-separated path of every command that does
// work, which is every command with a Run and no children.
func leafCommandPaths(t *testing.T) []string {
	t.Helper()

	var paths []string

	var walk func(command *cobra.Command, prefix []string)
	walk = func(command *cobra.Command, prefix []string) {
		children := command.Commands()
		if len(children) == 0 {
			if command.Runnable() {
				paths = append(paths, strings.Join(prefix, " "))
			}
			return
		}
		for _, child := range children {
			// help and completion are injected by Cobra rather than written
			// here, so they are not part of the surface this project documents.
			if child.Name() == "help" || child.Name() == "completion" {
				continue
			}
			walk(child, append(append([]string(nil), prefix...), child.Name()))
		}
	}
	walk(NewRootCommand(), nil)

	if len(paths) < 100 {
		t.Fatalf("found %d leaf commands; the walk is not reaching the tree", len(paths))
	}

	return paths
}
