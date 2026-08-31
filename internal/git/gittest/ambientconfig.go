// Package gittest provides guards for test suites that shell out to git.
//
// It exists because they can silently reconfigure the repository they are
// running inside. Backend.Clone persists http.extraHeader into the cloned
// repository so later fetches carry authentication, and a test that pointed it
// at the working copy rather than a temporary directory wrote this into the
// project's own .git/config:
//
//	http.extraheader = Authorization: Basic <base64 of dummy-user:dummy-password>
//	user.name        = Test User
//	user.email       = test@example.local
//	remote.upstream.url = https://example.local/scm/PRJ/upstream.git
//
// An unscoped http.extraHeader is attached to every HTTP request git makes, and
// an explicit Authorization header takes precedence over any credential helper,
// so every push to GitHub sent dummy-user:dummy-password and was rejected with
// "Password authentication is not supported for Git operations". The wrong
// user.name and user.email meanwhile ended up on real commits.
//
// The tests were later isolated with t.TempDir, but nothing detected the
// original damage or would catch a recurrence. Comparing the ambient
// configuration before and after a package's tests does, and it catches any new
// key rather than only the values that happened to leak the first time.
//
// One scope the guard reads is not private to the test run. In a linked
// worktree "git config --local" resolves to $GIT_COMMON_DIR/config -- the main
// checkout's .git/config, shared by every worktree of the repository. A branch
// created, tracked or deleted anywhere in that set rewrites the file the guard
// is watching while the tests are running, and the comparison then reports it
// in every guarded package at once. The shared scope is still compared, because
// a test writing configuration from a worktree lands in that same file; git's
// own bookkeeping for branch and remote operations is excluded from it. See
// concurrentWorktreeBookkeeping.
package gittest

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/git/execgit"
)

// ConfigSnapshot records the repository-scoped git configuration visible from
// the current working directory.
type ConfigSnapshot struct {
	// Available is false when git is missing or the directory is not a
	// repository. Comparing an unavailable snapshot reports no differences, so
	// the guard degrades to a no-op rather than failing for the wrong reason.
	Available bool
	entries   map[string]string
}

// SnapshotAmbientConfig reads the local and worktree scoped configuration of
// the repository the tests are running inside.
//
// Global and system scopes are deliberately excluded: they belong to the
// developer, and a test has no business changing them either, but including
// them would make the guard fail whenever an unrelated tool touched the user's
// machine mid-run.
func SnapshotAmbientConfig() ConfigSnapshot {
	return snapshotAmbientConfigIn("")
}

// snapshotAmbientConfigIn reads the same two scopes from a named directory.
// An empty directory means the process working directory, which is what the
// exported entry point wants; the tests for this package pass a temporary
// repository instead. They must not use t.Chdir: it moves the working directory
// for the whole test binary, and that is exactly the fault this package exists
// to catch.
func snapshotAmbientConfigIn(directory string) ConfigSnapshot {
	entries := map[string]string{}
	found := false

	for _, scope := range []string{"--local", "--worktree"} {
		// #nosec G204 -- fixed binary, fixed arguments; scope is one of the two
		// literals in the loop above.
		command := exec.Command("git", "config", scope, "--list", "-z")
		// Git exports GIT_DIR and friends to every hook it runs, and honours
		// them over the working directory. Without scrubbing them the guard
		// would inspect the hook's repository rather than the one the tests are
		// running in — and the pre-push hook is exactly where the live suite
		// runs.
		command.Env = execgit.ScopeFreeEnv()
		command.Dir = directory

		output, err := command.Output()
		if err != nil {
			// A repository without worktree config returns an error for
			// --worktree; that is not a failure, it just has nothing to record.
			continue
		}
		found = true

		for _, record := range strings.Split(string(output), "\x00") {
			if record == "" {
				continue
			}
			key, value, _ := strings.Cut(record, "\n")
			entries[scope+" "+key] = value
		}
	}

	return ConfigSnapshot{Available: found, entries: entries}
}

// concurrentWorktreeBookkeeping reports whether a scoped key is one git writes
// into the shared repository configuration on behalf of an operation that may
// have happened in any worktree, rather than in the tests being compared.
//
// It applies to the --local scope only. That scope is shared -- in a linked
// worktree it resolves to the main checkout's .git/config -- so a sibling
// worktree running git branch, git checkout -b, git push -u or git fetch
// rewrites it mid-run. This project develops in a couple of dozen parallel
// worktrees, and the resulting diff surfaced as a simultaneous, unreproducible
// failure of every guarded package with no --- FAIL line to explain it.
//
// The --worktree scope is $GIT_DIR/config.worktree, which no other worktree can
// reach, so it is compared in full and nothing is excluded from it.
//
// What is given up is branch tracking bookkeeping in the shared scope. Neither
// incident this guard exists for involved it: http.extraheader, user.name,
// user.email, remote.upstream.url and core.bare are all still reported. The
// serious half of a test that ran git checkout -b here by mistake -- moving the
// working copy off the branch the developer had checked out -- was never
// something a comparison of configuration keys could see.
func concurrentWorktreeBookkeeping(scopedKey string) bool {
	scope, key, scoped := strings.Cut(scopedKey, " ")
	if !scoped || scope != "--local" {
		return false
	}

	switch {
	case strings.HasPrefix(key, "branch."):
		// branch.<name>.remote, .merge and .pushremote, written when a branch
		// gains an upstream and removed when the branch is deleted.
		return true
	case strings.HasPrefix(key, "remote.") && strings.HasSuffix(key, ".fetch"):
		// The refspec git writes alongside a remote it fetches from. The URL of
		// that remote is deliberately not excluded: remote.upstream.url is one
		// of the four keys the first incident wrote.
		return true
	case strings.HasPrefix(key, "lfs."):
		// lfs.<url>.access, written by git-lfs the first time it authenticates
		// against a remote.
		return true
	}

	return false
}

