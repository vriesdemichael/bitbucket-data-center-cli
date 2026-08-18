package auth

import (
	"fmt"
	"os"
	"testing"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/git"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/git/gittest"
)

// TestMain fails this package when its tests reconfigure the repository they
// run inside.
//
// These tests exercise `bb auth setup-git`, which writes git configuration by
// design, so a test that resolves the wrong directory writes into this
// repository instead of a temporary one. That is not hypothetical: an earlier
// version of the setup-git tests used t.Chdir, which moves the working
// directory for the whole test binary, and the result was core.bare=true
// written into this project's own configuration — breaking `git rev-parse` in
// the main checkout and every worktree, and surfacing as unrelated failures in
// other packages.
//
// The guard was already installed on internal/cli and internal/git/execgit but
// not here, so nothing caught it.
func TestMain(m *testing.M) {
	// Same reasoning as the guard below, one step earlier: bb auth status reads
	// git configuration, and the default backend shells out to real git. Left
	// alone, every test that runs status would read whichever global
	// configuration the developer happens to have, so its result would depend on
	// whether they had run bb auth setup-git rather than on the code.
	//
	// Defaulting it here rather than at five call sites means a test added later
	// cannot reintroduce the problem by forgetting to inject. Tests that care
	// about the backend pass Dependencies.GitBackend explicitly.
	defaultGitBackend = func() git.Backend { return &gitConfigStub{} }

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
