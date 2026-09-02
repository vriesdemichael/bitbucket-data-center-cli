package config

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/diagnostics"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	"github.com/zalando/go-keyring"
	"gopkg.in/yaml.v3"
)

const (
	// No default Bitbucket version target: the supported version is whatever the
	// container stack runs and the live suite passes against (ADR 042). Operators
	// may still pin one for their own environment via BITBUCKET_VERSION_TARGET.
	defaultBitbucketVersionTarget = ""
	defaultProjectKey             = "TEST"
	defaultRequestTimeout         = 20 * time.Second
	defaultRetryCount             = 2
	defaultRetryBackoff           = 250 * time.Millisecond
	defaultLogLevel               = string(diagnostics.LevelError)
	defaultLogFormat              = string(diagnostics.FormatText)
	keyringServiceName            = "bb"
)

type AppConfig struct {
	BitbucketURL           string
	BitbucketVersionTarget string
	ProjectKey             string
	// RepoSlug is the repository within ProjectKey, from the environment or an
	// inferred git context. Commands that take --repo use the flag instead; this
	// is the fallback for the ones that do not.
	RepoSlug           string
	BitbucketToken     string
	BitbucketUsername  string
	BitbucketPassword  string
	CAFile             string
	InsecureSkipVerify bool
	ClientCertFile     string
	ClientKeyFile      string
	RequestTimeout     time.Duration
	RetryCount         int
	RetryBackoff       time.Duration
	LogLevel           string
	LogFormat          string
	DiagnosticsEnabled bool
	AuthSource         string
	// UsedInsecureStorage reports that a credential in use was read from the
	// plaintext config fallback rather than the OS keyring. It is set only when
	// such a credential is actually used, not merely present in the file.
	UsedInsecureStorage bool

	// flagSourced records which settings arrived from a command-line flag,
	// keyed by the environment variable that would otherwise have supplied
	// them. It exists so a validation failure can name the input the user
	// actually used; see nameFor.
	flagSourced map[string]bool
}

type StoredConfig struct {
	DefaultHost     string                   `yaml:"default_host,omitempty"`
	Hosts           map[string]StoredProfile `yaml:"hosts,omitempty"`
	InsecureSecrets map[string]StoredSecret  `yaml:"insecure_secrets,omitempty"`
	UpdateBaseURL   string                   `yaml:"update_base_url,omitempty"`
}

type PolicyConfig struct {
	RequireKeyring          *bool    `yaml:"require_keyring,omitempty"`
	CAFile                  string   `yaml:"ca_file,omitempty"`
	AllowedHosts            []string `yaml:"allowed_hosts,omitempty"`
	AllowInsecureSkipVerify *bool    `yaml:"allow_insecure_skip_verify,omitempty"`
	DisableUpdate           *bool    `yaml:"disable_update,omitempty"`
	UpdateBaseURL           string   `yaml:"update_base_url,omitempty"`
	// MCPAuditFile mandates where `bb ai mcp serve` writes its audit trail.
	// When set, the server audits whether or not --audit-file is passed, and
	// rejects a --audit-file naming a different path.
	//
	// This is a fleet control rather than a developer one. The agent being
	// audited runs in the developer's own IDE, so an audit destination they can
	// change by editing a config file records only what they allow it to.
	MCPAuditFile string `yaml:"mcp_audit_file,omitempty"`
	// The five settings below decide who may vouch for a new bb binary, so
	// they are read from administrative policy only — never from an
	// environment variable or a flag. See ResolveUpdateTrust.
	UpdateTrustedRoot       string `yaml:"update_trusted_root,omitempty"`
	UpdateTUFURL            string `yaml:"update_tuf_url,omitempty"`
	UpdateSignatureIdentity string `yaml:"update_signature_identity,omitempty"`
	UpdateSignatureIssuer   string `yaml:"update_signature_issuer,omitempty"`
	AllowUnverifiedUpdate   *bool  `yaml:"allow_unverified_update,omitempty"`
}

type SystemConfigFile struct {
	DefaultHost             string                   `yaml:"default_host,omitempty"`
	ProjectKey              string                   `yaml:"project_key,omitempty"`
	Hosts                   map[string]StoredProfile `yaml:"hosts,omitempty"`
	InsecureSecrets         map[string]StoredSecret  `yaml:"insecure_secrets,omitempty"`
	RequireKeyring          *bool                    `yaml:"require_keyring,omitempty"`
	CAFile                  string                   `yaml:"ca_file,omitempty"`
	AllowedHosts            []string                 `yaml:"allowed_hosts,omitempty"`
	AllowInsecureSkipVerify *bool                    `yaml:"allow_insecure_skip_verify,omitempty"`
	DisableUpdate           *bool                    `yaml:"disable_update,omitempty"`
	UpdateBaseURL           string                   `yaml:"update_base_url,omitempty"`
	MCPAuditFile            string                   `yaml:"mcp_audit_file,omitempty"`
	UpdateTrustedRoot       string                   `yaml:"update_trusted_root,omitempty"`
	UpdateTUFURL            string                   `yaml:"update_tuf_url,omitempty"`
	UpdateSignatureIdentity string                   `yaml:"update_signature_identity,omitempty"`
	UpdateSignatureIssuer   string                   `yaml:"update_signature_issuer,omitempty"`
	AllowUnverifiedUpdate   *bool                    `yaml:"allow_unverified_update,omitempty"`
	Policies                *PolicyConfig            `yaml:"policies,omitempty"`
	Policy                  *PolicyConfig            `yaml:"policy,omitempty"`
}

func (sys SystemConfigFile) StoredConfig() StoredConfig {
	return StoredConfig{
		DefaultHost:     sys.DefaultHost,
		Hosts:           sys.Hosts,
		InsecureSecrets: sys.InsecureSecrets,
		UpdateBaseURL:   sys.UpdateBaseURL,
	}
}

func (sys SystemConfigFile) PolicyConfig() PolicyConfig {
	return PolicyConfig{
		RequireKeyring:          sys.RequireKeyring,
		CAFile:                  sys.CAFile,
		AllowedHosts:            sys.AllowedHosts,
		AllowInsecureSkipVerify: sys.AllowInsecureSkipVerify,
		DisableUpdate:           sys.DisableUpdate,
		UpdateBaseURL:           sys.UpdateBaseURL,
		MCPAuditFile:            sys.MCPAuditFile,
		UpdateTrustedRoot:       sys.UpdateTrustedRoot,
		UpdateTUFURL:            sys.UpdateTUFURL,
		UpdateSignatureIdentity: sys.UpdateSignatureIdentity,
		UpdateSignatureIssuer:   sys.UpdateSignatureIssuer,
		AllowUnverifiedUpdate:   sys.AllowUnverifiedUpdate,
	}
}

type WorkspaceConfigFile struct {
	DefaultHost   string                   `yaml:"default_host,omitempty"`
	ProjectKey    string                   `yaml:"project_key,omitempty"`
	Hosts         map[string]StoredProfile `yaml:"hosts,omitempty"`
	UpdateBaseURL string                   `yaml:"update_base_url,omitempty"`
}

type StoredProfile struct {
	URL        string   `yaml:"url"`
	Aliases    []string `yaml:"aliases,omitempty"`
	Username   string   `yaml:"username,omitempty"`
	AuthMode   string   `yaml:"auth_mode,omitempty"`
	ClientCert string   `yaml:"client_cert,omitempty"`
	ClientKey  string   `yaml:"client_key,omitempty"`
}

type StoredSecret struct {
	Token    string `yaml:"token,omitempty"`
	Password string `yaml:"password,omitempty"`
}

type LoginInput struct {
	Host       string
	Aliases    []string
	Username   string
	Password   string
	Token      string
	ClientCert string
	ClientKey  string
	SetDefault bool
	// RequireKeyring fails the login when the OS keyring cannot store the
	// secret, instead of falling back to plaintext in the config file.
	RequireKeyring bool
}

type LoginResult struct {
	Host                string
	Aliases             []string
	AuthMode            string
	UsedInsecureStorage bool
}

type ServerContext struct {
	Host      string
	Aliases   []string
	AuthMode  string
	Username  string
	IsDefault bool
}

type AliasMatch struct {
	Host     string
	Endpoint string
}

// Overrides are per-invocation values that outrank the environment when
// configuration is resolved, for flags like `bb api --host` and
// `bb ai mcp serve --host/--token`.
//
// They exist so a command can target a specific instance without writing to the
// process environment. Setting BITBUCKET_URL or BITBUCKET_TOKEN to steer a load
// works, but it outlives the call: it retargets everything the process does
// afterwards, is inherited by any subprocess bb spawns, and is not safe to do
// from more than one goroutine. Passing the value down the call it belongs to
// has none of those failure modes.
type Overrides struct {
	// Host targets a specific Bitbucket instance, ahead of BITBUCKET_URL and
	// any configured default host.
	Host string
	// Token supplies the credential directly, ahead of BITBUCKET_TOKEN and any
	// stored credential.
	Token string
	// ProjectKey and RepoSlug carry a repository context resolved from the git
	// remote, ahead of BITBUCKET_PROJECT_KEY and BITBUCKET_REPO_SLUG.
	ProjectKey string
	RepoSlug   string

	// Runtime settings supplied by a flag.
	//
	// These are pointers because "not passed" and "passed empty" are different:
	// a flag left alone must let the environment keep its own precedence slot,
	// which is the layer ADR-021 describes and the implementation did not have.
	// They used to reach here by being written into BB_* with os.Setenv, which
	// destroyed the user's real value rather than outranking it for one
	// invocation.
	CAFile             *string
	InsecureSkipVerify *bool
	ClientCert         *string
	ClientKey          *string
	RequestTimeout     *string
	RetryCount         *int
	RetryBackoff       *string
}

