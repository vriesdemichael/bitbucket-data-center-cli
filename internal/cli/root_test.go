package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/git"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/services/diff"
	pullrequestactivityservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/pullrequestactivity"
)

type inferenceGitBackendStub struct {
	repoRoot string
	rootErr  error
	remotes  []git.Remote
	listErr  error
}

func (stub inferenceGitBackendStub) Version(context.Context) (string, error) {
	return "", nil
}

func (stub inferenceGitBackendStub) Clone(context.Context, string, git.CloneOptions) error {
	return nil
}

func (stub inferenceGitBackendStub) AddRemote(context.Context, string, git.Remote) error {
	return nil
}

func (stub inferenceGitBackendStub) Fetch(context.Context, string, git.FetchOptions) error {
	return nil
}

func (stub inferenceGitBackendStub) Checkout(context.Context, string, git.CheckoutOptions) error {
	return nil
}

func (stub inferenceGitBackendStub) RepositoryRoot(context.Context, string) (string, error) {
	if stub.rootErr != nil {
		return "", stub.rootErr
	}
	return stub.repoRoot, nil
}

func (stub inferenceGitBackendStub) ListRemotes(context.Context, string) ([]git.Remote, error) {
	if stub.listErr != nil {
		return nil, stub.listErr
	}
	return stub.remotes, nil
}

func init() {
	// Block external network access during tests by default
	os.Setenv("BB_BLOCK_EXTERNAL_NETWORK", "1")
}

func TestAuthStatusSmoke(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_VERSION_TARGET", "9.4.16")
	t.Setenv("BITBUCKET_TOKEN", "")
	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("BITBUCKET_PASSWORD", "")
	t.Setenv("ADMIN_USER", "")
	t.Setenv("ADMIN_PASSWORD", "")

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"auth", "status"})

	err := command.Execute()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if !strings.Contains(buffer.String(), "Target Bitbucket") {
		t.Fatalf("expected auth status output, got: %s", buffer.String())
	}

	if !strings.Contains(buffer.String(), "auth=none") {
		t.Fatalf("expected auth mode in output, got: %s", buffer.String())
	}

	if !strings.Contains(buffer.String(), "source=env/default") {
		t.Fatalf("expected auth source in output, got: %s", buffer.String())
	}
}
func TestBranchValidationErrors(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	tests := []struct {
		name         string
		args         []string
		expectAppErr bool
	}{
		{name: "branch create missing start-point", args: []string{"branch", "create", "feature/demo"}, expectAppErr: false},
		{name: "branch restriction create missing matcher-id", args: []string{"branch", "restriction", "create", "--type", "read-only"}, expectAppErr: false},
		{name: "branch restriction create access key overflow", args: []string{"branch", "restriction", "create", "--type", "read-only", "--matcher-id", "refs/heads/main", "--access-key-id", "2147483648"}, expectAppErr: true},
		{name: "branch restriction list invalid matcher-type", args: []string{"branch", "restriction", "list", "--matcher-type", "invalid"}, expectAppErr: true},
		{name: "branch restriction update access key overflow", args: []string{"branch", "restriction", "update", "12", "--type", "read-only", "--matcher-id", "refs/heads/main", "--access-key-id", "2147483648"}, expectAppErr: true},
		{name: "branch restriction update invalid id", args: []string{"branch", "restriction", "update", "bad", "--type", "read-only", "--matcher-id", "refs/heads/main"}, expectAppErr: true},
		{name: "branch default set blank", args: []string{"branch", "default", "set", " "}, expectAppErr: true},
		{name: "branch model update blank", args: []string{"branch", "model", "update", " "}, expectAppErr: true},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			command := NewRootCommand()
			command.SetOut(&bytes.Buffer{})
			command.SetErr(&bytes.Buffer{})
			command.SetArgs(testCase.args)

			err := command.Execute()
			if err == nil {
				t.Fatalf("expected validation error for args: %v", testCase.args)
			}
			exitCode := apperrors.ExitCode(err)
			if testCase.expectAppErr && exitCode != 2 && exitCode != 4 {
				t.Fatalf("expected validation exit code 2 or 4, got %d (%v)", exitCode, err)
			}
		})
	}
}

func TestBranchCommandsFailOnInvalidRepositorySelector(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "http://example.local")
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	tests := []struct {
		name string
		args []string
	}{
		{name: "list invalid repo selector", args: []string{"branch", "list", "--repo", "bad"}},
		{name: "create invalid repo selector", args: []string{"branch", "create", "feature/demo", "--start-point", "abc", "--repo", "bad"}},
		{name: "delete invalid repo selector", args: []string{"branch", "delete", "feature/demo", "--repo", "bad"}},
		{name: "default get invalid repo selector", args: []string{"branch", "default", "get", "--repo", "bad"}},
		{name: "default set invalid repo selector", args: []string{"branch", "default", "set", "main", "--repo", "bad"}},
		{name: "model inspect invalid repo selector", args: []string{"branch", "model", "inspect", "abc", "--repo", "bad"}},
		{name: "model update invalid repo selector", args: []string{"branch", "model", "update", "main", "--repo", "bad"}},
		{name: "restriction list invalid repo selector", args: []string{"branch", "restriction", "list", "--repo", "bad"}},
		{name: "restriction get invalid repo selector", args: []string{"branch", "restriction", "get", "12", "--repo", "bad"}},
		{name: "restriction create invalid repo selector", args: []string{"branch", "restriction", "create", "--type", "read-only", "--matcher-id", "refs/heads/main", "--repo", "bad"}},
		{name: "restriction update invalid repo selector", args: []string{"branch", "restriction", "update", "12", "--type", "read-only", "--matcher-id", "refs/heads/main", "--repo", "bad"}},
		{name: "restriction delete invalid repo selector", args: []string{"branch", "restriction", "delete", "12", "--repo", "bad"}},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			command := NewRootCommand()
			command.SetOut(&bytes.Buffer{})
			command.SetErr(&bytes.Buffer{})
			command.SetArgs(testCase.args)

			err := command.Execute()
			if err == nil {
				t.Fatalf("expected repository validation error for args: %v", testCase.args)
			}
			if apperrors.ExitCode(err) != 2 {
				t.Fatalf("expected validation exit code 2 for args %v, got %d (%v)", testCase.args, apperrors.ExitCode(err), err)
			}
		})
	}
}

func TestBranchCommandsFailOnInvalidConfig(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "://bad-url")
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	tests := []struct {
		name string
		args []string
	}{
		{name: "list invalid config", args: []string{"branch", "list"}},
		{name: "create invalid config", args: []string{"branch", "create", "feature/demo", "--start-point", "abc"}},
		{name: "delete invalid config", args: []string{"branch", "delete", "feature/demo"}},
		{name: "default get invalid config", args: []string{"branch", "default", "get"}},
		{name: "default set invalid config", args: []string{"branch", "default", "set", "main"}},
		{name: "model inspect invalid config", args: []string{"branch", "model", "inspect", "abc"}},
		{name: "model update invalid config", args: []string{"branch", "model", "update", "main"}},
		{name: "restriction list invalid config", args: []string{"branch", "restriction", "list"}},
		{name: "restriction get invalid config", args: []string{"branch", "restriction", "get", "12"}},
		{name: "restriction create invalid config", args: []string{"branch", "restriction", "create", "--type", "read-only", "--matcher-id", "refs/heads/main"}},
		{name: "restriction update invalid config", args: []string{"branch", "restriction", "update", "12", "--type", "read-only", "--matcher-id", "refs/heads/main"}},
		{name: "restriction delete invalid config", args: []string{"branch", "restriction", "delete", "12"}},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			command := NewRootCommand()
			command.SetOut(&bytes.Buffer{})
			command.SetErr(&bytes.Buffer{})
			command.SetArgs(testCase.args)

			err := command.Execute()
			if err == nil {
				t.Fatalf("expected config validation error for args: %v", testCase.args)
			}
			if apperrors.ExitCode(err) != 2 {
				t.Fatalf("expected validation exit code 2 for args %v, got %d (%v)", testCase.args, apperrors.ExitCode(err), err)
			}
		})
	}
}

func TestTagCreateJSON(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/rest/api/latest/projects/TEST/repos/demo/tags" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
		_, _ = writer.Write([]byte(`{"displayId":"v1.0.0","latestCommit":"abc123","type":"ANNOTATED"}`))
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"--json", "tag", "create", "v1.0.0", "--start-point", "abc123"})

	err := command.Execute()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if !strings.Contains(buffer.String(), "v1.0.0") {
		t.Fatalf("expected created tag output, got: %s", buffer.String())
	}
}

func TestBuildStatusSetJSON(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/rest/build-status/latest/commits/abc123" {
			http.NotFound(writer, request)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"--json", "build", "status", "set", "abc123", "--key", "ci/main", "--state", "SUCCESSFUL", "--url", "https://example.invalid/build/1"})

	err := command.Execute()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if !strings.Contains(buffer.String(), "\"status\": \"ok\"") {
		t.Fatalf("expected ok response, got: %s", buffer.String())
	}
}

func TestInsightsReportSetJSON(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPut || request.URL.Path != "/rest/insights/latest/projects/TEST/repos/demo/commits/abc123/reports/lint" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
		_, _ = writer.Write([]byte(`{"key":"lint","title":"Lint","result":"PASS"}`))
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"--json", "insights", "report", "set", "abc123", "lint", "--body", `{"title":"Lint","result":"PASS","data":[{"title":"warnings","type":"NUMBER","value":{"value":0}}]}`})

	err := command.Execute()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if !strings.Contains(buffer.String(), "\"key\": \"lint\"") {
		t.Fatalf("expected report key in output, got: %s", buffer.String())
	}
}
func TestAuthStatusJSON(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "http://localhost:7990")
	t.Setenv("BITBUCKET_VERSION_TARGET", "9.4.16")
	t.Setenv("BITBUCKET_TOKEN", "")
	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("BITBUCKET_PASSWORD", "")
	t.Setenv("ADMIN_USER", "")
	t.Setenv("ADMIN_PASSWORD", "")

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"--json", "auth", "status"})

	err := command.Execute()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	parsed := decodeJSONEnvelopeDataMap(t, buffer.Bytes())

	if asString(parsed["bitbucket_url"]) != "http://localhost:7990" {
		t.Fatalf("unexpected bitbucket_url: %q", asString(parsed["bitbucket_url"]))
	}

	if asString(parsed["auth_mode"]) != "none" {
		t.Fatalf("unexpected auth_mode: %q", asString(parsed["auth_mode"]))
	}

	if asString(parsed["auth_source"]) != "env/default" {
		t.Fatalf("unexpected auth_source: %q", asString(parsed["auth_source"]))
	}
}

func asString(value any) string {
	if typed, ok := value.(string); ok {
		return typed
	}
	return ""
}

func decodeJSONEnvelopeDataMap(t *testing.T, raw []byte) map[string]any {
	t.Helper()

	var envelope struct {
		Version string         `json:"version"`
		Data    map[string]any `json:"data"`
	}

	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("expected valid json output, got: %s (%v)", string(raw), err)
	}

	if envelope.Version != jsonoutput.ContractVersion {
		t.Fatalf("expected json envelope version %q, got %q", jsonoutput.ContractVersion, envelope.Version)
	}

	if envelope.Data == nil {
		t.Fatalf("expected json envelope data payload, got: %s", string(raw))
	}

	return envelope.Data
}

func TestRootTransportFlagsOverrideEnvironment(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "http://localhost:7990")
	t.Setenv("BB_REQUEST_TIMEOUT", "not-a-duration")

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"--request-timeout", "1s", "auth", "status"})

	err := command.Execute()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestApplyRuntimeFlagOverridesBranches(t *testing.T) {
	if err := applyRuntimeFlagOverrides(nil); err != nil {
		t.Fatalf("expected nil command to be a no-op, got: %v", err)
	}

	command := NewRootCommand()
	if err := command.PersistentFlags().Set("ca-file", " "); err != nil {
		t.Fatalf("set ca-file: %v", err)
	}
	if err := command.PersistentFlags().Set("insecure-skip-verify", "true"); err != nil {
		t.Fatalf("set insecure-skip-verify: %v", err)
	}
	if err := command.PersistentFlags().Set("request-timeout", "30s"); err != nil {
		t.Fatalf("set request-timeout: %v", err)
	}
	if err := command.PersistentFlags().Set("retry-count", "4"); err != nil {
		t.Fatalf("set retry-count: %v", err)
	}
	if err := command.PersistentFlags().Set("retry-backoff", "500ms"); err != nil {
		t.Fatalf("set retry-backoff: %v", err)
	}

	t.Setenv("BB_CA_FILE", "/tmp/keep")
	if err := applyRuntimeFlagOverrides(command); err != nil {
		t.Fatalf("expected runtime overrides to apply, got: %v", err)
	}

	if value := os.Getenv("BB_CA_FILE"); value != "" {
		t.Fatalf("expected BB_CA_FILE to be unset by blank flag value, got %q", value)
	}
	if value := os.Getenv("BB_INSECURE_SKIP_VERIFY"); value != "true" {
		t.Fatalf("unexpected BB_INSECURE_SKIP_VERIFY value: %q", value)
	}
	if value := os.Getenv("BB_REQUEST_TIMEOUT"); value != "30s" {
		t.Fatalf("unexpected BB_REQUEST_TIMEOUT value: %q", value)
	}
	if value := os.Getenv("BB_RETRY_COUNT"); value != "4" {
		t.Fatalf("unexpected BB_RETRY_COUNT value: %q", value)
	}
	if value := os.Getenv("BB_RETRY_BACKOFF"); value != "500ms" {
		t.Fatalf("unexpected BB_RETRY_BACKOFF value: %q", value)
	}
}

func TestAdminHealthSmoke(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/rest/api/1.0/projects" {
			http.NotFound(writer, request)
			return
		}
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_TOKEN", "")
	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("BITBUCKET_PASSWORD", "")
	t.Setenv("ADMIN_USER", "")
	t.Setenv("ADMIN_PASSWORD", "")

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"admin", "health"})

	err := command.Execute()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if !strings.Contains(buffer.String(), "Bitbucket health: OK") {
		t.Fatalf("expected health output, got: %s", buffer.String())
	}
}

func TestAdminHealthJSON(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_TOKEN", "")
	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("BITBUCKET_PASSWORD", "")
	t.Setenv("ADMIN_USER", "")
	t.Setenv("ADMIN_PASSWORD", "")

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"--json", "admin", "health"})

	err := command.Execute()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	parsed := decodeJSONEnvelopeDataMap(t, buffer.Bytes())

	if healthy, ok := parsed["healthy"].(bool); !ok || !healthy {
		t.Fatalf("expected healthy=true, got: %#v", parsed["healthy"])
	}
}

func TestDiffRefsNameOnly(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/rest/api/latest/projects/TEST/repos/demo/patch" {
			http.NotFound(writer, request)
			return
		}
		_, _ = writer.Write([]byte("diff --git a/a.txt b/a.txt\ndiff --git a/dir/b.go b/dir/b.go\n"))
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")
	t.Setenv("BITBUCKET_TOKEN", "")
	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("BITBUCKET_PASSWORD", "")
	t.Setenv("ADMIN_USER", "")
	t.Setenv("ADMIN_PASSWORD", "")

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"diff", "refs", "main", "feature", "--name-only"})

	err := command.Execute()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	output := buffer.String()
	if !strings.Contains(output, "a.txt") || !strings.Contains(output, "dir/b.go") {
		t.Fatalf("expected changed files in output, got: %s", output)
	}
}

func TestDiffRefsRejectsMultipleOutputModes(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "http://localhost:7990")
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	command := NewRootCommand()
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{"diff", "refs", "main", "feature", "--patch", "--stat"})

	err := command.Execute()
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestRepoSettingsSecurityPermissionsUsersList(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/rest/api/latest/projects/TEST/repos/demo/permissions/users" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
		_, _ = writer.Write([]byte(`{"values":[{"permission":"REPO_ADMIN","user":{"name":"admin","displayName":"Admin User"}}],"isLastPage":true}`))
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"repo", "settings", "security", "permissions", "users", "list"})

	err := command.Execute()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if !strings.Contains(buffer.String(), "Admin User") || !strings.Contains(buffer.String(), "REPO_ADMIN") {
		t.Fatalf("expected user permissions output, got: %s", buffer.String())
	}
}

func TestRepoSettingsWorkflowWebhooksList(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/rest/api/latest/projects/TEST/repos/demo/webhooks" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
		_, _ = writer.Write([]byte(`{"values":[{"name":"ci"}],"size":1}`))
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"repo", "settings", "workflow", "webhooks", "list"})

	err := command.Execute()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if !strings.Contains(buffer.String(), "Webhooks configured: 1") {
		t.Fatalf("expected webhook count output, got: %s", buffer.String())
	}
}

func TestRepoSettingsPullRequestsGet(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/rest/api/latest/projects/TEST/repos/demo/settings/pull-requests" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
		_, _ = writer.Write([]byte(`{"requiredAllTasksComplete":true,"requiredApprovers":{"enabled":true,"count":"2"}}`))
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"repo", "settings", "pull-requests", "get"})

	err := command.Execute()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	output := buffer.String()
	if !strings.Contains(output, "Required tasks complete: true") || !strings.Contains(output, "Required approvers: 2") {
		t.Fatalf("expected pull request settings summary, got: %s", output)
	}
}

func TestRepoSettingsSecurityPermissionsUsersGrant(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPut || request.URL.Path != "/rest/api/latest/projects/TEST/repos/demo/permissions/users" {
			http.NotFound(writer, request)
			return
		}
		if request.URL.Query().Get("name") != "alice" || request.URL.Query().Get("permission") != "REPO_WRITE" {
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte("invalid query"))
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"repo", "settings", "security", "permissions", "users", "grant", "alice", "repo_write"})

	err := command.Execute()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if !strings.Contains(buffer.String(), "Granted REPO_WRITE to alice") {
		t.Fatalf("expected permission grant output, got: %s", buffer.String())
	}
}

func TestRepoSettingsWorkflowWebhooksCreate(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/rest/api/latest/projects/TEST/repos/demo/webhooks" {
			http.NotFound(writer, request)
			return
		}
		body, _ := io.ReadAll(request.Body)
		if !strings.Contains(string(body), `"name":"ci-hook"`) || !strings.Contains(string(body), `"url":"http://example.local/hook"`) {
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte("invalid payload"))
			return
		}
		writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
		_, _ = writer.Write([]byte(`{"name":"ci-hook"}`))
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"repo", "settings", "workflow", "webhooks", "create", "ci-hook", "http://example.local/hook", "--event", "repo:refs_changed"})

	err := command.Execute()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if !strings.Contains(buffer.String(), "Webhook created: ci-hook") {
		t.Fatalf("expected webhook create output, got: %s", buffer.String())
	}
}

