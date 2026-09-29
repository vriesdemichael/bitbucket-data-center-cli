package cli

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
)

// credentialBeacon is a server that is not Bitbucket: it answers every request
// alike and records the Authorization it was sent, so a test can see which
// credential reached which host.
type credentialBeacon struct {
	*httptest.Server

	mu            sync.Mutex
	authorization []string
}

// mock-inventory: routing-beacon — the reply is never read as Bitbucket's; the subject is which credential a request to this host carried.
func newCredentialBeacon(t *testing.T) *credentialBeacon {
	t.Helper()

	beacon := &credentialBeacon{}
	beacon.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		beacon.mu.Lock()
		beacon.authorization = append(beacon.authorization, r.Header.Get("Authorization"))
		beacon.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(beacon.Close)

	return beacon
}

// seen is the Authorization of every request the beacon received.
func (beacon *credentialBeacon) seen() []string {
	beacon.mu.Lock()
	defer beacon.mu.Unlock()

	return append([]string(nil), beacon.authorization...)
}

// withNothingConfigured is a machine bb has never been set up on: no stored
// configuration, no host or credential in the environment, and no policy or
// workspace file.
func withNothingConfigured(t *testing.T) {
	t.Helper()

	for _, key := range []string{
		"BITBUCKET_URL", "BITBUCKET_TOKEN", "BITBUCKET_USERNAME", "BITBUCKET_USER", "BITBUCKET_PASSWORD",
		"ADMIN_USER", "ADMIN_PASSWORD", "BB_REQUIRE_KEYRING", "BB_DISABLE_STORED_CONFIG",
	} {
		t.Setenv(key, "")
	}
	absent := t.TempDir()
	t.Setenv("BB_CONFIG_PATH", filepath.Join(absent, "config.yaml"))
	t.Setenv("BB_SYSTEM_CONFIG_PATH", filepath.Join(absent, "policy.yaml"))
	t.Setenv("BB_WORKSPACE_CONFIG_PATH", filepath.Join(absent, "workspace.yaml"))
}

// withDefaultHostToken stores a token for host and makes it the default.
func withDefaultHostToken(t *testing.T, host, token string) {
	t.Helper()

	withNothingConfigured(t)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	stored := fmt.Sprintf("default_host: %s\nhosts:\n  %s:\n    url: %s\ninsecure_secrets:\n  %s:\n    token: %s\n",
		host, host, host, host, token)
	if err := os.WriteFile(configPath, []byte(stored), 0o600); err != nil {
		t.Fatalf("write stored config: %v", err)
	}
	t.Setenv("BB_CONFIG_PATH", configPath)
}

func runRoot(t *testing.T, args ...string) (string, error) {
	t.Helper()

	cmd := NewRootCommand()
	output := &bytes.Buffer{}
	cmd.SetOut(output)
	cmd.SetErr(output)
	cmd.SetArgs(args)
	err := cmd.Execute()

	return output.String(), err
}

// bb auth token-url exists to say where to make the first token, so it has to
// work before there is one: --host names the server, and nothing else is
// needed. It used to load the configuration without the host and fail with "no
// Bitbucket host configured". A host given bare is https, as everywhere else.
func TestAuthTokenURLNeedsNoLogin(t *testing.T) {
	withNothingConfigured(t)

	for host, want := range map[string]string{
		"https://bitbucket.acme.corp": "https://bitbucket.acme.corp/plugins/servlet/access-tokens/manage",
		"bitbucket.acme.corp":         "https://bitbucket.acme.corp/plugins/servlet/access-tokens/manage",
	} {
		output, err := runRoot(t, "auth", "token-url", "--host", host)
		if err != nil {
			t.Fatalf("auth token-url --host %s with nothing configured: %v\n%s", host, err, output)
		}
		if !strings.Contains(output, want) {
			t.Fatalf("auth token-url --host %s: want %s, got: %s", host, want, output)
		}
	}
}

// The identity lookup token-url makes goes to the host --host names, with the
// credential stored for that host and no other. It used to take the default
// host's.
func TestAuthTokenURLKeepsTheDefaultCredentialAtHome(t *testing.T) {
	defaultHost := newCredentialBeacon(t)
	named := newCredentialBeacon(t)
	withDefaultHostToken(t, defaultHost.URL, "default-token")

	output, err := runRoot(t, "auth", "token-url", "--host", named.URL)
	if err != nil {
		t.Fatalf("auth token-url --host: %v\n%s", err, output)
	}
	if !strings.Contains(output, named.URL+"/plugins/servlet/access-tokens/manage") {
		t.Fatalf("the token URL must be on the named host, got: %s", output)
	}
	for _, authorization := range named.seen() {
		if authorization != "" {
			t.Fatalf("the named host was sent a credential it was not stored for: %q", authorization)
		}
	}

	// Without --host the lookup goes to the default host with its own
	// credential, so the check above is of something that does get sent.
	if _, err := runRoot(t, "auth", "token-url"); err != nil {
		t.Fatalf("auth token-url: %v", err)
	}
	if seen := defaultHost.seen(); len(seen) == 0 || seen[0] != "Bearer default-token" {
		t.Fatalf("the default host must get its own credential, got %q", seen)
	}
}
