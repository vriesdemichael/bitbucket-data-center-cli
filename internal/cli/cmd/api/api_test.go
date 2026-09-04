package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

func newTestDependencies(serverURL string, jsonMode bool, dryRun bool) Dependencies {
	return Dependencies{
		JSONEnabled:   func() bool { return jsonMode },
		DryRunEnabled: func() bool { return dryRun },
		LoadConfig: func(config.Overrides) (config.AppConfig, error) {
			return config.AppConfig{
				BitbucketURL:   serverURL,
				BitbucketToken: "test-token",
			}, nil
		},
		// The real writer, not a hand-rolled envelope. This double used to
		// build its own, which meant the tests asserted against a copy that
		// drifted: it was still emitting the "version" field ADR-064 removed,
		// so it proved the double worked rather than the command did.
		WriteJSON: jsonoutput.Write,
	}
}

func TestApiGetSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/rest/api/1.0/projects" {
			t.Errorf("expected path /rest/api/1.0/projects, got %s", r.URL.Path)
		}
		if r.URL.Query().Get("limit") != "10" {
			t.Errorf("expected limit=10, got %s", r.URL.Query().Get("limit"))
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("expected bearer auth, got %s", r.Header.Get("Authorization"))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"size":1,"values":[{"key":"PRJ","name":"Project"}]}`))
	}))
	defer server.Close()

	deps := newTestDependencies(server.URL, false, false)
	cmd := New(deps)

	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"/rest/api/1.0/projects", "-X", "GET", "-f", "limit=10"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, `"key": "PRJ"`) {
		t.Fatalf("expected formatted output containing PRJ, got: %s", output)
	}
}

func TestApiGetJSONEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"demo"}`))
	}))
	defer server.Close()

	deps := newTestDependencies(server.URL, true, false)
	cmd := New(deps)

	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"/rest/api/1.0/projects/PRJ"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("failed to decode JSON envelope: %v\nOutput: %s", err, buf.String())
	}

	if _, present := env["version"]; present {
		t.Fatalf("the envelope still carries a contract version: %v", env)
	}
	data, ok := env["data"].(map[string]any)
	if !ok || data["name"] != "demo" {
		t.Fatalf("expected data.name demo, got %v", env["data"])
	}
}

func TestApiPostWithTypedAndRawFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("failed to decode payload: %v", err)
		}

		if payload["name"] != "feature/x" {
			t.Errorf("expected name=feature/x, got %v", payload["name"])
		}
		if payload["active"] != true {
			t.Errorf("expected active=true, got %v", payload["active"])
		}
		if payload["count"] != float64(42) {
			t.Errorf("expected count=42, got %v", payload["count"])
		}
		if payload["ratio"] != 3.14 {
			t.Errorf("expected ratio=3.14, got %v", payload["ratio"])
		}
		if payload["nothing"] != nil {
			t.Errorf("expected nothing=nil, got %v", payload["nothing"])
		}
		nested, ok := payload["meta"].(map[string]any)
		if !ok || nested["env"] != "prod" {
			t.Errorf("expected meta.env=prod, got %v", payload["meta"])
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":123,"status":"created"}`))
	}))
	defer server.Close()

	deps := newTestDependencies(server.URL, false, false)
	cmd := New(deps)

	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"/rest/api/1.0/branches",
		"-X", "POST",
		"-f", "name=feature/x",
		"-F", "active=true",
		"-F", "count=42",
		"-F", "ratio=3.14",
		"-F", "nothing=null",
		"-F", `meta={"env":"prod"}`,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(buf.String(), `"status": "created"`) {
		t.Fatalf("expected created status, got: %s", buf.String())
	}
}

