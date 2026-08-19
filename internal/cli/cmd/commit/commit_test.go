package commitcmd_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	commitcmd "github.com/vriesdemichael/bitbucket-server-cli/internal/cli/cmd/commit"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

func newMockCommitServer(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/commits":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":"abcdef123456","displayId":"abcdef1","message":"Initial commit"}]}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/commits/abcdef123456":
			_, _ = w.Write([]byte(`{"id":"abcdef123456","displayId":"abcdef1","message":"Initial commit"}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/commits/abcdef123456/pull-requests":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":1,"title":"Add feature","state":"OPEN"}]}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/compare/commits":
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
	deps := newTestDependencies(t, server.URL, false)

	cmd := commitcmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on list: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "abcdef1") {
		t.Fatalf("expected 'abcdef1' in output, got: %s", out)
	}
}

func TestCommitGet(t *testing.T) {
	server := newMockCommitServer(t)
	deps := newTestDependencies(t, server.URL, false)

	cmd := commitcmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"get", "abcdef123456"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on get: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "abcdef123456") {
		t.Fatalf("expected 'abcdef123456' in output, got: %s", out)
	}
}

func TestCommitCompare(t *testing.T) {
	server := newMockCommitServer(t)
	deps := newTestDependencies(t, server.URL, false)

	cmd := commitcmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"compare", "main", "feature"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on compare: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "abcdef1") {
		t.Fatalf("expected 'abcdef1' in output, got: %s", out)
	}
}

func TestCommitPRs(t *testing.T) {
	server := newMockCommitServer(t)
	deps := newTestDependencies(t, server.URL, false)

	cmd := commitcmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"prs", "abcdef123456"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on prs: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "#1") {
		t.Fatalf("expected '#1' in output, got: %s", out)
	}
}
