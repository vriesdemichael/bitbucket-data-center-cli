package repocmd_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	repocmd "github.com/vriesdemichael/bitbucket-server-cli/internal/cli/cmd/repo"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

type mockPermChecker struct{}

func (m *mockPermChecker) CheckRepoPermission(ctx context.Context, projectKey, repoSlug string, permission openapigenerated.GetRepositories1ParamsPermission) error {
	return nil
}
func (m *mockPermChecker) CheckRepoAdmin(ctx context.Context, projectKey, repoSlug string) error {
	return nil
}
func (m *mockPermChecker) CheckProjectAdmin(ctx context.Context, projectKey string) error {
	return nil
}
func (m *mockPermChecker) CheckProjectWrite(ctx context.Context, projectKey string) error {
	return nil
}
func (m *mockPermChecker) InspectRepoPermissions(ctx context.Context, projectKey, repoSlug string) (map[string]bool, error) {
	return map[string]bool{"REPO_READ": true, "REPO_WRITE": true, "REPO_ADMIN": true}, nil
}

func testDeps(serverURL string) repocmd.Dependencies {
	cfg := config.AppConfig{
		BitbucketURL: serverURL,
		ProjectKey:   "PROJ",
	}
	return repocmd.Dependencies{
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
		PermissionChecker: func(c *openapigenerated.ClientWithResponses) repocmd.PermissionChecker {
			return &mockPermChecker{}
		},
	}
}

func TestRepoList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"values": [
				{"slug": "repo1", "name": "Repo 1", "project": {"key": "PROJ"}},
				{"slug": "repo2", "name": "Repo 2", "project": {"key": "PROJ"}}
			],
			"size": 2,
			"isLastPage": true
		}`))
	}))
	defer server.Close()

	deps := testDeps(server.URL)
	cmd := repocmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if out == "" {
		t.Fatal("expected output, got empty")
	}
}

func TestRepoAdminCreateDryRun(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"values": [], "size": 0, "isLastPage": true}`))
	}))
	defer server.Close()

	deps := testDeps(server.URL)
	deps.DryRunEnabled = func() bool { return true }
	deps.JSONEnabled = func() bool { return true }

	cmd := repocmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"admin", "create", "--project", "PROJ", "--name", "new-repo"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var envelope map[string]any
	if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	data, ok := envelope["data"].(map[string]any)
	if !ok || data["dryRun"] != true {
		t.Fatalf("expected dryRun=true, got %v", envelope)
	}
}

func TestRepoAdminDeleteDryRun(t *testing.T) {
	deps := testDeps("http://dummy")
	deps.DryRunEnabled = func() bool { return true }
	deps.JSONEnabled = func() bool { return true }

	cmd := repocmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"admin", "delete", "--repo", "PROJ/my-repo"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var envelope map[string]any
	if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	data, ok := envelope["data"].(map[string]any)
	if !ok || data["dryRun"] != true {
		t.Fatalf("expected dryRun=true, got %v", envelope)
	}
}

func TestRepoSettingsWebhooksList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"values": [
				{"id": 1, "name": "Hook 1", "url": "https://ci.example.com/hook"}
			],
			"size": 1,
			"isLastPage": true
		}`))
	}))
	defer server.Close()

	deps := testDeps(server.URL)
	cmd := repocmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"settings", "workflow", "webhooks", "list", "--repo", "PROJ/my-repo"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRepoPermissionsShow(t *testing.T) {
	deps := testDeps("http://dummy")
	cmd := repocmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"permissions", "show", "--repo", "PROJ/my-repo"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
