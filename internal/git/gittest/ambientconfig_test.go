package gittest

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func snapshot(entries map[string]string) ConfigSnapshot {
	return ConfigSnapshot{Available: true, entries: entries}
}

func TestDiffDetectsEveryKindOfChange(t *testing.T) {
	before := snapshot(map[string]string{
		"--local remote.origin.url": "https://github.com/owner/repo.git",
		"--local user.email":        "dev@example.com",
		"--local core.bare":         "false",
	})
	after := snapshot(map[string]string{
		"--local remote.origin.url": "https://github.com/owner/repo.git",
		"--local user.email":        "test@example.local",
		"--local http.extraheader":  "Authorization: Basic dummy",
	})

	differences := Diff(before, after)
	joined := strings.Join(differences, "\n")

	if len(differences) != 3 {
		t.Fatalf("expected 3 differences, got %d:\n%s", len(differences), joined)
	}
	if !strings.Contains(joined, "added --local http.extraheader = Authorization: Basic dummy") {
		t.Fatalf("expected the added key to be reported, got:\n%s", joined)
	}
	if !strings.Contains(joined, "changed --local user.email from dev@example.com to test@example.local") {
		t.Fatalf("expected the changed key to be reported, got:\n%s", joined)
	}
	if !strings.Contains(joined, "removed --local core.bare") {
		t.Fatalf("expected the removed key to be reported, got:\n%s", joined)
	}
}

// A developer's own repository-scoped settings must not be reported. The guard
// compares before against after, so pre-existing configuration is invisible to
// it no matter how unusual it looks.
func TestDiffIgnoresPreExistingConfiguration(t *testing.T) {
	entries := map[string]string{
		"--local user.email":        "personal@example.com",
		"--local http.extraheader":  "Authorization: Bearer already-here",
		"--local remote.origin.url": "https://github.com/owner/repo.git",
	}

	if differences := Diff(snapshot(entries), snapshot(entries)); len(differences) != 0 {
		t.Fatalf("expected no differences for unchanged config, got %v", differences)
	}
}

// Without git, or outside a repository, the guard must stay silent rather than
// failing a suite for a reason that has nothing to do with the tests.
func TestDiffIsInertWhenSnapshotsAreUnavailable(t *testing.T) {
	populated := snapshot(map[string]string{"--local user.email": "dev@example.com"})
	unavailable := ConfigSnapshot{Available: false}

	if differences := Diff(unavailable, populated); len(differences) != 0 {
		t.Fatalf("expected no differences when the first snapshot is unavailable, got %v", differences)
	}
	if differences := Diff(populated, unavailable); len(differences) != 0 {
		t.Fatalf("expected no differences when the second snapshot is unavailable, got %v", differences)
	}
}

func TestDiffIsStable(t *testing.T) {
	before := snapshot(map[string]string{})
	after := snapshot(map[string]string{
		"--local z.key": "1",
		"--local a.key": "2",
		"--local m.key": "3",
	})

	first := strings.Join(Diff(before, after), "|")
	for run := 0; run < 5; run++ {
		if got := strings.Join(Diff(before, after), "|"); got != first {
			t.Fatalf("Diff is not stable across runs: %q then %q", first, got)
		}
	}
}

func TestFailureMessageIsActionable(t *testing.T) {
	message := FailureMessage([]string{"added --local http.extraheader = Authorization: Basic dummy"})

	for _, expected := range []string{
		"added --local http.extraheader",
		"t.TempDir()",
		"git config --local --unset",
	} {
		if !strings.Contains(message, expected) {
			t.Fatalf("expected the failure message to mention %q, got:\n%s", expected, message)
		}
	}
}

// The guard is only useful if it can actually read the repository it runs in.
func TestSnapshotAmbientConfigReadsTheRepository(t *testing.T) {
	current := SnapshotAmbientConfig()
	if !current.Available {
		t.Skip("not running inside a git repository")
	}

	if len(current.entries) == 0 {
		t.Fatal("expected at least one repository-scoped config entry")
	}
	for key := range current.entries {
		if !strings.HasPrefix(key, "--local ") && !strings.HasPrefix(key, "--worktree ") {
			t.Fatalf("entry %q is not scoped to the repository", key)
		}
	}
}

// The --local scope is shared by every worktree of the repository. A sibling
// worktree creating, tracking or deleting a branch rewrites it while the tests
// are running, and before this was excluded the comparison blamed the tests --
// in every guarded package at once, with no --- FAIL line to explain it.
func TestDiffIgnoresConfigAnotherWorktreeWrote(t *testing.T) {
	before := snapshot(map[string]string{
		"--local core.bare":           "false",
		"--local branch.gone.remote":  "origin",
		"--local branch.gone.merge":   "refs/heads/gone",
		"--local remote.origin.url":   "https://github.com/owner/repo.git",
		"--local remote.origin.fetch": "+refs/heads/*:refs/remotes/origin/*",
	})
	after := snapshot(map[string]string{
		"--local core.bare": "false",
		// A sibling ran push -u on a new branch.
		"--local branch.sibling.remote": "origin",
		"--local branch.sibling.merge":  "refs/heads/sibling",
		// A sibling deleted the branch it had finished with.
		"--local remote.origin.url": "https://github.com/owner/repo.git",
		// A sibling re-pointed a fetch refspec, and git-lfs authenticated.
		"--local remote.origin.fetch":                                   "+refs/heads/main:refs/remotes/origin/main",
		"--local lfs.https://github.com/owner/repo.git/info/lfs.access": "basic",
	})

	if differences := Diff(before, after); len(differences) != 0 {
		t.Fatalf("expected sibling-worktree bookkeeping to be ignored, got %v", differences)
	}
}

