package auth

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/git/execgit"
)

// gitCommand builds a git invocation with git's repository-scoping variables
// removed.
//
// Raw exec.Command("git", ...) inherits the environment, and git exports GIT_DIR
// to every hook it runs. Tests executed from a pre-commit hook therefore inherit
// a GIT_DIR pointing at the repository being committed to, and git honours it
// over -C — so `git -C <tempdir> init` reinitialises the real repository, and
// because GIT_DIR is set without GIT_WORK_TREE, it does so as a bare one. That
// wrote core.bare=true into this project's configuration and broke git rev-parse
// in the main checkout and every worktree, while passing cleanly on every manual
// run outside a hook.
func gitCommand(args ...string) *exec.Cmd {
	command := exec.Command("git", args...)
	command.Env = execgit.ScopeFreeEnv()
	return command
}

// writeStoredConfig writes a bb configuration with credentials in the insecure
// fallback rather than the keyring, so these tests never touch the developer's
// real credential store.
func writeStoredConfig(t *testing.T, hostURL, token string, aliases ...string) {
	t.Helper()

	configPath := filepath.Join(t.TempDir(), "config.yaml")

	// Host keys are scheme://host, so they must be quoted to be valid YAML keys.
	key := "\"" + hostURL + "\""

	aliasBlock := ""
	if len(aliases) > 0 {
		aliasBlock = "    aliases:\n"
		for _, alias := range aliases {
			aliasBlock += "      - " + alias + "\n"
		}
	}

	contents := "default_host: " + key + "\n" +
		"hosts:\n" +
		"  " + key + ":\n" +
		"    url: " + hostURL + "\n" +
		"    auth_mode: token\n" +
		aliasBlock +
		"insecure_secrets:\n" +
		"  " + key + ":\n" +
		"    token: " + token + "\n"

	if err := os.WriteFile(configPath, []byte(contents), 0o600); err != nil {
		t.Fatalf("write stored config: %v", err)
	}

	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")
}

func TestResolveGitCredentialReturnsStoredToken(t *testing.T) {
	writeStoredConfig(t, "https://bb-credhelper-fixture.invalid", "secret-token")

	username, password, ok := resolveGitCredential(credentialRequest{
		Protocol: "https",
		Host:     "bb-credhelper-fixture.invalid",
	})
	if !ok {
		t.Fatal("expected credentials for the configured host")
	}
	if password != "secret-token" {
		t.Fatalf("unexpected password: %q", password)
	}
	// Bitbucket ignores the username when the password is a valid access token,
	// but git requires the field to be present.
	if strings.TrimSpace(username) == "" {
		t.Fatal("expected a non-empty username")
	}
}

// Deployments commonly serve the API and git traffic from different hostnames,
// so a configured alias is a genuine match rather than a fallback.
func TestResolveGitCredentialMatchesConfiguredAlias(t *testing.T) {
	writeStoredConfig(t, "https://bb-credhelper-fixture.invalid", "secret-token", "git.example.com")

	if _, _, ok := resolveGitCredential(credentialRequest{Protocol: "https", Host: "git.example.com"}); !ok {
		t.Fatal("expected a configured alias to resolve")
	}
}

// The regression this command exists to prevent: the stored host is also the
// default host, so a lookup that falls back to the default would hand the
// Bitbucket token to any host git happens to ask about.
func TestResolveGitCredentialRefusesUnconfiguredHosts(t *testing.T) {
	writeStoredConfig(t, "https://bb-credhelper-fixture.invalid", "secret-token")

	for _, host := range []string{"github.com", "gitlab.com", "bb-credhelper-fixture.invalid.attacker.test"} {
		if _, _, ok := resolveGitCredential(credentialRequest{Protocol: "https", Host: host}); ok {
			t.Fatalf("resolved credentials for unconfigured host %q", host)
		}
	}
}

func TestResolveGitCredentialWithNothingStored(t *testing.T) {
	t.Setenv("BB_CONFIG_PATH", filepath.Join(t.TempDir(), "absent.yaml"))
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")

	if _, _, ok := resolveGitCredential(credentialRequest{Protocol: "https", Host: "bb-credhelper-fixture.invalid"}); ok {
		t.Fatal("expected no credentials when nothing is stored")
	}
}

