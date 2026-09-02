package repocmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

func TestWebhookHelperFunctions(t *testing.T) {
	payload := map[string]any{"values": []any{map[string]any{"id": float64(42), "name": "ci", "url": "http://example.invalid/hook"}}}

	entries := webhookEntries(payload)
	if len(entries) != 1 {
		t.Fatalf("expected one webhook entry, got %d", len(entries))
	}
	if !webhookExistsByNameAndURL(payload, "CI", "http://example.invalid/hook") {
		t.Fatal("expected webhook to match by name+url case-insensitively")
	}
	if !webhookExistsByID(payload, "42") {
		t.Fatal("expected webhook to match by numeric id")
	}
	if webhookExistsByID(payload, "999") {
		t.Fatal("did not expect webhook id 999 to exist")
	}
}

func TestRepoSettingsCommands(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/repo1/webhooks":
			_, _ = w.Write([]byte(`{"values":[{"id":42,"name":"ci-hook","url":"https://ci.example.com/webhook","active":true}]}`))

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/repo1/webhooks":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":43,"name":"new-hook","url":"https://ci.example.com/new","active":true}`))

		case r.Method == http.MethodDelete && path == "/rest/api/latest/projects/PRJ/repos/repo1/webhooks/42":
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/repo1/settings/pull-requests":
			_, _ = w.Write([]byte(`{"requiredApprovers":{"enabled":true,"count":1},"requiredSuccessfulBuilds":1,"requiredAllTasksComplete":true,"mergeConfig":{"strategies":[{"id":"no-ff","enabled":true,"name":"No Fast-Forward"}]}}`))

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/repo1/settings/pull-requests":
			_, _ = w.Write([]byte(`{"requiredApprovers":{"enabled":true,"count":2},"requiredSuccessfulBuilds":1,"requiredAllTasksComplete":true}`))

		case r.Method == http.MethodGet && strings.Contains(path, "/projects/PRJ/repos/repo1/conditions"):
			_, _ = w.Write([]byte(`{"values":[{"id":101,"refMatcher":{"id":"refs/heads/master"},"count":1}]}`))

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/repo1/settings/pull-requests/strategy":
			_, _ = w.Write([]byte(`{"id":"no-ff"}`))

		case r.Method == http.MethodGet && strings.Contains(path, "/settings/auto-merge"):
			_, _ = w.Write([]byte(`{"enabled":true,"strategyId":"no-ff"}`))

		case r.Method == http.MethodPut && strings.Contains(path, "/settings/auto-merge"):
			_, _ = w.Write([]byte(`{"enabled":true,"strategyId":"no-ff"}`))

		case r.Method == http.MethodDelete && strings.Contains(path, "/settings/auto-merge"):
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodGet && strings.Contains(path, "/settings/auto-decline"):
			_, _ = w.Write([]byte(`{"enabled":false}`))

		case r.Method == http.MethodPut && strings.Contains(path, "/settings/auto-decline"):
			_, _ = w.Write([]byte(`{"enabled":true,"inactivityWeeks":4}`))

		case r.Method == http.MethodDelete && strings.Contains(path, "/settings/auto-decline"):
			w.WriteHeader(http.StatusNoContent)

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
	}

	// 1. Webhooks list (human & JSON)
	cmd := New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"settings", "workflow", "webhooks", "list", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on webhooks list: %v", err)
	}
	if !strings.Contains(buf.String(), "Webhooks configured: 1") {
		t.Fatalf("expected Webhooks configured in output: %s", buf.String())
	}
	jsonEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"settings", "workflow", "webhooks", "list", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on webhooks list json: %v", err)
	}
	if !strings.Contains(buf.String(), "webhooks") {
		t.Fatalf("expected webhooks in json output: %s", buf.String())
	}
	jsonEnabled = false

	// 2. Webhooks create (dry-run, conflict dry-run, real, json)
	dryRunEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"settings", "workflow", "webhooks", "create", "new-hook", "https://ci.example.com/new", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on webhooks create dry-run: %v", err)
	}
	// Conflict preview
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"settings", "workflow", "webhooks", "create", "ci-hook", "https://ci.example.com/webhook", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on webhooks create dry-run conflict: %v", err)
	}
	dryRunEnabled = false

	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"settings", "workflow", "webhooks", "create", "new-hook", "https://ci.example.com/new", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on webhooks create: %v", err)
	}
	if !strings.Contains(buf.String(), "Webhook created") {
		t.Fatalf("expected Webhook created in output: %s", buf.String())
	}

	// 3. Webhooks delete (dry-run, real, json)
	dryRunEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"settings", "workflow", "webhooks", "delete", "42", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on webhooks delete dry-run: %v", err)
	}
	dryRunEnabled = false

	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"settings", "workflow", "webhooks", "delete", "42", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on webhooks delete: %v", err)
	}
	if !strings.Contains(buf.String(), "Webhook deleted") {
		t.Fatalf("expected Webhook deleted in output: %s", buf.String())
	}

	// 4. Pull-requests settings get
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"settings", "pull-requests", "get", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on pull-requests get: %v", err)
	}
	if !strings.Contains(buf.String(), "Required approvers: 1") {
		t.Fatalf("expected Required approvers in output: %s", buf.String())
	}

	// 5. Pull-requests merge-checks list
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"settings", "pull-requests", "merge-checks", "list", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on merge-checks list: %v", err)
	}
	if !strings.Contains(buf.String(), "101") {
		t.Fatalf("expected 101 in output: %s", buf.String())
	}

	// 6. Pull-requests update-approvers & set-strategy
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"settings", "pull-requests", "update-approvers", "--count", "2", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on update-approvers: %v", err)
	}
	if !strings.Contains(buf.String(), "Updated pull-request settings: requiredApprovers=2") {
		t.Fatalf("expected Required approvers updated in output: %s", buf.String())
	}

	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"settings", "pull-requests", "set-strategy", "no-ff", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on set-strategy: %v", err)
	}
	if !strings.Contains(buf.String(), "Updated default merge strategy to no-ff") {
		t.Fatalf("expected Merge strategy updated in output: %s", buf.String())
	}

	// 7. Auto-merge get, set, delete
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"settings", "auto-merge", "get", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on auto-merge get: %v", err)
	}
	if !strings.Contains(buf.String(), "Auto-merge enabled") {
		t.Fatalf("expected Auto-merge enabled in output: %s", buf.String())
	}

	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"settings", "auto-merge", "set", "--enabled", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on auto-merge set: %v", err)
	}
	if !strings.Contains(buf.String(), "Updated auto-merge settings: enabled=true") {
		t.Fatalf("expected Updated auto-merge settings in output: %s", buf.String())
	}

	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"settings", "auto-merge", "delete", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on auto-merge delete: %v", err)
	}
	if !strings.Contains(buf.String(), "Deleted auto-merge settings") {
		t.Fatalf("expected Deleted auto-merge settings in output: %s", buf.String())
	}

	// 8. Auto-decline get, set, delete
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"settings", "auto-decline", "get", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on auto-decline get: %v", err)
	}
	if !strings.Contains(buf.String(), "Auto-decline enabled: false") {
		t.Fatalf("expected Auto-decline enabled: false in output: %s", buf.String())
	}

	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"settings", "auto-decline", "set", "--enabled", "--inactivity-weeks", "4", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on auto-decline set: %v", err)
	}
	if !strings.Contains(buf.String(), "Updated auto-decline settings: enabled=true inactivityWeeks=4") {
		t.Fatalf("expected Updated auto-decline settings in output: %s", buf.String())
	}

	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"settings", "auto-decline", "delete", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on auto-decline delete: %v", err)
	}
	if !strings.Contains(buf.String(), "Deleted auto-decline settings") {
		t.Fatalf("expected Deleted auto-decline settings in output: %s", buf.String())
	}
}

func TestRepoSettingsJSONAndDryRunModes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/repo1/settings/pull-requests":
			_, _ = w.Write([]byte(`{"requiredApprovers":{"enabled":true,"count":1},"requiredSuccessfulBuilds":1,"requiredAllTasksComplete":true,"mergeConfig":{"strategies":[{"id":"no-ff","enabled":true,"name":"No Fast-Forward"}]}}`))

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/repo1/settings/pull-requests":
			_, _ = w.Write([]byte(`{"requiredApprovers":{"enabled":true,"count":1},"requiredSuccessfulBuilds":1,"requiredAllTasksComplete":false}`))

		case r.Method == http.MethodGet && strings.Contains(path, "/projects/PRJ/repos/repo1/conditions"):
			_, _ = w.Write([]byte(`{"values":[{"id":101,"refMatcher":{"id":"refs/heads/master"},"count":1}]}`))

		case r.Method == http.MethodGet && strings.Contains(path, "/settings/auto-merge"):
			_, _ = w.Write([]byte(`{"enabled":true,"strategyId":"no-ff"}`))

		case r.Method == http.MethodPut && strings.Contains(path, "/settings/auto-merge"):
			_, _ = w.Write([]byte(`{"enabled":true,"strategyId":"no-ff"}`))

		case r.Method == http.MethodDelete && strings.Contains(path, "/settings/auto-merge"):
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodGet && strings.Contains(path, "/settings/auto-decline"):
			_, _ = w.Write([]byte(`{"enabled":true,"inactivityWeeks":4}`))

		case r.Method == http.MethodPut && strings.Contains(path, "/settings/auto-decline"):
			_, _ = w.Write([]byte(`{"enabled":true,"inactivityWeeks":4}`))

		case r.Method == http.MethodDelete && strings.Contains(path, "/settings/auto-decline"):
			w.WriteHeader(http.StatusNoContent)

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
	}

	// 1. Pull requests get JSON
	jsonEnabled = true
	cmd := New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"settings", "pull-requests", "get", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on pull-requests get JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "requiredApprovers") {
		t.Fatalf("expected the pull request settings in JSON output: %s", buf.String())
	}

	// 2. Pull requests merge-checks list JSON
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"settings", "pull-requests", "merge-checks", "list", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on merge-checks list JSON: %v", err)
	}
	if !strings.Contains(buf.String(), `"checks"`) {
		t.Fatalf("expected the merge checks in JSON output: %s", buf.String())
	}

	// 3. Pull requests update (dry-run noop, dry-run update, real execution, JSON)
	dryRunEnabled = true
	jsonEnabled = false
	// Noop dry-run (required-all-tasks already true)
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"settings", "pull-requests", "update", "--required-all-tasks-complete", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on update dry-run noop: %v", err)
	}

	// Update dry-run (required-all-tasks false != true)
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"settings", "pull-requests", "update", "--required-all-tasks-complete=false", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on update dry-run update: %v", err)
	}

	// Real execution
	dryRunEnabled = false
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"settings", "pull-requests", "update", "--required-all-tasks-complete=false", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on update real: %v", err)
	}
	if !strings.Contains(buf.String(), "requiredAllTasksComplete=false") {
		t.Fatalf("expected requiredAllTasksComplete=false in output: %s", buf.String())
	}

	// JSON mode
	jsonEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"settings", "pull-requests", "update", "--required-all-tasks-complete=false", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on update JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "requiredApprovers") {
		t.Fatalf("expected pull_request_settings in JSON: %s", buf.String())
	}
	jsonEnabled = false

	// 4. Auto-merge JSON modes
	jsonEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"settings", "auto-merge", "get", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on auto-merge get JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "enabled") {
		t.Fatalf("expected enabled in JSON: %s", buf.String())
	}

	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"settings", "auto-merge", "set", "--enabled", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on auto-merge set JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "enabled") {
		t.Fatalf("expected enabled in JSON: %s", buf.String())
	}

	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"settings", "auto-merge", "delete", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on auto-merge delete JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "deleted") {
		t.Fatalf("expected deleted in JSON: %s", buf.String())
	}

	// 5. Auto-decline JSON modes
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"settings", "auto-decline", "get", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on auto-decline get JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "enabled") {
		t.Fatalf("expected enabled in JSON: %s", buf.String())
	}

	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"settings", "auto-decline", "set", "--enabled", "--inactivity-weeks", "4", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on auto-decline set JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "inactivityWeeks") {
		t.Fatalf("expected inactivityWeeks in JSON: %s", buf.String())
	}

	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"settings", "auto-decline", "delete", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on auto-decline delete JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "deleted") {
		t.Fatalf("expected deleted in JSON: %s", buf.String())
	}
	jsonEnabled = false

	// 6. Auto-merge & Auto-decline Human modes
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"settings", "auto-merge", "get", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on auto-merge get: %v", err)
	}
	if !strings.Contains(buf.String(), "Auto-merge enabled:") {
		t.Fatalf("expected Auto-merge enabled: in output: %s", buf.String())
	}

	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"settings", "auto-decline", "get", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on auto-decline get: %v", err)
	}
	if !strings.Contains(buf.String(), "Auto-decline enabled:") {
		t.Fatalf("expected Auto-decline enabled: in output: %s", buf.String())
	}
}