func TestApiPostWithInputFileAndStdin(t *testing.T) {
	tempDir := t.TempDir()
	inputPath := filepath.Join(tempDir, "input.json")
	if err := os.WriteFile(inputPath, []byte(`{"key":"val-from-file"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer server.Close()

	deps := newTestDependencies(server.URL, false, false)

	// 1. Input file
	{
		cmd := New(deps)
		buf := &bytes.Buffer{}
		cmd.SetOut(buf)
		cmd.SetErr(buf)
		cmd.SetArgs([]string{"/rest/api/1.0/test", "--method", "POST", "--input", inputPath})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(buf.String(), "val-from-file") {
			t.Fatalf("expected file content, got: %s", buf.String())
		}
	}

	// 2. Stdin input
	{
		cmd := New(deps)
		buf := &bytes.Buffer{}
		cmd.SetOut(buf)
		cmd.SetErr(buf)
		cmd.SetIn(strings.NewReader(`{"key":"val-from-stdin"}`))
		cmd.SetArgs([]string{"/rest/api/1.0/test", "-X", "POST", "--input", "-"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(buf.String(), "val-from-stdin") {
			t.Fatalf("expected stdin content, got: %s", buf.String())
		}
	}
}

func TestApiFieldWithAtFile(t *testing.T) {
	tempDir := t.TempDir()
	jsonFile := filepath.Join(tempDir, "data.json")
	if err := os.WriteFile(jsonFile, []byte(`{"nested":"value"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	txtFile := filepath.Join(tempDir, "text.txt")
	if err := os.WriteFile(txtFile, []byte(`simple text content`), 0o600); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		_ = json.Unmarshal(body, &payload)

		nested, ok := payload["json_data"].(map[string]any)
		if !ok || nested["nested"] != "value" {
			t.Errorf("expected json_data.nested=value, got %v", payload["json_data"])
		}
		if payload["text_data"] != "simple text content" {
			t.Errorf("expected text_data content, got %v", payload["text_data"])
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	deps := newTestDependencies(server.URL, false, false)
	cmd := New(deps)
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"/rest/api/1.0/test",
		"-F", "json_data=@" + jsonFile,
		"-F", "text_data=@" + txtFile,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApiDryRun(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	// 1. Dry run on GET should succeed
	{
		deps := newTestDependencies(server.URL, false, true)
		cmd := New(deps)
		buf := &bytes.Buffer{}
		cmd.SetOut(buf)
		cmd.SetErr(buf)
		cmd.SetArgs([]string{"/rest/api/1.0/projects"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("expected GET to succeed under dry-run, got error: %v", err)
		}
	}

	// 2. Dry run on POST/PUT/DELETE should be refused
	mutatingMethods := []string{"POST", "PUT", "DELETE", "PATCH"}
	for _, m := range mutatingMethods {
		deps := newTestDependencies(server.URL, false, true)
		cmd := New(deps)
		buf := &bytes.Buffer{}
		cmd.SetOut(buf)
		cmd.SetErr(buf)
		cmd.SetArgs([]string{"/rest/api/1.0/projects", "-X", m})

		err := cmd.Execute()
		if err == nil {
			t.Fatalf("expected %s under dry-run to be refused", m)
		}
		if !strings.Contains(err.Error(), "--dry-run: refusing mutating") {
			t.Fatalf("expected refusal error for %s, got: %v", m, err)
		}
	}
}

func TestApiCustomHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Audit-Reason") != "ticket-1234" {
			t.Errorf("expected X-Audit-Reason header, got %s", r.Header.Get("X-Audit-Reason"))
		}
		if r.Header.Get("X-Custom") != "val" {
			t.Errorf("expected X-Custom header, got %s", r.Header.Get("X-Custom"))
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	deps := newTestDependencies(server.URL, false, false)
	cmd := New(deps)
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"/rest/api/1.0/projects",
		"-H", "X-Audit-Reason: ticket-1234",
		"-H", "X-Custom:val",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApiInvalidArguments(t *testing.T) {
	deps := newTestDependencies("http://example.local", false, false)

	// Missing argument
	{
		cmd := New(deps)
		cmd.SetArgs([]string{})
		if err := cmd.Execute(); err == nil {
			t.Fatal("expected error on missing path argument")
		}
	}

	// Invalid header format
	{
		cmd := New(deps)
		cmd.SetArgs([]string{"/rest/api/1.0/test", "-H", "InvalidHeaderWithoutColon"})
		if err := cmd.Execute(); err == nil {
			t.Fatal("expected error on invalid header format")
		}
	}

	// Invalid field format
	{
		cmd := New(deps)
		cmd.SetArgs([]string{"/rest/api/1.0/test", "-f", "InvalidFieldWithoutEquals"})
		if err := cmd.Execute(); err == nil {
			t.Fatal("expected error on invalid field format")
		}
	}

	// Missing input file
	{
		cmd := New(deps)
		cmd.SetArgs([]string{"/rest/api/1.0/test", "--input", "non-existent-file.json"})
		if err := cmd.Execute(); err == nil {
			t.Fatal("expected error on missing input file")
		}
	}

	// Missing @file in field
	{
		cmd := New(deps)
		cmd.SetArgs([]string{"/rest/api/1.0/test", "-F", "data=@non-existent-file.json"})
		if err := cmd.Execute(); err == nil {
			t.Fatal("expected error on missing field @file")
		}
	}
}

func TestApiNonJSONOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("plain text response line 1\nline 2"))
	}))
	defer server.Close()

	// Human mode
	{
		deps := newTestDependencies(server.URL, false, false)
		cmd := New(deps)
		buf := &bytes.Buffer{}
		cmd.SetOut(buf)
		cmd.SetErr(buf)
		cmd.SetArgs([]string{"/raw/file.txt"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(buf.String(), "plain text response line 1") {
			t.Fatalf("expected plain text, got: %s", buf.String())
		}
	}

	// Machine mode
	{
		deps := newTestDependencies(server.URL, true, false)
		cmd := New(deps)
		buf := &bytes.Buffer{}
		cmd.SetOut(buf)
		cmd.SetErr(buf)
		cmd.SetArgs([]string{"/raw/file.txt"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(buf.String(), `"bbVersion"`) {
			t.Fatalf("expected machine envelope, got: %s", buf.String())
		}
	}
}

func TestApiDefaults(t *testing.T) {
	cmd := New(Dependencies{})
	if cmd == nil {
		t.Fatal("expected non-nil command from default dependencies")
	}
}

func TestApiLoadConfigError(t *testing.T) {
	deps := Dependencies{
		LoadConfig: func(config.Overrides) (config.AppConfig, error) {
			return config.AppConfig{}, apperrors.New(apperrors.KindValidation, "forced config error", nil)
		},
	}
	cmd := New(deps)
	cmd.SetArgs([]string{"/rest/api/1.0/test"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected config error")
	}
	if !strings.Contains(err.Error(), "forced config error") {
		t.Fatalf("expected forced config error, got: %v", err)
	}
}

func TestApiEmptyPath(t *testing.T) {
	deps := newTestDependencies("http://example.local", false, false)
	cmd := New(deps)
	cmd.SetArgs([]string{"   "})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error on whitespace-only path")
	}
	if !strings.Contains(err.Error(), "path cannot be empty") {
		t.Fatalf("expected path cannot be empty, got: %v", err)
	}
}

func TestApiEmptyFieldKey(t *testing.T) {
	deps := newTestDependencies("http://example.local", false, false)
	cmd := New(deps)
	cmd.SetArgs([]string{"/rest/api/1.0/test", "-f", "=value"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error on empty field key")
	}
	if !strings.Contains(err.Error(), "empty key in field") {
		t.Fatalf("expected empty key in field error, got: %v", err)
	}
}

func TestApiPaginatedErrors(t *testing.T) {
	// 1. Pagination server error on page 2
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"size":1,"isLastPage":false,"nextPageStart":1,"values":[{"id":1}]}`))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"errors":[{"message":"server error"}]}`))
	}))
	defer server.Close()

	deps := newTestDependencies(server.URL, false, false)
	cmd := New(deps)
	cmd.SetArgs([]string{"/rest/api/1.0/paged", "--paginate"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error on failed pagination page")
	}
}

func TestApiPaginatedInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not a json payload"))
	}))
	defer server.Close()

	deps := newTestDependencies(server.URL, false, false)
	cmd := New(deps)
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"/rest/api/1.0/raw", "--paginate"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "not a json payload") {
		t.Fatalf("expected raw text fallback on invalid JSON pagination, got: %s", buf.String())
	}
}

