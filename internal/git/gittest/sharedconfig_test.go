package gittest

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/git/execgit"
)

// git runs a git command in directory and fails the test if it does not succeed.
func git(t *testing.T, directory string, arguments ...string) string {
	t.Helper()

	command := exec.Command("git", arguments...)
	command.Dir = directory
	// Git honours GIT_DIR and friends over the working directory, and exports
	// them to every hook it runs -- the pre-commit hook runs this suite. Without
	// scrubbing them these fixtures operate on the hook's repository instead of
	// the temporary one, and `git init` under an inherited GIT_DIR reinitialises
	// the real worktree as bare. That wrote core.bare=true into this project's
	// shared configuration and broke git rev-parse in every worktree, which is
	// the second incident in this package's documentation, committed by its own
	// tests.
	command.Env = execgit.ScopeFreeEnv()

	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(arguments, " "), directory, err, output)
	}

	return strings.TrimSpace(string(output))
}

// repositoryWithTwoWorktrees builds a repository with a remote and two linked
// worktrees, and returns their paths. The two worktrees share one .git/config,
// which is the whole point of the fixture.
func repositoryWithTwoWorktrees(t *testing.T) (observer string, sibling string, upstream string) {
	t.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}

	root := t.TempDir()
	checkout := filepath.Join(root, "checkout")

	git(t, root, "init", "-q", "checkout")
	// -c rather than git config: the identity must not land in the fixture's
	// own configuration, which is what the snapshots are comparing.
	git(t, checkout, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.local",
		"commit", "-q", "--allow-empty", "-m", "initial")

	// The repository is its own remote. That is enough to give a branch an
	// upstream, which is what makes git write branch bookkeeping.
	git(t, checkout, "remote", "add", "origin", checkout)
	git(t, checkout, "fetch", "-q", "origin")

	// The branch the fixture commit landed on, the only one with a
	// remote-tracking counterpart to point an upstream at.
	upstream = "origin/" + git(t, checkout, "rev-parse", "--abbrev-ref", "HEAD")

	observer = filepath.Join(root, "observer")
	sibling = filepath.Join(root, "sibling")
	git(t, checkout, "worktree", "add", "-q", observer, "-b", "observer-work")
	git(t, checkout, "worktree", "add", "-q", sibling, "-b", "sibling-work")

	return observer, sibling, upstream
}

// The guard reads --local, which in a linked worktree is $GIT_COMMON_DIR/config
// -- the main checkout's .git/config, shared with every sibling worktree. This
// asserts the bleed is real rather than assumed: a branch operation in one
// worktree is visible in the other's snapshot.
func TestLocalScopeIsSharedBetweenWorktrees(t *testing.T) {
	t.Parallel()

	observer, sibling, upstream := repositoryWithTwoWorktrees(t)

	before := snapshotAmbientConfigIn(observer)
	if !before.Available {
		t.Fatal("expected the fixture worktree to be a repository")
	}

	git(t, sibling, "branch", "--set-upstream-to", upstream, "sibling-work")

	after := snapshotAmbientConfigIn(observer)
	if _, leaked := after.entries["--local branch.sibling-work.remote"]; !leaked {
		t.Fatal("the fixture no longer reproduces the shared-config bleed; " +
			"if git changed how --local resolves in a worktree, revisit concurrentWorktreeBookkeeping")
	}
}

// The failure this fixes: a sibling worktree gives a branch an upstream while
// the tests are running, and the guard blames the tests.
func TestSiblingWorktreeBranchOperationDoesNotFailTheGuard(t *testing.T) {
	t.Parallel()

	observer, sibling, upstream := repositoryWithTwoWorktrees(t)

	before := snapshotAmbientConfigIn(observer)

	git(t, sibling, "branch", "--set-upstream-to", upstream, "sibling-work")
	git(t, sibling, "branch", "-q", "later-work")
	git(t, sibling, "branch", "-q", "--set-upstream-to", upstream, "later-work")
	git(t, sibling, "branch", "-q", "-D", "later-work")

	if differences := Diff(before, snapshotAmbientConfigIn(observer)); len(differences) > 0 {
		t.Fatalf("a sibling worktree's branch operations failed the guard:\n%s", FailureMessage(differences))
	}
}

// And the guard still catches the thing it exists for, written through real git
// from the worktree the tests would be running in.
func TestConfigWrittenFromTheObservedWorktreeStillFailsTheGuard(t *testing.T) {
	t.Parallel()

	observer, _, _ := repositoryWithTwoWorktrees(t)

	before := snapshotAmbientConfigIn(observer)

	// No scope flag, exactly as Backend.Clone writes it into a repository it
	// believes it created.
	git(t, observer, "config", "http.extraHeader", "Authorization: Basic ZHVtbXk=")
	git(t, observer, "config", "user.email", "test@example.local")

	differences := Diff(before, snapshotAmbientConfigIn(observer))
	joined := strings.Join(differences, "\n")

	if !strings.Contains(joined, "added --local http.extraheader") {
		t.Fatalf("expected http.extraheader to be reported, got:\n%s", joined)
	}
	if !strings.Contains(joined, "added --local user.email") {
		t.Fatalf("expected user.email to be reported, got:\n%s", joined)
	}
}
