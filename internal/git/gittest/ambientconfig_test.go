package gittest

import (
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
