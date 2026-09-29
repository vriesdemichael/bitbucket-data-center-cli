package api

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// credentialBeacon is a server that is not Bitbucket: it answers every request
// alike and records the Authorization it was sent, so a test can see which host
// a request reached and which credential went with it.
type credentialBeacon struct {
	*httptest.Server

	mu            sync.Mutex
	hits          int
	authorization string
}

// mock-inventory: routing-beacon — the reply is never read as Bitbucket's; the subject is which host a request reached and what Authorization went with it.
func newCredentialBeacon(t *testing.T) *credentialBeacon {
	t.Helper()

	beacon := &credentialBeacon{}
	beacon.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		beacon.mu.Lock()
		beacon.hits++
		beacon.authorization = r.Header.Get("Authorization")
		beacon.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(beacon.Close)

	return beacon
}

func (beacon *credentialBeacon) seen() (int, string) {
	beacon.mu.Lock()
	defer beacon.mu.Unlock()

	return beacon.hits, beacon.authorization
}

// storeCredentials stores a token for each host, the first as the default, and
// clears every credential the environment could supply, so the only
// credentials in play are the stored ones. The policy and workspace tiers are
// pointed at files that are not there, so this machine's own cannot steer the
// load.
func storeCredentials(t *testing.T, tokens ...[2]string) {
	t.Helper()

	for _, key := range []string{
		"BITBUCKET_URL", "BITBUCKET_TOKEN", "BITBUCKET_USERNAME", "BITBUCKET_USER", "BITBUCKET_PASSWORD",
		"ADMIN_USER", "ADMIN_PASSWORD", "BB_REQUIRE_KEYRING", "BB_DISABLE_STORED_CONFIG",
	} {
		t.Setenv(key, "")
	}
	absent := t.TempDir()
	t.Setenv("BB_SYSTEM_CONFIG_PATH", filepath.Join(absent, "policy.yaml"))
	t.Setenv("BB_WORKSPACE_CONFIG_PATH", filepath.Join(absent, "workspace.yaml"))

	hosts, secrets := "hosts:\n", "insecure_secrets:\n"
	for _, stored := range tokens {
		hosts += fmt.Sprintf("  %s:\n    url: %s\n", stored[0], stored[0])
		secrets += fmt.Sprintf("  %s:\n    token: %s\n", stored[0], stored[1])
	}

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	storedConfig := "default_host: " + tokens[0][0] + "\n" + hosts + secrets
	if err := os.WriteFile(configPath, []byte(storedConfig), 0o600); err != nil {
		t.Fatalf("write stored config: %v", err)
	}
	t.Setenv("BB_CONFIG_PATH", configPath)
}

// An endpoint given as a URL names its host, and the configuration is loaded
// for that host. It used to be loaded for the default host, so `bb api
// http://elsewhere/...` sent the default host's credential to elsewhere -- and
// so did naming the default with --host alongside it -- while a URL on another
// stored host got the default's credential rather than its own.
func TestApiEndpointURLGetsOnlyItsOwnHostsCredential(t *testing.T) {
	defaultHost := newCredentialBeacon(t)
	otherStored := newCredentialBeacon(t)
	foreign := newCredentialBeacon(t)
	storeCredentials(t, [2]string{defaultHost.URL, "default-token"}, [2]string{otherStored.URL, "other-token"})

	run := func(dryRun bool, args ...string) error {
		cmd := New(Dependencies{DryRunEnabled: func() bool { return dryRun }})
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs(args)
		return cmd.Execute()
	}

	// A GET streams through the downloader and a POST does not; both are asked.
	for _, method := range []string{"GET", "POST"} {
		if err := run(false, foreign.URL+"/rest/api/1.0/projects", "-X", method); err != nil {
			t.Fatalf("%s to a URL on a host with nothing stored: %v", method, err)
		}
	}
	if hits, authorization := foreign.seen(); hits != 2 || authorization != "" {
		t.Fatalf("a host with nothing stored must be reached with no credential, got %d hits and Authorization %q", hits, authorization)
	}

	if err := run(false, otherStored.URL+"/rest/api/1.0/projects"); err != nil {
		t.Fatalf("a URL on another stored host: %v", err)
	}
	if hits, authorization := otherStored.seen(); hits != 1 || authorization != "Bearer other-token" {
		t.Fatalf("another stored host must get its own credential, got %d hits and Authorization %q", hits, authorization)
	}

	// Refused before anything is sent, and before a dry run is previewed: the
	// real run would refuse it, so a preview saying it would be sent is wrong.
	for _, dryRun := range []bool{false, true} {
		err := run(dryRun, foreign.URL+"/rest/api/1.0/projects", "-X", "POST", "--host", defaultHost.URL)
		if !apperrors.IsKind(err, apperrors.KindValidation) || !strings.Contains(err.Error(), "different servers") {
			t.Fatalf("--host and a URL on another server must be refused (dry run %v), got %v", dryRun, err)
		}
	}
	if hits, _ := foreign.seen(); hits != 2 {
		t.Fatalf("a refused request must not reach the other host, got %d hits", hits)
	}

	// The default host's own URL, with --host or without, still carries its
	// credential.
	for _, args := range [][]string{
		{defaultHost.URL + "/rest/api/1.0/projects"},
		{defaultHost.URL + "/rest/api/1.0/projects", "--host", defaultHost.URL},
	} {
		if err := run(false, args...); err != nil {
			t.Fatalf("bb api %v: %v", args, err)
		}
	}
	if hits, authorization := defaultHost.seen(); hits != 2 || authorization != "Bearer default-token" {
		t.Fatalf("the default host must get its own credential, got %d hits and Authorization %q", hits, authorization)
	}
}

