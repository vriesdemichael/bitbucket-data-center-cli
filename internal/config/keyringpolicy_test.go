package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
)

// writePlaintextCredentialConfig produces a stored config holding a credential
// in the plaintext fallback, as a machine with no working OS keyring would.
//
// The host is unique per test so the real keyring never holds an entry for it,
// which is what forces the fallback path deterministically.
func writePlaintextCredentialConfig(t *testing.T, host string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	contents := "default_host: " + host + "\n" +
		"hosts:\n" +
		"    " + host + ":\n" +
		"        url: " + host + "\n" +
		"        auth_mode: token\n" +
		"insecure_secrets:\n" +
		"    " + host + ":\n" +
		"        token: plaintext-token\n"

	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	return path
}

func clearAuthEnvironment(t *testing.T) {
	t.Helper()

	for _, key := range []string{
		"BITBUCKET_TOKEN", "BITBUCKET_USERNAME", "BITBUCKET_USER", "BITBUCKET_PASSWORD",
		"ADMIN_USER", "ADMIN_PASSWORD", "BB_REQUIRE_KEYRING", "BB_DISABLE_STORED_CONFIG",
	} {
		t.Setenv(key, "")
	}
}

func TestLoadFromEnvFlagsCredentialsReadFromPlaintextFallback(t *testing.T) {
	clearAuthEnvironment(t)
	host := "https://plaintext-flag.example.invalid"
	t.Setenv("BB_CONFIG_PATH", writePlaintextCredentialConfig(t, host))
	t.Setenv("BITBUCKET_URL", host)

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !cfg.UsedInsecureStorage {
		t.Fatal("expected UsedInsecureStorage for a credential read from the config fallback")
	}
	if cfg.BitbucketToken != "plaintext-token" {
		t.Fatalf("expected the fallback token to be used, got %q", cfg.BitbucketToken)
	}
	if got := cfg.CredentialStorage(); got != "config-file-plaintext" {
		t.Fatalf("expected config-file-plaintext, got %q", got)
	}
}

func TestLoadFromEnvRefusesPlaintextWhenKeyringRequired(t *testing.T) {
	clearAuthEnvironment(t)
	host := "https://plaintext-refused.example.invalid"
	t.Setenv("BB_CONFIG_PATH", writePlaintextCredentialConfig(t, host))
	t.Setenv("BITBUCKET_URL", host)
	t.Setenv("BB_REQUIRE_KEYRING", "1")

	_, err := LoadFromEnv()
	if err == nil {
		t.Fatal("expected loading to fail when plaintext storage is in use and the keyring is required")
	}

	// Enforcing only at login would let a config written before the policy was
	// set keep serving plaintext credentials indefinitely.
	if apperrors.KindOf(err) != apperrors.KindPermanent {
		t.Fatalf("expected permanent, got kind %q (%v)", apperrors.KindOf(err), err)
	}
	if apperrors.ExitCode(err) != 1 {
		t.Fatalf("expected exit code 1, got %d", apperrors.ExitCode(err))
	}
}

func TestLoadFromEnvAllowsEnvironmentCredentialsWhenKeyringRequired(t *testing.T) {
	clearAuthEnvironment(t)
	host := "https://env-wins.example.invalid"
	t.Setenv("BB_CONFIG_PATH", writePlaintextCredentialConfig(t, host))
	t.Setenv("BITBUCKET_URL", host)
	t.Setenv("BB_REQUIRE_KEYRING", "1")
	// A token supplied per invocation never touches the config file, so the
	// policy has nothing to object to — this is the documented escape hatch.
	t.Setenv("BITBUCKET_TOKEN", "token-from-environment")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("expected environment credentials to satisfy the policy, got %v", err)
	}

	if cfg.UsedInsecureStorage {
		t.Fatal("environment credentials must not be reported as insecure storage")
	}
	if got := cfg.CredentialStorage(); got != "environment" {
		t.Fatalf("expected environment, got %q", got)
	}
}

func TestLoadFromEnvRejectsMalformedKeyringPolicy(t *testing.T) {
	clearAuthEnvironment(t)
	host := "https://malformed-policy.example.invalid"
	t.Setenv("BB_CONFIG_PATH", writePlaintextCredentialConfig(t, host))
	t.Setenv("BITBUCKET_URL", host)
	t.Setenv("BB_REQUIRE_KEYRING", "yes-please")

	_, err := LoadFromEnv()
	if err == nil {
		t.Fatal("expected a malformed BB_REQUIRE_KEYRING to be rejected")
	}
	if apperrors.KindOf(err) != apperrors.KindValidation {
		t.Fatalf("expected validation, got kind %q (%v)", apperrors.KindOf(err), err)
	}
}

