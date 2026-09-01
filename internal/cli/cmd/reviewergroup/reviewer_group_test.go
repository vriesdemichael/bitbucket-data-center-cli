package reviewergroupcmd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

func TestReviewerGroupCommands(t *testing.T) {
	emptyRepoList := false
	emptyUserList := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/repo1/settings/reviewer-groups":
			if emptyRepoList {
				_, _ = w.Write([]byte(`{"values":[]}`))
				return
			}
			_, _ = w.Write([]byte(`{"values":[{"id":201,"name":"team-a","description":"Team A"}]}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/settings/reviewer-groups":
			_, _ = w.Write([]byte(`{"values":[{"id":202,"name":"team-b","description":"Team B"}]}`))

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/repo1/settings/reviewer-groups":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":203,"name":"team-c"}`))

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/settings/reviewer-groups":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":204,"name":"team-d"}`))

		case r.Method == http.MethodPut && path == "/rest/api/latest/projects/PRJ/repos/repo1/settings/reviewer-groups/201":
			_, _ = w.Write([]byte(`{"id":201,"name":"team-a-updated","description":"Team A Updated"}`))

		case r.Method == http.MethodPut && path == "/rest/api/latest/projects/PRJ/settings/reviewer-groups/202":
			_, _ = w.Write([]byte(`{"id":202,"name":"team-b-updated","description":"Team B Updated"}`))

		case r.Method == http.MethodDelete && path == "/rest/api/latest/projects/PRJ/repos/repo1/settings/reviewer-groups/201":
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodDelete && path == "/rest/api/latest/projects/PRJ/settings/reviewer-groups/202":
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/repo1/settings/reviewer-groups/201/users":
			if emptyUserList {
				_, _ = w.Write([]byte(`[]`))
				return
			}
			_, _ = w.Write([]byte(`[{"name":"alice","displayName":"Alice","emailAddress":"alice@example.com","active":true}]`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/settings/reviewer-groups/202/users":
			_, _ = w.Write([]byte(`[{"name":"bob","displayName":"Bob","emailAddress":"bob@example.com","active":false}]`))

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
	if !strings.Contains(buf.String(), `"reviewerGroups"`) {
		t.Fatalf("expected reviewerGroups in JSON output: %s", buf.String())
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

	// 4. create repo group in dry-run mode (create vs conflict)
	dryRunEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"create", "team-c", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on create repo dry-run: %v", err)
	}
	// Conflict in dry run: team-a already exists
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"create", "team-a", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on create repo dry-run conflict: %v", err)
	}
	dryRunEnabled = false

	// 5. create repo group (execute and JSON)
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"create", "team-c", "--repo", "PRJ/repo1", "--description", "Desc C"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on create repo: %v", err)
	}
	if !strings.Contains(buf.String(), "Created reviewer group") {
		t.Fatalf("expected Created reviewer group in create output: %s", buf.String())
	}
	jsonEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"create", "team-c", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on create repo JSON: %v", err)
	}
	jsonEnabled = false

	// 6. create project group in dry-run mode (create vs conflict)
	dryRunEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"create", "team-d", "--project", "PRJ"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on create project dry-run: %v", err)
	}
	// Conflict in dry run: team-b already exists
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"create", "team-b", "--project", "PRJ"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on create project dry-run conflict: %v", err)
	}
	dryRunEnabled = false

	// 7. create project group (execute and JSON)
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"create", "team-d", "--project", "PRJ", "--description", "Desc D"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on create project: %v", err)
	}
	if !strings.Contains(buf.String(), "Created reviewer group") {
		t.Fatalf("expected Created reviewer group in create output: %s", buf.String())
	}
	jsonEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"create", "team-d", "--project", "PRJ"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on create project JSON: %v", err)
	}
	jsonEnabled = false

	// 8. update repo group (dry-run: update, no-op, blocked)
	dryRunEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"update", "201", "--name", "team-a-new", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on update repo dry-run: %v", err)
	}
	// No-op preview (matching name and description)
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"update", "201", "--name", "team-a", "--description", "Team A", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on update repo dry-run no-op: %v", err)
	}
	// Blocked preview (group 999 not found)
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"update", "999", "--name", "team-x", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on update repo dry-run blocked: %v", err)
	}
	dryRunEnabled = false

	// 9. update repo group (real and JSON)
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"update", "201", "--name", "team-a-updated", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on update repo: %v", err)
	}
	if !strings.Contains(buf.String(), "Updated reviewer group") {
		t.Fatalf("expected Updated reviewer group in update output: %s", buf.String())
	}
	jsonEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"update", "201", "--name", "team-a-updated", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on update repo JSON: %v", err)
	}
	jsonEnabled = false

	// 10. update project group (dry-run: update, no-op, blocked)
	dryRunEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"update", "202", "--name", "team-b-new", "--project", "PRJ"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on update project dry-run: %v", err)
	}
	// No-op preview
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"update", "202", "--name", "team-b", "--description", "Team B", "--project", "PRJ"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on update project dry-run no-op: %v", err)
	}
	// Blocked preview
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"update", "999", "--name", "team-x", "--project", "PRJ"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on update project dry-run blocked: %v", err)
	}
	dryRunEnabled = false

	// 11. update project group (real and JSON)
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"update", "202", "--name", "team-b-updated", "--project", "PRJ"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on update project: %v", err)
	}
	if !strings.Contains(buf.String(), "Updated reviewer group") {
		t.Fatalf("expected Updated reviewer group in update output: %s", buf.String())
	}
	jsonEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"update", "202", "--name", "team-b-updated", "--project", "PRJ"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on update project JSON: %v", err)
	}
	jsonEnabled = false

	// 12. users on repo group (human, JSON, and empty)
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
	jsonEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"users", "201", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on users JSON: %v", err)
	}
	jsonEnabled = false

	// Empty users list
	emptyUserList = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"users", "201", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on empty users: %v", err)
	}
	if !strings.Contains(buf.String(), "No users found") {
		t.Fatalf("expected No users found, got: %s", buf.String())
	}
	emptyUserList = false

	// 13. users on project group is not supported -> returns error
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"users", "202", "--project", "PRJ"})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error on project users (only repo supported)")
	}

	// 14. delete repo group in dry-run mode (found vs not-found)
	dryRunEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"delete", "201", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on delete dry-run: %v", err)
	}
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"delete", "999", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on delete dry-run not-found: %v", err)
	}
	dryRunEnabled = false

	// 15. delete repo group (real and JSON)
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
	jsonEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"delete", "201", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on delete repo JSON: %v", err)
	}
	jsonEnabled = false

	// 16. delete project group in dry-run mode (found vs not-found)
	dryRunEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"delete", "202", "--project", "PRJ"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on delete project dry-run: %v", err)
	}
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"delete", "999", "--project", "PRJ"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on delete project dry-run not-found: %v", err)
	}
	dryRunEnabled = false

	// 17. delete project group (real and JSON)
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
	jsonEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"delete", "202", "--project", "PRJ"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on delete project JSON: %v", err)
	}
	jsonEnabled = false

	// 18. Empty reviewer groups list
	emptyRepoList = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on empty repo list: %v", err)
	}
	if !strings.Contains(buf.String(), "No reviewer groups found") {
		t.Fatalf("expected No reviewer groups found, got: %s", buf.String())
	}
	emptyRepoList = false

	// 19. Mutual exclusivity errors (--project and --repo together)
	commandsWithFlags := [][]string{
		{"list", "--project", "PRJ", "--repo", "PRJ/repo1"},
		{"create", "group-x", "--project", "PRJ", "--repo", "PRJ/repo1"},
		{"update", "201", "--project", "PRJ", "--repo", "PRJ/repo1"},
		{"delete", "201", "--project", "PRJ", "--repo", "PRJ/repo1"},
	}
	for _, args := range commandsWithFlags {
		cmd = New(deps)
		buf.Reset()
		cmd.SetOut(buf)
		cmd.SetErr(buf)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Fatalf("expected mutual exclusivity error for args: %v", args)
		}
	}

	// 20. Missing project key validation error
	cfgNoProject := config.AppConfig{BitbucketURL: server.URL}
	depsNoProject := deps
	depsNoProject.LoadConfig = func() (config.AppConfig, error) { return cfgNoProject, nil }
	depsNoProject.LoadConfigAndClient = func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
		client, err := openapi.NewClientWithResponsesFromConfig(cfgNoProject)
		return cfgNoProject, client, err
	}
	cmdNoProjectArgs := [][]string{
		{"list"},
		{"create", "group-x"},
		{"update", "201"},
		{"delete", "201"},
	}
	for _, args := range cmdNoProjectArgs {
		cmd = New(depsNoProject)
		buf.Reset()
		cmd.SetOut(buf)
		cmd.SetErr(buf)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Fatalf("expected missing project error for args: %v", args)
		}
	}
}

type mockReviewerGroupPermChecker struct {
	repoErr    error
	projectErr error
}

func (m *mockReviewerGroupPermChecker) CheckRepoPermission(ctx context.Context, projectKey, repoSlug string, permission openapigenerated.GetRepositories1ParamsPermission) error {
	return m.repoErr
}

func (m *mockReviewerGroupPermChecker) CheckProjectAdmin(ctx context.Context, projectKey string) error {
	return m.projectErr
}

func TestReviewerGroupDefaults(t *testing.T) {
	t.Setenv("BITBUCKET_URL", "http://localhost:7990")
	var deps Dependencies
	d := deps.withDefaults()

	if d.JSONEnabled == nil || d.JSONEnabled() {
		t.Fatal("expected JSONEnabled to default to false")
	}
	if d.DryRunEnabled == nil || d.DryRunEnabled() {
		t.Fatal("expected DryRunEnabled to default to false")
	}
	if d.WriteJSON == nil {
		t.Fatal("expected WriteJSON to default to non-nil")
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

func TestReviewerGroupPermissionRejections(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":201,"name":"group1"}]`))
	}))
	t.Cleanup(server.Close)

	cfg := config.AppConfig{BitbucketURL: server.URL, ProjectKey: "PRJ"}
	deps := Dependencies{
		DryRunEnabled: func() bool { return true },
		LoadConfig:    func() (config.AppConfig, error) { return cfg, nil },
		LoadConfigAndClient: func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
			client, err := openapi.NewClientWithResponsesFromConfig(cfg)
			return cfg, client, err
		},
		PermissionChecker: func(c *openapigenerated.ClientWithResponses) PermissionChecker {
			return &mockReviewerGroupPermChecker{repoErr: http.ErrAbortHandler, projectErr: http.ErrAbortHandler}
		},
	}

	// Repo create dry-run permission rejection
	cmd := New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"create", "group-new", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected permission error on repo create dry-run")
	}

	// Repo update dry-run permission rejection
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"update", "201", "--name", "group-renamed", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected permission error on repo update dry-run")
	}

	// Repo delete dry-run permission rejection
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"delete", "201", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected permission error on repo delete dry-run")
	}

	// Project create dry-run permission rejection
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"create", "group-new", "--project", "PRJ"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected permission error on project create dry-run")
	}

	// Project update dry-run permission rejection
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"update", "201", "--name", "group-renamed", "--project", "PRJ"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected permission error on project update dry-run")
	}

	// Project delete dry-run permission rejection
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"delete", "201", "--project", "PRJ"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected permission error on project delete dry-run")
	}
}
