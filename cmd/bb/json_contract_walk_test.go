package main

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli"
)

// exemptionSourceFile is the internal/cli file that owns the exemption list.
//
// The list is read out of it rather than copied here. A second copy is a second
// thing to keep right, and a path exempted here but not there would drop a
// command out of the walk with no reason recorded and nothing failing -- an
// earlier version of this file claimed a test guarded that, and no such test
// existed.
const exemptionSourceFile = "../../internal/cli/json_contract_test.go"

// exemptionSourcePath is resolved once, at init, because the walk changes the
// working directory to a sealed scratch directory before it reads anything and
// a relative path would then point nowhere.
var exemptionSourcePath = func() string {
	absolute, err := filepath.Abs(exemptionSourceFile)
	if err != nil {
		return exemptionSourceFile
	}
	return absolute
}()

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
// Two properties are checked: stdout under --json is valid bb.machine JSON, and
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

// exemptCommands reads the exemption list out of internal/cli.
//
// Reading it is what stops the walk and the list disagreeing. The alternative
// -- a copy here, plus a test that compares the two -- was what an earlier
// version of this file claimed to have and did not.
func exemptCommands(t *testing.T) map[string]bool {
	t.Helper()

	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, exemptionSourcePath, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s failed: %v", exemptionSourcePath, err)
	}

	exempt := map[string]bool{}

	ast.Inspect(file, func(node ast.Node) bool {
		value, ok := node.(*ast.ValueSpec)
		if !ok || len(value.Names) != 1 || value.Names[0].Name != "commandsThatDoNotEmitJSON" {
			return true
		}
		for _, expression := range value.Values {
			literal, ok := expression.(*ast.CompositeLit)
			if !ok {
				continue
			}
			for _, element := range literal.Elts {
				pair, ok := element.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key, ok := pair.Key.(*ast.BasicLit)
				if !ok || key.Kind != token.STRING {
					continue
				}
				exempt[strings.Trim(key.Value, `"`)] = true
			}
		}
		return false
	})

	if len(exempt) == 0 {
		t.Fatalf("found no exemptions in %s; the reader has stopped matching how the list is written, so the walk would run commands that are meant to be excluded", exemptionSourceFile)
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

// TestTheExemptionReaderFindsTheRealList is the sabotage, kept as a test
// (ADR-067).
//
// A reader that silently returns nothing would exempt nothing, which fails
// loudly; a reader that silently returned everything would exempt the whole
// tree and the walk would pass having checked nothing. Pin the contents.
func TestTheExemptionReaderFindsTheRealList(t *testing.T) {
	exempt := exemptCommands(t)

	for _, path := range []string{"ai skill show", "api"} {
		if !exempt[path] {
			t.Errorf("the reader missed the exemption for %q", path)
		}
	}
	if exempt["repo delete"] {
		t.Error("the reader invented an exemption; every command would be excluded from the walk")
	}
	if len(exempt) > 5 {
		t.Errorf("the reader found %d exemptions; the list is meant to stay small enough to review", len(exempt))
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