func TestRequireKeyringReadsTheEnvironment(t *testing.T) {
	testCases := []struct {
		value    string
		expected bool
	}{
		{value: "", expected: false},
		{value: "0", expected: false},
		{value: "false", expected: false},
		{value: "1", expected: true},
		{value: "true", expected: true},
	}

	for _, testCase := range testCases {
		t.Run("BB_REQUIRE_KEYRING="+testCase.value, func(t *testing.T) {
			t.Setenv("BB_REQUIRE_KEYRING", testCase.value)

			required, err := RequireKeyring()
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if required != testCase.expected {
				t.Fatalf("expected %v, got %v", testCase.expected, required)
			}
		})
	}
}

func TestRequireKeyringPolicyHonoursTheFlagWithoutTheEnvironment(t *testing.T) {
	t.Setenv("BB_REQUIRE_KEYRING", "")

	required, err := requireKeyringPolicy(true)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !required {
		t.Fatal("expected the flag alone to enable the policy")
	}
}

func TestSaveLoginRejectsRequireKeyringWithMalformedPolicy(t *testing.T) {
	clearAuthEnvironment(t)
	t.Setenv("BB_CONFIG_PATH", filepath.Join(t.TempDir(), "config.yaml"))
	t.Setenv("BB_REQUIRE_KEYRING", "not-a-bool")

	_, err := SaveLogin(LoginInput{Host: "https://policy.example.invalid", Token: "tok"})
	if err == nil {
		t.Fatal("expected a malformed policy to be rejected")
	}
	if apperrors.KindOf(err) != apperrors.KindValidation {
		t.Fatalf("expected validation, got kind %q (%v)", apperrors.KindOf(err), err)
	}
}

func TestCredentialStorage(t *testing.T) {
	testCases := []struct {
		name     string
		config   AppConfig
		expected string
	}{
		{
			name:     "no credentials",
			config:   AppConfig{AuthSource: "stored"},
			expected: "none",
		},
		{
			name:     "keyring",
			config:   AppConfig{BitbucketToken: "tok", AuthSource: "stored"},
			expected: "keyring",
		},
		{
			name:     "plaintext fallback",
			config:   AppConfig{BitbucketToken: "tok", AuthSource: "stored", UsedInsecureStorage: true},
			expected: "config-file-plaintext",
		},
		{
			name:     "environment",
			config:   AppConfig{BitbucketToken: "tok", AuthSource: "env"},
			expected: "environment",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := testCase.config.CredentialStorage(); got != testCase.expected {
				t.Fatalf("expected %q, got %q", testCase.expected, got)
			}
		})
	}
}

func TestKeyringUnavailableErrorIsPermanentAndActionable(t *testing.T) {
	err := keyringUnavailableError(os.ErrPermission)

	if apperrors.KindOf(err) != apperrors.KindPermanent {
		t.Fatalf("expected permanent, got %q", apperrors.KindOf(err))
	}
	// Retrying the same command on the same host changes nothing, so a caller
	// must be able to tell this apart from a transient failure.
	if apperrors.ExitCode(err) != 1 {
		t.Fatalf("expected exit code 1, got %d", apperrors.ExitCode(err))
	}
	if got := apperrors.MessageOf(err); got == "" {
		t.Fatal("expected a remediation message")
	}
}

// withUnavailableKeyring simulates the machines where the fallback actually
// fires: headless servers, containers, WSL without gnome-keyring.
func withUnavailableKeyring(t *testing.T) {
	t.Helper()

	failure := errors.New("no keyring daemon")
	originalSet, originalGet, originalDelete := keyringSet, keyringGet, keyringDelete

	keyringSet = func(string, string, string) error { return failure }
	keyringGet = func(string, string) (string, error) { return "", failure }
	keyringDelete = func(string, string) error { return failure }

	t.Cleanup(func() {
		keyringSet, keyringGet, keyringDelete = originalSet, originalGet, originalDelete
	})
}

// withWorkingKeyring substitutes an in-memory store, so a test can assert that
// no secret reached the config file without touching the real credential store.
func withWorkingKeyring(t *testing.T) map[string]string {
	t.Helper()

	store := map[string]string{}
	originalSet, originalGet, originalDelete := keyringSet, keyringGet, keyringDelete

	keyringSet = func(service, user, secret string) error {
		store[service+"/"+user] = secret
		return nil
	}
	keyringGet = func(service, user string) (string, error) {
		secret, ok := store[service+"/"+user]
		if !ok {
			return "", errors.New("not found")
		}
		return secret, nil
	}
	keyringDelete = func(service, user string) error {
		delete(store, service+"/"+user)
		return nil
	}

	t.Cleanup(func() {
		keyringSet, keyringGet, keyringDelete = originalSet, originalGet, originalDelete
	})

	return store
}

