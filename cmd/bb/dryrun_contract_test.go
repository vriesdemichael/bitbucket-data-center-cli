package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli"
)

// TestEveryLeafCommandUnderDryRunAnswersInPreview walks the command tree with
// --dry-run --json and holds every answer to ADR-096: the flags choose the
// member, so it is preview, whatever the command found -- a verdict, a read's
// data, or a failure that is itself the verdict, exiting 0 because the verdict
// is in the document. A top-level error is only for no verdict at all, and for
// the one command that does not take the flag.
//
// With no configuration and no server, most commands stop at the first thing
// they need; that stop is a verdict too, since the real run would stop there.
func TestEveryLeafCommandUnderDryRunAnswersInPreview(t *testing.T) {
	sealEnvironment(t)

	commandsThatDoNotEmitJSON := exemptCommands(t)
	withoutDryRun := map[string]bool{"ai mcp serve": true}
	noVerdict := map[string]bool{"transient": true, "cancelled": true, "internal": true, "unknown_outcome": true}

	reported := 0

	for _, path := range leafCommandPaths(t) {
		if commandsThatDoNotEmitJSON[path] {
			continue
		}

		t.Run(path, func(t *testing.T) {
			t.Parallel()

			args := append([]string{"--dry-run", "--json", "--no-input"}, strings.Fields(path)...)
			stdout, exit := runForStdoutAndExit(args)

			var document map[string]json.RawMessage
			decoder := json.NewDecoder(bytes.NewReader(stdout))
			if err := decoder.Decode(&document); err != nil {
				t.Fatalf("wrote no JSON document under --dry-run --json: %v\n%s", err, stdout)
			}
			if decoder.More() {
				t.Errorf("wrote more than one document (ADR-075)\n%s", stdout)
			}

			var meta struct {
				Command string `json:"command"`
			}
			_ = json.Unmarshal(document["meta"], &meta)
			if meta.Command != path {
				t.Errorf("meta.command = %q, want %q", meta.Command, path)
			}

			if rawError, isError := document["error"]; isError {
				var failure struct {
					Kind string `json:"kind"`
				}
				_ = json.Unmarshal(rawError, &failure)
				if !withoutDryRun[path] && !noVerdict[failure.Kind] {
					t.Errorf("a %s failure is a verdict, and belongs in preview.error\n%s", failure.Kind, stdout)
				}
				if exit == 0 {
					t.Errorf("a top-level error exited 0\n%s", stdout)
				}
				return
			}

			if _, isPreview := document["preview"]; !isPreview || len(document) != 2 {
				t.Fatalf("members = %v, want preview and meta\n%s", keysOf(document), stdout)
			}
			if exit != 0 {
				t.Errorf("a preview exited %d; under --json the verdict is in the document, so it exits 0", exit)
			}
		})

		reported++
	}

	if reported < 200 {
		t.Fatalf("walked only %d commands; the tree walk has stopped reaching the tree", reported)
	}
}

// TestADryRunVerdictInTextExitsWithTheRealRunsCode: in text, the verdict goes
// to stdout and the exit code is the one the real run would have, so a script
// gating on `bb x --dry-run && bb x` stops at the check (ADR-096).
func TestADryRunVerdictInTextExitsWithTheRealRunsCode(t *testing.T) {
	t.Parallel()

	stdout, exit := runForStdoutAndExit([]string{"--dry-run", "--no-input", "pr", "merge"})
	if exit != 2 {
		t.Fatalf("exit = %d, want 2: the real run fails for the missing pull request id", exit)
	}
	for _, want := range []string{"Dry run: bb pr merge would fail", "pr merge takes <pr-id>"} {
		if !strings.Contains(string(stdout), want) {
			t.Fatalf("stdout lacks %q:\n%s", want, stdout)
		}
	}
}

// TestACommandWithoutDryRunRefusesTheFlagAsItself: --dry-run on a command that
// does not take it is an invalid invocation, not a verdict on a run that would
// never happen.
func TestACommandWithoutDryRunRefusesTheFlagAsItself(t *testing.T) {
	t.Parallel()

	stdout, exit := runForStdoutAndExit([]string{"--dry-run", "--json", "ai", "mcp", "serve"})
	if exit != 2 {
		t.Fatalf("exit = %d, want 2", exit)
	}

	var document map[string]json.RawMessage
	if err := json.Unmarshal(stdout, &document); err != nil {
		t.Fatalf("not one JSON document: %v\n%s", err, stdout)
	}
	if _, isError := document["error"]; !isError {
		t.Fatalf("members = %v, want a top-level error\n%s", keysOf(document), stdout)
	}
}

// runForStdoutAndExit runs bb through the real entry point and returns stdout
// and the exit code.
func runForStdoutAndExit(args []string) ([]byte, int) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	root := cli.NewRootCommand()
	root.SetArgs(args)
	root.SetErr(stderr)

	exit := executeRootCommand(root, args, stdout, stderr)

	return stdout.Bytes(), exit
}

func keysOf(document map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(document))
	for key := range document {
		keys = append(keys, key)
	}

	return keys
}
