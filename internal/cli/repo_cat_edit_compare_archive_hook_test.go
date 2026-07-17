package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestRepoCatEditCLI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/raw/path/to/file.txt":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("hello raw file"))
		case r.Method == http.MethodPut && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/browse/path/to/file.txt":
			_ = r.ParseMultipartForm(10 * 1024)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"abc1234","displayId":"abc1234"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_TOKEN", "test-token")
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "repo")

	// Test cat
	out, err := executeTestCLI(t, "repo", "cat", "path/to/file.txt")
	if err != nil {
		t.Fatalf("cat failed: %v", err)
	}
	if out != "hello raw file" {
		t.Fatalf("unexpected cat output: %s", out)
	}

	// Test cat json
	out, err = executeTestCLI(t, "--json", "repo", "cat", "path/to/file.txt")
	if err != nil {
		t.Fatalf("cat json failed: %v", err)
	}
	if !strings.Contains(out, `"content": "hello raw file"`) {
		t.Fatalf("unexpected cat json: %s", out)
	}

	// Test edit
	out, err = executeTestCLI(t, "repo", "edit", "path/to/file.txt", "--content", "new content", "--message", "updating file")
	if err != nil {
		t.Fatalf("edit failed: %v", err)
	}
	if !strings.Contains(out, "Successfully edited path/to/file.txt in commit abc1234") {
		t.Fatalf("unexpected edit output: %s", out)
	}

	// Test edit JSON
	out, err = executeTestCLI(t, "--json", "repo", "edit", "path/to/file.txt", "--content", "new content", "--message", "updating file")
	if err != nil {
		t.Fatalf("edit json failed: %v", err)
	}
	if !strings.Contains(out, `"id": "abc1234"`) {
		t.Fatalf("unexpected edit json output: %s", out)
	}
}

