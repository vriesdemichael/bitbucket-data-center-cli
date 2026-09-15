package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/diagnostics"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/zalando/go-keyring"
	"gopkg.in/yaml.v3"
)

// The tiers a configuration file belongs to. A file is also the kind of source
// a setting read from it reports.
const (
	TierStored    = "stored"
	TierWorkspace = "workspace"
	TierSystem    = "system"
)

// The other kinds of place a setting can come from.
const (
	SourceFlag        = "flag"
	SourceEnvironment = "environment"
	SourceDotenv      = "dotenv"
	SourceOverride    = "override"
	SourceKeyring     = "keyring"
	SourceRegistry    = "registry"
	SourceDefault     = "default"
)

// SourceKinds lists every kind a SettingSource can have.
func SourceKinds() []string {
	return []string{
		SourceFlag, SourceEnvironment, SourceDotenv, SourceOverride,
		TierWorkspace, TierStored, TierSystem, SourceRegistry, SourceKeyring, SourceDefault,
	}
}

// registryPolicyLocation is the key loadPlatformPolicy reads on Windows.
const registryPolicyLocation = `HKEY_LOCAL_MACHINE\Software\Policies\bb`

// DiagnoseInput is what the invocation contributes to a diagnosis.
type DiagnoseInput struct {
	// Overrides are the values the global flags supplied.
	Overrides Overrides
	// ChangedFlags names the global flags that were passed and still travel
	// through the environment rather than Overrides: log-level and log-format.
	ChangedFlags map[string]bool
}

// Diagnosis is what bb doctor found in the configuration bb would load.
type Diagnosis struct {
	Files []DiagnosedFile
	// Settings are resolved from every file that parses, including one the
	// schema rejects. bb itself runs no command while a file is invalid, so
	// these are what it would use once the files are repaired.
	Settings []DiagnosedSetting
	Keyring  KeyringDiagnosis
}

// DiagnosedFile is one configuration file and what is wrong with it.
type DiagnosedFile struct {
	Tier string
	// Path is empty when there is no file to name: no workspace file was found,
	// or the stored file's location could not be worked out.
	Path string
	// PathFrom names what chose the path: the variable that set it, "default",
	// "search" for a workspace file found above the working directory, or
	// "machine" for the fixed system location.
	PathFrom string
	// Read is false when bb does not read the file at all, and NotRead says why.
	Read    bool
	NotRead string
	Exists  bool
	// Parses and MatchesSchema are false for a file that is not there.
	Parses        bool
	MatchesSchema bool
	// Problem is a failure that stopped the file being checked further: it could
	// not be read, or it is not YAML.
	Problem    string
	Violations []SchemaViolation
	// Ignored are keys the schema accepts that bb does not read from this tier.
	Ignored []IgnoredKey
	// Secrets says which hosts have a plaintext credential in the file.
	Secrets []SecretPresence
}

// Valid reports whether bb would load the file: one it does not read, or that
// is not there, is no obstacle.
func (file DiagnosedFile) Valid() bool {
	return !file.Read || (file.Problem == "" && len(file.Violations) == 0)
}

// IgnoredKey is a key that means something in another tier's file.
type IgnoredKey struct {
	Key      string
	Line     int
	ReadFrom []string
}

// SecretPresence says whether a plaintext token or password is held for a host,
// and never what it is.
type SecretPresence struct {
	Host     string
	Token    bool
	Password bool
}

// SettingSource is where a setting's value came from.
type SettingSource struct {
	// Kind is one of SourceKinds.
	Kind string
	// Name is the flag, variable, key within a file, keyring entry or registry
	// value that carries the setting.
	Name string
	// Path is the file, .env file or registry key it was found in.
	Path string
}

// DiagnosedSetting is one effective setting and where it came from.
type DiagnosedSetting struct {
	Name string
	// Value is empty for a secret, whatever it holds.
	Value      string
	Secret     bool
	Configured bool
	Source     SettingSource
	// Shadowed are the other places that set it and lost to Source.
	Shadowed []SettingSource
	// Problem is what a command loading this setting would refuse.
	Problem string
}

// KeyringDiagnosis reports on the OS keyring when keyring-backed storage is
// required.
type KeyringDiagnosis struct {
	Required   bool
	RequiredBy SettingSource
	Checked    bool
	Reachable  bool
	Problem    string
}

// Diagnose checks the configuration bb would load, reading each file on its own
// so that every problem is reported rather than the first.
//
// It needs no host and makes no network call. It loads .env the way a
// configuration load does, so the variables it reports are the ones a command
// would see.
func Diagnose(input DiagnoseInput) Diagnosis {
	ambient := environmentNames()
	dotenv := readDotenvFiles()
	loadDotEnv()

	return diagnose(diagnosisInputs{
		getenv:         os.Getenv,
		ambient:        func(name string) bool { return ambient[name] },
		dotenv:         dotenv,
		files:          []fileInput{storedFileInput(), workspaceFileInput(), systemFileInput()},
		platformPolicy: loadPlatformPolicy(),
		flags:          input.Overrides,
		changedFlags:   input.ChangedFlags,
		secrets:        func(host, key string) (string, string) { return storedSecrets(host, key) },
		probeKeyring:   probeKeyring,
	})
}

