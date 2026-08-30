package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
)

func TestLoadFromEnvNonHostDefaults(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "http://localhost:7990")
	t.Setenv("BITBUCKET_VERSION_TARGET", "")
	t.Setenv("BITBUCKET_PROJECT_KEY", "")
	t.Setenv("BB_CA_FILE", "")
	t.Setenv("BB_INSECURE_SKIP_VERIFY", "")
	t.Setenv("BB_REQUEST_TIMEOUT", "")
	t.Setenv("BB_RETRY_COUNT", "")
	t.Setenv("BB_RETRY_BACKOFF", "")
	t.Setenv("BB_LOG_LEVEL", "")
	t.Setenv("BB_LOG_FORMAT", "")

	config, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if config.BitbucketURL != "http://localhost:7990" {
		t.Fatalf("expected url %q, got %q", "http://localhost:7990", config.BitbucketURL)
	}
	if config.RequestTimeout != defaultRequestTimeout {
		t.Fatalf("expected default timeout %s, got %s", defaultRequestTimeout, config.RequestTimeout)
	}
	if config.RetryCount != defaultRetryCount {
		t.Fatalf("expected default retry count %d, got %d", defaultRetryCount, config.RetryCount)
	}
	if config.RetryBackoff != defaultRetryBackoff {
		t.Fatalf("expected default retry backoff %s, got %s", defaultRetryBackoff, config.RetryBackoff)
	}
	if config.LogLevel != defaultLogLevel {
		t.Fatalf("expected default log level %q, got %q", defaultLogLevel, config.LogLevel)
	}
	if config.LogFormat != defaultLogFormat {
		t.Fatalf("expected default log format %q, got %q", defaultLogFormat, config.LogFormat)
	}
	if config.DiagnosticsEnabled {
		t.Fatal("expected diagnostics to be disabled by default")
	}
}

func TestLoadFromEnvErrorsWhenNoHostConfigured(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "")
	t.Setenv("BB_CONFIG_PATH", filepath.Join(t.TempDir(), "empty-config.yaml"))

	_, err := LoadFromEnv()
	if err == nil {
		t.Fatal("expected error when no host is configured, got nil")
	}
	if !apperrors.IsKind(err, apperrors.KindValidation) {
		t.Fatalf("expected KindValidation error, got: %v", err)
	}
}

func TestDotenvCandidatesWalkToRepositoryRoot(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoRoot, "go.mod"), []byte("module example.com/testrepo\n\ngo 1.24\n"), 0o600); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	nested := filepath.Join(repoRoot, "internal", "config")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	t.Chdir(nested)

	candidates := dotenvCandidates()
	if len(candidates) < 2 {
		t.Fatalf("expected multiple dotenv candidates, got %#v", candidates)
	}
	expectedPrefix := []string{
		filepath.Join(nested, ".env"),
		filepath.Join(filepath.Dir(nested), ".env"),
		filepath.Join(repoRoot, ".env"),
	}
	if !reflect.DeepEqual(candidates[:len(expectedPrefix)], expectedPrefix) {
		t.Fatalf("unexpected dotenv candidates prefix: got %#v want %#v", candidates[:len(expectedPrefix)], expectedPrefix)
	}

	for _, candidate := range candidates {
		if filepath.Dir(candidate) == filepath.Dir(repoRoot) {
			t.Fatalf("expected candidates to stop at repository root, found parent directory candidate: %q", candidate)
		}
	}
}

func TestLoadFromEnvFindsRepositoryDotenvFromNestedWorkingDirectory(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoRoot, "go.mod"), []byte("module example.com/testrepo\n\ngo 1.24\n"), 0o600); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, ".env"), []byte("BITBUCKET_URL=http://localhost:7990\nBITBUCKET_USERNAME=test-user\nBITBUCKET_PASSWORD=test-pass\n"), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	nested := filepath.Join(repoRoot, "internal", "config")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	t.Chdir(nested)

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	unsetEnvKeys(t,
		"BITBUCKET_USERNAME",
		"BITBUCKET_PASSWORD",
		"BITBUCKET_USER",
		"ADMIN_USER",
		"ADMIN_PASSWORD",
		"BITBUCKET_TOKEN",
		"BITBUCKET_URL",
		"BB_REQUEST_TIMEOUT",
		"BB_RETRY_COUNT",
		"BB_RETRY_BACKOFF",
		"BB_LOG_LEVEL",
		"BB_LOG_FORMAT",
	)

	loaded, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("load from env: %v", err)
	}
	if loaded.BitbucketUsername != "test-user" || loaded.BitbucketPassword != "test-pass" {
		t.Fatalf("expected credentials from repository .env, got username=%q password=%q", loaded.BitbucketUsername, loaded.BitbucketPassword)
	}
}

func unsetEnvKeys(t *testing.T, keys ...string) {
	t.Helper()

	for _, key := range keys {
		value, found := os.LookupEnv(key)
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unsetenv %s: %v", key, err)
		}
		t.Cleanup(func() {
			if found {
				_ = os.Setenv(key, value)
				return
			}
			_ = os.Unsetenv(key)
		})
	}
}

func TestLoadFromEnvTransportOverrides(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "http://localhost:7990")
	t.Setenv("BB_INSECURE_SKIP_VERIFY", "true")
	t.Setenv("BB_REQUEST_TIMEOUT", "45s")
	t.Setenv("BB_RETRY_COUNT", "5")
	t.Setenv("BB_RETRY_BACKOFF", "900ms")
	t.Setenv("BB_LOG_LEVEL", "debug")
	t.Setenv("BB_LOG_FORMAT", "jsonl")

	caFile := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caFile, []byte("-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"), 0o600); err != nil {
		t.Fatalf("write ca file: %v", err)
	}
	t.Setenv("BB_CA_FILE", caFile)

	certFile := filepath.Join(t.TempDir(), "client.crt")
	if err := os.WriteFile(certFile, []byte("-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"), 0o600); err != nil {
		t.Fatalf("write client cert file: %v", err)
	}
	t.Setenv("BB_CLIENT_CERT", certFile)

	keyFile := filepath.Join(t.TempDir(), "client.key")
	if err := os.WriteFile(keyFile, []byte("-----BEGIN PRIVATE KEY-----\nMIIB\n-----END PRIVATE KEY-----\n"), 0o600); err != nil {
		t.Fatalf("write client key file: %v", err)
	}
	t.Setenv("BB_CLIENT_KEY", keyFile)

	loaded, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if !loaded.InsecureSkipVerify {
		t.Fatal("expected insecure skip verify to be true")
	}
	if loaded.RequestTimeout != 45*time.Second {
		t.Fatalf("unexpected request timeout: %s", loaded.RequestTimeout)
	}
	if loaded.RetryCount != 5 {
		t.Fatalf("unexpected retry count: %d", loaded.RetryCount)
	}
	if loaded.RetryBackoff != 900*time.Millisecond {
		t.Fatalf("unexpected retry backoff: %s", loaded.RetryBackoff)
	}
	if loaded.CAFile != caFile {
		t.Fatalf("unexpected ca file: %q", loaded.CAFile)
	}
	if loaded.ClientCertFile != certFile {
		t.Fatalf("unexpected client cert file: %q", loaded.ClientCertFile)
	}
	if loaded.ClientKeyFile != keyFile {
		t.Fatalf("unexpected client key file: %q", loaded.ClientKeyFile)
	}
	if loaded.LogLevel != "debug" {
		t.Fatalf("unexpected log level: %q", loaded.LogLevel)
	}
	if loaded.LogFormat != "jsonl" {
		t.Fatalf("unexpected log format: %q", loaded.LogFormat)
	}
	if !loaded.DiagnosticsEnabled {
		t.Fatal("expected diagnostics to be enabled when logging env is configured")
	}
}

func TestLoadFromEnvTransportOverrideValidation(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BB_CA_FILE", "")
	t.Setenv("BB_LOG_LEVEL", "")
	t.Setenv("BB_LOG_FORMAT", "")

	t.Run("invalid bool", func(t *testing.T) {
		t.Setenv("BB_INSECURE_SKIP_VERIFY", "maybe")
		t.Setenv("BB_REQUEST_TIMEOUT", "")
		t.Setenv("BB_RETRY_COUNT", "")
		t.Setenv("BB_RETRY_BACKOFF", "")
		if _, err := LoadFromEnv(); err == nil {
			t.Fatal("expected validation error")
		}
	})

	t.Run("invalid timeout", func(t *testing.T) {
		t.Setenv("BB_INSECURE_SKIP_VERIFY", "")
		t.Setenv("BB_REQUEST_TIMEOUT", "soon")
		t.Setenv("BB_RETRY_COUNT", "")
		t.Setenv("BB_RETRY_BACKOFF", "")
		if _, err := LoadFromEnv(); err == nil {
			t.Fatal("expected validation error")
		}
	})

	t.Run("invalid retry count", func(t *testing.T) {
		t.Setenv("BB_INSECURE_SKIP_VERIFY", "")
		t.Setenv("BB_REQUEST_TIMEOUT", "")
		t.Setenv("BB_RETRY_COUNT", "-2")
		t.Setenv("BB_RETRY_BACKOFF", "")
		if _, err := LoadFromEnv(); err == nil {
			t.Fatal("expected validation error")
		}
	})

	t.Run("invalid retry backoff", func(t *testing.T) {
		t.Setenv("BB_INSECURE_SKIP_VERIFY", "")
		t.Setenv("BB_REQUEST_TIMEOUT", "")
		t.Setenv("BB_RETRY_COUNT", "")
		t.Setenv("BB_RETRY_BACKOFF", "0s")
		if _, err := LoadFromEnv(); err == nil {
			t.Fatal("expected validation error")
		}
	})

	t.Run("invalid ca path", func(t *testing.T) {
		t.Setenv("BB_INSECURE_SKIP_VERIFY", "")
		t.Setenv("BB_REQUEST_TIMEOUT", "")
		t.Setenv("BB_RETRY_COUNT", "")
		t.Setenv("BB_RETRY_BACKOFF", "")
		t.Setenv("BB_CA_FILE", filepath.Join(t.TempDir(), "missing.pem"))
		if _, err := LoadFromEnv(); err == nil {
			t.Fatal("expected validation error")
		}
	})

	t.Run("invalid log level", func(t *testing.T) {
		t.Setenv("BB_INSECURE_SKIP_VERIFY", "")
		t.Setenv("BB_REQUEST_TIMEOUT", "")
		t.Setenv("BB_RETRY_COUNT", "")
		t.Setenv("BB_RETRY_BACKOFF", "")
		t.Setenv("BB_CA_FILE", "")
		t.Setenv("BB_LOG_LEVEL", "trace")
		t.Setenv("BB_LOG_FORMAT", "")
		if _, err := LoadFromEnv(); err == nil {
			t.Fatal("expected validation error")
		}
	})

	t.Run("invalid log format", func(t *testing.T) {
		t.Setenv("BB_INSECURE_SKIP_VERIFY", "")
		t.Setenv("BB_REQUEST_TIMEOUT", "")
		t.Setenv("BB_RETRY_COUNT", "")
		t.Setenv("BB_RETRY_BACKOFF", "")
		t.Setenv("BB_CA_FILE", "")
		t.Setenv("BB_LOG_LEVEL", "")
		t.Setenv("BB_LOG_FORMAT", "structured")
		if _, err := LoadFromEnv(); err == nil {
			t.Fatal("expected validation error")
		}
	})

	t.Run("client cert set without client key", func(t *testing.T) {
		certFile := filepath.Join(t.TempDir(), "client.crt")
		_ = os.WriteFile(certFile, []byte("cert"), 0o600)
		t.Setenv("BB_CLIENT_CERT", certFile)
		t.Setenv("BB_CLIENT_KEY", "")
		if _, err := LoadFromEnv(); err == nil {
			t.Fatal("expected validation error when client key is unset")
		}
	})

	t.Run("client key set without client cert", func(t *testing.T) {
		keyFile := filepath.Join(t.TempDir(), "client.key")
		_ = os.WriteFile(keyFile, []byte("key"), 0o600)
		t.Setenv("BB_CLIENT_CERT", "")
		t.Setenv("BB_CLIENT_KEY", keyFile)
		if _, err := LoadFromEnv(); err == nil {
			t.Fatal("expected validation error when client cert is unset")
		}
	})

	t.Run("missing client cert path", func(t *testing.T) {
		dir := t.TempDir()
		keyFile := filepath.Join(dir, "client.key")
		_ = os.WriteFile(keyFile, []byte("key"), 0o600)
		t.Setenv("BB_CLIENT_CERT", filepath.Join(dir, "missing.crt"))
		t.Setenv("BB_CLIENT_KEY", keyFile)
		if _, err := LoadFromEnv(); err == nil {
			t.Fatal("expected validation error for missing client cert")
		}
	})

	t.Run("missing client key path", func(t *testing.T) {
		dir := t.TempDir()
		certFile := filepath.Join(dir, "client.crt")
		_ = os.WriteFile(certFile, []byte("cert"), 0o600)
		t.Setenv("BB_CLIENT_CERT", certFile)
		t.Setenv("BB_CLIENT_KEY", filepath.Join(dir, "missing.key"))
		if _, err := LoadFromEnv(); err == nil {
			t.Fatal("expected validation error for missing client key")
		}
	})
}