// runtimeSetting names one setting in both spellings, so an error can blame the
// input the user actually used.
//
// A flag written into BB_* loses its origin, and validation then reports the
// environment variable: `bb --retry-count -5` said "BB_RETRY_COUNT must be
// greater than or equal to 0", sending the user to look for a variable they
// never set. ADR-054 asks for a message naming the flag; this is what lets one
// exist.
type runtimeSetting struct {
	environment string
	flag        string
}

var (
	settingCAFile             = runtimeSetting{"BB_CA_FILE", "--ca-file"}
	settingInsecureSkipVerify = runtimeSetting{"BB_INSECURE_SKIP_VERIFY", "--insecure-skip-verify"}
	settingClientCert         = runtimeSetting{"BB_CLIENT_CERT", "--client-cert"}
	settingClientKey          = runtimeSetting{"BB_CLIENT_KEY", "--client-key"}
	settingRequestTimeout     = runtimeSetting{"BB_REQUEST_TIMEOUT", "--request-timeout"}
	settingRetryCount         = runtimeSetting{"BB_RETRY_COUNT", "--retry-count"}
	settingRetryBackoff       = runtimeSetting{"BB_RETRY_BACKOFF", "--retry-backoff"}
)

// nameFor is nameOf against the sources this configuration was resolved from.
func (config AppConfig) nameFor(setting runtimeSetting) string {
	return nameOf(config.flagSourced, setting)
}

