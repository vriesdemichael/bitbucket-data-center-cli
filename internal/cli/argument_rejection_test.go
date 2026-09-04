package cli

import (
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// Arguments a command can reject on its own, rejected without a server.
//
// These replace the error half of governance_test.go, which asserted the same
// rejections against a mock answering one status to every request. That mock
// could not tell a local rejection from a remote one: the command was called
// with a bad argument, an error came back, and the test was satisfied either
// way -- including in the case where bb had dutifully sent nonsense to
// Bitbucket and repeated the complaint.
//
// Pointing BITBUCKET_URL at a closed port is the whole assertion. Anything that
// reaches the network fails with a connection error and a transient kind, so a
// validation kind is proof the argument never left the process, which is what
// ADR-054 asks for.
const unreachableBitbucket = "http://127.0.0.1:1"

func configureUnreachableEnv(t *testing.T) {
	t.Helper()

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", unreachableBitbucket)
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")
	t.Setenv("BITBUCKET_TOKEN", "test-token")
	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("BITBUCKET_PASSWORD", "")
}

func TestCommandsRejectBadArgumentsWithoutCallingBitbucket(t *testing.T) {
	cases := []struct {
		name string
		args []string
		// wantMessage is a fragment the rejection must carry. For an enum that
		// is the list of values, because a rejection that does not say what was
		// allowed sends the caller back to the documentation.
		wantMessage string
	}{
		{
			name:        "project permission outside the allowed set",
			args:        []string{"project", "permissions", "users", "grant", "PRJ", "u1", "INVALID"},
			wantMessage: "PROJECT_READ, PROJECT_WRITE, PROJECT_ADMIN",
		},
		{
			name:        "repository permission outside the allowed set",
			args:        []string{"repo", "settings", "security", "permissions", "users", "grant", "u1", "INVALID"},
			wantMessage: "REPO_READ",
		},
		{
			name:        "merge strategy outside the allowed set",
			args:        []string{"repo", "settings", "pull-requests", "set-strategy", "nonsense"},
			wantMessage: "must be one of",
		},
		{
			name: "reviewer condition payload that is not JSON",
			args: []string{"reviewer", "condition", "create", "{invalid}", "--project", "PRJ"},
		},
		{
			name: "reviewer condition update payload that is not JSON",
			args: []string{"reviewer", "condition", "update", "1", "{invalid}", "--project", "PRJ"},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			configureUnreachableEnv(t)

			output, err := executeTestCLI(t, testCase.args...)
			if err == nil {
				t.Fatalf("expected the argument to be rejected, got:\n%s", output)
			}

			if code := apperrors.ExitCode(err); code != 2 {
				t.Fatalf("exit code = %d, want 2 (validation); a network exit code means the "+
					"argument was sent to Bitbucket rather than rejected here (%v)", code, err)
			}

			if testCase.wantMessage != "" && !strings.Contains(err.Error(), testCase.wantMessage) {
				t.Errorf("rejection does not name what was allowed: %v", err)
			}
		})
	}
}
