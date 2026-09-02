package webhookcmd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

	if safeString(nil) != "" {
		t.Fatal("expected safeString(nil) to be empty string")
	}
	s := "hello"
	if safeString(&s) != "hello" {
		t.Fatal("expected safeString(&s) to be hello")
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

func TestWebhookCommands(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/repo1/webhooks":
			_, _ = w.Write([]byte(`{"values":[{"id":123,"name":"wh","url":"http://url","active":true,"events":["repo:refs_changed"]}]}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/repo1/webhooks/123":
			_, _ = w.Write([]byte(`{"id":123,"name":"wh","url":"http://url","active":true}`))

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/repo1/webhooks":
			_, _ = w.Write([]byte(`{"id":456,"name":"wh-created","url":"http://url-created","active":true,"events":["repo:refs_changed"]}`))

		case r.Method == http.MethodPut && path == "/rest/api/latest/projects/PRJ/repos/repo1/webhooks/123":
			_, _ = w.Write([]byte(`{"id":123,"name":"wh-new","url":"http://url","active":true}`))

		case r.Method == http.MethodDelete && path == "/rest/api/latest/projects/PRJ/repos/repo1/webhooks/123":
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/repo1/webhooks/test":
			_, _ = w.Write([]byte(`{"status":"ok"}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/repo1/webhooks/123/statistics/summary":
			_, _ = w.Write([]byte(`{"successfulInvocations":5,"failedInvocations":0}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/repo1/webhooks/123/statistics":
			_, _ = w.Write([]byte(`{"invocations":[]}`))

		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	cfg := config.AppConfig{
		BitbucketURL: server.URL,
		ProjectKey:   "PRJ",
	}

	jsonEnabled := false
	dryRunEnabled := false

	deps := Dependencies{
		JSONEnabled:   func() bool { return jsonEnabled },
		DryRunEnabled: func() bool { return dryRunEnabled },
		LoadConfig:    func() (config.AppConfig, error) { return cfg, nil },
		LoadConfigAndClient: func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
			client, err := openapi.NewClientWithResponsesFromConfig(cfg)
			return cfg, client, err
		},
		PermissionChecker: func(c *openapigenerated.ClientWithResponses) PermissionChecker {
			return &mockPermChecker{}
		},
	}

	// 1. List (human, JSON, pagination, empty)
	cmd := New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on list: %v", err)
	}
	if !strings.Contains(buf.String(), "wh") {
		t.Fatalf("expected wh in list output: %s", buf.String())
	}

	jsonEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on list JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "wh") {
		t.Fatalf("expected wh in list JSON output: %s", buf.String())
	}
	jsonEnabled = false

	// List with start out of bounds (empty)
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list", "--repo", "PRJ/repo1", "--start", "10"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on list start 10: %v", err)
	}
	if !strings.Contains(buf.String(), "No webhooks found") {
		t.Fatalf("expected No webhooks found in output: %s", buf.String())
	}

	// 2. Get (human & JSON)
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"get", "123", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on get: %v", err)
	}
	if !strings.Contains(buf.String(), "wh") {
		t.Fatalf("expected wh in get output: %s", buf.String())
	}

	jsonEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"get", "123", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on get JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "webhook") {
		t.Fatalf("expected webhook in get JSON output: %s", buf.String())
	}
	jsonEnabled = false

	// 3. Update (dry-run, validation error, active=true, active=false, JSON)
	dryRunEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"update", "123", "--name", "wh-new", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on update dry-run: %v", err)
	}

	dryRunEnabled = false
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"update", "123", "--active", "invalid", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for invalid active flag value")
	}

	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"update", "123", "--name", "wh-new", "--active", "true", "--event", "repo:refs_changed", "--url", "http://new-url", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on update: %v", err)
	}
	if !strings.Contains(buf.String(), "Updated webhook") {
		t.Fatalf("expected Updated webhook in update output: %s", buf.String())
	}

	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"update", "123", "--active", "false", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on update active false: %v", err)
	}

	jsonEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"update", "123", "--name", "wh-new", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on update JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "status") {
		t.Fatalf("expected status in update JSON output: %s", buf.String())
	}
	jsonEnabled = false

	// 4. Test (dry-run, real, custom URL, JSON)
	dryRunEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"test", "123", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on test dry-run: %v", err)
	}

	dryRunEnabled = false
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"test", "123", "--url", "http://custom-url", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on test: %v", err)
	}
	if !strings.Contains(buf.String(), "status") {
		t.Fatalf("expected status in test output: %s", buf.String())
	}

	jsonEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"test", "123", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on test JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "status") {
		t.Fatalf("expected status in test JSON output: %s", buf.String())
	}
	jsonEnabled = false

	// 5. Stats (detailed, summary, JSON)
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"stats", "123", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on stats: %v", err)
	}
	if !strings.Contains(buf.String(), "invocations") {
		t.Fatalf("expected invocations in stats output: %s", buf.String())
	}

	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"stats", "123", "--summary", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on stats summary: %v", err)
	}
	if !strings.Contains(buf.String(), "successfulInvocations") {
		t.Fatalf("expected successfulInvocations in stats summary output: %s", buf.String())
	}

	jsonEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"stats", "123", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on stats JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "invocations") {
		t.Fatalf("expected invocations in stats JSON output: %s", buf.String())
	}
	jsonEnabled = false

	// 6. Create (dry-run, real, JSON)
	dryRunEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"create", "wh-created", "http://url-created", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on create dry-run: %v", err)
	}

	dryRunEnabled = false
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"create", "wh-created", "http://url-created", "--event", "repo:refs_changed", "--active=true", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on create: %v", err)
	}
	if !strings.Contains(buf.String(), "Created webhook") || !strings.Contains(buf.String(), "456") {
		t.Fatalf("expected Created webhook 456 in create output: %s", buf.String())
	}

	jsonEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"create", "wh-created", "http://url-created", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on create JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "wh-created") {
		t.Fatalf("expected wh-created in create JSON output: %s", buf.String())
	}
	jsonEnabled = false

	// 7. Delete (dry-run, real, JSON)
	dryRunEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"delete", "123", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on delete dry-run: %v", err)
	}

	dryRunEnabled = false
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"delete", "123", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on delete: %v", err)
	}
	if !strings.Contains(buf.String(), "Deleted webhook") {
		t.Fatalf("expected Deleted webhook in delete output: %s", buf.String())
	}

	jsonEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"delete", "123", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on delete JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "ok") {
		t.Fatalf("expected ok in delete JSON output: %s", buf.String())
	}
	jsonEnabled = false
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