func TestLoadFromEnvInvalidURL(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "://broken")
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")

	_, err := LoadFromEnv()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadFromEnvNormalizesURLAndAliasUsername(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "localhost:7990")
	t.Setenv("BITBUCKET_USER", "admin")
	t.Setenv("BITBUCKET_PASSWORD", "admin")
	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("ADMIN_USER", "")
	t.Setenv("ADMIN_PASSWORD", "")

	config, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if config.BitbucketURL != "https://localhost:7990" {
		t.Fatalf("expected normalized URL, got %q", config.BitbucketURL)
	}

	if config.BitbucketUsername != "admin" {
		t.Fatalf("expected username from BITBUCKET_USER alias, got %q", config.BitbucketUsername)
	}
}

func TestSaveLoginAndLoadStoredConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "bb", "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")

	_, err := SaveLogin(LoginInput{
		Host:       "localhost:7990",
		Username:   "admin",
		Password:   "admin",
		SetDefault: true,
	})
	if err != nil {
		t.Fatalf("expected save login to succeed, got: %v", err)
	}

	stored, err := LoadStoredConfig()
	if err != nil {
		t.Fatalf("expected load stored config to succeed, got: %v", err)
	}

	if stored.DefaultHost == "" {
		t.Fatal("expected default host to be set")
	}

	profile, ok := stored.Hosts[stored.DefaultHost]
	if !ok {
		t.Fatal("expected stored host profile")
	}

	if profile.URL != "https://localhost:7990" {
		t.Fatalf("unexpected stored URL: %q", profile.URL)
	}

	if profile.AuthMode != "basic" {
		t.Fatalf("unexpected auth mode: %q", profile.AuthMode)
	}
}

func TestSaveLoginWithClientCertAndKey(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "bb", "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")

	dir := t.TempDir()
	certFile := filepath.Join(dir, "client.crt")
	keyFile := filepath.Join(dir, "client.key")
	_ = os.WriteFile(certFile, []byte("cert"), 0o600)
	_ = os.WriteFile(keyFile, []byte("key"), 0o600)

	_, err := SaveLogin(LoginInput{
		Host:       "https://mtls.example.com",
		Token:      "token-val",
		ClientCert: certFile,
		ClientKey:  keyFile,
		SetDefault: true,
	})
	if err != nil {
		t.Fatalf("save login: %v", err)
	}

	stored, err := LoadStoredConfig()
	if err != nil {
		t.Fatalf("load stored config: %v", err)
	}

	profile := stored.Hosts["https://mtls.example.com"]
	if profile.ClientCert != certFile {
		t.Fatalf("expected client cert %q, got %q", certFile, profile.ClientCert)
	}
	if profile.ClientKey != keyFile {
		t.Fatalf("expected client key %q, got %q", keyFile, profile.ClientKey)
	}

	// Loading config with unset env adopts stored client cert & key
	t.Setenv("BITBUCKET_URL", "https://mtls.example.com")
	t.Setenv("BB_CLIENT_CERT", "")
	t.Setenv("BB_CLIENT_KEY", "")
	loaded, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("load from env: %v", err)
	}
	if loaded.ClientCertFile != certFile {
		t.Fatalf("expected adopted client cert %q, got %q", certFile, loaded.ClientCertFile)
	}
	if loaded.ClientKeyFile != keyFile {
		t.Fatalf("expected adopted client key %q, got %q", keyFile, loaded.ClientKeyFile)
	}
}

func TestListServerContextsAndSetDefaultHost(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "bb", "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")

	if _, err := SaveLogin(LoginInput{Host: "http://alpha.local:7990", Token: "token-alpha", SetDefault: true}); err != nil {
		t.Fatalf("save alpha login: %v", err)
	}
	if _, err := SaveLogin(LoginInput{Host: "http://beta.local:7990", Token: "token-beta", SetDefault: false}); err != nil {
		t.Fatalf("save beta login: %v", err)
	}

	contexts, err := ListServerContexts()
	if err != nil {
		t.Fatalf("list server contexts: %v", err)
	}
	if len(contexts) != 2 {
		t.Fatalf("expected 2 contexts, got %d", len(contexts))
	}
	if !contexts[0].IsDefault || contexts[0].Host != "http://alpha.local:7990" {
		t.Fatalf("expected alpha as default first entry, got %+v", contexts[0])
	}

	selected, err := SetDefaultHost("http://beta.local:7990")
	if err != nil {
		t.Fatalf("set default host: %v", err)
	}
	if selected != "http://beta.local:7990" {
		t.Fatalf("unexpected selected host: %q", selected)
	}

	contexts, err = ListServerContexts()
	if err != nil {
		t.Fatalf("list server contexts after update: %v", err)
	}
	if !contexts[0].IsDefault || contexts[0].Host != "http://beta.local:7990" {
		t.Fatalf("expected beta as default first entry, got %+v", contexts[0])
	}

	if _, err := SetDefaultHost("http://missing.local:7990"); err == nil || apperrors.ExitCode(err) != 4 {
		t.Fatalf("expected not found when selecting missing host, got: %v", err)
	}

	if _, err := SetDefaultHost(" "); err == nil || apperrors.ExitCode(err) != 2 {
		t.Fatalf("expected validation error for empty host, got: %v", err)
	}
}

func TestServerContextConfigErrorBranchesAndSorting(t *testing.T) {
	brokenConfigPath := t.TempDir()
	t.Setenv("BB_CONFIG_PATH", brokenConfigPath)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")

	if _, err := ListServerContexts(); err == nil {
		t.Fatal("expected list server contexts to fail for directory config path")
	}
	if _, err := SetDefaultHost("http://example.local:7990"); err == nil {
		t.Fatal("expected set default host to fail for directory config path")
	}

	baseDir := t.TempDir()
	configPath := filepath.Join(baseDir, "bb", "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)

	stored := StoredConfig{
		DefaultHost: "",
		Hosts: map[string]StoredProfile{
			"http://b.local:7990": {URL: "http://b.local:7990"},
			"http://a.local:7990": {URL: "http://a.local:7990", AuthMode: "token"},
		},
		InsecureSecrets: map[string]StoredSecret{},
	}
	if err := SaveStoredConfig(stored); err != nil {
		t.Fatalf("save stored config: %v", err)
	}

	contexts, err := ListServerContexts()
	if err != nil {
		t.Fatalf("list server contexts: %v", err)
	}
	if len(contexts) != 2 {
		t.Fatalf("expected two contexts, got %d", len(contexts))
	}
	if contexts[0].Host != "http://a.local:7990" {
		t.Fatalf("expected lexical sort when no default host, got %+v", contexts)
	}
	if contexts[1].AuthMode != "none" {
		t.Fatalf("expected empty auth_mode to fallback to none, got %+v", contexts[1])
	}

	parentFile := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(parentFile, []byte("x"), 0o600); err != nil {
		t.Fatalf("write parent marker file: %v", err)
	}
	t.Setenv("BB_CONFIG_PATH", filepath.Join(parentFile, "config.yaml"))
	if _, err := SetDefaultHost("http://a.local:7990"); err == nil {
		t.Fatal("expected save failure when config parent is a file")
	}
}

func TestConfigAuthModeAndLogoutBranches(t *testing.T) {
	if (AppConfig{}).AuthMode() != "none" {
		t.Fatal("expected auth mode none")
	}
	if (AppConfig{BitbucketToken: "t"}).AuthMode() != "token" {
		t.Fatal("expected auth mode token")
	}
	if (AppConfig{BitbucketUsername: "u", BitbucketPassword: "p"}).AuthMode() != "basic" {
		t.Fatal("expected auth mode basic")
	}

	configPath := filepath.Join(t.TempDir(), "bb", "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	if err := Logout(""); err == nil || apperrors.ExitCode(err) != 4 {
		t.Fatalf("expected not found logout error when no stored host, got: %v", err)
	}
}

func TestResolveStoredCredentialsAndLoadFromStoredHost(t *testing.T) {
	if _, ok := resolveStoredCredentials(StoredConfig{}, "http://localhost:7990"); ok {
		t.Fatal("expected not found when stored config is empty")
	}

	configPath := filepath.Join(t.TempDir(), "bb", "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")
	t.Setenv("BITBUCKET_URL", "")
	t.Setenv("BITBUCKET_TOKEN", "")
	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("BITBUCKET_USER", "")
	t.Setenv("BITBUCKET_PASSWORD", "")
	t.Setenv("ADMIN_USER", "")
	t.Setenv("ADMIN_PASSWORD", "")

	stored := StoredConfig{
		DefaultHost: "http://stored.local:7990",
		Hosts: map[string]StoredProfile{
			"http://stored.local:7990": {URL: "http://stored.local:7990", Username: "stored-user", AuthMode: "basic"},
		},
		InsecureSecrets: map[string]StoredSecret{
			"http://stored.local:7990": {Password: "stored-pass"},
		},
	}
	if err := SaveStoredConfig(stored); err != nil {
		t.Fatalf("save stored config: %v", err)
	}

	resolved, ok := resolveStoredCredentials(stored, "http://unknown.local:7990")
	if !ok {
		t.Fatal("expected stored credentials via default host")
	}
	if resolved.BitbucketURL != "http://stored.local:7990" || resolved.BitbucketUsername != "stored-user" || resolved.BitbucketPassword != "stored-pass" {
		t.Fatalf("unexpected resolved stored credentials: %+v", resolved)
	}

	loaded, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("load from env with stored host failed: %v", err)
	}
	if loaded.BitbucketURL != "http://stored.local:7990" {
		t.Fatalf("expected stored default host URL, got %q", loaded.BitbucketURL)
	}
	if loaded.AuthSource != "stored" {
		t.Fatalf("expected auth source stored, got %q", loaded.AuthSource)
	}
}

func TestSaveLoginValidationBranches(t *testing.T) {
	if _, err := SaveLogin(LoginInput{}); err == nil {
		t.Fatal("expected validation error for missing host")
	}
	if _, err := SaveLogin(LoginInput{Host: "localhost:7990", Token: "t", Username: "u", Password: "p"}); err == nil {
		t.Fatal("expected mutually exclusive auth input validation error")
	}
	if _, err := SaveLogin(LoginInput{Host: "localhost:7990", Username: "u"}); err == nil {
		t.Fatal("expected username/password pair validation error")
	}
}

