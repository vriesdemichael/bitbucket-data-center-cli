package execgit_test

import (
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/git/gittest"
)

// TestMain fails this package when its tests reconfigure the repository they
// run inside. Clone persists http.extraHeader into the repository it clones
// into, so a target directory that is not a temporary one silently rewrites the
// project's own git configuration. That happened, and it broke authentication
// for every push until someone noticed months later.
func TestMain(m *testing.M) { gittest.Guard(m) }
