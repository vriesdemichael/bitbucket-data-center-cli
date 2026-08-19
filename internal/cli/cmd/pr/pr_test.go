package prcmd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

type testChecker struct{}

func (testChecker) CheckRepoPermission(ctx context.Context, projectKey, repoSlug string, permission openapigenerated.GetRepositories1ParamsPermission) error {
	return nil
}

func newMockPRServer(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":42,"title":"Test PR","state":"OPEN","open":true,"fromRef":{"displayId":"feature/x"},"toRef":{"displayId":"main"}}]}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42":
			_, _ = w.Write([]byte(`{"id":42,"title":"Test PR","description":"Desc","state":"OPEN","open":true,"version":1,"fromRef":{"displayId":"feature/x"},"toRef":{"displayId":"main"},"reviewers":[{"user":{"name":"alice","displayName":"Alice"},"approved":false,"status":"UNAPPROVED"}]}`))

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests":
			_, _ = w.Write([]byte(`{"id":43,"title":"Created PR","state":"OPEN","open":true,"fromRef":{"displayId":"feature/y"},"toRef":{"displayId":"main"}}`))

		case r.Method == http.MethodPut && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42":
			_, _ = w.Write([]byte(`{"id":42,"title":"Updated PR","description":"New Desc","state":"OPEN","open":true,"version":2,"fromRef":{"displayId":"feature/x"},"toRef":{"displayId":"main"}}`))

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/merge":
			_, _ = w.Write([]byte(`{"id":42,"title":"Test PR","state":"MERGED","open":false,"closed":true}`))

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/decline":
			_, _ = w.Write([]byte(`{"id":42,"title":"Test PR","state":"DECLINED","open":false,"closed":true}`))

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/reopen":
			_, _ = w.Write([]byte(`{"id":42,"title":"Test PR","state":"OPEN","open":true,"closed":false}`))

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/approve":
			_, _ = w.Write([]byte(`{"id":42,"title":"Test PR","state":"OPEN","open":true,"reviewers":[{"user":{"name":"alice"},"approved":true,"status":"APPROVED"}]}`))

		case r.Method == http.MethodDelete && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/approve":
			_, _ = w.Write([]byte(`{"id":42,"title":"Test PR","state":"OPEN","open":true,"reviewers":[{"user":{"name":"alice"},"approved":false,"status":"UNAPPROVED"}]}`))

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/participants":
			_, _ = w.Write([]byte(`{"id":42,"title":"Test PR","state":"OPEN","open":true,"reviewers":[{"user":{"name":"bob","displayName":"Bob"},"role":"REVIEWER","status":"UNAPPROVED"}]}`))

		case r.Method == http.MethodDelete && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/participants/bob":
			_, _ = w.Write([]byte(`{"id":42,"title":"Test PR","state":"OPEN","open":true}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/activities":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"action":"COMMENTED","comment":{"id":101,"version":1,"text":"Comment 1","state":"OPEN","author":{"name":"alice"}}}]}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/comments":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":101,"version":1,"text":"Comment 1","state":"OPEN","author":{"name":"alice"}}]}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/comments/101":
			_, _ = w.Write([]byte(`{"id":101,"version":1,"text":"Comment 1","state":"OPEN","author":{"name":"alice"}}`))

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/comments":
			_, _ = w.Write([]byte(`{"id":102,"version":1,"text":"New comment","state":"OPEN","author":{"name":"alice"}}`))

		case r.Method == http.MethodPut && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/comments/101":
			_, _ = w.Write([]byte(`{"id":101,"version":2,"text":"Comment 1","state":"RESOLVED","author":{"name":"alice"}}`))

		case (r.Method == http.MethodPut || r.Method == http.MethodDelete) && strings.Contains(path, "/comment-likes/latest/"):
			_, _ = w.Write([]byte(`{"emoticon":{"value":"thumbsup"}}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/commits":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":"c123","displayId":"c123","message":"Commit 1"}]}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/changes":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"path":{"toString":"file1.go"}}]}`))

		case r.Method == http.MethodGet && (strings.HasSuffix(path, "42.diff") || strings.HasSuffix(path, "/diff")):
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("diff --git a/file1.go b/file1.go\n--- a/file1.go\n+++ b/file1.go\n@@ -1 +1 @@\n-old\n+new\n"))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/auto-merge":
			_, _ = w.Write([]byte(`{"enabled":true,"strategyId":"no-ff"}`))

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/auto-merge":
			_, _ = w.Write([]byte(`{"enabled":true,"strategyId":"no-ff"}`))

		case r.Method == http.MethodDelete && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/auto-merge":
			w.WriteHeader(http.StatusNoContent)

		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	return server
}