func TestRepoSettingsPullRequestsUpdate(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPost && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/settings/pull-requests" {
			body, _ := io.ReadAll(request.Body)
			if !strings.Contains(string(body), `"requiredAllTasksComplete":true`) {
				writer.WriteHeader(http.StatusBadRequest)
				_, _ = writer.Write([]byte("invalid payload"))
				return
			}
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"requiredAllTasksComplete":true}`))
			return
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"repo", "settings", "pull-requests", "update", "--required-all-tasks-complete=true"})

	err := command.Execute()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if !strings.Contains(buffer.String(), "Updated pull-request settings: requiredAllTasksComplete=true") {
		t.Fatalf("expected pull-request update output, got: %s", buffer.String())
	}
}

func TestRepoSettingsWorkflowWebhooksDelete(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodDelete || request.URL.Path != "/rest/api/latest/projects/TEST/repos/demo/webhooks/42" {
			http.NotFound(writer, request)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"repo", "settings", "workflow", "webhooks", "delete", "42"})

	err := command.Execute()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if !strings.Contains(buffer.String(), "Webhook deleted: 42") {
		t.Fatalf("expected webhook delete output, got: %s", buffer.String())
	}
}

func TestRepoSettingsPullRequestsUpdateApprovers(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/rest/api/latest/projects/TEST/repos/demo/settings/pull-requests" {
			http.NotFound(writer, request)
			return
		}
		body, _ := io.ReadAll(request.Body)
		if !strings.Contains(string(body), `"requiredApprovers":2`) {
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte("invalid payload"))
			return
		}
		writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
		_, _ = writer.Write([]byte(`{"requiredApprovers":2}`))
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"repo", "settings", "pull-requests", "update-approvers", "--count", "2"})

	err := command.Execute()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if !strings.Contains(buffer.String(), "Updated pull-request settings: requiredApprovers=2") {
		t.Fatalf("expected pull-request approvers update output, got: %s", buffer.String())
	}
}

func TestRepoCommentListCommitJSON(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/rest/api/latest/projects/TEST/repos/demo/commits/abc123/comments" {
			http.NotFound(writer, request)
			return
		}
		if request.URL.Query().Get("path") != "seed.txt" {
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte("path query must be seed.txt"))
			return
		}
		writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
		_, _ = writer.Write([]byte(`{"values":[{"id":101,"text":"hello commit","version":1}],"isLastPage":true}`))
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"--json", "repo", "comment", "list", "--commit", "abc123", "--path", "seed.txt"})

	if err := command.Execute(); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	parsed := decodeJSONEnvelopeDataMap(t, buffer.Bytes())

	contextPayload, ok := parsed["context"].(map[string]any)
	if !ok {
		t.Fatalf("expected context payload, got: %#v", parsed["context"])
	}

	if contextPayload["type"] != "commit" || contextPayload["commit_id"] != "abc123" {
		t.Fatalf("unexpected context payload: %#v", contextPayload)
	}

	comments, ok := parsed["comments"].([]any)
	if !ok || len(comments) != 1 {
		t.Fatalf("expected one comment in output, got: %#v", parsed["comments"])
	}
}

func TestRepoCommentCreatePRJSON(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || !strings.HasSuffix(request.URL.Path, "/projects/TEST/repos/demo/pull-requests/77/comments") {
			http.NotFound(writer, request)
			return
		}

		body, _ := io.ReadAll(request.Body)
		if !strings.Contains(string(body), `"text":"hello pr"`) {
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte("missing text"))
			return
		}

		writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
		writer.WriteHeader(http.StatusCreated)
		_, _ = writer.Write([]byte(`{"id":202,"text":"hello pr","version":0}`))
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"--json", "repo", "comment", "create", "--pr", "77", "--text", "hello pr"})

	if err := command.Execute(); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	parsed := decodeJSONEnvelopeDataMap(t, buffer.Bytes())

	contextPayload, ok := parsed["context"].(map[string]any)
	if !ok || contextPayload["type"] != "pull_request" || contextPayload["pull_request_id"] != "77" {
		t.Fatalf("unexpected context payload: %#v", parsed["context"])
	}
}

func TestRepoCommentDeleteAutoResolvesVersion(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	var getCommentCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/commits/abc123/comments/300":
			getCommentCalls.Add(1)
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"id":300,"text":"to-delete","version":4}`))
		case request.Method == http.MethodDelete && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/commits/abc123/comments/300":
			if request.URL.Query().Get("version") != "4" {
				writer.WriteHeader(http.StatusBadRequest)
				_, _ = writer.Write([]byte("expected version=4"))
				return
			}
			writer.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"repo", "comment", "delete", "--commit", "abc123", "--id", "300"})

	if err := command.Execute(); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if getCommentCalls.Load() != 1 {
		t.Fatalf("expected one version lookup call, got: %d", getCommentCalls.Load())
	}

	if !strings.Contains(buffer.String(), "Deleted comment 300 (version=4)") {
		t.Fatalf("expected delete output with resolved version, got: %s", buffer.String())
	}
}

func TestRepoCommentDeleteWithoutResolvedVersionPrintsSimpleMessage(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/commits/abc123/comments/301":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"id":301,"text":"to-delete"}`))
		case request.Method == http.MethodDelete && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/commits/abc123/comments/301":
			if request.URL.Query().Get("version") != "" {
				writer.WriteHeader(http.StatusBadRequest)
				_, _ = writer.Write([]byte("version query should be absent when unresolved"))
				return
			}
			writer.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"repo", "comment", "delete", "--commit", "abc123", "--id", "301"})

	if err := command.Execute(); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if !strings.Contains(buffer.String(), "Deleted comment 301") || strings.Contains(buffer.String(), "version=") {
		t.Fatalf("expected simple delete message without version, got: %s", buffer.String())
	}
}

func TestAdminHealthPropagatesHardFailure(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusServiceUnavailable)
		_, _ = writer.Write([]byte("unavailable"))
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_TOKEN", "")
	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("BITBUCKET_PASSWORD", "")
	t.Setenv("ADMIN_USER", "")
	t.Setenv("ADMIN_PASSWORD", "")

	command := NewRootCommand()
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{"admin", "health"})

	err := command.Execute()
	if err == nil {
		t.Fatal("expected transient failure error")
	}
	if apperrors.ExitCode(err) != 10 {
		t.Fatalf("expected transient exit code 10, got %d (%v)", apperrors.ExitCode(err), err)
	}
}

func TestPRListAndIssueCommandUnavailable(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/rest/api/latest/projects/TEST/repos/demo/pull-requests" {
			http.NotFound(writer, request)
			return
		}

		if request.URL.Query().Get("state") != "OPEN" {
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte("unexpected state query"))
			return
		}

		writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
		_, _ = writer.Write([]byte(`{"isLastPage":true,"nextPageStart":0,"values":[{"id":22,"title":"Feature PR","state":"OPEN","open":true,"closed":false,"fromRef":{"displayId":"feature/demo"},"toRef":{"displayId":"master"}}]}`))
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	prCommand := NewRootCommand()
	prOutput := &bytes.Buffer{}
	prCommand.SetOut(prOutput)
	prCommand.SetErr(prOutput)
	prCommand.SetArgs([]string{"pr", "list", "--state", "open", "--limit", "10"})

	if err := prCommand.Execute(); err != nil {
		t.Fatalf("expected pr list to succeed, got: %v", err)
	}
	if !strings.Contains(prOutput.String(), "#22") || !strings.Contains(prOutput.String(), "feature/demo -> master") {
		t.Fatalf("expected pull request in human output, got: %s", prOutput.String())
	}

	issueCommand := NewRootCommand()
	issueOutput := &bytes.Buffer{}
	issueCommand.SetOut(issueOutput)
	issueCommand.SetErr(issueOutput)
	issueCommand.SetArgs([]string{"issue", "list"})

	err := issueCommand.Execute()
	if err == nil {
		t.Fatal("expected issue command to be unavailable")
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("expected unknown command error, got: %v", err)
	}
}

func TestBulkCommandAvailableFromRoot(t *testing.T) {
	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"bulk", "--help"})

	if err := command.Execute(); err != nil {
		t.Fatalf("expected bulk help to succeed, got: %v", err)
	}
	if !strings.Contains(buffer.String(), "plan") || !strings.Contains(buffer.String(), "apply") || !strings.Contains(buffer.String(), "status") {
		t.Fatalf("expected bulk subcommands in help output, got: %s", buffer.String())
	}
}

func TestPRLifecycleReviewAndTaskCommands(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/30":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"id":30,"title":"Feature PR","state":"OPEN","open":true,"closed":false,"fromRef":{"displayId":"feature/demo"},"toRef":{"displayId":"master"},"participants":[{"role":"REVIEWER","status":"UNAPPROVED","approved":false,"user":{"name":"reviewer1"}}]}`))
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/30/merge":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"conflicted":true,"outcome":"CONFLICTED","vetoes":[{"summaryMessage":"Required builds failed","detailedMessage":"1 required build is failing"},{"summaryMessage":"Minimum approvals not met","detailedMessage":"At least 1 approval is required"}]}`))
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/30/activities":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"isLastPage":true,"values":[{"id":901,"action":"COMMENTED","createdDate":1700000000000,"comment":{"id":301,"text":"pr thread comment","version":2}},{"id":902,"action":"COMMENTED","createdDate":1700000000001,"comment":{"id":302,"text":"anchored thread comment","version":1,"anchor":{"path":{"parent":"src","name":"seed.txt"}}}},{"id":903,"action":"APPROVED","createdDate":1700000000002}]}`))
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/30/comments":
			if request.URL.Query().Get("path") != "seed.txt" {
				writer.WriteHeader(http.StatusBadRequest)
				_, _ = writer.Write([]byte("expected path query"))
				return
			}
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"isLastPage":true,"values":[{"id":301,"text":"pr thread comment","version":2}]}`))
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/30/comments/301":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"id":301,"text":"pr thread comment\n\nwith details","version":2,"state":"OPEN","author":{"name":"reviewer1","displayName":"Reviewer One"},"anchor":{"path":{"parent":"src","name":"seed.txt"}}}`))
		case request.Method == http.MethodPost && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/30/merge":
			if request.URL.Query().Get("version") != "1" {
				writer.WriteHeader(http.StatusConflict)
				_, _ = writer.Write([]byte("expected version query"))
				return
			}
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"id":30,"title":"Feature PR","state":"MERGED","open":false,"closed":true}`))
		case request.Method == http.MethodPost && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/30/approve":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"id":30,"title":"Feature PR","state":"OPEN","open":true,"closed":false,"participants":[{"role":"REVIEWER","status":"APPROVED","approved":true,"user":{"name":"reviewer1"}}]}`))
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/30/tasks":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"isLastPage":true,"nextPageStart":0,"values":[{"id":700,"text":"Task A","state":"OPEN","resolved":false}]}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	testCases := []struct {
		name          string
		args          []string
		expectSnippet string
	}{
		{name: "pr get json", args: []string{"--json", "pr", "get", "30"}, expectSnippet: `"mergeability"`},
		{name: "pr get human mergeability", args: []string{"pr", "get", "30"}, expectSnippet: "Mergeable: no"},
		{name: "pr get human mergeability blockers", args: []string{"pr", "get", "30"}, expectSnippet: "Required builds failed (1 required build is failing)"},
		{name: "pr comment list aggregate json", args: []string{"--json", "pr", "comment", "list", "30", "--limit", "10"}, expectSnippet: `"source": "activities"`},
		{name: "pr comment list aggregate human", args: []string{"pr", "comment", "list", "30", "--limit", "10"}, expectSnippet: "anchored thread comment"},
		{name: "pr comment list trimmed path json", args: []string{"--json", "pr", "comment", "list", "30", "--path", "  seed.txt  ", "--limit", "10"}, expectSnippet: `"path": "seed.txt"`},
		{name: "pr comment list json", args: []string{"--json", "pr", "comment", "list", "30", "--path", "seed.txt", "--limit", "10"}, expectSnippet: `"comments"`},
		{name: "pr comment list human", args: []string{"pr", "comment", "list", "30", "--path", "seed.txt", "--limit", "10"}, expectSnippet: "pr thread comment"},
		{name: "pr comment get json", args: []string{"--json", "pr", "comment", "get", "30", "301"}, expectSnippet: `"comment"`},
		{name: "pr comment get human", args: []string{"pr", "comment", "get", "30", "301"}, expectSnippet: "Reviewer One"},
		{name: "pr activity list json", args: []string{"--json", "pr", "activity", "list", "30", "--limit", "10"}, expectSnippet: `"activities"`},
		{name: "pr activity list human", args: []string{"pr", "activity", "list", "30", "--limit", "10"}, expectSnippet: "[901 COMMENTED]"},
		{name: "pr merge human", args: []string{"pr", "merge", "30", "--version", "1"}, expectSnippet: "Merged pull request #30"},
		{name: "pr review approve human", args: []string{"pr", "review", "approve", "30"}, expectSnippet: "Approved pull request #30"},
		{name: "pr task list json", args: []string{"--json", "pr", "task", "list", "30", "--state", "open"}, expectSnippet: `"tasks"`},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			command := NewRootCommand()
			output := &bytes.Buffer{}
			command.SetOut(output)
			command.SetErr(output)
			command.SetArgs(testCase.args)

			if err := command.Execute(); err != nil {
				t.Fatalf("expected command to succeed, got: %v (output: %s)", err, output.String())
			}

			if !strings.Contains(output.String(), testCase.expectSnippet) {
				t.Fatalf("expected output to contain %q, got: %s", testCase.expectSnippet, output.String())
			}
		})
	}
}

func TestPRCommentListNoCommentsAndMissingRepo(t *testing.T) {
	t.Run("no comments found", func(t *testing.T) {
		t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.Method != http.MethodGet || request.URL.Path != "/rest/api/latest/projects/TEST/repos/demo/pull-requests/30/activities" {
				http.NotFound(writer, request)
				return
			}
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"isLastPage":true,"values":[]}`))
		}))
		defer server.Close()

		t.Setenv("BITBUCKET_URL", server.URL)
		t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
		t.Setenv("BITBUCKET_REPO_SLUG", "demo")

		command := NewRootCommand()
		output := &bytes.Buffer{}
		command.SetOut(output)
		command.SetErr(output)
		command.SetArgs([]string{"pr", "comment", "list", "30"})

		if err := command.Execute(); err != nil {
			t.Fatalf("expected no-comments command to succeed, got: %v", err)
		}
		if !strings.Contains(output.String(), "No comments found") {
			t.Fatalf("expected no comments message, got: %s", output.String())
		}
	})

	t.Run("no activities found", func(t *testing.T) {
		t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.Method != http.MethodGet || request.URL.Path != "/rest/api/latest/projects/TEST/repos/demo/pull-requests/30/activities" {
				http.NotFound(writer, request)
				return
			}
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"isLastPage":true,"values":[]}`))
		}))
		defer server.Close()

		t.Setenv("BITBUCKET_URL", server.URL)
		t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
		t.Setenv("BITBUCKET_REPO_SLUG", "demo")

		command := NewRootCommand()
		output := &bytes.Buffer{}
		command.SetOut(output)
		command.SetErr(output)
		command.SetArgs([]string{"pr", "activity", "list", "30"})

		if err := command.Execute(); err != nil {
			t.Fatalf("expected no-activities command to succeed, got: %v", err)
		}
		if !strings.Contains(output.String(), "No activities found") {
			t.Fatalf("expected no activities message, got: %s", output.String())
		}
	})

	t.Run("missing repo slug", func(t *testing.T) {
		t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
		t.Setenv("BITBUCKET_REPO_SLUG", "")

		command := NewRootCommand()
		output := &bytes.Buffer{}
		command.SetOut(output)
		command.SetErr(output)
		command.SetArgs([]string{"pr", "comment", "list", "30"})

		if err := command.Execute(); err == nil {
			t.Fatal("expected missing repo slug error")
		}
	})
}

func TestPRExtendedLifecycleReviewerAndTaskCommands(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/30/decline":
			if request.URL.Query().Get("version") != "1" {
				writer.WriteHeader(http.StatusConflict)
				_, _ = writer.Write([]byte("expected version query"))
				return
			}
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"id":30,"title":"Feature PR","state":"DECLINED","open":false,"closed":true}`))
		case request.Method == http.MethodPost && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/30/reopen":
			if request.URL.Query().Get("version") != "1" {
				writer.WriteHeader(http.StatusConflict)
				_, _ = writer.Write([]byte("expected version query"))
				return
			}
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"id":30,"title":"Feature PR","state":"OPEN","open":true,"closed":false}`))
		case request.Method == http.MethodDelete && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/30/approve":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"id":30,"title":"Feature PR","state":"OPEN","open":true,"closed":false}`))
		case request.Method == http.MethodPost && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/30/participants":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"id":30,"title":"Feature PR","state":"OPEN","open":true,"closed":false}`))
		case request.Method == http.MethodDelete && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/30/participants/reviewer2":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"id":30,"title":"Feature PR","state":"OPEN","open":true,"closed":false}`))
		case request.Method == http.MethodPost && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/30/tasks":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"id":701,"text":"Task B","state":"OPEN","resolved":false}`))
		case request.Method == http.MethodPut && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/30/tasks/700":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"id":700,"text":"Task A+","state":"RESOLVED","resolved":true}`))
		case request.Method == http.MethodDelete && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/30/tasks/700":
			if request.URL.Query().Get("version") != "2" {
				writer.WriteHeader(http.StatusConflict)
				_, _ = writer.Write([]byte("expected version query"))
				return
			}
			writer.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	testCases := []struct {
		name          string
		args          []string
		expectSnippet string
	}{
		{name: "pr decline", args: []string{"pr", "decline", "30", "--version", "1"}, expectSnippet: "Declined pull request #30"},
		{name: "pr reopen", args: []string{"pr", "reopen", "30", "--version", "1"}, expectSnippet: "Reopened pull request #30"},
		{name: "pr review unapprove", args: []string{"pr", "review", "unapprove", "30"}, expectSnippet: "Removed approval for pull request #30"},
		{name: "pr reviewer add", args: []string{"pr", "review", "reviewer", "add", "30", "--user", "reviewer2"}, expectSnippet: "Added reviewer reviewer2"},
		{name: "pr reviewer remove", args: []string{"pr", "review", "reviewer", "remove", "30", "--user", "reviewer2"}, expectSnippet: "Removed reviewer reviewer2"},
		{name: "pr task create", args: []string{"pr", "task", "create", "30", "--text", "Task B"}, expectSnippet: "Created task 701"},
		{name: "pr task update", args: []string{"pr", "task", "update", "30", "--task", "700", "--text", "Task A+", "--resolved=true", "--version", "2"}, expectSnippet: "Updated task 700"},
		{name: "pr task delete json", args: []string{"--json", "pr", "task", "delete", "30", "--task", "700", "--version", "2"}, expectSnippet: `"status": "ok"`},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			command := NewRootCommand()
			output := &bytes.Buffer{}
			command.SetOut(output)
			command.SetErr(output)
			command.SetArgs(testCase.args)

			if err := command.Execute(); err != nil {
				t.Fatalf("expected command to succeed, got: %v (output: %s)", err, output.String())
			}

			if !strings.Contains(output.String(), testCase.expectSnippet) {
				t.Fatalf("expected output to contain %q, got: %s", testCase.expectSnippet, output.String())
			}
		})
	}
}

