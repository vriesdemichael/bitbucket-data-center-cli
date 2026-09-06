package testsupport

import (
	"os"
	"testing"

	"github.com/spf13/cobra"
)

// ambientSettings are the variables a unit test must not inherit.
//
// The config layer reads all of them, and two things put them in a test
// process without any test asking: the developer's shell, and the .env the
// live suite is configured with -- config.LoadWithOverrides loads .env itself,
// walking up from the working directory, so `go test ./...` in this checkout
// sees whatever credentials the local Bitbucket runs with.
//
// The result was a suite whose behaviour depended on the machine it ran on,
// and tests that defended against it one at a time with t.Setenv(key, "") --
// which is the call that disqualifies a test from t.Parallel. Clearing them
// once for the process is the same defence without that cost, and it is a
// stronger one: it also covers the tests that never thought to defend.
var ambientSettings = []string{
	"BITBUCKET_URL",
	"BITBUCKET_TOKEN",
	"BITBUCKET_USERNAME",
	"BITBUCKET_USER",
	"BITBUCKET_PASSWORD",
	"BITBUCKET_PROJECT_KEY",
	"BITBUCKET_REPO_SLUG",
	"ADMIN_USER",
	"ADMIN_PASSWORD",
	"BB_CONFIG_PATH",
	"BB_SYSTEM_CONFIG_PATH",
	"BB_WORKSPACE_CONFIG_PATH",
	"BB_LOG_LEVEL",
	"BB_LOG_FORMAT",
	"NO_COLOR",
}

// SealAmbientEnvironment empties the settings a unit test must not inherit and
// turns the stored config off, for the whole test binary.
//
// Called from TestMain, before any test runs. Process-wide is safe here in a
// way it never was per test: these are set once and never changed, so no test
// can observe another's value, and nothing has to be restored afterwards.
//
// A variable is emptied rather than unset because .env is loaded with
// godotenv, which fills in only names the environment does not already carry.
// An unset BITBUCKET_PASSWORD would be supplied by .env on the first config
// load; an empty one stays empty.
func SealAmbientEnvironment() {
	for _, key := range ambientSettings {
		_ = os.Setenv(key, "")
	}
	_ = os.Setenv("BB_DISABLE_STORED_CONFIG", "1")

	// No retries.
	//
	// The shipped policy is two, at 250ms and then 500ms, which is right for a
	// user whose server blinked and wrong for a test whose subject is the
	// failure: it waits 750ms for an answer it has already decided about.
	// Whole suites were spending their time here -- one clone test with a stub
	// git backend and no network anywhere took 830ms, and 750 of them were
	// sleep. A test that means to check the retrying sets its own count.
	_ = os.Setenv("BB_RETRY_COUNT", "0")
}

// SealedMain is TestMain for a package whose tests configure the CLI by
// passing it values rather than by publishing them.
func SealedMain(m *testing.M) int {
	SealAmbientEnvironment()
	SkipWindowsMousetrap()

	return m.Run()
}

// SkipWindowsMousetrap stops cobra checking whether the binary was launched
// from Explorer.
//
// The check exists so a double-clicked CLI can say it belongs in a terminal.
// It answers by walking the process table through CreateToolhelp32Snapshot to
// find the parent, which costs about 28ms per Execute on this host -- paid by
// every command a test runs, and paid nowhere else, since a test binary is
// never started by Explorer. One suite of sixty invocations spent 2.2 of its
// 3.1 seconds here.
//
// Setting the help text to empty is cobra's own way of turning it off.
func SkipWindowsMousetrap() {
	cobra.MousetrapHelpText = ""
}
