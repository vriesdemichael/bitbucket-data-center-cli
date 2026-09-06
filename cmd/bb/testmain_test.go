package main

import (
	"os"
	"testing"

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
func TestMain(m *testing.M) {
	os.Exit(testsupport.SealedMain(m))
}
