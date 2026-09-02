package projectcmd

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

type mockPermChecker struct{}

func (m *mockPermChecker) CheckProjectCreate(ctx context.Context) error {
	return nil
}
func (m *mockPermChecker) CheckProjectAdmin(ctx context.Context, projectKey string) error {
	return nil
}
func (m *mockPermChecker) InspectProjectPermissions(ctx context.Context, projectKey string) (map[string]bool, error) {
	return map[string]bool{"PROJECT_READ": true, "PROJECT_WRITE": true, "PROJECT_ADMIN": true}, nil
}

func newMockProjectServer(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		case r.Method == http.MethodGet && path == "/rest/api/latest/projects":
			limit := r.URL.Query().Get("limit")
			start := r.URL.Query().Get("start")
			if limit == "1" && start == "2" {
				_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"key":"PRJ-paginated","name":"Project Paginated"}]}`))
			} else {
				_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"key":"PRJ","name":"Project"}]}`))
			}

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ":
			_, _ = w.Write([]byte(`{"key":"PRJ","name":"Project","description":"Project Description"}`))

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"key":"PRJ2","name":"Project 2"}`))

		case r.Method == http.MethodPut && path == "/rest/api/latest/projects/PRJ":
			_, _ = w.Write([]byte(`{"key":"PRJ","name":"Project Updated"}`))

		case r.Method == http.MethodDelete && path == "/rest/api/latest/projects/PRJ":
			w.WriteHeader(http.StatusNoContent)

		// Permissions
		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/permissions/users":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"user":{"name":"alice","displayName":"Alice A"},"permission":"PROJECT_READ"}]}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/permissions/groups":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"group":{"name":"admins"},"permission":"PROJECT_ADMIN"}]}`))

		case r.Method == http.MethodPut && (path == "/rest/api/latest/projects/PRJ/permissions/users" || path == "/rest/api/latest/projects/PRJ/permissions/groups"):
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodDelete && (path == "/rest/api/latest/projects/PRJ/permissions/users" || path == "/rest/api/latest/projects/PRJ/permissions/groups"):
			w.WriteHeader(http.StatusNoContent)

		// Webhooks
		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/webhooks":
			_, _ = w.Write([]byte(`[{"id":123,"name":"wh","url":"http://url","active":true,"events":["repo:refs_changed"]}]`))

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/webhooks":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":123,"name":"wh","url":"http://url","active":true}`))

		case r.Method == http.MethodPut && path == "/rest/api/latest/projects/PRJ/webhooks/123":
			_, _ = w.Write([]byte(`{"id":123,"name":"wh-new","url":"http://url","active":true}`))

		case r.Method == http.MethodDelete && path == "/rest/api/latest/projects/PRJ/webhooks/123":
			w.WriteHeader(http.StatusNoContent)

		// Branch restrictions
		case r.Method == http.MethodGet && path == "/rest/branch-permissions/latest/projects/PRJ/restrictions":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":456,"type":"read-only","matcher":{"id":"refs/heads/master","type":{"id":"BRANCH"}}}]}`))

		case r.Method == http.MethodGet && path == "/rest/branch-permissions/latest/projects/PRJ/restrictions/456":
			_, _ = w.Write([]byte(`{"id":456,"type":"read-only","matcher":{"id":"refs/heads/master","type":{"id":"BRANCH"}}}`))

		case r.Method == http.MethodPost && path == "/rest/branch-permissions/latest/projects/PRJ/restrictions":
			_, _ = w.Write([]byte(`[{"id":456,"type":"read-only","matcher":{"id":"refs/heads/master","type":{"id":"BRANCH"}}}]`))

		case r.Method == http.MethodDelete && path == "/rest/branch-permissions/latest/projects/PRJ/restrictions/456":
			w.WriteHeader(http.StatusNoContent)

		// Default tasks
		case r.Method == http.MethodGet && path == "/rest/default-tasks/latest/projects/PRJ/tasks":
			_, _ = w.Write([]byte(`{"values":[{"id":789,"description":"task1"}]}`))

		case r.Method == http.MethodPost && path == "/rest/default-tasks/latest/projects/PRJ/tasks":
			_, _ = w.Write([]byte(`{"id":789,"description":"task1"}`))

		case r.Method == http.MethodDelete && path == "/rest/default-tasks/latest/projects/PRJ/tasks/789":
			w.WriteHeader(http.StatusNoContent)

		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	return server
}

