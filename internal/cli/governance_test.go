package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

func TestReviewerCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/rest/default-reviewers/latest/projects/PRJ/conditions":
			_, _ = writer.Write([]byte(`[{"id":1}]`))
		case request.Method == http.MethodDelete && request.URL.Path == "/rest/default-reviewers/latest/projects/PRJ/condition/1":
			writer.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)

	// List
	command.SetArgs([]string{"--json", "reviewer", "condition", "list", "--project", "PRJ"})
	if err := command.Execute(); err != nil {
		t.Fatalf("list execute failed: %v", err)
	}
	if !strings.Contains(buffer.String(), `"conditions"`) {
		t.Fatalf("expected conditions in output, got: %s", buffer.String())
	}

	// Delete
	buffer.Reset()
	command.SetArgs([]string{"--json", "reviewer", "condition", "delete", "1", "--project", "PRJ"})
	if err := command.Execute(); err != nil {
		t.Fatalf("delete execute failed: %v", err)
	}
	if !strings.Contains(buffer.String(), `"status": "ok"`) {
		t.Fatalf("expected ok status in output, got: %s", buffer.String())
	}
}

func TestHookCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/PRJ/settings/hooks":
			_, _ = writer.Write([]byte(`{"values":[{"enabled":true,"details":{"key":"h1"}}]}`))
		case request.Method == http.MethodPut && request.URL.Path == "/rest/api/latest/projects/PRJ/settings/hooks/h1/enabled":
			_, _ = writer.Write([]byte(`{"enabled":true}`))
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/PRJ/settings/hooks/h1/settings":
			_, _ = writer.Write([]byte(`{"foo":"bar"}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)

	// List
	command.SetArgs([]string{"--json", "hook", "list", "--project", "PRJ"})
	if err := command.Execute(); err != nil {
		t.Fatalf("list execute failed: %v", err)
	}
	if !strings.Contains(buffer.String(), `"hooks"`) {
		t.Fatalf("expected hooks in output, got: %s", buffer.String())
	}

	// Enable
	buffer.Reset()
	command.SetArgs([]string{"--json", "hook", "enable", "h1", "--project", "PRJ"})
	if err := command.Execute(); err != nil {
		t.Fatalf("enable execute failed: %v", err)
	}
	if !strings.Contains(buffer.String(), `"hook"`) {
		t.Fatalf("expected hook in output, got: %s", buffer.String())
	}

	// Configure (get)
	buffer.Reset()
	command.SetArgs([]string{"hook", "configure", "h1", "--project", "PRJ"})
	if err := command.Execute(); err != nil {
		t.Fatalf("configure get execute failed: %v", err)
	}
	if !strings.Contains(buffer.String(), `"foo": "bar"`) {
		t.Fatalf("expected settings in output, got: %s", buffer.String())
	}
}

func TestProjectPermissionsCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/PRJ/permissions/users":
			_, _ = writer.Write([]byte(`{"values":[{"user":{"name":"u1"},"permission":"PROJECT_READ"}]}`))
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/PRJ/permissions/groups":
			_, _ = writer.Write([]byte(`{"values":[{"group":{"name":"g1"},"permission":"PROJECT_WRITE"}]}`))
		case request.Method == http.MethodPut && request.URL.Path == "/rest/api/latest/projects/PRJ/permissions/users":
			writer.WriteHeader(http.StatusNoContent)
		case request.Method == http.MethodPut && request.URL.Path == "/rest/api/latest/projects/PRJ/permissions/groups":
			writer.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)

	// List Users
	command.SetArgs([]string{"--json", "project", "permissions", "users", "list", "PRJ"})
	if err := command.Execute(); err != nil {
		t.Fatalf("list users failed: %v", err)
	}
	if !strings.Contains(buffer.String(), `"u1"`) {
		t.Fatalf("expected u1 in output, got: %s", buffer.String())
	}

	// List Groups
	buffer.Reset()
	command.SetArgs([]string{"--json", "project", "permissions", "groups", "list", "PRJ"})
	if err := command.Execute(); err != nil {
		t.Fatalf("list groups failed: %v", err)
	}
	if !strings.Contains(buffer.String(), `"g1"`) {
		t.Fatalf("expected g1 in output, got: %s", buffer.String())
	}

	// Grant User
	buffer.Reset()
	command.SetArgs([]string{"--json", "project", "permissions", "users", "grant", "PRJ", "u1", "PROJECT_ADMIN"})
	if err := command.Execute(); err != nil {
		t.Fatalf("grant user failed: %v", err)
	}
	if !strings.Contains(buffer.String(), `"status": "ok"`) {
		t.Fatalf("expected ok in output, got: %s", buffer.String())
	}

	// Grant Group
	buffer.Reset()
	command.SetArgs([]string{"--json", "project", "permissions", "groups", "grant", "PRJ", "g1", "PROJECT_ADMIN"})
	if err := command.Execute(); err != nil {
		t.Fatalf("grant group failed: %v", err)
	}
	if !strings.Contains(buffer.String(), `"status": "ok"`) {
		t.Fatalf("expected ok in output, got: %s", buffer.String())
	}
}

func TestReviewerCLIErrors(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusForbidden)
		_, _ = writer.Write([]byte(`{"errors":[{"message":"forbidden"}]}`))
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()
	command.SetArgs([]string{"reviewer", "condition", "list", "--project", "PRJ"})
	if err := command.Execute(); err == nil {
		t.Fatal("expected error for forbidden list")
	}
}

func TestHookCLIErrors(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()
	command.SetArgs([]string{"hook", "enable", "h1", "--project", "PRJ"})
	if err := command.Execute(); err == nil {
		t.Fatal("expected error for server error")
	}
}

func TestProjectPermissionsCLIErrors(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()
	command.SetArgs([]string{"project", "permissions", "users", "grant", "PRJ", "u1", "INVALID"})
	if err := command.Execute(); err == nil {
		t.Fatal("expected error for invalid permission")
	}
}

func TestRepoScopedGovernanceCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/permissions/groups":
			_, _ = writer.Write([]byte(`{"values":[{"group":{"name":"g1"},"permission":"REPO_READ"}]}`))
		case request.URL.Path == "/rest/default-reviewers/latest/projects/PRJ/repos/demo/conditions":
			_, _ = writer.Write([]byte(`[{"id":1}]`))
		case request.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/settings/hooks":
			_, _ = writer.Write([]byte(`{"values":[]}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)

	// Repo permissions group list
	command.SetArgs([]string{"--json", "repo", "settings", "security", "permissions", "groups", "list", "--repo", "PRJ/demo"})
	if err := command.Execute(); err != nil {
		t.Fatalf("repo perm list failed: %v", err)
	}
	if !strings.Contains(buffer.String(), `"g1"`) {
		t.Fatalf("expected g1 in output, got: %s", buffer.String())
	}

	// Repo reviewer condition list
	buffer.Reset()
	command.SetArgs([]string{"--json", "reviewer", "condition", "list", "--repo", "PRJ/demo"})
	if err := command.Execute(); err != nil {
		t.Fatalf("repo reviewer list failed: %v", err)
	}
	if !strings.Contains(buffer.String(), `"conditions"`) {
		t.Fatalf("expected conditions in output, got: %s", buffer.String())
	}

	// Repo hook list
	buffer.Reset()
	command.SetArgs([]string{"--json", "hook", "list", "--repo", "PRJ/demo"})
	if err := command.Execute(); err != nil {
		t.Fatalf("repo hook list failed: %v", err)
	}
	if !strings.Contains(buffer.String(), `"hooks"`) {
		t.Fatalf("expected hooks in output, got: %s", buffer.String())
	}
}

func TestRevokeAndStrategyCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodDelete && request.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/permissions/users":
			writer.WriteHeader(http.StatusNoContent)
		case request.Method == http.MethodDelete && request.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/permissions/groups":
			writer.WriteHeader(http.StatusNoContent)
		case request.Method == http.MethodPost && request.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/settings/pull-requests":
			_, _ = writer.Write([]byte(`{"mergeConfig":{"defaultStrategy":{"id":"merge-base"}}}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)

	// Repo permissions user revoke
	command.SetArgs([]string{"--json", "repo", "settings", "security", "permissions", "users", "revoke", "alice", "--repo", "PRJ/demo"})
	if err := command.Execute(); err != nil {
		t.Fatalf("repo user revoke failed: %v", err)
	}

	// Repo permissions group revoke
	buffer.Reset()
	command.SetArgs([]string{"--json", "repo", "settings", "security", "permissions", "groups", "revoke", "admins", "--repo", "PRJ/demo"})
	if err := command.Execute(); err != nil {
		t.Fatalf("repo group revoke failed: %v", err)
	}

	// Repo PR set-strategy
	buffer.Reset()
	command.SetArgs([]string{"--json", "repo", "settings", "pull-requests", "set-strategy", "merge-base", "--repo", "PRJ/demo"})
	if err := command.Execute(); err != nil {
		t.Fatalf("repo set-strategy failed: %v", err)
	}
	if !strings.Contains(buffer.String(), `"merge-base"`) {
		t.Fatalf("expected strategy in output, got: %s", buffer.String())
	}
}

func TestHookConfigureCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.Method == http.MethodPut && request.URL.Path == "/rest/api/latest/projects/PRJ/settings/hooks/h1/settings" {
			_, _ = writer.Write([]byte(`{"foo":"updated"}`))
			return
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)

	// Configure with JSON arg
	command.SetArgs([]string{"--json", "hook", "configure", "h1", `{"foo":"updated"}`, "--project", "PRJ"})
	if err := command.Execute(); err != nil {
		t.Fatalf("hook configure failed: %v", err)
	}
	if !strings.Contains(buffer.String(), `"updated"`) {
		t.Fatalf("expected updated settings in output, got: %s", buffer.String())
	}
}

func TestHookConfigureFileAndStdinCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.Method == http.MethodPut && request.URL.Path == "/rest/api/latest/projects/PRJ/settings/hooks/h1/settings" {
			_, _ = writer.Write([]byte(`{"status":"ok"}`))
			return
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()

	// Test --config-file
	tmpFile := filepath.Join(t.TempDir(), "hook.json")
	_ = os.WriteFile(tmpFile, []byte(`{"foo":"bar"}`), 0644)

	command.SetArgs([]string{"--json", "hook", "configure", "h1", "--config-file", tmpFile, "--project", "PRJ"})
	if err := command.Execute(); err != nil {
		t.Fatalf("hook configure file failed: %v", err)
	}

	// Test stdin
	command = NewRootCommand()
	stdinBuffer := bytes.NewBufferString(`{"foo":"bar"}`)
	command.SetIn(stdinBuffer)
	command.SetArgs([]string{"--json", "hook", "configure", "h1", "-", "--project", "PRJ"})
	if err := command.Execute(); err != nil {
		t.Fatalf("hook configure stdin failed: %v", err)
	}
}

func TestReviewerConditionCreateCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.Method == http.MethodPost && request.URL.Path == "/rest/default-reviewers/latest/projects/PRJ/condition" {
			writer.WriteHeader(http.StatusCreated)
			_, _ = writer.Write([]byte(`{"id":5}`))
			return
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)

	command.SetArgs([]string{"--json", "reviewer", "condition", "create", `{"requiredApprovals":1}`, "--project", "PRJ"})
	if err := command.Execute(); err != nil {
		t.Fatalf("reviewer condition create failed: %v", err)
	}
	if !strings.Contains(buffer.String(), `"id": 5`) {
		t.Fatalf("expected id 5 in output, got: %s", buffer.String())
	}
}

func TestProjectPermissionsRevokeCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.Method == http.MethodDelete && (request.URL.Path == "/rest/api/latest/projects/PRJ/permissions/users" || request.URL.Path == "/rest/api/latest/projects/PRJ/permissions/groups") {
			writer.WriteHeader(http.StatusNoContent)
			return
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)

	// Revoke User
	command.SetArgs([]string{"--json", "project", "permissions", "users", "revoke", "PRJ", "u1"})
	if err := command.Execute(); err != nil {
		t.Fatalf("revoke user failed: %v", err)
	}

	// Revoke Group
	buffer.Reset()
	command.SetArgs([]string{"--json", "project", "permissions", "groups", "revoke", "PRJ", "g1"})
	if err := command.Execute(); err != nil {
		t.Fatalf("revoke group failed: %v", err)
	}
}

func TestRepoSettingsPermissionsCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/permissions/users":
			_, _ = writer.Write([]byte(`{"values":[{"user":{"name":"u1"},"permission":"REPO_READ"}]}`))
		case request.Method == http.MethodPut && request.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/permissions/users":
			writer.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)

	// List
	command.SetArgs([]string{"--json", "repo", "settings", "security", "permissions", "users", "list", "--repo", "PRJ/demo"})
	if err := command.Execute(); err != nil {
		t.Fatalf("repo user list failed: %v", err)
	}

	// Grant
	buffer.Reset()
	command.SetArgs([]string{"--json", "repo", "settings", "security", "permissions", "users", "grant", "u1", "REPO_WRITE", "--repo", "PRJ/demo"})
	if err := command.Execute(); err != nil {
		t.Fatalf("repo user grant failed: %v", err)
	}
}

func TestRepoSettingsPullRequestsAdditionalCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.Method == http.MethodPost && request.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/settings/pull-requests" {
			_, _ = writer.Write([]byte(`{"status":"ok"}`))
			return
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()

	// Update approvers
	command.SetArgs([]string{"--json", "repo", "settings", "pull-requests", "update-approvers", "--count", "2", "--repo", "PRJ/demo"})
	if err := command.Execute(); err != nil {
		t.Fatalf("update-approvers failed: %v", err)
	}

	// Update all tasks
	command.SetArgs([]string{"--json", "repo", "settings", "pull-requests", "update", "--required-all-tasks-complete", "--repo", "PRJ/demo"})
	if err := command.Execute(); err != nil {
		t.Fatalf("update failed: %v", err)
	}
}

func TestHumanOutputGovernanceCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.URL.Path == "/rest/api/latest/projects/PRJ/permissions/users":
			_, _ = writer.Write([]byte(`{"values":[{"user":{"name":"u1","displayName":"User 1"},"permission":"PROJECT_ADMIN"}]}`))
		case request.URL.Path == "/rest/api/latest/projects/PRJ/settings/hooks":
			_, _ = writer.Write([]byte(`{"values":[{"enabled":true,"details":{"key":"h1","name":"Hook 1"}}]}`))
		case request.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/settings/pull-requests":
			_, _ = writer.Write([]byte(`{"requiredAllTasksComplete":true,"requiredApprovers":{"enabled":true,"count":2},"mergeConfig":{"strategies":[{"id":"merge-base","name":"Base","enabled":true}]}}`))
		default:
			_, _ = writer.Write([]byte(`[]`))
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)

	// Project perms
	command.SetArgs([]string{"project", "permissions", "users", "list", "PRJ"})
	_ = command.Execute()

	// Hooks
	buffer.Reset()
	command.SetArgs([]string{"hook", "list", "--project", "PRJ"})
	_ = command.Execute()

	// PR settings
	buffer.Reset()
	command.SetArgs([]string{"repo", "settings", "pull-requests", "get", "--repo", "PRJ/demo"})
	_ = command.Execute()
}

func TestProjectPermissionsRevokeErrorsCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()

	// Revoke User Fail
	command.SetArgs([]string{"project", "permissions", "users", "revoke", "PRJ", "u1"})
	if err := command.Execute(); err == nil {
		t.Fatal("expected error for missing project")
	}

	// Revoke Group Fail
	command.SetArgs([]string{"project", "permissions", "groups", "revoke", "PRJ", "g1"})
	if err := command.Execute(); err == nil {
		t.Fatal("expected error for missing project")
	}
}

func TestHookEnableDisableErrorsCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()

	command.SetArgs([]string{"hook", "enable", "h1", "--project", "PRJ"})
	if err := command.Execute(); err == nil {
		t.Fatal("expected error")
	}

	command.SetArgs([]string{"hook", "disable", "h1", "--project", "PRJ"})
	if err := command.Execute(); err == nil {
		t.Fatal("expected error")
	}
}

func TestReviewerConditionDeleteErrorsCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()

	command.SetArgs([]string{"reviewer", "condition", "delete", "1", "--project", "PRJ"})
	if err := command.Execute(); err == nil {
		t.Fatal("expected error")
	}
}

func TestRepoSettingsPullRequestsHumanCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()

	command.SetArgs([]string{"repo", "settings", "pull-requests", "update", "--required-all-tasks-complete", "--repo", "PRJ/demo"})
	_ = command.Execute()

	command.SetArgs([]string{"repo", "settings", "pull-requests", "update-approvers", "--count", "2", "--repo", "PRJ/demo"})
	_ = command.Execute()
}

func TestReviewerCLIAdditional(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/rest/default-reviewers/latest/projects/PRJ/conditions" {
			_, _ = writer.Write([]byte(`[]`))
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")

	command := NewRootCommand()

	// Test default project from env
	command.SetArgs([]string{"reviewer", "condition", "list"})
	_ = command.Execute()
}

func TestHookCLIAdditional(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/rest/api/latest/projects/PRJ/settings/hooks/h1/settings" {
			_, _ = writer.Write([]byte(`{"a":1}`))
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")

	command := NewRootCommand()

	// Configure with current settings
	command.SetArgs([]string{"hook", "configure", "h1"})
	_ = command.Execute()

	// Configure with invalid JSON
	command.SetArgs([]string{"hook", "configure", "h1", "{invalid}"})
	if err := command.Execute(); err == nil {
		t.Fatal("expected error for invalid json")
	}
}

func TestRepoScopedGovernanceCLIAdditional(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodDelete && request.URL.Path == "/rest/default-reviewers/latest/projects/PRJ/repos/demo/condition/1":
			writer.WriteHeader(http.StatusNoContent)
		case request.Method == http.MethodPut && request.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/settings/hooks/h1/enabled":
			_, _ = writer.Write([]byte(`{"enabled":true}`))
		case request.Method == http.MethodDelete && request.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/settings/hooks/h1/enabled":
			writer.WriteHeader(http.StatusNoContent)
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/settings/hooks/h1/settings":
			_, _ = writer.Write([]byte(`{"b":2}`))
		case request.Method == http.MethodPut && request.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/settings/hooks/h1/settings":
			_, _ = writer.Write([]byte(`{"b":2}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)

	// Reviewer condition delete repo
	command.SetArgs([]string{"--json", "reviewer", "condition", "delete", "1", "--repo", "PRJ/demo"})
	_ = command.Execute()

	// Hook enable repo
	command.SetArgs([]string{"--json", "hook", "enable", "h1", "--repo", "PRJ/demo"})
	_ = command.Execute()

	// Hook disable repo
	command.SetArgs([]string{"--json", "hook", "disable", "h1", "--repo", "PRJ/demo"})
	_ = command.Execute()

	// Hook configure repo (get)
	command.SetArgs([]string{"--json", "hook", "configure", "h1", "--repo", "PRJ/demo"})
	_ = command.Execute()

	// Hook configure repo (set)
	command.SetArgs([]string{"--json", "hook", "configure", "h1", `{"b":2}`, "--repo", "PRJ/demo"})
	_ = command.Execute()
}

func TestProjectCoreCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/PRJ":
			_, _ = writer.Write([]byte(`{"key":"PRJ","name":"Project"}`))
		case request.Method == http.MethodPut && request.URL.Path == "/rest/api/latest/projects/PRJ":
			_, _ = writer.Write([]byte(`{"key":"PRJ","name":"Updated"}`))
		case request.Method == http.MethodDelete && request.URL.Path == "/rest/api/latest/projects/PRJ":
			writer.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)

	// Get
	command.SetArgs([]string{"project", "get", "PRJ"})
	_ = command.Execute()

	// Update
	command.SetArgs([]string{"project", "update", "PRJ", "--name", "Updated"})
	_ = command.Execute()

	// Delete
	command.SetArgs([]string{"project", "delete", "PRJ"})
	_ = command.Execute()
}

func TestReviewerConditionCreateUpdateRepoCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/rest/default-reviewers/latest/projects/PRJ/repos/demo/condition":
			writer.WriteHeader(http.StatusCreated)
			_, _ = writer.Write([]byte(`{"id":6}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)

	command.SetArgs([]string{"--json", "reviewer", "condition", "create", `{"requiredApprovals":1}`, "--repo", "PRJ/demo"})
	if err := command.Execute(); err != nil {
		t.Fatalf("reviewer condition create repo failed: %v", err)
	}
	if !strings.Contains(buffer.String(), `"id": 6`) {
		t.Fatalf("expected id 6 in output, got: %s", buffer.String())
	}
}

func TestRepoSettingsWorkflowWebhooksCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/webhooks":
			_, _ = writer.Write([]byte(`{"values":[{"id":42,"name":"w1"}]}`))
		case request.Method == http.MethodPost && request.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/webhooks":
			_, _ = writer.Write([]byte(`{"id":42,"name":"w1"}`))
		case request.Method == http.MethodDelete && request.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/webhooks/42":
			writer.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)

	// List
	command.SetArgs([]string{"--json", "repo", "settings", "workflow", "webhooks", "list", "--repo", "PRJ/demo"})
	_ = command.Execute()

	// Create
	command.SetArgs([]string{"--json", "repo", "settings", "workflow", "webhooks", "create", "w1", "http://h", "--repo", "PRJ/demo"})
	_ = command.Execute()

	// Delete
	command.SetArgs([]string{"--json", "repo", "settings", "workflow", "webhooks", "delete", "42", "--repo", "PRJ/demo"})
	_ = command.Execute()
}

func TestRepoSettingsPullRequestsErrorsCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()

	command.SetArgs([]string{"repo", "settings", "pull-requests", "get", "--repo", "PRJ/demo"})
	if err := command.Execute(); err == nil {
		t.Fatal("expected error")
	}

	command.SetArgs([]string{"repo", "settings", "pull-requests", "update", "--repo", "PRJ/demo"})
	if err := command.Execute(); err == nil {
		t.Fatal("expected error")
	}
}

func TestHookConfigureBranchesCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/rest/api/latest/projects/PRJ/settings/hooks/h1/settings" {
			_, _ = writer.Write([]byte(`{"foo":"bar"}`))
			return
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")

	command := NewRootCommand()

	// No hook key
	command.SetArgs([]string{"hook", "configure"})
	if err := command.Execute(); err == nil {
		t.Fatal("expected error for no hook key")
	}

	// Resolve reference failure
	command.SetArgs([]string{"hook", "configure", "h1", "--repo", "invalid"})
	if err := command.Execute(); err == nil {
		t.Fatal("expected error for invalid repo")
	}
}

func TestReviewerConditionRepoBranchesCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.Method == http.MethodDelete && request.URL.Path == "/rest/default-reviewers/latest/projects/PRJ/repos/demo/condition/1" {
			writer.WriteHeader(http.StatusNoContent)
			return
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()

	// Delete repo
	command.SetArgs([]string{"reviewer", "condition", "delete", "1", "--repo", "PRJ/demo"})
	_ = command.Execute()

	// List repo human
	command.SetArgs([]string{"reviewer", "condition", "list", "--repo", "PRJ/demo"})
	_ = command.Execute()
}

func TestHookConfigureFileErrorCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "http://localhost")
	command := NewRootCommand()
	command.SetArgs([]string{"hook", "configure", "h1", "--config-file", "nonexistent.json", "--project", "PRJ"})
	if err := command.Execute(); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestHookEnableDisableHumanCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"enabled":true}`))
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()

	command.SetArgs([]string{"hook", "enable", "h1", "--project", "PRJ"})
	_ = command.Execute()

	command.SetArgs([]string{"hook", "disable", "h1", "--project", "PRJ"})
	_ = command.Execute()
}

func TestProjectPermissionsListPaginationCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	var userCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if userCount.Add(1) == 1 {
			_, _ = writer.Write([]byte(`{"isLastPage":false,"nextPageStart":1,"values":[{"user":{"name":"u1"},"permission":"PROJECT_READ"}]}`))
		} else {
			_, _ = writer.Write([]byte(`{"isLastPage":true,"values":[{"user":{"name":"u2"},"permission":"PROJECT_READ"}]}`))
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()
	command.SetArgs([]string{"project", "permissions", "users", "list", "PRJ", "--limit", "1"})
	_ = command.Execute()
}

func TestHookConfigureNoSettingsCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/rest/api/latest/projects/PRJ/settings/hooks/h1/settings" {
			_, _ = writer.Write([]byte(`{}`))
			return
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()
	command.SetArgs([]string{"hook", "configure", "h1", "--project", "PRJ"})
	_ = command.Execute()
}

func TestPRReviewerCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/30/participants/u1" {
			_, _ = writer.Write([]byte(`{"id":30}`))
			return
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()

	// Add reviewer
	command.SetArgs([]string{"pr", "review", "reviewer", "add", "30", "--user", "u1", "--repo", "PRJ/demo"})
	_ = command.Execute()

	// Remove reviewer
	command.SetArgs([]string{"pr", "review", "reviewer", "remove", "30", "--user", "u1", "--repo", "PRJ/demo"})
	_ = command.Execute()
}

func TestReviewerConditionCreateProjectCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.Method == http.MethodPost && request.URL.Path == "/rest/default-reviewers/latest/projects/PRJ/condition" {
			writer.WriteHeader(http.StatusCreated)
			_, _ = writer.Write([]byte(`{"id":7}`))
			return
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()
	command.SetArgs([]string{"--json", "reviewer", "condition", "create", `{"requiredApprovals":1}`, "--project", "PRJ"})
	_ = command.Execute()
}

func TestHookConfigureFileSuccessCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.Method == http.MethodPut && request.URL.Path == "/rest/api/latest/projects/PRJ/settings/hooks/h1/settings" {
			_, _ = writer.Write([]byte(`{"status":"ok"}`))
			return
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()
	tmpFile := filepath.Join(t.TempDir(), "ok.json")
	_ = os.WriteFile(tmpFile, []byte(`{"a":1}`), 0644)

	command.SetArgs([]string{"--json", "hook", "configure", "h1", "--config-file", tmpFile, "--project", "PRJ"})
	_ = command.Execute()
}

func TestProjectConfigErrorsCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "://invalid")

	commands := [][]string{
		{"project", "list"},
		{"project", "get", "PRJ"},
		{"project", "create", "PRJ", "--name", "N"},
		{"project", "update", "PRJ", "--name", "N"},
		{"project", "delete", "PRJ"},
		{"project", "permissions", "users", "list", "PRJ"},
		{"project", "permissions", "groups", "list", "PRJ"},
		{"project", "permissions", "users", "grant", "PRJ", "u", "p"},
		{"project", "permissions", "groups", "grant", "PRJ", "g", "p"},
		{"project", "permissions", "users", "revoke", "PRJ", "u"},
		{"project", "permissions", "groups", "revoke", "PRJ", "g"},
	}

	for _, args := range commands {
		cmd := NewRootCommand()
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Fatalf("expected error for command %v", args)
		}
	}
}

func TestProjectHumanCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/PRJ":
			_, _ = writer.Write([]byte(`{"key":"PRJ","name":"Project"}`))
		case request.Method == http.MethodPost && request.URL.Path == "/rest/api/latest/projects":
			writer.WriteHeader(http.StatusCreated)
			_, _ = writer.Write([]byte(`{"key":"PRJ3","name":"New"}`))
		default:
			_, _ = writer.Write([]byte(`{"values":[]}`))
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()

	command.SetArgs([]string{"project", "get", "PRJ"})
	_ = command.Execute()

	command.SetArgs([]string{"project", "create", "PRJ3", "--name", "New"})
	_ = command.Execute()

	command.SetArgs([]string{"project", "list"})
	_ = command.Execute()
}

func TestHookConfigureRepoGetCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/settings/hooks/h1/settings" {
			_, _ = writer.Write([]byte(`{"c":3}`))
			return
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()
	command.SetArgs([]string{"hook", "configure", "h1", "--repo", "PRJ/demo"})
	_ = command.Execute()
}

func TestReviewerConditionCreateRepoSuccessCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.Method == http.MethodPost && request.URL.Path == "/rest/default-reviewers/latest/projects/PRJ/repos/demo/condition" {
			writer.WriteHeader(http.StatusCreated)
			_, _ = writer.Write([]byte(`{"id":8}`))
			return
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()
	command.SetArgs([]string{"--json", "reviewer", "condition", "create", `{}`, "--repo", "PRJ/demo"})
	_ = command.Execute()
}

func TestProjectCoreCLIErrors(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()

	command.SetArgs([]string{"project", "create", "P", "--name", "N"})
	if err := command.Execute(); err == nil {
		t.Fatal("expected error")
	}

	command.SetArgs([]string{"project", "update", "P", "--name", "N"})
	if err := command.Execute(); err == nil {
		t.Fatal("expected error")
	}

	command.SetArgs([]string{"project", "delete", "P"})
	if err := command.Execute(); err == nil {
		t.Fatal("expected error")
	}
}

func TestReviewerConditionCreateInvalidJSONCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	command := NewRootCommand()
	command.SetArgs([]string{"reviewer", "condition", "create", "{invalid}"})
	if err := command.Execute(); err == nil {
		t.Fatal("expected error")
	}
}

func TestHookEnableDisableRepoErrorsCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()

	command.SetArgs([]string{"hook", "enable", "h1", "--repo", "PRJ/demo"})
	if err := command.Execute(); err == nil {
		t.Fatal("expected error")
	}

	command.SetArgs([]string{"hook", "disable", "h1", "--repo", "PRJ/demo"})
	if err := command.Execute(); err == nil {
		t.Fatal("expected error")
	}
}

func TestHookConfigureRepoErrorsCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()

	command.SetArgs([]string{"hook", "configure", "h1", "--repo", "PRJ/demo"})
	if err := command.Execute(); err == nil {
		t.Fatal("expected error")
	}

	command.SetArgs([]string{"hook", "configure", "h1", `{"a":1}`, "--repo", "PRJ/demo"})
	if err := command.Execute(); err == nil {
		t.Fatal("expected error")
	}
}

func TestHookConfigureFileInvalidJSONCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.NotFound(writer, request)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()
	tmpFile := filepath.Join(t.TempDir(), "bad.json")
	_ = os.WriteFile(tmpFile, []byte(`{invalid}`), 0644)

	command.SetArgs([]string{"hook", "configure", "h1", "--config-file", tmpFile, "--project", "PRJ"})
	if err := command.Execute(); err == nil {
		t.Fatal("expected error")
	}
}

func TestProjectListFilterCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"isLastPage":true,"values":[{"key":"PRJ","name":"Project"}]}`))
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()
	command.SetArgs([]string{"project", "list", "--name", "Project"})
	_ = command.Execute()
}

func TestProjectListEmptyCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"isLastPage":true,"values":[]}`))
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()
	command.SetArgs([]string{"project", "list"})
	_ = command.Execute()
}

func TestReviewerConditionListEmptyCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`[]`))
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()
	command.SetArgs([]string{"reviewer", "condition", "list", "--project", "PRJ"})
	_ = command.Execute()
}

func TestHookListEmptyCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"isLastPage":true,"values":[]}`))
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()
	command.SetArgs([]string{"hook", "list", "--project", "PRJ"})
	_ = command.Execute()
}

func TestRootHelpersCLI(t *testing.T) {
	// Exercise all safe helpers in root.go
	_ = safeString(nil)
	s := "test"
	_ = safeString(&s)

	_ = safeInt32(nil)
	i32 := int32(1)
	_ = safeInt32(&i32)

	_ = safeInt64(nil)
	i64 := int64(1)
	_ = safeInt64(&i64)

	_ = safeStringSlice(nil)
	ss := []string{"a"}
	_ = safeStringSlice(&ss)

	_ = safeUsers(nil)
	_ = safeUsers(&[]openapigenerated.RestApplicationUser{})

	_ = safeStringFromTagType(nil)
	_ = safeStringFromBuildState(nil)
	_ = safeStringFromInsightResult(nil)
}

func TestReviewerConditionListHumanNotEmptyCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`[{"id":1}]`))
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()
	command.SetArgs([]string{"reviewer", "condition", "list", "--project", "PRJ"})
	_ = command.Execute()
}

func TestHookConfigureStdinSuccessCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.Method == http.MethodPut && request.URL.Path == "/rest/api/latest/projects/PRJ/settings/hooks/h1/settings" {
			_, _ = writer.Write([]byte(`{"status":"ok"}`))
			return
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()
	stdinBuffer := bytes.NewBufferString(`{"foo":"bar"}`)
	command.SetIn(stdinBuffer)
	command.SetArgs([]string{"--json", "hook", "configure", "h1", "-", "--project", "PRJ"})
	_ = command.Execute()
}

func TestReviewerConditionUpdateCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.Method == http.MethodPut {
			_, _ = writer.Write([]byte(`{"id":1,"requiredApprovals":3}`))
			return
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()

	// Project update
	command.SetArgs([]string{"--json", "reviewer", "condition", "update", "1", `{"requiredApprovals":3}`, "--project", "PRJ"})
	_ = command.Execute()

	// Repo update
	command.SetArgs([]string{"--json", "reviewer", "condition", "update", "1", `{"requiredApprovals":3}`, "--repo", "PRJ/demo"})
	_ = command.Execute()
}

func TestPRCoreCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && strings.Contains(request.URL.Path, "pull-requests/30"):
			_, _ = writer.Write([]byte(`{"id":30,"title":"T","version":1,"fromRef":{"displayId":"f"},"toRef":{"displayId":"t"}}`))
		case request.Method == http.MethodGet && strings.Contains(request.URL.Path, "pull-requests"):
			_, _ = writer.Write([]byte(`{"values":[{"id":30}],"isLastPage":true}`))
		case request.Method == http.MethodPost && strings.Contains(request.URL.Path, "pull-requests/30/merge"):
			_, _ = writer.Write([]byte(`{"id":30,"state":"MERGED"}`))
		case request.Method == http.MethodPost && strings.Contains(request.URL.Path, "pull-requests"):
			writer.WriteHeader(http.StatusCreated)
			_, _ = writer.Write([]byte(`{"id":31}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	// PR get human
	command := NewRootCommand()
	command.SetArgs([]string{"pr", "get", "30", "--repo", "PRJ/demo"})
	_ = command.Execute()

	// PR get json
	command = NewRootCommand()
	command.SetArgs([]string{"--json", "pr", "get", "30", "--repo", "PRJ/demo"})
	_ = command.Execute()

	// PR list human
	command = NewRootCommand()
	command.SetArgs([]string{"pr", "list", "--repo", "PRJ/demo"})
	_ = command.Execute()

	// PR list json
	command = NewRootCommand()
	command.SetArgs([]string{"--json", "pr", "list", "--repo", "PRJ/demo"})
	_ = command.Execute()

	// PR create human
	command = NewRootCommand()
	command.SetArgs([]string{"pr", "create", "--from-ref", "f", "--to-ref", "t", "--title", "T", "--repo", "PRJ/demo"})
	_ = command.Execute()

	// PR create json
	command = NewRootCommand()
	command.SetArgs([]string{"--json", "pr", "create", "--from-ref", "f", "--to-ref", "t", "--title", "T", "--repo", "PRJ/demo"})
	_ = command.Execute()

	// PR merge human
	command = NewRootCommand()
	command.SetArgs([]string{"pr", "merge", "30", "--repo", "PRJ/demo"})
	_ = command.Execute()

	// PR merge json
	command = NewRootCommand()
	command.SetArgs([]string{"--json", "pr", "merge", "30", "--repo", "PRJ/demo"})
	_ = command.Execute()
}

func TestPRApproveCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/30/participants/admin" {
			_, _ = writer.Write([]byte(`{"id":30}`))
			return
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_USERNAME", "admin")

	command := NewRootCommand()

	command.SetArgs([]string{"pr", "approve", "30", "--repo", "PRJ/demo"})
	_ = command.Execute()

	command.SetArgs([]string{"pr", "unapprove", "30", "--repo", "PRJ/demo"})
	_ = command.Execute()
}

func TestReviewerConditionUpdateProjectFallbackCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.Method == http.MethodPut && request.URL.Path == "/rest/default-reviewers/latest/projects/PRJ/condition/1" {
			_, _ = writer.Write([]byte(`{"id":1}`))
			return
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")

	command := NewRootCommand()
	command.SetArgs([]string{"--json", "reviewer", "condition", "update", "1", `{"requiredApprovals":1}`})
	_ = command.Execute()
}

func TestHookConfigureProjectGetCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/rest/api/latest/projects/PRJ/settings/hooks/h1/settings" {
			_, _ = writer.Write([]byte(`{"d":4}`))
			return
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")

	command := NewRootCommand()
	command.SetArgs([]string{"hook", "configure", "h1"})
	_ = command.Execute()
}

func TestHookConfigureRepoArgCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.Method == http.MethodPut && strings.Contains(request.URL.Path, "settings") {
			_, _ = writer.Write([]byte(`{"e":5}`))
			return
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()
	command.SetArgs([]string{"--json", "hook", "configure", "h1", `{"e":5}`, "--repo", "PRJ/demo"})
	_ = command.Execute()
}

func TestHookListDisabledHumanCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"isLastPage":true,"values":[{"enabled":false,"details":{"key":"h2","name":"Hook 2"}}]}`))
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()
	command.SetArgs([]string{"hook", "list", "--project", "PRJ"})
	_ = command.Execute()
}

func TestPRMergeErrorCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusConflict)
		_, _ = writer.Write([]byte(`{"errors":[{"message":"conflict"}]}`))
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()
	command.SetArgs([]string{"pr", "merge", "30", "--repo", "PRJ/demo"})
	if err := command.Execute(); err == nil {
		t.Fatal("expected error")
	}
}

func TestHookConfigureEmptyFileCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "http://localhost")
	command := NewRootCommand()
	tmpFile := filepath.Join(t.TempDir(), "empty.json")
	_ = os.WriteFile(tmpFile, []byte(""), 0644)
	command.SetArgs([]string{"hook", "configure", "h1", "--config-file", tmpFile, "--project", "PRJ"})
	if err := command.Execute(); err == nil {
		t.Fatal("expected error")
	}
}

func TestPullRequestRepoResolveFallbackCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "http://localhost")
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	command := NewRootCommand()
	// Run a command that uses resolvePullRequestRepositoryReference
	command.SetArgs([]string{"pr", "list"})
	_ = command.Execute()
}

func TestReviewerConditionDeleteFallbackCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.Method == http.MethodDelete && strings.Contains(request.URL.Path, "condition/1") {
			writer.WriteHeader(http.StatusNoContent)
			return
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")

	command := NewRootCommand()
	command.SetArgs([]string{"--json", "reviewer", "condition", "delete", "1"})
	_ = command.Execute()
}

func TestRootHelpersAdditionalCLI(t *testing.T) {
	_, _ = normalizeAccessKeyIDs(nil)
	_, _ = normalizeAccessKeyIDs([]int{1, 2})
	_, _ = normalizeAccessKeyIDs([]int{-1})
}

func TestSafeHelpersNonNilCLI(t *testing.T) {
	st := openapigenerated.RestBuildStatusState("SUCCESS")
	_ = safeStringFromBuildState(&st)

	ir := openapigenerated.RestInsightReportResult("PASS")
	_ = safeStringFromInsightResult(&ir)

	tt := openapigenerated.RestTagType("LIGHTWEIGHT")
	_ = safeStringFromTagType(&tt)
}

func TestMoreCLIErrorPaths(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	// repo settings commands that need error paths hit
	command := NewRootCommand()
	command.SetArgs([]string{"repo", "settings", "workflow", "webhooks", "list", "--repo", "PRJ/demo"})
	_ = command.Execute()

	command = NewRootCommand()
	command.SetArgs([]string{"repo", "settings", "workflow", "webhooks", "create", "w1", "http://h", "--repo", "PRJ/demo"})
	_ = command.Execute()

	command = NewRootCommand()
	command.SetArgs([]string{"repo", "settings", "workflow", "webhooks", "delete", "42", "--repo", "PRJ/demo"})
	_ = command.Execute()

	command = NewRootCommand()
	command.SetArgs([]string{"repo", "settings", "pull-requests", "get", "--repo", "PRJ/demo"})
	_ = command.Execute()

	command = NewRootCommand()
	command.SetArgs([]string{"repo", "settings", "pull-requests", "update", "--repo", "PRJ/demo"})
	_ = command.Execute()

	command = NewRootCommand()
	command.SetArgs([]string{"repo", "settings", "pull-requests", "update-approvers", "--count", "1", "--repo", "PRJ/demo"})
	_ = command.Execute()

	command = NewRootCommand()
	command.SetArgs([]string{"repo", "settings", "pull-requests", "set-strategy", "s", "--repo", "PRJ/demo"})
	_ = command.Execute()

	command = NewRootCommand()
	command.SetArgs([]string{"repo", "settings", "pull-requests", "merge-checks", "list", "--repo", "PRJ/demo"})
	_ = command.Execute()

	// missing config for the same
	t.Setenv("BITBUCKET_URL", "")
	command = NewRootCommand()
	command.SetArgs([]string{"repo", "settings", "workflow", "webhooks", "list", "--repo", "PRJ/demo"})
	_ = command.Execute()

	command = NewRootCommand()
	command.SetArgs([]string{"repo", "settings", "workflow", "webhooks", "create", "w1", "http://h", "--repo", "PRJ/demo"})
	_ = command.Execute()

	command = NewRootCommand()
	command.SetArgs([]string{"repo", "settings", "workflow", "webhooks", "delete", "42", "--repo", "PRJ/demo"})
	_ = command.Execute()

	command = NewRootCommand()
	command.SetArgs([]string{"repo", "settings", "pull-requests", "get", "--repo", "PRJ/demo"})
	_ = command.Execute()

	command = NewRootCommand()
	command.SetArgs([]string{"repo", "settings", "pull-requests", "update", "--repo", "PRJ/demo"})
	_ = command.Execute()

	command = NewRootCommand()
	command.SetArgs([]string{"repo", "settings", "pull-requests", "update-approvers", "--count", "1", "--repo", "PRJ/demo"})
	_ = command.Execute()

	command = NewRootCommand()
	command.SetArgs([]string{"repo", "settings", "pull-requests", "set-strategy", "s", "--repo", "PRJ/demo"})
	_ = command.Execute()

	command = NewRootCommand()
	command.SetArgs([]string{"repo", "settings", "pull-requests", "merge-checks", "list", "--repo", "PRJ/demo"})
	_ = command.Execute()
}

func TestReviewerConditionUpdateFallbackCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.Method == http.MethodPut && request.URL.Path == "/rest/default-reviewers/latest/projects/PRJ/repos/demo/condition/1" {
			_, _ = writer.Write([]byte(`{"id":1}`))
			return
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()
	command.SetArgs([]string{"--json", "reviewer", "condition", "update", "1", `{"requiredApprovals":1}`, "--repo", "PRJ/demo"})
	_ = command.Execute()
}

func TestReviewerConditionUpdateInvalidJSONCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	command := NewRootCommand()
	command.SetArgs([]string{"reviewer", "condition", "update", "1", "{invalid}"})
	if err := command.Execute(); err == nil {
		t.Fatal("expected error")
	}
}

func TestHookCLIBranchesAdditional(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()

	// List repo error
	command.SetArgs([]string{"hook", "list", "--repo", "PRJ/demo"})
	_ = command.Execute()

	// List project missing
	t.Setenv("BITBUCKET_PROJECT_KEY", "")
	command.SetArgs([]string{"hook", "list"})
	_ = command.Execute()

	// Enable repo error
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	command.SetArgs([]string{"hook", "enable", "h1", "--repo", "PRJ/demo"})
	_ = command.Execute()

	// Enable missing project
	t.Setenv("BITBUCKET_PROJECT_KEY", "")
	command.SetArgs([]string{"hook", "enable", "h1"})
	_ = command.Execute()

	// Disable repo error
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	command.SetArgs([]string{"hook", "disable", "h1", "--repo", "PRJ/demo"})
	_ = command.Execute()

	// Disable missing project
	t.Setenv("BITBUCKET_PROJECT_KEY", "")
	command.SetArgs([]string{"hook", "disable", "h1"})
	_ = command.Execute()

	// Configure get missing project
	t.Setenv("BITBUCKET_PROJECT_KEY", "")
	command.SetArgs([]string{"hook", "configure", "h1"})
	_ = command.Execute()

	// Configure set missing project
	t.Setenv("BITBUCKET_PROJECT_KEY", "")
	command.SetArgs([]string{"hook", "configure", "h1", "{}"})
	_ = command.Execute()

	// Configure no args error
	command.SetArgs([]string{"hook", "configure"})
	_ = command.Execute()

	// Configure invalid repo format
	command.SetArgs([]string{"hook", "configure", "h1", "--repo", "invalid"})
	_ = command.Execute()
}

func TestReviewerCLIBranchesAdditional(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()

	// List repo error
	command.SetArgs([]string{"reviewer", "condition", "list", "--repo", "PRJ/demo"})
	_ = command.Execute()

	// List invalid repo format
	command.SetArgs([]string{"reviewer", "condition", "list", "--repo", "invalid"})
	_ = command.Execute()

	// Delete repo error
	command.SetArgs([]string{"reviewer", "condition", "delete", "1", "--repo", "PRJ/demo"})
	_ = command.Execute()

	// Create repo error
	command.SetArgs([]string{"reviewer", "condition", "create", "{}", "--repo", "PRJ/demo"})
	_ = command.Execute()

	// Update repo error
	command.SetArgs([]string{"reviewer", "condition", "update", "1", "{}", "--repo", "PRJ/demo"})
	_ = command.Execute()

	// Update invalid repo format
	command.SetArgs([]string{"reviewer", "condition", "update", "1", "{}", "--repo", "invalid"})
	_ = command.Execute()

	// Delete missing project
	t.Setenv("BITBUCKET_PROJECT_KEY", "")
	command.SetArgs([]string{"reviewer", "condition", "delete", "1"})
	_ = command.Execute()

	// Create missing project
	t.Setenv("BITBUCKET_PROJECT_KEY", "")
	command.SetArgs([]string{"reviewer", "condition", "create", "{}"})
	_ = command.Execute()

	// Update missing project
	t.Setenv("BITBUCKET_PROJECT_KEY", "")
	command.SetArgs([]string{"reviewer", "condition", "update", "1", "{}"})
	_ = command.Execute()
}

func TestCLIAllRemainingBranches(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "http://localhost")

	// This test tries to hit all the "err != nil" branches in loadConfigAndClient and resolveRepositoryReference
	// by forcing it to fail or parsing flags that cause errors.

	t.Setenv("BITBUCKET_URL", "://invalid") // Cause config to fail

	commands := [][]string{
		{"hook", "list", "--project", "P"},
		{"hook", "enable", "h1", "--project", "P"},
		{"hook", "disable", "h1", "--project", "P"},
		{"hook", "configure", "h1", "--project", "P"},
		{"hook", "configure", "h1", "{}", "--project", "P"},

		{"reviewer", "condition", "list", "--project", "P"},
		{"reviewer", "condition", "create", "{}", "--project", "P"},
		{"reviewer", "condition", "update", "1", "{}", "--project", "P"},
		{"reviewer", "condition", "delete", "1", "--project", "P"},

		{"repo", "settings", "pull-requests", "get", "--repo", "P/S"},
		{"repo", "settings", "pull-requests", "update", "--repo", "P/S"},
		{"repo", "settings", "pull-requests", "update-approvers", "--count", "1", "--repo", "P/S"},
		{"repo", "settings", "pull-requests", "set-strategy", "s", "--repo", "P/S"},
		{"repo", "settings", "pull-requests", "merge-checks", "list", "--repo", "P/S"},

		{"repo", "settings", "security", "permissions", "users", "list", "--repo", "P/S"},
		{"repo", "settings", "security", "permissions", "groups", "list", "--repo", "P/S"},
		{"repo", "settings", "security", "permissions", "users", "grant", "u", "p", "--repo", "P/S"},
		{"repo", "settings", "security", "permissions", "groups", "grant", "g", "p", "--repo", "P/S"},
		{"repo", "settings", "security", "permissions", "users", "revoke", "u", "--repo", "P/S"},
		{"repo", "settings", "security", "permissions", "groups", "revoke", "g", "--repo", "P/S"},
	}

	for _, args := range commands {
		cmd := NewRootCommand()
		cmd.SetArgs(args)
		_ = cmd.Execute()
	}

	t.Setenv("BITBUCKET_URL", "http://localhost") // Restore config, break repo string

	repoErrorCommands := [][]string{
		{"hook", "list", "--repo", "invalid"},
		{"hook", "enable", "h1", "--repo", "invalid"},
		{"hook", "disable", "h1", "--repo", "invalid"},
		{"hook", "configure", "h1", "--repo", "invalid"},
		{"hook", "configure", "h1", "{}", "--repo", "invalid"},

		{"reviewer", "condition", "list", "--repo", "invalid"},
		{"reviewer", "condition", "create", "{}", "--repo", "invalid"},
		{"reviewer", "condition", "update", "1", "{}", "--repo", "invalid"},
		{"reviewer", "condition", "delete", "1", "--repo", "invalid"},

		{"repo", "settings", "pull-requests", "get", "--repo", "invalid"},
		{"repo", "settings", "pull-requests", "update", "--repo", "invalid"},
		{"repo", "settings", "pull-requests", "update-approvers", "--count", "1", "--repo", "invalid"},
		{"repo", "settings", "pull-requests", "set-strategy", "s", "--repo", "invalid"},
		{"repo", "settings", "pull-requests", "merge-checks", "list", "--repo", "invalid"},

		{"repo", "settings", "security", "permissions", "users", "list", "--repo", "invalid"},
		{"repo", "settings", "security", "permissions", "groups", "list", "--repo", "invalid"},
		{"repo", "settings", "security", "permissions", "users", "grant", "u", "p", "--repo", "invalid"},
		{"repo", "settings", "security", "permissions", "groups", "grant", "g", "p", "--repo", "invalid"},
		{"repo", "settings", "security", "permissions", "users", "revoke", "u", "--repo", "invalid"},
		{"repo", "settings", "security", "permissions", "groups", "revoke", "g", "--repo", "invalid"},
	}

	for _, args := range repoErrorCommands {
		cmd := NewRootCommand()
		cmd.SetArgs(args)
		_ = cmd.Execute()
	}
}

func TestPRCoreUpdateDeclineReopenCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodPut && strings.Contains(request.URL.Path, "pull-requests/30"):
			_, _ = writer.Write([]byte(`{"id":30}`))
		case request.Method == http.MethodPost && strings.Contains(request.URL.Path, "pull-requests/30/decline"):
			_, _ = writer.Write([]byte(`{"id":30,"state":"DECLINED"}`))
		case request.Method == http.MethodPost && strings.Contains(request.URL.Path, "pull-requests/30/reopen"):
			_, _ = writer.Write([]byte(`{"id":30,"state":"OPEN"}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()

	// PR update (human)
	command.SetArgs([]string{"pr", "update", "30", "--version", "1", "--title", "test", "--repo", "PRJ/demo"})
	if err := command.Execute(); err != nil {
		t.Fatalf("pr update human failed: %v", err)
	}

	// PR update (json)
	command = NewRootCommand()
	command.SetArgs([]string{"--json", "pr", "update", "30", "--version", "1", "--title", "test", "--repo", "PRJ/demo"})
	if err := command.Execute(); err != nil {
		t.Fatalf("pr update json failed: %v", err)
	}

	// PR decline (human)
	command = NewRootCommand()
	command.SetArgs([]string{"pr", "decline", "30", "--repo", "PRJ/demo"})
	if err := command.Execute(); err != nil {
		t.Fatalf("pr decline human failed: %v", err)
	}

	// PR decline (json)
	command = NewRootCommand()
	command.SetArgs([]string{"--json", "pr", "decline", "30", "--repo", "PRJ/demo"})
	if err := command.Execute(); err != nil {
		t.Fatalf("pr decline json failed: %v", err)
	}

	// PR reopen (human)
	command = NewRootCommand()
	command.SetArgs([]string{"pr", "reopen", "30", "--repo", "PRJ/demo"})
	if err := command.Execute(); err != nil {
		t.Fatalf("pr reopen human failed: %v", err)
	}

	// PR reopen (json)
	command = NewRootCommand()
	command.SetArgs([]string{"--json", "pr", "reopen", "30", "--repo", "PRJ/demo"})
	if err := command.Execute(); err != nil {
		t.Fatalf("pr reopen json failed: %v", err)
	}
}

func TestPRCoreConfigErrorsCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "://invalid") // Cause config to fail due to invalid URL

	commands := [][]string{
		{"pr", "list", "--repo", "PRJ/demo"},
		{"pr", "get", "30", "--repo", "PRJ/demo"},
		{"pr", "create", "--from-ref", "f", "--to-ref", "t", "--title", "T", "--repo", "PRJ/demo"},
		{"pr", "update", "30", "--version", "1", "--title", "T", "--repo", "PRJ/demo"},
		{"pr", "merge", "30", "--repo", "PRJ/demo"},
		{"pr", "decline", "30", "--repo", "PRJ/demo"},
		{"pr", "reopen", "30", "--repo", "PRJ/demo"},
		{"pr", "review", "approve", "30", "--repo", "PRJ/demo"},
		{"pr", "review", "unapprove", "30", "--repo", "PRJ/demo"},
		{"pr", "review", "reviewer", "add", "30", "--user", "u", "--repo", "PRJ/demo"},
		{"pr", "review", "reviewer", "remove", "30", "--user", "u", "--repo", "PRJ/demo"},
		{"pr", "task", "list", "30", "--repo", "PRJ/demo"},
		{"pr", "task", "create", "30", "--text", "todo", "--repo", "PRJ/demo"},
		{"pr", "task", "update", "1", "--repo", "PRJ/demo"},
		{"pr", "task", "delete", "1", "--repo", "PRJ/demo"},
	}

	for _, args := range commands {
		cmd := NewRootCommand()
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Fatalf("expected error for command %v", args)
		} else {
			t.Logf("command %v returned error: %v", args, err)
		}
	}
}

func TestPRCoreErrorsCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()

	// get
	command.SetArgs([]string{"pr", "get", "30", "--repo", "invalid"})
	_ = command.Execute()
	command.SetArgs([]string{"pr", "get", "30", "--repo", "PRJ/demo"})
	_ = command.Execute()

	// create
	command.SetArgs([]string{"pr", "create", "--from-ref", "a", "--to-ref", "b", "--title", "c", "--repo", "invalid"})
	_ = command.Execute()
	command.SetArgs([]string{"pr", "create", "--from-ref", "a", "--to-ref", "b", "--title", "c", "--repo", "PRJ/demo"})
	_ = command.Execute()

	// update
	command.SetArgs([]string{"pr", "update", "30", "--version", "1", "--repo", "invalid"})
	_ = command.Execute()
	command.SetArgs([]string{"pr", "update", "30", "--version", "1", "--repo", "PRJ/demo"})
	_ = command.Execute()

	// decline
	command.SetArgs([]string{"pr", "decline", "30", "--repo", "invalid"})
	_ = command.Execute()
	command.SetArgs([]string{"pr", "decline", "30", "--repo", "PRJ/demo"})
	_ = command.Execute()

	// reopen
	command.SetArgs([]string{"pr", "reopen", "30", "--repo", "invalid"})
	_ = command.Execute()
	command.SetArgs([]string{"pr", "reopen", "30", "--repo", "PRJ/demo"})
	_ = command.Execute()

	// approve
	command.SetArgs([]string{"pr", "approve", "30", "--repo", "invalid"})
	_ = command.Execute()
	command.SetArgs([]string{"pr", "approve", "30", "--repo", "PRJ/demo"})
	_ = command.Execute()

	// unapprove
	command.SetArgs([]string{"pr", "unapprove", "30", "--repo", "invalid"})
	_ = command.Execute()
	command.SetArgs([]string{"pr", "unapprove", "30", "--repo", "PRJ/demo"})
	_ = command.Execute()
}

func TestPRSubCommandsCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && strings.Contains(request.URL.Path, "tasks"):
			_, _ = writer.Write([]byte(`{"values":[{"id":1,"state":"OPEN","text":"todo"}]}`))
		case request.Method == http.MethodPost && strings.Contains(request.URL.Path, "tasks"):
			writer.WriteHeader(http.StatusCreated)
			_, _ = writer.Write([]byte(`{"id":2}`))
		case request.Method == http.MethodPut && strings.Contains(request.URL.Path, "tasks/1"):
			_, _ = writer.Write([]byte(`{"id":1}`))
		case request.Method == http.MethodDelete && strings.Contains(request.URL.Path, "tasks/1"):
			writer.WriteHeader(http.StatusNoContent)
		case request.Method == http.MethodPost && strings.Contains(request.URL.Path, "participants/u2"):
			_, _ = writer.Write([]byte(`{"user":{"name":"u2"}}`))
		case request.Method == http.MethodDelete && strings.Contains(request.URL.Path, "participants/u2"):
			writer.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	commands := [][]string{
		{"pr", "review", "reviewer", "add", "30", "--user", "u2", "--repo", "PRJ/demo"},
		{"--json", "pr", "review", "reviewer", "add", "30", "--user", "u2", "--repo", "PRJ/demo"},
		{"pr", "review", "reviewer", "remove", "30", "--user", "u2", "--repo", "PRJ/demo"},
		{"--json", "pr", "review", "reviewer", "remove", "30", "--user", "u2", "--repo", "PRJ/demo"},

		{"pr", "task", "list", "30", "--repo", "PRJ/demo"},
		{"--json", "pr", "task", "list", "30", "--repo", "PRJ/demo"},

		{"pr", "task", "create", "30", "--text", "todo", "--repo", "PRJ/demo"},
		{"--json", "pr", "task", "create", "30", "--text", "todo", "--repo", "PRJ/demo"},

		{"pr", "task", "update", "1", "--repo", "PRJ/demo"},
		{"--json", "pr", "task", "update", "1", "--repo", "PRJ/demo"},

		{"pr", "task", "delete", "1", "--repo", "PRJ/demo"},
		{"--json", "pr", "task", "delete", "1", "--repo", "PRJ/demo"},
	}

	for _, args := range commands {
		cmd := NewRootCommand()
		cmd.SetArgs(args)
		_ = cmd.Execute()
	}
}

func TestRepoSettingsPermissionsErrorsCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	cmd1 := NewRootCommand()
	cmd1.SetArgs([]string{"repo", "settings", "security", "permissions", "groups", "list", "--repo", "PRJ/demo"})
	_ = cmd1.Execute()

	cmd2 := NewRootCommand()
	cmd2.SetArgs([]string{"repo", "settings", "security", "permissions", "users", "list", "--repo", "PRJ/demo"})
	_ = cmd2.Execute()

	cmd3 := NewRootCommand()
	cmd3.SetArgs([]string{"repo", "settings", "security", "permissions", "groups", "grant", "g1", "REPO_READ", "--repo", "PRJ/demo"})
	_ = cmd3.Execute()

	cmd4 := NewRootCommand()
	cmd4.SetArgs([]string{"repo", "settings", "security", "permissions", "users", "grant", "u1", "REPO_READ", "--repo", "PRJ/demo"})
	_ = cmd4.Execute()

	cmd5 := NewRootCommand()
	cmd5.SetArgs([]string{"repo", "settings", "security", "permissions", "groups", "revoke", "g1", "--repo", "PRJ/demo"})
	_ = cmd5.Execute()

	cmd6 := NewRootCommand()
	cmd6.SetArgs([]string{"repo", "settings", "security", "permissions", "users", "revoke", "u1", "--repo", "PRJ/demo"})
	_ = cmd6.Execute()
}

func TestRepoSettingsPermissionsMissingConfigCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "://invalid")

	// Ensure config load fails
	cmd1 := NewRootCommand()
	cmd1.SetArgs([]string{"repo", "settings", "security", "permissions", "groups", "list", "--repo", "PRJ/demo"})
	_ = cmd1.Execute()

	cmd2 := NewRootCommand()
	cmd2.SetArgs([]string{"repo", "settings", "security", "permissions", "users", "list", "--repo", "PRJ/demo"})
	_ = cmd2.Execute()

	cmd3 := NewRootCommand()
	cmd3.SetArgs([]string{"repo", "settings", "security", "permissions", "groups", "grant", "g1", "REPO_READ", "--repo", "PRJ/demo"})
	_ = cmd3.Execute()

	cmd4 := NewRootCommand()
	cmd4.SetArgs([]string{"repo", "settings", "security", "permissions", "users", "grant", "u1", "REPO_READ", "--repo", "PRJ/demo"})
	_ = cmd4.Execute()

	cmd5 := NewRootCommand()
	cmd5.SetArgs([]string{"repo", "settings", "security", "permissions", "groups", "revoke", "g1", "--repo", "PRJ/demo"})
	_ = cmd5.Execute()

	cmd6 := NewRootCommand()
	cmd6.SetArgs([]string{"repo", "settings", "security", "permissions", "users", "revoke", "u1", "--repo", "PRJ/demo"})
	_ = cmd6.Execute()
}

func TestPRWatchUnwatchRebaseCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/repos":
			if request.URL.Query().Get("projectkey") == "FORBIDDEN" {
				writer.WriteHeader(http.StatusForbidden)
				_, _ = writer.Write([]byte(`{"errors":[{"message":"permission denied"}]}`))
				return
			}
			_, _ = writer.Write([]byte(`{"values":[{"slug":"demo","name":"demo","project":{"key":"PRJ"}}],"isLastPage":true}`))
		case request.Method == http.MethodPost && request.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/30/watch":
			writer.WriteHeader(http.StatusNoContent)
		case request.Method == http.MethodDelete && request.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/30/watch":
			writer.WriteHeader(http.StatusNoContent)
		case request.Method == http.MethodGet && request.URL.Path == "/rest/git/latest/projects/PRJ/repos/demo/pull-requests/30/rebase":
			_, _ = writer.Write([]byte(`{"vetoes":[]}`))
		case request.Method == http.MethodGet && request.URL.Path == "/rest/git/latest/projects/PRJ/repos/demo/pull-requests/31/rebase":
			_, _ = writer.Write([]byte(`{"vetoes":[{"summaryMessage":"blocked","detailedMessage":"conflict"}]}`))
		case request.Method == http.MethodPost && request.URL.Path == "/rest/git/latest/projects/PRJ/repos/demo/pull-requests/30/rebase":
			_, _ = writer.Write([]byte(`{"refChange":{"toHash":"newhash"}}`))
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/30":
			_, _ = writer.Write([]byte(`{"id":30,"title":"T","version":1}`))
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/99":
			writer.WriteHeader(http.StatusNotFound)
			_, _ = writer.Write([]byte(`{"errors":[{"message":"not found"}]}`))
		case request.Method == http.MethodPost && request.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/99/watch":
			writer.WriteHeader(http.StatusInternalServerError)
		case request.Method == http.MethodDelete && request.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/99/watch":
			writer.WriteHeader(http.StatusInternalServerError)
		case request.Method == http.MethodGet && request.URL.Path == "/rest/git/latest/projects/PRJ/repos/demo/pull-requests/99/rebase":
			writer.WriteHeader(http.StatusInternalServerError)
		case request.Method == http.MethodPost && request.URL.Path == "/rest/git/latest/projects/PRJ/repos/demo/pull-requests/99/rebase":
			writer.WriteHeader(http.StatusInternalServerError)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	commands := [][]string{
		{"pr", "watch", "30", "--repo", "PRJ/demo"},
		{"--json", "pr", "watch", "30", "--repo", "PRJ/demo"},
		{"--dry-run", "pr", "watch", "30", "--repo", "PRJ/demo"},
		{"--json", "--dry-run", "pr", "watch", "30", "--repo", "PRJ/demo"},

		{"pr", "unwatch", "30", "--repo", "PRJ/demo"},
		{"--json", "pr", "unwatch", "30", "--repo", "PRJ/demo"},
		{"--dry-run", "pr", "unwatch", "30", "--repo", "PRJ/demo"},
		{"--json", "--dry-run", "pr", "unwatch", "30", "--repo", "PRJ/demo"},

		{"pr", "rebase", "30", "--repo", "PRJ/demo"},
		{"--json", "pr", "rebase", "30", "--repo", "PRJ/demo"},
		{"--dry-run", "pr", "rebase", "30", "--repo", "PRJ/demo"},
		{"--json", "--dry-run", "pr", "rebase", "30", "--repo", "PRJ/demo"},

		{"--dry-run", "pr", "rebase", "31", "--repo", "PRJ/demo"},
	}

	for _, args := range commands {
		cmd := NewRootCommand()
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("command failed: %v %v", args, err)
		}
	}

	// Test config load error path
	{
		t.Setenv("BITBUCKET_URL", "")
		for _, op := range []string{"watch", "unwatch", "rebase"} {
			cmd := NewRootCommand()
			cmd.SetArgs([]string{"pr", op, "30", "--repo", "PRJ/demo"})
			_ = cmd.Execute()
		}
		t.Setenv("BITBUCKET_URL", server.URL)
	}

	// Test repo resolution error path
	{
		for _, op := range []string{"watch", "unwatch", "rebase"} {
			cmd := NewRootCommand()
			cmd.SetArgs([]string{"pr", op, "30", "--repo", "invalid"})
			_ = cmd.Execute()
		}
	}

	// Test permission checker error path in dry-run
	{
		for _, op := range []string{"watch", "unwatch", "rebase"} {
			cmd := NewRootCommand()
			cmd.SetArgs([]string{"--dry-run", "pr", op, "30", "--repo", "FORBIDDEN/demo"})
			_ = cmd.Execute()
		}
	}

	// Test validation & live errors
	errorCommands := [][]string{
		{"--dry-run", "pr", "watch", "99", "--repo", "PRJ/demo"},
		{"pr", "watch", "99", "--repo", "PRJ/demo"},
		{"--dry-run", "pr", "unwatch", "99", "--repo", "PRJ/demo"},
		{"pr", "unwatch", "99", "--repo", "PRJ/demo"},
		{"--dry-run", "pr", "rebase", "99", "--repo", "PRJ/demo"},
		{"pr", "rebase", "99", "--repo", "PRJ/demo"},
	}
	for _, args := range errorCommands {
		cmd := NewRootCommand()
		cmd.SetArgs(args)
		_ = cmd.Execute()
	}
}

func TestCommitPRsAndParticipantsCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/repos":
			_, _ = writer.Write([]byte(`{"values":[{"slug":"demo","name":"demo","project":{"key":"PRJ"}}],"isLastPage":true}`))
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/commits/sha123/pull-requests":
			_, _ = writer.Write([]byte(`{"values":[{"id":42,"title":"PR Title","state":"OPEN","open":true,"closed":false,"draft":false,"version":1}]}`))
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/commits/sha999/pull-requests":
			_, _ = writer.Write([]byte(`{"values":[]}`))
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/commits/shaError/pull-requests":
			writer.WriteHeader(http.StatusInternalServerError)
			_, _ = writer.Write([]byte(`{"errors":[{"message":"internal error"}]}`))
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/participants":
			filter := request.URL.Query().Get("filter")
			if filter == "user1" {
				_, _ = writer.Write([]byte(`{"values":[{"active":true,"displayName":"User One","emailAddress":"user1@example.com","id":1,"name":"user1","slug":"user1"}]}`))
			} else if filter == "userInactive" {
				_, _ = writer.Write([]byte(`{"values":[{"active":false,"displayName":"User Inactive","emailAddress":"inactive@example.com","id":2,"name":"userinactive","slug":"userinactive"}]}`))
			} else if filter == "userError" {
				writer.WriteHeader(http.StatusInternalServerError)
				_, _ = writer.Write([]byte(`{"errors":[{"message":"internal error"}]}`))
			} else {
				_, _ = writer.Write([]byte(`{"values":[]}`))
			}
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	commands := [][]string{
		{"commit", "prs", "sha123", "--repo", "PRJ/demo"},
		{"--json", "commit", "prs", "sha123", "--repo", "PRJ/demo"},
		{"commit", "prs", "sha999", "--repo", "PRJ/demo"},

		{"pr", "participants", "--search", "user1", "--repo", "PRJ/demo"},
		{"--json", "pr", "participants", "--search", "user1", "--repo", "PRJ/demo"},
		{"pr", "participants", "--search", "user999", "--repo", "PRJ/demo"},
		{"pr", "participants", "--search", "userInactive", "--repo", "PRJ/demo"},
	}

	for _, args := range commands {
		cmd := NewRootCommand()
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("command failed: %v %v", args, err)
		}
	}

	// Test config load error path
	{
		t.Setenv("BITBUCKET_URL", "")
		cmd1 := NewRootCommand()
		cmd1.SetArgs([]string{"commit", "prs", "sha123", "--repo", "PRJ/demo"})
		_ = cmd1.Execute()

		cmd2 := NewRootCommand()
		cmd2.SetArgs([]string{"pr", "participants", "--search", "user1", "--repo", "PRJ/demo"})
		_ = cmd2.Execute()
		t.Setenv("BITBUCKET_URL", server.URL)
	}

	// Test repo resolution error path
	{
		cmd1 := NewRootCommand()
		cmd1.SetArgs([]string{"commit", "prs", "sha123", "--repo", "invalid"})
		_ = cmd1.Execute()

		cmd2 := NewRootCommand()
		cmd2.SetArgs([]string{"pr", "participants", "--search", "user1", "--repo", "invalid"})
		_ = cmd2.Execute()
	}

	// Test missing search query validation
	{
		cmd := NewRootCommand()
		cmd.SetArgs([]string{"pr", "participants", "--repo", "PRJ/demo"})
		_ = cmd.Execute()
	}

	// Test CLI service error paths
	{
		cmd1 := NewRootCommand()
		cmd1.SetArgs([]string{"commit", "prs", "shaError", "--repo", "PRJ/demo"})
		_ = cmd1.Execute()

		cmd2 := NewRootCommand()
		cmd2.SetArgs([]string{"pr", "participants", "--search", "userError", "--repo", "PRJ/demo"})
		_ = cmd2.Execute()
	}
}

func TestReviewerGroupAndDefaultReviewersCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		// Repo permissions
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/repos" && request.URL.Query().Get("permission") == "REPO_ADMIN":
			projectKey := request.URL.Query().Get("projectKey")
			if projectKey == "FORBIDDEN" {
				_, _ = writer.Write([]byte(`{"values":[],"isLastPage":true}`))
			} else {
				_, _ = writer.Write([]byte(`{"values":[{"slug":"demo","name":"demo","project":{"key":"PRJ"}}],"isLastPage":true}`))
			}

		// Project permissions
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/PRJ/permissions/users":
			_, _ = writer.Write([]byte(`{"values":[]}`))
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/FORBIDDEN/permissions/users":
			writer.WriteHeader(http.StatusForbidden)
			_, _ = writer.Write([]byte(`{"errors":[{"message":"Forbidden"}]}`))

		// Repository Reviewer Groups
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/settings/reviewer-groups":
			_, _ = writer.Write([]byte(`{"values":[{"id":2,"name":"group2","description":"desc2"}]}`))
		case request.Method == http.MethodPost && request.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/settings/reviewer-groups":
			writer.WriteHeader(http.StatusCreated)
			_, _ = writer.Write([]byte(`{"id":4,"name":"group4"}`))
		case request.Method == http.MethodPut && request.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/settings/reviewer-groups/2":
			_, _ = writer.Write([]byte(`{"id":2,"name":"group2-updated"}`))
		case request.Method == http.MethodDelete && request.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/settings/reviewer-groups/2":
			writer.WriteHeader(http.StatusNoContent)
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/settings/reviewer-groups/2/users":
			_, _ = writer.Write([]byte(`[{"name":"user1","displayName":"User One","emailAddress":"user1@example.com","active":true}]`))

		// Project Reviewer Groups
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/PRJ/settings/reviewer-groups":
			_, _ = writer.Write([]byte(`{"values":[{"id":1,"name":"group1","description":"desc1"}]}`))
		case request.Method == http.MethodPost && request.URL.Path == "/rest/api/latest/projects/PRJ/settings/reviewer-groups":
			writer.WriteHeader(http.StatusCreated)
			_, _ = writer.Write([]byte(`{"id":3,"name":"group3"}`))
		case request.Method == http.MethodPut && request.URL.Path == "/rest/api/latest/projects/PRJ/settings/reviewer-groups/1":
			_, _ = writer.Write([]byte(`{"id":1,"name":"group1-updated"}`))
		case request.Method == http.MethodDelete && request.URL.Path == "/rest/api/latest/projects/PRJ/settings/reviewer-groups/1":
			writer.WriteHeader(http.StatusNoContent)

		// Default Reviewers
		case request.Method == http.MethodGet && request.URL.Path == "/rest/default-reviewers/latest/projects/PRJ/repos/demo/reviewers":
			_, _ = writer.Write([]byte(`[{"id":5,"requiredApprovals":2,"reviewers":[{"name":"user1","displayName":"User One"}]}]`))

		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	// Test reviewer-group subcommands
	testCases := [][]string{
		// List
		{"reviewer-group", "list", "--project", "PRJ"},
		{"--json", "reviewer-group", "list", "--project", "PRJ"},
		{"reviewer-group", "list", "--repo", "PRJ/demo"},
		{"--json", "reviewer-group", "list", "--repo", "PRJ/demo"},

		// Create
		{"reviewer-group", "create", "group3", "--project", "PRJ", "--description", "desc3"},
		{"--json", "reviewer-group", "create", "group3", "--project", "PRJ"},
		{"reviewer-group", "create", "group4", "--repo", "PRJ/demo"},
		{"--json", "reviewer-group", "create", "group4", "--repo", "PRJ/demo"},
		{"--dry-run", "reviewer-group", "create", "group3", "--project", "PRJ"},
		{"--dry-run", "reviewer-group", "create", "group1", "--project", "PRJ"}, // conflict
		{"--dry-run", "reviewer-group", "create", "group4", "--repo", "PRJ/demo"},
		{"--dry-run", "reviewer-group", "create", "group2", "--repo", "PRJ/demo"}, // conflict

		// Update
		{"reviewer-group", "update", "1", "--project", "PRJ", "--name", "newname"},
		{"--json", "reviewer-group", "update", "1", "--project", "PRJ"},
		{"reviewer-group", "update", "2", "--repo", "PRJ/demo", "--description", "newdesc"},
		{"--json", "reviewer-group", "update", "2", "--repo", "PRJ/demo"},
		{"--dry-run", "reviewer-group", "update", "1", "--project", "PRJ", "--name", "newname"},
		{"--dry-run", "reviewer-group", "update", "1", "--project", "PRJ"}, // no-op
		{"--dry-run", "reviewer-group", "update", "99", "--project", "PRJ"}, // not found
		{"--dry-run", "reviewer-group", "update", "2", "--repo", "PRJ/demo", "--description", "newdesc"},
		{"--dry-run", "reviewer-group", "update", "2", "--repo", "PRJ/demo"}, // no-op
		{"--dry-run", "reviewer-group", "update", "99", "--repo", "PRJ/demo"}, // not found

		// Delete
		{"reviewer-group", "delete", "1", "--project", "PRJ"},
		{"--json", "reviewer-group", "delete", "1", "--project", "PRJ"},
		{"reviewer-group", "delete", "2", "--repo", "PRJ/demo"},
		{"--json", "reviewer-group", "delete", "2", "--repo", "PRJ/demo"},
		{"--dry-run", "reviewer-group", "delete", "1", "--project", "PRJ"},
		{"--dry-run", "reviewer-group", "delete", "99", "--project", "PRJ"}, // no-op
		{"--dry-run", "reviewer-group", "delete", "2", "--repo", "PRJ/demo"},
		{"--dry-run", "reviewer-group", "delete", "99", "--repo", "PRJ/demo"}, // no-op

		// Users
		{"reviewer-group", "users", "2", "--repo", "PRJ/demo"},
		{"--json", "reviewer-group", "users", "2", "--repo", "PRJ/demo"},

		// Default Reviewers
		{"pr", "default-reviewers", "--repo", "PRJ/demo"},
		{"--json", "pr", "default-reviewers", "--repo", "PRJ/demo"},
		{"pr", "default-reviewers", "--repo", "PRJ/demo", "--source-repo-id", "1", "--target-repo-id", "1", "--source-ref", "refs/heads/feature", "--target-ref", "refs/heads/master"},
	}

	for _, args := range testCases {
		cmd := NewRootCommand()
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("reviewer group command failed: %v %v", args, err)
		}
	}

	// Validate mutual exclusivity
	invalidCases := [][]string{
		{"reviewer-group", "list", "--project", "PRJ", "--repo", "PRJ/demo"},
		{"reviewer-group", "create", "grp", "--project", "PRJ", "--repo", "PRJ/demo"},
		{"reviewer-group", "update", "1", "--project", "PRJ", "--repo", "PRJ/demo"},
		{"reviewer-group", "delete", "1", "--project", "PRJ", "--repo", "PRJ/demo"},
		{"reviewer-group", "users", "1", "--project", "PRJ"},
	}

	for _, args := range invalidCases {
		cmd := NewRootCommand()
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Fatalf("expected mutual exclusion error for args: %v", args)
		}
	}

	// Validate forbidden dry-runs (permission check failures)
	forbiddenCases := [][]string{
		{"--dry-run", "reviewer-group", "create", "grp", "--project", "FORBIDDEN"},
		{"--dry-run", "reviewer-group", "create", "grp", "--repo", "FORBIDDEN/demo"},
		{"--dry-run", "reviewer-group", "update", "1", "--project", "FORBIDDEN"},
		{"--dry-run", "reviewer-group", "update", "1", "--repo", "FORBIDDEN/demo"},
		{"--dry-run", "reviewer-group", "delete", "1", "--project", "FORBIDDEN"},
		{"--dry-run", "reviewer-group", "delete", "1", "--repo", "FORBIDDEN/demo"},
	}

	for _, args := range forbiddenCases {
		cmd := NewRootCommand()
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Fatalf("expected permission error for args: %v", args)
		}
	}
}

func TestDefaultReviewersCLIErrorsAndFallbacks(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	
	// 1. Error resolving repository (repo flag invalid)
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"pr", "default-reviewers", "--repo", "INVALID_FORMAT"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for invalid repository format")
	}

	// 2. Error getting default reviewers (server returns 500)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	cmd = NewRootCommand()
	cmd.SetArgs([]string{"pr", "default-reviewers", "--repo", "PRJ/demo"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error from server 500")
	}

	// 3. Empty conditions (returns empty list)
	serverEmpty := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`[]`))
	}))
	defer serverEmpty.Close()

	t.Setenv("BITBUCKET_URL", serverEmpty.URL)

	cmd = NewRootCommand()
	buffer := &bytes.Buffer{}
	cmd.SetOut(buffer)
	cmd.SetArgs([]string{"pr", "default-reviewers", "--repo", "PRJ/demo"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !strings.Contains(buffer.String(), "No default reviewers or conditions found") {
		t.Fatalf("expected empty warning in output, got: %s", buffer.String())
	}

	// 4. Ref matchers display ID coverage and DisplayName nil fallback
	serverRefs := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`[
			{
				"id": 1,
				"sourceRefMatcher": {"displayId": "refs/heads/src"},
				"targetRefMatcher": {"displayId": "refs/heads/tgt"},
				"requiredApprovals": 2,
				"reviewers": [
					{"name": "user_name_only"}
				]
			}
		]`))
	}))
	defer serverRefs.Close()

	t.Setenv("BITBUCKET_URL", serverRefs.URL)

	cmd = NewRootCommand()
	buffer.Reset()
	cmd.SetOut(buffer)
	cmd.SetArgs([]string{"pr", "default-reviewers", "--repo", "PRJ/demo"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !strings.Contains(buffer.String(), "refs/heads/src") || !strings.Contains(buffer.String(), "user_name_only") {
		t.Fatalf("expected refs and name fallback in output, got: %s", buffer.String())
	}
}



