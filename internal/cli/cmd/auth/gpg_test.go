package auth

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
)

func TestAuthGpgKeyCommands(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/gpg/latest/keys":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":"426","emailAddress":"user@example.com","fingerprint":"FINGERPRINT1"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/gpg/latest/keys":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"426","emailAddress":"user@example.com","fingerprint":"FINGERPRINT1"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/gpg/latest/keys/426":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/gpg/latest/keys":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var jsonFlag bool
	deps := Dependencies{
		JSONEnabled: func() bool { return jsonFlag },
		LoadConfig: func() (config.AppConfig, error) {
			return config.AppConfig{
				BitbucketURL: server.URL,
			}, nil
		},
		WriteJSON: func(writer io.Writer, payload any) error {
			return jsonoutput.Write(writer, payload)
		},
	}

	execute := func(args ...string) (string, error) {
		cmd := New(deps)
		buffer := &bytes.Buffer{}
		cmd.SetOut(buffer)
		cmd.SetErr(buffer)
		cmd.SetArgs(args)
		err := cmd.Execute()
		return buffer.String(), err
	}

	// 1. List
	out, err := execute("gpg-key", "list")
	if err != nil {
		t.Fatalf("expected list success, got err=%v out=%s", err, out)
	}
	if !strings.Contains(out, "426") || !strings.Contains(out, "user@example.com") {
		t.Fatalf("unexpected list output: %s", out)
	}

	// 2. List JSON
	jsonFlag = true
	out, err = execute("gpg-key", "list")
	if err != nil {
		t.Fatalf("expected list json success, got err=%v", err)
	}
	if !strings.Contains(out, `"id": "426"`) {
		t.Fatalf("unexpected list json output: %s", out)
	}
	jsonFlag = false

	// 3. Add (directly key text)
	out, err = execute("gpg-key", "add", "gpg-key-text")
	if err != nil {
		t.Fatalf("expected add success, got err=%v out=%s", err, out)
	}
	if !strings.Contains(out, "added successfully") {
		t.Fatalf("unexpected add output: %s", out)
	}

	// 4. Add (from file)
	tmpDir := t.TempDir()
	keyFile := filepath.Join(tmpDir, "my-key.pub")
	if err := os.WriteFile(keyFile, []byte("key-text-from-file"), 0600); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	out, err = execute("gpg-key", "add", keyFile)
	if err != nil {
		t.Fatalf("expected add key from file success, got err=%v out=%s", err, out)
	}
	if !strings.Contains(out, "added successfully") {
		t.Fatalf("unexpected add output: %s", out)
	}

	// 5. Add (error reading directory as file)
	_, err = execute("gpg-key", "add", tmpDir)
	if err == nil {
		t.Fatal("expected error adding key from directory")
	}

	// 6. Remove
	out, err = execute("gpg-key", "remove", "426")
	if err != nil {
		t.Fatalf("expected remove success, got err=%v out=%s", err, out)
	}
	if !strings.Contains(out, "removed successfully") {
		t.Fatalf("unexpected remove output: %s", out)
	}

	// 7. Clear (with -y)
	out, err = execute("gpg-key", "clear", "-y")
	if err != nil {
		t.Fatalf("expected clear success, got err=%v out=%s", err, out)
	}
	if !strings.Contains(out, "cleared successfully") {
		t.Fatalf("unexpected clear output: %s", out)
	}
}

func TestAuthGpgKeyCommandsErrors(t *testing.T) {
	// 1. HTTP 500 error responses from server
	errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer errorServer.Close()

	deps := Dependencies{
		LoadConfig: func() (config.AppConfig, error) {
			return config.AppConfig{
				BitbucketURL: errorServer.URL,
			}, nil
		},
	}

	execute := func(args ...string) (string, error) {
		cmd := New(deps)
		buffer := &bytes.Buffer{}
		cmd.SetOut(buffer)
		cmd.SetErr(buffer)
		cmd.SetArgs(args)
		err := cmd.Execute()
		return buffer.String(), err
	}

	if _, err := execute("gpg-key", "list"); err == nil {
		t.Fatal("expected error listing GPG keys on 500 status")
	}
	if _, err := execute("gpg-key", "add", "gpg-key-text"); err == nil {
		t.Fatal("expected error adding GPG key on 500 status")
	}
	if _, err := execute("gpg-key", "remove", "keyid"); err == nil {
		t.Fatal("expected error removing GPG key on 500 status")
	}
	if _, err := execute("gpg-key", "clear", "-y"); err == nil {
		t.Fatal("expected error clearing GPG keys on 500 status")
	}
}

func TestAuthGpgKeyCommandsAdditionalCoverage(t *testing.T) {
	// 1. deps.LoadConfig returns error
	{
		deps := Dependencies{
			LoadConfig: func() (config.AppConfig, error) {
				return config.AppConfig{}, fmt.Errorf("config error")
			},
		}
		cmd := New(deps)
		cmd.SetArgs([]string{"gpg-key", "list"})
		if err := cmd.Execute(); err == nil {
			t.Fatal("expected error when LoadConfig fails")
		}
	}

	// 2. Client creation fails (invalid URL)
	{
		deps := Dependencies{
			LoadConfig: func() (config.AppConfig, error) {
				return config.AppConfig{
					BitbucketURL: "://invalid",
				}, nil
			},
		}
		cmd := New(deps)
		cmd.SetArgs([]string{"gpg-key", "list"})
		if err := cmd.Execute(); err == nil {
			t.Fatal("expected error when client creation fails")
		}
	}

	// 3. List keys when keys list is empty
	{
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[]}`))
		}))
		defer server.Close()
		deps := Dependencies{
			LoadConfig: func() (config.AppConfig, error) {
				return config.AppConfig{
					BitbucketURL: server.URL,
				}, nil
			},
		}
		cmd := New(deps)
		buffer := &bytes.Buffer{}
		cmd.SetOut(buffer)
		cmd.SetArgs([]string{"gpg-key", "list"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(buffer.String(), "No GPG keys found") {
			t.Fatalf("expected empty message, got: %s", buffer.String())
		}
	}

	// 4. Add key with empty key text
	{
		deps := Dependencies{}
		cmd := New(deps)
		cmd.SetArgs([]string{"gpg-key", "add", "   "})
		if err := cmd.Execute(); err == nil {
			t.Fatal("expected error when adding empty key")
		}
	}

	// 5. Clear keys cancellation ("n")
	{
		deps := Dependencies{}
		cmd := New(deps)
		oldStdin := os.Stdin
		r, w, _ := os.Pipe()
		os.Stdin = r
		_, _ = w.Write([]byte("n\n"))
		_ = w.Close()
		cmd.SetArgs([]string{"gpg-key", "clear"})
		err := cmd.Execute()
		os.Stdin = oldStdin
		if err == nil {
			t.Fatal("expected error (cancelled) when clearing keys with 'n'")
		}
	}

	// 6. Clear keys confirmation ("y")
	{
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()
		deps := Dependencies{
			LoadConfig: func() (config.AppConfig, error) {
				return config.AppConfig{
					BitbucketURL: server.URL,
				}, nil
			},
		}
		cmd := New(deps)
		oldStdin := os.Stdin
		r, w, _ := os.Pipe()
		os.Stdin = r
		_, _ = w.Write([]byte("y\n"))
		_ = w.Close()
		cmd.SetArgs([]string{"gpg-key", "clear"})
		err := cmd.Execute()
		os.Stdin = oldStdin
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}