// LoadFromEnv resolves configuration from the environment and stored
// credentials, with nothing overridden.
func LoadFromEnv() (AppConfig, error) {
	return LoadWithOverrides(Overrides{})
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// LoadWithOverrides resolves configuration the same way LoadFromEnv does, with
// the given per-invocation values taking precedence.
func LoadWithOverrides(overrides Overrides) (AppConfig, error) {
	loadDotEnv()

	// flagSourced records which settings a flag supplied, so every message
	// below can name the input the user actually used rather than the variable
	// a flag was once written into.
	flagSourced := map[string]bool{}

	policy, err := LoadPolicy()
	if err != nil {
		return AppConfig{}, err
	}

	sysConfig, _ := LoadSystemConfig()
	workspaceConfig, _ := LoadWorkspaceConfig()
	storedConfig, _ := LoadStoredConfig()

	tlsSettings, err := resolveTLSSettings(policy, sysConfig, overrides, flagSourced)
	if err != nil {
		return AppConfig{}, err
	}

	requestTimeout, err := resolveDuration(flagSourced, settingRequestTimeout, overrides.RequestTimeout, defaultRequestTimeout)
	if err != nil {
		return AppConfig{}, apperrors.New(apperrors.KindValidation,
			nameOf(flagSourced, settingRequestTimeout)+" must be a valid duration (example: 20s)", err)
	}

	retryCount, err := resolveInt(flagSourced, settingRetryCount, overrides.RetryCount, defaultRetryCount)
	if err != nil {
		return AppConfig{}, apperrors.New(apperrors.KindValidation,
			nameOf(flagSourced, settingRetryCount)+" must be a non-negative integer", err)
	}

	retryBackoff, err := resolveDuration(flagSourced, settingRetryBackoff, overrides.RetryBackoff, defaultRetryBackoff)
	if err != nil {
		return AppConfig{}, apperrors.New(apperrors.KindValidation,
			nameOf(flagSourced, settingRetryBackoff)+" must be a valid duration (example: 250ms)", err)
	}

	rawLogLevel := strings.TrimSpace(os.Getenv("BB_LOG_LEVEL"))
	rawLogFormat := strings.TrimSpace(os.Getenv("BB_LOG_FORMAT"))
	diagnosticsEnabled := rawLogLevel != "" || rawLogFormat != ""

	logLevel := envOrDefault("BB_LOG_LEVEL", defaultLogLevel)
	if _, err := diagnostics.ParseLevel(logLevel); err != nil {
		return AppConfig{}, apperrors.New(apperrors.KindValidation, "BB_LOG_LEVEL must be one of: error,warn,info,debug", err)
	}

	logFormat := envOrDefault("BB_LOG_FORMAT", defaultLogFormat)
	if _, err := diagnostics.ParseFormat(logFormat); err != nil {
		return AppConfig{}, apperrors.New(apperrors.KindValidation, "BB_LOG_FORMAT must be one of: text,jsonl", err)
	}

	envHost := strings.TrimSpace(os.Getenv("BITBUCKET_URL"))
	if hostOverride := strings.TrimSpace(overrides.Host); hostOverride != "" {
		envHost = hostOverride
	}
	resolvedURL := ""
	if envHost != "" {
		resolvedURL = normalizeURL(envHost)
	} else if workspaceConfig.DefaultHost != "" {
		resolvedURL = resolveDefaultHostURL(workspaceConfig.DefaultHost, workspaceConfig.Hosts, storedConfig.Hosts, sysConfig.Hosts)
	} else if storedConfig.DefaultHost != "" {
		resolvedURL = resolveDefaultHostURL(storedConfig.DefaultHost, storedConfig.Hosts, sysConfig.Hosts, workspaceConfig.Hosts)
	} else if sysConfig.DefaultHost != "" {
		resolvedURL = resolveDefaultHostURL(sysConfig.DefaultHost, sysConfig.Hosts, storedConfig.Hosts, workspaceConfig.Hosts)
	}
	if resolvedURL == "" {
		return AppConfig{}, apperrors.New(apperrors.KindValidation, "no Bitbucket host configured: set BITBUCKET_URL or run 'bb auth login <host>'", nil)
	}

	if len(policy.AllowedHosts) > 0 && !IsHostAllowed(resolvedURL, policy.AllowedHosts) {
		return AppConfig{}, apperrors.New(
			apperrors.KindAuthorization,
			fmt.Sprintf("host %q is not permitted by administrative policy; allowed hosts: %s", resolvedURL, strings.Join(policy.AllowedHosts, ", ")),
			nil,
		)
	}

	// An inferred repository context outranks the environment for this
	// invocation, the same way a flag does. It used to arrive as the variable,
	// because inference wrote it there.
	projectKey := firstNonEmpty(strings.TrimSpace(overrides.ProjectKey), envOrDefault("BITBUCKET_PROJECT_KEY", ""))
	if projectKey == "" {
		projectKey = workspaceConfig.ProjectKey
	}
	if projectKey == "" {
		projectKey = defaultProjectKey
	}

	config := AppConfig{
		BitbucketURL:           resolvedURL,
		BitbucketVersionTarget: envOrDefault("BITBUCKET_VERSION_TARGET", defaultBitbucketVersionTarget),
		ProjectKey:             projectKey,
		RepoSlug:               firstNonEmpty(strings.TrimSpace(overrides.RepoSlug), envOrDefault("BITBUCKET_REPO_SLUG", "")),
		BitbucketToken:         firstNonEmpty(strings.TrimSpace(overrides.Token), envOrDefault("BITBUCKET_TOKEN", "")),
		BitbucketUsername:      envOrDefault("BITBUCKET_USERNAME", envOrDefault("BITBUCKET_USER", envOrDefault("ADMIN_USER", ""))),
		BitbucketPassword:      envOrDefault("BITBUCKET_PASSWORD", envOrDefault("ADMIN_PASSWORD", "")),
		CAFile:                 tlsSettings.CAFile,
		InsecureSkipVerify:     tlsSettings.InsecureSkipVerify,
		ClientCertFile:         tlsSettings.ClientCertFile,
		ClientKeyFile:          tlsSettings.ClientKeyFile,
		RequestTimeout:         requestTimeout,
		RetryCount:             retryCount,
		RetryBackoff:           retryBackoff,
		LogLevel:               strings.ToLower(strings.TrimSpace(logLevel)),
		LogFormat:              strings.ToLower(strings.TrimSpace(logFormat)),
		DiagnosticsEnabled:     diagnosticsEnabled,
		AuthSource:             "env/default",
		flagSourced:            flagSourced,
	}

	if os.Getenv("BB_DISABLE_STORED_CONFIG") != "1" {
		stored, foundStored := resolveStoredCredentials(storedConfig, config.BitbucketURL)
		if !foundStored && len(workspaceConfig.Hosts) > 0 {
			stored, foundStored = resolveStoredCredentials(StoredConfig{Hosts: workspaceConfig.Hosts}, config.BitbucketURL)
		}
		if !foundStored && len(sysConfig.Hosts) > 0 {
			stored, foundStored = resolveStoredCredentials(sysConfig.StoredConfig(), config.BitbucketURL)
		}
		if foundStored {
			adoptedStoredSecret := false

			if config.BitbucketUsername == "" && stored.BitbucketUsername != "" {
				config.BitbucketUsername = stored.BitbucketUsername
			}
			if config.BitbucketToken == "" && stored.BitbucketToken != "" {
				config.BitbucketToken = stored.BitbucketToken
				adoptedStoredSecret = true
			}
			if config.BitbucketPassword == "" && stored.BitbucketPassword != "" {
				config.BitbucketPassword = stored.BitbucketPassword
				adoptedStoredSecret = true
			}
			if config.ClientCertFile == "" && stored.ClientCertFile != "" {
				config.ClientCertFile = stored.ClientCertFile
			}
			if config.ClientKeyFile == "" && stored.ClientKeyFile != "" {
				config.ClientKeyFile = stored.ClientKeyFile
			}
			if config.BitbucketToken != "" || (config.BitbucketUsername != "" && config.BitbucketPassword != "") {
				config.AuthSource = "stored"
			}

			// Tracks the secret actually in use, not the label on AuthSource.
			config.UsedInsecureStorage = adoptedStoredSecret && stored.UsedInsecureStorage
		}

		if os.Getenv("BITBUCKET_TOKEN") != "" || os.Getenv("BITBUCKET_USERNAME") != "" || os.Getenv("BITBUCKET_USER") != "" || os.Getenv("BITBUCKET_PASSWORD") != "" || os.Getenv("ADMIN_USER") != "" || os.Getenv("ADMIN_PASSWORD") != "" {
			config.AuthSource = "env"
		}
	}

	if config.UsedInsecureStorage {
		requireKeyring, err := RequireKeyring()
		if err != nil {
			return AppConfig{}, err
		}
		if requireKeyring {
			return AppConfig{}, apperrors.New(
				apperrors.KindPermanent,
				"stored credentials for this host are held in the plaintext config fallback and keyring-backed storage is required; run 'bb auth login <host>' again with a working keyring, or supply BITBUCKET_TOKEN instead",
				nil,
			)
		}
	}

	if err := config.Validate(); err != nil {
		return AppConfig{}, err
	}

	return config, nil
}

func loadDotEnv() {
	for _, candidate := range dotenvCandidates() {
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		_ = godotenv.Load(candidate)
	}
}

func dotenvCandidates() []string {
	cwd, err := os.Getwd()
	if err != nil {
		return []string{".env"}
	}

	searchRoot := cwd
	if detected, found := findRepositoryRoot(cwd); found {
		searchRoot = detected
	}

	candidates := make([]string, 0)
	seen := map[string]struct{}{}
	for directory := cwd; ; directory = filepath.Dir(directory) {
		candidate := filepath.Join(directory, ".env")
		if _, ok := seen[candidate]; !ok {
			seen[candidate] = struct{}{}
			candidates = append(candidates, candidate)
		}

		parent := filepath.Dir(directory)
		if parent == directory || directory == searchRoot {
			break
		}
	}

	return candidates
}

func findRepositoryRoot(startDirectory string) (string, bool) {
	for directory := filepath.Clean(startDirectory); ; directory = filepath.Dir(directory) {
		if hasRepositoryMarker(directory) {
			return directory, true
		}

		parent := filepath.Dir(directory)
		if parent == directory {
			return "", false
		}
	}
}

func hasRepositoryMarker(directory string) bool {
	goModPath := filepath.Join(directory, "go.mod")
	if info, err := os.Stat(goModPath); err == nil && !info.IsDir() {
		return true
	}

	gitPath := filepath.Join(directory, ".git")
	if _, err := os.Stat(gitPath); err == nil {
		return true
	}

	return false
}

func resolveDefaultHostURL(defaultHost string, hostMaps ...map[string]StoredProfile) string {
	for _, hosts := range hostMaps {
		if profile, ok := hosts[defaultHost]; ok && profile.URL != "" {
			return normalizeURL(profile.URL)
		}
	}
	if strings.HasPrefix(defaultHost, "http://") || strings.HasPrefix(defaultHost, "https://") {
		return normalizeURL(defaultHost)
	}
	return ""
}

func SaveLogin(input LoginInput) (LoginResult, error) {
	host := normalizeURL(strings.TrimSpace(input.Host))
	if host == "" {
		return LoginResult{}, apperrors.New(apperrors.KindValidation, "host is required", nil)
	}

	policy, err := LoadPolicy()
	if err != nil {
		return LoginResult{}, err
	}
	if len(policy.AllowedHosts) > 0 && !IsHostAllowed(host, policy.AllowedHosts) {
		return LoginResult{}, apperrors.New(
			apperrors.KindAuthorization,
			fmt.Sprintf("host %q is not permitted by administrative policy; allowed hosts: %s", host, strings.Join(policy.AllowedHosts, ", ")),
			nil,
		)
	}

	aliases, err := normalizeAliases(input.Aliases)
	if err != nil {
		return LoginResult{}, err
	}

	hasToken := strings.TrimSpace(input.Token) != ""
	hasBasic := strings.TrimSpace(input.Username) != "" || strings.TrimSpace(input.Password) != ""
	if hasToken == hasBasic {
		return LoginResult{}, apperrors.New(apperrors.KindValidation, "provide either token or username/password", nil)
	}
	if hasBasic && (strings.TrimSpace(input.Username) == "" || strings.TrimSpace(input.Password) == "") {
		return LoginResult{}, apperrors.New(apperrors.KindValidation, "username and password must be provided together", nil)
	}

	stored, _ := LoadStoredConfig()
	if stored.Hosts == nil {
		stored.Hosts = map[string]StoredProfile{}
	}
	if stored.InsecureSecrets == nil {
		stored.InsecureSecrets = map[string]StoredSecret{}
	}

	key := hostKey(host)

	// Aliases are host-recognition config, not credentials: re-authenticating
	// against a host should not discard the ones already stored. Discovery
	// cannot find every alias, so an alias added by hand would otherwise be lost
	// on the next login -- silently, and only noticed later when repository
	// inference stops recognising a remote.
	aliases = mergeAliases(normalizeStoredAliases(stored.Hosts[key].Aliases), aliases)

	if err := ensureAliasOwnership(stored, key, aliases); err != nil {
		return LoginResult{}, err
	}

	existingProfile := stored.Hosts[key]
	profile := StoredProfile{
		URL:        host,
		Aliases:    aliases,
		ClientCert: existingProfile.ClientCert,
		ClientKey:  existingProfile.ClientKey,
	}
	if trimmed := strings.TrimSpace(input.ClientCert); trimmed != "" {
		profile.ClientCert = trimmed
	}
	if trimmed := strings.TrimSpace(input.ClientKey); trimmed != "" {
		profile.ClientKey = trimmed
	}
	result := LoginResult{Host: host, Aliases: append([]string{}, aliases...)}

	if hasToken {
		profile.AuthMode = "token"
		result.AuthMode = "token"
	} else {
		profile.AuthMode = "basic"
		profile.Username = strings.TrimSpace(input.Username)
		result.AuthMode = "basic"
	}

	requireKeyring, err := requireKeyringPolicy(input.RequireKeyring)
	if err != nil {
		return LoginResult{}, err
	}

	insecure := StoredSecret{}
	if hasToken {
		if keyringErr := keyringSet(keyringServiceName, key+":token", strings.TrimSpace(input.Token)); keyringErr != nil {
			if requireKeyring {
				return LoginResult{}, keyringUnavailableError(keyringErr)
			}
			insecure.Token = strings.TrimSpace(input.Token)
			result.UsedInsecureStorage = true
		}
		_ = keyringDelete(keyringServiceName, key+":password")
	} else {
		if keyringErr := keyringSet(keyringServiceName, key+":password", strings.TrimSpace(input.Password)); keyringErr != nil {
			if requireKeyring {
				return LoginResult{}, keyringUnavailableError(keyringErr)
			}
			insecure.Password = strings.TrimSpace(input.Password)
			result.UsedInsecureStorage = true
		}
		_ = keyringDelete(keyringServiceName, key+":token")
	}

	if insecure.Token != "" || insecure.Password != "" {
		stored.InsecureSecrets[key] = insecure
	} else {
		delete(stored.InsecureSecrets, key)
	}

	stored.Hosts[key] = profile
	if input.SetDefault || stored.DefaultHost == "" {
		stored.DefaultHost = key
	}

	if err := SaveStoredConfig(stored); err != nil {
		return LoginResult{}, err
	}

	return result, nil
}

func SetHostAliases(host string, aliases []string) ([]string, error) {
	trimmedHost := strings.TrimSpace(host)
	if trimmedHost == "" {
		return nil, apperrors.New(apperrors.KindValidation, "host is required", nil)
	}

	normalizedAliases, err := normalizeAliases(aliases)
	if err != nil {
		return nil, err
	}

	stored, err := LoadStoredConfig()
	if err != nil {
		return nil, err
	}

	key := hostKey(trimmedHost)
	profile, ok := stored.Hosts[key]
	if !ok {
		return nil, apperrors.New(apperrors.KindNotFound, fmt.Sprintf("no stored server context for %s", normalizeURL(trimmedHost)), nil)
	}

	if err := ensureAliasOwnership(stored, key, normalizedAliases); err != nil {
		return nil, err
	}

	profile.Aliases = normalizedAliases
	stored.Hosts[key] = profile
	if err := SaveStoredConfig(stored); err != nil {
		return nil, err
	}

	return append([]string{}, normalizedAliases...), nil
}

func AddHostAliases(host string, aliases []string) ([]string, string, error) {
	trimmedHost := strings.TrimSpace(host)
	if trimmedHost == "" {
		return nil, "", apperrors.New(apperrors.KindValidation, "host is required", nil)
	}

	normalizedAliases, err := normalizeAliases(aliases)
	if err != nil {
		return nil, "", err
	}

	stored, err := LoadStoredConfig()
	if err != nil {
		return nil, "", err
	}

	key := hostKey(trimmedHost)
	profile, ok := stored.Hosts[key]
	if !ok {
		return nil, "", apperrors.New(apperrors.KindNotFound, fmt.Sprintf("no stored server context for %s", normalizeURL(trimmedHost)), nil)
	}

	merged := append([]string{}, normalizeStoredAliases(profile.Aliases)...)
	seen := map[string]struct{}{}
	for _, existing := range merged {
		seen[existing] = struct{}{}
	}
	for _, alias := range normalizedAliases {
		if _, exists := seen[alias]; exists {
			continue
		}
		seen[alias] = struct{}{}
		merged = append(merged, alias)
	}

	if err := ensureAliasOwnership(stored, key, merged); err != nil {
		return nil, "", err
	}

	profile.Aliases = merged
	stored.Hosts[key] = profile
	if err := SaveStoredConfig(stored); err != nil {
		return nil, "", err
	}

	return append([]string{}, merged...), normalizeURL(profile.URL), nil
}

func RemoveHostAlias(host string, alias string) ([]string, string, error) {
	trimmedHost := strings.TrimSpace(host)
	if trimmedHost == "" {
		return nil, "", apperrors.New(apperrors.KindValidation, "host is required", nil)
	}

	normalizedAlias, err := normalizeAlias(alias)
	if err != nil {
		return nil, "", err
	}

	stored, err := LoadStoredConfig()
	if err != nil {
		return nil, "", err
	}

	key := hostKey(trimmedHost)
	profile, ok := stored.Hosts[key]
	if !ok {
		return nil, "", apperrors.New(apperrors.KindNotFound, fmt.Sprintf("no stored server context for %s", normalizeURL(trimmedHost)), nil)
	}

	updated := make([]string, 0, len(profile.Aliases))
	removed := false
	for _, existing := range normalizeStoredAliases(profile.Aliases) {
		if existing == normalizedAlias {
			removed = true
			continue
		}
		updated = append(updated, existing)
	}
	if !removed {
		return nil, "", apperrors.New(apperrors.KindNotFound, fmt.Sprintf("alias %s is not configured for %s", normalizedAlias, normalizeURL(trimmedHost)), nil)
	}

	profile.Aliases = updated
	stored.Hosts[key] = profile
	if err := SaveStoredConfig(stored); err != nil {
		return nil, "", err
	}

	return append([]string{}, updated...), normalizeURL(profile.URL), nil
}

func ListHostAliases(host string) ([]string, string, error) {
	trimmedHost := strings.TrimSpace(host)
	if trimmedHost == "" {
		return nil, "", apperrors.New(apperrors.KindValidation, "host is required", nil)
	}

	stored, err := LoadStoredConfig()
	if err != nil {
		return nil, "", err
	}

	key := hostKey(trimmedHost)
	profile, ok := stored.Hosts[key]
	if !ok {
		return nil, "", apperrors.New(apperrors.KindNotFound, fmt.Sprintf("no stored server context for %s", normalizeURL(trimmedHost)), nil)
	}

	return append([]string{}, normalizeStoredAliases(profile.Aliases)...), normalizeURL(profile.URL), nil
}

func MatchStoredHost(host string) (AliasMatch, bool, error) {
	stored, err := LoadStoredConfig()
	if err != nil {
		return AliasMatch{}, false, err
	}

	return resolveStoredHostAlias(stored, host)
}

func Logout(host string) error {
	stored, _ := LoadStoredConfig()
	hostURL := normalizeURL(strings.TrimSpace(host))
	if hostURL == "" {
		if stored.DefaultHost == "" {
			return apperrors.New(apperrors.KindNotFound, "no stored host to logout", nil)
		}
		hostURL = stored.DefaultHost
	}

	key := hostKey(hostURL)
	_ = keyringDelete(keyringServiceName, key+":token")
	_ = keyringDelete(keyringServiceName, key+":password")

	delete(stored.Hosts, key)
	delete(stored.InsecureSecrets, key)
	if stored.DefaultHost == key {
		stored.DefaultHost = ""
		for next := range stored.Hosts {
			stored.DefaultHost = next
			break
		}
	}

	return SaveStoredConfig(stored)
}

func ListServerContexts() ([]ServerContext, error) {
	stored, err := LoadStoredConfig()
	if err != nil {
		return nil, err
	}

	contexts := make([]ServerContext, 0, len(stored.Hosts))
	for key, profile := range stored.Hosts {
		mode := strings.TrimSpace(profile.AuthMode)
		if mode == "" {
			mode = "none"
		}

		contexts = append(contexts, ServerContext{
			Host:      normalizeURL(profile.URL),
			Aliases:   append([]string{}, normalizeStoredAliases(profile.Aliases)...),
			AuthMode:  mode,
			Username:  strings.TrimSpace(profile.Username),
			IsDefault: key == stored.DefaultHost,
		})
	}

	sort.SliceStable(contexts, func(left, right int) bool {
		if contexts[left].IsDefault != contexts[right].IsDefault {
			return contexts[left].IsDefault
		}
		return contexts[left].Host < contexts[right].Host
	})

	return contexts, nil
}

func SetDefaultHost(host string) (string, error) {
	trimmedHost := strings.TrimSpace(host)
	if trimmedHost == "" {
		return "", apperrors.New(apperrors.KindValidation, "host is required", nil)
	}

	stored, err := LoadStoredConfig()
	if err != nil {
		return "", err
	}

	key := hostKey(trimmedHost)
	profile, ok := stored.Hosts[key]
	if !ok {
		return "", apperrors.New(apperrors.KindNotFound, fmt.Sprintf("no stored server context for %s", normalizeURL(trimmedHost)), nil)
	}

	stored.DefaultHost = key
	if err := SaveStoredConfig(stored); err != nil {
		return "", err
	}

	return normalizeURL(profile.URL), nil
}

func LoadStoredConfig() (StoredConfig, error) {
	path, err := ConfigPath()
	if err != nil {
		return StoredConfig{}, err
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return StoredConfig{Hosts: map[string]StoredProfile{}, InsecureSecrets: map[string]StoredSecret{}}, nil
		}
		return StoredConfig{}, err
	}

	if err := ValidateConfigYAML(raw); err != nil {
		return StoredConfig{}, err
	}

	var stored StoredConfig
	if err := yaml.Unmarshal(raw, &stored); err != nil {
		return StoredConfig{}, err
	}
	if stored.Hosts == nil {
		stored.Hosts = map[string]StoredProfile{}
	}
	if stored.InsecureSecrets == nil {
		stored.InsecureSecrets = map[string]StoredSecret{}
	}

	return stored, nil
}

