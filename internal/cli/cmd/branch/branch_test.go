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

		case r.Method == http.MethodGet && strings.Contains(path, "/branches/info/"):
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":"refs/heads/main","displayId":"main"}]}`))

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

	// Human mode
	deps := newTestDependencies(t, server.URL, false, false)
	cmd := branchcmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "main") {
		t.Fatalf("expected 'main' in output, got: %s", buf.String())
	}

	// JSON mode
	depsJSON := newTestDependencies(t, server.URL, true, false)
	cmd = branchcmd.New(depsJSON)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on json list: %v", err)
	}
	if !strings.Contains(buf.String(), "branches") {
		t.Fatalf("expected 'branches' in json output, got: %s", buf.String())
	}
}

func TestBranchCreateAndDelete(t *testing.T) {
	server := newMockBranchServer(t)

	// Create dry-run
	depsDryRun := newTestDependencies(t, server.URL, false, true)
	cmd := branchcmd.New(depsDryRun)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"create", "feature-1", "--start-point", "main"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on create dry-run: %v", err)
	}

	// Create real execution
	deps := newTestDependencies(t, server.URL, false, false)
	cmd = branchcmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"create", "feature-1", "--start-point", "main"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on create: %v", err)
	}
	if !strings.Contains(buf.String(), "Created branch") {
		t.Fatalf("expected 'Created branch' in output, got: %s", buf.String())
	}

	// Delete dry-run
	cmd = branchcmd.New(depsDryRun)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"delete", "feature-1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on delete dry-run: %v", err)
	}

	// Delete real execution
	cmd = branchcmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"delete", "feature-1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on delete: %v", err)
	}
	if !strings.Contains(buf.String(), "Deleted branch") {
		t.Fatalf("expected 'Deleted branch' in output, got: %s", buf.String())
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
	if !strings.Contains(buf.String(), "main") {
		t.Fatalf("expected 'main' in output, got: %s", buf.String())
	}

	// Set dry-run
	depsDryRun := newTestDependencies(t, server.URL, false, true)
	cmd = branchcmd.New(depsDryRun)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"default", "set", "master"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on default set dry-run: %v", err)
	}

	// Set real execution
	cmd = branchcmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"default", "set", "master"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on default set: %v", err)
	}
	if !strings.Contains(buf.String(), "Default branch set to master") {
		t.Fatalf("expected Default branch set in output: %s", buf.String())
	}
}

func TestBranchModelInspectAndUpdate(t *testing.T) {
	server := newMockBranchServer(t)
	deps := newTestDependencies(t, server.URL, false, false)

	// Inspect human mode
	cmd := branchcmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"model", "inspect", "1111111"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on model inspect: %v", err)
	}
	if !strings.Contains(buf.String(), "main") {
		t.Fatalf("expected main in inspect output: %s", buf.String())
	}

	// Inspect JSON mode
	depsJSON := newTestDependencies(t, server.URL, true, false)
	cmd = branchcmd.New(depsJSON)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"model", "inspect", "1111111"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on model inspect JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "refs") {
		t.Fatalf("expected refs in json output: %s", buf.String())
	}

	// Model update dry run
	depsDryRun := newTestDependencies(t, server.URL, false, true)
	cmd = branchcmd.New(depsDryRun)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"model", "update", "develop"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on model update dry-run: %v", err)
	}

	// Model update real execution
	cmd = branchcmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"model", "update", "develop"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on model update: %v", err)
	}
	if !strings.Contains(buf.String(), "Branch model default updated to") {
		t.Fatalf("expected Branch model default updated in output: %s", buf.String())
	}
}

func TestBranchRestrictions(t *testing.T) {
	server := newMockBranchServer(t)
	deps := newTestDependencies(t, server.URL, false, false)

	// List
	cmd := branchcmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"restriction", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "read-only") {
		t.Fatalf("expected 'read-only' in output, got: %s", buf.String())
	}

	// Get (human and JSON)
	cmd = branchcmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"restriction", "get", "42"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on restriction get: %v", err)
	}
	if !strings.Contains(buf.String(), "read-only") {
		t.Fatalf("expected read-only in get output: %s", buf.String())
	}

	depsJSON := newTestDependencies(t, server.URL, true, false)
	cmd = branchcmd.New(depsJSON)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"restriction", "get", "42"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on restriction get JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "restriction") {
		t.Fatalf("expected restriction in JSON output: %s", buf.String())
	}

	// Create (dry-run and real)
	depsDryRun := newTestDependencies(t, server.URL, false, true)
	cmd = branchcmd.New(depsDryRun)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"restriction", "create", "--type", "read-only", "--matcher-id", "main"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on restriction create dry-run: %v", err)
	}

	cmd = branchcmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"restriction", "create", "--type", "read-only", "--matcher-id", "main"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on restriction create: %v", err)
	}
	if !strings.Contains(buf.String(), "Created restriction") {
		t.Fatalf("expected Created restriction in output: %s", buf.String())
	}

	// Update (dry-run and real)
	cmd = branchcmd.New(depsDryRun)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"restriction", "update", "42", "--type", "read-only", "--matcher-id", "main"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on restriction update dry-run: %v", err)
	}

	cmd = branchcmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"restriction", "update", "42", "--type", "read-only", "--matcher-id", "main"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on restriction update: %v", err)
	}
	if !strings.Contains(buf.String(), "Updated restriction") {
		t.Fatalf("expected Updated restriction in output: %s", buf.String())
	}

	// Delete (dry-run and real)
	cmd = branchcmd.New(depsDryRun)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"restriction", "delete", "42"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on restriction delete dry-run: %v", err)
	}

	cmd = branchcmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"restriction", "delete", "42"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on restriction delete: %v", err)
	}
	if !strings.Contains(buf.String(), "Deleted restriction") {
		t.Fatalf("expected Deleted restriction in output: %s", buf.String())
	}
}
