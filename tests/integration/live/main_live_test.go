//go:build live

package live_test

import (
	"fmt"
	"os"
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
