package repocmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
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
			_, _ = w.Write([]byte(`{"values":[{"key":{"id":1,"text":"ssh-rsa AAAA...","label":"repo-key"},"permission":"REPO_READ"}]}`))

		case r.Method == http.MethodPost && strings.Contains(path, "/projects/PRJ/repos/repo1/ssh"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"key":{"id":2,"text":"ssh-rsa AAAA...","label":"new-key"},"permission":"REPO_WRITE"}`))

		case r.Method == http.MethodDelete && strings.Contains(path, "/projects/PRJ/repos/repo1/ssh/"):
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodGet && strings.Contains(path, "/projects/PRJ/ssh"):
			_, _ = w.Write([]byte(`{"values":[{"key":{"id":10,"text":"ssh-rsa AAAA...","label":"proj-key"},"permission":"PROJECT_READ"}]}`))

		case r.Method == http.MethodPost && strings.Contains(path, "/projects/PRJ/ssh"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"key":{"id":20,"text":"ssh-rsa AAAA...","label":"new-proj-key"},"permission":"PROJECT_WRITE"}`))

		case r.Method == http.MethodDelete && strings.Contains(path, "/projects/PRJ/ssh/"):
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

func TestReadPublicKeyAndScope(t *testing.T) {
	// Direct text
	textKey := "ssh-rsa AAAA..."
	readKey, err := readPublicKey(textKey)
	if err != nil || readKey != textKey {
		t.Fatalf("unexpected readPublicKey text: %v", err)
	}

	// File text
	tmpDir := t.TempDir()
	keyFile := filepath.Join(tmpDir, "id_rsa.pub")
	if err := os.WriteFile(keyFile, []byte("ssh-ed25519 BBBB...\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	readKey, err = readPublicKey(keyFile)
	if err != nil || readKey != "ssh-ed25519 BBBB..." {
		t.Fatalf("unexpected readPublicKey file: %s, %v", readKey, err)
	}

	// Scope resolution
	proj, repo, isProj, err := resolveRepoSshKeyScope("PRJ", "")
	if err != nil || proj != "PRJ" || repo != "" || !isProj {
		t.Fatalf("unexpected resolveRepoSshKeyScope project: %s, %s, %v, %v", proj, repo, isProj, err)
	}

	proj, repo, isProj, err = resolveRepoSshKeyScope("", "PRJ/repo1")
	if err != nil || proj != "PRJ" || repo != "repo1" || isProj {
		t.Fatalf("unexpected resolveRepoSshKeyScope repo: %s, %s, %v, %v", proj, repo, isProj, err)
	}

	_, _, _, err = resolveRepoSshKeyScope("PRJ", "PRJ/repo1")
	if err == nil {
		t.Fatal("expected error when both project and repo are specified")
	}

	_, _, _, err = resolveRepoSshKeyScope("", "")
	if err == nil {
		t.Fatal("expected error when neither project nor repo is specified")
	}

	_, _, _, err = resolveRepoSshKeyScope("", "invalid-format")
	if err == nil {
		t.Fatal("expected error for invalid repo format")
	}
}

func TestRepoSshKeyAddAndRemove(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		case r.Method == http.MethodGet && strings.Contains(path, "/projects/PRJ/repos/repo1/ssh"):
			_, _ = w.Write([]byte(`{"values":[{"key":{"id":1,"text":"ssh-rsa AAAA...","label":"repo-key"},"permission":"REPO_READ"}]}`))

		case r.Method == http.MethodPost && strings.Contains(path, "/projects/PRJ/repos/repo1/ssh"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"key":{"id":2,"text":"ssh-rsa AAAA...","label":"new-repo-key"},"permission":"REPO_WRITE"}`))

		case r.Method == http.MethodDelete && strings.Contains(path, "/projects/PRJ/repos/repo1/ssh/2"):
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodGet && strings.Contains(path, "/projects/PRJ/ssh"):
			_, _ = w.Write([]byte(`{"values":[{"key":{"id":10,"text":"ssh-rsa AAAA...","label":"proj-key"},"permission":"PROJECT_READ"}]}`))

		case r.Method == http.MethodPost && strings.Contains(path, "/projects/PRJ/ssh"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"key":{"id":20,"text":"ssh-rsa AAAA...","label":"new-proj-key"},"permission":"PROJECT_WRITE"}`))

		case r.Method == http.MethodDelete && strings.Contains(path, "/projects/PRJ/ssh/20"):
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
	deps := Dependencies{
		JSONEnabled: func() bool { return jsonEnabled },
		LoadConfig:  func() (config.AppConfig, error) { return cfg, nil },
		LoadConfigAndClient: func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
			client, err := openapi.NewClientWithResponsesFromConfig(cfg)
			return cfg, client, err
		},
	}

	// 1. Repo key add (human & JSON)
	cmd := New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"ssh-key", "add", "ssh-rsa AAAA...", "--repo", "PRJ/repo1", "--label", "new-repo-key", "--read-write"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on repo ssh-key add: %v", err)
	}
	if !strings.Contains(buf.String(), "added successfully") {
		t.Fatalf("expected added successfully in output: %s", buf.String())
	}

	jsonEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"ssh-key", "add", "ssh-rsa AAAA...", "--repo", "PRJ/repo1", "--label", "new-repo-key", "--permission", "read-write"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on repo ssh-key add JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "key") {
		t.Fatalf("expected key in JSON output: %s", buf.String())
	}
	jsonEnabled = false

	// 2. Repo key remove (human & JSON)
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"ssh-key", "remove", "2", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on repo ssh-key remove: %v", err)
	}
	if !strings.Contains(buf.String(), "removed successfully") {
		t.Fatalf("expected removed successfully in output: %s", buf.String())
	}

	// 3. Project key add & list & remove
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"ssh-key", "list", "--project", "PRJ"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on project ssh-key list: %v", err)
	}
	if !strings.Contains(buf.String(), "proj-key") {
		t.Fatalf("expected proj-key in output: %s", buf.String())
	}

	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"ssh-key", "add", "ssh-rsa AAAA...", "--project", "PRJ", "--label", "new-proj-key", "--read-only"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on project ssh-key add: %v", err)
	}
	if !strings.Contains(buf.String(), "added successfully") {
		t.Fatalf("expected added successfully in output: %s", buf.String())
	}

	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"ssh-key", "remove", "20", "--project", "PRJ"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on project ssh-key remove: %v", err)
	}
	if !strings.Contains(buf.String(), "removed successfully") {
		t.Fatalf("expected removed successfully in output: %s", buf.String())
	}
}

