package cli

import (
	"net/http"
	"net/http/httptest"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// Two suites that drove commands against a hand-written Bitbucket are gone:
// one for auto-merge, auto-decline, labels, watch, default tasks and webhooks,
// and one for scoped build statuses, deployments and insight annotations.
//
// Both asserted the human line each command printed -- "Auto-merge enabled:
// true", "Repository-scoped build status ci/main set" -- against a payload the
// same file had written on the other side of the socket, so what they said is
// that the formatter and the fixture agreed. Every one of those command groups
// runs against a real instance in the live suite, and command-reach refuses to
// let any runnable command lose that.
//
// A third asserted that forty commands printed the word "dry-run" or something
// starting with a brace; TestAllCommandsExhaustivelyClassifiedForDryRun makes
// the first claim about every command without a server, and the output schemas
// are declared and checked against the live suite.

// mock-inventory: transport-fault — a server answering 500 to everything is injected, alongside a malformed URL and a malformed repository reference that never reach one; the subject is that each command reports rather than printing an empty success.
func TestNewCLICommandsErrorPaths(t *testing.T) {
	// 1. Client configuration failure (BITBUCKET_URL=://invalid)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "://invalid")
	t.Setenv("BITBUCKET_TOKEN", "test-token")

	errorCmds := [][]string{
		{"repo", "settings", "auto-merge", "get"},
		{"repo", "settings", "auto-merge", "set", "--enabled"},
		{"repo", "settings", "auto-merge", "delete"},
		{"repo", "settings", "auto-decline", "get"},
		{"repo", "settings", "auto-decline", "set", "--enabled", "--inactivity-weeks", "4"},
		{"repo", "settings", "auto-decline", "delete"},
		{"repo", "label", "list"},
		{"repo", "label", "add", "label3"},
		{"repo", "label", "remove", "label1"},
		{"repo", "watch"},
		{"repo", "unwatch"},
		{"repo", "default-task", "list"},
		{"repo", "default-task", "add", "task1"},
		{"repo", "default-task", "update", "123", "--description", "task1-updated"},
		{"repo", "default-task", "delete", "123"},
		{"webhook", "get", "1"},
		{"webhook", "update", "1", "--name", "hook1-updated"},
		{"webhook", "test", "1"},
		{"webhook", "stats", "1"},
		{"webhook", "stats", "1", "--summary"},
	}

	for _, args := range errorCmds {
		cmd := NewRootCommand()
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Errorf("expected error for command %v with invalid URL", args)
		}
	}

	// 2. Invalid repo format (e.g. --repo invalid)
	t.Setenv("BITBUCKET_URL", "http://localhost")
	for _, args := range errorCmds {
		fullArgs := append([]string(nil), args...)
		fullArgs = append(fullArgs, "--repo", "invalid")
		cmd := NewRootCommand()
		cmd.SetArgs(fullArgs)
		if err := cmd.Execute(); err == nil {
			t.Errorf("expected error for command %v with invalid repo format", fullArgs)
		}
	}

	// 3. Server error (HTTP 500)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "repo")

	for _, args := range errorCmds {
		cmd := NewRootCommand()
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Errorf("expected error for command %v with HTTP 500 response", args)
		}
	}

	// 4. Dry-run Server error / Permission check failure
	dryRunCmds := [][]string{
		{"repo", "settings", "auto-merge", "set", "--enabled", "--dry-run"},
		{"repo", "settings", "auto-merge", "delete", "--dry-run"},
		{"repo", "settings", "auto-decline", "set", "--enabled", "--inactivity-weeks", "4", "--dry-run"},
		{"repo", "settings", "auto-decline", "delete", "--dry-run"},
		{"repo", "label", "add", "label3", "--dry-run"},
		{"repo", "label", "remove", "label1", "--dry-run"},
		{"repo", "watch", "--dry-run"},
		{"repo", "unwatch", "--dry-run"},
		{"repo", "default-task", "add", "task1", "--dry-run"},
		{"repo", "default-task", "update", "123", "--description", "task1-updated", "--dry-run"},
		{"repo", "default-task", "delete", "123", "--dry-run"},
		{"webhook", "update", "1", "--name", "hook1-updated", "--dry-run"},
		{"webhook", "test", "1", "--dry-run"},
	}

	for _, args := range dryRunCmds {
		cmd := NewRootCommand()
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Errorf("expected error for dry-run command %v with HTTP 500 response", args)
		}
	}
}

// TestFlagValuesTheseCommandsRefuse covers the two arguments rejected before a
// request is built.
//
// It is what remains of TestNewCLICommandsDryRunAndJSON, which ran forty
// commands against a hand-written Bitbucket and checked that each printed the
// word "dry-run" or something starting with a brace. Neither says anything a
// stronger test does not already say without a server:
// TestAllCommandsExhaustivelyClassifiedForDryRun requires every command to
// declare a dry-run classification and fails on one that does not, the output
// schemas are declared and checked against the live suite, and command-reach
// requires every runnable command to be asserted by a live test at all.
//
// These two are different: they are the CLI refusing a value, which needs no
// server and produces no request.
func TestFlagValuesTheseCommandsRefuse(t *testing.T) {
	// A URL that does not resolve, so a command that got as far as a request
	// would fail for the wrong reason and the exit code would say so.
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "http://bitbucket.invalid")
	t.Setenv("BITBUCKET_TOKEN", "unused")
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "repo")

	cases := []struct {
		name string
		args []string
	}{
		{
			name: "webhook update --active takes a boolean",
			args: []string{"webhook", "update", "1", "--active", "invalid"},
		},
		{
			// Turning auto-decline on without saying after how long would
			// otherwise be sent to Bitbucket to reject, or worse accepted with
			// a default nobody chose.
			name: "auto-decline set needs an inactivity window",
			args: []string{"repo", "settings", "auto-decline", "set", "--enabled"},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := executeTestCLI(t, testCase.args...)
			if err == nil {
				t.Fatal("expected the value to be refused")
			}
			if code := apperrors.ExitCode(err); code != 2 {
				t.Errorf("exit code = %d, want 2 (validation): %v", code, err)
			}
		})
	}
}
