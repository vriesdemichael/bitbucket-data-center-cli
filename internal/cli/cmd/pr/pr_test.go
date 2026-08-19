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
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[]}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/comments":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":101,"version":1,"text":"Comment 1","state":"OPEN","author":{"name":"alice"}}]}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/comments/101":
			_, _ = w.Write([]byte(`{"id":101,"version":1,"text":"Comment 1","state":"OPEN","author":{"name":"alice"}}`))

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/comments":
			_, _ = w.Write([]byte(`{"id":102,"version":1,"text":"New comment","state":"OPEN","author":{"name":"alice"}}`))

		case r.Method == http.MethodPut && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/comments/101":
			_, _ = w.Write([]byte(`{"id":101,"version":2,"text":"Comment 1","state":"RESOLVED","author":{"name":"alice"}}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/auto-merge":
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
	for _, a := range args {
		if a == "--json" {
			jsonFlag = true
		}
		if a == "--dry-run" {
			dryRunFlag = true
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

	cmd := testPrCommand(deps)
	buffer := &bytes.Buffer{}
	cmd.SetOut(buffer)
	cmd.SetErr(buffer)

	fullArgs := append([]string{"pr"}, args...)
	cmd.SetArgs(fullArgs)
	err := cmd.Execute()
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
	out, err := executePr(t, server.URL, "get", "42", "--no-review-summary")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "#42") || !strings.Contains(out, "Test PR") {
		t.Fatalf("expected PR #42 in get output, got:\n%s", out)
	}
}

func TestPRCreate(t *testing.T) {
	server := newMockPRServer(t)
	out, err := executePr(t, server.URL, "create", "--from-ref", "feature/y", "--to-ref", "main", "--title", "Created PR")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Created pull request #43") {
		t.Fatalf("expected creation confirmation, got:\n%s", out)
	}
}

func TestPRUpdate(t *testing.T) {
	server := newMockPRServer(t)
	out, err := executePr(t, server.URL, "update", "42", "--version", "1", "--title", "Updated PR")
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

func TestPRCommentResolve(t *testing.T) {
	server := newMockPRServer(t)
	out, err := executePr(t, server.URL, "comment", "resolve", "42", "101")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Resolved comment 101") {
		t.Fatalf("expected resolve confirmation, got:\n%s", out)
	}
}

func TestPRAutoMergeGet(t *testing.T) {
	server := newMockPRServer(t)
	out, err := executePr(t, server.URL, "auto-merge", "get", "42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Auto-merge: enabled") {
		t.Fatalf("expected auto-merge enabled, got:\n%s", out)
	}
}

func TestPRAutoMergeDisable(t *testing.T) {
	server := newMockPRServer(t)
	out, err := executePr(t, server.URL, "auto-merge", "disable", "42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Disabled auto-merge on pull request #42") {
		t.Fatalf("expected disable confirmation, got:\n%s", out)
	}
}
