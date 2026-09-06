package repocmd_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	repocmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/repo"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
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

func TestRepoAdminDeleteDryRun(t *testing.T) {
	t.Parallel()

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

func TestRepoPermissionsShow(t *testing.T) {
	t.Parallel()

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

// Three suites went live rather than moved.
//
// TestRepoList asserted the output was not empty, against two repositories it
// wrote. TestRepoSettingsWebhooksList asserted the command returned no error
// against one hook it wrote. Both are covered against real repositories and
// real hooks in TestLiveRepoCLICoverage, in both output modes.
//
// TestRepoAdminCreateDryRun asserted dryRun=true in the envelope, from a
// preview whose only input was an empty page. What a preview is worth is
// whether it predicts the right thing, which needs a server that already has
// the repository: TestLiveResourceDryRunPredictionsReadRealState asks for one
// that exists and requires "conflict".
