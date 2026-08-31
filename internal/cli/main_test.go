package cli

import (
	"testing"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/git/gittest"
)

// TestMain fails this package when its tests reconfigure the repository they
// run inside. These tests drive commands such as `bb repo clone`, which reach
// the git backend and can write repository-scoped configuration.
func TestMain(m *testing.M) { gittest.Guard(m) }
