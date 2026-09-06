package cli

import (
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/git/gittest"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// TestMain seals the environment these tests run in and then fails the package
// if they reconfigure the repository they run inside.
//
// The seal empties the credentials and repository context the process might
// carry -- from the developer's shell or from the .env the live suite uses,
// which the config layer loads itself -- so a test says what it wants rather
// than defending against what it inherited. Tests defended one at a time with
// t.Setenv(key, ""), and that call is what disqualifies a test from
// t.Parallel.
//
// The git guard stays: these tests drive commands such as `bb repo clone`,
// which reach the git backend and can write repository-scoped configuration.
func TestMain(m *testing.M) {
	testsupport.SealAmbientEnvironment()
	testsupport.SkipWindowsMousetrap()
	gittest.Guard(m)
}