func TestLoadFromEnvEnvSourceOverridesStored(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "bb", "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")
	t.Setenv("BITBUCKET_URL", "http://stored.local:7990")
	t.Setenv("BITBUCKET_USERNAME", "env-user")
	t.Setenv("BITBUCKET_PASSWORD", "env-pass")

	stored := StoredConfig{
		DefaultHost: "http://stored.local:7990",
		Hosts: map[string]StoredProfile{
			"http://stored.local:7990": {URL: "http://stored.local:7990", Username: "stored-user", AuthMode: "basic"},
		},
		InsecureSecrets: map[string]StoredSecret{
			"http://stored.local:7990": {Password: "stored-pass"},
		},
	}
	if err := SaveStoredConfig(stored); err != nil {
		t.Fatalf("save stored config: %v", err)
	}

	loaded, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("load from env failed: %v", err)
	}
	if loaded.AuthSource != "env" {
		t.Fatalf("expected env auth source override, got %q", loaded.AuthSource)
	}
	if loaded.BitbucketUsername != "env-user" {
		t.Fatalf("expected env username to win, got %q", loaded.BitbucketUsername)
	}
}

func TestLogoutExplicitHostRemovesProfile(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "bb", "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)

	stored := StoredConfig{
		DefaultHost: "http://one.local:7990",
		Hosts: map[string]StoredProfile{
			"http://one.local:7990": {URL: "http://one.local:7990", AuthMode: "token"},
			"http://two.local:7990": {URL: "http://two.local:7990", AuthMode: "basic", Username: "admin"},
		},
		InsecureSecrets: map[string]StoredSecret{
			"http://one.local:7990": {Token: "t"},
			"http://two.local:7990": {Password: "p"},
		},
	}
	if err := SaveStoredConfig(stored); err != nil {
		t.Fatalf("save stored config: %v", err)
	}

	if err := Logout("http://one.local:7990"); err != nil {
		t.Fatalf("logout explicit host failed: %v", err)
	}

	after, err := LoadStoredConfig()
	if err != nil {
		t.Fatalf("load stored config: %v", err)
	}
	if _, ok := after.Hosts["http://one.local:7990"]; ok {
		t.Fatal("expected logged out host removed")
	}
	if after.DefaultHost != "http://two.local:7990" {
		t.Fatalf("expected default host rotated to remaining profile, got %q", after.DefaultHost)
	}
}

func TestSaveLoginTokenAndMapInitialization(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "bb", "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)

	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write empty config: %v", err)
	}

	result, err := SaveLogin(LoginInput{Host: "stored.local:7990", Token: "token-1", SetDefault: false})
	if err != nil {
		t.Fatalf("save token login failed: %v", err)
	}
	if result.AuthMode != "token" {
		t.Fatalf("expected token auth mode, got %q", result.AuthMode)
	}

	stored, err := LoadStoredConfig()
	if err != nil {
		t.Fatalf("load stored config: %v", err)
	}
	if stored.DefaultHost == "" {
		t.Fatal("expected default host to be set when config had none")
	}
	if stored.Hosts[stored.DefaultHost].AuthMode != "token" {
		t.Fatalf("expected stored token auth mode, got %q", stored.Hosts[stored.DefaultHost].AuthMode)
	}
}

func TestLoadStoredConfigInvalidYAML(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "bb", "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)

	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(": invalid yaml"), 0o600); err != nil {
		t.Fatalf("write invalid config: %v", err)
	}

	if _, err := LoadStoredConfig(); err == nil {
		t.Fatal("expected yaml decode error")
	}
}

func TestValidateAndHostKeyBranches(t *testing.T) {
	if err := (AppConfig{BitbucketURL: "http://localhost:7990", ProjectKey: "TEST", RequestTimeout: time.Second, RetryCount: 0, RetryBackoff: time.Second}).Validate(); err != nil {
		t.Fatalf("expected empty log level/format to validate with defaults, got: %v", err)
	}

	if err := (AppConfig{BitbucketURL: "http://localhost:7990", ProjectKey: ""}).Validate(); err == nil {
		t.Fatal("expected empty project key validation error")
	}

	if err := (AppConfig{BitbucketURL: "http://localhost:7990", ProjectKey: "THIS_PROJECT_KEY_IS_TOO_LONG"}).Validate(); err == nil {
		t.Fatal("expected project key max length validation error")
	}

	if err := (AppConfig{BitbucketURL: "http://localhost:7990", ProjectKey: "TEST", BitbucketUsername: "user"}).Validate(); err == nil {
		t.Fatal("expected username/password pairing validation error")
	}

	if err := (AppConfig{BitbucketURL: "http://localhost:7990", ProjectKey: "TEST", BitbucketToken: "tok", BitbucketUsername: "user", RequestTimeout: time.Second, RetryCount: 0, RetryBackoff: time.Second}).Validate(); err != nil {
		t.Fatalf("expected token auth with optional username to validate, got: %v", err)
	}

	if err := (AppConfig{BitbucketURL: "http://localhost:7990", ProjectKey: "TEST", RequestTimeout: 0, RetryCount: 0, RetryBackoff: time.Second}).Validate(); err == nil {
		t.Fatal("expected request timeout validation error")
	}

	if err := (AppConfig{BitbucketURL: "http://localhost:7990", ProjectKey: "TEST", RequestTimeout: time.Second, RetryCount: -1, RetryBackoff: time.Second}).Validate(); err == nil {
		t.Fatal("expected retry count validation error")
	}

	if err := (AppConfig{BitbucketURL: "http://localhost:7990", ProjectKey: "TEST", RequestTimeout: time.Second, RetryCount: 0, RetryBackoff: 0}).Validate(); err == nil {
		t.Fatal("expected retry backoff validation error")
	}

	if err := (AppConfig{BitbucketURL: "http://localhost:7990", ProjectKey: "TEST", RequestTimeout: time.Second, RetryCount: 0, RetryBackoff: time.Second, CAFile: t.TempDir()}).Validate(); err == nil {
		t.Fatal("expected CA file path validation error for directory")
	}

	tempDir := t.TempDir()
	validCert := filepath.Join(tempDir, "valid.crt")
	validKey := filepath.Join(tempDir, "valid.key")
	_ = os.WriteFile(validCert, []byte("cert"), 0o600)
	_ = os.WriteFile(validKey, []byte("key"), 0o600)

	if err := (AppConfig{BitbucketURL: "http://localhost:7990", ProjectKey: "TEST", RequestTimeout: time.Second, RetryCount: 0, RetryBackoff: time.Second, ClientCertFile: validCert}).Validate(); err == nil {
		t.Fatal("expected validation error when only ClientCertFile is set")
	}

	if err := (AppConfig{BitbucketURL: "http://localhost:7990", ProjectKey: "TEST", RequestTimeout: time.Second, RetryCount: 0, RetryBackoff: time.Second, ClientKeyFile: validKey}).Validate(); err == nil {
		t.Fatal("expected validation error when only ClientKeyFile is set")
	}

	if err := (AppConfig{BitbucketURL: "http://localhost:7990", ProjectKey: "TEST", RequestTimeout: time.Second, RetryCount: 0, RetryBackoff: time.Second, ClientCertFile: tempDir, ClientKeyFile: validKey}).Validate(); err == nil {
		t.Fatal("expected validation error when ClientCertFile is a directory")
	}

	if err := (AppConfig{BitbucketURL: "http://localhost:7990", ProjectKey: "TEST", RequestTimeout: time.Second, RetryCount: 0, RetryBackoff: time.Second, ClientCertFile: validCert, ClientKeyFile: tempDir}).Validate(); err == nil {
		t.Fatal("expected validation error when ClientKeyFile is a directory")
	}

	if err := (AppConfig{BitbucketURL: "http://localhost:7990", ProjectKey: "TEST", RequestTimeout: time.Second, RetryCount: 0, RetryBackoff: time.Second, ClientCertFile: validCert, ClientKeyFile: validKey}).Validate(); err != nil {
		t.Fatalf("expected valid client cert and key to pass validation, got: %v", err)
	}

	if hostKey("://bad") == "" {
		t.Fatal("expected hostKey fallback value for invalid URL")
	}
}

func TestConfigEnvParsingHelpers(t *testing.T) {
	t.Run("env bool helper", func(t *testing.T) {
		t.Setenv("BB_PARSE_BOOL", "")
		value, err := envBoolOrDefault("BB_PARSE_BOOL", true)
		if err != nil || !value {
			t.Fatalf("expected fallback true, got value=%v err=%v", value, err)
		}

		t.Setenv("BB_PARSE_BOOL", "false")
		value, err = envBoolOrDefault("BB_PARSE_BOOL", true)
		if err != nil || value {
			t.Fatalf("expected parsed false, got value=%v err=%v", value, err)
		}

		t.Setenv("BB_PARSE_BOOL", "not-bool")
		if _, err = envBoolOrDefault("BB_PARSE_BOOL", false); err == nil {
			t.Fatal("expected parse error")
		}
	})

	t.Run("env int helper", func(t *testing.T) {
		t.Setenv("BB_PARSE_INT", "")
		value, err := envIntOrDefault("BB_PARSE_INT", 7)
		if err != nil || value != 7 {
			t.Fatalf("expected fallback 7, got value=%d err=%v", value, err)
		}

		t.Setenv("BB_PARSE_INT", "12")
		value, err = envIntOrDefault("BB_PARSE_INT", 7)
		if err != nil || value != 12 {
			t.Fatalf("expected parsed 12, got value=%d err=%v", value, err)
		}

		t.Setenv("BB_PARSE_INT", "12x")
		if _, err = envIntOrDefault("BB_PARSE_INT", 7); err == nil {
			t.Fatal("expected parse error")
		}
	})

	t.Run("env duration helper", func(t *testing.T) {
		t.Setenv("BB_PARSE_DUR", "")
		value, err := envDurationOrDefault("BB_PARSE_DUR", 2*time.Second)
		if err != nil || value != 2*time.Second {
			t.Fatalf("expected fallback 2s, got value=%s err=%v", value, err)
		}

		t.Setenv("BB_PARSE_DUR", "350ms")
		value, err = envDurationOrDefault("BB_PARSE_DUR", 2*time.Second)
		if err != nil || value != 350*time.Millisecond {
			t.Fatalf("expected parsed 350ms, got value=%s err=%v", value, err)
		}

		t.Setenv("BB_PARSE_DUR", "later")
		if _, err = envDurationOrDefault("BB_PARSE_DUR", time.Second); err == nil {
			t.Fatal("expected parse error")
		}
	})
}

func TestLoadFromEnvUsesStoredTokenBranch(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "bb", "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")
	t.Setenv("BITBUCKET_URL", "http://stored.local:7990")
	t.Setenv("BITBUCKET_TOKEN", "")
	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("BITBUCKET_USER", "")
	t.Setenv("BITBUCKET_PASSWORD", "")
	t.Setenv("ADMIN_USER", "")
	t.Setenv("ADMIN_PASSWORD", "")

	stored := StoredConfig{
		DefaultHost: "http://stored.local:7990",
		Hosts: map[string]StoredProfile{
			"http://stored.local:7990": {URL: "http://stored.local:7990", AuthMode: "token"},
		},
		InsecureSecrets: map[string]StoredSecret{
			"http://stored.local:7990": {Token: "stored-token"},
		},
	}
	if err := SaveStoredConfig(stored); err != nil {
		t.Fatalf("save stored config: %v", err)
	}

	loaded, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("load from env failed: %v", err)
	}
	if loaded.BitbucketToken != "stored-token" {
		t.Fatalf("expected stored token branch to populate token, got %q", loaded.BitbucketToken)
	}
	if loaded.AuthSource != "stored" {
		t.Fatalf("expected stored auth source, got %q", loaded.AuthSource)
	}
}

