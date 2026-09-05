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

// mock-inventory: canned-response — a linked Jira is what produces a non-empty answer and the live stack has none, so the payload is written here; the subject is that both renderings show the commit. TestLiveJiraIssueCommitsAnswerEmpty covers the empty answer a real instance gives.
func TestCommitListJira(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/rest/jira/latest/issues/ISSUE-123/commits" {
			_, _ = w.Write([]byte(`{
				"isLastPage": true,
				"values": [
					{
						"toCommit": {
							"id": "jiracommit1",
							"displayId": "jc1",
							"message": "fix for issue 123"
						}
					}
				]
			}`))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "repo")
	t.Setenv("BITBUCKET_TOKEN", "test-token")

	out, err := executeTestCLI(t, "commit", "list", "--jira", "ISSUE-123")
	if err != nil {
		t.Fatalf("commit list --jira failed: %v", err)
	}
	if !strings.Contains(out, "jc1") || !strings.Contains(out, "fix for issue 123") {
		t.Fatalf("unexpected list output: %s", out)
	}

	out, err = executeTestCLI(t, "--json", "commit", "list", "--jira", "ISSUE-123")
	if err != nil {
		t.Fatalf("commit list --jira json failed: %v", err)
	}
	if !strings.Contains(out, `"commits"`) || !strings.Contains(out, "jiracommit1") {
		t.Fatalf("unexpected json output: %s", out)
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
