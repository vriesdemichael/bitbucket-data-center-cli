package update

import (
	"bufio"
	"fmt"
	"os"
	"testing"
)

// runningBinaryVariable makes this test binary stand in for a bb that is
// running, for the tests that replace one: it says "running", answers each line
// it reads with "still running", and exits once its input closes.
const runningBinaryVariable = "BB_UPDATE_TEST_RUNNING_BINARY"

func TestMain(m *testing.M) {
	if os.Getenv(runningBinaryVariable) != "" {
		fmt.Println("running")
		lines := bufio.NewScanner(os.Stdin)
		for lines.Scan() {
			fmt.Println("still running")
		}
		os.Exit(0)
	}

	os.Exit(m.Run())
}