func SaveStoredConfig(stored StoredConfig) error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	encoded, err := yaml.Marshal(stored)
	if err != nil {
		return err
	}

	return os.WriteFile(path, encoded, 0o600)
}

func ConfigPath() (string, error) {
	if custom := strings.TrimSpace(os.Getenv("BB_CONFIG_PATH")); custom != "" {
		return custom, nil
	}

	baseDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(baseDir, "bb", "config.yaml"), nil
}

// SystemConfigPath returns the machine-level configuration file, the one that
// carries administrative policy.
//
// BB_SYSTEM_CONFIG_PATH redirects it under `go test` and nowhere else. Outside a
// test, the environment is precisely what this tier exists to outrank: a
// released binary that honoured the variable would let anyone able to set one in
// a user's shell replace the entire policy tier with a file of their own, and
// with it require_keyring, allowed_hosts, allow_insecure_skip_verify, the
// mandated ca_file and disable_update. Windows registry policy was never
// affected, because it is merged from HKLM after the file.
//
// Automation that needs different policy writes the real path instead — in a
// container image that is one line at build time.
func SystemConfigPath() (string, error) {
	return systemConfigPath(os.Getenv("BB_SYSTEM_CONFIG_PATH"), testing.Testing())
}

func systemConfigPath(pathOverride string, overrideAllowed bool) (string, error) {
	if custom := strings.TrimSpace(pathOverride); custom != "" && overrideAllowed {
		return custom, nil
	}

	return machineConfigPath(), nil
}

func WorkspaceConfigPath() (string, error) {
	if custom := strings.TrimSpace(os.Getenv("BB_WORKSPACE_CONFIG_PATH")); custom != "" {
		return custom, nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	searchRoot := cwd
	if detected, found := findRepositoryRoot(cwd); found {
		searchRoot = detected
	}

	for directory := cwd; ; directory = filepath.Dir(directory) {
		candidate := filepath.Join(directory, ".bb", "config.yaml")
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return candidate, nil
		}

		parent := filepath.Dir(directory)
		if parent == directory || directory == searchRoot {
			break
		}
	}

	return "", nil
}

