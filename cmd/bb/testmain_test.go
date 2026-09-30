package main

import (
	"os"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/git/gittest"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// TestMain seals the process this binary's tests run in.
//
// The credentials and repository context a test process inherits -- from the
// developer's shell, or from the .env the config layer loads itself while
// walking up from the working directory -- decided what these tests saw. They
// defended against it one at a time with t.Setenv(key, ""), which is the call
// that stops a test declaring itself parallel.
//
// It also turns the retry policy off, so a test whose subject is a failure
// stops sleeping through 750ms of backoff first, and turns off cobra's
// Explorer check, which walks the Windows process table on every Execute.
//
// A git one of these tests starts runs this binary as its credential helper,
// because the helper line bb writes names the executable it was written by.
// That run is bb, not the tests, and it keeps the environment git gave it.
// Starting git is also why the tests run under the ambient-config guard.
func TestMain(m *testing.M) {
	if os.Getenv(actAsBB) == "1" {
		os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
	}

	testsupport.SealAmbientEnvironment()
	testsupport.SkipWindowsMousetrap()
	gittest.Guard(m)
}
