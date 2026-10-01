package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
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

// useStoredConfig turns the stored config back on, which the seal turns off,
// and clears BB_REQUIRE_KEYRING, which the seal leaves alone. The credential
// variables need nothing: the seal has already emptied them.
func useStoredConfig(t *testing.T) {
	t.Helper()

	t.Setenv("BB_REQUIRE_KEYRING", "")
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")
}

func TestLoadFromEnvFlagsCredentialsReadFromPlaintextFallback(t *testing.T) {
	useStoredConfig(t)
	host := "https://plaintext-flag.example.invalid"
	t.Setenv("BB_CONFIG_PATH", writePlaintextCredentialConfig(t, host))

	cfg, err := LoadWithOverrides(Overrides{Host: host})
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
	useStoredConfig(t)
	host := "https://plaintext-refused.example.invalid"
	t.Setenv("BB_CONFIG_PATH", writePlaintextCredentialConfig(t, host))
	t.Setenv("BB_REQUIRE_KEYRING", "1")

	_, err := LoadWithOverrides(Overrides{Host: host})
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

// TestTheStrictLookupRefusesPlaintextWhenKeyringRequired holds the requirement
// where git's credential helper and a clone read a credential (ADR-047: it
// holds where a credential is read). Both go through LoadStoredAuthForHostStrict
// rather than the configuration load, and the lookup handed them the plaintext
// fallback the load refuses.
func TestTheStrictLookupRefusesPlaintextWhenKeyringRequired(t *testing.T) {
	cases := []struct {
		name    string
		require func(t *testing.T)
	}{
		{
			name:    "BB_REQUIRE_KEYRING",
			require: func(t *testing.T) { t.Setenv("BB_REQUIRE_KEYRING", "1") },
		},
		{
			name: "the require_keyring policy",
			require: func(t *testing.T) {
				policy := filepath.Join(t.TempDir(), "system.yaml")
				if err := os.WriteFile(policy, []byte("require_keyring: true\n"), 0o600); err != nil {
					t.Fatalf("write policy: %v", err)
				}
				t.Setenv("BB_SYSTEM_CONFIG_PATH", policy)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			useStoredConfig(t)
			host := "https://strict-plaintext.example.invalid"
			t.Setenv("BB_CONFIG_PATH", writePlaintextCredentialConfig(t, host))
			t.Setenv("BB_SYSTEM_CONFIG_PATH", filepath.Join(t.TempDir(), "absent.yaml"))

			// Without the requirement the plaintext credential is the answer, so
			// the refusal below is the requirement's doing.
			allowed, found, err := LoadStoredAuthForHostStrict(host)
			if err != nil || !found || allowed.BitbucketToken != "plaintext-token" {
				t.Fatalf("without the requirement: found=%v err=%v; the check below proves nothing", found, err)
			}

			tc.require(t)

			resolved, found, err := LoadStoredAuthForHostStrict(host)
			if err == nil {
				t.Fatalf("the plaintext credential was handed out under the requirement: found=%v, token present=%v", found, resolved.BitbucketToken != "")
			}
			if found || resolved.BitbucketToken != "" {
				t.Fatal("a refusal still returned the credential")
			}
			if apperrors.KindOf(err) != apperrors.KindPermanent {
				t.Fatalf("expected permanent, got kind %q (%v)", apperrors.KindOf(err), err)
			}
			message := apperrors.MessageOf(err)
			if !strings.Contains(message, "keyring") || !strings.Contains(message, host) {
				t.Fatalf("the refusal does not say the keyring is required for %s: %s", host, message)
			}
			if strings.Contains(err.Error(), "plaintext-token") {
				t.Fatalf("the refusal carries the credential: %v", err)
			}
		})
	}
}

// TestTheStrictLookupKeepsAKeyringCredentialWhenKeyringRequired: the
// requirement refuses only a secret adopted from the plaintext fallback. A
// stale file entry beside a credential the keyring holds is not one.
func TestTheStrictLookupKeepsAKeyringCredentialWhenKeyringRequired(t *testing.T) {
	useStoredConfig(t)
	host := "https://strict-keyring.example.invalid"
	t.Setenv("BB_CONFIG_PATH", writePlaintextCredentialConfig(t, host))
	t.Setenv("BB_SYSTEM_CONFIG_PATH", filepath.Join(t.TempDir(), "absent.yaml"))
	t.Setenv("BB_REQUIRE_KEYRING", "1")

	store := withWorkingKeyring(t)
	store["bb/"+host+":token"] = "keyring-token"

	resolved, found, err := LoadStoredAuthForHostStrict(host)
	if err != nil {
		t.Fatalf("a keyring credential was refused: %v", err)
	}
	if !found || resolved.BitbucketToken != "keyring-token" || resolved.UsedInsecureStorage {
		t.Fatalf("expected the keyring token, got found=%v keyring=%v insecure=%v", found, resolved.BitbucketToken == "keyring-token", resolved.UsedInsecureStorage)
	}
}

func TestLoadFromEnvAllowsEnvironmentCredentialsWhenKeyringRequired(t *testing.T) {
	useStoredConfig(t)
	host := "https://env-wins.example.invalid"
	t.Setenv("BB_CONFIG_PATH", writePlaintextCredentialConfig(t, host))
	t.Setenv("BB_REQUIRE_KEYRING", "1")
	// A token supplied per invocation never touches the config file, so the
	// policy has nothing to object to — this is the documented escape hatch.
	t.Setenv("BITBUCKET_TOKEN", "token-from-environment")

	cfg, err := LoadWithOverrides(Overrides{Host: host})
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
	useStoredConfig(t)
	host := "https://malformed-policy.example.invalid"
	t.Setenv("BB_CONFIG_PATH", writePlaintextCredentialConfig(t, host))
	t.Setenv("BB_REQUIRE_KEYRING", "yes-please")

	_, err := LoadWithOverrides(Overrides{Host: host})
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
	useStoredConfig(t)
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
	t.Parallel()

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
	t.Parallel()

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

// withUnavailableKeyring simulates the machines with no keyring: headless
// servers, containers, WSL without gnome-keyring.
func withUnavailableKeyring(t *testing.T) {
	t.Helper()

	UseUnavailableKeyring(t, errors.New("no keyring daemon"))
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

func TestSaveLoginStoresPlaintextWhenAskedAndTheKeyringIsUnavailable(t *testing.T) {
	useStoredConfig(t)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	withUnavailableKeyring(t)

	result, err := SaveLogin(LoginInput{Host: "https://fallback.example.invalid", Token: "tok", SetDefault: true, AllowInsecureStorage: true})
	if err != nil {
		t.Fatalf("expected the login to store the token in plaintext, got %v", err)
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

// existingConfig writes a config file holding another host, so a test can
// assert that a refused login left it byte for byte as it was.
func existingConfig(t *testing.T) (string, []byte) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	contents := []byte("default_host: https://other.example.invalid\n" +
		"hosts:\n" +
		"    https://other.example.invalid:\n" +
		"        url: https://other.example.invalid\n" +
		"        auth_mode: token\n")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	return path, contents
}

func assertConfigUnchanged(t *testing.T, path string, before []byte) {
	t.Helper()

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("a refused login rewrote the config file:\n%s", after)
	}
}

// TestSaveLoginRefusesPlaintextUnlessAsked is the rule: where the keyring
// cannot hold the secret, nobody lands in plaintext without deciding to.
func TestSaveLoginRefusesPlaintextUnlessAsked(t *testing.T) {
	cases := []struct {
		name   string
		input  LoginInput
		secret string
	}{
		{"token", LoginInput{Host: "https://refused.example.invalid", Token: "refused-token", SetDefault: true}, "refused-token"},
		{"basic auth", LoginInput{Host: "https://refused-basic.example.invalid", Username: "alice", Password: "refused-password", SetDefault: true}, "refused-password"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			useStoredConfig(t)
			configPath, before := existingConfig(t)
			t.Setenv("BB_CONFIG_PATH", configPath)
			withUnavailableKeyring(t)

			_, err := SaveLogin(tc.input)
			if err == nil {
				t.Fatal("expected the login to refuse plaintext it was not asked for")
			}
			if apperrors.KindOf(err) != apperrors.KindPermanent || apperrors.ExitCode(err) != 1 {
				t.Fatalf("expected permanent, exit 1, got kind %q (%v)", apperrors.KindOf(err), err)
			}
			// Both ways forward, so the reader does not have to look either up.
			message := apperrors.MessageOf(err)
			for _, want := range []string{"keyring is unavailable", "--allow-insecure-storage", "BITBUCKET_TOKEN"} {
				if !strings.Contains(message, want) {
					t.Errorf("the refusal does not name %q: %s", want, message)
				}
			}
			if strings.Contains(message, tc.secret) {
				t.Errorf("the refusal carries the secret: %s", message)
			}

			assertConfigUnchanged(t, configPath, before)
		})
	}
}

// TestSaveLoginRequirementOutranksAllowInsecureStorage holds the order: an
// operator who mandates the keyring has decided, and the flag does not undo it.
func TestSaveLoginRequirementOutranksAllowInsecureStorage(t *testing.T) {
	cases := []struct {
		name    string
		require func(t *testing.T, input *LoginInput)
		message string
	}{
		{
			name:    "--require-keyring",
			require: func(_ *testing.T, input *LoginInput) { input.RequireKeyring = true },
			message: "keyring-backed storage is required;",
		},
		{
			name:    "BB_REQUIRE_KEYRING",
			require: func(t *testing.T, _ *LoginInput) { t.Setenv("BB_REQUIRE_KEYRING", "1") },
			message: "keyring-backed storage is required;",
		},
		{
			name: "the require_keyring policy",
			require: func(t *testing.T, _ *LoginInput) {
				policy := filepath.Join(t.TempDir(), "system.yaml")
				if err := os.WriteFile(policy, []byte("require_keyring: true\n"), 0o600); err != nil {
					t.Fatalf("write policy: %v", err)
				}
				t.Setenv("BB_SYSTEM_CONFIG_PATH", policy)
			},
			message: "required by administrative policy",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			useStoredConfig(t)
			configPath, before := existingConfig(t)
			t.Setenv("BB_CONFIG_PATH", configPath)
			t.Setenv("BB_SYSTEM_CONFIG_PATH", filepath.Join(t.TempDir(), "absent.yaml"))
			withUnavailableKeyring(t)

			input := LoginInput{Host: "https://outranked.example.invalid", Token: "outranked-token", AllowInsecureStorage: true}
			tc.require(t, &input)

			_, err := SaveLogin(input)
			if err == nil {
				t.Fatal("expected the requirement to refuse plaintext despite --allow-insecure-storage")
			}
			if apperrors.KindOf(err) != apperrors.KindPermanent {
				t.Fatalf("expected permanent, got kind %q (%v)", apperrors.KindOf(err), err)
			}
			if message := apperrors.MessageOf(err); !strings.Contains(message, tc.message) {
				t.Fatalf("expected the requirement's own message, got %s", message)
			}

			assertConfigUnchanged(t, configPath, before)
		})
	}
}

// TestARefusedLoginLeavesTheStoredPlaintextCredentialWorking is the upgrade
// path: a credential stored in plaintext before the rule keeps working on the
// machine with no keyring, and a login refused there does not take it away.
func TestARefusedLoginLeavesTheStoredPlaintextCredentialWorking(t *testing.T) {
	useStoredConfig(t)
	host := "https://upgraded.example.invalid"
	t.Setenv("BB_CONFIG_PATH", writePlaintextCredentialConfig(t, host))
	withUnavailableKeyring(t)

	if _, err := SaveLogin(LoginInput{Host: host, Token: "rotated-token"}); err == nil {
		t.Fatal("expected the new login to be refused without --allow-insecure-storage")
	}

	cfg, err := LoadWithOverrides(Overrides{Host: host})
	if err != nil {
		t.Fatalf("the stored plaintext credential no longer loads: %v", err)
	}
	if cfg.BitbucketToken != "plaintext-token" || !cfg.UsedInsecureStorage {
		t.Fatalf("expected the stored plaintext token in use, got token %q, insecure %v", cfg.BitbucketToken, cfg.UsedInsecureStorage)
	}
}

func TestSaveLoginRefusesToFallBackWhenKeyringIsRequired(t *testing.T) {
	useStoredConfig(t)
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
	useStoredConfig(t)
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
	useStoredConfig(t)
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
	useStoredConfig(t)
	host := "https://stale.example.invalid"
	configPath := writePlaintextCredentialConfig(t, host)
	t.Setenv("BB_CONFIG_PATH", configPath)

	store := withWorkingKeyring(t)
	store["bb/"+host+":token"] = "keyring-token"

	cfg, err := LoadWithOverrides(Overrides{Host: host})
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
	useStoredConfig(t)
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

func TestSaveLoginStoresPlaintextForBasicAuthWhenAsked(t *testing.T) {
	useStoredConfig(t)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	withUnavailableKeyring(t)

	result, err := SaveLogin(LoginInput{
		Host:                 "https://basic-fallback.example.invalid",
		Username:             "alice",
		Password:             "hunter2",
		SetDefault:           true,
		AllowInsecureStorage: true,
	})
	if err != nil {
		t.Fatalf("expected the login to store the password in plaintext, got %v", err)
	}
	if !result.UsedInsecureStorage || result.AuthMode != "basic" {
		t.Fatalf("unexpected result %+v", result)
	}
}

func TestBasicAuthCredentialsAreReadFromTheKeyring(t *testing.T) {
	useStoredConfig(t)
	host := "https://basic-keyring.example.invalid"
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	contents := "default_host: " + host + "\nhosts:\n    " + host + ":\n        url: " + host +
		"\n        username: alice\n        auth_mode: basic\n"
	if err := os.WriteFile(configPath, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("BB_CONFIG_PATH", configPath)

	store := withWorkingKeyring(t)
	store["bb/"+host+":password"] = "keyring-password"

	cfg, err := LoadWithOverrides(Overrides{Host: host})
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
	useStoredConfig(t)
	host := "https://ambient-env.example.invalid"
	t.Setenv("BB_CONFIG_PATH", writePlaintextCredentialConfig(t, host))
	// Set, but supplying no token — the credential in use still comes from the
	// plaintext file.
	t.Setenv("ADMIN_USER", "admin")

	cfg, err := LoadWithOverrides(Overrides{Host: host})
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
	useStoredConfig(t)
	host := "https://ambient-bypass.example.invalid"
	t.Setenv("BB_CONFIG_PATH", writePlaintextCredentialConfig(t, host))
	t.Setenv("ADMIN_USER", "admin")
	t.Setenv("BB_REQUIRE_KEYRING", "1")

	// The policy must still refuse: the secret genuinely came off disk.
	if _, err := LoadWithOverrides(Overrides{Host: host}); err == nil {
		t.Fatal("expected the policy to hold despite an ambient auth variable")
	}
}

func TestEnvironmentSuppliedTokenIsNotReportedAsPlaintext(t *testing.T) {
	useStoredConfig(t)
	host := "https://env-token.example.invalid"
	t.Setenv("BB_CONFIG_PATH", writePlaintextCredentialConfig(t, host))
	// This one really does supply the credential, so the file entry is unused.
	t.Setenv("BITBUCKET_TOKEN", "token-from-environment")

	cfg, err := LoadWithOverrides(Overrides{Host: host})
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