func LoadSystemConfig() (SystemConfigFile, error) {
	path, err := SystemConfigPath()
	if err != nil {
		return SystemConfigFile{}, err
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return SystemConfigFile{
				Hosts:           map[string]StoredProfile{},
				InsecureSecrets: map[string]StoredSecret{},
			}, nil
		}
		return SystemConfigFile{}, err
	}

	if err := ValidateConfigYAML(raw); err != nil {
		return SystemConfigFile{}, err
	}

	var sys SystemConfigFile
	if err := yaml.Unmarshal(raw, &sys); err != nil {
		return SystemConfigFile{}, err
	}
	if sys.Hosts == nil {
		sys.Hosts = map[string]StoredProfile{}
	}
	if sys.InsecureSecrets == nil {
		sys.InsecureSecrets = map[string]StoredSecret{}
	}

	return sys, nil
}

func LoadWorkspaceConfig() (WorkspaceConfigFile, error) {
	path, err := WorkspaceConfigPath()
	if err != nil || path == "" {
		return WorkspaceConfigFile{Hosts: map[string]StoredProfile{}}, err
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return WorkspaceConfigFile{Hosts: map[string]StoredProfile{}}, nil
		}
		return WorkspaceConfigFile{Hosts: map[string]StoredProfile{}}, err
	}

	if err := ValidateConfigYAML(raw); err != nil {
		return WorkspaceConfigFile{Hosts: map[string]StoredProfile{}}, err
	}

	var ws WorkspaceConfigFile
	if err := yaml.Unmarshal(raw, &ws); err != nil {
		return WorkspaceConfigFile{Hosts: map[string]StoredProfile{}}, err
	}
	if ws.Hosts == nil {
		ws.Hosts = map[string]StoredProfile{}
	}

	return ws, nil
}

func LoadPolicy() (PolicyConfig, error) {
	sys, err := LoadSystemConfig()
	if err != nil {
		return PolicyConfig{}, err
	}

	policy := sys.PolicyConfig()
	if sys.Policies != nil {
		mergePolicy(&policy, *sys.Policies)
	}
	if sys.Policy != nil {
		mergePolicy(&policy, *sys.Policy)
	}

	platformPolicy := loadPlatformPolicy()
	mergePolicy(&policy, platformPolicy)

	return policy, nil
}

func mergePolicy(target *PolicyConfig, source PolicyConfig) {
	if source.RequireKeyring != nil {
		target.RequireKeyring = source.RequireKeyring
	}
	if strings.TrimSpace(source.CAFile) != "" {
		target.CAFile = strings.TrimSpace(source.CAFile)
	}
	if len(source.AllowedHosts) > 0 {
		target.AllowedHosts = source.AllowedHosts
	}
	if source.AllowInsecureSkipVerify != nil {
		target.AllowInsecureSkipVerify = source.AllowInsecureSkipVerify
	}
	if source.DisableUpdate != nil {
		target.DisableUpdate = source.DisableUpdate
	}
	if strings.TrimSpace(source.UpdateBaseURL) != "" {
		target.UpdateBaseURL = strings.TrimSpace(source.UpdateBaseURL)
	}
	if strings.TrimSpace(source.MCPAuditFile) != "" {
		target.MCPAuditFile = strings.TrimSpace(source.MCPAuditFile)
	}
	if strings.TrimSpace(source.UpdateTrustedRoot) != "" {
		target.UpdateTrustedRoot = strings.TrimSpace(source.UpdateTrustedRoot)
	}
	if strings.TrimSpace(source.UpdateTUFURL) != "" {
		target.UpdateTUFURL = strings.TrimSpace(source.UpdateTUFURL)
	}
	if strings.TrimSpace(source.UpdateSignatureIdentity) != "" {
		target.UpdateSignatureIdentity = strings.TrimSpace(source.UpdateSignatureIdentity)
	}
	if strings.TrimSpace(source.UpdateSignatureIssuer) != "" {
		target.UpdateSignatureIssuer = strings.TrimSpace(source.UpdateSignatureIssuer)
	}
	if source.AllowUnverifiedUpdate != nil {
		target.AllowUnverifiedUpdate = source.AllowUnverifiedUpdate
	}
}

// TLSSettings is the resolved TLS material an outbound HTTP client is built
// from, after administrative policy has been applied.
type TLSSettings struct {
	CAFile             string
	InsecureSkipVerify bool
	ClientCertFile     string
	ClientKeyFile      string
}

// ResolveTLSSettings resolves the TLS configuration from the environment with
// administrative policy and system configuration layered on top.
//
// It is deliberately independent of host resolution and stored credentials, so
// commands that run without a configured Bitbucket host — `bb update` in
// particular — inherit the same policy as the API client instead of reading the
// environment on their own.
// ResolveTLSSettingsWith is ResolveTLSSettings for a caller that has global
// flags to apply. bb update is the one that matters: it downloads and executes
// a new binary, and --ca-file, --insecure-skip-verify and --client-cert have to
// reach it. They used to, by being written into BB_*; carrying them as values
// broke that until this existed.
func ResolveTLSSettingsWith(overrides Overrides) (TLSSettings, error) {
	policy, err := LoadPolicy()
	if err != nil {
		return TLSSettings{}, err
	}
	sysConfig, _ := LoadSystemConfig()

	return resolveTLSSettings(policy, sysConfig, overrides, map[string]bool{})
}

func ResolveTLSSettings() (TLSSettings, error) {
	policy, err := LoadPolicy()
	if err != nil {
		return TLSSettings{}, err
	}
	sysConfig, _ := LoadSystemConfig()
	return ResolveTLSSettingsFrom(policy, sysConfig)
}

// ResolveTLSSettingsFrom is ResolveTLSSettings for callers that have already
// loaded policy and system configuration.
// ResolveTLSSettingsFrom is ResolveTLSSettings for callers that have already
// loaded policy and system configuration, and have no flags to apply.
func ResolveTLSSettingsFrom(policy PolicyConfig, sysConfig SystemConfigFile) (TLSSettings, error) {
	return resolveTLSSettings(policy, sysConfig, Overrides{}, map[string]bool{})
}

func resolveTLSSettings(policy PolicyConfig, sysConfig SystemConfigFile, overrides Overrides, sourced map[string]bool) (TLSSettings, error) {
	insecureSkipVerify, err := resolveBool(sourced, settingInsecureSkipVerify, overrides.InsecureSkipVerify, false)
	if err != nil {
		return TLSSettings{}, apperrors.New(apperrors.KindValidation, nameOf(sourced, settingInsecureSkipVerify)+" must be a boolean", err)
	}
	if policy.AllowInsecureSkipVerify != nil && !*policy.AllowInsecureSkipVerify && insecureSkipVerify {
		return TLSSettings{}, apperrors.New(apperrors.KindAuthorization, "insecure TLS verification is disabled by administrative policy", nil)
	}

	caFile := resolveString(sourced, settingCAFile, overrides.CAFile)
	if policy.CAFile != "" {
		if caFile == "" {
			caFile = policy.CAFile
		} else if filepath.Clean(caFile) != filepath.Clean(policy.CAFile) {
			return TLSSettings{}, apperrors.New(
				apperrors.KindAuthorization,
				fmt.Sprintf("overriding CA bundle is disabled by administrative policy; mandated CA file: %s", policy.CAFile),
				nil,
			)
		}
	} else if caFile == "" && sysConfig.CAFile != "" {
		caFile = sysConfig.CAFile
	}

	return TLSSettings{
		CAFile:             caFile,
		InsecureSkipVerify: insecureSkipVerify,
		ClientCertFile:     resolveString(sourced, settingClientCert, overrides.ClientCert),
		ClientKeyFile:      resolveString(sourced, settingClientKey, overrides.ClientKey),
	}, nil
}

func IsHostAllowed(targetURL string, allowedHosts []string) bool {
	if len(allowedHosts) == 0 {
		return true
	}

	trimmedTarget := strings.TrimSpace(targetURL)
	normTarget := normalizeURL(trimmedTarget)
	targetParsed, err := url.Parse(normTarget)
	targetHost := ""
	if err == nil {
		targetHost = strings.ToLower(targetParsed.Hostname())
	}

	for _, allowed := range allowedHosts {
		trimmedAllowed := strings.TrimSpace(allowed)
		if trimmedAllowed == "" {
			continue
		}
		normAllowed := normalizeURL(trimmedAllowed)
		if normAllowed == normTarget {
			return true
		}
		allowedParsed, parseErr := url.Parse(normAllowed)
		if parseErr == nil && strings.ToLower(allowedParsed.Hostname()) == targetHost {
			return true
		}
		if strings.EqualFold(trimmedAllowed, targetHost) {
			return true
		}
	}

	return false
}