// TestResolveStoredCredentialsCrossScheme verifies that credentials stored
// under https://host are found when the runtime URL uses http://host and vice
// versa (issue #92).
func TestResolveStoredCredentialsCrossScheme(t *testing.T) {
	t.Run("https stored, http runtime", func(t *testing.T) {
		stored := StoredConfig{
			Hosts: map[string]StoredProfile{
				"https://bitbucket.corp": {URL: "https://bitbucket.corp", Username: "alice", AuthMode: "token"},
			},
			InsecureSecrets: map[string]StoredSecret{
				"https://bitbucket.corp": {Token: "secret-token"},
			},
		}

		resolved, ok := resolveStoredCredentials(stored, "http://bitbucket.corp")
		if !ok {
			t.Fatal("expected cross-scheme fallback to find credentials")
		}
		if resolved.BitbucketToken != "secret-token" {
			t.Fatalf("expected token secret-token, got %q", resolved.BitbucketToken)
		}
	})

	t.Run("http stored, https runtime", func(t *testing.T) {
		stored := StoredConfig{
			Hosts: map[string]StoredProfile{
				"http://bitbucket.corp": {URL: "http://bitbucket.corp", Username: "bob", AuthMode: "token"},
			},
			InsecureSecrets: map[string]StoredSecret{
				"http://bitbucket.corp": {Token: "bob-token"},
			},
		}

		resolved, ok := resolveStoredCredentials(stored, "https://bitbucket.corp")
		if !ok {
			t.Fatal("expected cross-scheme fallback to find credentials")
		}
		if resolved.BitbucketToken != "bob-token" {
			t.Fatalf("expected token bob-token, got %q", resolved.BitbucketToken)
		}
	})

	t.Run("exact scheme match takes priority", func(t *testing.T) {
		stored := StoredConfig{
			Hosts: map[string]StoredProfile{
				"https://bitbucket.corp": {URL: "https://bitbucket.corp", Username: "alice", AuthMode: "token"},
				"http://bitbucket.corp":  {URL: "http://bitbucket.corp", Username: "http-user", AuthMode: "token"},
			},
			InsecureSecrets: map[string]StoredSecret{
				"https://bitbucket.corp": {Token: "https-token"},
				"http://bitbucket.corp":  {Token: "http-token"},
			},
		}

		resolved, ok := resolveStoredCredentials(stored, "https://bitbucket.corp")
		if !ok {
			t.Fatal("expected credentials to be found")
		}
		if resolved.BitbucketToken != "https-token" {
			t.Fatalf("expected exact scheme match to win, got token %q", resolved.BitbucketToken)
		}
	})
}

func TestHostKeyAltScheme(t *testing.T) {
	if got := hostKeyAltScheme("http://bitbucket.corp"); got != "https://bitbucket.corp" {
		t.Fatalf("expected https alt, got %q", got)
	}
	if got := hostKeyAltScheme("https://bitbucket.corp:7990"); got != "http://bitbucket.corp:7990" {
		t.Fatalf("expected http alt with port, got %q", got)
	}
	if got := hostKeyAltScheme("://bad"); got != "" {
		t.Fatalf("expected empty string for invalid URL, got %q", got)
	}
}

func TestLoadStoredAuthForHost(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "bb", "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")

	// Empty config → not found.
	_, ok, err := LoadStoredAuthForHost("https://stored.bitbucket.example")
	if err != nil {
		t.Fatalf("unexpected error on empty config: %v", err)
	}
	if ok {
		t.Fatal("expected not found on empty config")
	}

	// After saving, the host should resolve.
	if _, err := SaveLogin(LoginInput{Host: "https://stored.bitbucket.example", Token: "tok", SetDefault: true}); err != nil {
		t.Fatalf("save login failed: %v", err)
	}

	cfg, ok, err := LoadStoredAuthForHost("https://stored.bitbucket.example")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected stored auth to be found")
	}
	if cfg.BitbucketToken != "tok" {
		t.Fatalf("unexpected token: %q", cfg.BitbucketToken)
	}

	// Config path that's a directory → LoadStoredConfig fails → error propagated.
	dirAsConfig := t.TempDir()
	t.Setenv("BB_CONFIG_PATH", dirAsConfig)
	_, _, err = LoadStoredAuthForHost("https://stored.bitbucket.example")
	if err == nil {
		t.Fatal("expected error when config path is a directory")
	}
}

func TestHostAliasesCRUDAndLookup(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "bb", "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")

	if _, err := SaveLogin(LoginInput{Host: "https://bitbucket.example", Token: "tok", SetDefault: true}); err != nil {
		t.Fatalf("save login failed: %v", err)
	}

	aliases, err := AddHostAliases("https://bitbucket.example", []string{"git.example.org:7999", "ssh://git.example.org:7999"})
	if err != nil {
		t.Fatalf("add aliases failed: %v", err)
	}
	if len(aliases) != 1 || aliases[0] != "git.example.org:7999" {
		t.Fatalf("unexpected aliases: %+v", aliases)
	}

	match, ok, err := MatchStoredHost("ssh://git.example.org:7999/scm/PRJ/repo.git")
	if err != nil {
		t.Fatalf("match stored host failed: %v", err)
	}
	if !ok {
		t.Fatal("expected alias match to be found")
	}
	if match.Host != "https://bitbucket.example" {
		t.Fatalf("expected canonical host, got %q", match.Host)
	}

	resolved, ok := resolveStoredCredentials(StoredConfig{
		Hosts: map[string]StoredProfile{
			"https://bitbucket.example": {URL: "https://bitbucket.example", Aliases: []string{"git.example.org:7999"}, AuthMode: "token"},
		},
		InsecureSecrets: map[string]StoredSecret{
			"https://bitbucket.example": {Token: "tok"},
		},
	}, "ssh://git.example.org:7999/scm/PRJ/repo.git")
	if !ok || resolved.BitbucketURL != "https://bitbucket.example" {
		t.Fatalf("expected alias-backed stored auth resolution, got ok=%v cfg=%+v", ok, resolved)
	}

	remaining, err := RemoveHostAlias("https://bitbucket.example", "git.example.org:7999")
	if err != nil {
		t.Fatalf("remove alias failed: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("expected no aliases after removal, got %+v", remaining)
	}
}

func TestAliasConflictValidation(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "bb", "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")

	if _, err := SaveLogin(LoginInput{Host: "https://one.example", Token: "tok1", SetDefault: true}); err != nil {
		t.Fatalf("save login one failed: %v", err)
	}
	if _, err := SaveLogin(LoginInput{Host: "https://two.example", Token: "tok2", SetDefault: false}); err != nil {
		t.Fatalf("save login two failed: %v", err)
	}
	if _, err := AddHostAliases("https://one.example", []string{"git.shared.example:22"}); err != nil {
		t.Fatalf("add alias to first host failed: %v", err)
	}
	if _, err := AddHostAliases("https://two.example", []string{"git.shared.example:22"}); err == nil {
		t.Fatal("expected alias conflict")
	}
	if _, err := SaveLogin(LoginInput{Host: "https://two.example", Token: "tok2", Aliases: []string{"git.shared.example:22"}, SetDefault: false}); err == nil {
		t.Fatal("expected alias conflict during login save")
	}
}

func TestSaveLoginNormalizesAndDeduplicatesAliases(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "bb", "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")

	result, err := SaveLogin(LoginInput{
		Host:       "https://bitbucket.example",
		Aliases:    []string{"git.example.org:7999", "ssh://git.example.org:7999", "git@git.example.org:scm/PRJ/repo.git"},
		Token:      "tok",
		SetDefault: true,
	})
	if err != nil {
		t.Fatalf("save login failed: %v", err)
	}
	if len(result.Aliases) != 2 {
		t.Fatalf("expected deduplicated aliases, got %+v", result.Aliases)
	}

	stored, err := LoadStoredConfig()
	if err != nil {
		t.Fatalf("load stored config failed: %v", err)
	}
	profile := stored.Hosts[stored.DefaultHost]
	if len(profile.Aliases) != 2 {
		t.Fatalf("expected deduplicated stored aliases, got %+v", profile.Aliases)
	}
}

func TestSetAndListHostAliases(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "bb", "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")

	if _, err := SaveLogin(LoginInput{Host: "https://bitbucket.example", Token: "tok", SetDefault: true}); err != nil {
		t.Fatalf("save login failed: %v", err)
	}

	aliases, err := SetHostAliases("https://bitbucket.example", []string{"ssh://git.example.org:7999", "git.example.org:7999"})
	if err != nil {
		t.Fatalf("set aliases failed: %v", err)
	}
	if len(aliases) != 1 || aliases[0] != "git.example.org:7999" {
		t.Fatalf("unexpected aliases: %+v", aliases)
	}

	listed, canonicalHost, err := ListHostAliases("https://bitbucket.example")
	if err != nil {
		t.Fatalf("list aliases failed: %v", err)
	}
	if canonicalHost != "https://bitbucket.example" {
		t.Fatalf("unexpected canonical host: %q", canonicalHost)
	}
	if len(listed) != 1 || listed[0] != "git.example.org:7999" {
		t.Fatalf("unexpected listed aliases: %+v", listed)
	}

	cleared, err := SetHostAliases("https://bitbucket.example", nil)
	if err != nil {
		t.Fatalf("clear aliases failed: %v", err)
	}
	if len(cleared) != 0 {
		t.Fatalf("expected cleared aliases to be empty, got %+v", cleared)
	}
}

func TestLoginAndServerContextsAlwaysExposeAliasSlices(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "bb", "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")

	result, err := SaveLogin(LoginInput{Host: "https://bitbucket.example", Token: "tok", SetDefault: true})
	if err != nil {
		t.Fatalf("save login failed: %v", err)
	}
	if result.Aliases == nil {
		t.Fatal("expected login result aliases to be a non-nil empty slice")
	}

	aliases, host, err := ListHostAliases("https://bitbucket.example")
	if err != nil {
		t.Fatalf("list aliases failed: %v", err)
	}
	if host != "https://bitbucket.example" {
		t.Fatalf("unexpected host: %q", host)
	}
	if aliases == nil {
		t.Fatal("expected listed aliases to be a non-nil empty slice")
	}

	contexts, err := ListServerContexts()
	if err != nil {
		t.Fatalf("list contexts failed: %v", err)
	}
	if len(contexts) != 1 {
		t.Fatalf("expected one context, got %d", len(contexts))
	}
	if contexts[0].Aliases == nil {
		t.Fatal("expected server context aliases to be a non-nil empty slice")
	}
	if got := normalizeStoredAliases([]string(nil)); got == nil {
		t.Fatal("expected normalized stored aliases to be non-nil for empty input")
	}
}

func TestAliasOperationsValidationAndNotFound(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "bb", "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")

	if _, err := SetHostAliases("", []string{"git.example.org:22"}); err == nil {
		t.Fatal("expected validation error for empty host")
	}
	if _, err := AddHostAliases("https://missing.example", []string{"git.example.org:22"}); err == nil {
		t.Fatal("expected not found for missing host on add")
	}
	if _, _, err := ListHostAliases("https://missing.example"); err == nil {
		t.Fatal("expected not found for missing host on list")
	}
	if _, err := RemoveHostAlias("https://missing.example", "git.example.org:22"); err == nil {
		t.Fatal("expected not found for missing host on remove")
	}
	if _, _, err := MatchStoredHost(" "); err != nil {
		t.Fatalf("expected empty runtime match to be ignored without error, got: %v", err)
	}
	if _, err := normalizeAlias("://bad"); err == nil {
		t.Fatal("expected invalid alias error")
	}

	t.Setenv("BB_CONFIG_PATH", t.TempDir())
	if _, _, err := ListHostAliases("https://broken.example"); err == nil {
		t.Fatal("expected load config error when config path is a directory")
	}
	if _, _, err := MatchStoredHost("https://broken.example"); err == nil {
		t.Fatal("expected load config error for match stored host")
	}
}