// diagnosisInputs is everything a diagnosis reads, gathered so that diagnose
// can be exercised without a process environment, a registry or a keyring.
type diagnosisInputs struct {
	// getenv reads the environment after .env has been applied.
	getenv func(string) string
	// ambient reports whether a variable was set before .env was applied.
	ambient        func(string) bool
	dotenv         []dotenvFile
	files          []fileInput
	platformPolicy PolicyConfig
	flags          Overrides
	changedFlags   map[string]bool
	secrets        func(host, key string) (token, password string)
	probeKeyring   func() error
}

type dotenvFile struct {
	path   string
	values map[string]string
}

type fileInput struct {
	tier     string
	path     string
	pathFrom string
	notRead  string
	exists   bool
	raw      []byte
	readErr  error
}

func environmentNames() map[string]bool {
	names := map[string]bool{}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		names[name] = true
	}

	return names
}

// readDotenvFiles reads the files loadDotEnv applies, nearest first. godotenv
// fills in only names the environment does not already carry, so the nearest
// file that names a variable is the one that supplied it.
func readDotenvFiles() []dotenvFile {
	files := []dotenvFile{}
	for _, candidate := range dotenvCandidates() {
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		values, err := godotenv.Read(candidate)
		if err != nil {
			continue
		}
		files = append(files, dotenvFile{path: candidate, values: values})
	}

	return files
}

func pathFrom(variable, fallback string) string {
	if strings.TrimSpace(os.Getenv(variable)) != "" {
		return variable
	}

	return fallback
}

func storedFileInput() fileInput {
	input := fileInput{tier: TierStored, pathFrom: pathFrom("BB_CONFIG_PATH", "default")}

	path, err := ConfigPath()
	if err != nil {
		input.notRead = "its location could not be worked out: " + err.Error()
		return input
	}
	input.path = path

	if os.Getenv("BB_DISABLE_STORED_CONFIG") == "1" {
		input.notRead = "BB_DISABLE_STORED_CONFIG=1"
		_, statErr := os.Stat(path)
		input.exists = statErr == nil
		return input
	}

	return readFileInput(input)
}

func workspaceFileInput() fileInput {
	input := fileInput{tier: TierWorkspace, pathFrom: pathFrom("BB_WORKSPACE_CONFIG_PATH", "search")}

	path, err := WorkspaceConfigPath()
	if err != nil {
		input.readErr = err
		return input
	}
	if path == "" {
		return input
	}
	input.path = path

	return readFileInput(input)
}

func systemFileInput() fileInput {
	input := fileInput{tier: TierSystem, pathFrom: "machine"}
	if testing.Testing() && strings.TrimSpace(os.Getenv("BB_SYSTEM_CONFIG_PATH")) != "" {
		input.pathFrom = "BB_SYSTEM_CONFIG_PATH"
	}

	// SystemConfigPath has no failure of its own to report.
	input.path, _ = SystemConfigPath()

	return readFileInput(input)
}

func readFileInput(input fileInput) fileInput {
	raw, err := os.ReadFile(input.path)
	switch {
	case err == nil:
		input.exists, input.raw = true, raw
	case os.IsNotExist(err):
	default:
		input.exists, input.readErr = true, err
	}

	return input
}

// probeKeyring asks the credential store for an entry bb never writes.
//
// Not found is the answer a reachable store gives, and anything else means bb
// could not talk to it. A read rather than a write, because bb doctor changes
// nothing; where the store is locked, the read asks for the unlock a login
// would.
func probeKeyring() error {
	_, err := keyringGet(keyringServiceName, "bb-doctor:probe")
	if err == nil || errors.Is(err, keyring.ErrNotFound) {
		return nil
	}

	return err
}

func diagnose(in diagnosisInputs) Diagnosis {
	var (
		stored    StoredConfig
		workspace WorkspaceConfigFile
		system    SystemConfigFile
	)
	destinations := map[string]any{TierStored: &stored, TierWorkspace: &workspace, TierSystem: &system}

	diagnosis := Diagnosis{}
	paths := map[string]string{}
	for _, input := range in.files {
		diagnosis.Files = append(diagnosis.Files, inspectConfigFile(input, destinations[input.tier]))
		paths[input.tier] = input.path
	}

	resolved := resolution{
		in:         in,
		paths:      paths,
		stored:     stored,
		workspace:  workspace,
		system:     system,
		policy:     effectivePolicy(system, in.platformPolicy),
		storedRead: in.getenv("BB_DISABLE_STORED_CONFIG") != "1",
	}
	diagnosis.Settings = resolved.settings()

	for _, setting := range diagnosis.Settings {
		if setting.Name == "require_keyring" {
			diagnosis.Keyring = resolved.keyring(setting)
		}
	}

	return diagnosis
}

