package repocmd

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/safederef"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
)

func TestRepoDefaults(t *testing.T) {
	var deps Dependencies
	d := deps.withDefaults()

	if d.JSONEnabled == nil || d.JSONEnabled() {
		t.Fatal("expected JSONEnabled to default to false")
	}
	if d.DryRunEnabled == nil || d.DryRunEnabled() {
		t.Fatal("expected DryRunEnabled to default to false")
	}
	if d.WriteJSON == nil || d.WriteJSONList == nil {
		t.Fatal("expected WriteJSON and WriteJSONList to default to non-nil")
	}

	// The default loaders are the configuration's own. The one that takes
	// overrides resolves the host it is handed. The ones that take none fail
	// under the seal, which names no host, the way the configuration does,
	// where a stub would answer or fail in words of its own. Reading
	// BITBUCKET_URL is internal/config's to test.
	if d.LoadConfig == nil || d.LoadConfigWithOverrides == nil || d.LoadConfigAndClient == nil {
		t.Fatal("expected LoadConfig, LoadConfigWithOverrides and LoadConfigAndClient to default to non-nil")
	}
	cfg, err := d.LoadConfigWithOverrides(config.Overrides{Host: "http://bitbucket.invalid"})
	if err != nil || cfg.BitbucketURL != "http://bitbucket.invalid" {
		t.Fatalf("the default LoadConfigWithOverrides did not resolve the host it was handed: %q, %v", cfg.BitbucketURL, err)
	}
	if _, err := d.LoadConfig(); err == nil || !strings.Contains(err.Error(), "no Bitbucket host configured") {
		t.Fatalf("the default LoadConfig is not the configuration's own: %v", err)
	}
	if _, _, err := d.LoadConfigAndClient(); err == nil || !strings.Contains(err.Error(), "no Bitbucket host configured") {
		t.Fatalf("the default LoadConfigAndClient does not load through it: %v", err)
	}

	// Handed a configuration, the default LoadConfigAndClient builds a client for it.
	handed := Dependencies{LoadConfig: func() (config.AppConfig, error) {
		return config.AppConfig{BitbucketURL: "http://bitbucket.invalid"}, nil
	}}
	cfg, client, err := handed.withDefaults().LoadConfigAndClient()
	if err != nil || client == nil || cfg.BitbucketURL != "http://bitbucket.invalid" {
		t.Fatalf("unexpected LoadConfigAndClient: %v", err)
	}
}

func TestRepoHelpers(t *testing.T) {
	t.Parallel()

	if safederef.String(nil) != "" {
		t.Fatal("expected empty string for safederef.String(nil)")
	}
	s := "hello"
	if safederef.String(&s) != "hello" {
		t.Fatal("expected hello for safederef.String(&s)")
	}

	cfg := config.AppConfig{ProjectKey: "PRJ"}
	ref, err := resolveRepoReference("PRJ/repo1", cfg)
	if err != nil || ref.ProjectKey != "PRJ" || ref.Slug != "repo1" {
		t.Fatalf("unexpected resolveRepoReference result: (%v, %v)", ref, err)
	}

	// Ssh key scope resolution
	proj, repo, isProj, err := resolveRepoSshKeyScope("PRJ", "")
	if err != nil || !isProj || proj != "PRJ" || repo != "" {
		t.Fatalf("unexpected resolveRepoSshKeyScope project result: %v", err)
	}

	proj, repo, isProj, err = resolveRepoSshKeyScope("", "PRJ/repo1")
	if err != nil || isProj || proj != "PRJ" || repo != "repo1" {
		t.Fatalf("unexpected resolveRepoSshKeyScope repo result: %v", err)
	}

	if _, _, _, err := resolveRepoSshKeyScope("PRJ", "PRJ/repo1"); err == nil {
		t.Fatal("expected error when both project and repo are provided")
	}
	if _, _, _, err := resolveRepoSshKeyScope("", "invalid-repo"); err == nil {
		t.Fatal("expected error for invalid repo format")
	}
	if _, _, _, err := resolveRepoSshKeyScope("", ""); err == nil {
		t.Fatal("expected error when neither project nor repo is provided")
	}

	// Read public key
	tmpKey := filepath.Join(t.TempDir(), "id_rsa.pub")
	if err := os.WriteFile(tmpKey, []byte("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5... test@key\n"), 0600); err != nil {
		t.Fatalf("failed to write tmp key: %v", err)
	}
	content, err := readPublicKey(tmpKey)
	if err != nil || content != "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5... test@key" {
		t.Fatalf("readPublicKey from file failed: %v, got %q", err, content)
	}
	content, err = readPublicKey("ssh-rsa raw-key")
	if err != nil || content != "ssh-rsa raw-key" {
		t.Fatalf("readPublicKey raw string failed: %v, got %q", err, content)
	}
}

func TestRepoCloneHelpers(t *testing.T) {
	t.Parallel()

	if !isExplicitHTTPCloneURL("https://bitbucket.example.com/scm/PRJ/repo.git") {
		t.Fatal("expected isExplicitHTTPCloneURL to be true for https URL")
	}
	if !isExplicitHTTPCloneURL("http://bitbucket.example.com/scm/PRJ/repo.git") {
		t.Fatal("expected isExplicitHTTPCloneURL to be true for http URL")
	}
	if isExplicitHTTPCloneURL("ssh://git@bitbucket.example.com/PRJ/repo.git") {
		t.Fatal("expected isExplicitHTTPCloneURL to be false for ssh URL")
	}

	if !sameCloneHost("https://bitbucket.example.com", "bitbucket.example.com") {
		t.Fatal("expected sameCloneHost to match")
	}
	if sameCloneHost("https://bitbucket1.example.com", "https://bitbucket2.example.com") {
		t.Fatal("expected sameCloneHost to not match different hosts")
	}

	// splitCloneDirectoryAndExtraArgs
	dir, args := splitCloneDirectoryAndExtraArgs("default-dir", nil)
	if dir != "default-dir" || len(args) != 0 {
		t.Fatalf("unexpected empty args result: (%q, %v)", dir, args)
	}

	dir, args = splitCloneDirectoryAndExtraArgs("default-dir", []string{"custom-dir", "--depth", "1"})
	if dir != "custom-dir" || !reflect.DeepEqual(args, []string{"--depth", "1"}) {
		t.Fatalf("unexpected custom dir result: (%q, %v)", dir, args)
	}

	dir, args = splitCloneDirectoryAndExtraArgs("default-dir", []string{"--depth", "1"})
	if dir != "default-dir" || !reflect.DeepEqual(args, []string{"--depth", "1"}) {
		t.Fatalf("unexpected flag args result: (%q, %v)", dir, args)
	}
}