// allowed_hosts refuses a host outside the list before any request is made, and
// a URL endpoint names a host. It used to be checked against the default host
// instead, and the request went to the URL's host regardless.
func TestApiEndpointURLIsHeldToAllowedHosts(t *testing.T) {
	defaultHost := newCredentialBeacon(t)
	foreign := newCredentialBeacon(t)
	storeCredentials(t, [2]string{defaultHost.URL, "default-token"})

	policyPath := filepath.Join(t.TempDir(), "policy.yaml")
	if err := os.WriteFile(policyPath, []byte(fmt.Sprintf("allowed_hosts:\n  - %q\n", defaultHost.URL)), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	t.Setenv("BB_SYSTEM_CONFIG_PATH", policyPath)

	// allowed_hosts compares host names, not ports, so the other server is
	// reached by another name for the same loopback.
	elsewhere := strings.Replace(foreign.URL, "127.0.0.1", "localhost", 1)

	cmd := New(Dependencies{})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{elsewhere + "/rest/api/1.0/projects"})

	err := cmd.Execute()
	if !apperrors.IsKind(err, apperrors.KindAuthorization) || !strings.Contains(err.Error(), "not permitted by administrative policy") {
		t.Fatalf("a URL on a host outside allowed_hosts must be refused, got %v", err)
	}
	if hits, _ := foreign.seen(); hits != 0 {
		t.Fatalf("a refused host must not be reached, got %d hits", hits)
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

// TestAMutatingRequestUnderDryRunIsPreviewedNotRefused: bb api cannot ask
// Bitbucket whether an arbitrary request would go through, so it shows the
// request it would send, predicted, and sends nothing. Refusing it used to be
// the answer, and under ADR-096 a refusal reads as a verdict that the real run
// fails -- which it would not.
func TestAMutatingRequestUnderDryRunIsPreviewedNotRefused(t *testing.T) {
	t.Parallel()

	for method, action := range map[string]string{"POST": "create", "PUT": "update", "PATCH": "update", "DELETE": "delete"} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()

			// A configuration pointing nowhere: a request sent would fail, so a
			// preview that succeeds sent none.
			deps := newTestDependencies("http://127.0.0.1:1", true, true)
			var out bytes.Buffer
			cmd := New(deps)
			cmd.SetOut(&out)
			cmd.SetArgs([]string{"-X", method, "/rest/api/latest/projects/X"})
			if err := cmd.Execute(); err != nil {
				t.Fatalf("a %s under --dry-run failed: %v", method, err)
			}

			for _, want := range []string{`"preview": {`, `"tier": "predicted"`, `"action": "` + action + `"`, `"method": "` + method + `"`} {
				if !strings.Contains(out.String(), want) {
					t.Fatalf("the preview lacks %s:\n%s", want, out.String())
				}
			}
		})
	}
}
