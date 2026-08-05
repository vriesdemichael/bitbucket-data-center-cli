package execgit

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// These tests must not assume the ambient environment is free of git's
// repository-scoping variables: git exports them to every hook it runs, and the
// suite is itself run from a pre-push hook. Any git command here therefore
// passes an explicit environment rather than inheriting one.

func TestScopeFreeEnvRemovesRepositoryScopeVars(t *testing.T) {
	for _, name := range repositoryScopeVars {
		t.Setenv(name, "/somewhere/else")
	}
	t.Setenv("BB_EXECGIT_SENTINEL", "kept")

	filtered := ScopeFreeEnv()

	for _, name := range repositoryScopeVars {
		if containsEnvVar(filtered, name) {
			t.Errorf("%s must be stripped so git is scoped by the working directory alone", name)
		}
	}

	if !containsEnvVar(filtered, "BB_EXECGIT_SENTINEL") {
		t.Error("unrelated environment variables must be preserved")
	}
}

// TestScopeFreeEnvPreservesEverythingElse pins that filtering removes only the
// scoping variables, so credential and proxy configuration still reaches git.
func TestScopeFreeEnvPreservesEverythingElse(t *testing.T) {
	t.Setenv("GIT_DIR", "/somewhere/else/.git")
	t.Setenv("GIT_TERMINAL_PROMPT", "0")
	t.Setenv("HTTPS_PROXY", "http://proxy.example:3128")

	filtered := ScopeFreeEnv()

	// Compare against the actual environment rather than a fixed count: more
	// scoping variables may already be present when running under a hook.
	for _, entry := range os.Environ() {
		name, _, found := strings.Cut(entry, "=")
		if !found || slices.Contains(repositoryScopeVars, name) {
			continue
		}
		if !containsEnvVar(filtered, name) {
			t.Errorf("%s is not repository-scoping and must be preserved", name)
		}
	}

	if containsEnvVar(filtered, "GIT_DIR") {
		t.Error("GIT_DIR must be stripped")
	}
	if !containsEnvVar(filtered, "GIT_TERMINAL_PROMPT") {
		t.Error("GIT_TERMINAL_PROMPT is not repository-scoping and must be preserved")
	}
}

// TestScopeFreeEnvIsolatesGitFromAnAmbientRepository is the behavioural check:
// with GIT_DIR pointing at another repository, a git command run with the
// filtered environment must still resolve the one in its working directory.
func TestScopeFreeEnvIsolatesGitFromAnAmbientRepository(t *testing.T) {
	decoy := t.TempDir()
	target := t.TempDir()

	// Initialise with a scrubbed environment so an ambient GIT_DIR cannot
	// redirect the setup itself.
	runGitIn(t, ScopeFreeEnv(), decoy, "init")
	runGitIn(t, ScopeFreeEnv(), target, "init")

	t.Setenv("GIT_DIR", filepath.Join(decoy, ".git"))
	t.Setenv("GIT_INDEX_FILE", filepath.Join(decoy, ".git", "index"))

	resolved := runGitIn(t, ScopeFreeEnv(), target, "rev-parse", "--absolute-git-dir")

	if !sameDir(t, resolved, filepath.Join(target, ".git")) {
		t.Errorf("git resolved %q, want the repository in %q", resolved, target)
	}
	if sameDir(t, resolved, filepath.Join(decoy, ".git")) {
		t.Errorf("git followed the ambient GIT_DIR and resolved %q", resolved)
	}
}

// sameDir compares two paths tolerating separator, symlink and case
// differences, which all vary across the platforms this suite runs on.
func sameDir(t *testing.T, a, b string) bool {
	t.Helper()

	normalize := func(p string) string {
		if resolved, err := filepath.EvalSymlinks(p); err == nil {
			p = resolved
		}
		return strings.ToLower(filepath.ToSlash(filepath.Clean(p)))
	}

	return normalize(a) == normalize(b)
}

// runGitIn runs git in dir with an explicit environment and returns trimmed
// output, skipping the test if git is unavailable.
func runGitIn(t *testing.T, environment []string, dir string, args ...string) string {
	t.Helper()

	command := exec.Command("git", args...)
	command.Dir = dir
	command.Env = environment

	output, err := command.CombinedOutput()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			t.Skip("git is not available")
		}
		t.Fatalf("git %s in %s failed: %v: %s", strings.Join(args, " "), dir, err, output)
	}

	return strings.TrimSpace(string(output))
}

func containsEnvVar(environment []string, name string) bool {
	prefix := name + "="
	for _, entry := range environment {
		if strings.HasPrefix(entry, prefix) {
			return true
		}
	}
	return false
}
