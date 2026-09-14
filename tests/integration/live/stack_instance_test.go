//go:build live

package live_test

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
)

// stackInstanceFile is where scripts/stack.sh up records this checkout's own
// Bitbucket instance: its URL, whose port Docker assigns in a linked worktree,
// and its container.
const stackInstanceFile = ".tmp/bitbucket.env"

// The variables the suite keeps the instance under once it has read the file.
// They are set on the process, so a test that clears BITBUCKET_URL can still
// find the instance it was pointed at.
const (
	stackInstanceURLVariable       = "BB_STACK_URL"
	stackInstanceContainerVariable = "BB_STACK_CONTAINER"
)

// applyStackInstanceToProcess points the suite at this checkout's instance.
//
// It runs before the defaults, so http://localhost:7990 is what a checkout
// without an instance file gets, not what every worktree gets. An explicit
// BITBUCKET_URL still wins: the suite runs against other servers too.
func applyStackInstanceToProcess() {
	instance, ok := readStackInstance()
	if !ok {
		return
	}

	if strings.TrimSpace(os.Getenv("BITBUCKET_URL")) == "" {
		_ = os.Setenv("BITBUCKET_URL", instance["BITBUCKET_URL"])
	}
	_ = os.Setenv(stackInstanceURLVariable, instance["BITBUCKET_URL"])
	_ = os.Setenv(stackInstanceContainerVariable, instance["BB_STACK_CONTAINER"])
}

// readStackInstance reads the instance file from the repository root, found by
// walking up from the working directory to go.mod.
func readStackInstance() (map[string]string, bool) {
	directory, err := os.Getwd()
	if err != nil {
		return nil, false
	}

	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return nil, false
		}
		directory = parent
	}

	values, err := godotenv.Read(filepath.Join(directory, filepath.FromSlash(stackInstanceFile)))
	if err != nil || strings.TrimSpace(values["BITBUCKET_URL"]) == "" {
		return nil, false
	}

	return values, true
}

// liveInstanceURL is the server the suite was pointed at, without a trailing
// slash. Tests that log in from a clean environment read it before clearing
// BITBUCKET_URL, so the login reaches this checkout's instance rather than a
// port written into the test.
func liveInstanceURL() string {
	return strings.TrimRight(strings.TrimSpace(os.Getenv("BITBUCKET_URL")), "/")
}
