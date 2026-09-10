//go:build live

package live_test

import (
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/git/gittest"
)

// TestMain fails the live suite when it reconfigures the repository it runs
// inside. This suite pushes real commits through the git backend, so it is the
// most likely place for a helper to lose its working directory and operate on
// the project checkout instead of its own fixture.
func TestMain(m *testing.M) {
	// The real credential store, not the in-memory one a test binary gets by
	// default. TestLiveGitCredentialHelperAuthenticatesClone runs `bb auth
	// login` here and then spawns a separately built bb as git's credential
	// helper -- another process, which can only find the credential where the
	// operating system keeps it.
	config.UseOSKeyring()

	configureLiveCLIConstants()

	// The suite is unusable without credentials and git, and it used to skip
	// every test that noticed -- so `task test:live` against an unconfigured
	// machine reported success having run nothing at all. The build tag is
	// already the opt-in: asking for this suite and not being set up for it is
	// a misconfiguration, and the answer to it is a message, not a green run.
	//
	// Once, here, rather than per test: newLiveHarness runs for every test in
	// the suite, so skipping there turned one cause into a hundred silences,
	// and failing there would turn it into a hundred failures.
	if reason := liveSuiteUnusable(); reason != "" {
		fmt.Fprintf(os.Stderr, "the live suite cannot run: %s\n", reason)
		os.Exit(1)
	}

	before := gittest.SnapshotAmbientConfig()
	code := m.Run()

	if differences := gittest.Diff(before, gittest.SnapshotAmbientConfig()); len(differences) > 0 {
		fmt.Fprint(os.Stderr, gittest.FailureMessage(differences))
		if code == 0 {
			code = 1
		}
	}

	os.Exit(code)
}

// liveSuiteUnusable names what is missing, or returns empty when the suite can
// run. Everything it checks is a precondition of the suite as a whole rather
// than of any one test.
func liveSuiteUnusable() string {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		return fmt.Sprintf("the configuration did not load: %v", err)
	}

	if cfg.BitbucketUsername == "" || cfg.BitbucketPassword == "" {
		return "no credentials. Set BITBUCKET_USERNAME/BITBUCKET_PASSWORD (or ADMIN_USER/ADMIN_PASSWORD), " +
			"or run `task stack:up` which writes them"
	}

	if _, err := exec.LookPath("git"); err != nil {
		return "git is not on PATH, and the suite seeds repositories by pushing real commits"
	}

	return ""
}