func inspectConfigFile(input fileInput, into any) DiagnosedFile {
	file := DiagnosedFile{
		Tier:       input.tier,
		Path:       input.path,
		PathFrom:   input.pathFrom,
		Read:       input.notRead == "",
		NotRead:    input.notRead,
		Exists:     input.exists,
		Violations: []SchemaViolation{},
		Ignored:    []IgnoredKey{},
		Secrets:    []SecretPresence{},
	}

	if input.readErr != nil {
		file.Problem = "could not be read: " + input.readErr.Error()
		return file
	}
	if !file.Read || !file.Exists {
		return file
	}

	data, err := configDocument(input.raw)
	if err != nil {
		file.Problem = apperrors.MessageOf(err)
		return file
	}
	file.Parses = true

	// Already known to parse; the node tree only places keys on lines.
	var document yaml.Node
	_ = yaml.Unmarshal(input.raw, &document)

	if data != nil {
		file.Violations = configSchemaViolations(data, &document)
	}
	file.MatchesSchema = len(file.Violations) == 0
	file.Ignored = ignoredKeys(input.tier, &document)

	if err := yaml.Unmarshal(input.raw, into); err != nil {
		if file.MatchesSchema {
			file.Problem = "does not decode: " + err.Error()
		}
		return file
	}
	file.Secrets = secretPresence(into)

	return file
}

// tierReadKeys are the top-level keys each tier's loader decodes and uses.
//
// Taken from the structs the loaders decode into, so a key added to one is
// counted here without a second list to keep. The system file decodes a
// project_key that nothing uses: LoadWithOverrides takes one from the workspace
// file only.
var tierReadKeys = sync.OnceValue(func() map[string]map[string]bool {
	system := yamlKeysOf(SystemConfigFile{})
	delete(system, "project_key")

	return map[string]map[string]bool{
		TierStored:    yamlKeysOf(StoredConfig{}),
		TierWorkspace: yamlKeysOf(WorkspaceConfigFile{}),
		TierSystem:    system,
	}
})

var schemaTopLevelKeys = sync.OnceValue(func() map[string]bool {
	keys := map[string]bool{}
	properties, _ := ConfigJSONSchema()["properties"].(map[string]any)
	for key := range properties {
		keys[key] = true
	}

	return keys
})

func yamlKeysOf(value any) map[string]bool {
	// $schema is for an editor, and every tier accepts it.
	keys := map[string]bool{"$schema": true}

	typeOf := reflect.TypeOf(value)
	for index := 0; index < typeOf.NumField(); index++ {
		name, _, _ := strings.Cut(typeOf.Field(index).Tag.Get("yaml"), ",")
		if name != "" && name != "-" {
			keys[name] = true
		}
	}

	return keys
}

// ignoredKeys finds the keys the schema accepts that this tier's loader never
// reads. They are valid, so a load says nothing about them, and a require_keyring
// in the user's own file silently mandates nothing.
func ignoredKeys(tier string, document *yaml.Node) []IgnoredKey {
	ignored := []IgnoredKey{}

	root := document
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return ignored
	}

	reads := tierReadKeys()
	for index := 0; index+1 < len(root.Content); index += 2 {
		key := root.Content[index].Value
		if !schemaTopLevelKeys()[key] || reads[tier][key] {
			continue
		}

		readFrom := []string{}
		for _, other := range []string{TierWorkspace, TierStored, TierSystem} {
			if reads[other][key] {
				readFrom = append(readFrom, other)
			}
		}
		ignored = append(ignored, IgnoredKey{Key: key, Line: root.Content[index].Line, ReadFrom: readFrom})
	}

	return ignored
}

func secretPresence(decoded any) []SecretPresence {
	var secrets map[string]StoredSecret
	switch file := decoded.(type) {
	case *StoredConfig:
		secrets = file.InsecureSecrets
	case *SystemConfigFile:
		secrets = file.InsecureSecrets
	}

	presence := []SecretPresence{}
	for host, secret := range secrets {
		token, password := strings.TrimSpace(secret.Token) != "", strings.TrimSpace(secret.Password) != ""
		if token || password {
			presence = append(presence, SecretPresence{Host: host, Token: token, Password: password})
		}
	}
	sort.Slice(presence, func(i, j int) bool { return presence[i].Host < presence[j].Host })

	return presence
}

// effectivePolicy merges policy the way LoadPolicy does: the system file's top
// level, then its policies block, then its policy block, then the registry, each
// replacing what came before.
func effectivePolicy(system SystemConfigFile, platform PolicyConfig) PolicyConfig {
	policy := system.PolicyConfig()
	if system.Policies != nil {
		mergePolicy(&policy, *system.Policies)
	}
	if system.Policy != nil {
		mergePolicy(&policy, *system.Policy)
	}
	mergePolicy(&policy, platform)

	return policy
}