func TestAliasOperationsAdditionalBranches(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "bb", "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")

	if _, err := SaveLogin(LoginInput{Host: "https://bitbucket.example", Token: "tok", SetDefault: true}); err != nil {
		t.Fatalf("save login failed: %v", err)
	}
	if _, err := SetHostAliases("https://bitbucket.example", []string{"git.example.org:22"}); err != nil {
		t.Fatalf("set aliases failed: %v", err)
	}
	if _, err := RemoveHostAlias("https://bitbucket.example", "git.missing.org:22"); err == nil {
		t.Fatal("expected not found when removing non-existent alias")
	}

	if _, err := SetHostAliases("https://bitbucket.example", []string{"git@host-with-scp.example:scm/PRJ/repo.git"}); err != nil {
		t.Fatalf("expected scp-style alias normalization to succeed: %v", err)
	}

	stored := StoredConfig{
		Hosts: map[string]StoredProfile{
			"https://bitbucket.example": {URL: "https://bitbucket.example", Aliases: []string{"git.example.org:22"}},
		},
	}
	match, ok, err := resolveStoredHostAlias(stored, "git@git.example.org:scm/PRJ/repo.git")
	if err != nil {
		t.Fatalf("resolveStoredHostAlias returned error: %v", err)
	}
	if !ok || match.Endpoint != "git.example.org:22" {
		t.Fatalf("expected scp alias match, got ok=%v match=%+v", ok, match)
	}
	if _, ok, err := resolveStoredHostAlias(stored, "://bad"); err != nil || ok {
		t.Fatalf("expected invalid runtime url to return no match without error, got ok=%v err=%v", ok, err)
	}

	stored.Hosts["https://empty-auth.example"] = StoredProfile{URL: "https://empty-auth.example", Aliases: []string{"git.empty.example:22"}, AuthMode: "none"}
	resolved, ok := resolveStoredCredentials(stored, "git@git.empty.example:scm/PRJ/repo.git")
	if !ok || resolved.BitbucketURL != "https://empty-auth.example" {
		t.Fatalf("expected alias credential resolution to return canonical host even without secrets, got ok=%v cfg=%+v", ok, resolved)
	}

	t.Setenv("BB_CONFIG_PATH", filepath.Join(t.TempDir(), "bb", "config.yaml"))
	if _, err := SaveLogin(LoginInput{Host: "https://add-branches.example", Token: "tok", SetDefault: true}); err != nil {
		t.Fatalf("save login failed: %v", err)
	}
	aliases, err := AddHostAliases("https://add-branches.example", []string{"git.add.example:22", "git.add.example:22"})
	if err != nil {
		t.Fatalf("add aliases failed: %v", err)
	}
	if len(aliases) != 1 {
		t.Fatalf("expected duplicate aliases to collapse, got %+v", aliases)
	}
	if _, err := AddHostAliases("", []string{"git.example.org:22"}); err == nil {
		t.Fatal("expected validation error for empty host on add")
	}
	t.Setenv("BB_CONFIG_PATH", t.TempDir())
	if _, err := AddHostAliases("https://broken.example", []string{"git.example.org:22"}); err == nil {
		t.Fatal("expected load error for add aliases when config path is directory")
	}
	if _, err := RemoveHostAlias("https://broken.example", "git.example.org:22"); err == nil {
		t.Fatal("expected load error for remove alias when config path is directory")
	}
	if got := normalizeStoredAliases([]string{"://bad"}); got == nil || len(got) != 0 {
		t.Fatalf("expected invalid stored aliases to normalize to an empty slice, got %+v", got)
	}
}

// TestSaveLoginPreservesExistingAliases covers the alias loss that made
// re-authenticating destructive.
//
// Aliases are host-recognition config rather than credentials. Discovery cannot
// find every alias -- an instance whose SSH clone host differs from its web URL
// is the documented case for adding one by hand -- so a login that replaced the
// list undid the documented remedy, silently, on every run.
func TestSaveLoginPreservesExistingAliases(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "bb", "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")

	if _, err := SaveLogin(LoginInput{
		Host:    "localhost:7990",
		Token:   "token-one",
		Aliases: []string{"discovered.example:7999"},
	}); err != nil {
		t.Fatalf("first login failed: %v", err)
	}

	if _, err := AddHostAliases("localhost:7990", []string{"manual.example:7999"}); err != nil {
		t.Fatalf("adding a manual alias failed: %v", err)
	}

	// Logging in again, as happens on a token refresh.
	result, err := SaveLogin(LoginInput{
		Host:    "localhost:7990",
		Token:   "token-two",
		Aliases: []string{"discovered.example:7999"},
	})
	if err != nil {
		t.Fatalf("second login failed: %v", err)
	}

	stored, _, err := ListHostAliases("localhost:7990")
	if err != nil {
		t.Fatalf("listing aliases failed: %v", err)
	}

	if !containsAlias(stored, "manual.example:7999") {
		t.Fatalf("expected the manually added alias to survive a re-login, got: %v", stored)
	}
	if !containsAlias(stored, "discovered.example:7999") {
		t.Fatalf("expected the discovered alias to still be present, got: %v", stored)
	}
	if len(stored) != 2 {
		t.Fatalf("expected exactly the two aliases without duplication, got: %v", stored)
	}
	if !containsAlias(result.Aliases, "manual.example:7999") {
		t.Fatalf("expected the login result to report the merged list, got: %v", result.Aliases)
	}
}

func containsAlias(aliases []string, want string) bool {
	for _, alias := range aliases {
		if alias == want {
			return true
		}
	}
	return false
}

func TestMergeAliasesDeduplicatesAndPreservesOrder(t *testing.T) {
	merged := mergeAliases([]string{"a", "b"}, []string{"b", "c"})
	if len(merged) != 3 || merged[0] != "a" || merged[1] != "b" || merged[2] != "c" {
		t.Fatalf("merged = %v, want [a b c]", merged)
	}

	if merged := mergeAliases(nil, []string{"a"}); len(merged) != 1 || merged[0] != "a" {
		t.Fatalf("merged = %v, want [a]", merged)
	}
	if merged := mergeAliases([]string{"a"}, nil); len(merged) != 1 || merged[0] != "a" {
		t.Fatalf("merged = %v, want [a]", merged)
	}
}

func TestSystemConfigHierarchyAndPrecedence(t *testing.T) {
	tempDir := t.TempDir()

	sysPath := filepath.Join(tempDir, "system-config.yaml")
	userPath := filepath.Join(tempDir, "user-config.yaml")
	wsPath := filepath.Join(tempDir, "workspace-config.yaml")

	sysYAML := `
default_host: https://system.corp.internal
project_key: SYS
hosts:
  "https://system.corp.internal":
    url: https://system.corp.internal
update_base_url: https://system-mirror.corp.internal
`
	if err := os.WriteFile(sysPath, []byte(sysYAML), 0o600); err != nil {
		t.Fatalf("write system config: %v", err)
	}

	userYAML := `
default_host: https://user.corp.internal
hosts:
  "https://user.corp.internal":
    url: https://user.corp.internal
update_base_url: https://user-mirror.corp.internal
`
	if err := os.WriteFile(userPath, []byte(userYAML), 0o600); err != nil {
		t.Fatalf("write user config: %v", err)
	}

	wsYAML := `
default_host: https://workspace.corp.internal
project_key: WORKSPACE
hosts:
  "https://workspace.corp.internal":
    url: https://workspace.corp.internal
update_base_url: https://workspace-mirror.corp.internal
`
	if err := os.WriteFile(wsPath, []byte(wsYAML), 0o600); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}

	t.Setenv("BB_SYSTEM_CONFIG_PATH", sysPath)
	t.Setenv("BB_CONFIG_PATH", userPath)
	t.Setenv("BB_WORKSPACE_CONFIG_PATH", wsPath)
	t.Setenv("BITBUCKET_URL", "")
	t.Setenv("BITBUCKET_PROJECT_KEY", "")
	t.Setenv("BB_DISABLE_STORED_CONFIG", "0")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}

	// Workspace takes highest precedence
	if cfg.BitbucketURL != "https://workspace.corp.internal" {
		t.Fatalf("expected workspace URL, got: %s", cfg.BitbucketURL)
	}
	if cfg.ProjectKey != "WORKSPACE" {
		t.Fatalf("expected workspace project key, got: %s", cfg.ProjectKey)
	}

	// Without workspace config, user takes precedence over system
	t.Setenv("BB_WORKSPACE_CONFIG_PATH", filepath.Join(tempDir, "nonexistent.yaml"))
	cfgUser, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}
	if cfgUser.BitbucketURL != "https://user.corp.internal" {
		t.Fatalf("expected user URL, got: %s", cfgUser.BitbucketURL)
	}
	if cfgUser.ProjectKey != defaultProjectKey {
		t.Fatalf("expected default project key, got: %s", cfgUser.ProjectKey)
	}

	// Without user default host, system takes precedence
	emptyUserYAML := `hosts: {}`
	if err := os.WriteFile(userPath, []byte(emptyUserYAML), 0o600); err != nil {
		t.Fatalf("write empty user config: %v", err)
	}
	cfgSys, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}
	if cfgSys.BitbucketURL != "https://system.corp.internal" {
		t.Fatalf("expected system URL, got: %s", cfgSys.BitbucketURL)
	}
}

func TestSystemPolicyAllowedHosts(t *testing.T) {
	tempDir := t.TempDir()
	sysPath := filepath.Join(tempDir, "system-config.yaml")

	sysYAML := `
allowed_hosts:
  - https://allowed.internal
  - bitbucket.corp.internal
`
	if err := os.WriteFile(sysPath, []byte(sysYAML), 0o600); err != nil {
		t.Fatalf("write system config: %v", err)
	}

	t.Setenv("BB_SYSTEM_CONFIG_PATH", sysPath)
	t.Setenv("BB_CONFIG_PATH", filepath.Join(tempDir, "user.yaml"))
	t.Setenv("BB_WORKSPACE_CONFIG_PATH", filepath.Join(tempDir, "nonexistent.yaml"))

	// Unauthorized host in BITBUCKET_URL should be rejected with KindAuthorization
	t.Setenv("BITBUCKET_URL", "https://unauthorized.internal")
	_, err := LoadFromEnv()
	if err == nil {
		t.Fatal("expected error for unauthorized host, got nil")
	}
	if !apperrors.IsKind(err, apperrors.KindAuthorization) {
		t.Fatalf("expected KindAuthorization, got: %v", err)
	}

	// Allowed host should succeed
	t.Setenv("BITBUCKET_URL", "https://allowed.internal")
	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("expected allowed host to succeed, got: %v", err)
	}
	if cfg.BitbucketURL != "https://allowed.internal" {
		t.Fatalf("expected allowed host, got: %s", cfg.BitbucketURL)
	}

	// Hostname without scheme match
	t.Setenv("BITBUCKET_URL", "https://bitbucket.corp.internal")
	cfg2, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("expected hostname match to succeed, got: %v", err)
	}
	if cfg2.BitbucketURL != "https://bitbucket.corp.internal" {
		t.Fatalf("expected allowed host, got: %s", cfg2.BitbucketURL)
	}

	// SaveLogin with unauthorized host rejected
	_, err = SaveLogin(LoginInput{
		Host:  "https://evil.internal",
		Token: "secret",
	})
	if err == nil {
		t.Fatal("expected SaveLogin to reject unauthorized host, got nil")
	}
	if !apperrors.IsKind(err, apperrors.KindAuthorization) {
		t.Fatalf("expected KindAuthorization, got: %v", err)
	}
}

