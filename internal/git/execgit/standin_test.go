package execgit

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// standInGitArguments names the file a stand-in git writes its arguments to.
// Only a test that installs the stand-in sets it.
const standInGitArguments = "BB_EXECGIT_STAND_IN_ARGUMENTS"

// ActAsStandInGit turns this test binary into git's stand-in when a test has
// installed it as one: it records the arguments it was started with and exits.
// TestMain calls it before anything else, so the copy on PATH never runs the
// tests itself.
//
// The stand-in is this binary rather than a shell script because a script
// named git runs only where a shell interprets it, and the suite runs on every
// operating system (ADR-016).
func ActAsStandInGit() {
	record := os.Getenv(standInGitArguments)
	if record == "" {
		return
	}

	if err := os.WriteFile(record, []byte(strings.Join(os.Args[1:], "\n")+"\n"), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "stand-in git: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

// installStandInGit puts a copy of this test binary first on PATH as git, and
// returns the file it will write its arguments to.
//
// Which name makes it git is asked of the system rather than decided by
// operating system: a plain "git" is tried first, and where the system starts
// only a file carrying an executable extension, the copy takes the one this
// binary carries.
//
// Not parallel: it replaces PATH for the process.
func installStandInGit(t *testing.T) string {
	t.Helper()

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("locate the test binary: %v", err)
	}
	contents, err := os.ReadFile(self)
	if err != nil {
		t.Fatalf("read the test binary: %v", err)
	}

	directory := t.TempDir()
	standIn := filepath.Join(directory, "git")
	// #nosec G306 -- the stand-in has to be executable to be started as git.
	if err := os.WriteFile(standIn, contents, 0o755); err != nil {
		t.Fatalf("write the stand-in git: %v", err)
	}

	record := filepath.Join(directory, "arguments.log")
	t.Setenv(standInGitArguments, record)
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))

	if gitResolvesInto(t, directory) {
		return record
	}

	if extension := filepath.Ext(self); extension != "" {
		if err := os.Rename(standIn, standIn+extension); err != nil {
			t.Fatalf("give the stand-in git the extension %s: %v", extension, err)
		}
		if gitResolvesInto(t, directory) {
			return record
		}
	}

	found, err := exec.LookPath("git")
	t.Fatalf("the system does not start the stand-in in %s as git; it resolves git to %q (%v)", directory, found, err)
	return ""
}

// gitResolvesInto reports whether "git" now resolves to a file in directory.
func gitResolvesInto(t *testing.T, directory string) bool {
	t.Helper()

	found, err := exec.LookPath("git")
	return err == nil && sameDir(t, filepath.Dir(found), directory)
}
