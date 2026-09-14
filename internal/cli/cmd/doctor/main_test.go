package doctorcmd

import (
	"os"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// TestMain seals the process these tests run in (ADR-082). The tests hand the
// command a diagnosis rather than a configuration, so nothing they check should
// depend on the machine they run on.
func TestMain(m *testing.M) {
	os.Exit(testsupport.SealedMain(m))
}