func TestResolveRepositorySelector(t *testing.T) {
	t.Run("uses env fallback", func(t *testing.T) {
		t.Setenv("BITBUCKET_REPO_SLUG", "demo")

		repo, err := resolveRepositorySelector("", config.AppConfig{ProjectKey: "TEST"})
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if repo.ProjectKey != "TEST" || repo.Slug != "demo" {
			t.Fatalf("unexpected selector: %+v", repo)
		}
	})

	t.Run("rejects missing values", func(t *testing.T) {
		t.Setenv("BITBUCKET_REPO_SLUG", "")

		_, err := resolveRepositorySelector("", config.AppConfig{})
		if err == nil {
			t.Fatal("expected validation error")
		}
	})

	t.Run("rejects invalid format", func(t *testing.T) {
		_, err := resolveRepositorySelector("badformat", config.AppConfig{})
		if err == nil {
			t.Fatal("expected validation error")
		}
	})

	t.Run("accepts explicit selector", func(t *testing.T) {
		repo, err := resolveRepositorySelector("PRJ/repo", config.AppConfig{})
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if repo.ProjectKey != "PRJ" || repo.Slug != "repo" {
			t.Fatalf("unexpected selector: %+v", repo)
		}
	})
}

func TestApplyInferredRepositoryContext(t *testing.T) {
	originalFactory := gitBackendFactory
	t.Cleanup(func() {
		gitBackendFactory = originalFactory
	})

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "http://bitbucket.local:7990")
	t.Setenv("BITBUCKET_PROJECT_KEY", "ENV")
	t.Setenv("BITBUCKET_REPO_SLUG", "env-repo")

	t.Run("sets inferred repository and host", func(t *testing.T) {
		gitBackendFactory = func() git.Backend {
			return inferenceGitBackendStub{
				repoRoot: "/tmp/repo",
				remotes: []git.Remote{{
					Name: "origin",
					URL:  "https://bitbucket.local:7990/scm/PRJ/demo.git",
				}},
			}
		}

		cmd := &cobra.Command{Use: "branch list"}
		cmd.Flags().String("repo", "", "")
		errBuffer := &bytes.Buffer{}
		cmd.SetErr(errBuffer)

		if err := applyInferredRepositoryContext(cmd, false); err != nil {
			t.Fatalf("apply inferred repository context failed: %v", err)
		}

		if got := os.Getenv("BITBUCKET_PROJECT_KEY"); got != "PRJ" {
			t.Fatalf("expected inferred project key, got %q", got)
		}
		if got := os.Getenv("BITBUCKET_REPO_SLUG"); got != "demo" {
			t.Fatalf("expected inferred repo slug, got %q", got)
		}
		if !strings.Contains(errBuffer.String(), "Using repository context from git remote") {
			t.Fatalf("expected inference notice, got: %s", errBuffer.String())
		}
	})

	t.Run("nil command is ignored", func(t *testing.T) {
		if err := applyInferredRepositoryContext(nil, false); err != nil {
			t.Fatalf("expected nil command to be ignored, got: %v", err)
		}
	})

	t.Run("command without repo flag is ignored", func(t *testing.T) {
		cmd := &cobra.Command{Use: "project list"}
		if err := applyInferredRepositoryContext(cmd, false); err != nil {
			t.Fatalf("expected no error when repo flag is absent, got: %v", err)
		}
	})

	t.Run("explicit repo flag skips inference", func(t *testing.T) {
		gitBackendFactory = func() git.Backend {
			return inferenceGitBackendStub{
				repoRoot: "/tmp/repo",
				remotes:  []git.Remote{{Name: "origin", URL: "https://bitbucket.local:7990/scm/PRJ/demo.git"}},
			}
		}

		t.Setenv("BITBUCKET_PROJECT_KEY", "EXPLICIT")
		t.Setenv("BITBUCKET_REPO_SLUG", "keep")

		cmd := &cobra.Command{Use: "branch list"}
		cmd.Flags().String("repo", "", "")
		if err := cmd.Flags().Set("repo", "OVERRIDE/repo"); err != nil {
			t.Fatalf("set repo flag: %v", err)
		}

		if err := applyInferredRepositoryContext(cmd, false); err != nil {
			t.Fatalf("expected no error for explicit repo, got: %v", err)
		}

		if got := os.Getenv("BITBUCKET_PROJECT_KEY"); got != "EXPLICIT" {
			t.Fatalf("expected project key unchanged, got %q", got)
		}
		if got := os.Getenv("BITBUCKET_REPO_SLUG"); got != "keep" {
			t.Fatalf("expected repo slug unchanged, got %q", got)
		}
	})

	t.Run("ambiguous remotes returns validation error", func(t *testing.T) {
		gitBackendFactory = func() git.Backend {
			return inferenceGitBackendStub{
				repoRoot: "/tmp/repo",
				remotes: []git.Remote{
					{Name: "origin", URL: "https://bitbucket.local:7990/scm/PRJ/demo.git"},
					{Name: "upstream", URL: "https://bitbucket.local:7990/scm/ALT/demo.git"},
				},
			}
		}

		cmd := &cobra.Command{Use: "branch list"}
		cmd.Flags().String("repo", "", "")

		err := applyInferredRepositoryContext(cmd, false)
		if err == nil {
			t.Fatal("expected ambiguity error")
		}
		if apperrors.ExitCode(err) != 2 {
			t.Fatalf("expected validation exit code, got %d (%v)", apperrors.ExitCode(err), err)
		}
	})

	t.Run("non-repository git context is ignored", func(t *testing.T) {
		gitBackendFactory = func() git.Backend {
			return inferenceGitBackendStub{rootErr: errors.New("fatal: not a git repository (or any of the parent directories): .git")}
		}

		cmd := &cobra.Command{Use: "branch list"}
		cmd.Flags().String("repo", "", "")

		if err := applyInferredRepositoryContext(cmd, false); err != nil {
			t.Fatalf("expected non-repository error to be ignored, got: %v", err)
		}
	})

	t.Run("json mode does not emit inference banner", func(t *testing.T) {
		gitBackendFactory = func() git.Backend {
			return inferenceGitBackendStub{
				repoRoot: "/tmp/repo",
				remotes:  []git.Remote{{Name: "origin", URL: "https://bitbucket.local:7990/scm/PRJ/demo.git"}},
			}
		}

		cmd := &cobra.Command{Use: "branch list"}
		cmd.Flags().String("repo", "", "")
		errBuffer := &bytes.Buffer{}
		cmd.SetErr(errBuffer)

		if err := applyInferredRepositoryContext(cmd, true); err != nil {
			t.Fatalf("json inference failed: %v", err)
		}
		if errBuffer.Len() != 0 {
			t.Fatalf("expected no banner output in json mode, got: %q", errBuffer.String())
		}
	})

	t.Run("load config errors are ignored", func(t *testing.T) {
		t.Setenv("BITBUCKET_URL", "://bad-url")
		cmd := &cobra.Command{Use: "branch list"}
		cmd.Flags().String("repo", "", "")

		if err := applyInferredRepositoryContext(cmd, false); err != nil {
			t.Fatalf("expected load config error to be ignored, got: %v", err)
		}
	})
}

func TestPRCommandsUseInferredRepositoryContext(t *testing.T) {
	originalFactory := gitBackendFactory
	t.Cleanup(func() {
		gitBackendFactory = originalFactory
	})

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json;charset=UTF-8")

		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/30":
			_, _ = writer.Write([]byte(`{"id":30,"title":"Feature PR","state":"OPEN","open":true,"closed":false,"fromRef":{"displayId":"feature/demo"},"toRef":{"displayId":"master"},"participants":[{"role":"REVIEWER","status":"UNAPPROVED","approved":false,"user":{"name":"reviewer1"}}]}`))
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/30/merge":
			_, _ = writer.Write([]byte(`{"conflicted":false,"outcome":"CLEAN","vetoes":[]}`))
		case request.Method == http.MethodPost && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/30/approve":
			_, _ = writer.Write([]byte(`{"id":30,"title":"Feature PR","state":"OPEN","open":true,"closed":false,"participants":[{"role":"REVIEWER","status":"APPROVED","approved":true,"user":{"name":"reviewer1"}}]}`))
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/42":
			_, _ = writer.Write([]byte(`{"id":42,"title":"Build PR","state":"OPEN","open":true,"closed":false,"fromRef":{"displayId":"feature/x","latestCommit":"abc123"},"toRef":{"displayId":"main"}}`))
		case request.Method == http.MethodGet && request.URL.Path == "/rest/build-status/latest/commits/abc123":
			_, _ = writer.Write([]byte(`{"values":[{"key":"ci/main","state":"SUCCESSFUL","url":"https://ci.example/1","name":"CI"}],"isLastPage":true}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "")
	t.Setenv("BITBUCKET_REPO_SLUG", "")
	t.Setenv("BITBUCKET_TOKEN", "")
	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("BITBUCKET_PASSWORD", "")
	t.Setenv("ADMIN_USER", "")
	t.Setenv("ADMIN_PASSWORD", "")

	gitBackendFactory = func() git.Backend {
		return inferenceGitBackendStub{
			repoRoot: "/tmp/repo",
			remotes: []git.Remote{{
				Name: "origin",
				URL:  server.URL + "/scm/TEST/demo.git",
			}},
		}
	}

	testCases := []struct {
		name          string
		args          []string
		expectSnippet string
	}{
		{name: "pr get", args: []string{"pr", "get", "30"}, expectSnippet: "Feature PR"},
		{name: "pr review approve", args: []string{"pr", "review", "approve", "30"}, expectSnippet: "Approved pull request #30"},
		{name: "pr build status", args: []string{"pr", "build", "status", "42"}, expectSnippet: "SUCCESSFUL"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			command := NewRootCommand()
			output := &bytes.Buffer{}
			command.SetOut(output)
			command.SetErr(output)
			command.SetArgs(testCase.args)

			if err := command.Execute(); err != nil {
				t.Fatalf("expected command to succeed, got: %v (output: %s)", err, output.String())
			}

			if !strings.Contains(output.String(), "Using repository context from git remote") {
				t.Fatalf("expected inference banner, got: %s", output.String())
			}
			if !strings.Contains(output.String(), testCase.expectSnippet) {
				t.Fatalf("expected output to contain %q, got: %s", testCase.expectSnippet, output.String())
			}
		})
	}
}

func TestPRGetHumanMergeabilityDetailOnlyBlocker(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json;charset=UTF-8")

		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/30":
			_, _ = writer.Write([]byte(`{"id":30,"title":"Feature PR","state":"OPEN","open":true,"closed":false,"fromRef":{"displayId":"feature/demo"},"toRef":{"displayId":"master"}}`))
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/30/merge":
			_, _ = writer.Write([]byte(`{"conflicted":false,"outcome":"UNKNOWN","vetoes":[{"summaryMessage":"","detailedMessage":"detail only blocker"}]}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	command := NewRootCommand()
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs([]string{"pr", "get", "30"})

	if err := command.Execute(); err != nil {
		t.Fatalf("expected pr get to succeed, got: %v (output: %s)", err, output.String())
	}
	if !strings.Contains(output.String(), "- detail only blocker") {
		t.Fatalf("expected detail-only merge blocker in output, got: %s", output.String())
	}
}

func TestInferenceHelperFunctions(t *testing.T) {
	originalFactory := gitBackendFactory
	t.Cleanup(func() {
		gitBackendFactory = originalFactory
	})

	t.Run("parse bitbucket remote supports ssh and https", func(t *testing.T) {
		host, project, slug, ok := parseBitbucketRemote("git@bitbucket.local:scm/PRJ/repo.git")
		if !ok || host != "bitbucket.local" || project != "PRJ" || slug != "repo" {
			t.Fatalf("unexpected ssh remote parse result: ok=%v host=%q project=%q slug=%q", ok, host, project, slug)
		}

		host, project, slug, ok = parseBitbucketRemote("https://bitbucket.local/scm/PRJ/repo.git")
		if !ok || host != "bitbucket.local" || project != "PRJ" || slug != "repo" {
			t.Fatalf("unexpected https remote parse result: ok=%v host=%q project=%q slug=%q", ok, host, project, slug)
		}
	})

	t.Run("parse bitbucket path invalid input", func(t *testing.T) {
		if _, _, ok := parseBitbucketPath("/"); ok {
			t.Fatal("expected invalid path to fail")
		}
	})

	t.Run("normalize host endpoint keeps explicit or default ports", func(t *testing.T) {
		if got := normalizeHostEndpoint(""); got != "" {
			t.Fatalf("expected empty endpoint to normalize to empty, got %q", got)
		}
		if got := normalizeHostEndpoint("https://Bitbucket.Local:7990"); got != "bitbucket.local:7990" {
			t.Fatalf("unexpected normalized host: %q", got)
		}
		if got := normalizeHostEndpoint("https://bitbucket.local"); got != "bitbucket.local:443" {
			t.Fatalf("unexpected normalized https default port: %q", got)
		}
		if got := normalizeHostEndpoint("http://bitbucket.local"); got != "bitbucket.local:80" {
			t.Fatalf("unexpected normalized http default port: %q", got)
		}
		if got := normalizeHostEndpoint("git@bitbucket.local:scm/PRJ/repo.git"); got != "bitbucket.local:22" {
			t.Fatalf("unexpected normalized scp endpoint: %q", got)
		}
		if got := normalizeHostEndpoint("ssh://git@bitbucket.local/scm/PRJ/repo.git"); got != "bitbucket.local:22" {
			t.Fatalf("unexpected normalized ssh endpoint: %q", got)
		}
		if got := normalizeHostEndpoint("bad host value"); got != "" {
			t.Fatalf("expected invalid host to normalize to empty, got %q", got)
		}
		if got := normalizeHostEndpoint("http://[::1"); got != "" {
			t.Fatalf("expected malformed URL to normalize to empty, got %q", got)
		}
	})

	t.Run("non repository errors are recognized", func(t *testing.T) {
		if isNonRepositoryError(nil) {
			t.Fatal("did not expect nil error to match")
		}
		if !isNonRepositoryError(errors.New("not a git repository")) {
			t.Fatal("expected non-repository error to match")
		}
		if isNonRepositoryError(errors.New("permission denied")) {
			t.Fatal("did not expect unrelated error to match")
		}
	})

	t.Run("authenticated host lookup includes runtime and stored profiles", func(t *testing.T) {
		lookup := authenticatedHostLookup(
			config.AppConfig{BitbucketURL: "http://runtime.local:7990"},
			config.StoredConfig{Hosts: map[string]config.StoredProfile{
				"blank":                    {URL: ""},
				"malformed":                {URL: "http://[::1"},
				"http://stored.local:7990": {URL: "http://stored.local:7990", Aliases: []string{"git.stored.local:7999"}},
			}},
		)

		if lookup["runtime.local:7990"] == "" {
			t.Fatal("expected runtime host in lookup")
		}
		if lookup["stored.local:7990"] == "" {
			t.Fatal("expected stored host in lookup")
		}
		if lookup["git.stored.local:7999"] == "" {
			t.Fatal("expected stored alias in lookup")
		}
	})

	t.Run("normalize host endpoint loose collapses default web ports", func(t *testing.T) {
		if got := normalizeHostEndpointLoose("https://bitbucket.local"); got != "bitbucket.local" {
			t.Fatalf("unexpected https loose endpoint: %q", got)
		}
		if got := normalizeHostEndpointLoose("http://bitbucket.local"); got != "bitbucket.local" {
			t.Fatalf("unexpected http loose endpoint: %q", got)
		}
		if got := normalizeHostEndpointLoose("ssh://git@bitbucket.local:7999/scm/PRJ/repo.git"); got != "bitbucket.local:7999" {
			t.Fatalf("unexpected ssh loose endpoint: %q", got)
		}
	})

	t.Run("normalize remote endpoint supports scp and https forms", func(t *testing.T) {
		if got := normalizeRemoteEndpoint(""); got != "" {
			t.Fatalf("expected empty remote endpoint to normalize to empty, got %q", got)
		}
		if got := normalizeRemoteEndpoint("git@bitbucket.local:scm/PRJ/repo.git"); got != "bitbucket.local:22" {
			t.Fatalf("unexpected scp remote endpoint: %q", got)
		}
		if got := normalizeRemoteEndpoint("https://bitbucket.local/scm/PRJ/repo.git"); got != "bitbucket.local" {
			t.Fatalf("unexpected https remote endpoint: %q", got)
		}
		if got := normalizeRemoteEndpoint("://bad"); got != "" {
			t.Fatalf("expected invalid remote endpoint to normalize to empty, got %q", got)
		}
	})

	t.Run("normalize host endpoint loose handles invalid scp forms", func(t *testing.T) {
		if got := normalizeHostEndpointLoose(""); got != "" {
			t.Fatalf("expected empty loose endpoint to normalize to empty, got %q", got)
		}
		if got := normalizeHostEndpointLoose("git@bitbucket.local"); got != "" {
			t.Fatalf("expected invalid scp endpoint to normalize to empty, got %q", got)
		}
		if got := normalizeHostEndpointLoose("https://bitbucket.local:7990"); got != "bitbucket.local:7990" {
			t.Fatalf("unexpected explicit port endpoint: %q", got)
		}
		if got := normalizeHostEndpointLoose("://bad"); got != "" {
			t.Fatalf("expected invalid endpoint to normalize to empty, got %q", got)
		}
	})

	t.Run("parse bitbucket remote malformed forms", func(t *testing.T) {
		if _, _, _, ok := parseBitbucketRemote("https://%zz"); ok {
			t.Fatal("expected malformed https remote to fail")
		}
		if _, _, _, ok := parseBitbucketRemote("git@bitbucket.local"); ok {
			t.Fatal("expected ssh remote without colon to fail")
		}
		if _, _, _, ok := parseBitbucketRemote("git@bitbucket.local:"); ok {
			t.Fatal("expected ssh remote with empty path to fail")
		}
	})

	t.Run("parse bitbucket path single segment fails", func(t *testing.T) {
		if _, _, ok := parseBitbucketPath("repo"); ok {
			t.Fatal("expected single-segment path parse to fail")
		}
	})

	t.Run("infer context ignores remotes without authenticated host match", func(t *testing.T) {
		gitBackendFactory = func() git.Backend {
			return inferenceGitBackendStub{
				repoRoot: "/tmp/repo",
				remotes:  []git.Remote{{Name: "origin", URL: "https://other-host.local/scm/PRJ/repo.git"}},
			}
		}

		inferred, err := inferRepositoryContextFromGit(config.AppConfig{BitbucketURL: "https://bitbucket.local:7990"})
		if err != nil {
			t.Fatalf("infer context failed: %v", err)
		}
		if inferred != nil {
			t.Fatalf("expected nil inferred context for unmatched host, got %+v", inferred)
		}
	})

	t.Run("infer context ignores non-repository errors from remotes", func(t *testing.T) {
		gitBackendFactory = func() git.Backend {
			return inferenceGitBackendStub{
				repoRoot: "/tmp/repo",
				listErr:  errors.New("fatal: not a git repository"),
			}
		}

		inferred, err := inferRepositoryContextFromGit(config.AppConfig{BitbucketURL: "https://bitbucket.local:7990"})
		if err != nil {
			t.Fatalf("expected nil error for non-repository remotes listing, got: %v", err)
		}
		if inferred != nil {
			t.Fatalf("expected nil inferred context, got %+v", inferred)
		}
	})

	t.Run("infer context returns backend errors", func(t *testing.T) {
		gitBackendFactory = func() git.Backend {
			return inferenceGitBackendStub{rootErr: errors.New("backend failure")}
		}

		_, err := inferRepositoryContextFromGit(config.AppConfig{BitbucketURL: "https://bitbucket.local:7990"})
		if err == nil {
			t.Fatal("expected backend error to be returned")
		}
	})

	t.Run("infer context with nil backend returns nil", func(t *testing.T) {
		gitBackendFactory = func() git.Backend { return nil }

		inferred, err := inferRepositoryContextFromGit(config.AppConfig{BitbucketURL: "https://bitbucket.local:7990"})
		if err != nil {
			t.Fatalf("expected nil error, got: %v", err)
		}
		if inferred != nil {
			t.Fatalf("expected nil inferred context, got %+v", inferred)
		}
	})

	t.Run("infer context with no remotes returns nil", func(t *testing.T) {
		gitBackendFactory = func() git.Backend {
			return inferenceGitBackendStub{repoRoot: "/tmp/repo", remotes: nil}
		}

		inferred, err := inferRepositoryContextFromGit(config.AppConfig{BitbucketURL: "https://bitbucket.local:7990"})
		if err != nil {
			t.Fatalf("infer context failed: %v", err)
		}
		if inferred != nil {
			t.Fatalf("expected nil inferred context, got %+v", inferred)
		}
	})

	t.Run("infer context with no authenticated hosts returns nil", func(t *testing.T) {
		t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
		t.Setenv("BITBUCKET_URL", "")
		gitBackendFactory = func() git.Backend {
			return inferenceGitBackendStub{repoRoot: "/tmp/repo", remotes: []git.Remote{{Name: "origin", URL: "https://bitbucket.local/scm/PRJ/repo.git"}}}
		}

		inferred, err := inferRepositoryContextFromGit(config.AppConfig{})
		if err != nil {
			t.Fatalf("infer context failed: %v", err)
		}
		if inferred != nil {
			t.Fatalf("expected nil inferred context, got %+v", inferred)
		}
	})

	t.Run("infer context returns nil when working directory cannot be resolved", func(t *testing.T) {
		gitBackendFactory = func() git.Backend {
			return inferenceGitBackendStub{repoRoot: "/tmp/repo", remotes: []git.Remote{{Name: "origin", URL: "https://bitbucket.local/scm/PRJ/repo.git"}}}
		}

		originalDirectory, err := os.Getwd()
		if err != nil {
			t.Fatalf("getwd failed: %v", err)
		}
		badDirectory := t.TempDir()
		if err := os.Chdir(badDirectory); err != nil {
			t.Fatalf("chdir failed: %v", err)
		}
		if err := os.RemoveAll(badDirectory); err != nil {
			t.Fatalf("remove temp directory failed: %v", err)
		}
		t.Cleanup(func() { _ = os.Chdir(originalDirectory) })

		inferred, err := inferRepositoryContextFromGit(config.AppConfig{BitbucketURL: "https://bitbucket.local:7990"})
		if err != nil {
			t.Fatalf("expected nil error when getwd fails, got: %v", err)
		}
		if inferred != nil {
			t.Fatalf("expected nil inferred context when getwd fails, got %+v", inferred)
		}
	})

	t.Run("infer context returns remote listing errors", func(t *testing.T) {
		gitBackendFactory = func() git.Backend {
			return inferenceGitBackendStub{repoRoot: "/tmp/repo", listErr: errors.New("remote listing failed")}
		}

		_, err := inferRepositoryContextFromGit(config.AppConfig{BitbucketURL: "https://bitbucket.local:7990"})
		if err == nil {
			t.Fatal("expected remote listing error")
		}
	})

	t.Run("infer context skips invalid remote entries", func(t *testing.T) {
		gitBackendFactory = func() git.Backend {
			return inferenceGitBackendStub{
				repoRoot: "/tmp/repo",
				remotes: []git.Remote{
					{Name: "invalid", URL: "not-a-remote"},
					{Name: "origin", URL: "https://bitbucket.local:7990/scm/PRJ/repo.git"},
				},
			}
		}

		inferred, err := inferRepositoryContextFromGit(config.AppConfig{BitbucketURL: "https://bitbucket.local:7990"})
		if err != nil {
			t.Fatalf("infer context failed: %v", err)
		}
		if inferred == nil || inferred.ProjectKey != "PRJ" {
			t.Fatalf("expected valid remote to be selected, got %+v", inferred)
		}
	})

	t.Run("infer context ambiguity sorts by remote and project details", func(t *testing.T) {
		gitBackendFactory = func() git.Backend {
			return inferenceGitBackendStub{
				repoRoot: "/tmp/repo",
				remotes: []git.Remote{
					{Name: "alpha", URL: "https://bitbucket.local:7990/scm/PRJ/b.git"},
					{Name: "alpha", URL: "https://bitbucket.local:7990/scm/PRJ/a.git"},
					{Name: "beta", URL: "https://bitbucket.local:7990/scm/ZZZ/z.git"},
				},
			}
		}

		_, err := inferRepositoryContextFromGit(config.AppConfig{BitbucketURL: "https://bitbucket.local:7990"})
		if err == nil {
			t.Fatal("expected ambiguity error")
		}
		if !strings.Contains(err.Error(), "ambiguous git remote context") {
			t.Fatalf("expected ambiguity guidance, got: %v", err)
		}
	})

	t.Run("infer context matches stored alias remote to canonical host", func(t *testing.T) {
		configPath := filepath.Join(t.TempDir(), "bb", "config.yaml")
		t.Setenv("BB_CONFIG_PATH", configPath)
		t.Setenv("BB_DISABLE_STORED_CONFIG", "")
		if _, err := config.SaveLogin(config.LoginInput{Host: "https://bitbucket.local", Token: "tok", SetDefault: true}); err != nil {
			t.Fatalf("save login failed: %v", err)
		}
		if _, err := config.SetHostAliases("https://bitbucket.local", []string{"git.bitbucket.local:7999"}); err != nil {
			t.Fatalf("set aliases failed: %v", err)
		}

		gitBackendFactory = func() git.Backend {
			return inferenceGitBackendStub{
				repoRoot: "/tmp/repo",
				remotes:  []git.Remote{{Name: "origin", URL: "ssh://git@git.bitbucket.local:7999/scm/PRJ/repo.git"}},
			}
		}

		inferred, err := inferRepositoryContextFromGit(config.AppConfig{})
		if err != nil {
			t.Fatalf("infer context failed: %v", err)
		}
		if inferred == nil {
			t.Fatal("expected inferred context from alias remote")
		}
		if inferred.Host != "https://bitbucket.local" || inferred.ProjectKey != "PRJ" || inferred.Slug != "repo" {
			t.Fatalf("unexpected inferred context: %+v", inferred)
		}
	})

	t.Run("parse bitbucket remote invalid input", func(t *testing.T) {
		if _, _, _, ok := parseBitbucketRemote("not-a-remote"); ok {
			t.Fatal("expected invalid remote parsing to fail")
		}
	})

	t.Run("parse bitbucket path fallback project slash repo", func(t *testing.T) {
		project, slug, ok := parseBitbucketPath("PRJ/repo.git")
		if !ok || project != "PRJ" || slug != "repo" {
			t.Fatalf("unexpected fallback parse result: ok=%v project=%q slug=%q", ok, project, slug)
		}
	})
}

func TestRootCommandPreRunPropagatesInferenceErrors(t *testing.T) {
	originalFactory := gitBackendFactory
	t.Cleanup(func() {
		gitBackendFactory = originalFactory
	})

	gitBackendFactory = func() git.Backend {
		return inferenceGitBackendStub{
			repoRoot: "/tmp/repo",
			remotes: []git.Remote{
				{Name: "origin", URL: "https://bitbucket.local:7990/scm/PRJ/demo.git"},
				{Name: "upstream", URL: "https://bitbucket.local:7990/scm/ALT/demo.git"},
			},
		}
	}

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "https://bitbucket.local:7990")
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	command := NewRootCommand()
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{"branch", "list"})

	err := command.Execute()
	if err == nil {
		t.Fatal("expected inference ambiguity error")
	}
	if apperrors.ExitCode(err) != 2 {
		t.Fatalf("expected validation exit code, got %d (%v)", apperrors.ExitCode(err), err)
	}
}

