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
// --host. A cloned repository carries .bb/config.yaml and .env, and either
// could name a host: the fallback then sent the user's token for their own
// Bitbucket to a server the repository author chose. Neither chooses the host
// now (#770), and bb names what it left unused; a host named on the command
// line still meets the fallback, and gets no credential.
func TestACredentialDoesNotFollowAHostTheRepositoryChose(t *testing.T) {
	const storedToken = "stored-default-host-token"

	t.Run("a workspace file naming another host does not move bb", func(t *testing.T) {
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
		// Away from any .env above the package: LoadFromEnv reads the nearest.
		t.Chdir(t.TempDir())

		cfg, err := LoadFromEnv()
		if err != nil {
			t.Fatalf("LoadFromEnv: %v", err)
		}

		if cfg.BitbucketURL != "https://trusted.example.com" {
			t.Fatalf("the workspace file chose the host bb talks to: %s", cfg.BitbucketURL)
		}
		assertIgnoredHostSetting(t, cfg, "default_host", workspacePath)
	})

	t.Run("a .env naming another host does not move bb", func(t *testing.T) {
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
		os.Unsetenv("BITBUCKET_URL")
		t.Chdir(working)

		cfg, err := LoadFromEnv()
		if err != nil {
			t.Fatalf("LoadFromEnv: %v", err)
		}

		if cfg.BitbucketURL != "https://trusted.example.com" {
			t.Fatalf("the .env chose the host bb talks to: %s", cfg.BitbucketURL)
		}
		if _, set := os.LookupEnv("BITBUCKET_URL"); set {
			t.Error("the .env still set BITBUCKET_URL")
		}
		assertIgnoredHostSetting(t, cfg, "BITBUCKET_URL", filepath.Join(working, ".env"))
	})

	t.Run("a host named on the command line gets no credential either", func(t *testing.T) {
		// `bb api http://elsewhere/...` and `bb ai mcp serve --host` are the
		// same question asked by an agent rather than by a file, and a
		// prompt-injected agent asks it on purpose.
		t.Setenv("BB_CONFIG_PATH", storedWithDefaultHostCredential(t, storedToken))
		t.Setenv("BB_WORKSPACE_CONFIG_PATH", filepath.Join(t.TempDir(), "absent.yaml"))
		t.Setenv("BB_DISABLE_STORED_CONFIG", "0")
		t.Chdir(t.TempDir())

		cfg, err := LoadWithOverrides(Overrides{Host: "https://other.example.com"})
		if err != nil {
			t.Fatalf("LoadWithOverrides: %v", err)
		}

		if cfg.BitbucketURL != "https://other.example.com" {
			t.Fatalf("the host named on the command line is not the host bb talks to: %s", cfg.BitbucketURL)
		}
		if cfg.BitbucketToken == storedToken {
			t.Fatal("the stored token for another host followed a host named on the command line")
		}
	})

	t.Run("a workspace file naming the stored host changes nothing", func(t *testing.T) {
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
		t.Chdir(t.TempDir())

		cfg, err := LoadFromEnv()
		if err != nil {
			t.Fatalf("LoadFromEnv: %v", err)
		}

		if cfg.BitbucketToken != storedToken {
			t.Fatalf("a workspace file naming the stored host lost its credential: %q", cfg.BitbucketToken)
		}
	})

	t.Run("a .env cannot move the stored host to plain http", func(t *testing.T) {
		// #730. The destination is the user's own host, so the host check
		// passes; the scheme is what the file changed. A credential stored
		// for https://host sent to http://host crosses the network in the
		// clear, for anyone on the path to read.
		working := t.TempDir()
		dotenv := "BITBUCKET_URL=http://trusted.example.com\n"
		if err := os.WriteFile(filepath.Join(working, ".env"), []byte(dotenv), 0o600); err != nil {
			t.Fatalf("write .env: %v", err)
		}

		t.Setenv("BB_CONFIG_PATH", storedWithDefaultHostCredential(t, storedToken))
		t.Setenv("BB_WORKSPACE_CONFIG_PATH", filepath.Join(t.TempDir(), "absent.yaml"))
		t.Setenv("BB_DISABLE_STORED_CONFIG", "0")
		// Unset rather than empty, so the .env can supply it.
		t.Setenv("BITBUCKET_URL", "")
		os.Unsetenv("BITBUCKET_URL")
		t.Chdir(working)

		cfg, err := LoadFromEnv()
		if err != nil {
			t.Fatalf("LoadFromEnv: %v", err)
		}

		if cfg.BitbucketURL != "https://trusted.example.com" {
			t.Fatalf("the .env moved bb to %s", cfg.BitbucketURL)
		}
		assertIgnoredHostSetting(t, cfg, "BITBUCKET_URL", filepath.Join(working, ".env"))
	})

	t.Run("a credential stored for http is sent over https", func(t *testing.T) {
		// The other direction adds TLS rather than removing it: a login made
		// before the server had a certificate keeps working after.
		path := filepath.Join(t.TempDir(), "config.yaml")
		stored := strings.Join([]string{
			"default_host: http://trusted.example.com",
			"hosts:",
			"  http://trusted.example.com:",
			"    url: http://trusted.example.com",
			"    username: alice",
			"insecure_secrets:",
			"  http://trusted.example.com:",
			"    token: " + storedToken,
			"",
		}, "\n")
		if err := os.WriteFile(path, []byte(stored), 0o600); err != nil {
			t.Fatalf("write stored config: %v", err)
		}

		t.Setenv("BB_CONFIG_PATH", path)
		t.Setenv("BB_WORKSPACE_CONFIG_PATH", filepath.Join(t.TempDir(), "absent.yaml"))
		t.Setenv("BB_DISABLE_STORED_CONFIG", "0")
		t.Chdir(t.TempDir())

		cfg, err := LoadWithOverrides(Overrides{Host: "https://trusted.example.com"})
		if err != nil {
			t.Fatalf("LoadWithOverrides: %v", err)
		}

		if cfg.BitbucketToken != storedToken {
			t.Fatalf("the token stored for http was not sent over https: %q", cfg.BitbucketToken)
		}
	})
}

// assertIgnoredHostSetting fails unless the load names the host setting it left
// unused, and the file it was in.
func assertIgnoredHostSetting(t *testing.T, cfg AppConfig, name, path string) {
	t.Helper()

	// Resolved, because a temporary directory can sit behind a symlink, and the
	// working directory bb searched from names it by its real path.
	resolved := func(path string) string {
		if real, err := filepath.EvalSymlinks(path); err == nil {
			return real
		}
		return filepath.Clean(path)
	}
	for _, ignored := range cfg.IgnoredHostSettings {
		if ignored.Name == name && resolved(ignored.Path) == resolved(path) {
			return
		}
	}
	t.Errorf("the load does not name %s in %s as ignored: %+v", name, path, cfg.IgnoredHostSettings)
}
