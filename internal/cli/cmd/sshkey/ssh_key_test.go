package sshkeycmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
)

func TestSSHKeyWithDefaults(t *testing.T) {
	d := Dependencies{}.withDefaults()
	if d.JSONEnabled == nil || d.JSONEnabled() {
		t.Fatal("expected default JSONEnabled to return false")
	}
	if d.WriteJSON == nil || d.WriteJSONList == nil {
		t.Fatal("expected default write functions to be non-nil")
	}

	// The default loader is the configuration's own. The seal names no host,
	// so it fails the way the configuration does, where a stub would answer
	// or fail in words of its own. Reading BITBUCKET_URL is internal/config's
	// to test.
	if d.LoadConfig == nil || d.LoadConfigAndClient == nil {
		t.Fatal("expected default LoadConfig and LoadConfigAndClient to be non-nil")
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
	}}.withDefaults()
	cfg, client, err := handed.LoadConfigAndClient()
	if err != nil || client == nil || cfg.BitbucketURL != "http://bitbucket.invalid" {
		t.Fatalf("unexpected LoadConfigAndClient: %v", err)
	}
}

func TestReadPublicKey(t *testing.T) {
	t.Parallel()

	raw := "ssh-rsa AAAA..."
	key, err := readPublicKey(raw)
	if err != nil || key != raw {
		t.Fatalf("unexpected readPublicKey text: %v", err)
	}

	tmpDir := t.TempDir()
	keyFile := filepath.Join(tmpDir, "id_rsa.pub")
	if err := os.WriteFile(keyFile, []byte("ssh-ed25519 BBBB...\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	key, err = readPublicKey(keyFile)
	if err != nil || key != "ssh-ed25519 BBBB..." {
		t.Fatalf("unexpected readPublicKey file: %s, %v", key, err)
	}
}

// The ssh key command suite is live now.
//
// A key the fixture returned proves the formatter reads the fixture.
// TestLiveSSHKeyLifecycle adds a real key, lists it and removes it.
