package testsupport

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

// ambientSettings are the variables a unit test must not inherit: every
// variable bb reads, but the four SealAmbientEnvironment sets.
//
// Two things put them in a test process without any test asking: the
// developer's shell, and the .env the live suite is configured with --
// config.LoadWithOverrides loads .env itself, walking up from the working
// directory, so `go test ./...` in this checkout sees whatever credentials the
// local Bitbucket runs with.
//
// The result was a suite whose behaviour depended on the machine it ran on,
// and tests that defended against it one at a time with t.Setenv(key, "") --
// which is the call that disqualifies a test from t.Parallel. Clearing them
// once for the process is the same defence without that cost, and it is a
// stronger one: it also covers the tests that never thought to defend.
//
// bb reads an empty value of each as it reads an unset one, but NO_COLOR,
// which it takes as set whatever the value, so a sealed test renders without
// colour on every machine. TestTheSealWritesEveryVariableBBReads, in
// internal/config, fails when bb reads a BB_ or BITBUCKET_ name this list
// leaves out. ADMIN_USER, ADMIN_PASSWORD and NO_COLOR it cannot see, so they
// are kept here by hand; the interactivity names have a guard of their own.
var ambientSettings = []string{
	// Connection and credentials.
	"BITBUCKET_URL",
	"BITBUCKET_VERSION_TARGET",
	"BITBUCKET_TOKEN",
	"BITBUCKET_USERNAME",
	"BITBUCKET_USER",
	"BITBUCKET_PASSWORD",
	"ADMIN_USER",
	"ADMIN_PASSWORD",
	"BB_REQUIRE_KEYRING",
	"BB_REQUEST_TIMEOUT",
	"BB_RETRY_BACKOFF",

	// TLS.
	"BB_CA_FILE",
	"BB_CLIENT_CERT",
	"BB_CLIENT_KEY",
	"BB_INSECURE_SKIP_VERIFY",

	// Repository context.
	"BITBUCKET_PROJECT_KEY",
	"BITBUCKET_REPO_SLUG",

	// Configuration files.
	"BB_SYSTEM_CONFIG_PATH",
	"BB_WORKSPACE_CONFIG_PATH",

	// Webhook credentials.
	"BB_WEBHOOK_SECRET",
	"BB_WEBHOOK_PASSWORD",

	// Interactivity. The names after the two of bb's own are the ones
	// interactive.Detect takes as "nobody is there"; set, they change the
	// reason a refusal gives, so a test saw "CI is set" on a runner and
	// "CLAUDECODE is set" under Claude Code.
	// TestTheSealEmptiesWhatSaysNobodyIsThere, in internal/cli/interactive,
	// holds this to that list.
	// TERM is left alone: only "dumb" means anything to bb, and git and the
	// renderer read it too.
	"BB_NO_PROMPT",
	"BB_NO_PROMPT_VARS",
	"CI",
	"DEBIAN_FRONTEND",
	"NONINTERACTIVE",
	"CLAUDECODE",
	"AI_AGENT",
	"CURSOR_TRACE_ID",
	"CODEX_THREAD_ID",
	"REPLIT_ENVIRONMENT",
	"AIDER_CHAT",

	// Updates.
	"BB_DISABLE_UPDATE",
	"BB_UPDATE_BASE_URL",
	"BB_ALLOW_HTTP_UPDATE",

	// Output and diagnostics.
	"BB_LOG_LEVEL",
	"BB_LOG_FORMAT",
	"NO_COLOR",
	"BB_ERROR_HARVEST",

	// Shell completion. Cobra reads BB_ACTIVE_HELP, from the root command's
	// name.
	"BB_COMPLETION_TIMEOUT",
	"BB_COMPLETION_DEBUG",
	"BB_ACTIVE_HELP",
}

// SealAmbientEnvironment empties the settings a unit test must not inherit and
// turns the stored config off, for the whole test binary.
//
// Called from TestMain, before any test runs. Process-wide is safe here in a
// way it never was per test: these are set once and never changed, so no test
// can observe another's value, and nothing has to be restored afterwards.
// Sealing a sealed process again writes the same values and keeps its
// configuration directory.
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

	// The user's own configuration file is out of reach.
	//
	// An empty BB_CONFIG_PATH is not "no file": it is bb's default path, the
	// developer's own configuration. BB_DISABLE_STORED_CONFIG keeps the
	// configuration load from reading it, but a command that writes a login --
	// bb auth login, the token a clone asks for -- writes there all the same,
	// and a test that lists stored hosts reads the developer's. Pointing it at a
	// directory of the process's own keeps every such test away from the file,
	// whichever command it runs. A test that needs a configuration of its own
	// still sets BB_CONFIG_PATH itself.
	if sealedConfigDirectory == "" {
		directory, err := os.MkdirTemp("", "bb-test-config-")
		if err != nil {
			panic(fmt.Sprintf("seal the test environment: make a configuration directory of its own: %v", err))
		}
		sealedConfigDirectory = directory
	}
	_ = os.Setenv("BB_CONFIG_PATH", filepath.Join(sealedConfigDirectory, "config.yaml"))

	// No retries.
	//
	// The shipped policy is two, at 250ms and then 500ms, which is right for a
	// user whose server blinked and wrong for a test whose subject is the
	// failure: it waits 750ms for an answer it has already decided about.
	// Whole suites were spending their time here -- one clone test with a stub
	// git backend and no network anywhere took 830ms, and 750 of them were
	// sleep. A test that means to check the retrying sets its own count.
	_ = os.Setenv("BB_RETRY_COUNT", "0")

	// No network beyond this machine (ADR-029).
	//
	// The transport refuses a host that is not loopback when this is set, and
	// it was set by the packages that thought of it. cmd/bb did not, and its
	// walk of every command ran `bb update` for real: twice, under --json and
	// --yaml, against GitHub's release API, comparing the two answers. When
	// one of the two requests failed the documents differed and the walk was
	// red for a reason that had nothing to do with the tree -- and when both
	// succeeded it had downloaded a release and installed it over the test
	// binary.
	_ = os.Setenv("BB_BLOCK_EXTERNAL_NETWORK", "1")
}

// SealedMain is TestMain for a package whose tests configure the CLI by
// passing it values rather than by publishing them.
func SealedMain(m *testing.M) int {
	SealAmbientEnvironment()
	SkipWindowsMousetrap()

	code := m.Run()
	_ = os.RemoveAll(sealedConfigDirectory)

	return code
}

// sealedConfigDirectory holds the configuration file BB_CONFIG_PATH names in a
// sealed process.
var sealedConfigDirectory string

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
