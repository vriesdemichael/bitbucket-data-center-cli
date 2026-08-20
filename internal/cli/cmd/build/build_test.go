package buildcmd_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	buildcmd "github.com/vriesdemichael/bitbucket-server-cli/internal/cli/cmd/build"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

type testPermissionChecker struct{}

func (testPermissionChecker) CheckRepoPermission(ctx context.Context, projectKey, repoSlug string, permission openapigenerated.GetRepositories1ParamsPermission) error {
	return nil
}

func newMockBuildServer(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=UTF-8")
		path := r.URL.Path

		switch {
		case r.Method == http.MethodPost && path == "/rest/build-status/latest/commits/commit1":
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodGet && path == "/rest/build-status/latest/commits/commit1":
			if r.URL.Query().Get("key") == "empty" {
				_, _ = w.Write([]byte(`{"isLastPage":true,"values":[]}`))
				return
			}
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"key":"ci1","name":"CI Build","state":"SUCCESSFUL","url":"http://ci.example.com","description":"Passed"}]}`))

		case r.Method == http.MethodGet && path == "/rest/build-status/latest/commits/stats/commit1":
			_, _ = w.Write([]byte(`{"successful":1,"failed":0,"inProgress":0,"unknown":0,"cancelled":0}`))

		case r.Method == http.MethodPost && path == "/rest/build-status/latest/commits/stats":
			_, _ = w.Write([]byte(`{"commit1":{"successful":1,"failed":0,"inProgress":0,"unknown":0,"cancelled":0}}`))

		case (r.Method == http.MethodPost || r.Method == http.MethodPut) && strings.HasSuffix(path, "/commits/commit1/builds"):
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodGet && strings.HasSuffix(path, "/commits/commit1/builds"):
			_, _ = w.Write([]byte(`{"key":"ci1","name":"CI Build","state":"SUCCESSFUL","url":"http://ci.example.com"}`))

		case r.Method == http.MethodDelete && strings.HasSuffix(path, "/commits/commit1/builds"):
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodGet && strings.Contains(path, "/required-builds/latest/projects/PRJ/repos/demo/conditions"):
			if r.URL.Query().Get("empty") == "true" {
				_, _ = w.Write([]byte(`{"isLastPage":true,"values":[]}`))
				return
			}
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":501,"refMatcher":{"id":"refs/heads/main"},"buildParentKeys":["ci-required"],"count":1}]}`))

		case r.Method == http.MethodPost && strings.Contains(path, "/required-builds/latest/projects/PRJ/repos/demo/condition"):
			_, _ = w.Write([]byte(`{"id":501,"refMatcher":{"id":"refs/heads/main"},"buildParentKeys":["ci-required"],"count":1}`))

		case r.Method == http.MethodPut && strings.Contains(path, "/required-builds/latest/projects/PRJ/repos/demo/condition/501"):
			_, _ = w.Write([]byte(`{"id":501,"refMatcher":{"id":"refs/heads/main"},"buildParentKeys":["ci-updated"],"count":1}`))

		case r.Method == http.MethodDelete && strings.Contains(path, "/required-builds/latest/projects/PRJ/repos/demo/condition/501"):
			w.WriteHeader(http.StatusNoContent)

		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	return server
}

func newTestDependencies(serverURL string, jsonMode bool, dryRun bool) buildcmd.Dependencies {
	cfg := config.AppConfig{
		BitbucketURL: serverURL,
		ProjectKey:   "PRJ",
	}

	return buildcmd.Dependencies{
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
		PermissionChecker: func(client *openapigenerated.ClientWithResponses) buildcmd.PermissionChecker {
			return testPermissionChecker{}
		},
	}
}

