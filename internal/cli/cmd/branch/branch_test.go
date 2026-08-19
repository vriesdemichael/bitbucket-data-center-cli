package branchcmd_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	branchcmd "github.com/vriesdemichael/bitbucket-server-cli/internal/cli/cmd/branch"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

type testPermissionChecker struct{}

func (testPermissionChecker) CheckRepoPermission(ctx context.Context, projectKey, repoSlug string, permission openapigenerated.GetRepositories1ParamsPermission) error {
	return nil
}

func newMockBranchServer(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/default-branch":
			_, _ = w.Write([]byte(`{"id":"refs/heads/main","displayId":"main"}`))

		case r.Method == http.MethodPut && path == "/rest/api/latest/projects/PRJ/repos/demo/default-branch":
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/branches":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":"refs/heads/main","displayId":"main","latestCommit":"1111111"}]}`))

		case r.Method == http.MethodPost && path == "/rest/branch-utils/latest/projects/PRJ/repos/demo/branches":
			_, _ = w.Write([]byte(`{"id":"refs/heads/feature-1","displayId":"feature-1","latestCommit":"2222222"}`))

		case r.Method == http.MethodDelete && path == "/rest/branch-utils/latest/projects/PRJ/repos/demo/branches":
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodGet && path == "/rest/branch-permissions/latest/projects/PRJ/repos/demo/restrictions":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":42,"type":"read-only","matcher":{"id":"refs/heads/main","displayId":"main","type":{"id":"BRANCH"}}}]}`))

		case r.Method == http.MethodPost && path == "/rest/branch-permissions/latest/projects/PRJ/repos/demo/restrictions":
			_, _ = w.Write([]byte(`[{"id":43,"type":"read-only","matcher":{"id":"refs/heads/main","displayId":"main","type":{"id":"BRANCH"}}}]`))

		case r.Method == http.MethodGet && path == "/rest/branch-permissions/latest/projects/PRJ/repos/demo/restrictions/42":
			_, _ = w.Write([]byte(`{"id":42,"type":"read-only","matcher":{"id":"refs/heads/main","displayId":"main","type":{"id":"BRANCH"}}}`))

		case r.Method == http.MethodPut && path == "/rest/branch-permissions/latest/projects/PRJ/repos/demo/restrictions/42":
			_, _ = w.Write([]byte(`{"id":42,"type":"read-only","matcher":{"id":"refs/heads/main","displayId":"main","type":{"id":"BRANCH"}}}`))

		case r.Method == http.MethodDelete && path == "/rest/branch-permissions/latest/projects/PRJ/repos/demo/restrictions/42":
			w.WriteHeader(http.StatusNoContent)

		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	return server
}

func newTestDependencies(t *testing.T, serverURL string, jsonMode bool, dryRun bool) branchcmd.Dependencies {
	cfg := config.AppConfig{
		BitbucketURL: serverURL,
		ProjectKey:   "PRJ",
	}
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	return branchcmd.Dependencies{
		JSONEnabled:   func() bool { return jsonMode },
		DryRunEnabled: func() bool { return dryRun },
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
		PermissionChecker: func(client *openapigenerated.ClientWithResponses) branchcmd.PermissionChecker {
			return testPermissionChecker{}
		},
	}
}

func TestBranchList(t *testing.T) {
	server := newMockBranchServer(t)
	deps := newTestDependencies(t, server.URL, false, false)

	cmd := branchcmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "main") {
		t.Fatalf("expected 'main' in output, got: %s", out)
	}
}

func TestBranchCreate(t *testing.T) {
	server := newMockBranchServer(t)
	deps := newTestDependencies(t, server.URL, false, false)

	cmd := branchcmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"create", "feature-1", "--start-point", "main"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Created branch") {
		t.Fatalf("expected 'Created branch' in output, got: %s", out)
	}
}

func TestBranchDefaultGetAndSet(t *testing.T) {
	server := newMockBranchServer(t)
	deps := newTestDependencies(t, server.URL, false, false)

	cmd := branchcmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"default", "get"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "main") {
		t.Fatalf("expected 'main' in output, got: %s", out)
	}

	buf.Reset()
	cmd.SetArgs([]string{"default", "set", "master"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBranchRestrictionListAndCreate(t *testing.T) {
	server := newMockBranchServer(t)
	deps := newTestDependencies(t, server.URL, false, false)

	cmd := branchcmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"restriction", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "read-only") {
		t.Fatalf("expected 'read-only' in output, got: %s", out)
	}

	buf.Reset()
	cmd.SetArgs([]string{"restriction", "create", "--type", "read-only", "--matcher-id", "main"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
