package webhookcmd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/safederef"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

type mockPermChecker struct {
	err error
}

func (m *mockPermChecker) CheckRepoPermission(ctx context.Context, projectKey, repoSlug string, permission openapigenerated.GetRepositories1ParamsPermission) error {
	return m.err
}

func TestWebhookHelperFunctions(t *testing.T) {
	b := boolPtr(true)
	if b == nil || !*b {
		t.Fatal("expected boolPtr(true) to be non-nil and true")
	}

	if safederef.String(nil) != "" {
		t.Fatal("expected safederef.String(nil) to be empty string")
	}
	s := "hello"
	if safederef.String(&s) != "hello" {
		t.Fatal("expected safederef.String(&s) to be hello")
	}
}

func TestWebhookWithDefaults(t *testing.T) {
	t.Setenv("BITBUCKET_URL", "http://localhost:7990")
	d := Dependencies{}.withDefaults()
	if d.JSONEnabled == nil || d.JSONEnabled() {
		t.Fatal("expected default JSONEnabled to return false")
	}
	if d.DryRunEnabled == nil || d.DryRunEnabled() {
		t.Fatal("expected default DryRunEnabled to return false")
	}
	if d.WriteJSON == nil {
		t.Fatal("expected default WriteJSON to be non-nil")
	}
	if d.LoadConfig != nil {
		cfg, err := d.LoadConfig()
		if err != nil || cfg.BitbucketURL != "http://localhost:7990" {
			t.Fatalf("unexpected LoadConfig: %v", err)
		}
	}
	if d.LoadConfigAndClient != nil {
		cfg, client, err := d.LoadConfigAndClient()
		if err != nil || client == nil || cfg.BitbucketURL != "http://localhost:7990" {
			t.Fatalf("unexpected LoadConfigAndClient: %v", err)
		}
	}
}

func TestWebhookErrorsAndEdgeCases(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/repo1/webhooks":
			// Non-paginated array payload
			_, _ = w.Write([]byte(`[{"id":123,"name":"wh","url":"http://url","active":true,"events":["repo:refs_changed"]}]`))

		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	cfg := config.AppConfig{
		BitbucketURL: server.URL,
		ProjectKey:   "PRJ",
	}

	// 1. Array payload in list
	deps := Dependencies{
		LoadConfig: func() (config.AppConfig, error) { return cfg, nil },
		LoadConfigAndClient: func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
			client, err := openapi.NewClientWithResponsesFromConfig(cfg)
			return cfg, client, err
		},
	}
	cmd := New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on list array payload: %v", err)
	}
	if !strings.Contains(buf.String(), "wh") {
		t.Fatalf("expected wh in list output: %s", buf.String())
	}

	// 2. Dry-run with permission checker returning error
	deps.DryRunEnabled = func() bool { return true }
	deps.PermissionChecker = func(c *openapigenerated.ClientWithResponses) PermissionChecker {
		return &mockPermChecker{err: http.ErrAbortHandler}
	}

	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"create", "wh", "http://url", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected permission error on create dry-run")
	}

	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"update", "123", "--name", "wh-new", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected permission error on update dry-run")
	}

	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"delete", "123", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected permission error on delete dry-run")
	}

	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"test", "123", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected permission error on test dry-run")
	}
}

// The webhook command suite is live now.
//
// It asserted each command's output against a webhook the file invented.
// The live suite creates a real webhook, lists it, tests it, reads its
// statistics and deletes it.