// resolution works out each setting from the decoded files, following the
// precedence of the code that reads it.
type resolution struct {
	in         diagnosisInputs
	paths      map[string]string
	stored     StoredConfig
	workspace  WorkspaceConfigFile
	system     SystemConfigFile
	policy     PolicyConfig
	storedRead bool
}

// settingCandidate is one place that sets a setting.
type settingCandidate struct {
	source SettingSource
	value  string
	// ineligible marks a value that is set and not consulted -- a variable a
	// flag replaced -- so it can be reported as shadowed and never win.
	ineligible bool
}

// resolveSetting takes the first eligible candidate, strongest first, and
// reports every other one as shadowed.
func resolveSetting(name, fallback string, candidates ...settingCandidate) DiagnosedSetting {
	setting := DiagnosedSetting{
		Name:     name,
		Value:    fallback,
		Source:   SettingSource{Kind: SourceDefault},
		Shadowed: []SettingSource{},
	}

	won := false
	for _, candidate := range candidates {
		if !won && !candidate.ineligible {
			setting.Value, setting.Source, setting.Configured, won = candidate.value, candidate.source, true, true
			continue
		}
		setting.Shadowed = append(setting.Shadowed, candidate.source)
	}

	return setting
}

// fromEnvironment is the candidate a variable supplies, from the process or
// from a .env file.
func (in diagnosisInputs) fromEnvironment(name string) []settingCandidate {
	value := strings.TrimSpace(in.getenv(name))
	if value == "" {
		return nil
	}

	source := SettingSource{Kind: SourceEnvironment, Name: name}
	if !in.ambient(name) {
		source = SettingSource{Kind: SourceDotenv, Name: name, Path: in.dotenvFileFor(name)}
	}

	return []settingCandidate{{source: source, value: value}}
}

// fromProcessEnvironment is fromEnvironment for a setting read by bb update,
// which never loads .env.
func (in diagnosisInputs) fromProcessEnvironment(name string) []settingCandidate {
	if !in.ambient(name) {
		return nil
	}

	return in.fromEnvironment(name)
}

func (in diagnosisInputs) dotenvFileFor(name string) string {
	for _, file := range in.dotenv {
		if _, ok := file.values[name]; ok {
			return file.path
		}
	}

	return ""
}

func (r resolution) fileSource(tier, key string) SettingSource {
	return SettingSource{Kind: tier, Name: key, Path: r.paths[tier]}
}

func (r resolution) fromFile(tier, key, value string) []settingCandidate {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	return []settingCandidate{{source: r.fileSource(tier, key), value: value}}
}

func overrideCandidate(name, value string) []settingCandidate {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	return []settingCandidate{{source: SettingSource{Kind: SourceOverride, Name: name}, value: strings.TrimSpace(value)}}
}

// flagOver puts a passed flag ahead of the variable it replaces. A flag that
// was passed, even empty, means the variable is not consulted at all.
func flagOver(passed bool, flag, variable []settingCandidate) []settingCandidate {
	if !passed {
		return variable
	}
	for index := range variable {
		variable[index].ineligible = true
	}

	return append(flag, variable...)
}

func (in diagnosisInputs) fromStringFlag(setting runtimeSetting, override *string) []settingCandidate {
	var flag []settingCandidate
	if override != nil && strings.TrimSpace(*override) != "" {
		flag = []settingCandidate{{source: SettingSource{Kind: SourceFlag, Name: setting.flag}, value: strings.TrimSpace(*override)}}
	}

	return flagOver(override != nil, flag, in.fromEnvironment(setting.environment))
}

// mandateOver orders a policy that mandates something ahead of the variable,
// and the variable ahead of a policy that does not: requireKeyringPolicy and
// IsUpdateDisabled honour policy only when it says true.
func mandateOver(policy, variable []settingCandidate) []settingCandidate {
	if len(policy) == 0 || policy[0].value != "true" {
		return append(append([]settingCandidate{}, variable...), policy...)
	}
	for index := range variable {
		variable[index].ineligible = true
	}

	ordered := []settingCandidate{policy[0]}
	ordered = append(ordered, variable...)

	return append(ordered, policy[1:]...)
}

