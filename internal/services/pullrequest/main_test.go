package pullrequest

import (
	"os"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// TestMain seals the process per ADR-082.
//
// No test here loads the configuration: each hands its client the address of
// the server it talks to. They used to load it from BITBUCKET_URL instead, and
// the config layer answered with more than they asked for -- the stored config
// too. On a machine where someone had run `bb auth login`, that supplied a
// username whose password lives in the keyring, and validation rejected the
// pair, so seven tests failed for everyone who uses the tool they are
// developing and passed on CI, which has no stored config. The seal stays so a
// test that starts loading it again cannot bring that back.
func TestMain(m *testing.M) {
	os.Exit(testsupport.SealedMain(m))
}
