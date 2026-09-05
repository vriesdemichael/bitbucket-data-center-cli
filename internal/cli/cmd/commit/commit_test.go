package commitcmd_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
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
