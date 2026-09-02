package sshkeycmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
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

func TestSSHKeyCommands(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		case r.Method == http.MethodGet && path == "/rest/ssh/latest/keys":
			if r.URL.Query().Get("start") == "10" {
				_, _ = w.Write([]byte(`{"values":[],"isLastPage":true}`))
			} else {
				_, _ = w.Write([]byte(`{"values":[{"id":12,"label":"my-key","fingerprint":"SHA256:abc"}],"isLastPage":true}`))
			}

		case r.Method == http.MethodPost && path == "/rest/ssh/latest/keys":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":13,"label":"new-key"}`))

		case r.Method == http.MethodDelete && path == "/rest/ssh/latest/keys/12":
			w.WriteHeader(http.StatusNoContent)

		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	cfg := config.AppConfig{
		BitbucketURL: server.URL,
	}

	jsonEnabled := false

	deps := Dependencies{
		JSONEnabled: func() bool { return jsonEnabled },
		LoadConfig:  func() (config.AppConfig, error) { return cfg, nil },
		LoadConfigAndClient: func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
			client, err := openapi.NewClientWithResponsesFromConfig(cfg)
			return cfg, client, err
		},
	}

	// 1. List (human, JSON, empty)
	cmd := New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on list: %v", err)
	}
	if !strings.Contains(buf.String(), "my-key") {
		t.Fatalf("expected my-key in list output: %s", buf.String())
	}

	// Empty list
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list", "--start", "10"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on list empty: %v", err)
	}
	if !strings.Contains(buf.String(), "No SSH keys found") {
		t.Fatalf("expected No SSH keys found in output: %s", buf.String())
	}

	// JSON list
	jsonEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on list JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "my-key") {
		t.Fatalf("expected my-key in list JSON output: %s", buf.String())
	}
	jsonEnabled = false

	// 2. Add (human & JSON)
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"add", "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQC dummy", "--label", "new-key"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on add: %v", err)
	}
	if !strings.Contains(buf.String(), "added successfully") {
		t.Fatalf("expected added successfully in add output: %s", buf.String())
	}

	jsonEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"add", "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQC dummy", "--label", "new-key"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on add JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "new-key") {
		t.Fatalf("expected new-key in add JSON output: %s", buf.String())
	}
	jsonEnabled = false

	// 3. Remove (human & JSON)
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"remove", "12"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on remove: %v", err)
	}
	if !strings.Contains(buf.String(), "removed successfully") {
		t.Fatalf("expected removed successfully in remove output: %s", buf.String())
	}

	jsonEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"remove", "12"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on remove JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "ok") {
		t.Fatalf("expected ok in remove JSON output: %s", buf.String())
	}
}
