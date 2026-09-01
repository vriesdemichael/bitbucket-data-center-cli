package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli"
)

// commandsThatDoNotEmitJSON mirrors the exemption list in internal/cli, for the
// two commands whose stdout is deliberately not an envelope.
//
// It is duplicated rather than exported because the list is a statement about
// the command surface, and the test that keeps it honest -- every name resolves
// to a real command, every entry carries a reason -- lives next to it there.
// TestTheTwoJSONExemptionListsAgree stops the copies drifting.
var commandsThatDoNotEmitJSON = map[string]bool{
	"ai skill show": true,
	"api":           true,
}

// TestEveryLeafCommandUnderJSONWritesExactlyOneEnvelope walks the whole command
// tree through the real entry point.
//
// It runs in cmd/bb rather than internal/cli because executeRootCommand is
// where the envelope contract is actually kept: a command that returns an error
// prints nothing itself, and cmd/bb writes the failure envelope for it. Driving
// the tree from internal/cli therefore watched 226 of 230 commands write
// nothing at all and assert nothing -- a guard that only covered the four
// commands reaching a success path without a server.
//
// Two properties are checked, and it is the second that ADR-075 turns on:
// stdout under --json is valid bb.machine JSON, and it is exactly one document.
// `bb bulk apply` printed its status and then returned an error, so cmd/bb
// appended a second envelope after it and `| jq` failed on the second -- which
// is the parse failure #474 was filed about.
func TestEveryLeafCommandUnderJSONWritesExactlyOneEnvelope(t *testing.T) {
	sealEnvironment(t)

	reported := 0

	for _, path := range leafCommandPaths(t) {
		if commandsThatDoNotEmitJSON[path] {
			continue
		}

		t.Run(path, func(t *testing.T) {
			sealEnvironment(t)

			stdout := &bytes.Buffer{}
			stderr := &bytes.Buffer{}
			args := append([]string{"--json", "--no-input"}, strings.Fields(path)...)

			root := cli.NewRootCommand()
			// executeRootCommand calls Execute(), which reads os.Args unless
			// the command has been given its own.
			root.SetArgs(args)
			root.SetErr(stderr)

			// The exit code is the command's business; stdout is ours.
			_ = executeRootCommand(root, args, stdout, stderr)

			raw := bytes.TrimSpace(stdout.Bytes())
			if len(raw) == 0 {
				t.Fatal("wrote nothing to stdout under --json; a failure that leaves stdout empty is indistinguishable from malformed output (ADR-046)")
			}

			decoder := json.NewDecoder(bytes.NewReader(raw))

			var envelope struct {
				Meta struct {
					Contract string `json:"contract"`
				} `json:"meta"`
			}
			if err := decoder.Decode(&envelope); err != nil {
				t.Fatalf("wrote non-JSON to stdout under --json: %v\n%s", err, raw)
			}
			if envelope.Meta.Contract != "bb.machine" {
				t.Errorf("contract = %q, want bb.machine\n%s", envelope.Meta.Contract, raw)
			}

			// Anything after the first document is a second document, which is
			// what breaks `| jq`.
			if decoder.More() {
				var trailing json.RawMessage
				_ = decoder.Decode(&trailing)
				t.Errorf("wrote more than one JSON document to stdout (ADR-075); the second begins:\n%s", trailing)
			}
		})

		reported++
	}

	// The count is asserted so the walk cannot quietly stop reaching the tree.
	if reported < 200 {
		t.Errorf("only %d commands were checked; the walk has stopped covering the surface", reported)
	}
}

// TestEveryWalkExemptionIsAReachableLeaf checks this copy of the exemption
// list still describes the command tree.
//
// An exemption that no longer resolves silently excludes nothing, so the walk
// would keep passing while the command it was written for went unchecked --
// the ADR-039 failure with a different filename. The reasons for the two
// entries live with the copy in internal/cli, next to the test that requires
// each one to carry a reason.
func TestEveryWalkExemptionIsAReachableLeaf(t *testing.T) {
	for path := range commandsThatDoNotEmitJSON {
		leaf := findLeaf(t, path)
		if leaf == nil {
			t.Errorf("%q is exempt but is not a command", path)
			continue
		}
		if !leaf.Runnable() {
			t.Errorf("%q is exempt but is not runnable, so the walk never reached it anyway", path)
		}
	}
}

// sealEnvironment points every path the CLI reads at a temporary directory, so
// invoking the tree cannot see or touch the developer's real configuration.
//
// It does not stop network access -- the repo-wide transport guard does that,
// reporting "external network access is disabled during tests". Most commands
// never get that far, because with no configuration there is no host and client
// construction fails first, but `bb update` reaches the transport and is
// stopped there rather than here.
func sealEnvironment(t *testing.T) {
	t.Helper()

	directory := scratchDirectory(t)
	t.Chdir(directory)
	t.Setenv("BB_CONFIG_PATH", directory+"/config.yaml")
	t.Setenv("HOME", directory)
	t.Setenv("USERPROFILE", directory)
	t.Setenv("XDG_CONFIG_HOME", directory)
	t.Setenv("APPDATA", directory)
	t.Setenv("BB_BULK_STATUS_DIR", directory+"/bulk")

	t.Setenv("BB_URL", "")
	t.Setenv("BB_TOKEN", "")
	t.Setenv("BB_NO_PROMPT", "1")

	// Diagnostics would otherwise write to stderr on every failure, and every
	// command in this walk fails.
	t.Setenv("BB_LOG_LEVEL", "")
	t.Setenv("BB_LOG_FORMAT", "")
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
	walk(cli.NewRootCommand(), nil)

	if len(paths) < 200 {
		t.Fatalf("found %d leaf commands; the walk is not reaching the tree", len(paths))
	}

	return paths
}

// findLeaf resolves a space-separated command path against a fresh tree.
func findLeaf(t *testing.T, path string) *cobra.Command {
	t.Helper()

	current := cli.NewRootCommand()
	for _, name := range strings.Fields(path) {
		var next *cobra.Command
		for _, candidate := range current.Commands() {
			if candidate.Name() == name {
				next = candidate
				break
			}
		}
		if next == nil {
			return nil
		}
		current = next
	}
	return current
}

// scratchDirectory is t.TempDir with best-effort removal.
//
// t.TempDir fails the test when it cannot delete the directory afterwards,
// which on Windows `bb update` reliably triggers: it opens a file under the
// sealed home and the handle outlives the command, so RemoveAll reports "being
// used by another process". That is the harness cleaning up after itself, not
// the command breaking its output contract, and failing the walk for it would
// teach the next reader to ignore the walk.
//
// Removal is registered before the caller changes directory, so it runs after
// the working directory has been restored -- a directory cannot be removed
// while it is the current one.
func scratchDirectory(t *testing.T) string {
	t.Helper()

	directory, err := os.MkdirTemp("", "bb-json-walk-")
	if err != nil {
		t.Fatalf("creating a scratch directory failed: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })

	return directory
}
