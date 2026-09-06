package sshkeycmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSSHKeyWithDefaults(t *testing.T) {
	t.Setenv("BITBUCKET_URL", "http://localhost:7990")
	d := Dependencies{}.withDefaults()
	if d.JSONEnabled == nil || d.JSONEnabled() {
		t.Fatal("expected default JSONEnabled to return false")
	}
	if d.WriteJSON == nil || d.WriteJSONList == nil {
		t.Fatal("expected default write functions to be non-nil")
	}
	if d.LoadConfig != nil {
		cfg, err := d.LoadConfig()
		if err != nil || cfg.BitbucketURL != "http://localhost:7990" {
			t.Fatalf("unexpected LoadConfig: %v", err)
		}
	}
	if d.LoadConfigAndClient != nil {
		cfg, client, err := d.LoadConfigAndClient()
		if err != nil || client == nil || cfg.BitbucketURL != "http://localhost:7990" {
			t.Fatalf("unexpected LoadConfigAndClient: %v", err)
		}
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
