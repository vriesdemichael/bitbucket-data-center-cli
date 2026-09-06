package buildcmd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/safederef"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

func TestBuildSafeHelpers(t *testing.T) {
	t.Parallel()

	if safederef.String(nil) != "" {
		t.Fatal("expected empty for safederef.String(nil)")
	}
	s := "hello"
	if safederef.String(&s) != "hello" {
		t.Fatal("expected hello for safederef.String(&s)")
	}

	if safederef.Int32(nil) != 0 {
		t.Fatal("expected 0 for safederef.Int32(nil)")
	}
	i32 := int32(42)
	if safederef.Int32(&i32) != 42 {
		t.Fatal("expected 42 for safederef.Int32(&i32)")
	}

	if safederef.Int64(nil) != 0 {
		t.Fatal("expected 0 for safederef.Int64(nil)")
	}
	i64 := int64(100)
	if safederef.Int64(&i64) != 100 {
		t.Fatal("expected 100 for safederef.Int64(&i64)")
	}

	if len(safederef.StringSlice(nil)) != 0 {
		t.Fatal("expected empty slice for safederef.StringSlice(nil)")
	}
	slice := []string{"a", "b"}
	if len(safederef.StringSlice(&slice)) != 2 {
		t.Fatal("expected 2 elements for safederef.StringSlice(&slice)")
	}

	if safeStringFromBuildState(nil) != "" {
		t.Fatal("expected empty for safeStringFromBuildState(nil)")
	}
	st := openapigenerated.RestBuildStatusStateSUCCESSFUL
	if safeStringFromBuildState(&st) != "SUCCESSFUL" {
		t.Fatal("expected SUCCESSFUL for safeStringFromBuildState")
	}
}

func TestBuildDefaults(t *testing.T) {
	t.Setenv("BITBUCKET_URL", "http://localhost:7990")
	var deps Dependencies
	d := deps.withDefaults()

	if d.JSONEnabled == nil || d.JSONEnabled() {
		t.Fatal("expected JSONEnabled to default to false")
	}
	if d.DryRunEnabled == nil || d.DryRunEnabled() {
		t.Fatal("expected DryRunEnabled to default to false")
	}
	if d.WriteJSON == nil || d.WriteJSONList == nil {
		t.Fatal("expected WriteJSON and WriteJSONList to default")
	}

	if d.LoadConfig != nil {
		cfg, err := d.LoadConfig()
		if err != nil || cfg.BitbucketURL != "http://localhost:7990" {
			t.Fatalf("unexpected LoadConfig result: %v", err)
		}
	}
	if d.LoadConfigAndClient != nil {
		cfg, client, err := d.LoadConfigAndClient()
		if err != nil || client == nil || cfg.BitbucketURL != "http://localhost:7990" {
			t.Fatalf("unexpected LoadConfigAndClient result: %v", err)
		}
	}
}

func TestResolveQualityRepoServiceAndClientErrors(t *testing.T) {
	t.Parallel()

	// Error loading config
	badDeps := Dependencies{
		LoadConfigAndClient: func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
			return config.AppConfig{}, nil, http.ErrAbortHandler
		},
	}
	if _, _, _, err := resolveQualityRepoServiceAndClient("PRJ/repo", badDeps); err == nil {
		t.Fatal("expected error on bad config")
	}

	// Error resolving repo selector
	cfg := config.AppConfig{}
	deps := Dependencies{
		LoadConfigAndClient: func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
			return cfg, nil, nil
		},
	}
	if _, _, _, err := resolveQualityRepoServiceAndClient("", deps); err == nil {
		t.Fatal("expected error on empty repo selector without env")
	}
}

type mockBuildPermChecker struct {
	repoErr    error
	projectErr error
}

func (m *mockBuildPermChecker) CheckRepoPermission(ctx context.Context, projectKey, repoSlug string, permission openapigenerated.GetRepositories1ParamsPermission) error {
	return m.repoErr
}

func (m *mockBuildPermChecker) CheckProjectAdmin(ctx context.Context, projectKey string) error {
	return m.projectErr
}

func TestBuildDryRunPermissionRejection(t *testing.T) {
	t.Parallel()

	// A listener that fails the test if it is reached, which is the
	// assertion: every case here is refused before a request exists.
	guard := httptest.NewServer(testsupport.UnreachedHandler(t))
	t.Cleanup(guard.Close)
	serverURL := guard.URL

	cfg := config.AppConfig{BitbucketURL: serverURL, ProjectKey: "PRJ"}
	deps := Dependencies{
		DryRunEnabled: func() bool { return true },
		LoadConfig:    func() (config.AppConfig, error) { return cfg, nil },
		LoadConfigAndClient: func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
			client, err := openapi.NewClientWithResponsesFromConfig(cfg)
			return cfg, client, err
		},
		PermissionChecker: func(c *openapigenerated.ClientWithResponses) PermissionChecker {
			return &mockBuildPermChecker{repoErr: http.ErrAbortHandler, projectErr: http.ErrAbortHandler}
		},
	}

	// Required create dry-run permission rejection
	cmd := New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"required", "create", "--body", `{"buildParentKeys":["ci"]}`, "--repo", "PRJ/demo"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected permission error on required create dry-run")
	}

	// Required update dry-run permission rejection
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"required", "update", "101", "--body", `{"buildParentKeys":["ci"]}`, "--repo", "PRJ/demo"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected permission error on required update dry-run")
	}

	// Required delete dry-run permission rejection
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"required", "delete", "101", "--repo", "PRJ/demo"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected permission error on required delete dry-run")
	}
}

// TestBuildStatusSetExtendedFlags is live now, in
// TestLiveCLICommandCoverage: the same ten flags, and then a `build status
// get` that has to find them. The unit version posted them at a handler that
// answered 204 to anything, so it could only fail if the command did not send
// a POST at all.
