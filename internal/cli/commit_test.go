package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func executeTestCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()

	// Disable colors so assertions can match plain text without ANSI codes.
	t.Setenv("NO_COLOR", "1")

	command := NewRootCommand()
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs(args)

	err := command.Execute()
	return output.String(), err
}

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

func TestCommitCLICommandsMock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/commits":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":"abc","displayId":"abc","message":"init"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/commits/abc":
			_, _ = w.Write([]byte(`{"id":"abc","displayId":"abc","message":"init"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/compare/commits":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":"def","displayId":"def","message":"feature"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/branches":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":"refs/heads/main","displayId":"main","type":"BRANCH"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/tags":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":"refs/tags/v1.0","displayId":"v1.0","type":"TAG"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "repo")
	t.Setenv("BITBUCKET_TOKEN", "test-token")

	// Commit list
	out, err := executeTestCLI(t, "commit", "list")
	if err != nil {
		t.Fatalf("commit list failed: %v", err)
	}
	if !strings.Contains(out, "abc") || !strings.Contains(out, "init") {
		t.Fatalf("unexpected list output: %s", out)
	}

	out, err = executeTestCLI(t, "--json", "commit", "list")
	if err != nil {
		t.Fatalf("commit list json failed: %v", err)
	}
	if !strings.Contains(out, `"commits"`) {
		t.Fatalf("unexpected list json output: %s", out)
	}

	// Commit get
	out, err = executeTestCLI(t, "commit", "get", "abc")
	if err != nil {
		t.Fatalf("commit get failed: %v", err)
	}
	if !strings.Contains(out, "Commit: abc") {
		t.Fatalf("unexpected get output: %s", out)
	}

	out, err = executeTestCLI(t, "--json", "commit", "get", "abc")
	if err != nil {
		t.Fatalf("commit get json failed: %v", err)
	}
	if !strings.Contains(out, `"commit"`) {
		t.Fatalf("unexpected get json output: %s", out)
	}

	// Commit compare
	out, err = executeTestCLI(t, "commit", "compare", "abc", "def")
	if err != nil {
		t.Fatalf("commit compare failed: %v", err)
	}
	if !strings.Contains(out, "def") || !strings.Contains(out, "feature") {
		t.Fatalf("unexpected compare output: %s", out)
	}

	out, err = executeTestCLI(t, "--json", "commit", "compare", "abc", "def")
	if err != nil {
		t.Fatalf("commit compare json failed: %v", err)
	}
	if !strings.Contains(out, `"commits"`) {
		t.Fatalf("unexpected compare json output: %s", out)
	}

	// Ref list
	out, err = executeTestCLI(t, "ref", "list")
	if err != nil {
		t.Fatalf("ref list failed: %v", err)
	}
	if !strings.Contains(out, "main") || !strings.Contains(out, "BRANCH") || !strings.Contains(out, "refs/heads/main") {
		t.Fatalf("unexpected ref list output: %s", out)
	}

	out, err = executeTestCLI(t, "--json", "ref", "list")
	if err != nil {
		t.Fatalf("ref list json failed: %v", err)
	}
	if !strings.Contains(out, `"refs"`) {
		t.Fatalf("unexpected ref list json output: %s", out)
	}

	// Ref resolve
	out, err = executeTestCLI(t, "ref", "resolve", "main")
	if err != nil {
		t.Fatalf("ref resolve failed: %v", err)
	}
	if !strings.Contains(out, "main") || !strings.Contains(out, "BRANCH") || !strings.Contains(out, "refs/heads/main") {
		t.Fatalf("unexpected ref resolve output: %s", out)
	}

	out, err = executeTestCLI(t, "--json", "ref", "resolve", "main")
	if err != nil {
		t.Fatalf("ref resolve json failed: %v", err)
	}
	if !strings.Contains(out, `"ref"`) {
		t.Fatalf("unexpected ref resolve json output: %s", out)
	}

	// Ref resolve not found
	_, err = executeTestCLI(t, "ref", "resolve", "missing")
	if err == nil {
		t.Fatalf("expected error for missing ref")
	}
}

func TestCommitListEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"isLastPage":true,"values":[]}`))
	}))
	t.Cleanup(server.Close)

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "repo")
	t.Setenv("BITBUCKET_TOKEN", "test-token")

	out, err := executeTestCLI(t, "commit", "list")
	if err != nil {
		t.Fatalf("commit list empty failed: %v", err)
	}
	if !strings.Contains(out, "No commits found") {
		t.Fatalf("expected empty-state message, got: %s", out)
	}
}

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