// Diff returns human-readable descriptions of every configuration key the tests
// added, removed or changed, except the shared-scope bookkeeping another
// worktree may have written concurrently. An empty result means the tests left
// the ambient repository exactly as they found it.
func Diff(before ConfigSnapshot, after ConfigSnapshot) []string {
	if !before.Available || !after.Available {
		return nil
	}

	differences := []string{}

	for key, afterValue := range after.entries {
		if concurrentWorktreeBookkeeping(key) {
			continue
		}

		beforeValue, existed := before.entries[key]
		switch {
		case !existed:
			differences = append(differences, "added "+key+" = "+afterValue)
		case beforeValue != afterValue:
			differences = append(differences, "changed "+key+" from "+beforeValue+" to "+afterValue)
		}
	}

	for key := range before.entries {
		if concurrentWorktreeBookkeeping(key) {
			continue
		}

		if _, still := after.entries[key]; !still {
			differences = append(differences, "removed "+key)
		}
	}

	sort.Strings(differences)

	return differences
}

// FailureMessage renders a diff as an explanation a reader can act on.
func FailureMessage(differences []string) string {
	var message strings.Builder
	message.WriteString("tests changed the git configuration of the repository they run inside:\n")
	for _, difference := range differences {
		message.WriteString("  " + difference + "\n")
	}
	message.WriteString(
		"\nA test that shells out to git must operate on a directory it created, normally t.TempDir().\n" +
			"Check for a git command missing -C, or a helper defaulting to the current directory.\n" +
			"Undo the damage with: git config --local --unset <key>\n",
	)

	return message.String()
}

// Guard runs a package's tests with the ambient repository placed out of reach,
// and fails the package if they changed it anyway.
//
// It replaces a TestMain that snapshots, runs and compares by hand. That block
// was copied into every package whose tests start a git process, and a copied
// block is one that drifts: the version in one package can gain a check the
// others never get, and the difference is invisible until it matters.
//
// The reach-removal is the part a hand-written TestMain did not do. Both
// incidents this package exists for were a git command finding the wrong
// repository by walking up from the working directory -- a helper that lost its
// cmd.Dir, and a git init that honoured an inherited GIT_DIR. Setting a ceiling
// at the repository root stops that search, so such a command fails with
// "not inside a git repository" at the moment of the mistake rather than
// succeeding against the developer's own checkout.
//
// The comparison is kept regardless, because a ceiling does not stop everything.
// A test that passes an explicit path, or sets GIT_DIR itself, still reaches the
// repository -- and a sibling worktree writing to the shared configuration is
// not this process at all. Prevention narrows the class; it does not close it.
//
// Guard calls os.Exit and does not return.
// Guard is deliberately three lines. It ends in os.Exit, which cannot be called
// from a test, so everything it decides lives in the two functions below where
// it can be.
func Guard(m *testing.M) {
	placeRepositoryOutOfReach(os.Stderr)
	before := SnapshotAmbientConfig()
	os.Exit(exitCode(m.Run(), before, os.Stderr))
}

// placeRepositoryOutOfReach sets a ceiling at the repository root and returns
// what it set, or "" when there was nothing to place.
//
// Ceilings are consulted only when git searches upward, so this does not affect
// a repository the tests create and address directly. execgit.ScopeFreeEnv
// strips this variable, which is what lets the snapshot still read the
// repository the tests may not.
//
// Nothing here is fatal. The comparison is the guarantee; the ceiling narrows
// what can reach the repository before the comparison has to notice.
//
// No previous value is restored: the caller ends in os.Exit, which does not run
// deferred functions, and a process about to exit has nothing to restore for.
func placeRepositoryOutOfReach(complaints io.Writer) string {
	return placeRepositoryOutOfReachFrom("", complaints)
}

// placeRepositoryOutOfReachFrom is placeRepositoryOutOfReach from a named
// directory. An empty directory means the process working directory; the tests
// pass one that is not a repository, to reach the branch where there is nothing
// to place.
func placeRepositoryOutOfReachFrom(directory string, complaints io.Writer) string {
	root, err := repositoryRoot(directory)
	if err != nil {
		return ""
	}

	ceiling := root
	if previous, set := os.LookupEnv(ceilingVariable); set && previous != "" {
		ceiling = root + string(os.PathListSeparator) + previous
	}

	if err := os.Setenv(ceilingVariable, ceiling); err != nil {
		fmt.Fprintf(complaints, "gittest: could not place the repository out of reach: %v\n", err)
		return ""
	}
	return ceiling
}

// exitCode reports what the test binary should exit with, failing a run whose
// tests passed but which left the repository changed.
func exitCode(code int, before ConfigSnapshot, report io.Writer) int {
	differences := Diff(before, SnapshotAmbientConfig())
	if len(differences) == 0 {
		return code
	}

	fmt.Fprint(report, FailureMessage(differences))
	if code == 0 {
		return 1
	}
	return code
}

// ceilingVariable stops git's upward search for a repository.
const ceilingVariable = "GIT_CEILING_DIRECTORIES"

// repositoryRoot locates the repository the tests are running inside.
//
// It asks git rather than walking for a .git entry, so a linked worktree
// answers with its own root rather than the main checkout's.
func repositoryRoot(directory string) (string, error) {
	command := exec.Command("git", "rev-parse", "--show-toplevel")
	command.Env = execgit.ScopeFreeEnv()
	command.Dir = directory

	output, err := command.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}
