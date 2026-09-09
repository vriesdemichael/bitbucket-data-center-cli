package cli

import (
	"strings"
	"testing"
)

// The gh spellings resolve to the commands they are meant to reach.
//
// Not an assertion that two paths print the same bytes: view, edit and close
// are Cobra aliases on one command, so equal output is true by construction and
// a test of it would pass for as long as the alias existed and prove nothing
// else. What can actually break is the wiring -- an alias dropped in a rename,
// or a canonical verb renamed out from under one -- so that is what is checked
// (ADR-050, ADR-067).
func TestGhSpellingsResolveToTheirCanonicalCommands(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		gh        []string
		canonical string
	}{
		{gh: []string{"pr", "view"}, canonical: "get"},
		{gh: []string{"pr", "edit"}, canonical: "update"},
		{gh: []string{"pr", "close"}, canonical: "decline"},
		{gh: []string{"pr", "checks"}, canonical: "checks"},
	}

	for _, testCase := range testCases {
		t.Run(strings.Join(testCase.gh, " "), func(t *testing.T) {
			t.Parallel()

			root := NewRootCommand()
			found, _, err := root.Find(testCase.gh)
			if err != nil {
				t.Fatalf("%v does not resolve: %v", testCase.gh, err)
			}
			if got := found.Name(); got != testCase.canonical {
				t.Fatalf("%v resolved to %q, want %q", testCase.gh, got, testCase.canonical)
			}
		})
	}
}

// bb pr checks is a second registration of the build status command, so it must
// carry its own flags rather than share them with bb pr build status. One
// *cobra.Command under two parents would pass a resolution test and then have
// the two spellings overwrite each other's --limit.
func TestChecksAndBuildStatusDoNotShareFlagState(t *testing.T) {
	t.Parallel()

	root := NewRootCommand()

	checks, _, err := root.Find([]string{"pr", "checks"})
	if err != nil {
		t.Fatalf("pr checks does not resolve: %v", err)
	}
	buildStatus, _, err := root.Find([]string{"pr", "build", "status"})
	if err != nil {
		t.Fatalf("pr build status does not resolve: %v", err)
	}

	if checks == buildStatus {
		t.Fatal("pr checks and pr build status are the same command value; each registration needs its own")
	}

	if err := checks.Flags().Set("limit", "7"); err != nil {
		t.Fatalf("set --limit on pr checks: %v", err)
	}
	if got := buildStatus.Flags().Lookup("limit").Value.String(); got == "7" {
		t.Fatal("setting --limit on pr checks changed pr build status; the two share flag state")
	}
}

// UnknownSubcommandError answers only for the case it exists for. Everything
// else has to come back nil, because it runs after every successful Execute and
// a false positive there would fail a command that worked.
func TestUnknownSubcommandErrorIgnoresEverythingElse(t *testing.T) {
	t.Parallel()

	t.Run("nil root", func(t *testing.T) {
		t.Parallel()

		if err := UnknownSubcommandError(nil, []string{"pr", "bogus"}); err != nil {
			t.Fatalf("expected nil for a nil root, got %v", err)
		}
	})

	testCases := []struct {
		name string
		args []string
	}{
		{name: "group with no arguments", args: []string{"pr"}},
		{name: "runnable command with its argument", args: []string{"pr", "get", "42"}},
		{name: "runnable command at the top level", args: []string{"browse"}},
		{name: "nothing at all", args: nil},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			root := NewRootCommand()
			root.SetArgs(testCase.args)
			// Execute so the flags are parsed, which is the state the check reads.
			_ = root.Execute()

			if err := UnknownSubcommandError(root, testCase.args); err != nil {
				t.Fatalf("expected nil for %v, got %v", testCase.args, err)
			}
		})
	}
}