func TestLoadConfigAndClientAndClientFactoryBranches(t *testing.T) {
	t.Run("load config failure", func(t *testing.T) {
		t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
		t.Setenv("BITBUCKET_URL", "://broken")
		t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")

		_, _, err := loadConfigAndClient()
		if err == nil {
			t.Fatal("expected config load failure")
		}
	})

	t.Run("client factory success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client, err := newAPIClientFromConfig(config.AppConfig{BitbucketURL: server.URL})
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client")
		}
	})
}

func TestLoadQualityRepoAndServiceBranches(t *testing.T) {
	t.Run("config load failure", func(t *testing.T) {
		t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
		t.Setenv("BITBUCKET_URL", "://broken")
		t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")

		_, _, err := loadQualityRepoAndService("")
		if err == nil {
			t.Fatal("expected config load failure")
		}
	})

	t.Run("invalid selector failure", func(t *testing.T) {
		t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
		t.Setenv("BITBUCKET_URL", "http://localhost:7990")
		t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
		t.Setenv("BITBUCKET_REPO_SLUG", "demo")

		_, _, err := loadQualityRepoAndService("bad-format")
		if err == nil {
			t.Fatal("expected repository selector validation error")
		}
		if apperrors.ExitCode(err) != 2 {
			t.Fatalf("expected validation exit code 2, got %d (%v)", apperrors.ExitCode(err), err)
		}
	})

	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
		t.Setenv("BITBUCKET_URL", server.URL)
		t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
		t.Setenv("BITBUCKET_REPO_SLUG", "demo")

		repo, service, err := loadQualityRepoAndService("")
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if repo.ProjectKey != "TEST" || repo.Slug != "demo" {
			t.Fatalf("unexpected repository ref: %+v", repo)
		}
		if service == nil {
			t.Fatal("expected non-nil service")
		}
	})
}