func (r resolution) settings() []DiagnosedSetting {
	host := r.host()
	requireKeyring := r.requireKeyring()

	settings := []DiagnosedSetting{host, r.projectKey()}
	settings = append(settings, r.credentials(host.Value, requireKeyring.Value == "true")...)
	settings = append(settings, r.caFile(), r.insecureSkipVerify())
	settings = append(settings, r.clientFiles(host.Value)...)
	settings = append(settings,
		r.duration("request_timeout", settingRequestTimeout, r.in.flags.RequestTimeout, defaultRequestTimeout),
		r.retryCount(),
		r.duration("retry_backoff", settingRetryBackoff, r.in.flags.RetryBackoff, defaultRetryBackoff),
		r.diagnostics("log_level", "BB_LOG_LEVEL", "log-level", defaultLogLevel, "error,warn,info,debug", func(value string) error {
			_, err := diagnostics.ParseLevel(value)
			return err
		}),
		r.diagnostics("log_format", "BB_LOG_FORMAT", "log-format", defaultLogFormat, "text,jsonl", func(value string) error {
			_, err := diagnostics.ParseFormat(value)
			return err
		}),
		r.updateBaseURL(),
		requireKeyring,
		r.policySetting("allowed_hosts", "AllowedHosts", "", policyList(func(p PolicyConfig) []string { return p.AllowedHosts })),
		r.policySetting("allow_insecure_skip_verify", "AllowInsecureSkipVerify", "true", policyFlag(func(p PolicyConfig) *bool { return p.AllowInsecureSkipVerify })),
		r.disableUpdate(),
		r.policySetting("allow_http_update", "AllowHTTPUpdate", "", policyFlag(func(p PolicyConfig) *bool { return p.AllowHTTPUpdate })),
		// The registry has no value for mcp_audit_file (ADR-058, point 6).
		r.policySetting("mcp_audit_file", "", "", policyText(func(p PolicyConfig) string { return p.MCPAuditFile })),
	)
	settings = append(settings, r.updateTrust()...)

	return settings
}

func (r resolution) host() DiagnosedSetting {
	candidates := overrideCandidate("host", r.in.flags.Host)
	for _, candidate := range r.in.fromEnvironment("BITBUCKET_URL") {
		candidate.value = normalizeURL(candidate.value)
		candidates = append(candidates, candidate)
	}
	candidates = append(candidates, r.defaultHost(TierWorkspace, r.workspace.DefaultHost, r.workspace.Hosts, r.stored.Hosts, r.system.Hosts)...)
	candidates = append(candidates, r.defaultHost(TierStored, r.stored.DefaultHost, r.stored.Hosts, r.system.Hosts, r.workspace.Hosts)...)
	candidates = append(candidates, r.defaultHost(TierSystem, r.system.DefaultHost, r.system.Hosts, r.stored.Hosts, r.workspace.Hosts)...)
	for index := range candidates {
		if candidates[index].source.Kind == SourceOverride {
			candidates[index].value = normalizeURL(candidates[index].value)
		}
	}

	setting := resolveSetting("host", "", candidates...)
	switch {
	case setting.Configured && setting.Value == "":
		setting.Problem = "default_host is neither a URL nor a host any configuration file defines"
	case setting.Value != "" && len(r.policy.AllowedHosts) > 0 && !IsHostAllowed(setting.Value, r.policy.AllowedHosts):
		setting.Problem = "not permitted by the allowed_hosts policy; commands refuse to reach it"
	}

	return setting
}

// defaultHost is a file's default_host, which wins whether or not it resolves:
// LoadWithOverrides stops at the first file that names one.
func (r resolution) defaultHost(tier, defaultHost string, hostMaps ...map[string]StoredProfile) []settingCandidate {
	if defaultHost == "" {
		return nil
	}

	return []settingCandidate{{source: r.fileSource(tier, "default_host"), value: resolveDefaultHostURL(defaultHost, hostMaps...)}}
}

func (r resolution) projectKey() DiagnosedSetting {
	candidates := overrideCandidate("project_key", r.in.flags.ProjectKey)
	candidates = append(candidates, r.in.fromEnvironment("BITBUCKET_PROJECT_KEY")...)
	candidates = append(candidates, r.fromFile(TierWorkspace, "project_key", r.workspace.ProjectKey)...)

	return resolveSetting("project_key", defaultProjectKey, candidates...)
}

// storedProfile finds the host profile LoadWithOverrides takes credentials
// from: the stored file's, then the workspace's, then the system file's.
func (r resolution) storedProfile(host string) (string, string, StoredProfile, map[string]StoredSecret, bool) {
	if host == "" || !r.storedRead {
		return "", "", StoredProfile{}, nil, false
	}

	for _, candidate := range []struct {
		tier   string
		config StoredConfig
	}{
		{TierStored, r.stored},
		{TierWorkspace, StoredConfig{Hosts: r.workspace.Hosts}},
		{TierSystem, r.system.StoredConfig()},
	} {
		if key, profile, found := storedProfileFor(candidate.config, host); found {
			return candidate.tier, key, profile, candidate.config.InsecureSecrets, true
		}
	}

	return "", "", StoredProfile{}, nil, false
}

