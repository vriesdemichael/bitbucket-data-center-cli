package openapi

import (
	"os"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// TestMain seals the process these tests run in (ADR-082).
//
// The clients they build go through bb's transport, which reads
// BB_ERROR_HARVEST and BB_BLOCK_EXTERNAL_NETWORK from the environment, so a
// harvest file exported in the developer's shell changed how a cut-short reply
// was classified.
func TestMain(m *testing.M) {
	os.Exit(testsupport.SealedMain(m))
}