func TestResolveCommentTargetRequiresExactlyOneContext(t *testing.T) {
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")
	cfg := config.AppConfig{ProjectKey: "TEST"}

	_, err := resolveCommentTarget("", "", "", cfg)
	if err == nil {
		t.Fatal("expected validation error for missing commit/pr")
	}

	_, err = resolveCommentTarget("", "abc123", "77", cfg)
	if err == nil {
		t.Fatal("expected validation error for both commit and pr")
	}

	target, err := resolveCommentTarget("", "abc123", "", cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if target.CommitID != "abc123" || target.PullRequestID != "" {
		t.Fatalf("unexpected target: %+v", target)
	}

	target, err = resolveCommentTarget("", "", " 77 ", cfg)
	if err != nil {
		t.Fatalf("expected no error for pull request target, got: %v", err)
	}
	if target.CommitID != "" || target.PullRequestID != "77" {
		t.Fatalf("unexpected pull request target: %+v", target)
	}
}

func TestResolveDiffOutputModeAndWriters(t *testing.T) {
	_, err := resolveDiffOutputMode(true, true, false)
	if err == nil {
		t.Fatal("expected validation error for multiple output modes")
	}

	mode, err := resolveDiffOutputMode(false, false, true)
	if err != nil || mode != diff.OutputKindNameOnly {
		t.Fatalf("expected name-only mode, got mode=%q err=%v", mode, err)
	}

	result := diff.Result{
		Names: []string{"a.txt", "b.go"},
		Stats: []map[string]any{{"path": "a.txt", "lines_added": 1, "lines_removed": 2}},
		Patch: "diff --git a/a.txt b/a.txt",
	}

	nameBuffer := &bytes.Buffer{}
	if err := writeDiffResult(nameBuffer, false, diff.OutputKindNameOnly, result); err != nil {
		t.Fatalf("expected no error writing names, got: %v", err)
	}
	if !strings.Contains(nameBuffer.String(), "a.txt") {
		t.Fatalf("expected name output, got: %s", nameBuffer.String())
	}

	statBuffer := &bytes.Buffer{}
	if err := writeDiffResult(statBuffer, true, diff.OutputKindStat, result); err != nil {
		t.Fatalf("expected no error writing stats json, got: %v", err)
	}
	if !strings.Contains(statBuffer.String(), "lines_added") {
		t.Fatalf("expected stats json output, got: %s", statBuffer.String())
	}

	patchBuffer := &bytes.Buffer{}
	if err := writeDiffResult(patchBuffer, false, diff.OutputKindPatch, result); err != nil {
		t.Fatalf("expected no error writing patch, got: %v", err)
	}
	if !strings.Contains(patchBuffer.String(), "diff --git") {
		t.Fatalf("expected patch output, got: %s", patchBuffer.String())
	}

	rawMode, err := resolveDiffOutputMode(false, false, false)
	if err != nil || rawMode != diff.OutputKindRaw {
		t.Fatalf("expected raw mode, got mode=%q err=%v", rawMode, err)
	}

	statPlainBuffer := &bytes.Buffer{}
	if err := writeDiffResult(statPlainBuffer, false, diff.OutputKindStat, result); err != nil {
		t.Fatalf("expected no error writing stats plain mode, got: %v", err)
	}
	if !strings.Contains(statPlainBuffer.String(), "lines_removed") {
		t.Fatalf("expected stats plain output, got: %s", statPlainBuffer.String())
	}

	rawJSONBuffer := &bytes.Buffer{}
	if err := writeDiffResult(rawJSONBuffer, true, diff.OutputKindRaw, result); err != nil {
		t.Fatalf("expected no error writing raw json, got: %v", err)
	}
	if !strings.Contains(rawJSONBuffer.String(), `"patch": "diff --git a/a.txt b/a.txt"`) {
		t.Fatalf("expected raw json patch output, got: %s", rawJSONBuffer.String())
	}
}

func TestCommentHelpersAndSafeHelpers(t *testing.T) {
	comment := openapigenerated.RestComment{}
	if commentIDString(comment) != "unknown" {
		t.Fatalf("expected unknown id")
	}
	if formatCommentSummary(comment) != "[unknown v?] <empty>" {
		t.Fatalf("unexpected summary for empty comment: %s", formatCommentSummary(comment))
	}

	id := int64(42)
	version := int32(3)
	text := " hello "
	comment = openapigenerated.RestComment{Id: &id, Version: &version, Text: &text}
	if commentIDString(comment) != "42" {
		t.Fatalf("expected comment id 42")
	}
	if formatCommentSummary(comment) != "[42 v3] hello" {
		t.Fatalf("unexpected comment summary: %s", formatCommentSummary(comment))
	}

	if safeString(nil) != "" {
		t.Fatal("expected empty safe string")
	}
	if safeInt32(nil) != 0 {
		t.Fatal("expected zero safe int32")
	}
	if safeInt64(nil) != 0 {
		t.Fatal("expected zero safe int64")
	}
	if len(safeStringSlice(nil)) != 0 {
		t.Fatal("expected empty safe string slice")
	}

	s := "x"
	i32 := int32(9)
	i64 := int64(10)
	if safeString(&s) != "x" || safeInt32(&i32) != 9 || safeInt64(&i64) != 10 {
		t.Fatal("expected pointer helper values")
	}

	tagType := openapigenerated.RestTagTypeTAG
	buildState := openapigenerated.RestBuildStatusStateSUCCESSFUL
	insight := openapigenerated.PASS
	if safeStringFromTagType(&tagType) != "TAG" {
		t.Fatal("unexpected tag type conversion")
	}
	if safeStringFromBuildState(&buildState) != "SUCCESSFUL" {
		t.Fatal("unexpected build state conversion")
	}
	if safeStringFromInsightResult(&insight) != "PASS" {
		t.Fatal("unexpected insight result conversion")
	}

	if safeStringFromTagType(nil) != "" {
		t.Fatal("expected empty string for nil tag type")
	}
	if safeStringFromBuildState(nil) != "" {
		t.Fatal("expected empty string for nil build state")
	}
	if safeStringFromInsightResult(nil) != "" {
		t.Fatal("expected empty string for nil insight result")
	}

	var detailedComment openapigenerated.RestComment
	if err := json.Unmarshal([]byte(`{"id":42,"version":3,"text":" hello ","state":"OPEN","author":{"name":"octocat","displayName":"Octo Cat"},"anchor":{"path":{"parent":"src","name":"main.go"}}}`), &detailedComment); err != nil {
		t.Fatalf("unmarshal detailed comment: %v", err)
	}
	detail := formatCommentDetail(detailedComment)
	if !strings.Contains(detail, "Path: src/main.go") || !strings.Contains(detail, "Author: Octo Cat") || !strings.Contains(detail, "State: OPEN") {
		t.Fatalf("unexpected comment detail output: %s", detail)
	}
	if commentAnchorPath(detailedComment) != "src/main.go" {
		t.Fatalf("unexpected anchor path: %s", commentAnchorPath(detailedComment))
	}
	if commentAuthorName(detailedComment) != "Octo Cat" {
		t.Fatalf("unexpected author display name: %s", commentAuthorName(detailedComment))
	}

	detailedComment = openapigenerated.RestComment{}
	if err := json.Unmarshal([]byte(`{"author":{"name":"octocat"},"anchor":{"path":{"name":"main.go"}}}`), &detailedComment); err != nil {
		t.Fatalf("unmarshal fallback comment: %v", err)
	}
	if commentAuthorName(detailedComment) != "octocat" {
		t.Fatalf("expected fallback author name, got: %s", commentAuthorName(detailedComment))
	}
	if commentAnchorPath(detailedComment) != "main.go" {
		t.Fatalf("expected filename anchor path, got: %s", commentAnchorPath(detailedComment))
	}

	detailedComment = openapigenerated.RestComment{}
	if commentAuthorName(detailedComment) != "" {
		t.Fatalf("expected empty author name for nil author, got: %s", commentAuthorName(detailedComment))
	}
	if commentAnchorPath(detailedComment) != "" {
		t.Fatalf("expected empty anchor path for nil anchor, got: %s", commentAnchorPath(detailedComment))
	}
	if err := json.Unmarshal([]byte(`{"anchor":{"path":{"parent":"src","name":""}}}`), &detailedComment); err != nil {
		t.Fatalf("unmarshal empty-name comment: %v", err)
	}
	if commentAnchorPath(detailedComment) != "src" {
		t.Fatalf("expected parent-only anchor path, got: %s", commentAnchorPath(detailedComment))
	}
	if err := json.Unmarshal([]byte(`{"anchor":{"path":{"parent":"","name":"main.go"}}}`), &detailedComment); err != nil {
		t.Fatalf("unmarshal empty-parent comment: %v", err)
	}
	if commentAnchorPath(detailedComment) != "main.go" {
		t.Fatalf("expected child-only anchor path, got: %s", commentAnchorPath(detailedComment))
	}

	activity := pullrequestactivityservice.Activity{ID: 77, Comment: &comment}
	if !strings.Contains(formatPullRequestActivitySummary(activity), "UNKNOWN") {
		t.Fatalf("expected UNKNOWN action summary, got: %s", formatPullRequestActivitySummary(activity))
	}
	activity.Comment = nil
	if formatPullRequestActivitySummary(activity) != "[77 UNKNOWN]" {
		t.Fatalf("unexpected non-comment activity summary: %s", formatPullRequestActivitySummary(activity))
	}
}

func TestWriteJSONMarshalError(t *testing.T) {
	err := writeJSON(&bytes.Buffer{}, map[string]any{"bad": func() {}})
	if err == nil {
		t.Fatal("expected marshal error")
	}
}

func TestTagViewDeleteAndListCommandPaths(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/tags":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"values":[{"displayId":"v2.0.0","type":"TAG","latestCommit":"abc"}],"isLastPage":true}`))
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/tags/v2.0.0":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"displayId":"v2.0.0","type":"TAG","latestCommit":"abc"}`))
		case request.Method == http.MethodDelete && request.URL.Path == "/rest/git/latest/projects/TEST/repos/demo/tags/v2.0.0":
			writer.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	humanListCommand := NewRootCommand()
	humanListBuffer := &bytes.Buffer{}
	humanListCommand.SetOut(humanListBuffer)
	humanListCommand.SetErr(humanListBuffer)
	humanListCommand.SetArgs([]string{"tag", "list", "--limit", "10", "--order-by", "ALPHABETICAL"})
	if err := humanListCommand.Execute(); err != nil {
		t.Fatalf("tag list human failed: %v", err)
	}
	if !strings.Contains(humanListBuffer.String(), "v2.0.0") {
		t.Fatalf("expected tag in human list output, got: %s", humanListBuffer.String())
	}

	jsonViewCommand := NewRootCommand()
	jsonViewBuffer := &bytes.Buffer{}
	jsonViewCommand.SetOut(jsonViewBuffer)
	jsonViewCommand.SetErr(jsonViewBuffer)
	jsonViewCommand.SetArgs([]string{"--json", "tag", "view", "v2.0.0"})
	if err := jsonViewCommand.Execute(); err != nil {
		t.Fatalf("tag view json failed: %v", err)
	}
	if !strings.Contains(jsonViewBuffer.String(), "v2.0.0") {
		t.Fatalf("expected tag id in view output, got: %s", jsonViewBuffer.String())
	}

	jsonDeleteCommand := NewRootCommand()
	jsonDeleteBuffer := &bytes.Buffer{}
	jsonDeleteCommand.SetOut(jsonDeleteBuffer)
	jsonDeleteCommand.SetErr(jsonDeleteBuffer)
	jsonDeleteCommand.SetArgs([]string{"--json", "tag", "delete", "v2.0.0"})
	if err := jsonDeleteCommand.Execute(); err != nil {
		t.Fatalf("tag delete json failed: %v", err)
	}
	if !strings.Contains(jsonDeleteBuffer.String(), `"status": "ok"`) {
		t.Fatalf("expected delete ok status, got: %s", jsonDeleteBuffer.String())
	}
}

func TestBranchCommandPaths(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/branches":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"values":[{"displayId":"main","id":"refs/heads/main","latestCommit":"abc","default":true}],"isLastPage":true}`))
		case request.Method == http.MethodPost && request.URL.Path == "/rest/branch-utils/latest/projects/TEST/repos/demo/branches":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"displayId":"feature/demo","id":"refs/heads/feature/demo"}`))
		case request.Method == http.MethodDelete && request.URL.Path == "/rest/branch-utils/latest/projects/TEST/repos/demo/branches":
			writer.WriteHeader(http.StatusNoContent)
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/default-branch":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"displayId":"main","id":"refs/heads/main"}`))
		case request.Method == http.MethodPut && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/default-branch":
			writer.WriteHeader(http.StatusNoContent)
		case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/rest/branch-utils/latest/projects/TEST/repos/demo/branches/info/"):
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"values":[{"displayId":"main","id":"refs/heads/main"}],"isLastPage":true}`))
		case request.Method == http.MethodGet && request.URL.Path == "/rest/branch-permissions/latest/projects/TEST/repos/demo/restrictions":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"values":[{"id":12,"type":"read-only","matcher":{"id":"refs/heads/main"},"users":[{"name":"alice"}],"groups":["devs"]}],"isLastPage":true}`))
		case request.Method == http.MethodPost && request.URL.Path == "/rest/branch-permissions/latest/projects/TEST/repos/demo/restrictions":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`[{"id":12,"type":"read-only"}]`))
		case request.Method == http.MethodPut && request.URL.Path == "/rest/branch-permissions/latest/projects/TEST/repos/demo/restrictions/12":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"id":12,"type":"read-only"}`))
		case request.Method == http.MethodGet && request.URL.Path == "/rest/branch-permissions/latest/projects/TEST/repos/demo/restrictions/12":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"id":12,"type":"read-only"}`))
		case request.Method == http.MethodDelete && request.URL.Path == "/rest/branch-permissions/latest/projects/TEST/repos/demo/restrictions/12":
			writer.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "branch list", args: []string{"branch", "list"}, want: "main"},
		{name: "branch list with details", args: []string{"branch", "list", "--details"}, want: "main"},
		{name: "branch create", args: []string{"branch", "create", "feature/demo", "--start-point", "abc"}, want: "Created branch"},
		{name: "branch delete", args: []string{"branch", "delete", "feature/demo"}, want: "Deleted branch"},
		{name: "branch default get", args: []string{"branch", "default", "get"}, want: "refs/heads/main"},
		{name: "branch default set", args: []string{"branch", "default", "set", "develop"}, want: "Default branch set to develop"},
		{name: "branch model inspect", args: []string{"branch", "model", "inspect", "abc"}, want: "refs/heads/main"},
		{name: "branch model update", args: []string{"branch", "model", "update", "develop"}, want: "Branch model default updated to develop"},
		{name: "branch restriction list", args: []string{"branch", "restriction", "list"}, want: "read-only"},
		{name: "branch restriction list filtered", args: []string{"branch", "restriction", "list", "--type", "read-only", "--matcher-type", "BRANCH", "--matcher-id", "refs/heads/main"}, want: "users=1"},
		{name: "branch restriction get", args: []string{"branch", "restriction", "get", "12"}, want: "id=12"},
		{name: "branch restriction update", args: []string{"branch", "restriction", "update", "12", "--type", "read-only", "--matcher-id", "refs/heads/main"}, want: "Updated restriction 12"},
		{name: "branch restriction delete", args: []string{"branch", "restriction", "delete", "12"}, want: "Deleted restriction 12"},
	}

	for _, testCase := range tests {
		command := NewRootCommand()
		buffer := &bytes.Buffer{}
		command.SetOut(buffer)
		command.SetErr(buffer)
		command.SetArgs(testCase.args)

		if err := command.Execute(); err != nil {
			t.Fatalf("%s failed: %v", testCase.name, err)
		}
		if !strings.Contains(buffer.String(), testCase.want) {
			t.Fatalf("%s expected %q in output, got: %s", testCase.name, testCase.want, buffer.String())
		}
	}

	jsonCommand := NewRootCommand()
	jsonBuffer := &bytes.Buffer{}
	jsonCommand.SetOut(jsonBuffer)
	jsonCommand.SetErr(jsonBuffer)
	jsonCommand.SetArgs([]string{"--json", "branch", "restriction", "create", "--type", "read-only", "--matcher-id", "refs/heads/main"})
	if err := jsonCommand.Execute(); err != nil {
		t.Fatalf("branch restriction create json failed: %v", err)
	}
	if !strings.Contains(jsonBuffer.String(), `"id": 12`) {
		t.Fatalf("expected restriction id in json output, got: %s", jsonBuffer.String())
	}

	jsonDeleteDryRunCommand := NewRootCommand()
	jsonDeleteDryRunBuffer := &bytes.Buffer{}
	jsonDeleteDryRunCommand.SetOut(jsonDeleteDryRunBuffer)
	jsonDeleteDryRunCommand.SetErr(jsonDeleteDryRunBuffer)
	jsonDeleteDryRunCommand.SetArgs([]string{"--json", "branch", "delete", "feature/demo", "--dry-run"})
	if err := jsonDeleteDryRunCommand.Execute(); err != nil {
		t.Fatalf("branch delete dry-run json failed: %v", err)
	}
	if !strings.Contains(jsonDeleteDryRunBuffer.String(), `"planning_mode": "stateful"`) {
		t.Fatalf("expected stateful planning mode in output, got: %s", jsonDeleteDryRunBuffer.String())
	}
	if !strings.Contains(jsonDeleteDryRunBuffer.String(), `"intent": "branch.delete"`) {
		t.Fatalf("expected branch.delete intent in output, got: %s", jsonDeleteDryRunBuffer.String())
	}

	jsonDeleteDryRunEndpointCommand := NewRootCommand()
	jsonDeleteDryRunEndpointBuffer := &bytes.Buffer{}
	jsonDeleteDryRunEndpointCommand.SetOut(jsonDeleteDryRunEndpointBuffer)
	jsonDeleteDryRunEndpointCommand.SetErr(jsonDeleteDryRunEndpointBuffer)
	jsonDeleteDryRunEndpointCommand.SetArgs([]string{"--json", "--dry-run", "branch", "delete", "feature/demo", "--end-point", "abc123"})
	if err := jsonDeleteDryRunEndpointCommand.Execute(); err != nil {
		t.Fatalf("branch delete dry-run with endpoint json failed: %v", err)
	}
	if !strings.Contains(jsonDeleteDryRunEndpointBuffer.String(), `"end_point": "abc123"`) {
		t.Fatalf("expected end_point in output, got: %s", jsonDeleteDryRunEndpointBuffer.String())
	}
	if !strings.Contains(jsonDeleteDryRunEndpointBuffer.String(), `end-point precondition`) {
		t.Fatalf("expected endpoint reason in output, got: %s", jsonDeleteDryRunEndpointBuffer.String())
	}

	jsonDefaultSetCommand := NewRootCommand()
	jsonDefaultSetBuffer := &bytes.Buffer{}
	jsonDefaultSetCommand.SetOut(jsonDefaultSetBuffer)
	jsonDefaultSetCommand.SetErr(jsonDefaultSetBuffer)
	jsonDefaultSetCommand.SetArgs([]string{"--json", "branch", "default", "set", "develop"})
	if err := jsonDefaultSetCommand.Execute(); err != nil {
		t.Fatalf("branch default set json failed: %v", err)
	}
	if !strings.Contains(jsonDefaultSetBuffer.String(), `"status": "ok"`) {
		t.Fatalf("expected status ok in output, got: %s", jsonDefaultSetBuffer.String())
	}

	jsonModelUpdateCommand := NewRootCommand()
	jsonModelUpdateBuffer := &bytes.Buffer{}
	jsonModelUpdateCommand.SetOut(jsonModelUpdateBuffer)
	jsonModelUpdateCommand.SetErr(jsonModelUpdateBuffer)
	jsonModelUpdateCommand.SetArgs([]string{"--json", "branch", "model", "update", "develop"})
	if err := jsonModelUpdateCommand.Execute(); err != nil {
		t.Fatalf("branch model update json failed: %v", err)
	}
	if !strings.Contains(jsonModelUpdateBuffer.String(), `"status": "ok"`) {
		t.Fatalf("expected status ok in model update output, got: %s", jsonModelUpdateBuffer.String())
	}

	jsonRestrictionGetCommand := NewRootCommand()
	jsonRestrictionGetBuffer := &bytes.Buffer{}
	jsonRestrictionGetCommand.SetOut(jsonRestrictionGetBuffer)
	jsonRestrictionGetCommand.SetErr(jsonRestrictionGetBuffer)
	jsonRestrictionGetCommand.SetArgs([]string{"--json", "branch", "restriction", "get", "12"})
	if err := jsonRestrictionGetCommand.Execute(); err != nil {
		t.Fatalf("branch restriction get json failed: %v", err)
	}
	if !strings.Contains(jsonRestrictionGetBuffer.String(), `"restriction"`) {
		t.Fatalf("expected restriction payload in output, got: %s", jsonRestrictionGetBuffer.String())
	}

	jsonRestrictionUpdateCommand := NewRootCommand()
	jsonRestrictionUpdateBuffer := &bytes.Buffer{}
	jsonRestrictionUpdateCommand.SetOut(jsonRestrictionUpdateBuffer)
	jsonRestrictionUpdateCommand.SetErr(jsonRestrictionUpdateBuffer)
	jsonRestrictionUpdateCommand.SetArgs([]string{"--json", "branch", "restriction", "update", "12", "--type", "read-only", "--matcher-id", "refs/heads/main"})
	if err := jsonRestrictionUpdateCommand.Execute(); err != nil {
		t.Fatalf("branch restriction update json failed: %v", err)
	}
	if !strings.Contains(jsonRestrictionUpdateBuffer.String(), `"id": 12`) {
		t.Fatalf("expected restriction id in update output, got: %s", jsonRestrictionUpdateBuffer.String())
	}

	jsonRestrictionDeleteCommand := NewRootCommand()
	jsonRestrictionDeleteBuffer := &bytes.Buffer{}
	jsonRestrictionDeleteCommand.SetOut(jsonRestrictionDeleteBuffer)
	jsonRestrictionDeleteCommand.SetErr(jsonRestrictionDeleteBuffer)
	jsonRestrictionDeleteCommand.SetArgs([]string{"--json", "branch", "restriction", "delete", "12"})
	if err := jsonRestrictionDeleteCommand.Execute(); err != nil {
		t.Fatalf("branch restriction delete json failed: %v", err)
	}
	if !strings.Contains(jsonRestrictionDeleteBuffer.String(), `"restriction_id": "12"`) {
		t.Fatalf("expected restriction_id in delete output, got: %s", jsonRestrictionDeleteBuffer.String())
	}

	jsonRestrictionCreateWithAccessKeyCommand := NewRootCommand()
	jsonRestrictionCreateWithAccessKeyBuffer := &bytes.Buffer{}
	jsonRestrictionCreateWithAccessKeyCommand.SetOut(jsonRestrictionCreateWithAccessKeyBuffer)
	jsonRestrictionCreateWithAccessKeyCommand.SetErr(jsonRestrictionCreateWithAccessKeyBuffer)
	jsonRestrictionCreateWithAccessKeyCommand.SetArgs([]string{"--json", "branch", "restriction", "create", "--type", "read-only", "--matcher-id", "refs/heads/main", "--user", "alice", "--group", "devs", "--access-key-id", "7"})
	if err := jsonRestrictionCreateWithAccessKeyCommand.Execute(); err != nil {
		t.Fatalf("branch restriction create with access key json failed: %v", err)
	}
	if !strings.Contains(jsonRestrictionCreateWithAccessKeyBuffer.String(), `"id": 12`) {
		t.Fatalf("expected restriction id in create with access key output, got: %s", jsonRestrictionCreateWithAccessKeyBuffer.String())
	}

	jsonRestrictionUpdateWithAccessKeyCommand := NewRootCommand()
	jsonRestrictionUpdateWithAccessKeyBuffer := &bytes.Buffer{}
	jsonRestrictionUpdateWithAccessKeyCommand.SetOut(jsonRestrictionUpdateWithAccessKeyBuffer)
	jsonRestrictionUpdateWithAccessKeyCommand.SetErr(jsonRestrictionUpdateWithAccessKeyBuffer)
	jsonRestrictionUpdateWithAccessKeyCommand.SetArgs([]string{"--json", "branch", "restriction", "update", "12", "--type", "read-only", "--matcher-id", "refs/heads/main", "--user", "alice", "--group", "devs", "--access-key-id", "7"})
	if err := jsonRestrictionUpdateWithAccessKeyCommand.Execute(); err != nil {
		t.Fatalf("branch restriction update with access key json failed: %v", err)
	}
	if !strings.Contains(jsonRestrictionUpdateWithAccessKeyBuffer.String(), `"id": 12`) {
		t.Fatalf("expected restriction id in update with access key output, got: %s", jsonRestrictionUpdateWithAccessKeyBuffer.String())
	}
}

func TestSafeUsersHelper(t *testing.T) {
	if len(safeUsers(nil)) != 0 {
		t.Fatal("expected safeUsers(nil) to return empty slice")
	}

	name := "alice"
	users := []openapigenerated.RestApplicationUser{{Name: &name}}
	if len(safeUsers(&users)) != 1 {
		t.Fatal("expected safeUsers to return provided users")
	}
}

func TestBranchCommandEmptyResultsOutput(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/branches":
			_, _ = writer.Write([]byte(`{"values":[],"isLastPage":true}`))
		case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/rest/branch-utils/latest/projects/TEST/repos/demo/branches/info/"):
			_, _ = writer.Write([]byte(`{"values":[],"isLastPage":true}`))
		case request.Method == http.MethodGet && request.URL.Path == "/rest/branch-permissions/latest/projects/TEST/repos/demo/restrictions":
			_, _ = writer.Write([]byte(`{"values":[],"isLastPage":true}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	tests := []struct {
		name          string
		args          []string
		expectSnippet string
	}{
		{name: "branch list empty", args: []string{"branch", "list"}, expectSnippet: "No branches found"},
		{name: "branch model inspect empty", args: []string{"branch", "model", "inspect", "abc"}, expectSnippet: "No matching refs found"},
		{name: "branch restriction list empty", args: []string{"branch", "restriction", "list"}, expectSnippet: "No restrictions found"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			command := NewRootCommand()
			buffer := &bytes.Buffer{}
			command.SetOut(buffer)
			command.SetErr(buffer)
			command.SetArgs(testCase.args)

			if err := command.Execute(); err != nil {
				t.Fatalf("command failed: %v", err)
			}
			if !strings.Contains(buffer.String(), testCase.expectSnippet) {
				t.Fatalf("expected output to contain %q, got: %s", testCase.expectSnippet, buffer.String())
			}
		})
	}
}

func TestBuildRequiredCommandPaths(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/rest/required-builds/latest/projects/TEST/repos/demo/conditions":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"values":[{"id":11,"buildParentKeys":["ci"]}],"isLastPage":true}`))
		case request.Method == http.MethodPost && request.URL.Path == "/rest/required-builds/latest/projects/TEST/repos/demo/condition":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"id":12,"buildParentKeys":["ci"]}`))
		case request.Method == http.MethodPut && request.URL.Path == "/rest/required-builds/latest/projects/TEST/repos/demo/condition/12":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"id":12,"buildParentKeys":["ci"]}`))
		case request.Method == http.MethodDelete && request.URL.Path == "/rest/required-builds/latest/projects/TEST/repos/demo/condition/12":
			writer.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	humanListCommand := NewRootCommand()
	humanListBuffer := &bytes.Buffer{}
	humanListCommand.SetOut(humanListBuffer)
	humanListCommand.SetErr(humanListBuffer)
	humanListCommand.SetArgs([]string{"build", "required", "list"})
	if err := humanListCommand.Execute(); err != nil {
		t.Fatalf("build required list failed: %v", err)
	}
	if !strings.Contains(humanListBuffer.String(), "id=11") {
		t.Fatalf("expected required id in list output, got: %s", humanListBuffer.String())
	}

	createCommand := NewRootCommand()
	createBuffer := &bytes.Buffer{}
	createCommand.SetOut(createBuffer)
	createCommand.SetErr(createBuffer)
	createCommand.SetArgs([]string{"--json", "build", "required", "create", "--body", `{"buildParentKeys":["ci"]}`})
	if err := createCommand.Execute(); err != nil {
		t.Fatalf("build required create failed: %v", err)
	}
	if !strings.Contains(createBuffer.String(), `"id": 12`) {
		t.Fatalf("expected created id in output, got: %s", createBuffer.String())
	}

	updateCommand := NewRootCommand()
	updateBuffer := &bytes.Buffer{}
	updateCommand.SetOut(updateBuffer)
	updateCommand.SetErr(updateBuffer)
	updateCommand.SetArgs([]string{"--json", "build", "required", "update", "12", "--body", `{"buildParentKeys":["ci"]}`})
	if err := updateCommand.Execute(); err != nil {
		t.Fatalf("build required update failed: %v", err)
	}
	if !strings.Contains(updateBuffer.String(), `"id": 12`) {
		t.Fatalf("expected updated id in output, got: %s", updateBuffer.String())
	}

	deleteCommand := NewRootCommand()
	deleteBuffer := &bytes.Buffer{}
	deleteCommand.SetOut(deleteBuffer)
	deleteCommand.SetErr(deleteBuffer)
	deleteCommand.SetArgs([]string{"build", "required", "delete", "12"})
	if err := deleteCommand.Execute(); err != nil {
		t.Fatalf("build required delete failed: %v", err)
	}
	if !strings.Contains(deleteBuffer.String(), "Deleted required build merge check 12") {
		t.Fatalf("expected delete message, got: %s", deleteBuffer.String())
	}
}

