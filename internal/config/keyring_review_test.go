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

	// Whether another case is the same file is the file system's answer, not the
	// operating system's: Windows and macOS ignore case by default, Linux does
	// not. Either way one file has one key, and two files have two.
	upper := strings.ToUpper(filepath.Join(directory, "config.yaml"))
	upperKey := keyFor(upper)
	if sameFile(t, upper, filepath.Join(directory, "config.yaml")) {
		if upperKey != absolute {
			t.Errorf("a differently-cased path to the same file keyed differently: %s vs %s", upperKey, absolute)
		}
	} else if upperKey == absolute {
		t.Errorf("a differently-cased path that is another file shared the key %s", absolute)
	}
}

// TestAPathSpelledAsOnDiskKeepsItsKey is what makes resolving the spelling safe
// to ship: a path already spelled as the file system holds it canonicalises to
// itself, so a credential stored under it before is still found. bb's own
// default on macOS, under Library/Application Support, is such a path.
func TestAPathSpelledAsOnDiskKeepsItsKey(t *testing.T) {
	t.Parallel()

	path := mixedCaseConfig(t)

	want := path
	if runtime.GOOS == "windows" {
		want = strings.ToLower(path)
	}
	if got := canonicalConfigPath(path); got != want {
		t.Errorf("canonicalConfigPath(%s) = %s, want %s", path, got, want)
	}
}

func TestAnotherSpellingResolvesToTheNameOnDisk(t *testing.T) {
	t.Parallel()

	onDisk := mixedCaseConfig(t)
	root := filepath.Dir(filepath.Dir(filepath.Dir(onDisk)))
	variant := filepath.Join(root, "application support", "BB", "Config.yaml")

	got := spelledOnDisk(variant)
	if sameFile(t, variant, onDisk) {
		if got != onDisk {
			t.Errorf("%s is the same file as %s but resolved to %s", variant, onDisk, got)
		}
	} else if got != variant {
		t.Errorf("%s names no file here, so it should stay as written, got %s", variant, got)
	}

	missing := filepath.Join(filepath.Dir(onDisk), "Not Yet", "config.yaml")
	if got := spelledOnDisk(missing); got != missing {
		t.Errorf("a path whose last parts do not exist yet should keep them as written: got %s, want %s", got, missing)
	}
}

// TestSpelledOnDiskKeepsWhatItCannotResolve covers the paths it hands back as
// written: one that is not absolute, the root of its volume, and one inside a
// directory that can be entered but not listed.
func TestSpelledOnDiskKeepsWhatItCannotResolve(t *testing.T) {
	t.Parallel()

	if got := spelledOnDisk("config.yaml"); got != "config.yaml" {
		t.Errorf("a relative path should come back as written, got %s", got)
	}

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve the temporary directory: %v", err)
	}

	top := filepath.VolumeName(root) + string(filepath.Separator)
	if got := spelledOnDisk(top); got != top {
		t.Errorf("the root of the volume should come back as written: got %s, want %s", got, top)
	}

	// Entered, not listed. Windows ignores the mode, which leaves a directory
	// that lists, and the name found is the one written either way.
	unlisted := filepath.Join(root, "Unlisted")
	if err := os.MkdirAll(unlisted, 0o700); err != nil {
		t.Fatalf("create %s: %v", unlisted, err)
	}
	path := filepath.Join(unlisted, "config.yaml")
	if err := os.WriteFile(path, []byte("hosts: {}\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := os.Chmod(unlisted, 0o100); err != nil {
		t.Fatalf("make %s unlistable: %v", unlisted, err)
	}
	t.Cleanup(func() { _ = os.Chmod(unlisted, 0o700) })

	if got := spelledOnDisk(path); got != path {
		t.Errorf("a name in a directory that cannot be listed should stay as written: got %s, want %s", got, path)
	}
}

// mixedCaseConfig creates Application Support/bb/config.yaml in a fresh
// directory and returns its path as the file system spells it.
func mixedCaseConfig(t *testing.T) string {
	t.Helper()

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve the temporary directory: %v", err)
	}
	directory := filepath.Join(root, "Application Support", "bb")
	if err := os.MkdirAll(directory, 0o750); err != nil {
		t.Fatalf("create %s: %v", directory, err)
	}
	path := filepath.Join(directory, "config.yaml")
	if err := os.WriteFile(path, []byte("hosts: {}\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}

	return path
}

// sameFile reports whether candidate opens the file at existing.
func sameFile(t *testing.T, candidate, existing string) bool {
	t.Helper()

	candidateInfo, err := os.Stat(candidate)
	if err != nil {
		return false
	}
	existingInfo, err := os.Stat(existing)
	if err != nil {
		t.Fatalf("stat %s: %v", existing, err)
	}

	return os.SameFile(candidateInfo, existingInfo)
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