func ResolveUpdateBaseURL(flagValue string) (string, error) {
	if trimmed := strings.TrimSpace(flagValue); trimmed != "" {
		if _, err := url.Parse(trimmed); err != nil {
			return "", apperrors.New(apperrors.KindValidation, fmt.Sprintf("invalid update base URL %q: %v", trimmed, err), err)
		}
		return normalizeURL(trimmed), nil
	}
	if envVal := strings.TrimSpace(os.Getenv("BB_UPDATE_BASE_URL")); envVal != "" {
		return normalizeURL(envVal), nil
	}
	if ws, err := LoadWorkspaceConfig(); err == nil && strings.TrimSpace(ws.UpdateBaseURL) != "" {
		return normalizeURL(ws.UpdateBaseURL), nil
	}
	if stored, err := LoadStoredConfig(); err == nil && strings.TrimSpace(stored.UpdateBaseURL) != "" {
		return normalizeURL(stored.UpdateBaseURL), nil
	}
	if sys, err := LoadSystemConfig(); err == nil {
		if strings.TrimSpace(sys.UpdateBaseURL) != "" {
			return normalizeURL(sys.UpdateBaseURL), nil
		}
		if sys.Policies != nil && strings.TrimSpace(sys.Policies.UpdateBaseURL) != "" {
			return normalizeURL(sys.Policies.UpdateBaseURL), nil
		}
		if sys.Policy != nil && strings.TrimSpace(sys.Policy.UpdateBaseURL) != "" {
			return normalizeURL(sys.Policy.UpdateBaseURL), nil
		}
	}
	policy, err := LoadPolicy()
	if err == nil && strings.TrimSpace(policy.UpdateBaseURL) != "" {
		return normalizeURL(policy.UpdateBaseURL), nil
	}
	return "https://api.github.com", nil
}

// UpdateTrust describes who is allowed to vouch for a new bb binary, and how
// the trust material backing that decision is obtained.
type UpdateTrust struct {
	// TrustedRootPath is a Sigstore trusted_root.json on disk. Set, it removes
	// the only remaining network dependency in verification: everything else
	// (SCTs, the Rekor inclusion promise, observer timestamps) is checked
	// against keys inside the trusted root, with no Fulcio or Rekor calls.
	TrustedRootPath string
	// TUFRepositoryURL is a mirror of the Sigstore TUF repository, for
	// organisations that mirror it rather than shipping a trusted root file.
	TUFRepositoryURL string
	// SignatureIdentity and SignatureIssuer override the release signer that
	// bb pins by default, for organisations that re-sign mirrored artifacts
	// with their own Fulcio instance.
	SignatureIdentity string
	SignatureIssuer   string
	// AllowUnverified drops Sigstore verification entirely. Checksum
	// verification still applies.
	AllowUnverified bool
}

// Configured reports whether policy asked for anything other than the built-in
// public-good trust configuration.
func (trust UpdateTrust) Configured() bool {
	return trust.TrustedRootPath != "" ||
		trust.TUFRepositoryURL != "" ||
		trust.SignatureIdentity != "" ||
		trust.SignatureIssuer != "" ||
		trust.AllowUnverified
}

// ResolveUpdateTrust reads the update trust settings from administrative
// policy.
//
// These deliberately have no environment variable or flag form. Every one of
// them changes which signer bb will accept for a binary it is about to execute,
// so honouring them from the environment would hand that decision to anyone who
// can set a variable in the user's shell. Policy — the system configuration
// file, or HKLM\Software\Policies\bb on Windows — is the only source.
func ResolveUpdateTrust() (UpdateTrust, error) {
	policy, err := LoadPolicy()
	if err != nil {
		return UpdateTrust{}, err
	}
	return resolveUpdateTrust(policy)
}

func resolveUpdateTrust(policy PolicyConfig) (UpdateTrust, error) {
	trust := UpdateTrust{
		TrustedRootPath:   strings.TrimSpace(policy.UpdateTrustedRoot),
		TUFRepositoryURL:  strings.TrimSpace(policy.UpdateTUFURL),
		SignatureIdentity: strings.TrimSpace(policy.UpdateSignatureIdentity),
		SignatureIssuer:   strings.TrimSpace(policy.UpdateSignatureIssuer),
	}
	if policy.AllowUnverifiedUpdate != nil {
		trust.AllowUnverified = *policy.AllowUnverifiedUpdate
	}

	if trust.TrustedRootPath != "" && trust.TUFRepositoryURL != "" {
		return UpdateTrust{}, apperrors.New(
			apperrors.KindValidation,
			"update_trusted_root and update_tuf_url are mutually exclusive; configure one source of Sigstore trust material",
			nil,
		)
	}

	if trust.TrustedRootPath != "" {
		info, err := os.Stat(trust.TrustedRootPath)
		if err != nil {
			return UpdateTrust{}, apperrors.New(
				apperrors.KindValidation,
				fmt.Sprintf("update_trusted_root is invalid: %q", trust.TrustedRootPath),
				err,
			)
		}
		if info.IsDir() {
			return UpdateTrust{}, apperrors.New(
				apperrors.KindValidation,
				fmt.Sprintf("update_trusted_root must be a file, not a directory: %q", trust.TrustedRootPath),
				nil,
			)
		}
	}

	if trust.TUFRepositoryURL != "" {
		// url.Parse is not a check here: it accepts a bare word, a relative
		// path, and any scheme at all. This setting names where the Sigstore
		// trust material for a binary bb is about to execute comes from, so it
		// is held to an absolute https URL. The sibling update_trusted_root is
		// validated properly, which made the asymmetry easy to miss.
		parsed, err := url.ParseRequestURI(trust.TUFRepositoryURL)
		if err != nil {
			return UpdateTrust{}, apperrors.New(
				apperrors.KindValidation,
				fmt.Sprintf("update_tuf_url is invalid: %q", trust.TUFRepositoryURL),
				err,
			)
		}
		if !strings.EqualFold(parsed.Scheme, "https") || parsed.Host == "" {
			return UpdateTrust{}, apperrors.New(
				apperrors.KindValidation,
				fmt.Sprintf("update_tuf_url must be an absolute https URL: %q", trust.TUFRepositoryURL),
				nil,
			)
		}
	}

	return trust, nil
}

// IsUpdateDisabled reports whether self-update is turned off, and by which
// lever.
//
// Naming the lever is the point of the second return value. An environment
// variable is a legitimate way to administer a fleet — through MDM, a container
// image, a login profile, a CI runner definition — so both sources are real
// policy, and neither is being demoted here. But they live in completely
// different places, and an operator trying to re-enable self-update has to know
// whether to edit the policy file or hunt down where BB_DISABLE_UPDATE is being
// set, which is much the harder of the two to find when the message does not
// say.
func IsUpdateDisabled() (bool, string, error) {
	policy, err := LoadPolicy()
	if err != nil {
		return false, "", err
	}
	if policy.DisableUpdate != nil && *policy.DisableUpdate {
		return true, fmt.Sprintf(
			"self-update is disabled by administrative policy (disable_update in %s); update bb using your system package manager",
			disableUpdatePolicyOrigin(),
		), nil
	}
	if envVal := strings.TrimSpace(os.Getenv("BB_DISABLE_UPDATE")); envVal == "1" || strings.EqualFold(envVal, "true") {
		return true, "self-update is disabled by the BB_DISABLE_UPDATE environment variable; unset it to re-enable self-update, or update bb using your system package manager", nil
	}
	return false, "", nil
}

// disableUpdatePolicyOrigin names the policy source that set disable_update.
//
// Policy is merged from the system configuration file and, on Windows, the
// registry, so the origin is resolved by asking which of the two actually
// carries the setting rather than by tracking provenance through the merge.
func disableUpdatePolicyOrigin() string {
	path, err := SystemConfigPath()
	if err != nil {
		path = ""
	}
	return policyOriginDescription(loadPlatformPolicy().DisableUpdate, platformPolicyDescription(), path)
}

// policyOriginDescription names where a boolean policy setting came from,
// given the value the platform-native store holds for it.
//
// It takes its inputs rather than reading them so that every branch is
// reachable from a test on any OS. Resolved in place, the registry branch is
// dead code on Linux and macOS and needs a registry write on Windows, which is
// how a message nobody can exercise ends up asserting nothing.
func policyOriginDescription(platformValue *bool, platformDescription, systemConfigPath string) string {
	if platformValue != nil && *platformValue && platformDescription != "" {
		return platformDescription
	}
	if strings.TrimSpace(systemConfigPath) == "" {
		return "the system configuration file"
	}
	return "the system configuration file " + systemConfigPath
}

// matchStoredHost finds the stored profile that genuinely corresponds to
// runtimeURL: an exact host match, a configured alias of that host, or the same
// host under the other scheme. It never guesses.
//
// This is deliberately separate from the default-host fallback below. Callers
// that hand credentials to another program must be able to ask "do I have
// credentials for *this* host" and get a truthful no.
func matchStoredHost(stored StoredConfig, runtimeURL string) (string, StoredProfile, bool) {
	key := hostKey(runtimeURL)
	if profile, ok := stored.Hosts[key]; ok {
		return key, profile, true
	}

	if matched, found, _ := resolveStoredHostAlias(stored, runtimeURL); found {
		aliasKey := hostKey(matched.Host)
		if profile, ok := stored.Hosts[aliasKey]; ok {
			return aliasKey, profile, true
		}
	}

	// Cross-scheme fallback: try alternate scheme (http↔https) for same host.
	// This lets tokens configured for https://host match http://host and vice versa.
	if altKey := hostKeyAltScheme(runtimeURL); altKey != key {
		if profile, ok := stored.Hosts[altKey]; ok {
			return altKey, profile, true
		}
	}

	return "", StoredProfile{}, false
}

