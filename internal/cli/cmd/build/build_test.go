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
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		case r.Method == http.MethodPost && path == "/rest/build-status/latest/commits/commit1":
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodGet && path == "/rest/build-status/latest/commits/commit1":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"key":"ci1","state":"SUCCESSFUL","url":"http://ci.example.com"}]}`))

		case r.Method == http.MethodGet && path == "/rest/build-status/latest/commits/stats/commit1":
			_, _ = w.Write([]byte(`{"successful":1,"failed":0,"inProgress":0,"unknown":0,"cancelled":0}`))

		case r.Method == http.MethodPost && path == "/rest/api/1.0/projects/PRJ/repos/demo/commits/commit1/builds":
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodGet && path == "/rest/api/1.0/projects/PRJ/repos/demo/commits/commit1/builds":
			_, _ = w.Write([]byte(`{"key":"ci1","state":"SUCCESSFUL","url":"http://ci.example.com"}`))

		case r.Method == http.MethodDelete && path == "/rest/api/1.0/projects/PRJ/repos/demo/commits/commit1/builds":
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
	deps := newTestDependencies(server.URL, false, false)

	cmd := buildcmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"status", "set", "commit1", "--key", "ci1", "--state", "SUCCESSFUL", "--url", "http://ci.example.com"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on set: %v", err)
	}

	buf.Reset()
	cmd.SetArgs([]string{"status", "get", "commit1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on get: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "ci1") {
		t.Fatalf("expected 'ci1' in output, got: %s", out)
	}
}

func TestBuildStatusStats(t *testing.T) {
	server := newMockBuildServer(t)
	deps := newTestDependencies(server.URL, false, false)

	cmd := buildcmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"status", "stats", "commit1"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on stats: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Successful:") {
		t.Fatalf("expected 'Successful:' in output, got: %s", out)
	}
}
