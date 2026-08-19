package deploymentcmd_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	deploymentcmd "github.com/vriesdemichael/bitbucket-server-cli/internal/cli/cmd/deployment"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

type testPermissionChecker struct{}

func (testPermissionChecker) CheckRepoPermission(ctx context.Context, projectKey, repoSlug string, permission openapigenerated.GetRepositories1ParamsPermission) error {
	return nil
}

func newMockDeploymentServer(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/demo/commits/commit1/deployments":
			_, _ = w.Write([]byte(`{"key":"dep1","displayName":"Deployment 1","state":"SUCCESSFUL","url":"http://dep.example.com"}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/commits/commit1/deployments":
			_, _ = w.Write([]byte(`{"key":"dep1","displayName":"Deployment 1","state":"SUCCESSFUL","url":"http://dep.example.com"}`))

		case r.Method == http.MethodDelete && path == "/rest/api/latest/projects/PRJ/repos/demo/commits/commit1/deployments":
			w.WriteHeader(http.StatusNoContent)

		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	return server
}

func newTestDependencies(serverURL string, jsonMode bool, dryRun bool) deploymentcmd.Dependencies {
	cfg := config.AppConfig{
		BitbucketURL: serverURL,
		ProjectKey:   "PRJ",
	}

	return deploymentcmd.Dependencies{
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
		PermissionChecker: func(client *openapigenerated.ClientWithResponses) deploymentcmd.PermissionChecker {
			return testPermissionChecker{}
		},
	}
}

func TestDeploymentCreateAndGet(t *testing.T) {
	server := newMockDeploymentServer(t)
	deps := newTestDependencies(server.URL, false, false)

	cmd := deploymentcmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"create", "commit1",
		"--deployment-sequence-number", "1",
		"--display-name", "Deployment 1",
		"--key", "dep1",
		"--state", "SUCCESSFUL",
		"--url", "http://dep.example.com",
		"--env-key", "prod",
		"--env-name", "Production",
		"--repo", "PRJ/demo",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on create: %v", err)
	}

	buf.Reset()
	cmd.SetArgs([]string{"get", "commit1", "--repo", "PRJ/demo"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on get: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "dep1") {
		t.Fatalf("expected 'dep1' in output, got: %s", out)
	}
}
