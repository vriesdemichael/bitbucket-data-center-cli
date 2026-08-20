package tagcmd_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tagcmd "github.com/vriesdemichael/bitbucket-server-cli/internal/cli/cmd/tag"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

type testPermissionChecker struct{}

func (testPermissionChecker) CheckRepoPermission(ctx context.Context, projectKey, repoSlug string, permission openapigenerated.GetRepositories1ParamsPermission) error {
	return nil
}

func newMockTagServer(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/tags":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":"refs/tags/v1.0","displayId":"v1.0","type":"ANNOTATED","latestCommit":"1111111"}]}`))

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/demo/tags":
			_, _ = w.Write([]byte(`{"id":"refs/tags/v1.1","displayId":"v1.1","type":"ANNOTATED","latestCommit":"2222222"}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/tags/v1.0":
			_, _ = w.Write([]byte(`{"id":"refs/tags/v1.0","displayId":"v1.0","type":"ANNOTATED","latestCommit":"1111111"}`))

		case r.Method == http.MethodDelete && path == "/rest/git/latest/projects/PRJ/repos/demo/tags/v1.0":
			w.WriteHeader(http.StatusNoContent)

		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	return server
}

func newTestDependencies(t *testing.T, serverURL string, jsonMode bool, dryRun bool) tagcmd.Dependencies {
	cfg := config.AppConfig{
		BitbucketURL: serverURL,
		ProjectKey:   "PRJ",
	}
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	return tagcmd.Dependencies{
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
		PermissionChecker: func(client *openapigenerated.ClientWithResponses) tagcmd.PermissionChecker {
			return testPermissionChecker{}
		},
	}
}

func TestTagList(t *testing.T) {
	server := newMockTagServer(t)

	// Human mode
	deps := newTestDependencies(t, server.URL, false, false)
	cmd := tagcmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on list: %v", err)
	}
	if !strings.Contains(buf.String(), "v1.0") {
		t.Fatalf("expected 'v1.0' in output, got: %s", buf.String())
	}

	// JSON mode
	depsJSON := newTestDependencies(t, server.URL, true, false)
	cmd = tagcmd.New(depsJSON)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on list json: %v", err)
	}
	if !strings.Contains(buf.String(), "tags") {
		t.Fatalf("expected 'tags' in json output, got: %s", buf.String())
	}
}

func TestTagCreate(t *testing.T) {
	server := newMockTagServer(t)

	// Dry run mode
	depsDryRun := newTestDependencies(t, server.URL, false, true)
	cmd := tagcmd.New(depsDryRun)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"create", "v1.1", "--start-point", "main"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on create dry-run: %v", err)
	}

	// Real execution
	deps := newTestDependencies(t, server.URL, false, false)
	cmd = tagcmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"create", "v1.1", "--start-point", "main"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on create: %v", err)
	}
	if !strings.Contains(buf.String(), "Created tag") {
		t.Fatalf("expected 'Created tag' in output, got: %s", buf.String())
	}

	// JSON execution
	depsJSON := newTestDependencies(t, server.URL, true, false)
	cmd = tagcmd.New(depsJSON)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"create", "v1.1", "--start-point", "main"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on create json: %v", err)
	}
	if !strings.Contains(buf.String(), "tag") {
		t.Fatalf("expected 'tag' in json output, got: %s", buf.String())
	}
}

func TestTagViewAndDelete(t *testing.T) {
	server := newMockTagServer(t)

	// View human
	deps := newTestDependencies(t, server.URL, false, false)
	cmd := tagcmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"view", "v1.0"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on view: %v", err)
	}
	if !strings.Contains(buf.String(), "Tag: v1.0") {
		t.Fatalf("expected 'Tag: v1.0' in output, got: %s", buf.String())
	}

	// View JSON
	depsJSON := newTestDependencies(t, server.URL, true, false)
	cmd = tagcmd.New(depsJSON)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"view", "v1.0"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on view json: %v", err)
	}
	if !strings.Contains(buf.String(), "tag") {
		t.Fatalf("expected 'tag' in json output, got: %s", buf.String())
	}

	// Delete dry run
	depsDryRun := newTestDependencies(t, server.URL, false, true)
	cmd = tagcmd.New(depsDryRun)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"delete", "v1.0"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on delete dry-run: %v", err)
	}

	// Delete real execution
	cmd = tagcmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"delete", "v1.0"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on delete: %v", err)
	}
	if !strings.Contains(buf.String(), "Deleted tag v1.0") {
		t.Fatalf("expected 'Deleted tag v1.0' in output, got: %s", buf.String())
	}
}