func TestInsightsReportAndAnnotationCommandPaths(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodPut && request.URL.Path == "/rest/insights/latest/projects/TEST/repos/demo/commits/abc/reports/lint":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"key":"lint","title":"Lint","result":"PASS"}`))
		case request.Method == http.MethodGet && request.URL.Path == "/rest/insights/latest/projects/TEST/repos/demo/commits/abc/reports/lint":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"key":"lint","title":"Lint","result":"PASS"}`))
		case request.Method == http.MethodGet && request.URL.Path == "/rest/insights/latest/projects/TEST/repos/demo/commits/abc/reports":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"values":[{"key":"lint","title":"Lint","result":"PASS"}],"isLastPage":true}`))
		case request.Method == http.MethodDelete && request.URL.Path == "/rest/insights/latest/projects/TEST/repos/demo/commits/abc/reports/lint":
			writer.WriteHeader(http.StatusNoContent)
		case request.Method == http.MethodPost && request.URL.Path == "/rest/insights/latest/projects/TEST/repos/demo/commits/abc/reports/lint/annotations":
			writer.WriteHeader(http.StatusNoContent)
		case request.Method == http.MethodGet && request.URL.Path == "/rest/insights/latest/projects/TEST/repos/demo/commits/abc/reports/lint/annotations":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"annotations":[{"externalId":"ann1","severity":"LOW","message":"note"}]}`))
		case request.Method == http.MethodDelete && request.URL.Path == "/rest/insights/latest/projects/TEST/repos/demo/commits/abc/reports/lint/annotations":
			writer.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	setCommand := NewRootCommand()
	setBuffer := &bytes.Buffer{}
	setCommand.SetOut(setBuffer)
	setCommand.SetErr(setBuffer)
	setCommand.SetArgs([]string{"--json", "insights", "report", "set", "abc", "lint", "--body", `{"title":"Lint","result":"PASS"}`})
	if err := setCommand.Execute(); err != nil {
		t.Fatalf("insights report set failed: %v", err)
	}

	getCommand := NewRootCommand()
	getBuffer := &bytes.Buffer{}
	getCommand.SetOut(getBuffer)
	getCommand.SetErr(getBuffer)
	getCommand.SetArgs([]string{"--json", "insights", "report", "get", "abc", "lint"})
	if err := getCommand.Execute(); err != nil {
		t.Fatalf("insights report get failed: %v", err)
	}
	if !strings.Contains(getBuffer.String(), `"key": "lint"`) {
		t.Fatalf("expected report key in get output, got: %s", getBuffer.String())
	}

	humanListCommand := NewRootCommand()
	humanListBuffer := &bytes.Buffer{}
	humanListCommand.SetOut(humanListBuffer)
	humanListCommand.SetErr(humanListBuffer)
	humanListCommand.SetArgs([]string{"insights", "report", "list", "abc"})
	if err := humanListCommand.Execute(); err != nil {
		t.Fatalf("insights report list failed: %v", err)
	}
	if !strings.Contains(humanListBuffer.String(), "lint") {
		t.Fatalf("expected report in list output, got: %s", humanListBuffer.String())
	}

	addAnnCommand := NewRootCommand()
	addAnnBuffer := &bytes.Buffer{}
	addAnnCommand.SetOut(addAnnBuffer)
	addAnnCommand.SetErr(addAnnBuffer)
	addAnnCommand.SetArgs([]string{"--json", "insights", "annotation", "add", "abc", "lint", "--body", `[{"externalId":"ann1","message":"note","severity":"LOW"}]`})
	if err := addAnnCommand.Execute(); err != nil {
		t.Fatalf("insights annotation add failed: %v", err)
	}

	humanListAnnCommand := NewRootCommand()
	humanListAnnBuffer := &bytes.Buffer{}
	humanListAnnCommand.SetOut(humanListAnnBuffer)
	humanListAnnCommand.SetErr(humanListAnnBuffer)
	humanListAnnCommand.SetArgs([]string{"insights", "annotation", "list", "abc", "lint"})
	if err := humanListAnnCommand.Execute(); err != nil {
		t.Fatalf("insights annotation list failed: %v", err)
	}
	if !strings.Contains(humanListAnnBuffer.String(), "ann1") {
		t.Fatalf("expected annotation id in output, got: %s", humanListAnnBuffer.String())
	}

	deleteAnnCommand := NewRootCommand()
	deleteAnnBuffer := &bytes.Buffer{}
	deleteAnnCommand.SetOut(deleteAnnBuffer)
	deleteAnnCommand.SetErr(deleteAnnBuffer)
	deleteAnnCommand.SetArgs([]string{"insights", "annotation", "delete", "abc", "lint", "--external-id", "ann1"})
	if err := deleteAnnCommand.Execute(); err != nil {
		t.Fatalf("insights annotation delete failed: %v", err)
	}
	if !strings.Contains(deleteAnnBuffer.String(), "Deleted annotations") {
		t.Fatalf("expected annotation delete output, got: %s", deleteAnnBuffer.String())
	}

	deleteReportCommand := NewRootCommand()
	deleteReportBuffer := &bytes.Buffer{}
	deleteReportCommand.SetOut(deleteReportBuffer)
	deleteReportCommand.SetErr(deleteReportBuffer)
	deleteReportCommand.SetArgs([]string{"insights", "report", "delete", "abc", "lint"})
	if err := deleteReportCommand.Execute(); err != nil {
		t.Fatalf("insights report delete failed: %v", err)
	}
	if !strings.Contains(deleteReportBuffer.String(), "Deleted report") {
		t.Fatalf("expected report delete output, got: %s", deleteReportBuffer.String())
	}
}

func TestRepoListCommandPaths(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.URL.Query().Get("limit"); got != "25" {
			t.Fatalf("expected limit query to be 25, got %q", got)
		}
		switch request.URL.Path {
		case "/rest/api/1.0/repos":
		case "/rest/api/1.0/projects/TEST/repos":
		default:
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
		_, _ = writer.Write([]byte(`{"values":[{"slug":"demo","name":"Demo Repo","public":false,"project":{"key":"TEST"}}],"isLastPage":true,"nextPageStart":0}`))
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	jsonCommand := NewRootCommand()
	jsonBuffer := &bytes.Buffer{}
	jsonCommand.SetOut(jsonBuffer)
	jsonCommand.SetErr(jsonBuffer)
	jsonCommand.SetArgs([]string{"--json", "repo", "list", "--project", "TEST", "--limit", "25"})
	if err := jsonCommand.Execute(); err != nil {
		t.Fatalf("repo list json failed: %v", err)
	}
	if !strings.Contains(jsonBuffer.String(), "demo") {
		t.Fatalf("expected repo slug in json output, got: %s", jsonBuffer.String())
	}

	humanCommand := NewRootCommand()
	humanBuffer := &bytes.Buffer{}
	humanCommand.SetOut(humanBuffer)
	humanCommand.SetErr(humanBuffer)
	humanCommand.SetArgs([]string{"repo", "list", "--project", "TEST", "--limit", "25"})
	if err := humanCommand.Execute(); err != nil {
		t.Fatalf("repo list human failed: %v", err)
	}
	if !strings.Contains(humanBuffer.String(), "TEST/demo") {
		t.Fatalf("expected project/slug output, got: %s", humanBuffer.String())
	}

	defaultCommand := NewRootCommand()
	defaultBuffer := &bytes.Buffer{}
	defaultCommand.SetOut(defaultBuffer)
	defaultCommand.SetErr(defaultBuffer)
	defaultCommand.SetArgs([]string{"repo", "list", "--limit", "25"})
	if err := defaultCommand.Execute(); err != nil {
		t.Fatalf("repo list default path failed: %v", err)
	}
	if !strings.Contains(defaultBuffer.String(), "TEST/demo") {
		t.Fatalf("expected default repo list output, got: %s", defaultBuffer.String())
	}
}

func TestAuthLoginAndLogoutJSON(t *testing.T) {
	t.Setenv("BB_CONFIG_PATH", filepath.Join(t.TempDir(), "auth-config.yaml"))
	t.Setenv("BB_DISABLE_STORED_CONFIG", "0")
	t.Setenv("BITBUCKET_URL", "http://localhost:7990")
	t.Setenv("BITBUCKET_TOKEN", "")
	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("BITBUCKET_PASSWORD", "")
	t.Setenv("ADMIN_USER", "")
	t.Setenv("ADMIN_PASSWORD", "")

	loginCommand := NewRootCommand()
	loginBuffer := &bytes.Buffer{}
	loginCommand.SetOut(loginBuffer)
	loginCommand.SetErr(loginBuffer)
	loginCommand.SetArgs([]string{"--json", "auth", "login", "http://localhost:7990", "--token", "abc123", "--set-default"})
	if err := loginCommand.Execute(); err != nil {
		t.Fatalf("auth login json failed: %v", err)
	}
	if !strings.Contains(loginBuffer.String(), `"auth_mode": "token"`) {
		t.Fatalf("expected token auth mode in login output, got: %s", loginBuffer.String())
	}

	logoutCommand := NewRootCommand()
	logoutBuffer := &bytes.Buffer{}
	logoutCommand.SetOut(logoutBuffer)
	logoutCommand.SetErr(logoutBuffer)
	logoutCommand.SetArgs([]string{"--json", "auth", "logout", "--host", "http://localhost:7990"})
	if err := logoutCommand.Execute(); err != nil {
		t.Fatalf("auth logout json failed: %v", err)
	}
	if !strings.Contains(logoutBuffer.String(), `"status": "ok"`) {
		t.Fatalf("expected status ok in logout output, got: %s", logoutBuffer.String())
	}
}

func TestAuthLoginWithPositionalHost(t *testing.T) {
	t.Setenv("BB_CONFIG_PATH", filepath.Join(t.TempDir(), "auth-config-positional.yaml"))
	t.Setenv("BB_DISABLE_STORED_CONFIG", "0")
	t.Setenv("BITBUCKET_TOKEN", "")
	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("BITBUCKET_PASSWORD", "")
	t.Setenv("ADMIN_USER", "")
	t.Setenv("ADMIN_PASSWORD", "")

	loginCommand := NewRootCommand()
	loginBuffer := &bytes.Buffer{}
	loginCommand.SetOut(loginBuffer)
	loginCommand.SetErr(loginBuffer)
	loginCommand.SetArgs([]string{"--json", "auth", "login", "http://positional.local:7990", "--token", "abc123", "--set-default"})
	if err := loginCommand.Execute(); err != nil {
		t.Fatalf("auth login json with positional host failed: %v", err)
	}
	if !strings.Contains(loginBuffer.String(), `"host": "http://positional.local:7990"`) {
		t.Fatalf("expected positional host in login output, got: %s", loginBuffer.String())
	}
}

func TestDiffPRNameOnlyAndCommitJSON(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/rest/api/latest/projects/TEST/repos/demo/pull-requests/99.diff":
			_, _ = writer.Write([]byte("diff --git a/a.txt b/a.txt\n"))
		case "/rest/api/latest/projects/TEST/repos/demo/patch":
			_, _ = writer.Write([]byte("diff --git a/seed.txt b/seed.txt\n"))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	prNameOnlyCmd := NewRootCommand()
	prNameOnlyBuffer := &bytes.Buffer{}
	prNameOnlyCmd.SetOut(prNameOnlyBuffer)
	prNameOnlyCmd.SetErr(prNameOnlyBuffer)
	prNameOnlyCmd.SetArgs([]string{"diff", "pr", "99", "--name-only"})
	if err := prNameOnlyCmd.Execute(); err != nil {
		t.Fatalf("diff pr name-only failed: %v", err)
	}
	if !strings.Contains(prNameOnlyBuffer.String(), "a.txt") {
		t.Fatalf("expected filename in diff pr name-only output, got: %s", prNameOnlyBuffer.String())
	}

	commitJSONCmd := NewRootCommand()
	commitJSONBuffer := &bytes.Buffer{}
	commitJSONCmd.SetOut(commitJSONBuffer)
	commitJSONCmd.SetErr(commitJSONBuffer)
	commitJSONCmd.SetArgs([]string{"--json", "diff", "commit", "abc123"})
	if err := commitJSONCmd.Execute(); err != nil {
		t.Fatalf("diff commit json failed: %v", err)
	}
	if !strings.Contains(commitJSONBuffer.String(), "diff --git") {
		t.Fatalf("expected patch payload in diff commit json output, got: %s", commitJSONBuffer.String())
	}
}

func TestBuildStatusHumanCommandPaths(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/rest/build-status/latest/commits/abc123":
			writer.WriteHeader(http.StatusNoContent)
		case request.Method == http.MethodGet && request.URL.Path == "/rest/build-status/latest/commits/abc123":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"isLastPage":true,"values":[]}`))
		case request.Method == http.MethodGet && request.URL.Path == "/rest/build-status/latest/commits/stats/abc123":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"successful":1,"failed":2,"inProgress":3,"unknown":4,"cancelled":5}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	setCmd := NewRootCommand()
	setBuffer := &bytes.Buffer{}
	setCmd.SetOut(setBuffer)
	setCmd.SetErr(setBuffer)
	setCmd.SetArgs([]string{"build", "status", "set", "abc123", "--key", "ci/main", "--state", "SUCCESSFUL", "--url", "https://ci.example/1"})
	if err := setCmd.Execute(); err != nil {
		t.Fatalf("build status set human failed: %v", err)
	}
	if !strings.Contains(setBuffer.String(), "Build status ci/main set on abc123") {
		t.Fatalf("expected human set output, got: %s", setBuffer.String())
	}

	getCmd := NewRootCommand()
	getBuffer := &bytes.Buffer{}
	getCmd.SetOut(getBuffer)
	getCmd.SetErr(getBuffer)
	getCmd.SetArgs([]string{"build", "status", "get", "abc123"})
	if err := getCmd.Execute(); err != nil {
		t.Fatalf("build status get human failed: %v", err)
	}
	if !strings.Contains(getBuffer.String(), "No build statuses found") {
		t.Fatalf("expected empty statuses message, got: %s", getBuffer.String())
	}

	statsCmd := NewRootCommand()
	statsBuffer := &bytes.Buffer{}
	statsCmd.SetOut(statsBuffer)
	statsCmd.SetErr(statsBuffer)
	statsCmd.SetArgs([]string{"build", "status", "stats", "abc123", "--include-unique"})
	if err := statsCmd.Execute(); err != nil {
		t.Fatalf("build status stats human failed: %v", err)
	}
	if !strings.Contains(statsBuffer.String(), "Successful: 1") || !strings.Contains(statsBuffer.String(), "Cancelled: 5") {
		t.Fatalf("expected stats lines in output, got: %s", statsBuffer.String())
	}
}

func TestRepoCommentCommandPathsCommitAndPR(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/commits/abc/comments":
			_, _ = writer.Write([]byte(`{"values":[{"id":101,"text":"commit note","version":1}],"isLastPage":true}`))
		case request.Method == http.MethodPost && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/commits/abc/comments":
			writer.WriteHeader(http.StatusCreated)
			_, _ = writer.Write([]byte(`{"id":101,"text":"commit note","version":1}`))
		case request.Method == http.MethodPut && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/commits/abc/comments/101":
			_, _ = writer.Write([]byte(`{"id":101,"text":"commit updated","version":2}`))
		case request.Method == http.MethodDelete && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/commits/abc/comments/101":
			writer.WriteHeader(http.StatusNoContent)
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/77/comments":
			_, _ = writer.Write([]byte(`{"values":[{"id":201,"text":"pr note","version":3}],"isLastPage":true}`))
		case request.Method == http.MethodPost && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/77/comments":
			writer.WriteHeader(http.StatusCreated)
			_, _ = writer.Write([]byte(`{"id":201,"text":"pr note","version":3}`))
		case request.Method == http.MethodPut && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/77/comments/201":
			_, _ = writer.Write([]byte(`{"id":201,"text":"pr updated","version":4}`))
		case request.Method == http.MethodDelete && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/77/comments/201":
			writer.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	listCommitJSON := NewRootCommand()
	listCommitJSONBuffer := &bytes.Buffer{}
	listCommitJSON.SetOut(listCommitJSONBuffer)
	listCommitJSON.SetErr(listCommitJSONBuffer)
	listCommitJSON.SetArgs([]string{"--json", "repo", "comment", "list", "--commit", "abc", "--path", "seed.txt", "--limit", "10"})
	if err := listCommitJSON.Execute(); err != nil {
		t.Fatalf("repo comment list commit json failed: %v", err)
	}
	if !strings.Contains(listCommitJSONBuffer.String(), "commit note") {
		t.Fatalf("expected commit comment in json output, got: %s", listCommitJSONBuffer.String())
	}

	createCommitHuman := NewRootCommand()
	createCommitHumanBuffer := &bytes.Buffer{}
	createCommitHuman.SetOut(createCommitHumanBuffer)
	createCommitHuman.SetErr(createCommitHumanBuffer)
	createCommitHuman.SetArgs([]string{"repo", "comment", "create", "--commit", "abc", "--text", "commit note"})
	if err := createCommitHuman.Execute(); err != nil {
		t.Fatalf("repo comment create commit human failed: %v", err)
	}
	if !strings.Contains(createCommitHumanBuffer.String(), "Created comment 101") {
		t.Fatalf("expected create output, got: %s", createCommitHumanBuffer.String())
	}

	updateCommitJSON := NewRootCommand()
	updateCommitJSONBuffer := &bytes.Buffer{}
	updateCommitJSON.SetOut(updateCommitJSONBuffer)
	updateCommitJSON.SetErr(updateCommitJSONBuffer)
	updateCommitJSON.SetArgs([]string{"--json", "repo", "comment", "update", "--commit", "abc", "--id", "101", "--text", "commit updated", "--version", "1"})
	if err := updateCommitJSON.Execute(); err != nil {
		t.Fatalf("repo comment update commit json failed: %v", err)
	}
	if !strings.Contains(updateCommitJSONBuffer.String(), "commit updated") {
		t.Fatalf("expected updated text in output, got: %s", updateCommitJSONBuffer.String())
	}

	deleteCommitHuman := NewRootCommand()
	deleteCommitHumanBuffer := &bytes.Buffer{}
	deleteCommitHuman.SetOut(deleteCommitHumanBuffer)
	deleteCommitHuman.SetErr(deleteCommitHumanBuffer)
	deleteCommitHuman.SetArgs([]string{"repo", "comment", "delete", "--commit", "abc", "--id", "101", "--version", "2"})
	if err := deleteCommitHuman.Execute(); err != nil {
		t.Fatalf("repo comment delete commit human failed: %v", err)
	}
	if !strings.Contains(deleteCommitHumanBuffer.String(), "Deleted comment 101 (version=2)") {
		t.Fatalf("expected delete output, got: %s", deleteCommitHumanBuffer.String())
	}

	listPRHuman := NewRootCommand()
	listPRHumanBuffer := &bytes.Buffer{}
	listPRHuman.SetOut(listPRHumanBuffer)
	listPRHuman.SetErr(listPRHumanBuffer)
	listPRHuman.SetArgs([]string{"repo", "comment", "list", "--pr", "77", "--path", "seed.txt", "--limit", "10"})
	if err := listPRHuman.Execute(); err != nil {
		t.Fatalf("repo comment list pr human failed: %v", err)
	}
	if !strings.Contains(listPRHumanBuffer.String(), "pr note") {
		t.Fatalf("expected pr note in human output, got: %s", listPRHumanBuffer.String())
	}

	createPRJSON := NewRootCommand()
	createPRJSONBuffer := &bytes.Buffer{}
	createPRJSON.SetOut(createPRJSONBuffer)
	createPRJSON.SetErr(createPRJSONBuffer)
	createPRJSON.SetArgs([]string{"--json", "repo", "comment", "create", "--pr", "77", "--text", "pr note"})
	if err := createPRJSON.Execute(); err != nil {
		t.Fatalf("repo comment create pr json failed: %v", err)
	}
	if !strings.Contains(createPRJSONBuffer.String(), "pr note") {
		t.Fatalf("expected pr note in json output, got: %s", createPRJSONBuffer.String())
	}

	updatePRHuman := NewRootCommand()
	updatePRHumanBuffer := &bytes.Buffer{}
	updatePRHuman.SetOut(updatePRHumanBuffer)
	updatePRHuman.SetErr(updatePRHumanBuffer)
	updatePRHuman.SetArgs([]string{"repo", "comment", "update", "--pr", "77", "--id", "201", "--text", "pr updated", "--version", "3"})
	if err := updatePRHuman.Execute(); err != nil {
		t.Fatalf("repo comment update pr human failed: %v", err)
	}
	if !strings.Contains(updatePRHumanBuffer.String(), "Updated comment 201") {
		t.Fatalf("expected updated human output, got: %s", updatePRHumanBuffer.String())
	}

	deletePRJSON := NewRootCommand()
	deletePRJSONBuffer := &bytes.Buffer{}
	deletePRJSON.SetOut(deletePRJSONBuffer)
	deletePRJSON.SetErr(deletePRJSONBuffer)
	deletePRJSON.SetArgs([]string{"--json", "repo", "comment", "delete", "--pr", "77", "--id", "201", "--version", "4"})
	if err := deletePRJSON.Execute(); err != nil {
		t.Fatalf("repo comment delete pr json failed: %v", err)
	}
	if !strings.Contains(deletePRJSONBuffer.String(), `"deleted"`) {
		t.Fatalf("expected deleted payload in json output, got: %s", deletePRJSONBuffer.String())
	}
}

func TestAuthStatusHostOverrideAndHumanLoginLogout(t *testing.T) {
	t.Setenv("BB_CONFIG_PATH", filepath.Join(t.TempDir(), "auth-config.yaml"))
	t.Setenv("BB_DISABLE_STORED_CONFIG", "0")
	t.Setenv("BITBUCKET_URL", "http://localhost:7990")
	t.Setenv("BITBUCKET_TOKEN", "")
	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("BITBUCKET_PASSWORD", "")
	t.Setenv("ADMIN_USER", "")
	t.Setenv("ADMIN_PASSWORD", "")

	statusCommand := NewRootCommand()
	statusBuffer := &bytes.Buffer{}
	statusCommand.SetOut(statusBuffer)
	statusCommand.SetErr(statusBuffer)
	statusCommand.SetArgs([]string{"auth", "status", "--host", "http://example.local:7990"})
	if err := statusCommand.Execute(); err != nil {
		t.Fatalf("auth status with host override failed: %v", err)
	}
	if !strings.Contains(statusBuffer.String(), "http://example.local:7990") {
		t.Fatalf("expected overridden host in status output, got: %s", statusBuffer.String())
	}

	loginCommand := NewRootCommand()
	loginBuffer := &bytes.Buffer{}
	loginCommand.SetOut(loginBuffer)
	loginCommand.SetErr(loginBuffer)
	loginCommand.SetArgs([]string{"auth", "login", "http://example.local:7990", "--token", "abc123", "--set-default"})
	if err := loginCommand.Execute(); err != nil {
		t.Fatalf("auth login human failed: %v", err)
	}
	if !strings.Contains(loginBuffer.String(), "Stored credentials for") {
		t.Fatalf("expected human login output, got: %s", loginBuffer.String())
	}

	logoutCommand := NewRootCommand()
	logoutBuffer := &bytes.Buffer{}
	logoutCommand.SetOut(logoutBuffer)
	logoutCommand.SetErr(logoutBuffer)
	logoutCommand.SetArgs([]string{"auth", "logout", "--host", "http://example.local:7990"})
	if err := logoutCommand.Execute(); err != nil {
		t.Fatalf("auth logout human failed: %v", err)
	}
	if !strings.Contains(logoutBuffer.String(), "Stored credentials removed") {
		t.Fatalf("expected human logout output, got: %s", logoutBuffer.String())
	}
}

func TestAuthTokenURLCommand(t *testing.T) {
	t.Setenv("BITBUCKET_URL", "http://localhost:7990")

	human := NewRootCommand()
	humanBuffer := &bytes.Buffer{}
	human.SetOut(humanBuffer)
	human.SetErr(humanBuffer)
	human.SetArgs([]string{"auth", "token-url", "--host", "https://bitbucket.acme.corp"})
	if err := human.Execute(); err != nil {
		t.Fatalf("auth token-url human failed: %v", err)
	}
	if !strings.Contains(humanBuffer.String(), "https://bitbucket.acme.corp/plugins/servlet/access-tokens/manage") {
		t.Fatalf("expected PAT URL in human output, got: %s", humanBuffer.String())
	}

	jsonCmd := NewRootCommand()
	jsonBuffer := &bytes.Buffer{}
	jsonCmd.SetOut(jsonBuffer)
	jsonCmd.SetErr(jsonBuffer)
	jsonCmd.SetArgs([]string{"--json", "auth", "token-url"})
	if err := jsonCmd.Execute(); err != nil {
		t.Fatalf("auth token-url json failed: %v", err)
	}
	if !strings.Contains(jsonBuffer.String(), `"token_url": "http://localhost:7990/plugins/servlet/access-tokens/manage"`) {
		t.Fatalf("expected token_url in json output, got: %s", jsonBuffer.String())
	}
}

func TestAuthIdentityCommand(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/rest/api/latest/users" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"name":"svc-bot","slug":"svc-bot","displayName":"Service Bot","emailAddress":"svc-bot@example.local","id":91,"type":"SERVICE","active":true}`))
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_TOKEN", "token-123")

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"--json", "auth", "identity", "--host", server.URL})
	if err := command.Execute(); err != nil {
		t.Fatalf("auth identity json failed: %v", err)
	}
	if !strings.Contains(buffer.String(), `"slug": "svc-bot"`) {
		t.Fatalf("expected identity slug in output, got: %s", buffer.String())
	}

	human := NewRootCommand()
	humanBuffer := &bytes.Buffer{}
	human.SetOut(humanBuffer)
	human.SetErr(humanBuffer)
	human.SetArgs([]string{"auth", "whoami", "--host", server.URL})
	if err := human.Execute(); err != nil {
		t.Fatalf("auth whoami failed: %v", err)
	}
	if !strings.Contains(humanBuffer.String(), "Authenticated user:") {
		t.Fatalf("expected identity human output, got: %s", humanBuffer.String())
	}

}

