package config

import (
	"os"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// TestTheSealWritesEveryVariableBBReads holds the ADR-082 seal to the source:
// every variable bb reads is one the seal writes, empty or set to a value of
// its own, so neither the developer's shell nor a .env reaches a unit test
// through it.
//
// The seal's list is kept by hand, and bb had come to read variables it left
// alone -- BITBUCKET_VERSION_TARGET, BB_REQUIRE_KEYRING, BB_CA_FILE and
// BB_REQUEST_TIMEOUT among them -- so a value exported in the shell decided
// what a test saw.
//
// It runs the seal twice. First with every variable absent, as a .env finds
// them: godotenv fills in any name the environment does not carry, so the seal
// has to write each one, not only those that happen to be set. Then with every
// variable set, as a shell hands them over.
func TestTheSealWritesEveryVariableBBReads(t *testing.T) {
	read := variablesInSource(t)
	for name := range variableReadByALibrary {
		read[name] = true
	}

	for name := range read {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatalf("unset %s: %v", name, err)
		}
	}
	testsupport.SealAmbientEnvironment()
	for _, name := range sortedKeys(read) {
		if _, set := os.LookupEnv(name); !set {
			t.Errorf("bb reads %s and the seal leaves it unset, for a .env to fill in; "+
				"add it to ambientSettings in internal/testsupport/ambient.go", name)
		}
	}

	const fromTheShell = "from the developer's shell"
	for name := range read {
		t.Setenv(name, fromTheShell)
	}
	testsupport.SealAmbientEnvironment()
	for _, name := range sortedKeys(read) {
		if os.Getenv(name) == fromTheShell {
			t.Errorf("bb reads %s and the seal leaves it as the shell set it; "+
				"add it to ambientSettings in internal/testsupport/ambient.go", name)
		}
	}
}