func TestRepoCompareCLI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/compare/changes":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"type":"MODIFY","path":{"components":["file.txt"],"name":"file.txt"}}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/compare/diff":
			_, _ = w.Write([]byte(`{"binary":false,"hunks":[{"sourceLine":1,"sourceSpan":1,"destinationLine":1,"destinationSpan":1,"context":"header","segments":[{"type":"ADDED","lines":[{"line":"added line"}]}]}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_TOKEN", "test-token")
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "repo")

	// Test compare changes table
	out, err := executeTestCLI(t, "repo", "compare", "main", "feat")
	if err != nil {
		t.Fatalf("compare changes failed: %v", err)
	}
	if !strings.Contains(out, "file.txt") || !strings.Contains(out, "MODIFY") {
		t.Fatalf("unexpected compare changes: %s", out)
	}

	// Test compare changes JSON
	out, err = executeTestCLI(t, "--json", "repo", "compare", "main", "feat")
	if err != nil {
		t.Fatalf("compare changes json failed: %v", err)
	}
	if !strings.Contains(out, `"changes"`) || !strings.Contains(out, `"MODIFY"`) {
		t.Fatalf("unexpected compare changes json: %s", out)
	}

	// Test compare diff
	out, err = executeTestCLI(t, "repo", "compare", "main", "feat", "--diff")
	if err != nil {
		t.Fatalf("compare diff failed: %v", err)
	}
	if !strings.Contains(out, "@@ -1,1 +1,1 @@ header") || !strings.Contains(out, "+added line") {
		t.Fatalf("unexpected compare diff output: %s", out)
	}
}

func TestRepoArchiveCLI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/archive" {
			w.Header().Set("Content-Type", "application/zip")
			_, _ = w.Write([]byte("fake zip data"))
		} else {
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_TOKEN", "test-token")
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "repo")

	// Test archive default file creation
	defer os.Remove("repo.zip")
	out, err := executeTestCLI(t, "repo", "archive")
	if err != nil {
		t.Fatalf("archive default failed: %v", err)
	}
	if !strings.Contains(out, "Successfully downloaded repository archive") {
		t.Fatalf("unexpected archive output: %s", out)
	}

	data, err := os.ReadFile("repo.zip")
	if err != nil || string(data) != "fake zip data" {
		t.Fatalf("repo.zip content mismatch: %s (err: %v)", string(data), err)
	}

	// Test archive stdout
	out, err = executeTestCLI(t, "repo", "archive", "-o", "-")
	if err != nil {
		t.Fatalf("archive stdout failed: %v", err)
	}
	if out != "fake zip data" {
		t.Fatalf("unexpected stdout content: %s", out)
	}
}

func TestRepoHookScriptCLI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/hook-scripts":
			_, _ = w.Write([]byte(`{"values":[{"script":{"id":123,"name":"my-script","description":"test hook"},"triggerIds":["trigger1"]}],"isLastPage":true}`))
		case r.Method == http.MethodPut && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/hook-scripts/123":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/hook-scripts/123":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_TOKEN", "test-token")
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "repo")

	// Test list
	out, err := executeTestCLI(t, "repo", "hook-script", "list")
	if err != nil {
		t.Fatalf("hook-script list failed: %v", err)
	}
	if !strings.Contains(out, "123") || !strings.Contains(out, "my-script") || !strings.Contains(out, "trigger1") {
		t.Fatalf("unexpected hook-script list output: %s", out)
	}

	// Test list json
	out, err = executeTestCLI(t, "--json", "repo", "hook-script", "list")
	if err != nil {
		t.Fatalf("hook-script list json failed: %v", err)
	}
	if !strings.Contains(out, `"triggerIds"`) || !strings.Contains(out, `"my-script"`) {
		t.Fatalf("unexpected hook-script list json output: %s", out)
	}

	// Test set
	out, err = executeTestCLI(t, "repo", "hook-script", "set", "123", "--trigger", "trigger1,trigger2")
	if err != nil {
		t.Fatalf("hook-script set failed: %v", err)
	}
	if !strings.Contains(out, "Successfully configured hook script 123") {
		t.Fatalf("unexpected hook-script set output: %s", out)
	}

	// Test set json
	out, err = executeTestCLI(t, "--json", "repo", "hook-script", "set", "123", "--trigger", "trigger1,trigger2")
	if err != nil {
		t.Fatalf("hook-script set json failed: %v", err)
	}
	if !strings.Contains(out, `"status"`) || !strings.Contains(out, `"success"`) {
		t.Fatalf("unexpected hook-script set json output: %s", out)
	}

	// Test remove
	out, err = executeTestCLI(t, "repo", "hook-script", "remove", "123")
	if err != nil {
		t.Fatalf("hook-script remove failed: %v", err)
	}
	if !strings.Contains(out, "Successfully removed hook script 123") {
		t.Fatalf("unexpected hook-script remove output: %s", out)
	}

	// Test remove json
	out, err = executeTestCLI(t, "--json", "repo", "hook-script", "remove", "123")
	if err != nil {
		t.Fatalf("hook-script remove json failed: %v", err)
	}
	if !strings.Contains(out, `"status"`) || !strings.Contains(out, `"success"`) {
		t.Fatalf("unexpected hook-script remove json output: %s", out)
	}
}

func TestRepoMutatingCLICommandsDryRun(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// pre-flight check permission call:
		if r.URL.Path == "/rest/api/latest/repos" {
			_, _ = w.Write([]byte(`{"values":[{"slug":"repo","project":{"key":"PRJ"}}],"isLastPage":true}`))
		}
	}))
	t.Cleanup(server.Close)

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_TOKEN", "test-token")
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "repo")

	// dry-run edit
	out, err := executeTestCLI(t, "--dry-run", "repo", "edit", "path.txt")
	if err != nil {
		t.Fatalf("dry-run edit failed: %v", err)
	}
	if !strings.Contains(out, "Dry-run") || !strings.Contains(out, "repo.edit") {
		t.Fatalf("unexpected dry-run edit output: %s", out)
	}

	// dry-run hook-script set
	out, err = executeTestCLI(t, "--dry-run", "repo", "hook-script", "set", "123")
	if err != nil {
		t.Fatalf("dry-run hook-script set failed: %v", err)
	}
	if !strings.Contains(out, "Dry-run") || !strings.Contains(out, "repo.hook-script.set") {
		t.Fatalf("unexpected dry-run hook-script set output: %s", out)
	}

	// dry-run hook-script remove
	out, err = executeTestCLI(t, "--dry-run", "repo", "hook-script", "remove", "123")
	if err != nil {
		t.Fatalf("dry-run hook-script remove failed: %v", err)
	}
	if !strings.Contains(out, "Dry-run") || !strings.Contains(out, "repo.hook-script.remove") {
		t.Fatalf("unexpected dry-run hook-script remove output: %s", out)
	}
}

func TestRepoCLIErrorAndEdgeCases(t *testing.T) {
	// Set up server that returns errors or specific mock behaviors
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/raw/error.txt":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":[{"message":"not found"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/compare/changes" && r.URL.Query().Get("from") == "empty":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/compare/changes" && r.URL.Query().Get("from") == "error":
			w.WriteHeader(http.StatusInternalServerError)
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/compare/diff" && r.URL.Query().Get("from") == "error":
			w.WriteHeader(http.StatusInternalServerError)
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/archive" && r.URL.Query().Get("at") == "error":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"errors":[{"message":"denied"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/archive":
			w.Header().Set("Content-Type", "application/zip")
			_, _ = w.Write([]byte("fake zip data"))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/hook-scripts" && r.URL.Query().Get("limit") == "100":
			w.WriteHeader(http.StatusInternalServerError)
		case r.URL.Path == "/rest/api/latest/repos":
			// return empty list to simulate not having required repository permission
			_, _ = w.Write([]byte(`{"values":[],"isLastPage":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_TOKEN", "test-token")
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "repo")

	// 1. repo cat errors
	_, err := executeTestCLI(t, "repo", "cat", "error.txt")
	if err == nil {
		t.Fatal("expected cat error")
	}

	// 2. repo edit dry-run permission failure
	_, err = executeTestCLI(t, "--dry-run", "repo", "edit", "path.txt")
	if err == nil || !strings.Contains(err.Error(), "REPO_WRITE") {
		t.Fatalf("expected dry-run permission failure, got %v", err)
	}

	// 3. repo compare changes: no changes
	out, err := executeTestCLI(t, "repo", "compare", "empty", "feat")
	if err != nil {
		t.Fatalf("compare failed: %v", err)
	}
	if !strings.Contains(out, "No changes found") {
		t.Fatalf("expected empty changes output, got %s", out)
	}

	// repo compare changes: json output
	out, err = executeTestCLI(t, "--json", "repo", "compare", "empty", "feat")
	if err != nil {
		t.Fatalf("compare json failed: %v", err)
	}
	if !strings.Contains(out, `"changes"`) {
		t.Fatalf("expected json changes, got %s", out)
	}

	// repo compare changes: API error
	_, err = executeTestCLI(t, "repo", "compare", "error", "feat")
	if err == nil {
		t.Fatal("expected compare changes error")
	}

	// repo compare diff: API error
	_, err = executeTestCLI(t, "repo", "compare", "error", "feat", "--diff")
	if err == nil {
		t.Fatal("expected compare diff error")
	}

	// 4. repo archive: options and formats
	defer os.Remove("repo.zip")
	out, err = executeTestCLI(t, "repo", "archive", "--at", "commit1", "--prefix", "folder/", "--path", "src/")
	if err != nil {
		t.Fatalf("archive with options failed: %v", err)
	}
	if !strings.Contains(out, "Successfully downloaded") {
		t.Fatalf("expected successful archive output, got %s", out)
	}

	// repo archive: JSON output mode
	out, err = executeTestCLI(t, "--json", "repo", "archive")
	if err != nil {
		t.Fatalf("archive json failed: %v", err)
	}
	if !strings.Contains(out, `"status"`) || !strings.Contains(out, `"success"`) {
		t.Fatalf("expected JSON success response, got %s", out)
	}

	// repo archive: API error
	_, err = executeTestCLI(t, "repo", "archive", "--at", "error")
	if err == nil {
		t.Fatal("expected archive API error")
	}

	// repo archive: invalid output file path (directory doesn't exist)
	_, err = executeTestCLI(t, "repo", "archive", "-o", "nonexistent/file.zip")
	if err == nil {
		t.Fatal("expected file creation error")
	}

	// 5. repo hook-script list: API error
	_, err = executeTestCLI(t, "repo", "hook-script", "list")
	if err == nil {
		t.Fatal("expected hook list error")
	}

	// 6. repo hook-script set/remove dry-run permission failure
	_, err = executeTestCLI(t, "--dry-run", "repo", "hook-script", "set", "123")
	if err == nil || !strings.Contains(err.Error(), "REPO_ADMIN") {
		t.Fatalf("expected dry-run permission failure, got %v", err)
	}

	_, err = executeTestCLI(t, "--dry-run", "repo", "hook-script", "remove", "123")
	if err == nil || !strings.Contains(err.Error(), "REPO_ADMIN") {
		t.Fatalf("expected dry-run permission failure, got %v", err)
	}
}
