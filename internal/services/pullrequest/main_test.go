package pullrequest

import (
	"os"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// TestMain seals the process per ADR-082.
//
// These tests call config.LoadFromEnv after setting only BITBUCKET_URL, and the
// config layer answers with more than the test asked for: it reads the stored
// config too. On a machine where someone has run `bb auth login`, that supplied
// a username whose password lives in the keyring, and validation rejected the
// pair -- so seven tests failed for everyone who actually uses the tool they
// are developing, and passed on CI, which has no stored config.
//
// The pre-commit hook runs the unit suite, so that was not a slow test. It was
// a contributor unable to commit until they exported a variable by hand.
func TestMain(m *testing.M) {
	os.Exit(testsupport.SealedMain(m))
}
