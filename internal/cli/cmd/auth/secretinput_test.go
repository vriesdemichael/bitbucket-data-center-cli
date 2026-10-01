package auth

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

func TestReadSecretFromStdin(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "bare value", input: "tok", expected: "tok"},
		{name: "trailing newline from echo", input: "tok\n", expected: "tok"},
		{name: "windows line ending", input: "tok\r\n", expected: "tok"},
		{name: "value with punctuation", input: "BBDC-abc.123_x-y\n", expected: "BBDC-abc.123_x-y"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			secret, err := readSecretFromStdin(strings.NewReader(testCase.input), "--token-stdin")
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if secret != testCase.expected {
				t.Fatalf("expected %q, got %q", testCase.expected, secret)
			}
		})
	}
}

func TestReadSecretFromStdinRejections(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		input   string
		wantMsg string
	}{
		{name: "empty", input: "", wantMsg: "stdin was empty"},
		{name: "whitespace only", input: "  \n", wantMsg: "stdin was empty"},
		// A secret arriving with interior whitespace is far more likely to be a
		// piping mistake than a real credential, and accepting it produces an
		// authentication failure much later, far from its cause.
		{name: "interior space", input: "two words\n", wantMsg: "contains whitespace"},
		{name: "multiple lines", input: "first\nsecond\n", wantMsg: "contains whitespace"},
		{name: "leading space", input: " tok\n", wantMsg: "contains whitespace"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := readSecretFromStdin(strings.NewReader(testCase.input), "--token-stdin")
			if err == nil {
				t.Fatal("expected an error")
			}
			if apperrors.KindOf(err) != apperrors.KindValidation {
				t.Fatalf("expected validation, got %q", apperrors.KindOf(err))
			}
			if !strings.Contains(err.Error(), testCase.wantMsg) {
				t.Fatalf("expected %q in %q", testCase.wantMsg, err.Error())
			}
		})
	}
}

func TestReadSecretFromStdinRejectsOversizedInput(t *testing.T) {
	t.Parallel()

	_, err := readSecretFromStdin(strings.NewReader(strings.Repeat("a", maxSecretLength+1)), "--token-stdin")
	if err == nil {
		t.Fatal("expected oversized input to be rejected")
	}
	if !strings.Contains(err.Error(), "not a credential") {
		t.Fatalf("unexpected error %q", err.Error())
	}
}

func TestReadSecretFromStdinAcceptsInputAtTheLimit(t *testing.T) {
	t.Parallel()

	secret, err := readSecretFromStdin(strings.NewReader(strings.Repeat("a", maxSecretLength)), "--token-stdin")
	if err != nil {
		t.Fatalf("expected input at the limit to be accepted, got %v", err)
	}
	if len(secret) != maxSecretLength {
		t.Fatalf("expected %d bytes, got %d", maxSecretLength, len(secret))
	}
}