func TestBuildStatusSetAndGet(t *testing.T) {
	server := newMockBuildServer(t)

	// 1. Status set real execution
	deps := newTestDependencies(server.URL, false, false)
	cmd := buildcmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"status", "set", "commit1",
		"--key", "ci1",
		"--state", "SUCCESSFUL",
		"--url", "http://ci.example.com",
		"--name", "CI Build",
		"--description", "Passed",
		"--build-number", "42",
		"--duration-ms", "120",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on set: %v", err)
	}
	if !strings.Contains(buf.String(), "Build status ci1 set on commit1") {
		t.Fatalf("expected Build status ci1 set in output: %s", buf.String())
	}

	// 2. Status set dry-run (existing key -> update)
	depsDryRun := newTestDependencies(server.URL, false, true)
	cmd = buildcmd.New(depsDryRun)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"status", "set", "commit1", "--key", "ci1", "--state", "SUCCESSFUL", "--url", "http://ci.example.com"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on set dry-run update: %v", err)
	}

	// 3. Status set dry-run (new key -> create)
	cmd = buildcmd.New(depsDryRun)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"status", "set", "commit1", "--key", "ci-new", "--state", "SUCCESSFUL", "--url", "http://ci.example.com"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on set dry-run create: %v", err)
	}

	// 4. Status set JSON mode
	depsJSON := newTestDependencies(server.URL, true, false)
	cmd = buildcmd.New(depsJSON)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"status", "set", "commit1", "--key", "ci1", "--state", "SUCCESSFUL", "--url", "http://ci.example.com"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on set JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "status") {
		t.Fatalf("expected status in JSON output: %s", buf.String())
	}

	// 5. Status get with order-by (human & JSON)
	cmd = buildcmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"status", "get", "commit1", "--order-by", "NEWEST"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on get: %v", err)
	}
	if !strings.Contains(buf.String(), "ci1") || !strings.Contains(buf.String(), "SUCCESSFUL") {
		t.Fatalf("expected ci1 and SUCCESSFUL in output: %s", buf.String())
	}

	cmd = buildcmd.New(depsJSON)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"status", "get", "commit1", "--order-by", "NEWEST"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on get JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "ci1") {
		t.Fatalf("expected ci1 in JSON output: %s", buf.String())
	}

	// 6. Status get all (human & JSON)
	cmd = buildcmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"status", "get", "commit1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on get all: %v", err)
	}
	if !strings.Contains(buf.String(), "ci1") {
		t.Fatalf("expected ci1 in get all output: %s", buf.String())
	}

	cmd = buildcmd.New(depsJSON)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"status", "get", "commit1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on get all JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "ci1") {
		t.Fatalf("expected ci1 in JSON list output: %s", buf.String())
	}
}

func TestBuildStatusStats(t *testing.T) {
	server := newMockBuildServer(t)

	// Single commit stats (human & JSON)
	deps := newTestDependencies(server.URL, false, false)
	cmd := buildcmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"status", "stats", "commit1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on stats: %v", err)
	}
	if !strings.Contains(buf.String(), "Successful:") {
		t.Fatalf("expected Successful: in output, got: %s", buf.String())
	}

	depsJSON := newTestDependencies(server.URL, true, false)
	cmd = buildcmd.New(depsJSON)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"status", "stats", "commit1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on stats JSON: %v", err)
	}
	// Multiple commit stats (human & JSON)
	cmd = buildcmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"status", "stats", "commit1", "commit1", "--include-unique"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on multiple stats: %v", err)
	}
	if !strings.Contains(buf.String(), "COMMIT") {
		t.Fatalf("expected COMMIT header in output, got: %s", buf.String())
	}

	cmd = buildcmd.New(depsJSON)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"status", "stats", "commit1", "commit1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on multiple stats JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "commit1") {
		t.Fatalf("expected commit1 in JSON stats: %s", buf.String())
	}
}