// credentials reports the username, token and password. The secrets carry no
// value into the diagnosis at all, so nothing downstream can print one.
func (r resolution) credentials(host string, requireKeyring bool) []DiagnosedSetting {
	flags := r.in.flags
	tier, key, profile, insecure, found := r.storedProfile(host)

	var keyringToken, keyringPassword string
	if found {
		keyringToken, keyringPassword = r.in.secrets(profile.URL, key)
	}
	stored := func(fromKeyring string, plaintext string, field string) []settingCandidate {
		switch {
		case !found:
			return nil
		case fromKeyring != "":
			return []settingCandidate{{source: SettingSource{Kind: SourceKeyring, Name: key}}}
		case strings.TrimSpace(plaintext) != "":
			return []settingCandidate{{source: r.fileSource(tier, "insecure_secrets."+key+"."+field)}}
		}
		return nil
	}
	secret := func(candidate []settingCandidate) []settingCandidate {
		for index := range candidate {
			candidate[index].value = ""
		}
		return candidate
	}

	usernames := overrideCandidate("username", flags.Username)
	for _, variable := range []string{"BITBUCKET_USERNAME", "BITBUCKET_USER", "ADMIN_USER"} {
		usernames = append(usernames, r.in.fromEnvironment(variable)...)
	}
	if found {
		usernames = append(usernames, r.fromFile(tier, "hosts."+key+".username", profile.Username)...)
	}

	// Naming a user outranks an ambient token; see suppliedCredentialToken.
	namedUser := strings.TrimSpace(flags.Username) != "" && strings.TrimSpace(flags.Password) != ""
	tokens := secret(overrideCandidate("token", flags.Token))
	for _, candidate := range secret(r.in.fromEnvironment("BITBUCKET_TOKEN")) {
		candidate.ineligible = namedUser
		tokens = append(tokens, candidate)
	}
	tokens = append(tokens, stored(keyringToken, insecure[key].Token, "token")...)

	passwords := secret(overrideCandidate("password", flags.Password))
	passwords = append(passwords, secret(r.in.fromEnvironment("BITBUCKET_PASSWORD"))...)
	passwords = append(passwords, secret(r.in.fromEnvironment("ADMIN_PASSWORD"))...)
	passwords = append(passwords, stored(keyringPassword, insecure[key].Password, "password")...)

	settings := []DiagnosedSetting{
		resolveSetting("username", "", usernames...),
		resolveSetting("token", "", tokens...),
		resolveSetting("password", "", passwords...),
	}
	for index := range settings[1:] {
		setting := &settings[index+1]
		setting.Secret = true
		if requireKeyring && setting.Source.Kind == tier && found {
			setting.Problem = "held in plaintext and keyring-backed storage is required; commands refuse to use it"
		}
	}

	return settings
}

func (r resolution) caFile() DiagnosedSetting {
	user := r.in.fromStringFlag(settingCAFile, r.in.flags.CAFile)
	mandated := r.policyCandidates("ca_file", "CAFile", policyText(func(p PolicyConfig) string { return p.CAFile }))

	setting := resolveSetting("ca_file", "", append(user, mandated...)...)
	switch {
	case len(mandated) > 0 && setting.Source.Kind != mandated[0].source.Kind &&
		filepath.Clean(setting.Value) != filepath.Clean(r.policy.CAFile):
		setting.Problem = fmt.Sprintf("differs from the CA file policy mandates (%s); commands refuse to run", r.policy.CAFile)
	case setting.Value != "":
		setting.Problem = pathProblem(setting.Value)
	}

	return setting
}

func (r resolution) insecureSkipVerify() DiagnosedSetting {
	var flag []settingCandidate
	if value := r.in.flags.InsecureSkipVerify; value != nil {
		flag = []settingCandidate{{source: SettingSource{Kind: SourceFlag, Name: settingInsecureSkipVerify.flag}, value: strconv.FormatBool(*value)}}
	}

	variable := r.in.fromEnvironment(settingInsecureSkipVerify.environment)
	setting := resolveSetting("insecure_skip_verify", "false", flagOver(flag != nil, flag, variable)...)
	if setting.Source.Kind != SourceFlag && setting.Configured {
		parsed, err := strconv.ParseBool(setting.Value)
		if err != nil {
			setting.Problem = settingInsecureSkipVerify.environment + " must be a boolean"
			return setting
		}
		setting.Value = strconv.FormatBool(parsed)
	}
	if setting.Value == "true" && r.policy.AllowInsecureSkipVerify != nil && !*r.policy.AllowInsecureSkipVerify {
		setting.Problem = "the allow_insecure_skip_verify policy forbids it; commands refuse to run"
	}

	return setting
}

func (r resolution) clientFiles(host string) []DiagnosedSetting {
	tier, key, profile, _, found := r.storedProfile(host)

	file := func(name string, setting runtimeSetting, override *string, fromProfile string) DiagnosedSetting {
		candidates := r.in.fromStringFlag(setting, override)
		if found {
			candidates = append(candidates, r.fromFile(tier, "hosts."+key+"."+name, fromProfile)...)
		}
		return resolveSetting(name, "", candidates...)
	}

	cert := file("client_cert", settingClientCert, r.in.flags.ClientCert, profile.ClientCert)
	clientKey := file("client_key", settingClientKey, r.in.flags.ClientKey, profile.ClientKey)
	for _, setting := range []*DiagnosedSetting{&cert, &clientKey} {
		switch {
		case setting.Value == "":
		case cert.Value == "" || clientKey.Value == "":
			setting.Problem = "client_cert and client_key must be set together"
		default:
			setting.Problem = pathProblem(setting.Value)
		}
	}

	return []DiagnosedSetting{cert, clientKey}
}

