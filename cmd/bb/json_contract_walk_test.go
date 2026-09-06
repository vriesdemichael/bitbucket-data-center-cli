package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/outputschemas"
)

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
// Two properties are checked: stdout under --json is a valid envelope, and
// it is exactly one document (ADR-075).
//
// What this does not cover is worth stating, because it was claimed once and
// was wrong. It does not catch the `bb bulk apply` double-write that ADR-075
// was written for: apply requires --from-plan, so it fails argument validation
// before Apply runs and never reaches the code that wrote two documents. That
// case is covered in internal/cli/cmd/bulk. What this catches is a command
// whose reachable path prints a payload and then fails -- the same shape,
// wherever it appears next.
func TestEveryLeafCommandUnderJSONWritesExactlyOneEnvelope(t *testing.T) {
	sealEnvironment(t)

	commandsThatDoNotEmitJSON := exemptCommands(t)

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

			// Checked structurally rather than by a tag. The envelope used to
			// carry a constant meta.contract saying "bb.machine", which made
			// this assertion easy and meaningless -- a constant proves only
			// that the constant was written. What identifies the document is
			// its shape: meta.bbVersion, and exactly one of data or error,
			// since which key is present is how a consumer tells success from
			// failure (ADR-046).
			var envelope map[string]json.RawMessage
			if err := decoder.Decode(&envelope); err != nil {
				t.Fatalf("wrote non-JSON to stdout under --json: %v\n%s", err, raw)
			}

			var meta struct {
				BBVersion string `json:"bbVersion"`
			}
			if rawMeta, present := envelope["meta"]; !present {
				t.Errorf("no meta on the envelope\n%s", raw)
			} else if err := json.Unmarshal(rawMeta, &meta); err != nil || meta.BBVersion == "" {
				t.Errorf("meta carries no bbVersion\n%s", raw)
			}

			_, hasData := envelope["data"]
			_, hasError := envelope["error"]
			if hasData == hasError {
				t.Errorf("the envelope carries %s data and error; which key is present is how success is told from failure\n%s",
					map[bool]string{true: "both", false: "neither"}[hasData], raw)
			}

			// Anything after the first document is a second document. A strict
			// decoder rejects the input outright; jq reads a value stream, so it
			// prints a result per document and exits 0 -- which is the quieter
			// and worse failure, because a script reading the last line gets the
			// wrong answer with no error.
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

// exemptCommands returns the one exemption list, imported rather than copied.
//
// It used to be a copy here, justified by a drift guard named in a comment that
// did not exist. It then briefly became an AST read of the internal/cli test
// file. Both were working around the list living somewhere this package could
// not import; moving it to outputschemas -- which needs it anyway, to count
// which commands owe a published schema -- makes the drift impossible instead
// of guarded.
func exemptCommands(t *testing.T) map[string]bool {
	t.Helper()

	exempt := map[string]bool{}
	for path, reason := range outputschemas.CommandsWithoutDataContract {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("%q is exempt with no reason; the reason is the part that gets reviewed", path)
		}
		exempt[path] = true
	}

	if len(exempt) == 0 {
		t.Fatal("the exemption list is empty; the walk would run commands meant to be excluded")
	}

	return exempt
}

// TestEveryExemptionIsAReachableLeaf checks the exemption list still describes
// the command tree.
//
// An exemption that no longer resolves silently excludes nothing, so the walk
// would keep passing while the command it was written for went unchecked --
// the ADR-039 failure with a different filename. This is the cmd/bb half; the
// internal/cli half additionally requires each entry to carry a reason.
func TestEveryExemptionIsAReachableLeaf(t *testing.T) {
	t.Parallel()

	for path := range exemptCommands(t) {
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

// TestTheExemptionListStaysSmallEnoughToReview bounds the escape hatch.
//
// Every entry removes a command from the walk, so a list that grew would
// quietly shrink what the contract covers. Five is not a meaningful ceiling in
// itself; it is low enough that crossing it is a conversation.
func TestTheExemptionListStaysSmallEnoughToReview(t *testing.T) {
	t.Parallel()

	exempt := exemptCommands(t)

	for _, path := range []string{"ai skill show", "api"} {
		if !exempt[path] {
			t.Errorf("the exemption for %q is gone; the walk now holds it to a contract it does not keep", path)
		}
	}
	// Cobra's help and completion are exempt too, and are not counted here:
	// the budget is on commands bb wrote and could have given a contract to.
	own := 0
	for path := range exempt {
		if outputschemas.CommandsCobraSupplies[path] {
			continue
		}
		own++
	}
	if own > 5 {
		t.Errorf("%d of bb's own commands are exempt; the list is meant to stay small enough to review", own)
	}

	for path := range outputschemas.CommandsCobraSupplies {
		if !exempt[path] {
			t.Errorf("%q is named as Cobra's but is not on the exemption list, so it excuses nothing", path)
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
