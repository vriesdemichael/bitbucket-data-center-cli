package config

import (
	"os"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// TestMain seals this package too, per ADR-082.
//
// It was tempting to exempt it: the seal disables the stored config, and this
// is the package that loads it. But the exemption turned out to be needed by
// exactly one test, and that test already writes its own fixtures into
// t.TempDir() and points BB_CONFIG_PATH at them -- so it re-enables the stored
// config for itself and reaches those, rather than the whole package reaching
// whatever the developer happens to have logged into.
//
// Sealing also fixed two tests that were failing here on any machine with a
// stored config: TestLoadFromEnvSystemCAFile and
// TestAnInferredContextReachesTheResolvedConfiguration, neither of which has
// stored credentials as its subject and neither of which had thought to say so.
func TestMain(m *testing.M) {
	os.Exit(testsupport.SealedMain(m))
}