func TestReadSecretFromStdinWithoutAReader(t *testing.T) {
	t.Parallel()

	if _, err := readSecretFromStdin(nil, "--token-stdin"); err == nil {
		t.Fatal("expected an error when stdin is unavailable")
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("stdin is closed")
}

func TestReadSecretFromStdinReportsReadFailure(t *testing.T) {
	t.Parallel()

	_, err := readSecretFromStdin(failingReader{}, "--token-stdin")
	if err == nil {
		t.Fatal("expected a read failure to be reported")
	}
	if !strings.Contains(err.Error(), "failed to read") {
		t.Fatalf("unexpected error %q", err.Error())
	}
}

// newLoginCommand builds the auth command tree with an isolated config file and
// separate output streams, so a test can tell stdout from stderr.
func newLoginCommand(t *testing.T) (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()

	t.Setenv("BB_CONFIG_PATH", filepath.Join(t.TempDir(), "config.yaml"))
	t.Setenv("BB_REQUIRE_KEYRING", "")

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	cmd := New(Dependencies{
		JSONEnabled: func() bool { return true },
		LoadConfig:  func() (config.AppConfig, error) { return config.LoadFromEnv() },
		WriteJSON:   func(writer io.Writer, payload any) error { return jsonoutput.Write(writer, payload) },
	})
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	// The real root command sets both, so a failing command does not dump usage
	// onto stdout. Matching it here keeps assertions about stdout meaningful.
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	return cmd, stdout, stderr
}

func TestLoginReadsTheTokenFromStdin(t *testing.T) {
	cmd, stdout, stderr := newLoginCommand(t)
	cmd.SetIn(strings.NewReader("piped-token\n"))
	cmd.SetArgs([]string{"login", "https://stdin-login.example.invalid", "--token-stdin", "--discover-aliases=false"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("login failed: %v (stderr: %s)", err, stderr.String())
	}

	var payload struct {
		Host     string `json:"host"`
		AuthMode string `json:"authMode"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &struct {
		Data any `json:"data"`
	}{Data: &payload}); err != nil {
		t.Fatalf("expected a parseable envelope on stdout, got %q (%v)", stdout.String(), err)
	}
	if payload.AuthMode != "token" {
		t.Fatalf("expected the piped token to be stored, got authMode %q", payload.AuthMode)
	}

	// The safe form has nothing to warn about.
	if strings.Contains(stderr.String(), "process list") {
		t.Fatalf("did not expect a process-list warning, got %q", stderr.String())
	}
}

func TestLoginRejectsBothStdinFlags(t *testing.T) {
	cmd, _, _ := newLoginCommand(t)
	cmd.SetIn(strings.NewReader("secret\n"))
	cmd.SetArgs([]string{"login", "https://both.example.invalid", "--token-stdin", "--password-stdin", "--username", "alice", "--discover-aliases=false"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected both stdin flags to be rejected: stdin carries one secret")
	}
	if apperrors.KindOf(err) != apperrors.KindValidation {
		t.Fatalf("expected validation, got %q", apperrors.KindOf(err))
	}
}

func TestLoginReadsThePasswordFromStdin(t *testing.T) {
	cmd, stdout, stderr := newLoginCommand(t)
	cmd.SetIn(strings.NewReader("piped-password\n"))
	cmd.SetArgs([]string{"login", "https://stdin-basic.example.invalid", "--username", "alice", "--password-stdin", "--discover-aliases=false"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("login failed: %v (stderr: %s)", err, stderr.String())
	}

	var payload struct {
		AuthMode string `json:"authMode"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &struct {
		Data any `json:"data"`
	}{Data: &payload}); err != nil {
		t.Fatalf("expected a parseable envelope on stdout, got %q (%v)", stdout.String(), err)
	}
	if payload.AuthMode != "basic" {
		t.Fatalf("expected basic auth mode, got %q", payload.AuthMode)
	}
}

func TestReportInsecureStorageNamesTheFile(t *testing.T) {
	t.Parallel()

	buffer := &bytes.Buffer{}
	reportInsecureStorage(buffer, "https://host.example.invalid")

	output := buffer.String()
	if !strings.Contains(output, "https://host.example.invalid") {
		t.Fatalf("expected the host named, got %q", output)
	}
	if !strings.Contains(output, "plaintext") {
		t.Fatalf("expected the exposure named plainly, got %q", output)
	}
	// A warning the reader cannot act on is decoration.
	if !strings.Contains(output, "bb auth logout --host https://host.example.invalid") || !strings.Contains(output, "BITBUCKET_TOKEN") {
		t.Fatalf("expected the remedy named, got %q", output)
	}
}

func TestDescribeCredentialStorage(t *testing.T) {
	t.Parallel()

	t.Run("keyring reports the bare kind", func(t *testing.T) {
		got := describeCredentialStorage(config.AppConfig{BitbucketToken: "tok", AuthSource: "stored"})
		if got != "keyring" {
			t.Fatalf("expected keyring, got %q", got)
		}
	})

	t.Run("plaintext names the file", func(t *testing.T) {
		got := describeCredentialStorage(config.AppConfig{BitbucketToken: "tok", AuthSource: "stored", UsedInsecureStorage: true})
		if !strings.HasPrefix(got, "config-file-plaintext (") {
			t.Fatalf("expected the path appended, got %q", got)
		}
	})
}

func TestStoredConfigLocationAlwaysReturnsSomething(t *testing.T) {
	t.Parallel()

	if got := storedConfigLocation(); strings.TrimSpace(got) == "" {
		t.Fatal("expected a non-empty location for the warning message")
	}
}

// noKeyring stands for a machine whose keyring cannot hold a secret: a headless
// server, a container, WSL without gnome-keyring.
func noKeyring(t *testing.T) {
	t.Helper()

	config.UseUnavailableKeyring(t, errors.New("no secret service on this bus"))
}

func TestLoginRefusesPlaintextWithoutAllowInsecureStorage(t *testing.T) {
	cmd, stdout, stderr := newLoginCommand(t)
	noKeyring(t)
	configPath := os.Getenv("BB_CONFIG_PATH")
	cmd.SetIn(strings.NewReader("unasked-token\n"))
	cmd.SetArgs([]string{"login", "https://unasked.example.invalid", "--token-stdin", "--discover-aliases=false"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected the login to refuse plaintext it was not asked for")
	}
	if apperrors.KindOf(err) != apperrors.KindPermanent || apperrors.ExitCode(err) != 1 {
		t.Fatalf("expected permanent, exit 1, got kind %q (%v)", apperrors.KindOf(err), err)
	}
	for _, want := range []string{"keyring is unavailable", "--allow-insecure-storage", "BITBUCKET_TOKEN"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}

	if _, statErr := os.Stat(configPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("a refused login wrote the config file (stat: %v)", statErr)
	}
	if stdout.Len() != 0 || strings.Contains(stderr.String(), "plaintext to") {
		t.Fatalf("a refused login reported storing something: stdout %q, stderr %q", stdout.String(), stderr.String())
	}
}

func TestLoginStoresPlaintextWithAllowInsecureStorage(t *testing.T) {
	cmd, stdout, stderr := newLoginCommand(t)
	noKeyring(t)
	configPath := os.Getenv("BB_CONFIG_PATH")
	cmd.SetIn(strings.NewReader("asked-token\n"))
	cmd.SetArgs([]string{"login", "https://asked.example.invalid", "--token-stdin", "--discover-aliases=false", "--allow-insecure-storage"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("login failed: %v (stderr: %s)", err, stderr.String())
	}

	var payload struct {
		UsedInsecureStorage bool `json:"usedInsecureStorage"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &struct {
		Data any `json:"data"`
	}{Data: &payload}); err != nil {
		t.Fatalf("expected a parseable envelope on stdout, got %q (%v)", stdout.String(), err)
	}
	if !payload.UsedInsecureStorage {
		t.Fatalf("expected usedInsecureStorage, got %s", stdout.String())
	}

	contents, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(contents), "asked-token") {
		t.Fatalf("expected the token in the config file, got:\n%s", contents)
	}

	// Asked for, and still said: the flag may sit in a script nobody rereads.
	if !strings.Contains(stderr.String(), "written in plaintext to "+configPath) {
		t.Fatalf("expected the plaintext warning on stderr, got %q", stderr.String())
	}
}

func TestLoginRejectsRequireKeyringWithAllowInsecureStorage(t *testing.T) {
	cmd, stdout, _ := newLoginCommand(t)
	configPath := os.Getenv("BB_CONFIG_PATH")
	cmd.SetIn(strings.NewReader("paired-token\n"))
	cmd.SetArgs([]string{"login", "https://paired.example.invalid", "--token-stdin", "--discover-aliases=false", "--require-keyring", "--allow-insecure-storage"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected the two flags together to be rejected")
	}
	// The flag-group wording is what the root command classifies as a usage
	// error, exit 2 (internal/cli/usageerrors.go).
	for _, want := range []string{"flags in the group", "require-keyring", "allow-insecure-storage"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the rejection does not name %q: %v", want, err)
		}
	}

	if _, statErr := os.Stat(configPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("a rejected login wrote the config file (stat: %v)", statErr)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected nothing on stdout, got %q", stdout.String())
	}
}

func TestLoginKeyringRequirementOutranksAllowInsecureStorage(t *testing.T) {
	cases := []struct {
		name    string
		require func(t *testing.T)
		message string
	}{
		{
			name:    "BB_REQUIRE_KEYRING",
			require: func(t *testing.T) { t.Setenv("BB_REQUIRE_KEYRING", "1") },
			message: "keyring-backed storage is required;",
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
			message: "required by administrative policy",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd, stdout, _ := newLoginCommand(t)
			t.Setenv("BB_SYSTEM_CONFIG_PATH", filepath.Join(t.TempDir(), "absent.yaml"))
			noKeyring(t)
			tc.require(t)
			configPath := os.Getenv("BB_CONFIG_PATH")
			cmd.SetIn(strings.NewReader("required-token\n"))
			cmd.SetArgs([]string{"login", "https://required.example.invalid", "--token-stdin", "--discover-aliases=false", "--allow-insecure-storage"})

			err := cmd.Execute()
			if err == nil {
				t.Fatal("expected the requirement to refuse plaintext despite --allow-insecure-storage")
			}
			if apperrors.KindOf(err) != apperrors.KindPermanent {
				t.Fatalf("expected permanent, got kind %q (%v)", apperrors.KindOf(err), err)
			}
			if !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("expected the requirement's own message, got %v", err)
			}

			if _, statErr := os.Stat(configPath); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("a refused login wrote the config file (stat: %v)", statErr)
			}
			if stdout.Len() != 0 {
				t.Fatalf("expected nothing on stdout, got %q", stdout.String())
			}
		})
	}
}

// TestAPlaintextCredentialStoredBeforeKeepsWorking is the upgrade: nobody with
// a credential already in the file is locked out on a machine with no keyring.
func TestAPlaintextCredentialStoredBeforeKeepsWorking(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	host := "https://stored-before.example.invalid"
	contents := "default_host: " + host + "\nhosts:\n    " + host + ":\n        url: " + host +
		"\n        auth_mode: token\ninsecure_secrets:\n    " + host + ":\n        token: plaintext-token\n"
	if err := os.WriteFile(configPath, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BITBUCKET_URL", host)
	clearAuthEnvironment(t)
	noKeyring(t)

	cfg, err := config.LoadFromEnv()
	if err != nil {
		t.Fatalf("the stored plaintext credential no longer loads: %v", err)
	}
	if cfg.BitbucketToken != "plaintext-token" || cfg.CredentialStorage() != "config-file-plaintext" {
		t.Fatalf("expected the stored plaintext token, got token %q, storage %q", cfg.BitbucketToken, cfg.CredentialStorage())
	}
}

func TestLoginRejectsAnEmptyPipedToken(t *testing.T) {
	cmd, stdout, _ := newLoginCommand(t)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs([]string{"login", "https://empty-stdin.example.invalid", "--token-stdin", "--discover-aliases=false"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected an empty pipe to be rejected rather than stored")
	}
	if apperrors.KindOf(err) != apperrors.KindValidation {
		t.Fatalf("expected validation, got %q", apperrors.KindOf(err))
	}
	if stdout.String() != "" {
		t.Fatalf("expected nothing written on the failure path, got %q", stdout.String())
	}
}

func TestLoginRejectsAnEmptyPipedPassword(t *testing.T) {
	cmd, _, _ := newLoginCommand(t)
	cmd.SetIn(strings.NewReader("\n"))
	cmd.SetArgs([]string{"login", "https://empty-pass.example.invalid", "--username", "alice", "--password-stdin", "--discover-aliases=false"})

	if err := cmd.Execute(); err == nil {
		t.Fatal("expected an empty pipe to be rejected rather than stored")
	}
}

func TestAuthStatusNamesPlaintextStorage(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	host := "https://status-plain.example.invalid"
	contents := "default_host: " + host + "\nhosts:\n    " + host + ":\n        url: " + host +
		"\n        auth_mode: token\ninsecure_secrets:\n    " + host + ":\n        token: plaintext-token\n"
	if err := os.WriteFile(configPath, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BITBUCKET_URL", host)
	clearAuthEnvironment(t)

	stdout := &bytes.Buffer{}
	cmd := New(Dependencies{
		JSONEnabled: func() bool { return false },
		LoadConfig:  func() (config.AppConfig, error) { return config.LoadFromEnv() },
	})
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"status"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("status failed: %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "config-file-plaintext") {
		t.Fatalf("expected plaintext storage reported, got %q", output)
	}
	// Naming the file is what makes the report actionable for an auditor.
	if !strings.Contains(output, configPath) {
		t.Fatalf("expected the config path named, got %q", output)
	}
}

// clearAuthEnvironment removes every variable LoadFromEnv treats as an auth
// source.
//
// ADMIN_USER and ADMIN_PASSWORD are set on the CI runner for the live suite and
// leak into the unit run, which is how the ambient-environment bug this guards
// against reached CI in the first place.
func clearAuthEnvironment(t *testing.T) {
	t.Helper()

	for _, key := range []string{
		"BITBUCKET_TOKEN", "BITBUCKET_USERNAME", "BITBUCKET_USER", "BITBUCKET_PASSWORD",
		"ADMIN_USER", "ADMIN_PASSWORD", "BB_REQUIRE_KEYRING", "BB_DISABLE_STORED_CONFIG",
	} {
		t.Setenv(key, "")
	}
}

func TestResolveLoginSecret(t *testing.T) {
	t.Parallel()

	t.Run("stdin is read when requested", func(t *testing.T) {
		secret, err := resolveLoginSecret(true, strings.NewReader("piped\n"), "--token-stdin")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if secret != "piped" {
			t.Fatalf("expected piped value, got %q", secret)
		}
	})

	t.Run("absent secret stays empty", func(t *testing.T) {
		// There is no value form to fall back to any more: --token and
		// --password were retired in #464, so a login naming neither stdin
		// flag simply has no secret to resolve.
		secret, err := resolveLoginSecret(false, nil, "--token-stdin")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if secret != "" {
			t.Fatalf("expected empty, got %q", secret)
		}
	})
}

// TestTheValueFormsAreGone is the regression guard for #464. A secret must not
// be accepted anywhere the operating system would expose it: a flag value lands
// in /proc/<pid>/cmdline, which is world-readable, and in shell history.
func TestTheValueFormsAreGone(t *testing.T) {
	t.Parallel()

	command := New(Dependencies{})

	var login *cobra.Command
	for _, child := range command.Commands() {
		if child.Name() == "login" {
			login = child
		}
	}
	if login == nil {
		t.Fatal("auth login is missing")
	}

	for _, name := range []string{"token", "password"} {
		if login.Flags().Lookup(name) != nil {
			t.Errorf("--%s still exists on auth login", name)
		}
	}

	for _, name := range []string{"token-stdin", "password-stdin"} {
		if login.Flags().Lookup(name) == nil {
			t.Errorf("--%s is missing; the safe form has to remain", name)
		}
	}
}
