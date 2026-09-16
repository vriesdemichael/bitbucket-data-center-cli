package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// storedWithDefaultHostCredential writes a stored configuration holding one
// host, its credential, and that host as the default.
func storedWithDefaultHostCredential(t *testing.T, token string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	stored := strings.Join([]string{
		"default_host: https://trusted.example.com",
		"hosts:",
		"  https://trusted.example.com:",
		"    url: https://trusted.example.com",
		"    username: alice",
		"insecure_secrets:",
		"  https://trusted.example.com:",
		"    token: " + token,
		"",
	}, "\n")

	if err := os.WriteFile(path, []byte(stored), 0o600); err != nil {
		t.Fatalf("write stored config: %v", err)
	}

	return path
}

// TestACredentialDoesNotFollowAHostTheRepositoryChose is the fallback in
// storedProfileFor meeting configuration that arrives with a clone.
//
// When no stored host matches, credentials resolve to the default host's, which
// is what lets `bb pr list` work against the active server without repeating
// --host. A cloned repository carries .bb/config.yaml and .env, and either can
// name a host: the fallback then sent the user's token for their own Bitbucket
// to a server the repository author chose.
func TestACredentialDoesNotFollowAHostTheRepositoryChose(t *testing.T) {
	const storedToken = "stored-default-host-token"

	t.Run("a workspace file naming another host gets no credential", func(t *testing.T) {
		workspacePath := filepath.Join(t.TempDir(), "workspace.yaml")
		workspace := strings.Join([]string{
			"default_host: https://attacker.example.net",
			"hosts:",
			"  https://attacker.example.net:",
			"    url: https://attacker.example.net",
			"",
		}, "\n")
		if err := os.WriteFile(workspacePath, []byte(workspace), 0o600); err != nil {
			t.Fatalf("write workspace config: %v", err)
		}

		t.Setenv("BB_CONFIG_PATH", storedWithDefaultHostCredential(t, storedToken))
		t.Setenv("BB_WORKSPACE_CONFIG_PATH", workspacePath)
		t.Setenv("BB_DISABLE_STORED_CONFIG", "0")
		t.Setenv("BITBUCKET_URL", "")
		t.Setenv("BITBUCKET_TOKEN", "")
		os.Unsetenv("BITBUCKET_URL")
		os.Unsetenv("BITBUCKET_TOKEN")

		cfg, err := LoadFromEnv()
		if err != nil {
			t.Fatalf("LoadFromEnv: %v", err)
		}

		if cfg.BitbucketURL != "https://attacker.example.net" {
			t.Fatalf("the workspace host is still the host bb talks to: %s", cfg.BitbucketURL)
		}
		if cfg.BitbucketToken == storedToken {
			t.Fatal("the stored token for another host followed a host the workspace file named")
		}
		if cfg.BitbucketUsername == "alice" {
			t.Errorf("the stored username followed it too: %q", cfg.BitbucketUsername)
		}
	})

	t.Run("a .env naming another host gets no credential", func(t *testing.T) {
		working := t.TempDir()
		dotenv := "BITBUCKET_URL=https://attacker.example.net\n"
		if err := os.WriteFile(filepath.Join(working, ".env"), []byte(dotenv), 0o600); err != nil {
			t.Fatalf("write .env: %v", err)
		}

		t.Setenv("BB_CONFIG_PATH", storedWithDefaultHostCredential(t, storedToken))
		t.Setenv("BB_WORKSPACE_CONFIG_PATH", filepath.Join(t.TempDir(), "absent.yaml"))
		t.Setenv("BB_DISABLE_STORED_CONFIG", "0")
		// Unset rather than empty: godotenv fills only what the environment
		// lacks, and an empty value is something the environment has.
		t.Setenv("BITBUCKET_URL", "")
		t.Setenv("BITBUCKET_TOKEN", "")
		os.Unsetenv("BITBUCKET_URL")
		os.Unsetenv("BITBUCKET_TOKEN")
		t.Chdir(working)

		cfg, err := LoadFromEnv()
		if err != nil {
			t.Fatalf("LoadFromEnv: %v", err)
		}

		if cfg.BitbucketURL != "https://attacker.example.net" {
			t.Fatalf("the .env host is still the host bb talks to: %s", cfg.BitbucketURL)
		}
		if cfg.BitbucketToken == storedToken {
			t.Fatal("the stored token for another host followed a host a .env file named")
		}
	})

	t.Run("a host named on the command line gets no credential either", func(t *testing.T) {
		// `bb api http://elsewhere/...` and `bb ai mcp serve --host` are the
		// same question asked by an agent rather than by a file, and a
		// prompt-injected agent asks it on purpose.
		t.Setenv("BB_CONFIG_PATH", storedWithDefaultHostCredential(t, storedToken))
		t.Setenv("BB_WORKSPACE_CONFIG_PATH", filepath.Join(t.TempDir(), "absent.yaml"))
		t.Setenv("BB_DISABLE_STORED_CONFIG", "0")
		t.Setenv("BITBUCKET_TOKEN", "")
		os.Unsetenv("BITBUCKET_TOKEN")
		t.Setenv("BITBUCKET_URL", "https://other.example.com")

		cfg, err := LoadFromEnv()
		if err != nil {
			t.Fatalf("LoadFromEnv: %v", err)
		}

		if cfg.BitbucketToken == storedToken {
			t.Fatal("the stored token for another host followed a host named on the command line")
		}
	})

	t.Run("a workspace file naming a stored host keeps its credential", func(t *testing.T) {
		workspacePath := filepath.Join(t.TempDir(), "workspace.yaml")
		workspace := strings.Join([]string{
			"default_host: https://trusted.example.com",
			"",
		}, "\n")
		if err := os.WriteFile(workspacePath, []byte(workspace), 0o600); err != nil {
			t.Fatalf("write workspace config: %v", err)
		}

		t.Setenv("BB_CONFIG_PATH", storedWithDefaultHostCredential(t, storedToken))
		t.Setenv("BB_WORKSPACE_CONFIG_PATH", workspacePath)
		t.Setenv("BB_DISABLE_STORED_CONFIG", "0")
		t.Setenv("BITBUCKET_URL", "")
		t.Setenv("BITBUCKET_TOKEN", "")
		os.Unsetenv("BITBUCKET_URL")
		os.Unsetenv("BITBUCKET_TOKEN")

		cfg, err := LoadFromEnv()
		if err != nil {
			t.Fatalf("LoadFromEnv: %v", err)
		}

		if cfg.BitbucketToken != storedToken {
			t.Fatalf("a workspace file naming the stored host lost its credential: %q", cfg.BitbucketToken)
		}
	})
}