func resolveStoredCredentials(stored StoredConfig, runtimeURL string) (AppConfig, bool) {
	if len(stored.Hosts) == 0 {
		return AppConfig{}, false
	}

	key, profile, ok := matchStoredHost(stored, runtimeURL)
	if !ok {
		// No host matched, so fall back to the configured default. This is what
		// makes `bb pr list` work against the active server without repeating
		// --host, and it is why callers that pass credentials to other programs
		// must use resolveStoredCredentialsStrict instead.
		if stored.DefaultHost == "" {
			return AppConfig{}, false
		}
		profile, ok = stored.Hosts[stored.DefaultHost]
		if !ok {
			return AppConfig{}, false
		}
		key = stored.DefaultHost
	}

	return credentialsForStoredHost(stored, key, profile), true
}

// resolveStoredCredentialsStrict resolves credentials only for a host that is
// actually configured, with no default-host fallback.
func resolveStoredCredentialsStrict(stored StoredConfig, runtimeURL string) (AppConfig, bool) {
	if len(stored.Hosts) == 0 {
		return AppConfig{}, false
	}

	key, profile, ok := matchStoredHost(stored, runtimeURL)
	if !ok {
		return AppConfig{}, false
	}

	return credentialsForStoredHost(stored, key, profile), true
}

func credentialsForStoredHost(stored StoredConfig, key string, profile StoredProfile) AppConfig {
	resolved := AppConfig{
		BitbucketURL:      normalizeURL(profile.URL),
		BitbucketUsername: profile.Username,
		ClientCertFile:    profile.ClientCert,
		ClientKeyFile:     profile.ClientKey,
	}

	if token, err := keyringGet(keyringServiceName, key+":token"); err == nil && strings.TrimSpace(token) != "" {
		resolved.BitbucketToken = token
	}
	if password, err := keyringGet(keyringServiceName, key+":password"); err == nil && strings.TrimSpace(password) != "" {
		resolved.BitbucketPassword = password
	}

	if resolved.BitbucketToken == "" || resolved.BitbucketPassword == "" {
		if insecure, ok := stored.InsecureSecrets[key]; ok {
			// Flag only when a plaintext secret is actually adopted. A stale
			// entry left beside a working keyring must not make every command
			// report insecure storage.
			if resolved.BitbucketToken == "" && strings.TrimSpace(insecure.Token) != "" {
				resolved.BitbucketToken = insecure.Token
				resolved.UsedInsecureStorage = true
			}
			if resolved.BitbucketPassword == "" && strings.TrimSpace(insecure.Password) != "" {
				resolved.BitbucketPassword = insecure.Password
				resolved.UsedInsecureStorage = true
			}
		}
	}

	return resolved
}

// The keyring surface is indirected so tests can exercise the
// keyring-unavailable path — the whole point of the fallback and of
// BB_REQUIRE_KEYRING — without depending on the machine they run on.
//
// go-keyring ships a mock, but it swaps a package-level provider with no way to
// restore the real one, which would make the behaviour of every later test in
// the binary depend on ordering. These are swapped and restored per test
// instead.
var (
	keyringSet    = keyring.Set
	keyringGet    = keyring.Get
	keyringDelete = keyring.Delete
)

var policyWarningWriter io.Writer

func getPolicyWarningWriter() io.Writer {
	if policyWarningWriter != nil {
		return policyWarningWriter
	}
	return os.Stderr
}

// SetPolicyWarningWriter sets an explicit destination for administrative policy warnings.
// Passing nil restores writing to os.Stderr.
func SetPolicyWarningWriter(w io.Writer) {
	policyWarningWriter = w
}

// RequireKeyring reports whether the operator has mandated keyring-backed
// credential storage via BB_REQUIRE_KEYRING or administrative policy.
func RequireKeyring() (bool, error) {
	return requireKeyringPolicy(false)
}

func requireKeyringPolicy(requestedByFlag bool) (bool, error) {
	policy, err := LoadPolicy()
	if err != nil {
		return false, err
	}
	if policy.RequireKeyring != nil && *policy.RequireKeyring {
		if raw := strings.TrimSpace(os.Getenv("BB_REQUIRE_KEYRING")); raw != "" {
			if b, parseErr := strconv.ParseBool(raw); parseErr == nil && !b {
				fmt.Fprintf(getPolicyWarningWriter(), "warning: BB_REQUIRE_KEYRING=%s is ignored; keyring-backed storage is mandated by administrative policy\n", raw)
			}
		}
		return true, nil
	}

	fromEnv, err := envBoolOrDefault("BB_REQUIRE_KEYRING", false)
	if err != nil {
		return false, apperrors.New(apperrors.KindValidation, "BB_REQUIRE_KEYRING must be a boolean", err)
	}

	return requestedByFlag || fromEnv, nil
}

// keyringUnavailableError reports a mandated keyring that could not be used.
//
// Classified permanent rather than transient: retrying the same command on the
// same host changes nothing, and a caller should surface it rather than loop.
func keyringUnavailableError(cause error) error {
	policy, _ := LoadPolicy()
	if policy.RequireKeyring != nil && *policy.RequireKeyring {
		return apperrors.New(
			apperrors.KindPermanent,
			"OS keyring is unavailable and keyring-backed storage is required by administrative policy; supply credentials through BITBUCKET_TOKEN instead of storing them",
			cause,
		)
	}

	return apperrors.New(
		apperrors.KindPermanent,
		"OS keyring is unavailable and keyring-backed storage is required; unset BB_REQUIRE_KEYRING or drop --require-keyring to allow the plaintext config fallback, or supply credentials through BITBUCKET_TOKEN instead of storing them",
		cause,
	)
}

func LoadStoredAuthForHost(runtimeURL string) (AppConfig, bool, error) {
	stored, err := LoadStoredConfig()
	if err != nil {
		return AppConfig{}, false, err
	}

	resolved, ok := resolveStoredCredentials(stored, runtimeURL)
	return resolved, ok, nil
}

// LoadStoredAuthForHostStrict resolves credentials for exactly the given host,
// with no fallback to the configured default.
//
// Use this whenever the credentials are about to be handed to another program.
// LoadStoredAuthForHost answers "which server should bb talk to", and will
// happily return the default server's credentials for a host it has never seen
// — correct for bb's own commands, and a credential leak for anything that
// passes the result outward.
func LoadStoredAuthForHostStrict(runtimeURL string) (AppConfig, bool, error) {
	stored, err := LoadStoredConfig()
	if err != nil {
		return AppConfig{}, false, err
	}

	resolved, ok := resolveStoredCredentialsStrict(stored, runtimeURL)
	return resolved, ok, nil
}

func (config AppConfig) Validate() error {
	parsedURL, err := url.Parse(config.BitbucketURL)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return apperrors.New(
			apperrors.KindValidation,
			fmt.Sprintf("BITBUCKET_URL is invalid: %q", config.BitbucketURL),
			err,
		)
	}

	projectKey := strings.TrimSpace(config.ProjectKey)
	if projectKey == "" {
		return apperrors.New(apperrors.KindValidation, "BITBUCKET_PROJECT_KEY cannot be empty", nil)
	}

	if len(projectKey) > 20 {
		return apperrors.New(apperrors.KindValidation, "BITBUCKET_PROJECT_KEY cannot exceed 20 characters", nil)
	}

	if config.RequestTimeout <= 0 {
		return apperrors.New(apperrors.KindValidation, config.nameFor(settingRequestTimeout)+" must be greater than 0", nil)
	}

	if config.RetryCount < 0 {
		return apperrors.New(apperrors.KindValidation, config.nameFor(settingRetryCount)+" must be greater than or equal to 0", nil)
	}

	if config.RetryBackoff <= 0 {
		return apperrors.New(apperrors.KindValidation, config.nameFor(settingRetryBackoff)+" must be greater than 0", nil)
	}

	levelToValidate := strings.TrimSpace(config.LogLevel)
	if levelToValidate == "" {
		levelToValidate = defaultLogLevel
	}
	if _, err := diagnostics.ParseLevel(levelToValidate); err != nil {
		return apperrors.New(apperrors.KindValidation, "BB_LOG_LEVEL must be one of: error,warn,info,debug", err)
	}

	formatToValidate := strings.TrimSpace(config.LogFormat)
	if formatToValidate == "" {
		formatToValidate = defaultLogFormat
	}
	if _, err := diagnostics.ParseFormat(formatToValidate); err != nil {
		return apperrors.New(apperrors.KindValidation, "BB_LOG_FORMAT must be one of: text,jsonl", err)
	}

	if config.CAFile != "" {
		info, err := os.Stat(config.CAFile)
		if err != nil {
			return apperrors.New(apperrors.KindValidation, fmt.Sprintf("%s is invalid: %q", config.nameFor(settingCAFile), config.CAFile), err)
		}
		if info.IsDir() {
			return apperrors.New(apperrors.KindValidation, config.nameFor(settingCAFile)+" must be a file path", nil)
		}
	}

	if config.ClientCertFile != "" || config.ClientKeyFile != "" {
		if config.ClientCertFile == "" || config.ClientKeyFile == "" {
			return apperrors.New(apperrors.KindValidation, config.nameFor(settingClientCert)+" and "+config.nameFor(settingClientKey)+" must be set together", nil)
		}
		certInfo, err := os.Stat(config.ClientCertFile)
		if err != nil {
			return apperrors.New(apperrors.KindValidation, fmt.Sprintf("%s is invalid: %q", config.nameFor(settingClientCert), config.ClientCertFile), err)
		}
		if certInfo.IsDir() {
			return apperrors.New(apperrors.KindValidation, config.nameFor(settingClientCert)+" must be a file path", nil)
		}
		keyInfo, err := os.Stat(config.ClientKeyFile)
		if err != nil {
			return apperrors.New(apperrors.KindValidation, fmt.Sprintf("%s is invalid: %q", config.nameFor(settingClientKey), config.ClientKeyFile), err)
		}
		if keyInfo.IsDir() {
			return apperrors.New(apperrors.KindValidation, config.nameFor(settingClientKey)+" must be a file path", nil)
		}
	}

	if config.BitbucketToken == "" && (config.BitbucketUsername == "") != (config.BitbucketPassword == "") {
		return apperrors.New(
			apperrors.KindValidation,
			"BITBUCKET_USERNAME and BITBUCKET_PASSWORD must be set together",
			nil,
		)
	}

	return nil
}

