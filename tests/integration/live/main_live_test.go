//go:build live

package live_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/git/gittest"
)

// TestMain fails the live suite when it reconfigures the repository it runs
// inside. This suite pushes real commits through the git backend, so it is the
// most likely place for a helper to lose its working directory and operate on
// the project checkout instead of its own fixture.
func TestMain(m *testing.M) {
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