func TestSaveLoginFallsBackToPlaintextWhenKeyringIsUnavailable(t *testing.T) {
	clearAuthEnvironment(t)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	withUnavailableKeyring(t)

	result, err := SaveLogin(LoginInput{Host: "https://fallback.example.invalid", Token: "tok", SetDefault: true})
	if err != nil {
		t.Fatalf("expected the fallback to succeed, got %v", err)
	}
	if !result.UsedInsecureStorage {
		t.Fatal("expected the result to report insecure storage")
	}

	// The credential really is in the file; the warning is not cosmetic.
	contents, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(contents), "tok") {
		t.Fatalf("expected the token in the config file, got:\n%s", contents)
	}
}

func TestSaveLoginRefusesToFallBackWhenKeyringIsRequired(t *testing.T) {
	clearAuthEnvironment(t)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	withUnavailableKeyring(t)

	_, err := SaveLogin(LoginInput{Host: "https://required.example.invalid", Token: "tok", RequireKeyring: true})
	if err == nil {
		t.Fatal("expected --require-keyring to fail rather than fall back")
	}
	if apperrors.KindOf(err) != apperrors.KindPermanent {
		t.Fatalf("expected permanent, got kind %q (%v)", apperrors.KindOf(err), err)
	}

	// Refusing must not leave the secret behind. A policy that errors after
	// writing the file would be worse than no policy at all.
	contents, readErr := os.ReadFile(configPath)
	if readErr == nil && strings.Contains(string(contents), "tok") {
		t.Fatalf("token was written despite the refusal:\n%s", contents)
	}
}

func TestSaveLoginRefusesToFallBackWhenPolicyComesFromTheEnvironment(t *testing.T) {
	clearAuthEnvironment(t)
	t.Setenv("BB_CONFIG_PATH", filepath.Join(t.TempDir(), "config.yaml"))
	t.Setenv("BB_REQUIRE_KEYRING", "1")
	withUnavailableKeyring(t)

	// An operator mandating keyring storage fleet-wide sets the variable; they
	// cannot add a flag to every invocation a user or agent makes.
	_, err := SaveLogin(LoginInput{Host: "https://env-policy.example.invalid", Token: "tok"})
	if err == nil {
		t.Fatal("expected BB_REQUIRE_KEYRING to fail the login")
	}
	if apperrors.KindOf(err) != apperrors.KindPermanent {
		t.Fatalf("expected permanent, got kind %q (%v)", apperrors.KindOf(err), err)
	}
}

func TestSaveLoginKeepsSecretsOutOfTheConfigFileWhenKeyringWorks(t *testing.T) {
	clearAuthEnvironment(t)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	store := withWorkingKeyring(t)

	result, err := SaveLogin(LoginInput{Host: "https://keyring.example.invalid", Token: "secret-token", SetDefault: true})
	if err != nil {
		t.Fatalf("expected login to succeed, got %v", err)
	}
	if result.UsedInsecureStorage {
		t.Fatal("did not expect insecure storage with a working keyring")
	}

	contents, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(contents), "secret-token") {
		t.Fatalf("token leaked into the config file:\n%s", contents)
	}
	if len(store) == 0 {
		t.Fatal("expected the secret to reach the keyring")
	}
}

func TestStaleInsecureEntryBesideAWorkingKeyringIsNotReportedAsInsecure(t *testing.T) {
	clearAuthEnvironment(t)
	host := "https://stale.example.invalid"
	configPath := writePlaintextCredentialConfig(t, host)
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BITBUCKET_URL", host)

	store := withWorkingKeyring(t)
	store["bb/"+host+":token"] = "keyring-token"

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// The keyring answered, so the leftover file entry was never adopted and
	// must not make every command warn.
	if cfg.UsedInsecureStorage {
		t.Fatal("a stale file entry beside a working keyring must not report insecure storage")
	}
	if cfg.BitbucketToken != "keyring-token" {
		t.Fatalf("expected the keyring token, got %q", cfg.BitbucketToken)
	}
	if got := cfg.CredentialStorage(); got != "keyring" {
		t.Fatalf("expected keyring, got %q", got)
	}
}

func TestSaveLoginRefusesBasicAuthFallbackWhenKeyringIsRequired(t *testing.T) {
	clearAuthEnvironment(t)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	withUnavailableKeyring(t)

	// Basic auth stores a password rather than a token, and took a separate
	// branch; the policy has to hold on both.
	_, err := SaveLogin(LoginInput{
		Host:           "https://basic-required.example.invalid",
		Username:       "alice",
		Password:       "hunter2",
		RequireKeyring: true,
	})
	if err == nil {
		t.Fatal("expected --require-keyring to fail for basic auth too")
	}
	if apperrors.KindOf(err) != apperrors.KindPermanent {
		t.Fatalf("expected permanent, got kind %q (%v)", apperrors.KindOf(err), err)
	}

	contents, readErr := os.ReadFile(configPath)
	if readErr == nil && strings.Contains(string(contents), "hunter2") {
		t.Fatalf("password was written despite the refusal:\n%s", contents)
	}
}