func pathProblem(path string) string {
	info, err := os.Stat(path)
	switch {
	case err != nil:
		return "cannot be opened: " + err.Error()
	case info.IsDir():
		return "is a directory, not a file"
	}

	return ""
}

func (r resolution) duration(name string, setting runtimeSetting, override *string, fallback time.Duration) DiagnosedSetting {
	resolved := resolveSetting(name, fallback.String(), r.in.fromStringFlag(setting, override)...)
	if !resolved.Configured {
		return resolved
	}

	parsed, err := time.ParseDuration(resolved.Value)
	switch {
	case err != nil:
		resolved.Problem = fmt.Sprintf("must be a valid duration (example: %s)", fallback)
	case parsed <= 0:
		resolved.Problem = "must be greater than 0"
	}

	return resolved
}

func (r resolution) retryCount() DiagnosedSetting {
	var flag []settingCandidate
	if value := r.in.flags.RetryCount; value != nil {
		flag = []settingCandidate{{source: SettingSource{Kind: SourceFlag, Name: settingRetryCount.flag}, value: strconv.Itoa(*value)}}
	}

	variable := r.in.fromEnvironment(settingRetryCount.environment)
	setting := resolveSetting("retry_count", strconv.Itoa(defaultRetryCount), flagOver(flag != nil, flag, variable)...)
	if !setting.Configured {
		return setting
	}

	count, err := strconv.Atoi(setting.Value)
	switch {
	case err != nil:
		setting.Problem = "must be a non-negative integer"
	case count < 0:
		setting.Problem = "must be greater than or equal to 0"
	}

	return setting
}

// diagnostics reports a setting the global flag writes into its variable, so
// the variable carries the flag's value and only the flag's being passed tells
// the two apart.
func (r resolution) diagnostics(name, variable, flag, fallback, allowed string, valid func(string) error) DiagnosedSetting {
	candidates := r.in.fromEnvironment(variable)
	if r.in.changedFlags[flag] {
		for index := range candidates {
			candidates[index].source = SettingSource{Kind: SourceFlag, Name: "--" + flag}
		}
	}

	setting := resolveSetting(name, fallback, candidates...)
	if setting.Configured && valid(setting.Value) != nil {
		setting.Problem = "must be one of: " + allowed
	}

	return setting
}

// updateBaseURL follows ResolveUpdateBaseURL, where the environment and the
// user's own files outrank the system file: a mirror there is a default, not a
// mandate. bb update's --base-url flag outranks all of them.
func (r resolution) updateBaseURL() DiagnosedSetting {
	candidates := r.in.fromProcessEnvironment("BB_UPDATE_BASE_URL")
	candidates = append(candidates, r.fromFile(TierWorkspace, "update_base_url", r.workspace.UpdateBaseURL)...)
	if r.storedRead {
		candidates = append(candidates, r.fromFile(TierStored, "update_base_url", r.stored.UpdateBaseURL)...)
	}
	candidates = append(candidates, r.fromFile(TierSystem, "update_base_url", r.system.UpdateBaseURL)...)
	if r.system.Policies != nil {
		candidates = append(candidates, r.fromFile(TierSystem, "policies.update_base_url", r.system.Policies.UpdateBaseURL)...)
	}
	if r.system.Policy != nil {
		candidates = append(candidates, r.fromFile(TierSystem, "policy.update_base_url", r.system.Policy.UpdateBaseURL)...)
	}
	candidates = append(candidates, r.fromRegistry("UpdateBaseURL", r.in.platformPolicy.UpdateBaseURL)...)

	for index := range candidates {
		candidates[index].value = normalizeURL(candidates[index].value)
	}

	setting := resolveSetting("update_base_url", "https://api.github.com", candidates...)
	// bb update refuses a base URL it may not fetch from, so say so here rather
	// than on the next update. --allow-http belongs to bb update and cannot be
	// seen from here; policy and BB_ALLOW_HTTP_UPDATE can.
	if err := r.updateHTTPPermission().CheckURL(setting.Value); err != nil {
		setting.Problem = apperrors.MessageOf(err)
	}

	return setting
}

// updateHTTPPermission follows ResolveUpdateHTTPPermission for a run without
// --allow-http.
func (r resolution) updateHTTPPermission() UpdateHTTPPermission {
	if r.policy.AllowHTTPUpdate != nil {
		return UpdateHTTPPermission{Allowed: *r.policy.AllowHTTPUpdate, ForbiddenByPolicy: !*r.policy.AllowHTTPUpdate}
	}
	if variable := r.in.fromProcessEnvironment(settingAllowHTTPUpdate.environment); len(variable) > 0 {
		allowed, err := strconv.ParseBool(variable[0].value)
		return UpdateHTTPPermission{Allowed: err == nil && allowed}
	}

	return UpdateHTTPPermission{}
}

