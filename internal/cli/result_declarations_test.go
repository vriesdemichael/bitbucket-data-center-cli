package cli

import (
	"strings"
	"testing"

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

// TestTagIsFullyModelled pins the pilot.
//
// tag was converted first because all four of its commands are small and three
// already returned a typed value, so a mismatch would be the mechanism's fault
// rather than the command's. Losing one of these means the conversion regressed.
func TestTagIsFullyModelled(t *testing.T) {
	t.Parallel()

	declared := map[string]bool{}
	for _, path := range result.DeclaredPaths() {
		declared[path] = true
	}

	for _, path := range []string{"tag list", "tag view", "tag create", "tag delete"} {
		if !declared[path] {
			t.Errorf("%q no longer declares a result type", path)
		}
	}
}

// TestUnmodelledCommandsSaySoRatherThanGuessing covers the other side of the
// migration: a command that has not been converted yet must answer honestly.
func TestUnmodelledCommandsSaySoRatherThanGuessing(t *testing.T) {
	t.Parallel()

	described := describeCommand("repo create")
	if described.Described {
		t.Fatalf("repo create is not modelled but reported a schema: %+v", described)
	}
	if !strings.Contains(described.Reason, "no output schema") {
		t.Errorf("reason = %q, want it to say no schema is published yet", described.Reason)
	}
}
