// This file is package gittest_test rather than package gittest so that the
// guard is installed here the same way every other package installs it, through
// the exported API. A package that guarded itself through unexported internals
// would not look guarded to anything that checks for the call.
package gittest_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/git/gittest"
)

// TestMain fails this package when its tests reconfigure the repository they
// run inside.
//
// The guard's own tests build real repositories and worktrees and write git
// configuration into them on purpose. Every one of those writes has to land in
// a directory the test created; a fixture that lost its cmd.Dir would write
// here instead, and the package that exists to catch that fault is the last
// place it should go unnoticed.
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
