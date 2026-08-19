package reviewergroupcmd

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

func TestReviewerGroupCommands(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/repo1/settings/reviewer-groups":
			_, _ = w.Write([]byte(`{"values":[{"id":201,"name":"team-a","description":"Team A"}]}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/settings/reviewer-groups":
			_, _ = w.Write([]byte(`{"values":[{"id":202,"name":"team-b","description":"Team B"}]}`))

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/repo1/settings/reviewer-groups":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":203,"name":"team-c"}`))

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/settings/reviewer-groups":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":204,"name":"team-d"}`))

		case r.Method == http.MethodDelete && path == "/rest/api/latest/projects/PRJ/repos/repo1/settings/reviewer-groups/201":
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodDelete && path == "/rest/api/latest/projects/PRJ/settings/reviewer-groups/202":
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/repo1/settings/reviewer-groups/201/users":
			_, _ = w.Write([]byte(`[{"name":"alice","displayName":"Alice"}]`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/settings/reviewer-groups/202/users":
			_, _ = w.Write([]byte(`[{"name":"bob","displayName":"Bob"}]`))

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

	// 1. list repo in human mode
	cmd := New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on list repo: %v", err)
	}
	if !strings.Contains(buf.String(), "team-a") {
		t.Fatalf("expected team-a in list output: %s", buf.String())
	}

	// 2. list repo in JSON mode
	jsonEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on list repo JSON: %v", err)
	}
	if !strings.Contains(buf.String(), `"reviewer_groups"`) {
		t.Fatalf("expected reviewer_groups in JSON output: %s", buf.String())
	}
	jsonEnabled = false

	// 3. list project
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list", "--project", "PRJ"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on list project: %v", err)
	}
	if !strings.Contains(buf.String(), "team-b") {
		t.Fatalf("expected team-b in list output: %s", buf.String())
	}

	// 4. create repo group in dry-run mode
	dryRunEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"create", "team-c", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on create repo dry-run: %v", err)
	}
	dryRunEnabled = false

	// 5. create repo group
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"create", "team-c", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on create repo: %v", err)
	}
	if !strings.Contains(buf.String(), "Created reviewer group") {
		t.Fatalf("expected Created reviewer group in create output: %s", buf.String())
	}

	// 6. create project group in dry-run mode
	dryRunEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"create", "team-d", "--project", "PRJ"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on create project dry-run: %v", err)
	}
	dryRunEnabled = false

	// 7. create project group
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"create", "team-d", "--project", "PRJ"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on create project: %v", err)
	}
	if !strings.Contains(buf.String(), "Created reviewer group") {
		t.Fatalf("expected Created reviewer group in create output: %s", buf.String())
	}

	// 6. users on repo group
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"users", "201", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on users: %v", err)
	}
	if !strings.Contains(buf.String(), "alice") {
		t.Fatalf("expected alice in users output: %s", buf.String())
	}

	// 7. users on project group is not supported -> returns error
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"users", "202", "--project", "PRJ"})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error on project users (only repo supported)")
	}

	// 8. delete repo group in dry-run mode
	dryRunEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"delete", "201", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on delete dry-run: %v", err)
	}
	dryRunEnabled = false

	// 9. delete repo group
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"delete", "201", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on delete repo: %v", err)
	}
	if !strings.Contains(buf.String(), "Deleted reviewer group") {
		t.Fatalf("expected Deleted reviewer group in delete output: %s", buf.String())
	}

	// 10. delete project group
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"delete", "202", "--project", "PRJ"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on delete project: %v", err)
	}
	if !strings.Contains(buf.String(), "Deleted reviewer group") {
		t.Fatalf("expected Deleted reviewer group in delete output: %s", buf.String())
	}

	// 11. missing project key validation error
	cfgNoProject := config.AppConfig{BitbucketURL: server.URL}
	depsNoProject := deps
	depsNoProject.LoadConfig = func() (config.AppConfig, error) { return cfgNoProject, nil }
	depsNoProject.LoadConfigAndClient = func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
		client, err := openapi.NewClientWithResponsesFromConfig(cfgNoProject)
		return cfgNoProject, client, err
	}
	cmd = New(depsNoProject)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error when project key is missing")
	}
}