// The full command path, exercised end to end so the response is asserted in the
// exact form git parses.
func TestGitCredentialGetEmitsGitProtocolResponse(t *testing.T) {
	writeStoredConfig(t, "https://bb-credhelper-fixture.invalid", "secret-token")

	cmd := newGitCredentialCommand()
	output := &strings.Builder{}
	cmd.SetOut(output)
	cmd.SetErr(output)
	cmd.SetIn(strings.NewReader("protocol=https\nhost=bb-credhelper-fixture.invalid\n\n"))
	cmd.SetArgs([]string{"get"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	response := output.String()
	if !strings.Contains(response, "password=secret-token") {
		t.Fatalf("expected the stored token in the response, got: %q", response)
	}
	// git reads key=value lines terminated by a blank line.
	if !strings.HasSuffix(response, "\n\n") {
		t.Fatalf("response is not blank-line terminated: %q", response)
	}
}

// isolatedGitGlobalConfig points git's global configuration at a temporary file
// so these tests never write to the developer's ~/.gitconfig. GIT_CONFIG_GLOBAL
// survives ScopeFreeEnv, which strips only git's repository-scoping variables.
func isolatedGitGlobalConfig(t *testing.T) string {
	t.Helper()

	configPath := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(configPath, nil, 0o600); err != nil {
		t.Fatalf("create global git config: %v", err)
	}

	t.Setenv("GIT_CONFIG_GLOBAL", configPath)
	return configPath
}

func readGitGlobalValues(t *testing.T, key string) []string {
	t.Helper()

	command := gitCommand("config", "--global", "--get-all", key)
	output, err := command.Output()
	if err != nil {
		// Exit 1 simply means the key is unset.
		return nil
	}

	values := []string{}
	values = append(values, strings.Split(strings.TrimSpace(string(output)), "\n")...)
	return values
}

func TestConfigureGitCredentialHelperWritesScopedGlobalConfig(t *testing.T) {
	isolatedGitGlobalConfig(t)

	key := "credential.https://bitbucket.example.com.helper"
	value := "!\"/usr/local/bin/bb\" auth git-credential"

	if err := defaultConfigureGitCredentialHelper(context.Background(), key, value, true, false); err != nil {
		t.Fatalf("configure helper: %v", err)
	}

	values := readGitGlobalValues(t, key)
	if len(values) != 1 || values[0] != value {
		t.Fatalf("expected exactly one helper value, got %#v", values)
	}
}

// credential.<url>.helper is multi-valued and git consults every entry in order,
// so bb must be the only helper for the host after setup. Anything inherited
// from a broader scope would otherwise answer first, possibly with a stale
// credential.
func TestConfigureGitCredentialHelperResetsAnInheritedHelper(t *testing.T) {
	isolatedGitGlobalConfig(t)

	key := "credential.https://bitbucket.example.com.helper"

	if err := gitCommand("config", "--global", "--add", key, "manager").Run(); err != nil {
		t.Fatalf("seed an existing helper: %v", err)
	}

	value := "!\"/usr/local/bin/bb\" auth git-credential"
	if err := defaultConfigureGitCredentialHelper(context.Background(), key, value, true, true); err != nil {
		t.Fatalf("configure helper with force: %v", err)
	}

	values := readGitGlobalValues(t, key)
	if len(values) != 1 {
		t.Fatalf("expected the inherited helper to be reset, got %#v", values)
	}
	if values[0] != value {
		t.Fatalf("expected bb to be the only helper, got %#v", values)
	}
}

// Replacing another credential manager without being asked would break
// authentication in a way that is hard to attribute, so it requires --force.
func TestConfigureGitCredentialHelperRefusesToClobberWithoutForce(t *testing.T) {
	isolatedGitGlobalConfig(t)

	key := "credential.https://bitbucket.example.com.helper"
	if err := gitCommand("config", "--global", "--add", key, "manager").Run(); err != nil {
		t.Fatalf("seed an existing helper: %v", err)
	}

	err := defaultConfigureGitCredentialHelper(context.Background(), key, "!bb auth git-credential", true, false)
	if err == nil {
		t.Fatal("expected an existing helper to be preserved without --force")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Fatalf("error should point at --force, got: %v", err)
	}

	if values := readGitGlobalValues(t, key); len(values) != 1 || values[0] != "manager" {
		t.Fatalf("existing helper should be untouched, got %#v", values)
	}
}

// Re-running setup is expected to be harmless.
func TestConfigureGitCredentialHelperIsIdempotent(t *testing.T) {
	isolatedGitGlobalConfig(t)

	key := "credential.https://bitbucket.example.com.helper"
	value := "!\"/usr/local/bin/bb\" auth git-credential"

	for range 2 {
		if err := defaultConfigureGitCredentialHelper(context.Background(), key, value, true, false); err != nil {
			t.Fatalf("configure helper: %v", err)
		}
	}

	if values := readGitGlobalValues(t, key); len(values) != 1 {
		t.Fatalf("expected a single helper after repeated setup, got %#v", values)
	}
}

// These two exercise local scope through configureGitCredentialHelperIn, which
// takes the working directory explicitly. They deliberately do not use t.Chdir:
// that moves the working directory for the whole test binary, so any later test
// shelling out to git relative to the process directory operates on whatever
// repository it lands in. Doing exactly that wrote core.bare=true into this
// project's own configuration and broke every worktree.

func TestConfigureGitCredentialHelperLocalScopeRequiresARepository(t *testing.T) {
	err := configureGitCredentialHelperIn(
		context.Background(),
		t.TempDir(),
		"credential.https://bitbucket.example.com.helper",
		"!bb auth git-credential",
		false,
		false,
	)
	if err == nil {
		t.Fatal("expected local scope outside a repository to fail")
	}
	if !strings.Contains(err.Error(), "not inside a git repository") {
		t.Fatalf("expected a repository error, got: %v", err)
	}
}

func TestConfigureGitCredentialHelperWritesLocalConfig(t *testing.T) {
	repository := t.TempDir()
	if err := gitCommand("-C", repository, "init").Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}

	key := "credential.https://bitbucket.example.com.helper"
	value := "!\"/usr/local/bin/bb\" auth git-credential"

	if err := configureGitCredentialHelperIn(context.Background(), repository, key, value, false, false); err != nil {
		t.Fatalf("configure helper locally: %v", err)
	}

	output, err := gitCommand("-C", repository, "config", "--local", "--get-all", key).Output()
	if err != nil {
		t.Fatalf("read local config: %v", err)
	}
	if !strings.Contains(string(output), "auth git-credential") {
		t.Fatalf("expected the helper in local config, got: %s", output)
	}

	// The write must have landed in the temporary repository, not here.
	if _, err := gitCommand("config", "--local", "--get", key).Output(); err == nil {
		t.Fatal("helper leaked into the repository the tests run inside")
	}
}

// Basic auth is the other supported mode, so the helper must supply a stored
// username and password pair as well as a token.
func TestResolveGitCredentialReturnsStoredBasicAuth(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	contents := "default_host: \"https://bb-basic-fixture.invalid\"\n" +
		"hosts:\n" +
		"  \"https://bb-basic-fixture.invalid\":\n" +
		"    url: https://bb-basic-fixture.invalid\n" +
		"    username: alice\n" +
		"    auth_mode: basic\n" +
		"insecure_secrets:\n" +
		"  \"https://bb-basic-fixture.invalid\":\n" +
		"    password: hunter2\n"

	if err := os.WriteFile(configPath, []byte(contents), 0o600); err != nil {
		t.Fatalf("write stored config: %v", err)
	}
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")

	username, password, ok := resolveGitCredential(credentialRequest{
		Protocol: "https",
		Host:     "bb-basic-fixture.invalid",
	})
	if !ok {
		t.Fatal("expected basic-auth credentials to resolve")
	}
	if username != "alice" || password != "hunter2" {
		t.Fatalf("unexpected credentials: %q / %q", username, password)
	}
}

// A configured host with no usable secret must read as "cannot help" rather than
// returning an empty pair, which git would try to authenticate with.
func TestResolveGitCredentialWithHostButNoSecret(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	contents := "hosts:\n" +
		"  \"https://bb-nosecret-fixture.invalid\":\n" +
		"    url: https://bb-nosecret-fixture.invalid\n" +
		"    auth_mode: token\n"

	if err := os.WriteFile(configPath, []byte(contents), 0o600); err != nil {
		t.Fatalf("write stored config: %v", err)
	}
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")

	if _, _, ok := resolveGitCredential(credentialRequest{
		Protocol: "https",
		Host:     "bb-nosecret-fixture.invalid",
	}); ok {
		t.Fatal("expected no credentials when the host has no stored secret")
	}
}

// setup-git reports what it configured in machine mode so automation can assert
// the scope it landed on.
func TestSetupGitEmitsJSONWhenMachineModeIsOn(t *testing.T) {
	var payload *GitCredentialSetup

	cmd := newSetupGitCommand(Dependencies{
		JSONEnabled: func() bool { return true },
		LoadConfig:  func() (config.AppConfig, error) { return config.AppConfig{}, nil },
		WriteJSON: func(_ io.Writer, value any) error {
			if typed, ok := value.(GitCredentialSetup); ok {
				payload = &typed
			}
			return nil
		},
		ConfigureGitCredentialHelper: func(context.Context, string, string, bool, bool) error { return nil },
	})
	cmd.SetOut(&strings.Builder{})
	cmd.SetErr(&strings.Builder{})
	cmd.SetArgs([]string{"--host", "https://bitbucket.example.com"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if payload == nil {
		t.Fatal("expected a JSON payload in machine mode")
	}
	if payload.Host != "https://bitbucket.example.com" {
		t.Fatalf("unexpected host in payload: %v", payload.Host)
	}
	if payload.Scope != "global" {
		t.Fatalf("unexpected scope in payload: %v", payload.Scope)
	}
}

// The exported wrapper resolves the working directory from the process; the
// global path never touches it.
func TestDefaultConfigureGitCredentialHelperGlobalPath(t *testing.T) {
	isolatedGitGlobalConfig(t)

	key := "credential.https://bb-wrapper-fixture.invalid.helper"
	if err := defaultConfigureGitCredentialHelper(context.Background(), key, "!bb auth git-credential", true, false); err != nil {
		t.Fatalf("configure helper: %v", err)
	}

	if values := readGitGlobalValues(t, key); len(values) != 1 {
		t.Fatalf("expected one helper value, got %#v", values)
	}
}
