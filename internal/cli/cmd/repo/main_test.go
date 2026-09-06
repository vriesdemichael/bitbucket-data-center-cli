package repocmd

import (
	"os"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// TestMain clears the credentials and repository context the process might
// otherwise carry into a test.
//
// Tests used to do this one at a time, six t.Setenv(key, "") calls at the top
// of each, which is why so few of them could declare t.Parallel. The values
// they actually wanted -- a host, a token, a project and slug -- travel through
// testSetup instead, so the only thing left for the environment to say is
// nothing.
func TestMain(m *testing.M) {
	os.Exit(testsupport.SealedMain(m))
}
