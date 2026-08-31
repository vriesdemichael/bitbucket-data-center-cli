package gittest_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/git/gittest"
)

// TestTheAmbientRepositoryIsOutOfReach is the property Guard adds over a
// hand-written TestMain.
//
// Both incidents this package exists for were a git command finding the wrong
// repository by walking up from the working directory: a helper that lost its
// cmd.Dir, and a git init that honoured an inherited GIT_DIR and reinitialised
// the real worktree as bare. Guard sets a ceiling at the repository root, so
// that search stops and the command fails where the mistake is rather than
// succeeding against the developer's checkout.
//
// This runs inside a guarded package, so the ceiling is already in place. It
// asserts what a careless test would actually hit.
func TestTheAmbientRepositoryIsOutOfReach(t *testing.T) {
	if _, set := os.LookupEnv("GIT_CEILING_DIRECTORIES"); !set {
		t.Fatal("Guard did not place a ceiling; every test in this package can reach the repository it runs in")
	}

	// The shape of the first incident: git invoked with no directory of its
	// own, from a working directory inside the repository.
	command := exec.Command("git", "config", "--local", "--list")
	output, err := command.CombinedOutput()

	if err == nil {
		t.Fatalf("a stray git command read this repository's configuration:\n%s", output)
	}
	if !strings.Contains(string(output), "git repository") {
		t.Errorf("git failed for some other reason than not finding a repository: %s", output)
	}
}

// TestTheGuardStillReadsTheRepositoryItProtects is the other half.
//
// The ceiling must not blind the comparison. execgit.ScopeFreeEnv strips
// GIT_CEILING_DIRECTORIES, which is what lets the snapshot see the repository
// the tests above cannot -- and if that ever stopped being true the guard would
// silently compare nothing at all and pass forever.
func TestTheGuardStillReadsTheRepositoryItProtects(t *testing.T) {
	if snapshot := gittestSnapshot(); !snapshot {
		t.Fatal("the snapshot found no repository; with the ceiling in place the guard is comparing nothing and can no longer fail")
	}
}

// TestAFixtureRepositoryIsUnaffected keeps the prevention from costing anything
// the tests legitimately do.
//
// A ceiling is consulted only when git searches upward, so a repository a test
// creates and addresses directly is untouched. If that were not so, every
// fixture in this package would break.
func TestAFixtureRepositoryIsUnaffected(t *testing.T) {
	directory := t.TempDir()

	for _, arguments := range [][]string{
		{"init", "-q", "."},
		{"config", "user.name", "fixture"},
		{"config", "--local", "--get", "user.name"},
	} {
		command := exec.Command("git", arguments...)
		command.Dir = directory
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %s in a fixture repository failed under the ceiling: %v\n%s",
				strings.Join(arguments, " "), err, output)
		}
	}
}

// gittestSnapshot reports whether the guard can still see a repository.
func gittestSnapshot() bool {
	return gittest.SnapshotAmbientConfig().Available
}