func TestBuildRepoScopedCommands(t *testing.T) {
	server := newMockBuildServer(t)
	deps := newTestDependencies(server.URL, false, false)
	depsDryRun := newTestDependencies(server.URL, false, true)
	depsJSON := newTestDependencies(server.URL, true, false)

	// 1. build set (real, dry-run, JSON)
	cmd := buildcmd.New(depsDryRun)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"set", "commit1", "--key", "ci1", "--state", "SUCCESSFUL", "--url", "http://ci.example.com", "--repo", "PRJ/demo"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on build set dry-run: %v", err)
	}

	cmd = buildcmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"set", "commit1", "--key", "ci1", "--state", "SUCCESSFUL", "--url", "http://ci.example.com", "--repo", "PRJ/demo"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on build set: %v", err)
	}
	if !strings.Contains(buf.String(), "Repository-scoped build status ci1 set on PRJ/demo at commit1") {
		t.Fatalf("expected Repository-scoped build status in output: %s", buf.String())
	}

	cmd = buildcmd.New(depsJSON)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"set", "commit1", "--key", "ci1", "--state", "SUCCESSFUL", "--url", "http://ci.example.com", "--repo", "PRJ/demo"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on build set JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "status") {
		t.Fatalf("expected status in JSON output: %s", buf.String())
	}

	// 2. build get (single key, list, JSON)
	cmd = buildcmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"get", "commit1", "--key", "ci1", "--repo", "PRJ/demo"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on build get: %v", err)
	}
	if !strings.Contains(buf.String(), "ci1") {
		t.Fatalf("expected ci1 in output: %s", buf.String())
	}

	cmd = buildcmd.New(depsJSON)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"get", "commit1", "--key", "ci1", "--repo", "PRJ/demo"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on build get JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "ci1") {
		t.Fatalf("expected ci1 in JSON output: %s", buf.String())
	}

	// 3. build delete (dry-run, real, JSON)
	cmd = buildcmd.New(depsDryRun)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"delete", "commit1", "--key", "ci1", "--repo", "PRJ/demo"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on build delete dry-run: %v", err)
	}

	cmd = buildcmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"delete", "commit1", "--key", "ci1", "--repo", "PRJ/demo"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on build delete: %v", err)
	}
	if !strings.Contains(buf.String(), "Deleted repository-scoped build status ci1 on PRJ/demo at commit1") {
		t.Fatalf("expected Deleted repository-scoped build status in output: %s", buf.String())
	}

	cmd = buildcmd.New(depsJSON)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"delete", "commit1", "--key", "ci1", "--repo", "PRJ/demo"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on build delete JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "status") || !strings.Contains(buf.String(), "PRJ/demo") {
		t.Fatalf("expected status and repository in JSON output: %s", buf.String())
	}

	// 4. build required list (human & JSON)
	cmd = buildcmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"required", "list", "--repo", "PRJ/demo"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on build required list: %v", err)
	}
	if !strings.Contains(buf.String(), "501") || !strings.Contains(buf.String(), "ci-required") {
		t.Fatalf("expected 501 and ci-required in list output: %s", buf.String())
	}

	cmd = buildcmd.New(depsJSON)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"required", "list", "--repo", "PRJ/demo"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on build required list JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "501") {
		t.Fatalf("expected 501 in JSON output: %s", buf.String())
	}

	// 5. build required create (body, dry-run, JSON)
	cmd = buildcmd.New(depsDryRun)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"required", "create", "--body", `{"buildParentKeys":["ci-required"]}`, "--repo", "PRJ/demo"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on build required create dry-run: %v", err)
	}

	cmd = buildcmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"required", "create", "--body", `{"buildParentKeys":["ci-required"]}`, "--repo", "PRJ/demo"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on build required create with body: %v", err)
	}
	if !strings.Contains(buf.String(), "ci-required") {
		t.Fatalf("expected ci-required in output: %s", buf.String())
	}

	// 6. build required update (body, dry-run, JSON)
	cmd = buildcmd.New(depsDryRun)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"required", "update", "501", "--body", `{"buildParentKeys":["ci-updated"]}`, "--repo", "PRJ/demo"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on build required update dry-run: %v", err)
	}

	cmd = buildcmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"required", "update", "501", "--body", `{"buildParentKeys":["ci-updated"]}`, "--repo", "PRJ/demo"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on build required update with body: %v", err)
	}
	if !strings.Contains(buf.String(), "ci-updated") {
		t.Fatalf("expected ci-updated in output: %s", buf.String())
	}

	// 7. build required delete (dry-run, real, JSON)
	cmd = buildcmd.New(depsDryRun)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"required", "delete", "501", "--repo", "PRJ/demo"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on build required delete dry-run: %v", err)
	}

	cmd = buildcmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"required", "delete", "501", "--repo", "PRJ/demo"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on build required delete: %v", err)
	}
	if !strings.Contains(buf.String(), "Deleted required build merge check 501") {
		t.Fatalf("expected Deleted required build merge check in output: %s", buf.String())
	}
}

func TestBuildValidationErrors(t *testing.T) {
	server := newMockBuildServer(t)
	deps := newTestDependencies(server.URL, false, false)

	// Missing state in status set
	cmd := buildcmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"status", "set", "commit1", "--key", "ci1"})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error when required flags are missing")
	}

	// Missing repo selector for repo-scoped build get
	buf.Reset()
	cmd.SetArgs([]string{"get", "commit1"})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error when repo selector is missing")
	}

	// Invalid body in required create
	buf.Reset()
	cmd.SetArgs([]string{"required", "create", "--body", "invalid-json", "--repo", "PRJ/demo"})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error on invalid JSON body")
	}

	// Invalid body in required update
	buf.Reset()
	cmd.SetArgs([]string{"required", "update", "501", "--body", "invalid-json", "--repo", "PRJ/demo"})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error on invalid JSON body")
	}
}
