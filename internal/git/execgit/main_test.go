package execgit

import (
	"fmt"
	"os"
	"testing"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/git/gittest"
)

// TestMain fails this package when its tests reconfigure the repository they
// run inside. Clone persists http.extraHeader into the repository it clones
// into, so a target directory that is not a temporary one silently rewrites the
// project's own git configuration. That happened, and it broke authentication
// for every push until someone noticed months later.
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
