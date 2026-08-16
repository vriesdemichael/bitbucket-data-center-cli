package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// captureStderr redirects os.Stderr for the duration of fn.
//
// loadConfig writes its warnings straight to os.Stderr rather than to a command
// stream, because it runs before any command has been resolved.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}

	original := os.Stderr
	os.Stderr = writer

	// Drained concurrently: a write larger than the pipe buffer would otherwise
	// block fn forever.
	captured := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(reader)
		captured <- string(data)
	}()

	fn()

	os.Stderr = original
	if err := writer.Close(); err != nil {
		t.Fatalf("close pipe: %v", err)
	}

	return <-captured
}

func writeInsecureConfig(t *testing.T, host string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	contents := "default_host: " + host + "\nhosts:\n    " + host + ":\n        url: " + host +
		"\n        auth_mode: token\ninsecure_secrets:\n    " + host + ":\n        token: plaintext-token\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	return path
}

// TestLoadConfigWarnsOncePerProcessAboutPlaintextStorage covers the warning that
// makes plaintext storage visible on every command rather than only at login.
//
// Whoever inherits a machine never sees the login-time message, and plaintext
// storage is a standing condition rather than a one-off event.
func TestLoadConfigWarnsOncePerProcessAboutPlaintextStorage(t *testing.T) {
	host := "https://warn-once.example.invalid"
	t.Setenv("BB_CONFIG_PATH", writeInsecureConfig(t, host))
	t.Setenv("BITBUCKET_URL", host)
	// ADMIN_USER and ADMIN_PASSWORD are set on the CI runner for the live suite
	// and leak into the unit run, so every auth variable has to be cleared for
	// this to test what it claims to.
	for _, key := range []string{
		"BITBUCKET_TOKEN", "BITBUCKET_USERNAME", "BITBUCKET_USER", "BITBUCKET_PASSWORD",
		"ADMIN_USER", "ADMIN_PASSWORD", "BB_REQUIRE_KEYRING", "BB_DISABLE_STORED_CONFIG",
	} {
		t.Setenv(key, "")
	}

	// The once is package state; reset it so this test does not depend on
	// whether some earlier test already consumed it.
	insecureStorageWarningOnce = sync.Once{}
	t.Cleanup(func() { insecureStorageWarningOnce = sync.Once{} })

	output := captureStderr(t, func() {
		cfg, err := loadConfig()
		if err != nil {
			t.Fatalf("loadConfig failed: %v", err)
		}
		if !cfg.UsedInsecureStorage {
			t.Fatal("expected the config to report insecure storage")
		}

		// A second load in the same process must stay quiet.
		if _, err := loadConfig(); err != nil {
			t.Fatalf("second loadConfig failed: %v", err)
		}
	})

	if count := strings.Count(output, "stored in plaintext"); count != 1 {
		t.Fatalf("expected exactly one warning, got %d in %q", count, output)
	}
	if !strings.Contains(output, "BB_REQUIRE_KEYRING") {
		t.Fatalf("expected the remedy named, got %q", output)
	}
}
