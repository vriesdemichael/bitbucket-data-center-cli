package buildcmd_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	buildcmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/build"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
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