func TestSaveLoginFallsBackToPlaintextForBasicAuth(t *testing.T) {
	clearAuthEnvironment(t)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	withUnavailableKeyring(t)

	result, err := SaveLogin(LoginInput{
		Host:       "https://basic-fallback.example.invalid",
		Username:   "alice",
		Password:   "hunter2",
		SetDefault: true,
	})
	if err != nil {
		t.Fatalf("expected the fallback to succeed, got %v", err)
	}
	if !result.UsedInsecureStorage || result.AuthMode != "basic" {
		t.Fatalf("unexpected result %+v", result)
	}
}

func TestBasicAuthCredentialsAreReadFromTheKeyring(t *testing.T) {
	clearAuthEnvironment(t)
	host := "https://basic-keyring.example.invalid"
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	contents := "default_host: " + host + "\nhosts:\n    " + host + ":\n        url: " + host +
		"\n        username: alice\n        auth_mode: basic\n"
	if err := os.WriteFile(configPath, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BITBUCKET_URL", host)

	store := withWorkingKeyring(t)
	store["bb/"+host+":password"] = "keyring-password"

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if cfg.BitbucketPassword != "keyring-password" {
		t.Fatalf("expected the keyring password, got %q", cfg.BitbucketPassword)
	}
	if cfg.UsedInsecureStorage {
		t.Fatal("a keyring-sourced password must not report insecure storage")
	}
}

// TestAmbientAuthEnvironmentDoesNotSuppressThePlaintextReport guards the hole
// that reached CI: AuthSource was relabelled "env" whenever any auth variable
// was set, and that relabelling used to clear UsedInsecureStorage.
//
// A CI runner with ADMIN_USER exported would then use the plaintext token from
// the config file while reporting "environment", suppressing both the warning
// and the BB_REQUIRE_KEYRING check.
func TestAmbientAuthEnvironmentDoesNotSuppressThePlaintextReport(t *testing.T) {
	clearAuthEnvironment(t)
	host := "https://ambient-env.example.invalid"
	t.Setenv("BB_CONFIG_PATH", writePlaintextCredentialConfig(t, host))
	t.Setenv("BITBUCKET_URL", host)
	// Set, but supplying no token — the credential in use still comes from the
	// plaintext file.
	t.Setenv("ADMIN_USER", "admin")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if cfg.BitbucketToken != "plaintext-token" {
		t.Fatalf("expected the plaintext token in use, got %q", cfg.BitbucketToken)
	}
	if !cfg.UsedInsecureStorage {
		t.Fatal("an unrelated auth environment variable must not clear the plaintext report")
	}
	if got := cfg.CredentialStorage(); got != "config-file-plaintext" {
		t.Fatalf("expected config-file-plaintext, got %q", got)
	}
}

func TestAmbientAuthEnvironmentDoesNotBypassTheKeyringPolicy(t *testing.T) {
	clearAuthEnvironment(t)
	host := "https://ambient-bypass.example.invalid"
	t.Setenv("BB_CONFIG_PATH", writePlaintextCredentialConfig(t, host))
	t.Setenv("BITBUCKET_URL", host)
	t.Setenv("ADMIN_USER", "admin")
	t.Setenv("BB_REQUIRE_KEYRING", "1")

	// The policy must still refuse: the secret genuinely came off disk.
	if _, err := LoadFromEnv(); err == nil {
		t.Fatal("expected the policy to hold despite an ambient auth variable")
	}
}

func TestEnvironmentSuppliedTokenIsNotReportedAsPlaintext(t *testing.T) {
	clearAuthEnvironment(t)
	host := "https://env-token.example.invalid"
	t.Setenv("BB_CONFIG_PATH", writePlaintextCredentialConfig(t, host))
	t.Setenv("BITBUCKET_URL", host)
	// This one really does supply the credential, so the file entry is unused.
	t.Setenv("BITBUCKET_TOKEN", "token-from-environment")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if cfg.BitbucketToken != "token-from-environment" {
		t.Fatalf("expected the environment token, got %q", cfg.BitbucketToken)
	}
	if cfg.UsedInsecureStorage {
		t.Fatal("an unused file entry must not be reported as in use")
	}
	if got := cfg.CredentialStorage(); got != "environment" {
		t.Fatalf("expected environment, got %q", got)
	}
}
