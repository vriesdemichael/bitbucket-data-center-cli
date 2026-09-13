package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// #587 scoped the keyring by config file and kept reading the host-only entries
// written before, so nobody had to log in again. A review found the fallback
// read them for every config on the machine: an old token overrode a new login,
// a second config acted as the first, and logging out of one logged out the
// other. These pin the rule that replaced it -- the host-only entries answer
// only for a config with nothing of its own -- and one spelling per file.

const reviewHost = "https://bitbucket.corp.example"

func useConfig(t *testing.T, path string) {
	t.Helper()
	t.Setenv("BB_CONFIG_PATH", path)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")
	t.Setenv("BITBUCKET_URL", "")
	t.Setenv("BITBUCKET_TOKEN", "")
	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("BITBUCKET_USER", "")
	t.Setenv("BITBUCKET_PASSWORD", "")
	t.Setenv("ADMIN_USER", "")
	t.Setenv("ADMIN_PASSWORD", "")
}

// storeProfileWithoutSecret writes a config naming the host, as a pre-upgrade
// bb left it: the secret lives only under the host-only keyring entry.
func storeProfileWithoutSecret(t *testing.T, path string) {
	t.Helper()
	useConfig(t, path)

	stored := StoredConfig{
		DefaultHost: hostKey(reviewHost),
		Hosts:       map[string]StoredProfile{hostKey(reviewHost): {URL: reviewHost, AuthMode: "token"}},
	}
	if err := SaveStoredConfig(stored); err != nil {
		t.Fatalf("save %s: %v", path, err)
	}
}

func resolved(t *testing.T, path string) AppConfig {
	t.Helper()
	useConfig(t, path)

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}

	return cfg
}

func TestAnOldTokenDoesNotOverrideANewLogin(t *testing.T) {
	store := withWorkingKeyring(t)
	path := filepath.Join(t.TempDir(), "config.yaml")

	store[keyringServiceName+"/"+hostKey(reviewHost)+":token"] = "old-revoked-token"

	useConfig(t, path)
	if _, err := SaveLogin(LoginInput{Host: reviewHost, Username: "svc", Password: "new-password"}); err != nil {
		t.Fatalf("login: %v", err)
	}

	cfg := resolved(t, path)
	if cfg.BitbucketToken != "" {
		t.Fatalf("a token stored before the login overrode it: token %q beside password login", cfg.BitbucketToken)
	}
	if cfg.BitbucketPassword != "new-password" {
		t.Fatalf("the new login did not resolve: password %q", cfg.BitbucketPassword)
	}
}

func TestASecondConfigDoesNotActAsTheFirst(t *testing.T) {
	store := withWorkingKeyring(t)
	directory := t.TempDir()
	personal := filepath.Join(directory, "personal.yaml")
	service := filepath.Join(directory, "service.yaml")

	storeProfileWithoutSecret(t, personal)
	store[keyringServiceName+"/"+hostKey(reviewHost)+":token"] = "personal-token"

	useConfig(t, service)
	if _, err := SaveLogin(LoginInput{Host: reviewHost, Username: "svc", Password: "service-password"}); err != nil {
		t.Fatalf("login: %v", err)
	}

	if cfg := resolved(t, service); cfg.BitbucketToken != "" {
		t.Fatalf("the service config picked up the personal token %q", cfg.BitbucketToken)
	}
	// And the first config still has what it had.
	if cfg := resolved(t, personal); cfg.BitbucketToken != "personal-token" {
		t.Fatalf("the personal config lost its token: %q", cfg.BitbucketToken)
	}
}

func TestLoggingOutOfOneConfigKeepsAnothersOldCredential(t *testing.T) {
	store := withWorkingKeyring(t)
	directory := t.TempDir()
	personal := filepath.Join(directory, "personal.yaml")
	service := filepath.Join(directory, "service.yaml")

	storeProfileWithoutSecret(t, personal)
	store[keyringServiceName+"/"+hostKey(reviewHost)+":token"] = "personal-token"

	useConfig(t, service)
	if _, err := SaveLogin(LoginInput{Host: reviewHost, Token: "service-token"}); err != nil {
		t.Fatalf("login: %v", err)
	}
	if err := Logout(reviewHost); err != nil {
		t.Fatalf("logout: %v", err)
	}

	if cfg := resolved(t, personal); cfg.BitbucketToken != "personal-token" {
		t.Fatalf("logging out of the service config evicted the personal one: token %q", cfg.BitbucketToken)
	}
}

// A config still on its old credential is the one using the host-only entry,
// so logging out of it removes that entry rather than leaving it behind.
func TestLoggingOutOfAConfigOnItsOldCredentialRemovesIt(t *testing.T) {
	store := withWorkingKeyring(t)
	path := filepath.Join(t.TempDir(), "config.yaml")

	storeProfileWithoutSecret(t, path)
	legacy := keyringServiceName + "/" + hostKey(reviewHost) + ":token"
	store[legacy] = "old-token"

	if err := Logout(reviewHost); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, left := store[legacy]; left {
		t.Fatal("logging out left the credential it was using in the keyring")
	}
}

func TestOneFileHasOneKeyHoweverItsPathIsSpelled(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "config.yaml"), []byte("hosts: {}\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Chdir(directory)

	keyFor := func(path string) string {
		t.Helper()
		t.Setenv("BB_CONFIG_PATH", path)

		return credentialKey(reviewHost)
	}

	absolute := keyFor(filepath.Join(directory, "config.yaml"))
	if relative := keyFor("config.yaml"); relative != absolute {
		t.Errorf("a relative path keyed differently from the absolute one: %s vs %s", relative, absolute)
	}
	if runtime.GOOS == "windows" {
		if upper := keyFor(strings.ToUpper(filepath.Join(directory, "config.yaml"))); upper != absolute {
			t.Errorf("a differently-cased path keyed differently on a case-insensitive file system: %s vs %s", upper, absolute)
		}
	}
}

func TestTheSameNameInTwoDirectoriesIsTwoKeys(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	t.Setenv("BB_CONFIG_PATH", "config.yaml")

	t.Chdir(first)
	firstKey := credentialKey(reviewHost)
	t.Chdir(second)
	secondKey := credentialKey(reviewHost)

	if firstKey == secondKey {
		t.Fatalf("config.yaml in two directories shared the key %s, so logging into one evicts the other", firstKey)
	}
}
