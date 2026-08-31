package prcmd

import (
	"fmt"
	"os"
	"testing"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/git/gittest"
)

// TestMain fails this package when its tests reconfigure the repository they
// run inside.
//
// These tests build throwaway repositories to exercise the CODEOWNERS lookup
// and `bb pr checkout`, both of which resolve a git repository from a
// directory. A helper that lost its cmd.Dir would run git here instead, and the
// working copy is a repository too, so the commands would succeed.
//
// The guard was missing here until ADR-071's governance test computed the set
// of packages that need it rather than relying on the list in AGENTS.md.
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
