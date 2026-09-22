package completion

import (
	"testing"

	"github.com/spf13/pflag"
)

// What the two tables promise, at their edges.
//
// The governance test in internal/cli walks the real tree and fails on a slot
// no table covers. This covers the answers that tree does not contain: a name
// nothing declares, and the exception that overrides a name declared
// everywhere else.

func TestDeclaredPositionalKnowsWhatItDoesNot(t *testing.T) {
	t.Parallel()

	if _, declared := DeclaredPositional("some command", "<invented>"); declared {
		t.Error("a placeholder no table covers reported itself declared, which is how a slot completes nothing forever")
	}

	kind, declared := DeclaredPositional("pr merge", "<pr-id>")
	if !declared || kind != KindPullRequest {
		t.Errorf("expected a pull request, got %q declared=%v", kind, declared)
	}

	// The exception table wins over the name, which is the whole reason it
	// exists: `project create` names a project that does not exist yet.
	kind, declared = DeclaredPositional("project create", "<project-key>")
	if !declared || kind != KindFree {
		t.Errorf("expected the key being created to complete nothing, got %q declared=%v", kind, declared)
	}
}

func TestDeclaredFlagKnowsWhatItDoesNot(t *testing.T) {
	t.Parallel()

	if _, declared := DeclaredFlag("some command", nil); declared {
		t.Error("a flag that does not exist reported itself declared")
	}

	set := pflag.NewFlagSet("probe", pflag.ContinueOnError)
	set.String("invented", "", "a flag no table covers")
	set.Bool("switch", false, "a flag that takes no value")
	set.String("repo", "", "a repository selector")

	if _, declared := DeclaredFlag("some command", set.Lookup("invented")); declared {
		t.Error("a flag name no table covers reported itself declared")
	}

	// A boolean takes no value, so there is nothing to complete and nothing
	// for either table to say about it.
	if kind, declared := DeclaredFlag("some command", set.Lookup("switch")); !declared || kind != KindFree {
		t.Errorf("expected a boolean to be declared as nothing to complete, got %q declared=%v", kind, declared)
	}

	if kind, declared := DeclaredFlag("pr merge", set.Lookup("repo")); !declared || kind != KindRepository {
		t.Errorf("expected --repo to be a repository wherever it is declared, got %q declared=%v", kind, declared)
	}
}