func executePr(t *testing.T, serverURL string, args ...string) (string, error) {
	t.Helper()

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", serverURL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")
	t.Setenv("BITBUCKET_TOKEN", "test-token")

	var jsonFlag bool
	var dryRunFlag bool
	filteredArgs := make([]string, 0, len(args))
	for _, a := range args {
		if a == "--json" {
			jsonFlag = true
		} else if a == "--dry-run" {
			dryRunFlag = true
		} else {
			filteredArgs = append(filteredArgs, a)
		}
	}

	deps := Dependencies{
		JSONEnabled:   func() bool { return jsonFlag },
		DryRunEnabled: func() bool { return dryRunFlag },
		LoadConfig: func() (config.AppConfig, error) {
			return config.LoadFromEnv()
		},
		LoadConfigAndClient: func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
			cfg, err := config.LoadFromEnv()
			if err != nil {
				return config.AppConfig{}, nil, err
			}
			client, err := openapi.NewClientWithResponsesFromConfig(cfg)
			return cfg, client, err
		},
		WriteJSON:     jsonoutput.Write,
		WriteJSONList: jsonoutput.WriteList,
		PermissionChecker: func(client *openapigenerated.ClientWithResponses) PermissionChecker {
			return testChecker{}
		},
	}

	root := New(deps)
	buffer := &bytes.Buffer{}
	root.SetOut(buffer)
	root.SetErr(buffer)

	root.SetArgs(filteredArgs)
	err := root.Execute()
	return buffer.String(), err
}

func TestPRList(t *testing.T) {
	server := newMockPRServer(t)
	out, err := executePr(t, server.URL, "list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "#42") || !strings.Contains(out, "Test PR") {
		t.Fatalf("expected PR #42 in list output, got:\n%s", out)
	}
}

func TestPRGet(t *testing.T) {
	server := newMockPRServer(t)
	out, err := executePr(t, server.URL, "get", "42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Test PR") || !strings.Contains(out, "feature/x") {
		t.Fatalf("expected PR details in get output, got:\n%s", out)
	}
}

func TestPRCreate(t *testing.T) {
	server := newMockPRServer(t)
	out, err := executePr(t, server.URL, "create", "--from-ref", "feature/y", "--to-ref", "main", "--title", "Created PR")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Created pull request #43") {
		t.Fatalf("expected create confirmation, got:\n%s", out)
	}
}

func TestPRUpdate(t *testing.T) {
	server := newMockPRServer(t)
	out, err := executePr(t, server.URL, "update", "42", "--version", "1", "--title", "Updated PR", "--description", "New Desc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Updated pull request #42") {
		t.Fatalf("expected update confirmation, got:\n%s", out)
	}
}

func TestPRMerge(t *testing.T) {
	server := newMockPRServer(t)
	out, err := executePr(t, server.URL, "merge", "42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Merged pull request #42") {
		t.Fatalf("expected merge confirmation, got:\n%s", out)
	}
}

func TestPRDecline(t *testing.T) {
	server := newMockPRServer(t)
	out, err := executePr(t, server.URL, "decline", "42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Declined pull request #42") {
		t.Fatalf("expected decline confirmation, got:\n%s", out)
	}
}

func TestPRReopen(t *testing.T) {
	server := newMockPRServer(t)
	out, err := executePr(t, server.URL, "reopen", "42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Reopened pull request #42") {
		t.Fatalf("expected reopen confirmation, got:\n%s", out)
	}
}

func TestPRReviewApprove(t *testing.T) {
	server := newMockPRServer(t)
	out, err := executePr(t, server.URL, "review", "approve", "42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Approved pull request #42") {
		t.Fatalf("expected approval confirmation, got:\n%s", out)
	}
}

func TestPRReviewUnapprove(t *testing.T) {
	server := newMockPRServer(t)
	out, err := executePr(t, server.URL, "review", "unapprove", "42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Removed approval for pull request #42") {
		t.Fatalf("expected unapprove confirmation, got:\n%s", out)
	}
}

func TestPRReviewerAdd(t *testing.T) {
	server := newMockPRServer(t)
	out, err := executePr(t, server.URL, "review", "reviewer", "add", "42", "--user", "bob")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Added reviewer bob to pull request #42") {
		t.Fatalf("expected reviewer add confirmation, got:\n%s", out)
	}
}