func TestRepoLabelsWatchAndTasks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		// Labels
		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/repo1/labels":
			_, _ = w.Write([]byte(`{"values":[{"name":"backend"},{"name":"go"}]}`))
		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/repo1/labels":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && strings.HasPrefix(path, "/rest/api/latest/projects/PRJ/repos/repo1/labels/"):
			w.WriteHeader(http.StatusNoContent)

		// Watch / Unwatch
		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/repo1/watch":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && path == "/rest/api/latest/projects/PRJ/repos/repo1/watch":
			w.WriteHeader(http.StatusNoContent)

		// Tasks
		case r.Method == http.MethodGet && path == "/rest/default-tasks/latest/projects/PRJ/repos/repo1/tasks":
			_, _ = w.Write([]byte(`{"values":[{"id":1001,"description":"Check tests"}]}`))
		case r.Method == http.MethodPost && path == "/rest/default-tasks/latest/projects/PRJ/repos/repo1/tasks":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":1002,"description":"New task"}`))
		case r.Method == http.MethodDelete && path == "/rest/default-tasks/latest/projects/PRJ/repos/repo1/tasks/1001":
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

	// 1. Labels list (human & JSON)
	cmd := New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"label", "list", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on label list: %v", err)
	}
	if !strings.Contains(buf.String(), "backend") {
		t.Fatalf("expected backend in label list output: %s", buf.String())
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

	// 2. Label add & remove (dry-run & real)
	dryRunEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"label", "add", "new-label", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on label add dry-run: %v", err)
	}
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"label", "remove", "old-label", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on label remove dry-run: %v", err)
	}
	dryRunEnabled = false

	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"label", "add", "new-label", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on label add: %v", err)
	}
	if !strings.Contains(buf.String(), "Added label:") {
		t.Fatalf("expected Added label: in output: %s", buf.String())
	}

	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"label", "remove", "old-label", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on label remove: %v", err)
	}
	if !strings.Contains(buf.String(), "Removed label:") {
		t.Fatalf("expected Removed label: in output: %s", buf.String())
	}

	// 3. Watch & Unwatch (dry-run & real)
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

	// 4. Default tasks (list, create, delete)
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"default-task", "list", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on default-task list: %v", err)
	}
	if !strings.Contains(buf.String(), "Check tests") {
		t.Fatalf("expected Check tests in default-task list output: %s", buf.String())
	}

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

	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"default-task", "delete", "1001", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on default-task delete: %v", err)
	}
	if !strings.Contains(buf.String(), "Deleted default task:") {
		t.Fatalf("expected Deleted default task: in output: %s", buf.String())
	}
}

// TestFinishArchiveFile covers the close that `bb repo archive` reports on.
//
// The failure branch is the point: io.Copy succeeding does not mean the bytes
// reached the disk, and this command used to print success over a truncated
// archive. Closing an already-closed file is the portable way to make Close
// fail.
func TestFinishArchiveFile(t *testing.T) {
	t.Run("nil file is not an error", func(t *testing.T) {
		if err := finishArchiveFile(nil, "unused"); err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
	})

	t.Run("closes an open file", func(t *testing.T) {
		file, err := os.Create(filepath.Join(t.TempDir(), "archive.zip"))
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if err := finishArchiveFile(file, "archive.zip"); err != nil {
			t.Fatalf("expected a clean close, got %v", err)
		}
	})

	t.Run("reports a failed close as an error naming the target", func(t *testing.T) {
		file, err := os.Create(filepath.Join(t.TempDir(), "archive.zip"))
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if err := file.Close(); err != nil {
			t.Fatalf("first close: %v", err)
		}

		err = finishArchiveFile(file, "/tmp/archive.zip")
		if err == nil {
			t.Fatal("expected a failed close to be reported, got nil")
		}
		if !apperrors.IsKind(err, apperrors.KindInternal) {
			t.Fatalf("expected KindInternal, got %v", err)
		}
		if !strings.Contains(err.Error(), "/tmp/archive.zip") {
			t.Fatalf("message does not name the target file: %v", err)
		}
	})
}
