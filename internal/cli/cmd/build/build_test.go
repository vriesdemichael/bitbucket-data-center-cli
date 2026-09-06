package buildcmd_test

import (
	"bytes"
	"context"
	"net/http/httptest"
	"testing"

	buildcmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/build"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

type testPermissionChecker struct{}

func (testPermissionChecker) CheckRepoPermission(ctx context.Context, projectKey, repoSlug string, permission openapigenerated.GetRepositories1ParamsPermission) error {
	return nil
}

// newMockBuildServer opens a listener that fails the test if anything reaches it.
//
// Everything still here refuses before it asks -- bad flags, a missing repository selector, a body that is not JSON, a permission that is denied. The handwritten Bitbucket it used to be answered build statuses nobody reads any more, and would have answered a request that should never have been made.
func newMockBuildServer(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(testsupport.UnreachedHandler(t))
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
	t.Parallel()

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
