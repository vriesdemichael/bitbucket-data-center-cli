package completionsetup

import (
	"os"
	"testing"
	"time"
)

// hangVariable makes this test binary a shell that never answers, for
// TestAShellThatDoesNotAnswerIsStopped to ask.
const hangVariable = "BB_COMPLETIONSETUP_TEST_HANG"

func TestMain(m *testing.M) {
	if os.Getenv(hangVariable) != "" {
		time.Sleep(time.Minute)
		os.Exit(1)
	}

	os.Exit(m.Run())
}