func (r resolution) fromRegistry(name, value string) []settingCandidate {
	if name == "" || strings.TrimSpace(value) == "" {
		return nil
	}

	return []settingCandidate{{source: SettingSource{Kind: SourceRegistry, Name: name, Path: registryPolicyLocation}, value: strings.TrimSpace(value)}}
}

func policyText(read func(PolicyConfig) string) func(PolicyConfig) string {
	return func(policy PolicyConfig) string { return strings.TrimSpace(read(policy)) }
}

func policyFlag(read func(PolicyConfig) *bool) func(PolicyConfig) string {
	return func(policy PolicyConfig) string {
		if value := read(policy); value != nil {
			return strconv.FormatBool(*value)
		}
		return ""
	}
}

func policyList(read func(PolicyConfig) []string) func(PolicyConfig) string {
	return func(policy PolicyConfig) string { return strings.Join(read(policy), ", ") }
}

// policyCandidates lists where a policy setting is set, strongest first, in
// the reverse of the order effectivePolicy merges them.
func (r resolution) policyCandidates(key, registryName string, value func(PolicyConfig) string) []settingCandidate {
	candidates := r.fromRegistry(registryName, value(r.in.platformPolicy))
	if r.system.Policy != nil {
		candidates = append(candidates, r.fromFile(TierSystem, "policy."+key, value(*r.system.Policy))...)
	}
	if r.system.Policies != nil {
		candidates = append(candidates, r.fromFile(TierSystem, "policies."+key, value(*r.system.Policies))...)
	}

	return append(candidates, r.fromFile(TierSystem, key, value(r.system.PolicyConfig()))...)
}

func (r resolution) policySetting(name, registryName, fallback string, value func(PolicyConfig) string) DiagnosedSetting {
	return resolveSetting(name, fallback, r.policyCandidates(name, registryName, value)...)
}

func (r resolution) requireKeyring() DiagnosedSetting {
	policy := r.policyCandidates("require_keyring", "RequireKeyring", policyFlag(func(p PolicyConfig) *bool { return p.RequireKeyring }))

	setting := resolveSetting("require_keyring", "false", mandateOver(policy, r.in.fromEnvironment("BB_REQUIRE_KEYRING"))...)
	if setting.Source.Kind == SourceEnvironment || setting.Source.Kind == SourceDotenv {
		parsed, err := strconv.ParseBool(setting.Value)
		if err != nil {
			setting.Problem = "BB_REQUIRE_KEYRING must be a boolean"
			return setting
		}
		setting.Value = strconv.FormatBool(parsed)
	}

	return setting
}

func (r resolution) disableUpdate() DiagnosedSetting {
	policy := r.policyCandidates("disable_update", "DisableUpdate", policyFlag(func(p PolicyConfig) *bool { return p.DisableUpdate }))

	variable := r.in.fromProcessEnvironment("BB_DISABLE_UPDATE")
	for index := range variable {
		disabled := variable[index].value == "1" || strings.EqualFold(variable[index].value, "true")
		variable[index].value = strconv.FormatBool(disabled)
	}

	return resolveSetting("disable_update", "false", mandateOver(policy, variable)...)
}

func (r resolution) updateTrust() []DiagnosedSetting {
	settings := []DiagnosedSetting{
		r.policySetting("update_trusted_root", "UpdateTrustedRoot", "", policyText(func(p PolicyConfig) string { return p.UpdateTrustedRoot })),
		r.policySetting("update_tuf_url", "UpdateTUFURL", "", policyText(func(p PolicyConfig) string { return p.UpdateTUFURL })),
		r.policySetting("update_signature_identity", "UpdateSignatureIdentity", "", policyText(func(p PolicyConfig) string { return p.UpdateSignatureIdentity })),
		r.policySetting("update_signature_issuer", "UpdateSignatureIssuer", "", policyText(func(p PolicyConfig) string { return p.UpdateSignatureIssuer })),
		r.policySetting("allow_unverified_update", "AllowUnverifiedUpdate", "false", policyFlag(func(p PolicyConfig) *bool { return p.AllowUnverifiedUpdate })),
	}

	// bb update refuses trust settings it cannot use; say which one.
	if _, err := resolveUpdateTrust(r.policy); err != nil {
		message := apperrors.MessageOf(err)
		for index := range settings[:2] {
			if strings.Contains(message, settings[index].Name) {
				settings[index].Problem = message
			}
		}
	}

	return settings
}

func (r resolution) keyring(requireKeyring DiagnosedSetting) KeyringDiagnosis {
	diagnosis := KeyringDiagnosis{Required: requireKeyring.Value == "true"}
	if !diagnosis.Required {
		return diagnosis
	}

	diagnosis.RequiredBy = requireKeyring.Source
	diagnosis.Checked = true
	if err := r.in.probeKeyring(); err != nil {
		diagnosis.Problem = "the OS keyring could not be reached: " + err.Error()
		return diagnosis
	}
	diagnosis.Reachable = true

	return diagnosis
}
