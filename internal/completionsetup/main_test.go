package completionsetup

import (
	"fmt"
	"os"
	"testing"
	"time"
)

// hangVariable makes this test binary a shell that never answers, for
// TestAShellThatDoesNotAnswerIsStopped to ask.
const hangVariable = "BB_COMPLETIONSETUP_TEST_HANG"

// modulePathVariable makes this test binary a shell that prints the
// PSModulePath it was given, for TestAShellIsAskedWithoutTheModulePathBbWasGiven.
const modulePathVariable = "BB_COMPLETIONSETUP_TEST_MODULE_PATH"

func TestMain(m *testing.M) {
	if os.Getenv(hangVariable) != "" {
		time.Sleep(time.Minute)
		os.Exit(1)
	}
	if os.Getenv(modulePathVariable) != "" {
		value, set := os.LookupEnv("PSModulePath")
		fmt.Printf("asked; PSModulePath set=%t %s\n", set, value)
		os.Exit(0)
	}

	os.Exit(m.Run())
}