func TestPRReviewerRemove(t *testing.T) {
	server := newMockPRServer(t)
	out, err := executePr(t, server.URL, "review", "reviewer", "remove", "42", "--user", "bob")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Removed reviewer bob from pull request #42") {
		t.Fatalf("expected reviewer remove confirmation, got:\n%s", out)
	}
}

func TestPRCommentCommands(t *testing.T) {
	server := newMockPRServer(t)

	// Add comment
	out, err := executePr(t, server.URL, "comment", "add", "42", "--text", "New comment")
	if err != nil {
		t.Fatalf("unexpected error on comment add: %v", err)
	}
	if !strings.Contains(out, "Created comment") && !strings.Contains(out, "102") {
		t.Fatalf("expected Created comment in add output: %s", out)
	}

	// List comments
	out, err = executePr(t, server.URL, "comment", "list", "42")
	if err != nil {
		t.Fatalf("unexpected error on comment list: %v", err)
	}
	if !strings.Contains(out, "Comment 1") {
		t.Fatalf("expected Comment 1 in list output: %s", out)
	}

	// Get comment
	out, err = executePr(t, server.URL, "comment", "get", "42", "101")
	if err != nil {
		t.Fatalf("unexpected error on comment get: %v", err)
	}
	if !strings.Contains(out, "Comment 1") {
		t.Fatalf("expected Comment 1 in get output: %s", out)
	}

	// Resolve comment
	out, err = executePr(t, server.URL, "comment", "resolve", "42", "101")
	if err != nil {
		t.Fatalf("unexpected error on resolve: %v", err)
	}
	if !strings.Contains(out, "Resolved comment 101") {
		t.Fatalf("expected resolve confirmation, got:\n%s", out)
	}

	// Reopen comment
	out, err = executePr(t, server.URL, "comment", "reopen", "42", "101")
	if err != nil {
		t.Fatalf("unexpected error on reopen: %v", err)
	}
	if !strings.Contains(out, "Reopened comment 101") {
		t.Fatalf("expected reopen confirmation, got:\n%s", out)
	}

	// React to comment
	out, err = executePr(t, server.URL, "comment", "react", "42", "101", "thumbsup")
	if err != nil {
		t.Fatalf("unexpected error on react: %v", err)
	}
	if !strings.Contains(out, "Added reaction") {
		t.Fatalf("expected reaction confirmation, got:\n%s", out)
	}
}

func TestPRCommitsAndFiles(t *testing.T) {
	server := newMockPRServer(t)

	// Commits
	out, err := executePr(t, server.URL, "commits", "42")
	if err != nil {
		t.Fatalf("unexpected error on commits: %v", err)
	}
	if !strings.Contains(out, "c123") {
		t.Fatalf("expected commit c123 in output: %s", out)
	}

	// Files
	out, err = executePr(t, server.URL, "files", "42")
	if err != nil {
		t.Fatalf("unexpected error on files: %v", err)
	}
	if !strings.Contains(out, "file1.go") {
		t.Fatalf("expected file1.go in output: %s", out)
	}

	// Diff
	out, err = executePr(t, server.URL, "diff", "42")
	if err != nil {
		t.Fatalf("unexpected error on diff: %v", err)
	}
	if !strings.Contains(out, "file1.go") {
		t.Fatalf("expected diff output, got: %s", out)
	}
}

func TestPRAutoMergeGet(t *testing.T) {
	server := newMockPRServer(t)
	out, err := executePr(t, server.URL, "auto-merge", "get", "42")
	if err != nil {
		t.Fatalf("unexpected error on auto-merge get: %v", err)
	}
	if !strings.Contains(out, "Auto-merge: enabled") {
		t.Fatalf("expected auto-merge enabled, got:\n%s", out)
	}
}

func TestPRAutoMergeEnableAndDisable(t *testing.T) {
	server := newMockPRServer(t)

	// Enable
	out, err := executePr(t, server.URL, "auto-merge", "enable", "42", "--strategy", "no-ff")
	if err != nil {
		t.Fatalf("unexpected error on enable: %v", err)
	}
	if !strings.Contains(out, "Enabled auto-merge") && !strings.Contains(out, "Merged pull request") {
		t.Fatalf("expected enable confirmation, got:\n%s", out)
	}

	// Disable
	out, err = executePr(t, server.URL, "auto-merge", "disable", "42")
	if err != nil {
		t.Fatalf("unexpected error on disable: %v", err)
	}
	if !strings.Contains(out, "Disabled auto-merge on pull request #42") {
		t.Fatalf("expected disable confirmation, got:\n%s", out)
	}
}
