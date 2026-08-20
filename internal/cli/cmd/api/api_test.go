package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
)

func newTestDependencies(serverURL string, jsonMode bool, dryRun bool) Dependencies {
	return Dependencies{
		JSONEnabled:   func() bool { return jsonMode },
		DryRunEnabled: func() bool { return dryRun },
		LoadConfig: func() (config.AppConfig, error) {
			return config.AppConfig{
				BitbucketURL:   serverURL,
				BitbucketToken: "test-token",
			}, nil
		},
		WriteJSON: func(w io.Writer, value any) error {
			envelope := map[string]any{
				"version": "v2",
				"data":    value,
				"meta": map[string]string{
					"contract": "bb.machine",
				},
			}
			enc := json.NewEncoder(w)
			enc.SetIndent("", "  ")
			return enc.Encode(envelope)
		},
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

	if env["version"] != "v2" {
		t.Fatalf("expected version v2, got %v", env["version"])
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

func TestApiPaginated(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		start := r.URL.Query().Get("start")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		if start == "" || start == "0" {
			_, _ = w.Write([]byte(`{
				"size": 2,
				"limit": 2,
				"isLastPage": false,
				"start": 0,
				"nextPageStart": 2,
				"values": [{"id": 1}, {"id": 2}]
			}`))
			return
		}

		if start == "2" {
			_, _ = w.Write([]byte(`{
				"size": 1,
				"limit": 2,
				"isLastPage": true,
				"start": 2,
				"values": [{"id": 3}]
			}`))
			return
		}

		t.Errorf("unexpected start parameter: %s", start)
	}))
	defer server.Close()

	deps := newTestDependencies(server.URL, true, false)
	cmd := New(deps)
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"/rest/api/1.0/groups", "--paginate"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if callCount != 2 {
		t.Fatalf("expected 2 page requests, got %d", callCount)
	}

	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("failed to decode json envelope: %v", err)
	}
	data := env["data"].(map[string]any)
	values := data["values"].([]any)
	if len(values) != 3 {
		t.Fatalf("expected 3 aggregated items, got %d: %v", len(values), values)
	}
	if data["isLastPage"] != true {
		t.Fatalf("expected isLastPage=true, got %v", data["isLastPage"])
	}
	if _, hasNext := data["nextPageStart"]; hasNext {
		t.Fatalf("expected nextPageStart to be removed in aggregated result")
	}
}

func TestApiPaginatedNonPagedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"single":"object"}`))
	}))
	defer server.Close()

	deps := newTestDependencies(server.URL, false, false)
	cmd := New(deps)
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"/rest/api/1.0/single", "--paginate"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(buf.String(), `"single": "object"`) {
		t.Fatalf("expected single object response, got: %s", buf.String())
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

func TestApiErrorResponses(t *testing.T) {
	testCases := []struct {
		name       string
		statusCode int
		body       string
		wantKind   apperrors.Kind
	}{
		{
			name:       "400 validation",
			statusCode: http.StatusBadRequest,
			body:       `{"errors":[{"message":"bad request"}]}`,
			wantKind:   apperrors.KindValidation,
		},
		{
			name:       "401 authentication",
			statusCode: http.StatusUnauthorized,
			body:       `{"errors":[{"message":"unauthorized"}]}`,
			wantKind:   apperrors.KindAuthentication,
		},
		{
			name:       "403 authorization",
			statusCode: http.StatusForbidden,
			body:       `{"errors":[{"message":"forbidden"}]}`,
			wantKind:   apperrors.KindAuthorization,
		},
		{
			name:       "404 not found",
			statusCode: http.StatusNotFound,
			body:       `{"errors":[{"message":"not found"}]}`,
			wantKind:   apperrors.KindNotFound,
		},
		{
			name:       "409 conflict",
			statusCode: http.StatusConflict,
			body:       `{"errors":[{"message":"conflict"}]}`,
			wantKind:   apperrors.KindConflict,
		},
		{
			name:       "500 internal",
			statusCode: http.StatusInternalServerError,
			body:       `{"errors":[{"message":"internal server error"}]}`,
			wantKind:   apperrors.KindTransient,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.statusCode)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			deps := newTestDependencies(server.URL, false, false)
			cmd := New(deps)
			cmd.SetArgs([]string{"/rest/api/1.0/test"})

			err := cmd.Execute()
			if err == nil {
				t.Fatalf("expected error for status %d", tc.statusCode)
			}
			if apperrors.KindOf(err) != tc.wantKind {
				t.Fatalf("expected kind %v, got %v (err: %v)", tc.wantKind, apperrors.KindOf(err), err)
			}
		})
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
		if !strings.Contains(buf.String(), `"contract": "bb.machine"`) {
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
		LoadConfig: func() (config.AppConfig, error) {
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

func TestApiTypedFieldQueryParamForGet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("active") != "true" {
			t.Errorf("expected active=true, got %s", r.URL.Query().Get("active"))
		}
		if r.URL.Query().Get("count") != "42" {
			t.Errorf("expected count=42, got %s", r.URL.Query().Get("count"))
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	deps := newTestDependencies(server.URL, false, false)
	cmd := New(deps)
	cmd.SetArgs([]string{"/rest/api/1.0/test", "-X", "GET", "-F", "active=true", "-F", "count=42"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApiEmptyResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	deps := newTestDependencies(server.URL, false, false)
	cmd := New(deps)
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"/rest/api/1.0/empty"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("expected empty output, got: %q", buf.String())
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
