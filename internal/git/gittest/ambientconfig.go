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
package gittest

import (
	"os/exec"
	"sort"
	"strings"

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

// Diff returns human-readable descriptions of every configuration key the tests
// added, removed or changed. An empty result means the tests left the ambient
// repository exactly as they found it.
func Diff(before ConfigSnapshot, after ConfigSnapshot) []string {
	if !before.Available || !after.Available {
		return nil
	}

	differences := []string{}

	for key, afterValue := range after.entries {
		beforeValue, existed := before.entries[key]
		switch {
		case !existed:
			differences = append(differences, "added "+key+" = "+afterValue)
		case beforeValue != afterValue:
			differences = append(differences, "changed "+key+" from "+beforeValue+" to "+afterValue)
		}
	}

	for key := range before.entries {
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
