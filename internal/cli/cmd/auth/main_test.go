package auth

import (
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/git/gittest"
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
func TestMain(m *testing.M) { gittest.Guard(m) }

// unreachableTimeout is what a test gives a host that does not exist.
//
// These tests point at names like example.local to assert on what bb does when
// a host cannot be reached. Resolving one costs 2.8 seconds on a machine whose
// resolver waits, and two subtests of TestAuthCommandAdditionalBranches spent
// 5.6 seconds of the package's 14 doing nothing but that. The assertion is
// about the failure, not about how long the failure takes.
const unreachableTimeout = 50 * time.Millisecond