func (config AppConfig) AuthMode() string {
	if config.BitbucketToken != "" {
		return "token"
	}

	if config.BitbucketUsername != "" && config.BitbucketPassword != "" {
		return "basic"
	}

	return "none"
}

// CredentialStorage names where the credential in use is held, so an operator
// auditing a machine can see it without grepping the config file.
func (config AppConfig) CredentialStorage() string {
	if config.AuthMode() == "none" {
		return "none"
	}

	// Plaintext wins over the AuthSource label. AuthSource only records that the
	// environment supplied something; if a secret was genuinely adopted from the
	// config file, saying "environment" would hide the exposure being reported.
	switch {
	case config.UsedInsecureStorage:
		return "config-file-plaintext"
	case config.AuthSource == "env" || config.AuthSource == "env/default":
		return "environment"
	default:
		return "keyring"
	}
}

func envOrDefault(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	return value
}

func normalizeURL(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return trimmed
	}

	if strings.Contains(trimmed, "://") {
		return trimmed
	}

	return "https://" + trimmed
}

func normalizeAlias(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", apperrors.New(apperrors.KindValidation, "alias is required", nil)
	}

	if strings.HasPrefix(trimmed, "git@") {
		at := strings.LastIndex(trimmed, "@")
		colon := strings.Index(trimmed[at+1:], ":")
		if at >= 0 && colon >= 0 {
			host := strings.TrimSpace(trimmed[at+1 : at+1+colon])
			if host != "" {
				return strings.ToLower(host + ":22"), nil
			}
		}
	}

	parseTarget := trimmed
	if !strings.Contains(parseTarget, "://") {
		parseTarget = "https://" + parseTarget
	}

	parsed, err := url.Parse(parseTarget)
	if err != nil || strings.TrimSpace(parsed.Hostname()) == "" {
		return "", apperrors.New(apperrors.KindValidation, fmt.Sprintf("alias %q is invalid", value), err)
	}

	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	port := parsed.Port()
	if port == "" {
		switch strings.ToLower(parsed.Scheme) {
		case "http":
			port = "80"
		case "ssh":
			port = "22"
		default:
			port = "443"
		}
	}

	return host + ":" + port, nil
}

func normalizeAliases(values []string) ([]string, error) {
	if len(values) == 0 {
		return []string{}, nil
	}

	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		normalized, err := normalizeAlias(value)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}

	return result, nil
}

func normalizeStoredAliases(values []string) []string {
	normalized, err := normalizeAliases(values)
	if err != nil {
		return []string{}
	}
	return normalized
}

func ensureAliasOwnership(stored StoredConfig, ownerKey string, aliases []string) error {
	for key, profile := range stored.Hosts {
		if key == ownerKey {
			continue
		}
		for _, existing := range normalizeStoredAliases(profile.Aliases) {
			for _, alias := range aliases {
				if existing == alias {
					return apperrors.New(apperrors.KindConflict, fmt.Sprintf("alias %s is already configured for %s", alias, normalizeURL(profile.URL)), nil)
				}
			}
		}
	}

	return nil
}

func resolveStoredHostAlias(stored StoredConfig, runtimeURL string) (AliasMatch, bool, error) {
	normalizedRuntime, err := normalizeAlias(runtimeURL)
	if err != nil {
		return AliasMatch{}, false, nil
	}

	for _, profile := range stored.Hosts {
		for _, alias := range normalizeStoredAliases(profile.Aliases) {
			if alias == normalizedRuntime {
				return AliasMatch{Host: normalizeURL(profile.URL), Endpoint: alias}, true, nil
			}
		}
	}

	return AliasMatch{}, false, nil
}

func hostKey(hostURL string) string {
	parsed, err := url.Parse(normalizeURL(hostURL))
	if err != nil {
		return normalizeURL(hostURL)
	}

	return strings.ToLower(parsed.Scheme + "://" + parsed.Host)
}

// hostKeyAltScheme returns the hostKey with the opposite scheme (http↔https).
// Returns an empty string when the URL is not parseable.
func hostKeyAltScheme(hostURL string) string {
	parsed, err := url.Parse(normalizeURL(hostURL))
	if err != nil || parsed.Host == "" {
		return ""
	}

	altScheme := "https"
	if strings.ToLower(parsed.Scheme) == "https" {
		altScheme = "http"
	}

	return altScheme + "://" + strings.ToLower(parsed.Host)
}

func envBoolOrDefault(key string, fallback bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, err
	}

	return parsed, nil
}

func envIntOrDefault(key string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, err
	}

	return parsed, nil
}

func envDurationOrDefault(key string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, err
	}

	return parsed, nil
}

// mergeAliases appends the additions that are not already present, preserving
// the order of both.
func mergeAliases(existing []string, additions []string) []string {
	merged := append([]string{}, existing...)
	seen := make(map[string]struct{}, len(merged))
	for _, alias := range merged {
		seen[alias] = struct{}{}
	}

	for _, alias := range additions {
		if _, present := seen[alias]; present {
			continue
		}
		seen[alias] = struct{}{}
		merged = append(merged, alias)
	}

	return merged
}

// nameOf reports which spelling to blame for a setting: the flag when the
// value came from one, the environment variable otherwise.
//
// A flag written into BB_* loses its origin, and validation then reported the
// variable: `bb --retry-count -5` said "BB_RETRY_COUNT must be greater than or
// equal to 0", sending the user to look for something they never set. ADR-054
// asks for a message naming the flag; this is what lets one exist.
func nameOf(sourced map[string]bool, setting runtimeSetting) string {
	if sourced[setting.environment] {
		return setting.flag
	}
	return setting.environment
}

// resolveString picks a setting's value, recording a flag as its source.
//
// An override outranks the environment for this invocation only. Writing the
// flag into the variable, as this used to, destroyed the user's own value
// rather than outranking it, and left nothing able to tell the two apart.
func resolveString(sourced map[string]bool, setting runtimeSetting, override *string) string {
	if override != nil {
		sourced[setting.environment] = true
		return strings.TrimSpace(*override)
	}
	return strings.TrimSpace(os.Getenv(setting.environment))
}

// resolveDuration is resolveString for a duration, falling back to a default.
func resolveDuration(sourced map[string]bool, setting runtimeSetting, override *string, fallback time.Duration) (time.Duration, error) {
	if override == nil {
		return envDurationOrDefault(setting.environment, fallback)
	}

	sourced[setting.environment] = true
	raw := strings.TrimSpace(*override)
	if raw == "" {
		return fallback, nil
	}
	return time.ParseDuration(raw)
}

// resolveInt is resolveString for an integer, falling back to a default.
func resolveInt(sourced map[string]bool, setting runtimeSetting, override *int, fallback int) (int, error) {
	if override == nil {
		return envIntOrDefault(setting.environment, fallback)
	}

	sourced[setting.environment] = true
	return *override, nil
}

// resolveBool is resolveString for a boolean, falling back to a default.
func resolveBool(sourced map[string]bool, setting runtimeSetting, override *bool, fallback bool) (bool, error) {
	if override == nil {
		return envBoolOrDefault(setting.environment, fallback)
	}

	sourced[setting.environment] = true
	return *override, nil
}

// ResolveRequestTimeoutWith resolves the HTTP request timeout, honouring a
// --request-timeout flag ahead of BB_REQUEST_TIMEOUT.
//
// It exists for the same reason as ResolveTLSSettingsWith: bb update runs
// without a configured host, so it cannot go through LoadWithOverrides, and
// parsing the variable itself meant the flag stopped reaching it once flags
// became values.
func ResolveRequestTimeoutWith(overrides Overrides, fallback time.Duration) (time.Duration, error) {
	sourced := map[string]bool{}

	timeout, err := resolveDuration(sourced, settingRequestTimeout, overrides.RequestTimeout, fallback)
	if err != nil {
		return 0, apperrors.New(apperrors.KindValidation,
			nameOf(sourced, settingRequestTimeout)+" must be a valid duration (example: 20s)", err)
	}
	if timeout <= 0 {
		return 0, apperrors.New(apperrors.KindValidation,
			nameOf(sourced, settingRequestTimeout)+" must be greater than 0", nil)
	}
	return timeout, nil
}
