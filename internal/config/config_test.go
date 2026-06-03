package config

import (
	"os"
	"path/filepath"
	"reflect"
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