func TestResolveRepositoryReferenceWrappers(t *testing.T) {
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")
	cfg := config.AppConfig{ProjectKey: "TEST"}

	diffRepo, err := resolveRepositoryReference("", cfg)
	if err != nil || diffRepo.ProjectKey != "TEST" || diffRepo.Slug != "demo" {
		t.Fatalf("unexpected diff repository reference: %+v err=%v", diffRepo, err)
	}

	settingsRepo, err := resolveRepositorySettingsReference("", cfg)
	if err != nil || settingsRepo.ProjectKey != "TEST" || settingsRepo.Slug != "demo" {
		t.Fatalf("unexpected settings repository reference: %+v err=%v", settingsRepo, err)
	}

	tagRepo, err := resolveTagRepositoryReference("", cfg)
	if err != nil || tagRepo.ProjectKey != "TEST" || tagRepo.Slug != "demo" {
		t.Fatalf("unexpected tag repository reference: %+v err=%v", tagRepo, err)
	}

	branchRepo, err := resolveBranchRepositoryReference("", cfg)
	if err != nil || branchRepo.ProjectKey != "TEST" || branchRepo.Slug != "demo" {
		t.Fatalf("unexpected branch repository reference: %+v err=%v", branchRepo, err)
	}

	qualityRepo, err := resolveQualityRepositoryReference("", cfg)
	if err != nil || qualityRepo.ProjectKey != "TEST" || qualityRepo.Slug != "demo" {
		t.Fatalf("unexpected quality repository reference: %+v err=%v", qualityRepo, err)
	}

	_, err = resolveRepositoryReference("bad-format", config.AppConfig{})
	if err == nil {
		t.Fatal("expected validation error for invalid diff repository selector")
	}

	_, err = resolveRepositorySettingsReference("bad-format", config.AppConfig{})
	if err == nil {
		t.Fatal("expected validation error for invalid settings repository selector")
	}

	_, err = resolveTagRepositoryReference("bad-format", config.AppConfig{})
	if err == nil {
		t.Fatal("expected validation error for invalid tag repository selector")
	}

	_, err = resolveBranchRepositoryReference("bad-format", config.AppConfig{})
	if err == nil {
		t.Fatal("expected validation error for invalid branch repository selector")
	}

	_, err = resolveQualityRepositoryReference("bad-format", config.AppConfig{})
	if err == nil {
		t.Fatal("expected validation error for invalid quality repository selector")
	}
}

func TestAdminHealthHumanLimitedOutput(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_TOKEN", "")
	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("BITBUCKET_PASSWORD", "")
	t.Setenv("ADMIN_USER", "")
	t.Setenv("ADMIN_PASSWORD", "")

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"admin", "health"})

	if err := command.Execute(); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if !strings.Contains(buffer.String(), "auth=limited") {
		t.Fatalf("expected auth limited output, got: %s", buffer.String())
	}
}

func TestTagBuildAndInsightsEmptyHumanOutputs(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/tags":
			_, _ = writer.Write([]byte(`{"values":[],"isLastPage":true}`))
		case request.Method == http.MethodGet && request.URL.Path == "/rest/build-status/latest/commits/abc123":
			_, _ = writer.Write([]byte(`{"isLastPage":true,"values":[{"key":"ci/main","state":"SUCCESSFUL","url":"https://ci.example"}]}`))
		case request.Method == http.MethodGet && request.URL.Path == "/rest/insights/latest/projects/TEST/repos/demo/commits/abc/reports":
			_, _ = writer.Write([]byte(`{"values":[],"isLastPage":true}`))
		case request.Method == http.MethodGet && request.URL.Path == "/rest/insights/latest/projects/TEST/repos/demo/commits/abc/reports/lint/annotations":
			_, _ = writer.Write([]byte(`{"annotations":[]}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	tagListCommand := NewRootCommand()
	tagListBuffer := &bytes.Buffer{}
	tagListCommand.SetOut(tagListBuffer)
	tagListCommand.SetErr(tagListBuffer)
	tagListCommand.SetArgs([]string{"tag", "list"})
	if err := tagListCommand.Execute(); err != nil {
		t.Fatalf("tag list empty failed: %v", err)
	}
	if !strings.Contains(tagListBuffer.String(), "No tags found") {
		t.Fatalf("expected no tags output, got: %s", tagListBuffer.String())
	}

	buildGetCommand := NewRootCommand()
	buildGetBuffer := &bytes.Buffer{}
	buildGetCommand.SetOut(buildGetBuffer)
	buildGetCommand.SetErr(buildGetBuffer)
	buildGetCommand.SetArgs([]string{"build", "status", "get", "abc123"})
	if err := buildGetCommand.Execute(); err != nil {
		t.Fatalf("build status get non-empty failed: %v", err)
	}
	if !strings.Contains(buildGetBuffer.String(), "ci/main") || !strings.Contains(buildGetBuffer.String(), "SUCCESSFUL") {
		t.Fatalf("expected populated build statuses output, got: %s", buildGetBuffer.String())
	}

	reportListCommand := NewRootCommand()
	reportListBuffer := &bytes.Buffer{}
	reportListCommand.SetOut(reportListBuffer)
	reportListCommand.SetErr(reportListBuffer)
	reportListCommand.SetArgs([]string{"insights", "report", "list", "abc"})
	if err := reportListCommand.Execute(); err != nil {
		t.Fatalf("insights report list empty failed: %v", err)
	}
	if !strings.Contains(reportListBuffer.String(), "No reports found") {
		t.Fatalf("expected no reports output, got: %s", reportListBuffer.String())
	}

	annotationListCommand := NewRootCommand()
	annotationListBuffer := &bytes.Buffer{}
	annotationListCommand.SetOut(annotationListBuffer)
	annotationListCommand.SetErr(annotationListBuffer)
	annotationListCommand.SetArgs([]string{"insights", "annotation", "list", "abc", "lint"})
	if err := annotationListCommand.Execute(); err != nil {
		t.Fatalf("insights annotation list empty failed: %v", err)
	}
	if !strings.Contains(annotationListBuffer.String(), "No annotations found") {
		t.Fatalf("expected no annotations output, got: %s", annotationListBuffer.String())
	}
}

func TestBuildAndInsightsValidationErrorPaths(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "http://localhost:7990")
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	tests := []struct {
		name string
		args []string
	}{
		{name: "build required create invalid json", args: []string{"build", "required", "create", "--body", "{"}},
		{name: "build required update invalid id", args: []string{"build", "required", "update", "bad", "--body", `{"buildParentKeys":["ci"]}`}},
		{name: "build required update invalid json", args: []string{"build", "required", "update", "12", "--body", "{"}},
		{name: "build required delete invalid id", args: []string{"build", "required", "delete", "bad"}},
		{name: "insights report set invalid json", args: []string{"insights", "report", "set", "abc", "lint", "--body", "{"}},
		{name: "insights annotation add invalid json", args: []string{"insights", "annotation", "add", "abc", "lint", "--body", "{"}},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			command := NewRootCommand()
			command.SetOut(&bytes.Buffer{})
			command.SetErr(&bytes.Buffer{})
			command.SetArgs(testCase.args)

			err := command.Execute()
			if err == nil {
				t.Fatalf("expected validation error for args: %v", testCase.args)
			}
			if apperrors.ExitCode(err) != 2 {
				t.Fatalf("expected validation exit code 2, got %d (%v)", apperrors.ExitCode(err), err)
			}
		})
	}
}

func TestRepoSettingsJSONCommandPaths(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/permissions/users":
			_, _ = writer.Write([]byte(`{"values":[{"permission":"REPO_READ","user":{"name":"alice","displayName":"Alice"}}],"isLastPage":true}`))
		case request.Method == http.MethodPut && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/permissions/users":
			writer.WriteHeader(http.StatusNoContent)
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/webhooks":
			_, _ = writer.Write([]byte(`{"values":[{"id":42,"name":"ci-hook"}],"size":1}`))
		case request.Method == http.MethodPost && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/webhooks":
			_, _ = writer.Write([]byte(`{"id":42,"name":"ci-hook"}`))
		case request.Method == http.MethodDelete && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/webhooks/42":
			writer.WriteHeader(http.StatusNoContent)
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/settings/pull-requests":
			_, _ = writer.Write([]byte(`{"requiredAllTasksComplete":true,"requiredApprovers":{"enabled":true,"count":"2"}}`))
		case request.Method == http.MethodPost && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/settings/pull-requests":
			_, _ = writer.Write([]byte(`{"requiredAllTasksComplete":true,"requiredApprovers":2}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	tests := []struct {
		name          string
		args          []string
		expectSnippet string
	}{
		{name: "permissions users list json", args: []string{"--json", "repo", "settings", "security", "permissions", "users", "list"}, expectSnippet: `"users"`},
		{name: "permissions users grant json", args: []string{"--json", "repo", "settings", "security", "permissions", "users", "grant", "alice", "repo_read"}, expectSnippet: `"status": "ok"`},
		{name: "webhooks list json", args: []string{"--json", "repo", "settings", "workflow", "webhooks", "list"}, expectSnippet: `"webhooks"`},
		{name: "webhooks create json", args: []string{"--json", "repo", "settings", "workflow", "webhooks", "create", "ci-hook", "http://example.local/hook"}, expectSnippet: `"webhook"`},
		{name: "webhooks delete json", args: []string{"--json", "repo", "settings", "workflow", "webhooks", "delete", "42"}, expectSnippet: `"webhook_id": "42"`},
		{name: "pull requests get json", args: []string{"--json", "repo", "settings", "pull-requests", "get"}, expectSnippet: `"pull_request_settings"`},
		{name: "pull requests update json", args: []string{"--json", "repo", "settings", "pull-requests", "update", "--required-all-tasks-complete=true"}, expectSnippet: `"status": "ok"`},
		{name: "pull requests update approvers json", args: []string{"--json", "repo", "settings", "pull-requests", "update-approvers", "--count", "2"}, expectSnippet: `"status": "ok"`},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			command := NewRootCommand()
			buffer := &bytes.Buffer{}
			command.SetOut(buffer)
			command.SetErr(buffer)
			command.SetArgs(testCase.args)

			if err := command.Execute(); err != nil {
				t.Fatalf("command failed: %v", err)
			}
			if !strings.Contains(buffer.String(), testCase.expectSnippet) {
				t.Fatalf("expected output to contain %q, got: %s", testCase.expectSnippet, buffer.String())
			}
		})
	}
}

func TestBranchCommandsPropagateServiceErrors(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusNotFound)
		_, _ = writer.Write([]byte("missing"))
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	tests := []struct {
		name string
		args []string
	}{
		{name: "branch list", args: []string{"branch", "list"}},
		{name: "branch create", args: []string{"branch", "create", "feature/demo", "--start-point", "abc"}},
		{name: "branch delete", args: []string{"branch", "delete", "feature/demo"}},
		{name: "branch default get", args: []string{"branch", "default", "get"}},
		{name: "branch default set", args: []string{"branch", "default", "set", "main"}},
		{name: "branch model inspect", args: []string{"branch", "model", "inspect", "abc"}},
		{name: "branch model update", args: []string{"branch", "model", "update", "main"}},
		{name: "branch restriction list", args: []string{"branch", "restriction", "list"}},
		{name: "branch restriction get", args: []string{"branch", "restriction", "get", "12"}},
		{name: "branch restriction create", args: []string{"branch", "restriction", "create", "--type", "read-only", "--matcher-id", "refs/heads/main"}},
		{name: "branch restriction update", args: []string{"branch", "restriction", "update", "12", "--type", "read-only", "--matcher-id", "refs/heads/main"}},
		{name: "branch restriction delete", args: []string{"branch", "restriction", "delete", "12"}},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			command := NewRootCommand()
			command.SetOut(&bytes.Buffer{})
			command.SetErr(&bytes.Buffer{})
			command.SetArgs(testCase.args)

			err := command.Execute()
			if err == nil {
				t.Fatalf("expected not found error for args: %v", testCase.args)
			}
			if apperrors.ExitCode(err) != 4 {
				t.Fatalf("expected exit code 4 for args %v, got %d (%v)", testCase.args, apperrors.ExitCode(err), err)
			}
		})
	}
}