// The exclusion buys nothing if it also hides the damage the guard exists for.
// These are the keys from both incidents recorded in the package documentation.
func TestDiffStillReportsEveryKeyFromBothIncidents(t *testing.T) {
	before := snapshot(map[string]string{"--local core.bare": "false"})
	after := snapshot(map[string]string{
		"--local core.bare":           "true",
		"--local http.extraheader":    "Authorization: Basic ZHVtbXk=",
		"--local user.name":           "Test User",
		"--local user.email":          "test@example.local",
		"--local remote.upstream.url": "https://example.local/scm/PRJ/upstream.git",
	})

	differences := Diff(before, after)
	joined := strings.Join(differences, "\n")

	for _, expected := range []string{
		"changed --local core.bare from false to true",
		"added --local http.extraheader",
		"added --local user.name",
		"added --local user.email",
		"added --local remote.upstream.url",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("expected %q to still be reported, got:\n%s", expected, joined)
		}
	}
	if len(differences) != 5 {
		t.Fatalf("expected exactly 5 differences, got %d:\n%s", len(differences), joined)
	}
}

// The worktree scope is $GIT_DIR/config.worktree, which no sibling can reach,
// so the exclusion must not apply to it.
func TestDiffReportsBookkeepingKeysInTheWorktreeScope(t *testing.T) {
	before := snapshot(map[string]string{})
	after := snapshot(map[string]string{
		"--worktree branch.local.remote": "origin",
		"--worktree remote.origin.fetch": "+refs/heads/*:refs/remotes/origin/*",
		"--worktree lfs.example.access":  "basic",
	})

	if differences := Diff(before, after); len(differences) != 3 {
		t.Fatalf("expected all 3 worktree-scoped keys to be reported, got %v", differences)
	}
}

// A remote's URL is not bookkeeping. remote.upstream.url is one of the four
// keys the first incident wrote, and only the refspec beside it is excluded.
func TestDiffSeparatesARemotesURLFromItsRefspec(t *testing.T) {
	before := snapshot(map[string]string{})
	after := snapshot(map[string]string{
		"--local remote.upstream.url":   "https://example.local/scm/PRJ/upstream.git",
		"--local remote.upstream.fetch": "+refs/heads/*:refs/remotes/upstream/*",
	})

	differences := Diff(before, after)
	if len(differences) != 1 || !strings.Contains(differences[0], "remote.upstream.url") {
		t.Fatalf("expected only the URL to be reported, got %v", differences)
	}
}

// TestPlaceRepositoryOutOfReachSetsACeiling covers the prevention half.
//
// Guard itself ends in os.Exit and cannot be called from a test, so what it
// decides lives here where it can be.
//
// Not parallel: these replace a process environment variable.
func TestPlaceRepositoryOutOfReachSetsACeiling(t *testing.T) {
	t.Setenv(ceilingVariable, "")

	ceiling := placeRepositoryOutOfReach(io.Discard)
	if ceiling == "" {
		t.Fatal("no ceiling was placed; a stray git command can still reach this repository")
	}

	root, err := repositoryRoot()
	if err != nil {
		t.Fatalf("repository root: %v", err)
	}
	if ceiling != root {
		t.Errorf("ceiling = %q, want the repository root %q", ceiling, root)
	}
	if os.Getenv(ceilingVariable) != ceiling {
		t.Errorf("the variable was not set to what was returned: %q", os.Getenv(ceilingVariable))
	}
}

// TestPlaceRepositoryOutOfReachKeepsAnExistingCeiling guards against taking
// away a boundary somebody else set.
func TestPlaceRepositoryOutOfReachKeepsAnExistingCeiling(t *testing.T) {
	existing := filepath.Join("somewhere", "else")
	t.Setenv(ceilingVariable, existing)

	ceiling := placeRepositoryOutOfReach(io.Discard)
	if !strings.Contains(ceiling, existing) {
		t.Errorf("ceiling = %q, want it to still contain %q", ceiling, existing)
	}
	if !strings.HasPrefix(ceiling, string(os.PathListSeparator)) && !strings.Contains(ceiling, string(os.PathListSeparator)) {
		t.Errorf("ceiling = %q, want the repository root joined to the existing value", ceiling)
	}
}

// TestExitCodeFailsARunThatChangedTheRepository is the reporting half.
func TestExitCodeFailsARunThatChangedTheRepository(t *testing.T) {
	unchanged := SnapshotAmbientConfig()

	cases := []struct {
		name     string
		before   ConfigSnapshot
		code     int
		want     int
		reported bool
	}{
		{name: "clean run, nothing changed", before: unchanged, code: 0, want: 0},
		{name: "failing run, nothing changed", before: unchanged, code: 2, want: 2},
		{
			name:     "passing run that changed the repository",
			before:   ConfigSnapshot{Available: true, entries: map[string]string{"--local invented.key": "x"}},
			code:     0,
			want:     1,
			reported: true,
		},
		{
			name:     "already failing, and it changed the repository too",
			before:   ConfigSnapshot{Available: true, entries: map[string]string{"--local invented.key": "x"}},
			code:     3,
			want:     3,
			reported: true,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			report := &bytes.Buffer{}
			if got := exitCode(testCase.code, testCase.before, report); got != testCase.want {
				t.Errorf("exit code = %d, want %d", got, testCase.want)
			}
			if reported := report.Len() > 0; reported != testCase.reported {
				t.Errorf("reported = %v, want %v (%q)", reported, testCase.reported, report.String())
			}
		})
	}
}
