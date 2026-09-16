package releasetags_test

import (
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/git/gittest"
)

// TestMain fails this package when its tests reconfigure the repository they
// run inside (ADR-071). The test below builds a repository of its own and tags
// it, and a git command that reached this project instead would tag the real
// history -- which is the one thing a package about release tags must not do.
func TestMain(m *testing.M) { gittest.Guard(m) }