func TestTagBuildInsightsCommandsPropagateServiceErrors(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusNotFound)
		_, _ = writer.Write([]byte("missing"))
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	tests := []struct {
		name string
		args []string
	}{
		{name: "tag list", args: []string{"tag", "list"}},
		{name: "tag create", args: []string{"tag", "create", "v1.0.0", "--start-point", "abc123"}},
		{name: "tag view", args: []string{"tag", "view", "v1.0.0"}},
		{name: "tag delete", args: []string{"tag", "delete", "v1.0.0"}},
		{name: "build status get", args: []string{"build", "status", "get", "abc123"}},
		{name: "build status stats", args: []string{"build", "status", "stats", "abc123"}},
		{name: "build required list", args: []string{"build", "required", "list"}},
		{name: "build required create", args: []string{"build", "required", "create", "--body", `{"buildParentKeys":["ci"]}`}},
		{name: "build required update", args: []string{"build", "required", "update", "12", "--body", `{"buildParentKeys":["ci"]}`}},
		{name: "build required delete", args: []string{"build", "required", "delete", "12"}},
		{name: "insights report list", args: []string{"insights", "report", "list", "abc"}},
		{name: "insights report set", args: []string{"insights", "report", "set", "abc", "lint", "--body", `{"title":"Lint","result":"PASS"}`}},
		{name: "insights report get", args: []string{"insights", "report", "get", "abc", "lint"}},
		{name: "insights report delete", args: []string{"insights", "report", "delete", "abc", "lint"}},
		{name: "insights annotation add", args: []string{"insights", "annotation", "add", "abc", "lint", "--body", `[{"externalId":"ann1","message":"note","severity":"LOW"}]`}},
		{name: "insights annotation list", args: []string{"insights", "annotation", "list", "abc", "lint"}},
		{name: "insights annotation delete", args: []string{"insights", "annotation", "delete", "abc", "lint", "--external-id", "ann1"}},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			command := NewRootCommand()
			command.SetOut(&bytes.Buffer{})
			command.SetErr(&bytes.Buffer{})
			command.SetArgs(testCase.args)

			err := command.Execute()
			if err == nil {
				t.Fatalf("expected not found error for args: %v", testCase.args)
			}
			if apperrors.ExitCode(err) != 4 {
				t.Fatalf("expected exit code 4 for args %v, got %d (%v)", testCase.args, apperrors.ExitCode(err), err)
			}
		})
	}
}

func TestRepoSettingsCommandsPropagateServiceErrors(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
		_, _ = writer.Write([]byte("unauthorized"))
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	tests := []struct {
		name string
		args []string
	}{
		{name: "permissions users list", args: []string{"repo", "settings", "security", "permissions", "users", "list"}},
		{name: "permissions users grant", args: []string{"repo", "settings", "security", "permissions", "users", "grant", "alice", "repo_read"}},
		{name: "webhooks list", args: []string{"repo", "settings", "workflow", "webhooks", "list"}},
		{name: "webhooks create", args: []string{"repo", "settings", "workflow", "webhooks", "create", "ci-hook", "http://example.local/hook"}},
		{name: "webhooks delete", args: []string{"repo", "settings", "workflow", "webhooks", "delete", "42"}},
		{name: "pull-requests get", args: []string{"repo", "settings", "pull-requests", "get"}},
		{name: "pull-requests update", args: []string{"repo", "settings", "pull-requests", "update", "--required-all-tasks-complete=true"}},
		{name: "pull-requests update approvers", args: []string{"repo", "settings", "pull-requests", "update-approvers", "--count", "2"}},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			command := NewRootCommand()
			command.SetOut(&bytes.Buffer{})
			command.SetErr(&bytes.Buffer{})
			command.SetArgs(testCase.args)

			err := command.Execute()
			if err == nil {
				t.Fatalf("expected authorization/authentication error for args: %v", testCase.args)
			}
			if apperrors.ExitCode(err) != 3 {
				t.Fatalf("expected exit code 3 for args %v, got %d (%v)", testCase.args, apperrors.ExitCode(err), err)
			}
		})
	}
}

func TestRepoSettingsPullRequestsMergeChecksList(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/rest/required-builds/latest/projects/TEST/repos/demo/conditions" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
		_, _ = writer.Write([]byte(`{"values":[{"id":1}]}`))
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetArgs([]string{"--json", "repo", "settings", "pull-requests", "merge-checks", "list"})

	err := command.Execute()
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	if !strings.Contains(buffer.String(), `"merge_checks"`) {
		t.Fatalf("expected merge_checks in output, got: %s", buffer.String())
	}
}

func TestProjectPermissionsUsersList(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/rest/api/latest/projects/PRJ/permissions/users" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
		_, _ = writer.Write([]byte(`{"values":[{"user":{"name":"alice"},"permission":"PROJECT_ADMIN"}],"isLastPage":true}`))
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetArgs([]string{"--json", "project", "permissions", "users", "list", "PRJ"})

	err := command.Execute()
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	if !strings.Contains(buffer.String(), `"alice"`) {
		t.Fatalf("expected alice in output, got: %s", buffer.String())
	}
}

func TestRepoSettingsSecurityPermissionsUsersGrantDryRunStateful(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/repos":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"values":[{"slug":"demo","name":"demo","project":{"key":"TEST"}}],"isLastPage":true}`))
			return
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/permissions/users":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"values":[{"user":{"name":"alice","displayName":"Alice"},"permission":"REPO_READ"}],"isLastPage":true}`))
			return
		case request.Method == http.MethodPut && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/permissions/users":
			t.Fatalf("grant endpoint must not be called in dry-run mode")
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"--json", "--dry-run", "repo", "settings", "security", "permissions", "users", "grant", "alice", "repo_write"})

	if err := command.Execute(); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	output := buffer.String()
	if !strings.Contains(output, `"planning_mode": "stateful"`) || !strings.Contains(output, `"predicted_action": "update"`) {
		t.Fatalf("expected stateful update preview output, got: %s", output)
	}
}

func TestProjectPermissionsUsersGrantDryRunStateful(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/PRJ/permissions/users":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"values":[{"user":{"name":"alice"},"permission":"PROJECT_READ"}],"isLastPage":true}`))
			return
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/PRJ/permissions/users":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"values":[{"user":{"name":"alice"},"permission":"PROJECT_READ"}],"isLastPage":true}`))
			return
		case request.Method == http.MethodPut && request.URL.Path == "/rest/api/latest/projects/PRJ/permissions/users":
			t.Fatalf("grant endpoint must not be called in dry-run mode")
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"--json", "--dry-run", "project", "permissions", "users", "grant", "PRJ", "alice", "project_write"})

	if err := command.Execute(); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	output := buffer.String()
	if !strings.Contains(output, `"planning_mode": "stateful"`) || !strings.Contains(output, `"predicted_action": "update"`) {
		t.Fatalf("expected stateful update preview output, got: %s", output)
	}
}

func TestHookList(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/rest/api/latest/projects/PRJ/settings/hooks" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
		_, _ = writer.Write([]byte(`{"values":[{"enabled":true,"details":{"key":"hook1","name":"Hook 1"}}],"isLastPage":true}`))
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetArgs([]string{"--json", "hook", "list", "--project", "PRJ"})

	err := command.Execute()
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	if !strings.Contains(buffer.String(), `"hook1"`) {
		t.Fatalf("expected hook1 in output, got: %s", buffer.String())
	}
}

func TestHookEnableDryRunStateful(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/PRJ/permissions/users":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"values":[{"group":{"name":"dummy"}}],"isLastPage":true}`))
			return
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/PRJ/settings/hooks":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"values":[{"enabled":false,"details":{"key":"hook1","name":"Hook 1"}}],"isLastPage":true}`))
			return
		case request.Method == http.MethodPut && request.URL.Path == "/rest/api/latest/projects/PRJ/settings/hooks/hook1/enabled":
			t.Fatalf("enable endpoint must not be called in dry-run mode")
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"--json", "--dry-run", "hook", "enable", "hook1", "--project", "PRJ"})

	if err := command.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if !strings.Contains(buffer.String(), `"predicted_action": "update"`) {
		t.Fatalf("expected update prediction, got: %s", buffer.String())
	}
}

func TestHookConfigureDryRunStateful(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/PRJ/permissions/users":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"values":[{"group":{"name":"dummy"}}],"isLastPage":true}`))
			return
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/PRJ/settings/hooks":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"values":[{"enabled":true,"details":{"key":"hook1","name":"Hook 1"}}],"isLastPage":true}`))
			return
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/PRJ/settings/hooks/hook1/settings":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"required":true}`))
			return
		case request.Method == http.MethodPut && request.URL.Path == "/rest/api/latest/projects/PRJ/settings/hooks/hook1/settings":
			t.Fatalf("configure endpoint must not be called in dry-run mode")
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"--json", "--dry-run", "hook", "configure", "hook1", `{"required":true}`, "--project", "PRJ"})

	if err := command.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if !strings.Contains(buffer.String(), `"predicted_action": "no-op"`) {
		t.Fatalf("expected no-op prediction, got: %s", buffer.String())
	}
}

func TestRepoSettingsWorkflowWebhooksCreateDryRunStateful(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/repos":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"values":[{"slug":"demo","name":"demo","project":{"key":"TEST"}}],"isLastPage":true}`))
			return
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/webhooks":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`[{"id":11,"name":"existing","url":"http://existing.local"}]`))
			return
		case request.Method == http.MethodPost && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/webhooks":
			t.Fatalf("create webhook endpoint must not be called in dry-run mode")
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"--json", "--dry-run", "repo", "settings", "workflow", "webhooks", "create", "newhook", "http://example.local/hook"})

	if err := command.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if !strings.Contains(buffer.String(), `"predicted_action": "create"`) {
		t.Fatalf("expected create prediction, got: %s", buffer.String())
	}
}

func TestRepoSettingsPullRequestsUpdateDryRunStateful(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/repos":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"values":[{"slug":"demo","name":"demo","project":{"key":"TEST"}}],"isLastPage":true}`))
			return
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/settings/pull-requests":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"requiredAllTasksComplete":false}`))
			return
		case request.Method == http.MethodPost && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/settings/pull-requests":
			t.Fatalf("update pull-request settings endpoint must not be called in dry-run mode")
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"--json", "--dry-run", "repo", "settings", "pull-requests", "update", "--required-all-tasks-complete=true"})

	if err := command.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if !strings.Contains(buffer.String(), `"predicted_action": "update"`) {
		t.Fatalf("expected update prediction, got: %s", buffer.String())
	}
}

func TestBranchCreateDryRunStateful(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/repos":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"values":[{"slug":"demo","name":"demo","project":{"key":"TEST"}}],"isLastPage":true}`))
			return
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/branches":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"values":[{"id":"refs/heads/main","displayId":"main"}],"isLastPage":true}`))
			return
		case request.Method == http.MethodPost && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/branches":
			t.Fatalf("branch create endpoint must not be called in dry-run mode")
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"--json", "--dry-run", "branch", "create", "feature/demo", "--start-point", "master"})

	if err := command.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if !strings.Contains(buffer.String(), `"planning_mode": "stateful"`) || !strings.Contains(buffer.String(), `"predicted_action": "create"`) {
		t.Fatalf("expected stateful create prediction, got: %s", buffer.String())
	}
}

func TestBranchDefaultSetDryRunStatefulNoop(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/repos":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"values":[{"slug":"demo","name":"demo","project":{"key":"TEST"}}],"isLastPage":true}`))
			return
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/default-branch":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"id":"refs/heads/master","displayId":"master"}`))
			return
		case request.Method == http.MethodPut && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/default-branch":
			t.Fatalf("set default endpoint must not be called in dry-run mode")
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"--json", "--dry-run", "branch", "default", "set", "master"})

	if err := command.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if !strings.Contains(buffer.String(), `"predicted_action": "no-op"`) {
		t.Fatalf("expected no-op prediction, got: %s", buffer.String())
	}
}

func TestTagCreateDryRunStateful(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/repos":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"values":[{"slug":"demo","name":"demo","project":{"key":"TEST"}}],"isLastPage":true}`))
			return
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/tags":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"values":[{"id":"refs/tags/v1.0.0","displayId":"v1.0.0"}],"isLastPage":true}`))
			return
		case request.Method == http.MethodPost && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/tags":
			t.Fatalf("tag create endpoint must not be called in dry-run mode")
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"--json", "--dry-run", "tag", "create", "v1.2.3", "--start-point", "master"})

	if err := command.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if !strings.Contains(buffer.String(), `"planning_mode": "stateful"`) || !strings.Contains(buffer.String(), `"predicted_action": "create"`) {
		t.Fatalf("expected stateful create prediction, got: %s", buffer.String())
	}
}

func TestReviewerConditionCreateDryRunStateful(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/PRJ/permissions/users":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"values":[{"user":{"name":"alice"}}],"isLastPage":true}`))
			return
		case request.Method == http.MethodGet && request.URL.Path == "/rest/default-reviewers/latest/projects/PRJ/conditions":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`[]`))
			return
		case request.Method == http.MethodPost && request.URL.Path == "/rest/default-reviewers/latest/projects/PRJ/conditions":
			t.Fatalf("reviewer condition create endpoint must not be called in dry-run mode")
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"--json", "--dry-run", "reviewer", "condition", "create", `{"requiredApprovals":1}`, "--project", "PRJ"})

	if err := command.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if !strings.Contains(buffer.String(), `"predicted_action": "create"`) {
		t.Fatalf("expected create prediction, got: %s", buffer.String())
	}
}

func TestProjectCreateDryRunStateful(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte(`{"errors":[{"message":"name is required"}]}`))
			return
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/PRJ":
			writer.WriteHeader(http.StatusNotFound)
			_, _ = writer.Write([]byte(`{"errors":[{"message":"not found"}]}`))
			return
		case request.Method == http.MethodPost && request.URL.Path == "/rest/api/latest/projects":
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte(`{"errors":[{"message":"name is required"}]}`))
			return
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"--json", "--dry-run", "project", "create", "PRJ", "--name", "Project"})

	if err := command.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if !strings.Contains(buffer.String(), `"predicted_action": "create"`) {
		t.Fatalf("expected create prediction, got: %s", buffer.String())
	}
}

func TestPRBuildStatusCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")

	const prPath = "/rest/api/latest/projects/TEST/repos/demo/pull-requests/42"
	const prPathNoCommit = "/rest/api/latest/projects/TEST/repos/demo/pull-requests/43"
	const buildStatusPath = "/rest/build-status/latest/commits/abc123"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json;charset=UTF-8")
		switch r.URL.Path {
		case prPath:
			_, _ = w.Write([]byte(`{"id":42,"title":"My PR","state":"OPEN","open":true,"closed":false,"fromRef":{"displayId":"feature/x","latestCommit":"abc123"},"toRef":{"displayId":"main"}}`))
		case prPathNoCommit:
			_, _ = w.Write([]byte(`{"id":43,"title":"NoCom PR","state":"OPEN","open":true,"closed":false,"fromRef":{"displayId":"branch"},"toRef":{"displayId":"main"}}`))
		case buildStatusPath:
			if r.URL.Query().Get("start") == "0" {
				_, _ = w.Write([]byte(`{"values":[{"key":"ci/main","state":"SUCCESSFUL","url":"https://ci.example/1","name":"CI"}],"isLastPage":true}`))
			} else {
				http.NotFound(w, r)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	tests := []struct {
		name          string
		args          []string
		expectSnippet string
		expectError   bool
	}{
		{
			name:          "human output with statuses",
			args:          []string{"pr", "build", "status", "42"},
			expectSnippet: "SUCCESSFUL",
		},
		{
			name:          "json output",
			args:          []string{"--json", "pr", "build", "status", "42"},
			expectSnippet: `"statuses"`,
		},
		{
			name:        "no commit on PR returns error",
			args:        []string{"pr", "build", "status", "43"},
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := NewRootCommand()
			buf := &bytes.Buffer{}
			cmd.SetOut(buf)
			cmd.SetErr(buf)
			cmd.SetArgs(tc.args)

			err := cmd.Execute()
			if tc.expectError {
				if err == nil {
					t.Fatalf("expected error, got none (output: %s)", buf.String())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v (output: %s)", err, buf.String())
			}
			if !strings.Contains(buf.String(), tc.expectSnippet) {
				t.Fatalf("expected %q in output, got: %s", tc.expectSnippet, buf.String())
			}
		})
	}

	// loadConfig fails (invalid env var) → covers error return after loadConfig
	t.Run("loadConfig error", func(t *testing.T) {
		t.Setenv("BB_INSECURE_SKIP_VERIFY", "notabool")
		cmd := NewRootCommand()
		buf := &bytes.Buffer{}
		cmd.SetOut(buf)
		cmd.SetErr(buf)
		cmd.SetArgs([]string{"pr", "build", "status", "42"})
		if err := cmd.Execute(); err == nil {
			t.Fatal("expected error for invalid BB_INSECURE_SKIP_VERIFY, got none")
		}
	})

	// no repo slug and no --repo flag → resolvePullRequestRepositoryReference fails
	t.Run("missing repo slug", func(t *testing.T) {
		t.Setenv("BITBUCKET_REPO_SLUG", "")
		cmd := NewRootCommand()
		buf := &bytes.Buffer{}
		cmd.SetOut(buf)
		cmd.SetErr(buf)
		cmd.SetArgs([]string{"pr", "build", "status", "42"})
		if err := cmd.Execute(); err == nil {
			t.Fatal("expected error for missing repo slug, got none")
		}
	})

	// Empty build statuses response
	emptyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json;charset=UTF-8")
		switch r.URL.Path {
		case prPath:
			_, _ = w.Write([]byte(`{"id":42,"title":"My PR","state":"OPEN","open":true,"closed":false,"fromRef":{"displayId":"feature/x","latestCommit":"abc123"},"toRef":{"displayId":"main"}}`))
		case buildStatusPath:
			_, _ = w.Write([]byte(`{"values":[],"isLastPage":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer emptyServer.Close()

	t.Setenv("BITBUCKET_URL", emptyServer.URL)

	emptyCmd := NewRootCommand()
	emptyBuf := &bytes.Buffer{}
	emptyCmd.SetOut(emptyBuf)
	emptyCmd.SetErr(emptyBuf)
	emptyCmd.SetArgs([]string{"pr", "build", "status", "42"})
	if err := emptyCmd.Execute(); err != nil {
		t.Fatalf("empty statuses: unexpected error: %v", err)
	}
	if !strings.Contains(emptyBuf.String(), "No build statuses found") {
		t.Fatalf("expected 'No build statuses found', got: %s", emptyBuf.String())
	}
}
