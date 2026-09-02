package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/outputschemas"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/result"
)

// TestEveryDeclaredResultNamesARealCommand keeps the declarations honest.
//
// A declaration is keyed by command path, so a typo or a renamed command leaves
// a schema describing nothing while `--describe` quietly falls back to "not
// described yet". That is the failure two published schemas already had: they
// described `bb branch get-default`, which has never existed, and were served
// for months because nothing compared the name to the tree.
func TestEveryDeclaredResultNamesARealCommand(t *testing.T) {
	t.Parallel()

	root := NewRootCommand()

	for _, path := range result.DeclaredPaths() {
		command := findCommandByPath(root, path)
		if command == nil {
			t.Errorf("a result is declared for %q, which is not a command", path)
			continue
		}
		if !command.Runnable() {
			t.Errorf("a result is declared for %q, which is a group rather than a command", path)
		}
	}
}

// TestDeclaredResultsAreReachableThroughDescribe checks the wiring end to end.
//
// Declaring a schema and having --describe answer with it are two different
// things, and the second is the one a caller experiences.
func TestDeclaredResultsAreReachableThroughDescribe(t *testing.T) {
	t.Parallel()

	declared := result.DeclaredPaths()
	if len(declared) == 0 {
		t.Fatal("no results are declared; the mechanism is wired to nothing")
	}

	for _, path := range declared {
		described := describeCommand(path)
		if !described.Described {
			t.Errorf("%q has a declared result but --describe reports it undescribed: %s", path, described.Reason)
		}
		if len(described.Schema) == 0 {
			t.Errorf("%q described with an empty schema", path)
		}
	}
}

// TestEveryCommandIsModelled is the gate this whole change exists to pass.
//
// Every runnable command must answer --describe with something true: a schema
// derived from the result type it fills in, a hand-written schema for the bulk
// artifacts, or a stated reason it has no data payload at all. Nothing may
// simply have been forgotten -- that was the state this replaced, where most of
// the surface published no contract and nothing said so.
//
// A new command fails this test until its author decides which of the three it
// is. That decision is the point.
func TestEveryCommandIsModelled(t *testing.T) {
	t.Parallel()

	declared := map[string]bool{}
	for _, path := range result.DeclaredPaths() {
		declared[path] = true
	}
	published := outputschemas.Schemas()

	root := NewRootCommand()

	var walk func(command *cobra.Command)
	walk = func(command *cobra.Command) {
		if command.Runnable() && command != root {
			path := commandPathWithoutRoot(command)
			switch {
			case declared[path]:
			case outputschemas.CommandsWithoutDataContract[path] != "":
			case outputschemas.CommandsWithoutDeclarableShape[path] != "":
			case published["output."+strings.ReplaceAll(path, " ", ".")+".schema.json"] != nil:
			default:
				t.Errorf("%q neither declares a result type nor says why it has none", path)
			}
		}
		for _, child := range command.Commands() {
			walk(child)
		}
	}
	walk(root)
}

// TestUnmodelledCommandsSaySoRatherThanGuessing covers the third answer.
//
// A command whose payload has no shape bb can promise must say that, rather than
// reporting an empty schema that looks like a guarantee.
func TestUnmodelledCommandsSaySoRatherThanGuessing(t *testing.T) {
	t.Parallel()

	described := describeCommand("webhook test")
	if described.Described {
		t.Fatalf("webhook test has no data payload but reported a schema: %+v", described)
	}
	if !strings.Contains(described.Reason, "no shape bb can promise") {
		t.Errorf("reason = %q, want it to say the payload has no shape bb can promise", described.Reason)
	}
}
