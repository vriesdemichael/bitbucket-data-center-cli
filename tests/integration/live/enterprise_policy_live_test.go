//go:build live

package live_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
)

// TestLiveEnterprisePolicyAllowedHosts verifies machine-level allowed_hosts policy
// against a live Bitbucket instance. It asserts that allowed hosts connect normally,
// while unlisted hosts are rejected with KindAuthorization before reaching the network.
func TestLiveEnterprisePolicyAllowedHosts(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	tempDir := t.TempDir()
	policyPath := filepath.Join(tempDir, "policy.yaml")
	t.Setenv("BB_SYSTEM_CONFIG_PATH", policyPath)

	// 1. Live harness host is explicitly allowed: command must succeed
	allowedYAML := fmt.Sprintf("allowed_hosts:\n  - %q\n", harness.config.BitbucketURL)
	if err := os.WriteFile(policyPath, []byte(allowedYAML), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	output, err := executeLiveCLI(t, "--json", "project", "list", "--limit", "1")
	if err != nil {
		t.Fatalf("expected allowed host to succeed against live server, got error: %v\noutput: %s", err, output)
	}

	// 2. Machine policy excludes live harness host: command must be rejected with KindAuthorization
	disallowedYAML := "allowed_hosts:\n  - \"https://other-bitbucket.corp.internal\"\n"
	if err := os.WriteFile(policyPath, []byte(disallowedYAML), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	output, err = executeLiveCLI(t, "--json", "project", "list", "--limit", "1")
	if err == nil {
		t.Fatalf("expected command to be refused by administrative policy, but it succeeded\noutput: %s", output)
	}
	if !apperrors.IsKind(err, apperrors.KindAuthorization) {
		t.Fatalf("expected KindAuthorization (exit code 3), got %v", err)
	}
	if !strings.Contains(err.Error(), "is not permitted by administrative policy") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

// TestLiveEnterprisePolicyInsecureSkipVerifyRefusal asserts that when machine policy
// disables insecure TLS skipping, any CLI invocation passing --insecure-skip-verify
// is halted before making any live HTTP requests.
func TestLiveEnterprisePolicyInsecureSkipVerifyRefusal(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	tempDir := t.TempDir()
	policyPath := filepath.Join(tempDir, "policy.yaml")
	t.Setenv("BB_SYSTEM_CONFIG_PATH", policyPath)

	policyYAML := "allow_insecure_skip_verify: false\n"
	if err := os.WriteFile(policyPath, []byte(policyYAML), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	output, err := executeLiveCLI(t, "--insecure-skip-verify", "project", "list", "--limit", "1")
	if err == nil {
		t.Fatalf("expected --insecure-skip-verify to be refused by policy, but it succeeded\noutput: %s", output)
	}
	if !apperrors.IsKind(err, apperrors.KindAuthorization) {
		t.Fatalf("expected KindAuthorization (exit code 3), got %v", err)
	}
	if !strings.Contains(err.Error(), "insecure TLS verification is disabled by administrative policy") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

// TestLiveWorkspaceConfigResolution asserts that .bb/config.yaml workspace configuration
// successfully scopes live CLI operations to the right default host and project key.
func TestLiveWorkspaceConfigResolution(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	tempDir := t.TempDir()
	wsPath := filepath.Join(tempDir, "config.yaml")
	wsYAML := fmt.Sprintf("default_host: %q\nproject_key: %q\n", harness.config.BitbucketURL, seeded.Key)
	if err := os.WriteFile(wsPath, []byte(wsYAML), 0o600); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}

	t.Setenv("BB_WORKSPACE_CONFIG_PATH", wsPath)
	t.Setenv("BITBUCKET_URL", "")
	t.Setenv("BITBUCKET_PROJECT_KEY", "")

	output, err := executeLiveCLI(t, "--json", "repo", "list", "--limit", "1")
	if err != nil {
		t.Fatalf("expected workspace config to resolve host and project against live server: %v\noutput: %s", err, output)
	}
	if !strings.Contains(output, repo.Slug) {
		t.Fatalf("expected repo slug in live output: %s", output)
	}
}

// TestLiveEnterpriseUpdateControls verifies that update execution respects
// administrative killswitches and custom mirror endpoints.
func TestLiveEnterpriseUpdateControls(t *testing.T) {
	tempDir := t.TempDir()
	policyPath := filepath.Join(tempDir, "policy.yaml")
	t.Setenv("BB_SYSTEM_CONFIG_PATH", policyPath)

	// 1. Env killswitch BB_DISABLE_UPDATE=1
	t.Setenv("BB_DISABLE_UPDATE", "1")
	output, err := executeLiveCLI(t, "--json", "update")
	if err == nil {
		t.Fatalf("expected update to be disabled by env killswitch, got output: %s", output)
	}
	if !apperrors.IsKind(err, apperrors.KindAuthorization) {
		t.Fatalf("expected KindAuthorization, got %v", err)
	}
	if !strings.Contains(err.Error(), "self-update is disabled by administrative policy") {
		t.Fatalf("unexpected error message: %v", err)
	}

	// 2. Machine policy killswitch disable_update: true
	t.Setenv("BB_DISABLE_UPDATE", "")
	if err := os.WriteFile(policyPath, []byte("disable_update: true\n"), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	output, err = executeLiveCLI(t, "--json", "update")
	if err == nil {
		t.Fatalf("expected update to be disabled by system policy, got output: %s", output)
	}
	if !apperrors.IsKind(err, apperrors.KindAuthorization) {
		t.Fatalf("expected KindAuthorization, got %v", err)
	}

	// 3. Mirror resolution with local test server
	if err := os.WriteFile(policyPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	ext := "tar.gz"
	if runtime.GOOS == "windows" {
		ext = "zip"
	}
	assetName := fmt.Sprintf("bb_9.9.9_%s_%s.%s", runtime.GOOS, runtime.GOARCH, ext)

	checksumContent := fmt.Sprintf("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef  %s\n", assetName)
	releasePayload := map[string]any{
		"tag_name": "v9.9.9",
		"html_url": "https://mirror.corp.local/releases/v9.9.9",
		"assets": []map[string]any{
			{
				"name":                 assetName,
				"browser_download_url": "/downloads/" + assetName,
			},
			{
				"name":                 "sha256sums.txt",
				"browser_download_url": "/downloads/sha256sums.txt",
			},
		},
	}

	var mirrorContacted bool
	mirrorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mirrorContacted = true
		if strings.HasSuffix(r.URL.Path, "sha256sums.txt") {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(checksumContent))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(releasePayload)
	}))
	defer mirrorServer.Close()

	output, err = executeLiveCLI(t, "--json", "update", "--dry-run", "--base-url", mirrorServer.URL)
	if !mirrorContacted {
		t.Fatal("expected mirror server to be contacted by update command")
	}
	if err == nil {
		if !strings.Contains(output, "v9.9.9") {
			t.Fatalf("expected update dry-run to preview release from mirror, got: %s", output)
		}
	} else {
		// When no signature bundle is present, update safely halts before applying
		if !strings.Contains(err.Error(), "sha256sums.txt.sigstore.json") {
			t.Fatalf("unexpected error contacting mirror: %v", err)
		}
	}
}

// TestLiveEnterprisePolicyAuthLoginAllowedHosts asserts that `bb auth login`
// enforces machine-level allowed_hosts policy against live hosts.
func TestLiveEnterprisePolicyAuthLoginAllowedHosts(t *testing.T) {
	harness := newLiveHarness(t)

	tempDir := t.TempDir()
	policyPath := filepath.Join(tempDir, "policy.yaml")
	t.Setenv("BB_SYSTEM_CONFIG_PATH", policyPath)
	t.Setenv("BB_CONFIG_PATH", filepath.Join(tempDir, "user.yaml"))

	// 1. Policy allows only the live harness host
	allowedYAML := fmt.Sprintf("allowed_hosts:\n  - %q\n", harness.config.BitbucketURL)
	if err := os.WriteFile(policyPath, []byte(allowedYAML), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	// Login to an unlisted external host must be blocked by policy
	output, err := executeLiveCLI(t, "auth", "login", "https://unauthorized-bitbucket.corp.local", "--token", "fake-token", "--discover-aliases=false")
	if err == nil {
		t.Fatalf("expected login to unlisted host to be blocked by policy, got output: %s", output)
	}
	if !apperrors.IsKind(err, apperrors.KindAuthorization) {
		t.Fatalf("expected KindAuthorization, got %v", err)
	}
	if !strings.Contains(err.Error(), "is not permitted by administrative policy") {
		t.Fatalf("unexpected error message: %v", err)
	}

	// Login to the allowed live host must succeed
	output, err = executeLiveCLI(t, "auth", "login", harness.config.BitbucketURL, "--username", harness.config.BitbucketUsername, "--password", harness.config.BitbucketPassword, "--discover-aliases=false")
	if err != nil {
		t.Fatalf("expected login to allowed live host to succeed: %v\noutput: %s", err, output)
	}
}

// TestLiveEnterprisePolicyRawAPIEscapeHatch asserts that the raw api command (`bb api`)
// strictly respects administrative allowed_hosts policy.
func TestLiveEnterprisePolicyRawAPIEscapeHatch(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	tempDir := t.TempDir()
	policyPath := filepath.Join(tempDir, "policy.yaml")
	t.Setenv("BB_SYSTEM_CONFIG_PATH", policyPath)

	// 1. Allowed host: bb api succeeds against live Bitbucket
	allowedYAML := fmt.Sprintf("allowed_hosts:\n  - %q\n", harness.config.BitbucketURL)
	if err := os.WriteFile(policyPath, []byte(allowedYAML), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	output, err := executeLiveCLI(t, "api", "/rest/api/latest/projects")
	if err != nil {
		t.Fatalf("expected bb api to succeed with allowed host policy: %v\noutput: %s", err, output)
	}
	if !strings.Contains(output, seeded.Key) {
		t.Fatalf("expected live project key in api output: %s", output)
	}

	// 2. Disallowed host: bb api is rejected with KindAuthorization before reaching network
	disallowedYAML := "allowed_hosts:\n  - \"https://different-host.corp.internal\"\n"
	if err := os.WriteFile(policyPath, []byte(disallowedYAML), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	output, err = executeLiveCLI(t, "api", "/rest/api/latest/projects")
	if err == nil {
		t.Fatalf("expected bb api to be rejected by administrative policy, but it succeeded\noutput: %s", output)
	}
	if !apperrors.IsKind(err, apperrors.KindAuthorization) {
		t.Fatalf("expected KindAuthorization, got %v", err)
	}
	if !strings.Contains(err.Error(), "is not permitted by administrative policy") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

// TestLiveEnterprisePolicyKeyringWarningOnLiveCommand asserts that attempting to
// bypass require_keyring policy via BB_REQUIRE_KEYRING=0 produces a warning on stderr.
func TestLiveEnterprisePolicyKeyringWarningOnLiveCommand(t *testing.T) {
	harness := newLiveHarness(t)

	configureLiveCLIEnv(t, harness, "PROJ", "repo")

	tempDir := t.TempDir()
	policyPath := filepath.Join(tempDir, "policy.yaml")
	t.Setenv("BB_SYSTEM_CONFIG_PATH", policyPath)
	t.Setenv("BB_REQUIRE_KEYRING", "0")

	policyYAML := "require_keyring: true\n"
	if err := os.WriteFile(policyPath, []byte(policyYAML), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	var warnBuf bytes.Buffer
	config.SetPolicyWarningWriter(&warnBuf)
	t.Cleanup(func() { config.SetPolicyWarningWriter(nil) })

	output, cmdErr := executeLiveCLI(t, "--json", "project", "list", "--limit", "1")
	if cmdErr != nil {
		t.Fatalf("command failed: %v\noutput: %s", cmdErr, output)
	}
	if !strings.Contains(warnBuf.String(), "warning: BB_REQUIRE_KEYRING=0 is ignored") {
		t.Fatalf("expected warning when user attempts to disable require_keyring policy, got: %s", warnBuf.String())
	}
}