func newTestDependencies(t *testing.T, serverURL string, jsonMode bool, dryRun bool) Dependencies {
	cfg := config.AppConfig{
		BitbucketURL: serverURL,
		ProjectKey:   "PRJ",
	}

	return Dependencies{
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
		PermissionChecker: func(*openapigenerated.ClientWithResponses) PermissionChecker {
			return &mockPermChecker{}
		},
	}
}

func TestProjectCRUD(t *testing.T) {
	server := newMockProjectServer(t)
	deps := newTestDependencies(t, server.URL, false, false)

	// list
	cmd := New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on list: %v", err)
	}
	if !strings.Contains(buf.String(), "PRJ") {
		t.Fatalf("expected PRJ in list output: %s", buf.String())
	}

	// get
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"get", "PRJ"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on get: %v", err)
	}
	if !strings.Contains(buf.String(), "Key: PRJ") {
		t.Fatalf("expected Key: PRJ in get output: %s", buf.String())
	}

	// create
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"create", "PRJ2", "--name", "Project 2"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on create: %v", err)
	}
	if !strings.Contains(buf.String(), "Created project") {
		t.Fatalf("expected Created project in create output: %s", buf.String())
	}

	// update
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"update", "PRJ", "--name", "Project Updated"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on update: %v", err)
	}
	if !strings.Contains(buf.String(), "Updated project") {
		t.Fatalf("expected Updated project in update output: %s", buf.String())
	}

	// delete
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"delete", "PRJ"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on delete: %v", err)
	}
	if !strings.Contains(buf.String(), "Deleted project") {
		t.Fatalf("expected Deleted project in delete output: %s", buf.String())
	}
}

func TestProjectPermissions(t *testing.T) {
	server := newMockProjectServer(t)
	deps := newTestDependencies(t, server.URL, false, false)

	// show
	cmd := New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"permissions", "show", "PRJ"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on permissions show: %v", err)
	}
	if !strings.Contains(buf.String(), "PROJECT_ADMIN: true") {
		t.Fatalf("expected PROJECT_ADMIN: true in show output: %s", buf.String())
	}

	// list users
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"permissions", "users", "list", "PRJ"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on permissions users list: %v", err)
	}
	if !strings.Contains(buf.String(), "Alice A") {
		t.Fatalf("expected Alice A in users list output: %s", buf.String())
	}

	// grant user
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"permissions", "grant", "PRJ", "alice", "PROJECT_ADMIN"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on permissions grant: %v", err)
	}
	if !strings.Contains(buf.String(), "Granted") {
		t.Fatalf("expected Granted in grant output: %s", buf.String())
	}

	// revoke user
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"permissions", "revoke", "PRJ", "alice"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on permissions revoke: %v", err)
	}
	if !strings.Contains(buf.String(), "Revoked") {
		t.Fatalf("expected Revoked in revoke output: %s", buf.String())
	}
}

func TestProjectSubcommands(t *testing.T) {
	server := newMockProjectServer(t)
	deps := newTestDependencies(t, server.URL, false, false)

	// webhooks list
	cmd := New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"webhook", "list", "PRJ"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on webhook list: %v", err)
	}
	if !strings.Contains(buf.String(), "wh") {
		t.Fatalf("expected wh in webhook list output: %s", buf.String())
	}

	// branch-restriction list
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"branch-restriction", "list", "PRJ"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on branch-restriction list: %v", err)
	}
	if !strings.Contains(buf.String(), "read-only") {
		t.Fatalf("expected read-only in branch-restriction list output: %s", buf.String())
	}

	// default-task list
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"default-task", "list", "PRJ"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on default-task list: %v", err)
	}
	if !strings.Contains(buf.String(), "task1") {
		t.Fatalf("expected task1 in default-task list output: %s", buf.String())
	}
}
