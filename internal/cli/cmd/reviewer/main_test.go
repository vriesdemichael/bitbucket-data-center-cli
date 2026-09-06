package reviewercmd

import (
	"os"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// TestMain seals the process these tests run in.
//
// It empties the credentials and repository context the environment might
// carry -- the developer's shell, or the .env the config layer loads itself --
// so a test says what it wants instead of clearing what it inherited with
// t.Setenv, which is the call that stops it running in parallel. It turns the
// retry policy off, so a test whose subject is a failure does not sleep
// through 750ms of backoff first. And it turns off cobra's Explorer check,
// which walks the Windows process table on every Execute.
func TestMain(m *testing.M) {
	os.Exit(testsupport.SealedMain(m))
}
