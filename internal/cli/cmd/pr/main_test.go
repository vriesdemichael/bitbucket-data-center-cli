package prcmd

import (
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/git/gittest"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
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
//
// The environment is sealed first: the credentials and repository context a
// test process inherits -- from the shell, or from the .env the config layer
// loads itself -- are what tests used to clear one at a time with t.Setenv,
// which is the call that stops them running in parallel. It also turns the
// retry policy off, so a test whose subject is a failure stops sleeping
// through 750ms of backoff waiting for an answer it has already decided about.
func TestMain(m *testing.M) {
	testsupport.SealAmbientEnvironment()
	testsupport.SkipWindowsMousetrap()
	gittest.Guard(m)
}
