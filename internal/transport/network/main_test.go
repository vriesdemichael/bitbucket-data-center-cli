package network

import (
	"os"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// TestMain seals the process these tests run in (ADR-082).
//
// The transport reads BB_ERROR_HARVEST when it is built, so a harvest file
// exported in the developer's shell wrapped every transport these tests made
// and failed the ones that look at what was built. A test whose subject is the
// variable sets it itself.
func TestMain(m *testing.M) {
	os.Exit(testsupport.SealedMain(m))
}
