package commitcmd_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	commitcmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/commit"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

func newMockCommitServer(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=UTF-8")
		path := r.URL.Path

		switch {
		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/commits":
			if r.URL.Query().Get("empty") == "true" || r.URL.Query().Get("start") == "999" {
				_, _ = w.Write([]byte(`{"isLastPage":true,"values":[]}`))
				return
			}
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":"abcdef123456","displayId":"abcdef1","message":"Initial commit\nSecond line"}]}`))

		case r.Method == http.MethodGet && path == "/rest/jira/latest/issues/ISSUE-101/commits":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"toCommit":{"id":"jira123456","displayId":"jira123","message":"Jira commit"}}]}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/commits/abcdef123456":
			_, _ = w.Write([]byte(`{"id":"abcdef123456","displayId":"abcdef1","message":"Initial commit"}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/commits/abcdef123456/pull-requests":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":1,"title":"Add feature","state":"OPEN"}]}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/commits/emptycommit/pull-requests":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[]}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/compare/commits":
			if r.URL.Query().Get("from") == "empty" {
				_, _ = w.Write([]byte(`{"isLastPage":true,"values":[]}`))
				return
			}
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":"abcdef123456","displayId":"abcdef1","message":"Initial commit"}]}`))

		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	return server
}

func newTestDependencies(t *testing.T, serverURL string, jsonMode bool) commitcmd.Dependencies {
	cfg := config.AppConfig{
		BitbucketURL: serverURL,
		ProjectKey:   "PRJ",
	}
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	return commitcmd.Dependencies{
		JSONEnabled: func() bool { return jsonMode },
		LoadConfig: func() (config.AppConfig, error) {
			return cfg, nil
		},
		LoadConfigAndClient: func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
			client, err := openapi.NewClientWithResponsesFromConfig(cfg)
			if err != nil {
				return config.AppConfig{}, nil, err
			}
			return cfg, client, nil
		},
	}
}

func TestCommitList(t *testing.T) {
	server := newMockCommitServer(t)

	// 1. Standard list (human & JSON)
	deps := newTestDependencies(t, server.URL, false)
	cmd := commitcmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list", "--path", "main.go", "--start", "0", "--limit", "10"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on list: %v", err)
	}
	if !strings.Contains(buf.String(), "abcdef1") || !strings.Contains(buf.String(), "Initial commit") {
		t.Fatalf("expected 'abcdef1' and 'Initial commit' in output, got: %s", buf.String())
	}

	depsJSON := newTestDependencies(t, server.URL, true)
	cmd = commitcmd.New(depsJSON)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on list JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "commits") || !strings.Contains(buf.String(), "abcdef123456") {
		t.Fatalf("expected commits JSON output, got: %s", buf.String())
	}

	// 2. Jira-associated commits list
	cmd = commitcmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list", "--jira", "ISSUE-101"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on list with --jira: %v", err)
	}
	if !strings.Contains(buf.String(), "jira123") {
		t.Fatalf("expected jira123 in output, got: %s", buf.String())
	}

	// 3. Empty commit list
	cmd = commitcmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list", "--start", "999"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on empty list: %v", err)
	}
}

func TestCommitGet(t *testing.T) {
	server := newMockCommitServer(t)

	// Human & JSON
	deps := newTestDependencies(t, server.URL, false)
	cmd := commitcmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"get", "abcdef123456", "--repo", "PRJ/demo"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on get: %v", err)
	}
	if !strings.Contains(buf.String(), "abcdef123456") || !strings.Contains(buf.String(), "Commit:") {
		t.Fatalf("expected 'abcdef123456' and Commit: in output, got: %s", buf.String())
	}

	depsJSON := newTestDependencies(t, server.URL, true)
	cmd = commitcmd.New(depsJSON)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"get", "abcdef123456"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on get JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "commit") || !strings.Contains(buf.String(), "abcdef123456") {
		t.Fatalf("expected commit in JSON output, got: %s", buf.String())
	}
}

func TestCommitCompare(t *testing.T) {
	server := newMockCommitServer(t)

	// Human, empty, and JSON
	deps := newTestDependencies(t, server.URL, false)
	cmd := commitcmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"compare", "main", "feature"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on compare: %v", err)
	}
	if !strings.Contains(buf.String(), "abcdef1") {
		t.Fatalf("expected 'abcdef1' in output, got: %s", buf.String())
	}

	cmd = commitcmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"compare", "empty", "feature"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on empty compare: %v", err)
	}
	if !strings.Contains(buf.String(), "No commits found between refs") {
		t.Fatalf("expected empty compare message, got: %s", buf.String())
	}

	depsJSON := newTestDependencies(t, server.URL, true)
	cmd = commitcmd.New(depsJSON)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"compare", "main", "feature"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on compare JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "commits") {
		t.Fatalf("expected commits in JSON output, got: %s", buf.String())
	}
}

func TestCommitPRs(t *testing.T) {
	server := newMockCommitServer(t)

	// Human, empty, and JSON
	deps := newTestDependencies(t, server.URL, false)
	cmd := commitcmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"prs", "abcdef123456"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on prs: %v", err)
	}
	if !strings.Contains(buf.String(), "#1") || !strings.Contains(buf.String(), "Add feature") {
		t.Fatalf("expected '#1' and 'Add feature' in output, got: %s", buf.String())
	}

	cmd = commitcmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"prs", "emptycommit"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on empty prs: %v", err)
	}
	if !strings.Contains(buf.String(), "No pull requests containing commit found") {
		t.Fatalf("expected empty prs message, got: %s", buf.String())
	}

	depsJSON := newTestDependencies(t, server.URL, true)
	cmd = commitcmd.New(depsJSON)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"prs", "abcdef123456"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on prs JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "pullRequests") {
		t.Fatalf("expected pullRequests in JSON output, got: %s", buf.String())
	}
}

func TestCommitValidationErrors(t *testing.T) {
	server := newMockCommitServer(t)
	deps := newTestDependencies(t, server.URL, false)

	// Invalid repo selector
	cmd := commitcmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list", "--repo", "invalid-repo-no-slash"})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error on invalid repo selector")
	}

	// Defaults coverage
	defaultCmd := commitcmd.New(commitcmd.Dependencies{})
	if defaultCmd == nil {
		t.Fatalf("expected default command to be created")
	}
}