func TestSystemPolicyAllowInsecureSkipVerifyFalse(t *testing.T) {
	tempDir := t.TempDir()
	sysPath := filepath.Join(tempDir, "system-config.yaml")

	sysYAML := `
allow_insecure_skip_verify: false
`
	if err := os.WriteFile(sysPath, []byte(sysYAML), 0o600); err != nil {
		t.Fatalf("write system config: %v", err)
	}

	t.Setenv("BB_SYSTEM_CONFIG_PATH", sysPath)
	t.Setenv("BB_CONFIG_PATH", filepath.Join(tempDir, "user.yaml"))
	t.Setenv("BITBUCKET_URL", "https://bb.example.local")

	// BB_INSECURE_SKIP_VERIFY=true must be rejected
	t.Setenv("BB_INSECURE_SKIP_VERIFY", "true")
	_, err := LoadFromEnv()
	if err == nil {
		t.Fatal("expected error when allow_insecure_skip_verify=false and BB_INSECURE_SKIP_VERIFY=true, got nil")
	}
	if !apperrors.IsKind(err, apperrors.KindAuthorization) {
		t.Fatalf("expected KindAuthorization, got: %v", err)
	}

	// BB_INSECURE_SKIP_VERIFY=false should succeed
	t.Setenv("BB_INSECURE_SKIP_VERIFY", "false")
	_, err = LoadFromEnv()
	if err != nil {
		t.Fatalf("expected success when BB_INSECURE_SKIP_VERIFY=false, got: %v", err)
	}
}

func TestSystemPolicyMandatedCAFile(t *testing.T) {
	tempDir := t.TempDir()
	sysPath := filepath.Join(tempDir, "system-config.yaml")
	mandatedCA := filepath.Join(tempDir, "corp-ca.pem")
	conflictingCA := filepath.Join(tempDir, "different-ca.pem")

	if err := os.WriteFile(mandatedCA, []byte("dummy-cert-data"), 0o600); err != nil {
		t.Fatalf("write mandated CA: %v", err)
	}
	if err := os.WriteFile(conflictingCA, []byte("different-cert-data"), 0o600); err != nil {
		t.Fatalf("write conflicting CA: %v", err)
	}

	sysYAML := fmt.Sprintf("ca_file: %s\n", mandatedCA)
	if err := os.WriteFile(sysPath, []byte(sysYAML), 0o600); err != nil {
		t.Fatalf("write system config: %v", err)
	}

	t.Setenv("BB_SYSTEM_CONFIG_PATH", sysPath)
	t.Setenv("BB_CONFIG_PATH", filepath.Join(tempDir, "user.yaml"))
	t.Setenv("BITBUCKET_URL", "https://bb.example.local")

	// Empty BB_CA_FILE adopts mandated CA file
	t.Setenv("BB_CA_FILE", "")
	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if cfg.CAFile != mandatedCA {
		t.Fatalf("expected mandated CA %q, got: %q", mandatedCA, cfg.CAFile)
	}

	// Identical BB_CA_FILE succeeds
	t.Setenv("BB_CA_FILE", mandatedCA)
	cfg2, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("expected success with matching CA, got: %v", err)
	}
	if cfg2.CAFile != mandatedCA {
		t.Fatalf("expected mandated CA %q, got: %q", mandatedCA, cfg2.CAFile)
	}

	// Conflicting BB_CA_FILE is rejected
	t.Setenv("BB_CA_FILE", conflictingCA)
	_, err = LoadFromEnv()
	if err == nil {
		t.Fatal("expected error when overriding mandated CA, got nil")
	}
	if !apperrors.IsKind(err, apperrors.KindAuthorization) {
		t.Fatalf("expected KindAuthorization, got: %v", err)
	}
}

func TestSystemPolicyRequireKeyringCannotBeBypassed(t *testing.T) {
	tempDir := t.TempDir()
	sysPath := filepath.Join(tempDir, "system-config.yaml")

	sysYAML := `
require_keyring: true
`
	if err := os.WriteFile(sysPath, []byte(sysYAML), 0o600); err != nil {
		t.Fatalf("write system config: %v", err)
	}

	t.Setenv("BB_SYSTEM_CONFIG_PATH", sysPath)
	t.Setenv("BB_CONFIG_PATH", filepath.Join(tempDir, "user.yaml"))

	// Even with BB_REQUIRE_KEYRING=0, RequireKeyring() must report true
	t.Setenv("BB_REQUIRE_KEYRING", "0")
	req, err := RequireKeyring()
	if err != nil {
		t.Fatalf("RequireKeyring error: %v", err)
	}
	if !req {
		t.Fatal("expected RequireKeyring to remain true due to system policy")
	}

	// SaveLogin fails when keyring is unavailable and refuses plaintext fallback
	originalKeyringSet := keyringSet
	keyringSet = func(string, string, string) error {
		return fmt.Errorf("mock keyring offline")
	}
	defer func() {
		keyringSet = originalKeyringSet
	}()

	_, err = SaveLogin(LoginInput{
		Host:  "https://bb.example.local",
		Token: "test-token",
	})
	if err == nil {
		t.Fatal("expected SaveLogin to fail when keyring is offline under policy, got nil")
	}
	if !apperrors.IsKind(err, apperrors.KindPermanent) {
		t.Fatalf("expected KindPermanent, got: %v", err)
	}
}

func TestResolveUpdateBaseURLHierarchy(t *testing.T) {
	tempDir := t.TempDir()
	sysPath := filepath.Join(tempDir, "system-config.yaml")
	userPath := filepath.Join(tempDir, "user-config.yaml")
	wsPath := filepath.Join(tempDir, "workspace-config.yaml")

	if err := os.WriteFile(sysPath, []byte("update_base_url: https://sys-mirror.corp\n"), 0o600); err != nil {
		t.Fatalf("write sys: %v", err)
	}
	if err := os.WriteFile(userPath, []byte("update_base_url: https://user-mirror.corp\n"), 0o600); err != nil {
		t.Fatalf("write user: %v", err)
	}
	if err := os.WriteFile(wsPath, []byte("update_base_url: https://ws-mirror.corp\n"), 0o600); err != nil {
		t.Fatalf("write ws: %v", err)
	}

	t.Setenv("BB_SYSTEM_CONFIG_PATH", sysPath)
	t.Setenv("BB_CONFIG_PATH", userPath)
	t.Setenv("BB_WORKSPACE_CONFIG_PATH", wsPath)
	t.Setenv("BB_UPDATE_BASE_URL", "")

	// 1. CLI flag beats everything
	url, err := ResolveUpdateBaseURL("https://flag-mirror.corp")
	if err != nil || url != "https://flag-mirror.corp" {
		t.Fatalf("expected flag URL, got %s, err: %v", url, err)
	}

	// 2. Env beats workspace/user/sys
	t.Setenv("BB_UPDATE_BASE_URL", "https://env-mirror.corp")
	url, err = ResolveUpdateBaseURL("")
	if err != nil || url != "https://env-mirror.corp" {
		t.Fatalf("expected env URL, got %s, err: %v", url, err)
	}

	// 3. Workspace beats user/sys
	t.Setenv("BB_UPDATE_BASE_URL", "")
	url, err = ResolveUpdateBaseURL("")
	if err != nil || url != "https://ws-mirror.corp" {
		t.Fatalf("expected ws URL, got %s, err: %v", url, err)
	}

	// 4. User beats sys
	t.Setenv("BB_WORKSPACE_CONFIG_PATH", filepath.Join(tempDir, "nonexistent.yaml"))
	url, err = ResolveUpdateBaseURL("")
	if err != nil || url != "https://user-mirror.corp" {
		t.Fatalf("expected user URL, got %s, err: %v", url, err)
	}

	// 5. Sys fallback
	if err := os.WriteFile(userPath, []byte("hosts: {}\n"), 0o600); err != nil {
		t.Fatalf("write user: %v", err)
	}
	url, err = ResolveUpdateBaseURL("")
	if err != nil || url != "https://sys-mirror.corp" {
		t.Fatalf("expected sys URL, got %s, err: %v", url, err)
	}

	// 6. Default github.com
	if err := os.WriteFile(sysPath, []byte("hosts: {}\n"), 0o600); err != nil {
		t.Fatalf("write sys: %v", err)
	}
	url, err = ResolveUpdateBaseURL("")
	if err != nil || url != "https://api.github.com" {
		t.Fatalf("expected default URL, got %s, err: %v", url, err)
	}
}

func TestIsUpdateDisabled(t *testing.T) {
	tempDir := t.TempDir()
	sysPath := filepath.Join(tempDir, "system-config.yaml")

	t.Setenv("BB_SYSTEM_CONFIG_PATH", sysPath)
	t.Setenv("BB_CONFIG_PATH", filepath.Join(tempDir, "user.yaml"))
	t.Setenv("BB_DISABLE_UPDATE", "")

	// Default: not disabled
	disabled, _, err := IsUpdateDisabled()
	if err != nil || disabled {
		t.Fatalf("expected not disabled, got disabled=%v, err=%v", disabled, err)
	}

	// Via BB_DISABLE_UPDATE=1
	t.Setenv("BB_DISABLE_UPDATE", "1")
	disabled, msg, err := IsUpdateDisabled()
	if err != nil || !disabled {
		t.Fatalf("expected disabled via env, got disabled=%v, err=%v", disabled, err)
	}
	// The message has to name the lever, not just report that one fired. The
	// policy file and BB_DISABLE_UPDATE live in unrelated places, and an
	// operator re-enabling self-update needs to know which one to go and
	// change -- the environment variable being much the harder to track down.
	if !strings.Contains(msg, "BB_DISABLE_UPDATE") {
		t.Fatalf("message does not name the environment variable: %s", msg)
	}

	// Via system policy
	t.Setenv("BB_DISABLE_UPDATE", "")
	if err := os.WriteFile(sysPath, []byte("disable_update: true\n"), 0o600); err != nil {
		t.Fatalf("write sys: %v", err)
	}
	disabled, msg, err = IsUpdateDisabled()
	if err != nil || !disabled {
		t.Fatalf("expected disabled via policy, got disabled=%v, err=%v", disabled, err)
	}
	if !strings.Contains(msg, "disable_update") {
		t.Fatalf("message does not name the policy setting: %s", msg)
	}
	if !strings.Contains(msg, sysPath) {
		t.Fatalf("message does not name the policy file %s: %s", sysPath, msg)
	}
	if strings.Contains(msg, "BB_DISABLE_UPDATE") {
		t.Fatalf("policy message wrongly blames the environment variable: %s", msg)
	}
}

func TestIsHostAllowed(t *testing.T) {
	allowed := []string{
		"https://bb.corp.internal",
		"staging.internal:8443",
		"exact-host.internal",
	}

	cases := []struct {
		url   string
		valid bool
	}{
		{"https://bb.corp.internal", true},
		{"https://bb.corp.internal/", true},
		{"https://bb.corp.internal:443", true},
		{"http://bb.corp.internal", true},
		{"https://staging.internal:8443", true},
		{"https://exact-host.internal", true},
		{"https://EXACT-HOST.internal/scm/PRJ/repo.git", true},
		{"https://other.corp.internal", false},
		{"https://evil.example.com", false},
	}

	for _, tc := range cases {
		if got := IsHostAllowed(tc.url, allowed); got != tc.valid {
			t.Errorf("IsHostAllowed(%q) = %v, want %v", tc.url, got, tc.valid)
		}
	}
}

