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

func TestRepoMiscCommands(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/repo1/labels":
			_, _ = w.Write([]byte(`{"values":[{"name":"label1"}]}`))

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/repo1/labels":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"name":"label2"}`))

		case r.Method == http.MethodDelete && path == "/rest/api/latest/projects/PRJ/repos/repo1/labels/label1":
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/repo1/watch":
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodDelete && path == "/rest/api/latest/projects/PRJ/repos/repo1/watch":
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodGet && strings.Contains(path, "/projects/PRJ/repos/repo1/tasks"):
			_, _ = w.Write([]byte(`{"values":[{"id":101,"description":"Review task"}]}`))

		case r.Method == http.MethodPost && strings.Contains(path, "/projects/PRJ/repos/repo1/tasks"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":102,"description":"New task"}`))

		case r.Method == http.MethodPut && strings.Contains(path, "/projects/PRJ/repos/repo1/tasks/101"):
			_, _ = w.Write([]byte(`{"id":101,"description":"Updated task"}`))

		case r.Method == http.MethodDelete && strings.Contains(path, "/projects/PRJ/repos/repo1/tasks/101"):
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodGet && strings.Contains(path, "/projects/PRJ/repos/repo1/ssh"):
			_, _ = w.Write([]byte(`{"values":[{"key":{"id":1,"text":"ssh-rsa AAAA..."},"permission":"REPO_READ"}]}`))

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

	// 1. Label list, add, remove (dry-run, real, JSON)
	cmd := New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"label", "list", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on label list: %v", err)
	}
	if !strings.Contains(buf.String(), "label1") {
		t.Fatalf("expected label1 in output: %s", buf.String())
	}

	jsonEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"label", "list", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on label list JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "labels") {
		t.Fatalf("expected labels in JSON output: %s", buf.String())
	}
	jsonEnabled = false

	// Label add (dry-run & real)
	dryRunEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"label", "add", "label2", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on label add dry-run: %v", err)
	}
	dryRunEnabled = false

	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"label", "add", "label2", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on label add: %v", err)
	}
	if !strings.Contains(buf.String(), "Added label:") {
		t.Fatalf("expected Added label in output: %s", buf.String())
	}

	// Label remove (dry-run & real)
	dryRunEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"label", "remove", "label1", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on label remove dry-run: %v", err)
	}
	dryRunEnabled = false

	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"label", "remove", "label1", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on label remove: %v", err)
	}
	if !strings.Contains(buf.String(), "Removed label:") {
		t.Fatalf("expected Removed label in output: %s", buf.String())
	}

	// 2. Watch and Unwatch (dry-run & real)
	dryRunEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"watch", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on watch dry-run: %v", err)
	}

	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"unwatch", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on unwatch dry-run: %v", err)
	}
	dryRunEnabled = false

	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"watch", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on watch: %v", err)
	}
	if !strings.Contains(buf.String(), "Watching repository") {
		t.Fatalf("expected Watching repository in output: %s", buf.String())
	}

	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"unwatch", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on unwatch: %v", err)
	}
	if !strings.Contains(buf.String(), "Unwatched repository") {
		t.Fatalf("expected Unwatched repository in output: %s", buf.String())
	}

	// 3. Default-task list, add, update, delete (dry-run & real)
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"default-task", "list", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on default-task list: %v", err)
	}
	if !strings.Contains(buf.String(), "Review task") {
		t.Fatalf("expected Review task in output: %s", buf.String())
	}

	// Default-task add
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"default-task", "add", "New task", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on default-task add: %v", err)
	}
	if !strings.Contains(buf.String(), "Created default task") {
		t.Fatalf("expected Created default task in output: %s", buf.String())
	}

	// Default-task update
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"default-task", "update", "101", "--description", "Updated task", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on default-task update: %v", err)
	}
	if !strings.Contains(buf.String(), "Updated default task") {
		t.Fatalf("expected Updated default task in output: %s", buf.String())
	}

	// Default-task delete
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"default-task", "delete", "101", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on default-task delete: %v", err)
	}
	if !strings.Contains(buf.String(), "Deleted default task") {
		t.Fatalf("expected Deleted default task in output: %s", buf.String())
	}

	// 4. SSH-key list, add, remove (human & JSON)
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"ssh-key", "list", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on ssh-key list: %v", err)
	}
	if !strings.Contains(buf.String(), "REPO_READ") {
		t.Fatalf("expected REPO_READ in output: %s", buf.String())
	}

	jsonEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"ssh-key", "list", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on ssh-key list JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "REPO_READ") {
		t.Fatalf("expected REPO_READ in JSON output: %s", buf.String())
	}
}