func TestApiHTMLResponseReturnsAuthenticationError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html;charset=UTF-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<!DOCTYPE html><html><head><title>Bitbucket Login</title></head><body><h1>Log In</h1></body></html>`))
	}))
	defer server.Close()

	deps := newTestDependencies(server.URL, false, false)
	cmd := New(deps)
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"/rest/api/1.0/projects/PRJ"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error on HTML login response, got nil")
	}
	if !apperrors.IsKind(err, apperrors.KindAuthentication) {
		t.Fatalf("expected authentication error, got kind %v (%v)", apperrors.KindOf(err), err)
	}
	if !strings.Contains(err.Error(), "expected JSON, got text/html") {
		t.Fatalf("expected actionable message about text/html, got: %v", err)
	}
}

func TestApiPaginatedHTMLResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html;charset=UTF-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<html>Login Page</html>`))
	}))
	defer server.Close()

	deps := newTestDependencies(server.URL, false, false)
	cmd := New(deps)
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"/rest/api/1.0/projects", "--paginate"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error on paginated HTML response, got nil")
	}
	if !apperrors.IsKind(err, apperrors.KindAuthentication) {
		t.Fatalf("expected authentication error on paginated HTML, got %v", err)
	}
}

func TestApiMangledPathSanitization(t *testing.T) {
	var requestedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	tests := []struct {
		name         string
		inputArg     string
		expectedPath string
	}{
		{
			name:         "msys2 drive with program files git prefix",
			inputArg:     "/C:/Program Files/Git/rest/api/1.0/projects/PROJ/repos/repo",
			expectedPath: "/rest/api/1.0/projects/PROJ/repos/repo",
		},
		{
			name:         "windows backslash with git prefix",
			inputArg:     `C:\Program Files\Git\rest\api\1.0\projects\PROJ`,
			expectedPath: "/rest/api/1.0/projects/PROJ",
		},
		{
			name:         "short msys drive prefix",
			inputArg:     "/c/rest/api/1.0/users",
			expectedPath: "/rest/api/1.0/users",
		},
		{
			name:         "custom plugin with drive letter",
			inputArg:     "/C:/plugins/servlet/custom",
			expectedPath: "/plugins/servlet/custom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := newTestDependencies(server.URL, false, false)
			cmd := New(deps)
			outBuf := &bytes.Buffer{}
			errBuf := &bytes.Buffer{}
			cmd.SetOut(outBuf)
			cmd.SetErr(errBuf)
			cmd.SetArgs([]string{tt.inputArg})

			if err := cmd.Execute(); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if requestedPath != tt.expectedPath {
				t.Fatalf("expected requested path %q, got %q", tt.expectedPath, requestedPath)
			}
			if !strings.Contains(errBuf.String(), "warning: path") || !strings.Contains(errBuf.String(), "MSYS_NO_PATHCONV=1") {
				t.Fatalf("expected warning naming MSYS_NO_PATHCONV on stderr, got: %s", errBuf.String())
			}
		})
	}
}

