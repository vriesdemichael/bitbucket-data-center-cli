package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// malformed is YAML no parser will accept: a tab cannot start a token.
const malformed = "default_host: corp-a\nhosts:\n  corp-a: {url: https://a.example}\n  corp-b: {url: https://b.example}\n\tbad-indent: {url: https://oops}\n"

func writeMalformed(t *testing.T, name string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(malformed), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	return path
}

// TestADamagedConfigIsReportedRatherThanTreatedAsEmpty is #567.
//
// Three loads discarded their error, so a file bb could not parse resolved as
// a file that was not there: the user was told they were not logged in, and
// the remedy that message prescribed rewrote the config and deleted every
// other host in it.
func TestADamagedConfigIsReportedRatherThanTreatedAsEmpty(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		variable string
		names    string
	}{
		{name: "stored", variable: "BB_CONFIG_PATH", names: "stored configuration"},
		{name: "system", variable: "BB_SYSTEM_CONFIG_PATH", names: "system configuration"},
		{name: "workspace", variable: "BB_WORKSPACE_CONFIG_PATH", names: "workspace configuration"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			path := writeMalformed(t, testCase.name+".yaml")
			t.Setenv(testCase.variable, path)
			t.Setenv("BB_DISABLE_STORED_CONFIG", "")
			t.Setenv("BITBUCKET_URL", "https://bitbucket.example")
			t.Setenv("BITBUCKET_TOKEN", "t")

			_, err := LoadFromEnv()
			if err == nil {
				t.Fatal("a config that cannot be parsed was accepted")
			}

			message := err.Error()
			if !strings.Contains(message, testCase.names) {
				t.Errorf("the error does not say which file: %v", message)
			}
			if !strings.Contains(message, path) {
				t.Errorf("the error does not name the path %s: %v", path, message)
			}
			// The sentence that turned a damaged config into a destroyed one.
			if strings.Contains(message, "auth login") {
				t.Errorf("the error still prescribes the remedy that deletes the file: %v", message)
			}
		})
	}
}

// The system file carries the administrator's policy. Ignoring a broken one
// silently means the controls do not apply and nobody is told, so a policy
// could be switched off by corrupting the file.
func TestABrokenSystemPolicyFailsClosed(t *testing.T) {
	t.Setenv("BB_SYSTEM_CONFIG_PATH", writeMalformed(t, "system.yaml"))
	t.Setenv("BITBUCKET_URL", "https://bitbucket.example")
	t.Setenv("BITBUCKET_TOKEN", "t")

	if _, err := LoadFromEnv(); err == nil {
		t.Fatal("a damaged system policy was ignored, so the policy did not apply and nothing said so")
	}
}

// TestSaveLoginRefusesToRewriteAConfigItCouldNotRead is the data loss itself.
func TestSaveLoginRefusesToRewriteAConfigItCouldNotRead(t *testing.T) {
	path := writeMalformed(t, "stored.yaml")
	t.Setenv("BB_CONFIG_PATH", path)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	if _, err := SaveLogin(LoginInput{Host: "https://bitbucket.example", Token: "t"}); err == nil {
		t.Fatal("login rewrote a config it could not read")
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture after: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("the config was modified:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// The common case has to keep working: a config that is simply absent is not
// an error, and the "run auth login" advice is right there and only there.
func TestAnAbsentConfigIsNotAnError(t *testing.T) {
	t.Setenv("BB_CONFIG_PATH", filepath.Join(t.TempDir(), "not-created.yaml"))
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")
	t.Setenv("BITBUCKET_URL", "https://bitbucket.example")
	t.Setenv("BITBUCKET_TOKEN", "t")

	if _, err := LoadFromEnv(); err != nil {
		t.Fatalf("an absent config must resolve, got: %v", err)
	}
}

// TestTwoConfigFilesKeepSeparateCredentialsForOneHost is #587.
//
// The keyring entry was keyed by host alone, so a second identity against the
// same Bitbucket host evicted the first -- even from a different config file,
// because the path did not reach the key.
func TestTwoConfigFilesKeepSeparateCredentialsForOneHost(t *testing.T) {
	const host = "https://bitbucket.example"

	directory := t.TempDir()
	personal := filepath.Join(directory, "personal.yaml")
	service := filepath.Join(directory, "service.yaml")

	login := func(path, token string) {
		t.Helper()
		t.Setenv("BB_CONFIG_PATH", path)
		t.Setenv("BB_DISABLE_STORED_CONFIG", "")
		if _, err := SaveLogin(LoginInput{Host: host, Token: token}); err != nil {
			t.Fatalf("login into %s: %v", path, err)
		}
	}

	tokenFor := func(path string) string {
		t.Helper()
		t.Setenv("BB_CONFIG_PATH", path)
		t.Setenv("BB_DISABLE_STORED_CONFIG", "")
		t.Setenv("BITBUCKET_TOKEN", "")
		t.Setenv("BITBUCKET_URL", "")

		cfg, err := LoadFromEnv()
		if err != nil {
			t.Fatalf("load %s: %v", path, err)
		}

		return cfg.BitbucketToken
	}

	login(personal, "personal-token")
	login(service, "service-token")

	// The second login must not have evicted the first.
	if got := tokenFor(personal); got != "personal-token" {
		t.Errorf("the personal config resolves %q, want personal-token", got)
	}
	if got := tokenFor(service); got != "service-token" {
		t.Errorf("the service config resolves %q, want service-token", got)
	}
}

// A credential stored before the key carried the config file still resolves,
// so nobody has to log in again for the fix.
func TestALegacyKeyringEntryStillResolves(t *testing.T) {
	const host = "https://legacy.example"

	path := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("BB_CONFIG_PATH", path)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")
	t.Setenv("BITBUCKET_TOKEN", "")
	t.Setenv("BITBUCKET_URL", "")

	if _, err := SaveLogin(LoginInput{Host: host, Token: "current"}); err != nil {
		t.Fatalf("login: %v", err)
	}

	// Rewrite the secret the way a pre-fix bb stored it: host key, no file.
	if err := keyringDelete(keyringServiceName, credentialKey(host)+":token"); err != nil {
		t.Fatalf("clear the scoped entry: %v", err)
	}
	if err := keyringSet(keyringServiceName, hostKey(host)+":token", "legacy"); err != nil {
		t.Fatalf("write the legacy entry: %v", err)
	}

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.BitbucketToken != "legacy" {
		t.Fatalf("the legacy credential did not resolve, got %q", cfg.BitbucketToken)
	}
}