func TestSystemPolicyRequireKeyringWarningWhenDisabledInEnv(t *testing.T) {
	tempDir := t.TempDir()
	sysConfig := filepath.Join(tempDir, "system.yaml")
	t.Setenv("BB_SYSTEM_CONFIG_PATH", sysConfig)

	// Mandate keyring in system policy
	if err := os.WriteFile(sysConfig, []byte("require_keyring: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Capture warnings
	var warnBuf bytes.Buffer
	origWarningWriter := policyWarningWriter
	policyWarningWriter = &warnBuf
	t.Cleanup(func() { policyWarningWriter = origWarningWriter })

	// User attempts to disable keyring requirement via env
	t.Setenv("BB_REQUIRE_KEYRING", "0")

	req, err := RequireKeyring()
	if err != nil {
		t.Fatalf("RequireKeyring error: %v", err)
	}
	if !req {
		t.Fatal("expected RequireKeyring to be true under policy")
	}

	output := warnBuf.String()
	if !strings.Contains(output, "warning: BB_REQUIRE_KEYRING=0 is ignored; keyring-backed storage is mandated by administrative policy") {
		t.Fatalf("expected warning about ignored BB_REQUIRE_KEYRING, got: %q", output)
	}
}

func TestConfigJSONSchemaAndValidation(t *testing.T) {
	schema := ConfigJSONSchema()
	if schema["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
		t.Fatalf("unexpected schema version: %v", schema["$schema"])
	}

	// Valid config with all supported fields
	validYAML := `
$schema: https://raw.githubusercontent.com/vriesdemichael/bitbucket-data-center-cli/main/docs/reference/schemas/config.schema.json
default_host: https://bitbucket.corp.internal
project_key: PROJ
require_keyring: true
ca_file: /etc/ssl/certs/corp.pem
allowed_hosts:
  - https://bitbucket.corp.internal
allow_insecure_skip_verify: false
disable_update: true
update_base_url: https://artifactory.corp.internal/artifactory/bb
hosts:
  https://bitbucket.corp.internal:
    url: https://bitbucket.corp.internal
    auth_mode: token
    aliases:
      - origin
insecure_secrets:
  https://bitbucket.corp.internal:
    token: secret-token
`
	if err := ValidateConfigYAML([]byte(validYAML)); err != nil {
		t.Fatalf("expected valid YAML to pass schema validation, got: %v", err)
	}

	// Valid empty YAML
	if err := ValidateConfigYAML([]byte("")); err != nil {
		t.Fatalf("empty YAML should pass, got: %v", err)
	}

	// Valid YAML with policy block
	policyBlockYAML := `
policy:
  require_keyring: true
  allowed_hosts:
    - https://bb.example.com
`
	if err := ValidateConfigYAML([]byte(policyBlockYAML)); err != nil {
		t.Fatalf("policy block YAML should pass, got: %v", err)
	}

	// Invalid YAML: unknown property
	invalidUnknownProperty := `
unknown_field: true
`
	if err := ValidateConfigYAML([]byte(invalidUnknownProperty)); err == nil {
		t.Fatal("expected error on unknown property, got nil")
	}

	// Invalid YAML: wrong type for require_keyring (string instead of boolean)
	invalidType := `
require_keyring: "yes"
`
	if err := ValidateConfigYAML([]byte(invalidType)); err == nil {
		t.Fatal("expected error on wrong property type, got nil")
	}

	// Invalid YAML: host entry missing required "url" field
	invalidHost := `
hosts:
  local:
    auth_mode: token
`
	if err := ValidateConfigYAML([]byte(invalidHost)); err == nil {
		t.Fatal("expected error on host missing url, got nil")
	}

	// Valid host mTLS configuration
	validHostMTLS := `
hosts:
  https://bb.example.com:
    url: https://bb.example.com
    client_cert: /etc/ssl/client.crt
    client_key: /etc/ssl/client.key
`
	if err := ValidateConfigYAML([]byte(validHostMTLS)); err != nil {
		t.Fatalf("valid host mTLS YAML should pass, got: %v", err)
	}

	// Invalid YAML: root-level client_cert (not supported at root level)
	invalidRootMTLS := `
default_host: https://bb.local
client_cert: /tmp/c.crt
`
	if err := ValidateConfigYAML([]byte(invalidRootMTLS)); err == nil {
		t.Fatal("expected error on root-level client_cert, got nil")
	}

	// Invalid YAML: policy-level client_cert (not supported in policy block)
	invalidPolicyMTLS := `
policy:
  client_cert: /tmp/p.crt
`
	if err := ValidateConfigYAML([]byte(invalidPolicyMTLS)); err == nil {
		t.Fatal("expected error on policy-level client_cert, got nil")
	}
}

func TestLoadSystemConfigRejectsSchemaViolations(t *testing.T) {
	tempDir := t.TempDir()
	sysPath := filepath.Join(tempDir, "config.yaml")
	t.Setenv("BB_SYSTEM_CONFIG_PATH", sysPath)

	if err := os.WriteFile(sysPath, []byte("illegal_property: 123\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadSystemConfig()
	if err == nil {
		t.Fatal("expected LoadSystemConfig to fail with schema violation error")
	}
	if !strings.Contains(err.Error(), "configuration does not match schema") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestLoadFromEnvPolicyError(t *testing.T) {
	tempDir := t.TempDir()
	sysPath := filepath.Join(tempDir, "invalid.yaml")
	if err := os.WriteFile(sysPath, []byte("policy:\n  require_keyring: [broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BB_SYSTEM_CONFIG_PATH", sysPath)

	_, err := LoadFromEnv()
	if err == nil {
		t.Fatal("expected LoadFromEnv to fail when policy loading fails")
	}
}

func TestLoadFromEnvSystemCAFile(t *testing.T) {
	tempDir := t.TempDir()
	sysPath := filepath.Join(tempDir, "sys.yaml")
	caPath := filepath.Join(tempDir, "sys-ca.crt")
	if err := os.WriteFile(caPath, []byte("-----BEGIN CERTIFICATE-----\nfake\n-----END CERTIFICATE-----\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sysPath, []byte(fmt.Sprintf("ca_file: %q\n", filepath.ToSlash(caPath))), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BB_SYSTEM_CONFIG_PATH", sysPath)
	t.Setenv("BB_CA_FILE", "")
	t.Setenv("BITBUCKET_URL", "https://bb.example.local")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}
	if filepath.Clean(cfg.CAFile) != filepath.Clean(caPath) {
		t.Fatalf("expected ca_file from system config %s, got %s", caPath, cfg.CAFile)
	}
}

func TestLoadFromEnvWorkspaceCredentials(t *testing.T) {
	tempDir := t.TempDir()
	wsPath := filepath.Join(tempDir, "workspace.yaml")
	wsYAML := `
hosts:
  https://bb.example.local:
    url: https://bb.example.local
    username: ws-user
`
	if err := os.WriteFile(wsPath, []byte(wsYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BB_WORKSPACE_CONFIG_PATH", wsPath)
	t.Setenv("BB_CONFIG_PATH", filepath.Join(tempDir, "user-empty.yaml"))
	t.Setenv("BITBUCKET_URL", "https://bb.example.local")
	t.Setenv("BITBUCKET_TOKEN", "test-token")
	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("BITBUCKET_USER", "")
	t.Setenv("ADMIN_USER", "")
	t.Setenv("BITBUCKET_PASSWORD", "")
	t.Setenv("ADMIN_PASSWORD", "")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}
	if cfg.BitbucketUsername != "ws-user" {
		t.Fatalf("expected ws-user username from workspace, got %s", cfg.BitbucketUsername)
	}
}

func TestSaveLoginPolicyAllowedHostsRejection(t *testing.T) {
	tempDir := t.TempDir()
	sysPath := filepath.Join(tempDir, "sys.yaml")
	if err := os.WriteFile(sysPath, []byte("allowed_hosts:\n  - https://allowed.internal\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BB_SYSTEM_CONFIG_PATH", sysPath)
	t.Setenv("BB_CONFIG_PATH", filepath.Join(tempDir, "user.yaml"))

	_, err := SaveLogin(LoginInput{
		Host:     "https://disallowed.internal",
		Username: "admin",
		Token:    "secret",
	})
	if err == nil {
		t.Fatal("expected SaveLogin to reject disallowed host")
	}
	if !apperrors.IsKind(err, apperrors.KindAuthorization) {
		t.Fatalf("expected KindAuthorization, got %v", err)
	}
}

func TestSaveLoginPolicyError(t *testing.T) {
	tempDir := t.TempDir()
	sysPath := filepath.Join(tempDir, "invalid.yaml")
	if err := os.WriteFile(sysPath, []byte("policies: [invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BB_SYSTEM_CONFIG_PATH", sysPath)

	_, err := SaveLogin(LoginInput{
		Host:     "https://bb.internal",
		Username: "admin",
		Token:    "secret",
	})
	if err == nil {
		t.Fatal("expected SaveLogin to fail on policy error")
	}
}

func TestSystemConfigPathDefault(t *testing.T) {
	t.Setenv("BB_SYSTEM_CONFIG_PATH", "")
	p, err := SystemConfigPath()
	if err != nil {
		t.Fatalf("SystemConfigPath error: %v", err)
	}
	if p == "" {
		t.Fatal("expected non-empty system config path")
	}
}

func TestWorkspaceConfigPathDiscovery(t *testing.T) {
	tempDir := t.TempDir()
	bbDir := filepath.Join(tempDir, ".bb")
	if err := os.MkdirAll(bbDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgFile := filepath.Join(bbDir, "config.yaml")
	if err := os.WriteFile(cfgFile, []byte("default_host: https://bb.local\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("BB_WORKSPACE_CONFIG_PATH", cfgFile)
	found, err := WorkspaceConfigPath()
	if err != nil || found != cfgFile {
		t.Fatalf("expected %s, got %s, err: %v", cfgFile, found, err)
	}
}

func TestMergePolicyAllFields(t *testing.T) {
	tr := true
	fl := false
	p1 := PolicyConfig{}
	p2 := PolicyConfig{
		RequireKeyring:          &tr,
		CAFile:                  "/ca.crt",
		AllowedHosts:            []string{"https://bb.local"},
		AllowInsecureSkipVerify: &fl,
		DisableUpdate:           &tr,
		UpdateBaseURL:           "https://mirror.local",
	}

	mergePolicy(&p1, p2)
	if p1.RequireKeyring == nil || !*p1.RequireKeyring {
		t.Errorf("RequireKeyring not merged")
	}
	if p1.CAFile != "/ca.crt" {
		t.Errorf("CAFile not merged")
	}
	if len(p1.AllowedHosts) != 1 || p1.AllowedHosts[0] != "https://bb.local" {
		t.Errorf("AllowedHosts not merged")
	}
	if p1.AllowInsecureSkipVerify == nil || *p1.AllowInsecureSkipVerify {
		t.Errorf("AllowInsecureSkipVerify not merged")
	}
	if p1.DisableUpdate == nil || !*p1.DisableUpdate {
		t.Errorf("DisableUpdate not merged")
	}
	if p1.UpdateBaseURL != "https://mirror.local" {
		t.Errorf("UpdateBaseURL not merged")
	}
}

func TestIsHostAllowedEdgeCases(t *testing.T) {
	if !IsHostAllowed("https://bb.local", nil) {
		t.Error("nil allowed hosts should allow all")
	}
	if !IsHostAllowed("https://bb.local", []string{}) {
		t.Error("empty allowed hosts should allow all")
	}
	if IsHostAllowed("https://bb.local", []string{"", "   ", "https://other.local"}) {
		t.Error("whitespace and other host should not match bb.local")
	}
	if !IsHostAllowed("https://bb.local", []string{"bb.local"}) {
		t.Error("hostname without scheme should match")
	}
}

func TestResolveUpdateBaseURLPoliciesVariants(t *testing.T) {
	tempDir := t.TempDir()
	sysPath := filepath.Join(tempDir, "sys.yaml")

	t.Setenv("BB_SYSTEM_CONFIG_PATH", sysPath)
	t.Setenv("BB_WORKSPACE_CONFIG_PATH", filepath.Join(tempDir, "ws.yaml"))
	t.Setenv("BB_CONFIG_PATH", filepath.Join(tempDir, "user.yaml"))
	t.Setenv("BB_UPDATE_BASE_URL", "")

	// 1. In policies block
	if err := os.WriteFile(sysPath, []byte("policies:\n  update_base_url: https://policies-mirror.local\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	url, err := ResolveUpdateBaseURL("")
	if err != nil || url != "https://policies-mirror.local" {
		t.Fatalf("expected policies-mirror, got %s, err: %v", url, err)
	}

	// 2. In policy singular block
	if err := os.WriteFile(sysPath, []byte("policy:\n  update_base_url: https://policy-singular-mirror.local\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	url, err = ResolveUpdateBaseURL("")
	if err != nil || url != "https://policy-singular-mirror.local" {
		t.Fatalf("expected policy-singular-mirror, got %s, err: %v", url, err)
	}

	// 3. Error on invalid URL
	_, err = ResolveUpdateBaseURL(":\x7finvalid")
	if err == nil {
		t.Fatal("expected error on invalid URL")
	}
}

func TestValidateConfigYAMLCommentsOnly(t *testing.T) {
	err := ValidateConfigYAML([]byte("# just a comment\n# another comment\n"))
	if err != nil {
		t.Fatalf("comments-only YAML should be valid, got: %v", err)
	}
}

func TestResolveUpdateTrustFromSystemPolicy(t *testing.T) {
	t.Run("defaults to the public trust root", func(t *testing.T) {
		tempDir := t.TempDir()
		t.Setenv("BB_SYSTEM_CONFIG_PATH", filepath.Join(tempDir, "system-config.yaml"))
		t.Setenv("BB_CONFIG_PATH", filepath.Join(tempDir, "user.yaml"))

		trust, err := ResolveUpdateTrust()
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}
		if trust.Configured() {
			t.Fatalf("expected an unconfigured trust policy, got: %+v", trust)
		}
	})

	t.Run("reads every setting from the policies block", func(t *testing.T) {
		tempDir := t.TempDir()
		sysPath := filepath.Join(tempDir, "system-config.yaml")
		trustedRoot := filepath.Join(tempDir, "trusted_root.json")
		if err := os.WriteFile(trustedRoot, []byte("{}"), 0o600); err != nil {
			t.Fatalf("write trusted root: %v", err)
		}

		sysYAML := fmt.Sprintf(`policies:
  update_trusted_root: %s
  update_signature_identity: https://github.com/corp/bb/.github/workflows/mirror.yml@refs/heads/main
  update_signature_issuer: https://fulcio.corp.internal
`, trustedRoot)
		if err := os.WriteFile(sysPath, []byte(sysYAML), 0o600); err != nil {
			t.Fatalf("write system config: %v", err)
		}

		t.Setenv("BB_SYSTEM_CONFIG_PATH", sysPath)
		t.Setenv("BB_CONFIG_PATH", filepath.Join(tempDir, "user.yaml"))

		trust, err := ResolveUpdateTrust()
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}
		if trust.TrustedRootPath != trustedRoot {
			t.Fatalf("expected trusted root %q, got %q", trustedRoot, trust.TrustedRootPath)
		}
		if trust.SignatureIssuer != "https://fulcio.corp.internal" {
			t.Fatalf("unexpected issuer: %q", trust.SignatureIssuer)
		}
		if !strings.HasPrefix(trust.SignatureIdentity, "https://github.com/corp/bb/") {
			t.Fatalf("unexpected identity: %q", trust.SignatureIdentity)
		}
		if trust.AllowUnverified {
			t.Fatal("expected verification to remain enabled")
		}
	})

	t.Run("environment variables cannot set update trust", func(t *testing.T) {
		tempDir := t.TempDir()
		trustedRoot := filepath.Join(tempDir, "attacker_root.json")
		if err := os.WriteFile(trustedRoot, []byte("{}"), 0o600); err != nil {
			t.Fatalf("write trusted root: %v", err)
		}

		t.Setenv("BB_SYSTEM_CONFIG_PATH", filepath.Join(tempDir, "system-config.yaml"))
		t.Setenv("BB_CONFIG_PATH", filepath.Join(tempDir, "user.yaml"))
		t.Setenv("BB_UPDATE_TRUSTED_ROOT", trustedRoot)
		t.Setenv("BB_UPDATE_SIGNATURE_IDENTITY", "https://attacker.example/workflow.yml@refs/heads/main")
		t.Setenv("BB_ALLOW_UNVERIFIED_UPDATE", "1")

		trust, err := ResolveUpdateTrust()
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}
		if trust.Configured() {
			t.Fatalf("environment must not influence update trust, got: %+v", trust)
		}
	})

	t.Run("allow_unverified_update is honoured from policy", func(t *testing.T) {
		tempDir := t.TempDir()
		sysPath := filepath.Join(tempDir, "system-config.yaml")
		if err := os.WriteFile(sysPath, []byte("allow_unverified_update: true\n"), 0o600); err != nil {
			t.Fatalf("write system config: %v", err)
		}

		t.Setenv("BB_SYSTEM_CONFIG_PATH", sysPath)
		t.Setenv("BB_CONFIG_PATH", filepath.Join(tempDir, "user.yaml"))

		trust, err := ResolveUpdateTrust()
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}
		if !trust.AllowUnverified {
			t.Fatal("expected allow_unverified_update to be honoured")
		}
	})

	t.Run("rejects a missing trusted root", func(t *testing.T) {
		tempDir := t.TempDir()
		sysPath := filepath.Join(tempDir, "system-config.yaml")
		missing := filepath.Join(tempDir, "absent.json")
		if err := os.WriteFile(sysPath, []byte(fmt.Sprintf("update_trusted_root: %s\n", missing)), 0o600); err != nil {
			t.Fatalf("write system config: %v", err)
		}

		t.Setenv("BB_SYSTEM_CONFIG_PATH", sysPath)
		t.Setenv("BB_CONFIG_PATH", filepath.Join(tempDir, "user.yaml"))

		_, err := ResolveUpdateTrust()
		if !apperrors.IsKind(err, apperrors.KindValidation) {
			t.Fatalf("expected KindValidation, got: %v", err)
		}
	})

	t.Run("rejects both trust sources at once", func(t *testing.T) {
		tempDir := t.TempDir()
		sysPath := filepath.Join(tempDir, "system-config.yaml")
		trustedRoot := filepath.Join(tempDir, "trusted_root.json")
		if err := os.WriteFile(trustedRoot, []byte("{}"), 0o600); err != nil {
			t.Fatalf("write trusted root: %v", err)
		}
		sysYAML := fmt.Sprintf("update_trusted_root: %s\nupdate_tuf_url: https://artifactory.corp.internal/tuf\n", trustedRoot)
		if err := os.WriteFile(sysPath, []byte(sysYAML), 0o600); err != nil {
			t.Fatalf("write system config: %v", err)
		}

		t.Setenv("BB_SYSTEM_CONFIG_PATH", sysPath)
		t.Setenv("BB_CONFIG_PATH", filepath.Join(tempDir, "user.yaml"))

		_, err := ResolveUpdateTrust()
		if !apperrors.IsKind(err, apperrors.KindValidation) {
			t.Fatalf("expected KindValidation, got: %v", err)
		}
	})

	// url.Parse accepted every one of these, which is why it was never a
	// check. This setting names where the Sigstore trust material for a binary
	// bb is about to execute comes from.
	t.Run("rejects a tuf url that is not an absolute https URL", func(t *testing.T) {
		for _, value := range []string{
			"not a url",
			"artifactory.corp.internal/tuf",
			"http://artifactory.corp.internal/tuf",
			"file:///etc/passwd",
			"/var/lib/tuf",
		} {
			tempDir := t.TempDir()
			sysPath := filepath.Join(tempDir, "system-config.yaml")
			if err := os.WriteFile(sysPath, []byte(fmt.Sprintf("update_tuf_url: %q\n", value)), 0o600); err != nil {
				t.Fatalf("write system config: %v", err)
			}

			t.Setenv("BB_SYSTEM_CONFIG_PATH", sysPath)
			t.Setenv("BB_CONFIG_PATH", filepath.Join(tempDir, "user.yaml"))

			if _, err := ResolveUpdateTrust(); !apperrors.IsKind(err, apperrors.KindValidation) {
				t.Fatalf("expected %q to be rejected, got: %v", value, err)
			}
		}
	})

	t.Run("accepts an absolute https tuf url", func(t *testing.T) {
		tempDir := t.TempDir()
		sysPath := filepath.Join(tempDir, "system-config.yaml")
		if err := os.WriteFile(sysPath, []byte("update_tuf_url: https://artifactory.corp.internal/tuf\n"), 0o600); err != nil {
			t.Fatalf("write system config: %v", err)
		}

		t.Setenv("BB_SYSTEM_CONFIG_PATH", sysPath)
		t.Setenv("BB_CONFIG_PATH", filepath.Join(tempDir, "user.yaml"))

		trust, err := ResolveUpdateTrust()
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}
		if trust.TUFRepositoryURL != "https://artifactory.corp.internal/tuf" {
			t.Fatalf("unexpected tuf url: %q", trust.TUFRepositoryURL)
		}
	})
}

func TestSystemConfigPathIgnoresEnvironmentOutsideTests(t *testing.T) {
	attackerPath := filepath.Join(t.TempDir(), "attacker-policy.yaml")

	// Under `go test`, the override is honoured: every policy test in this
	// package depends on it.
	testPath, err := systemConfigPath(attackerPath, true)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if testPath != attackerPath {
		t.Fatalf("expected the override to apply under test, got %q", testPath)
	}

	// In a released binary it is ignored, so administrative policy cannot be
	// swapped out by anyone able to set a variable in the user's shell.
	releasePath, err := systemConfigPath(attackerPath, false)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if releasePath == attackerPath {
		t.Fatal("BB_SYSTEM_CONFIG_PATH must not redirect administrative policy outside tests")
	}

	expected := machineConfigPath()
	if releasePath != expected {
		t.Fatalf("expected the machine path %q, got %q", expected, releasePath)
	}

	// An empty override leaves the machine path in place either way.
	if emptyPath, err := systemConfigPath("", true); err != nil || emptyPath != expected {
		t.Fatalf("expected the machine path for an empty override, got %q (%v)", emptyPath, err)
	}
}

func TestMachineConfigPathIsNotEnvironmentDerived(t *testing.T) {
	before := machineConfigPath()

	// ProgramData is the Windows half of the same question: it names the
	// directory policy is read from, and it must not come from the caller's
	// environment either. On other platforms the path is a constant.
	t.Setenv("ProgramData", filepath.Join(t.TempDir(), "spoofed"))

	if after := machineConfigPath(); after != before {
		t.Fatalf("machine config path must not follow the environment: %q became %q", before, after)
	}
}

// TestPolicyOriginDescription covers every branch of the naming used by the
// update killswitch message, including the Windows registry branch that is
// unreachable on other platforms when resolved in place.
func TestPolicyOriginDescription(t *testing.T) {
	enabled := true
	disabled := false

	cases := []struct {
		name             string
		platformValue    *bool
		platformDesc     string
		systemConfigPath string
		want             string
	}{
		{
			name:             "registry holds the setting",
			platformValue:    &enabled,
			platformDesc:     `Windows registry policy HKEY_LOCAL_MACHINE\Software\Policies\bb`,
			systemConfigPath: `C:\ProgramData\bb\config.yaml`,
			want:             `Windows registry policy HKEY_LOCAL_MACHINE\Software\Policies\bb`,
		},
		{
			name:             "registry present but does not set it",
			platformValue:    &disabled,
			platformDesc:     `Windows registry policy HKEY_LOCAL_MACHINE\Software\Policies\bb`,
			systemConfigPath: `C:\ProgramData\bb\config.yaml`,
			want:             `the system configuration file C:\ProgramData\bb\config.yaml`,
		},
		{
			name:             "no platform policy store",
			platformValue:    nil,
			platformDesc:     "",
			systemConfigPath: "/etc/bb/config.yaml",
			want:             "the system configuration file /etc/bb/config.yaml",
		},
		{
			// Set on a platform with no store to name: fall back rather than
			// return an empty description.
			name:             "platform sets it but has no description",
			platformValue:    &enabled,
			platformDesc:     "",
			systemConfigPath: "/etc/bb/config.yaml",
			want:             "the system configuration file /etc/bb/config.yaml",
		},
		{
			name:             "no resolvable config path",
			platformValue:    nil,
			platformDesc:     "",
			systemConfigPath: "   ",
			want:             "the system configuration file",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := policyOriginDescription(testCase.platformValue, testCase.platformDesc, testCase.systemConfigPath)
			if got != testCase.want {
				t.Fatalf("policyOriginDescription() = %q, want %q", got, testCase.want)
			}
		})
	}
}
