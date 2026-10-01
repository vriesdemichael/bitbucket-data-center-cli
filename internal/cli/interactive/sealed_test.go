package interactive

import (
	"os"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// TestMain seals the process these tests run in (ADR-082).
func TestMain(m *testing.M) {
	os.Exit(testsupport.SealedMain(m))
}

// TestTheSealEmptiesWhatSaysNobodyIsThere holds the seal to the names Detect
// takes as "nobody is there".
//
// A command a test runs asks Detect with the process environment, so each of
// these set by a runner or a coding harness changed the reason a refusal gave:
// "CI is set" on one machine, "CLAUDECODE is set" on another. A name added to
// the list here and not to the seal would do it again.
func TestTheSealEmptiesWhatSaysNobodyIsThere(t *testing.T) {
	names := append([]string{disableVariable, extensionVariable}, knownNonInteractive...)
	for _, name := range names {
		t.Setenv(name, "1")
	}

	testsupport.SealAmbientEnvironment()

	for _, name := range names {
		if truthy(os.LookupEnv, name) {
			t.Errorf("%s is still set after the seal; add it to ambientSettings in internal/testsupport/ambient.go", name)
		}
	}
}
