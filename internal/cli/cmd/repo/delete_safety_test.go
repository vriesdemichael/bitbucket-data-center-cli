package repocmd

import (
	"strings"
	"testing"
)

// TestRepoDeleteWillNotActOnAnInferredTarget is issue #472.
//
// bb repo delete took no positional argument, had no --yes, and let
// applyInferredRepositoryContext fill the target from the git remote. Typed
// with no arguments inside a checkout, it deleted that repository. ADR-054
// used this very command as its example of one made safe by explicit flags,
// target arguments or --dry-run, and it had none of the three.
//
// The rule that replaces it (ADR-073): the target must be named, and --yes
// does nothing when it is not.
func TestRepoDeleteWillNotActOnAnInferredTarget(t *testing.T) {
	t.Parallel()

	// The dangerous shape: a target is available without the caller naming it.
	// This is what applyInferredRepositoryContext produces from a git remote,
	// and it is indistinguishable from an operator having configured one.
	setup := testSetup{Host: "https://bitbucket.example.com", Token: "token", ProjectKey: "PRJ", RepoSlug: "repo"}

	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "no target and no confirmation",
			args: []string{"repo", "delete"},
			want: "--yes is required",
		},
		{
			name: "confirmation without a named target is inert",
			args: []string{"repo", "delete", "--yes"},
			want: "--yes only applies when the target is named explicitly",
		},
		{
			name: "the alias behaves the same way",
			args: []string{"repo", "admin", "delete", "--yes"},
			want: "--yes only applies when the target is named explicitly",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := executeTestCLIWith(t, setup, testCase.args...)
			if err == nil {
				t.Fatal("the repository was deleted without an explicitly named target")
			}
			if !strings.Contains(err.Error(), testCase.want) {
				t.Errorf("error = %q, want it to mention %q", err.Error(), testCase.want)
			}
		})
	}
}

// TestRepoDeleteRefusalNamesTheFlag is the half ADR-054 left unsaid.
//
// Declining to prompt is not the same as proceeding: a run with nobody to ask
// must still say which flag would have supplied the answer, or the caller
// learns nothing it can act on. An agent that reads this message can fix its
// own invocation.
func TestRepoDeleteRefusalNamesTheFlag(t *testing.T) {
	t.Parallel()

	setup := testSetup{Host: "https://bitbucket.example.com", Token: "token"}

	_, err := executeTestCLIWith(t, setup, "repo", "delete", "PRJ/repo")
	if err == nil {
		t.Fatal("the repository was deleted with no confirmation and no --yes")
	}

	message := err.Error()
	for _, expected := range []string{"--yes", "PRJ/repo"} {
		if !strings.Contains(message, expected) {
			t.Errorf("error = %q, want it to mention %q", message, expected)
		}
	}
}

// TestRepoDeleteRefusesTwoDifferentTargets covers a silent resolution.
//
// PROJECT/slug as an argument and a different --repo both name a repository.
// Preferring one is what every other command here does, and for an
// irreversible one it means the caller watches the wrong repository survive.
func TestRepoDeleteRefusesTwoDifferentTargets(t *testing.T) {
	t.Parallel()

	setup := testSetup{Host: "https://bitbucket.example.com", Token: "token"}

	_, err := executeTestCLIWith(t, setup, "repo", "delete", "PRJ/repo", "--repo", "OTHER/other", "--yes")
	if err == nil {
		t.Fatal("two contradictory targets were resolved silently")
	}
	for _, expected := range []string{"PRJ/repo", "OTHER/other"} {
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("error = %q, want it to name %q", err.Error(), expected)
		}
	}

	// The same repository twice is not a contradiction.
	if _, err := executeTestCLIWith(t, setup, "repo", "delete", "PRJ/repo", "--repo", "PRJ/repo", "--yes"); err != nil {
		if strings.Contains(err.Error(), "two different repositories") {
			t.Errorf("naming the same repository twice was rejected: %v", err)
		}
	}
}

// TestNoInputRefusesWithoutPrompting pins the flag ADR-072 declares.
//
// The record stated --no-input as rule 1 and the field was documented as "the
// --no-input flag" before anything registered it, so a refusal could name a
// flag nobody could pass. This fails if that regresses.
func TestNoInputRefusesWithoutPrompting(t *testing.T) {
	t.Parallel()

	setup := testSetup{Host: "https://bitbucket.example.com", Token: "token"}

	_, err := executeTestCLIWith(t, setup, "repo", "delete", "PRJ/repo", "--no-input")
	if err == nil {
		t.Fatal("the repository was deleted with --no-input and no --yes")
	}
	if !strings.Contains(err.Error(), "--yes is required") {
		t.Errorf("error = %q, want the refusal to name --yes", err.Error())
	}
	if !strings.Contains(err.Error(), "--no-input") {
		t.Errorf("error = %q, want it to say --no-input is why nobody was asked", err.Error())
	}
}

// TestRepoDeleteTreatsAnInferredTargetAsUnnamed closes a hole in the fix for
// #472.
//
// applyInferredRepositoryContext sets --repo from the git remote and marks the
// flag Changed, because every command reads the flag to resolve its target.
// The first version of this guard read Changed as "the caller named it", so
// inside a checkout whose remote matches the configured host, `bb repo delete
// --yes` was treated as an explicit target and deleted the repository you were
// standing in -- the exact scenario the issue reports, reintroduced by its fix.
func TestRepoDeleteTreatsAnInferredTargetAsUnnamed(t *testing.T) {
	t.Parallel()

	setup := testSetup{Host: "https://bitbucket.example.com", Token: "token", Inferred: true}

	// --repo is set the way inference sets it: a real value, flag marked
	// Changed, but nothing the caller wrote.
	_, err := executeTestCLIWith(t, setup, "repo", "delete", "--repo", "PRJ/repo", "--yes")
	if err == nil {
		t.Fatal("--yes applied to an inferred target; the repository would have been deleted")
	}
	if !strings.Contains(err.Error(), "only applies when the target is named explicitly") {
		t.Errorf("error = %q, want it to say --yes does not apply to an inferred target", err.Error())
	}
}
