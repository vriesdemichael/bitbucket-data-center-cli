package api

import (
	"bytes"
	"fmt"
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

func TestApiInvalidArguments(t *testing.T) {
	t.Parallel()

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

func TestApiDefaults(t *testing.T) {
	t.Parallel()

	cmd := New(Dependencies{})
	if cmd == nil {
		t.Fatal("expected non-nil command from default dependencies")
	}
}

func TestApiLoadConfigError(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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

// mock-inventory: transport-fault — the second page is made to fail, which no live instance can be asked for; the subject is that a walk interrupted halfway reports rather than returning the pages it got.
func TestApiPaginatedErrors(t *testing.T) {
	t.Parallel()

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

// mock-inventory: routing-beacon — the server answers only with which one it is; the subject is which host the request reached.
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
// mock-inventory: routing-beacon — two beacons, neither pretending to be Bitbucket; the subject is that --host beats the stored default, which needs two hosts to tell apart.
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
// mock-inventory: routing-beacon — the subject is that --host does not write BITBUCKET_URL into the process environment.
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