// isolateStoredConfig points config lookups at an empty temporary file so a
// test never reads — or authenticates against — the developer's real Bitbucket
// hosts.
func isolateStoredConfig(t *testing.T) {
	t.Helper()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("hosts: {}\n"), 0o600); err != nil {
		t.Fatalf("write stored config: %v", err)
	}
	t.Setenv("BB_CONFIG_PATH", configPath)
}

func TestApiHostFlagOverride(t *testing.T) {
	isolateStoredConfig(t)

	var serverHit bool
	customServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverHit = true
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"host":"custom"}`))
	}))
	defer customServer.Close()

	// Default dependencies point to a fake dummy server that fails
	deps := newTestDependencies("http://default-non-existent.example", false, false)
	cmd := New(deps)
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"/rest/api/1.0/projects", "--host", customServer.URL})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !serverHit {
		t.Fatal("expected custom server to be reached via --host flag")
	}
	if !strings.Contains(buf.String(), `"host": "custom"`) {
		t.Fatalf("expected custom server response, got: %s", buf.String())
	}
}

// TestApiHostFlagDoesNotFallBackToDefaultHost pins the whole point of --host: a
// host that is not in the stored config must still be the host that is called.
// Resolving stored credentials leniently returns the default server's entire
// profile, URL included, so the command used to answer confidently from a
// server the caller never named.
func TestApiHostFlagDoesNotFallBackToDefaultHost(t *testing.T) {
	var defaultHit, customHit bool

	defaultServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defaultHit = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"who":"default"}`))
	}))
	defer defaultServer.Close()

	customServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		customHit = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"who":"custom"}`))
	}))
	defer customServer.Close()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	storedConfig := fmt.Sprintf(
		"default_host: %s\nhosts:\n  %s:\n    url: %s\n    username: someone\ninsecure_secrets:\n  %s:\n    token: stored-token\n",
		defaultServer.URL, defaultServer.URL, defaultServer.URL, defaultServer.URL)
	if err := os.WriteFile(configPath, []byte(storedConfig), 0o600); err != nil {
		t.Fatalf("write stored config: %v", err)
	}
	t.Setenv("BB_CONFIG_PATH", configPath)

	deps := newTestDependencies(defaultServer.URL, false, false)
	cmd := New(deps)
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"/rest/api/1.0/projects", "--host", customServer.URL})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if defaultHit || !customHit {
		t.Fatalf("--host must target the requested server, not the stored default (default hit=%v, custom hit=%v): %s",
			defaultHit, customHit, buf.String())
	}
}

// TestApiHostFlagLeavesEnvironmentAlone pins how --host reaches the config
// load: as an argument, not as a write to the process environment.
//
// Steering a load by exporting BITBUCKET_URL works, but it outlives the call —
// it retargets everything the process does afterwards and is inherited by any
// subprocess bb spawns.
func TestApiHostFlagLeavesEnvironmentAlone(t *testing.T) {
	isolateStoredConfig(t)
	t.Setenv("BITBUCKET_URL", "https://original.example")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	var seen config.Overrides
	var envDuringLoad string
	deps := newTestDependencies(server.URL, false, false)
	deps.LoadConfig = func(overrides config.Overrides) (config.AppConfig, error) {
		seen = overrides
		envDuringLoad = os.Getenv("BITBUCKET_URL")
		return config.AppConfig{BitbucketURL: server.URL, BitbucketToken: "test-token"}, nil
	}

	cmd := New(deps)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"/rest/api/1.0/projects", "--host", server.URL})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if seen.Host != server.URL {
		t.Fatalf("expected the host to be passed to the load, got %q", seen.Host)
	}
	if envDuringLoad != "https://original.example" {
		t.Fatalf("BITBUCKET_URL must not be rewritten during the load, got %q", envDuringLoad)
	}
	if got := os.Getenv("BITBUCKET_URL"); got != "https://original.example" {
		t.Fatalf("BITBUCKET_URL must be untouched after the command, got %q", got)
	}
}

// TestApiSanitizePreservesLegitimatePaths guards the MSYS2 recovery from eating
// real endpoints. `bb api` reaches plugin paths that contain "/rest/" partway
// through, and a heuristic matching on words like "git" truncated them.
func TestApiSanitizePreservesLegitimatePaths(t *testing.T) {
	for _, path := range []string{
		"/plugins/servlet/git-lfs/rest/objects/batch",
		"/git/rest/api/1.0/projects",
		"/bitbucket/rest/api/1.0/projects",
		"/plugins/servlet/applinks/whoami",
		"/rest/api/1.0/projects",
		"rest/api/1.0/projects",
		"/status",
	} {
		got, mangled := sanitizeMangledPath(path)
		if mangled || got != path {
			t.Errorf("path %q must be left alone, got %q (mangled=%v)", path, got, mangled)
		}
	}
}

// TestApiHTMLResponseAllowedOutsideRest keeps the login-page check to the REST
// API. Plugin and servlet endpoints legitimately render HTML.
func TestApiHTMLResponseAllowedOutsideRest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html;charset=UTF-8")
		_, _ = w.Write([]byte(`<html><body>plugin page</body></html>`))
	}))
	defer server.Close()

	deps := newTestDependencies(server.URL, false, false)
	cmd := New(deps)
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"/plugins/servlet/custom-report"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("HTML from a non-REST path must not be rejected: %v", err)
	}
	if !strings.Contains(buf.String(), "plugin page") {
		t.Fatalf("expected the plugin page body, got: %s", buf.String())
	}
}
