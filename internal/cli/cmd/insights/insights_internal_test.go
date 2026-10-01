package insightscmd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/safederef"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

func TestInsightsSafeHelpers(t *testing.T) {
	t.Parallel()

	if safederef.String(nil) != "" {
		t.Fatal("expected empty for safederef.String(nil)")
	}
	s := "hello"
	if safederef.String(&s) != "hello" {
		t.Fatal("expected hello for safederef.String(&s)")
	}

	if safeStringFromInsightResult(nil) != "" {
		t.Fatal("expected empty for safeStringFromInsightResult(nil)")
	}
	res := openapigenerated.PASS
	if safeStringFromInsightResult(&res) != "PASS" {
		t.Fatal("expected PASS for safeStringFromInsightResult")
	}
}

func TestInsightsDefaults(t *testing.T) {
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

	// The default loader is the configuration's own. The seal names no host,
	// so it fails the way the configuration does, where a stub would answer
	// or fail in words of its own. Reading BITBUCKET_URL is internal/config's
	// to test.
	if d.LoadConfig == nil || d.LoadConfigAndClient == nil {
		t.Fatal("expected LoadConfig and LoadConfigAndClient to default to non-nil")
	}
	if _, err := d.LoadConfig(); err == nil || !strings.Contains(err.Error(), "no Bitbucket host configured") {
		t.Fatalf("the default LoadConfig is not the configuration's own: %v", err)
	}
	if _, _, err := d.LoadConfigAndClient(); err == nil || !strings.Contains(err.Error(), "no Bitbucket host configured") {
		t.Fatalf("the default LoadConfigAndClient does not load through it: %v", err)
	}

	// Handed a configuration, the default LoadConfigAndClient builds a client for it.
	handed := Dependencies{LoadConfig: func() (config.AppConfig, error) {
		return config.AppConfig{BitbucketURL: "http://bitbucket.invalid"}, nil
	}}
	cfg, client, err := handed.withDefaults().LoadConfigAndClient()
	if err != nil || client == nil || cfg.BitbucketURL != "http://bitbucket.invalid" {
		t.Fatalf("unexpected LoadConfigAndClient result: %v", err)
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

type mockInsightsPermChecker struct {
	repoErr    error
	projectErr error
}

func (m *mockInsightsPermChecker) CheckRepoPermission(ctx context.Context, projectKey, repoSlug string, permission openapigenerated.GetRepositories1ParamsPermission) error {
	return m.repoErr
}

func (m *mockInsightsPermChecker) CheckProjectAdmin(ctx context.Context, projectKey string) error {
	return m.projectErr
}

func TestInsightsDryRunPermissionRejection(t *testing.T) {
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
			return &mockInsightsPermChecker{repoErr: http.ErrAbortHandler, projectErr: http.ErrAbortHandler}
		},
	}

	// Report create dry-run permission rejection
	cmd := New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"report", "create", "commit1", "--key", "report1", "--title", "Report 1", "--repo", "PRJ/demo"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected permission error on report create dry-run")
	}

	// Report delete dry-run permission rejection
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"report", "delete", "commit1", "report1", "--repo", "PRJ/demo"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected permission error on report delete dry-run")
	}

	// Annotation add dry-run permission rejection
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"annotation", "add", "commit1", "report1", "--body", `[{"path":"main.go","line":10,"message":"Fix this","severity":"HIGH"}]`, "--repo", "PRJ/demo"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected permission error on annotation add dry-run")
	}

	// Annotation delete dry-run permission rejection
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"annotation", "delete", "commit1", "report1", "--repo", "PRJ/demo"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected permission error on annotation delete dry-run")
	}
}

// TestInsightsReportDeleteInternal is live now, in
// TestLiveCLICommandCoverage, in both output modes. The unit version deleted
// a report from a handler that answered 204 to every DELETE, which is true of
// a report that was never there.
