package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCommitCLICommandValidation(t *testing.T) {
	output, err := executeTestCLI(t, "commit", "get")
	if err == nil {
		t.Fatal("expected error for empty commit get")
	}
	if !strings.Contains(err.Error(), "accepts 1 arg(s)") {
		t.Fatalf("expected arg validation error, got: %v (output: %s)", err, output)
	}

	_, err = executeTestCLI(t, "commit", "compare", "abc")
	if err == nil {
		t.Fatal("expected error for compare missing arg")
	}

	_, err = executeTestCLI(t, "ref", "resolve")
	if err == nil {
		t.Fatal("expected error for resolve missing arg")
	}
}

// mock-inventory: transport-fault — a server answering 500 is injected; the subject is that the failure reaches the caller rather than reading as an issue with no commits.
func TestCommitListJiraError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"jira error"}`))
	}))
	t.Cleanup(server.Close)

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "repo")
	t.Setenv("BITBUCKET_TOKEN", "test-token")

	_, err := executeTestCLI(t, "commit", "list", "--jira", "ISSUE-123")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// TestCommitListJira is gone rather than moved. Reaching a non-empty answer
// needs a Jira linked to Bitbucket, which this stack has not got, so the
// payload had to be written here -- and what it then asserted was that the
// commit renderer prints a commit, which every other `commit list` in the
// live suite already asks of real ones. The one thing that was particular to
// this endpoint, unwrapping toCommit, is asserted where it happens, in
// internal/services/jira.
