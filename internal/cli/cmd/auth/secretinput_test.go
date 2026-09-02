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
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
)

func TestReadSecretFromStdin(t *testing.T) {
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
	_, err := readSecretFromStdin(strings.NewReader(strings.Repeat("a", maxSecretLength+1)), "--token-stdin")
	if err == nil {
		t.Fatal("expected oversized input to be rejected")
	}
	if !strings.Contains(err.Error(), "not a credential") {
		t.Fatalf("unexpected error %q", err.Error())
	}
}

func TestReadSecretFromStdinAcceptsInputAtTheLimit(t *testing.T) {
	secret, err := readSecretFromStdin(strings.NewReader(strings.Repeat("a", maxSecretLength)), "--token-stdin")
	if err != nil {
		t.Fatalf("expected input at the limit to be accepted, got %v", err)
	}
	if len(secret) != maxSecretLength {
		t.Fatalf("expected %d bytes, got %d", maxSecretLength, len(secret))
	}
}

func TestReadSecretFromStdinWithoutAReader(t *testing.T) {
	if _, err := readSecretFromStdin(nil, "--token-stdin"); err == nil {
		t.Fatal("expected an error when stdin is unavailable")
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("stdin is closed")
}

func TestReadSecretFromStdinReportsReadFailure(t *testing.T) {
	_, err := readSecretFromStdin(failingReader{}, "--token-stdin")
	if err == nil {
		t.Fatal("expected a read failure to be reported")
	}
	if !strings.Contains(err.Error(), "failed to read") {
		t.Fatalf("unexpected error %q", err.Error())
	}
}

func TestResolveLoginSecret(t *testing.T) {
	t.Run("flag value is used as-is", func(t *testing.T) {
		secret, err := resolveLoginSecret("  tok  ", false, nil, "--token", "--token-stdin")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if secret != "tok" {
			t.Fatalf("expected trimmed flag value, got %q", secret)
		}
	})

	t.Run("stdin is read when requested", func(t *testing.T) {
		secret, err := resolveLoginSecret("", true, strings.NewReader("piped\n"), "--token", "--token-stdin")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if secret != "piped" {
			t.Fatalf("expected piped value, got %q", secret)
		}
	})

	t.Run("both forms are rejected", func(t *testing.T) {
		// Passing both means the caller has not achieved the exposure they were
		// reaching for, so this is an error rather than a silent precedence rule.
		_, err := resolveLoginSecret("tok", true, strings.NewReader("piped\n"), "--token", "--token-stdin")
		if err == nil {
			t.Fatal("expected mutually exclusive flags to be rejected")
		}
		if !strings.Contains(err.Error(), "mutually exclusive") {
			t.Fatalf("unexpected error %q", err.Error())
		}
	})

	t.Run("absent secret stays empty", func(t *testing.T) {
		secret, err := resolveLoginSecret("", false, nil, "--token", "--token-stdin")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if secret != "" {
			t.Fatalf("expected empty, got %q", secret)
		}
	})
}

func TestWarnAboutSecretsOnTheCommandLine(t *testing.T) {
	newCommand := func() (*cobra.Command, *bytes.Buffer) {
		stderr := &bytes.Buffer{}
		cmd := &cobra.Command{Use: "login"}
		cmd.Flags().String("token", "", "")
		cmd.Flags().String("password", "", "")
		cmd.SetOut(io.Discard)
		cmd.SetErr(stderr)
		return cmd, stderr
	}

	t.Run("warns for a flag-supplied token", func(t *testing.T) {
		cmd, stderr := newCommand()
		if err := cmd.Flags().Set("token", "tok"); err != nil {
			t.Fatalf("set flag: %v", err)
		}

		warnAboutSecretsOnTheCommandLine(cmd)

		output := stderr.String()
		if !strings.Contains(output, "--token puts the credential in the process list") {
			t.Fatalf("expected a process-list warning, got %q", output)
		}
		if !strings.Contains(output, "--token-stdin") {
			t.Fatalf("expected the safe alternative to be named, got %q", output)
		}
	})

	t.Run("silent when no secret flag was used", func(t *testing.T) {
		cmd, stderr := newCommand()

		warnAboutSecretsOnTheCommandLine(cmd)

		if stderr.String() != "" {
			t.Fatalf("expected no warning, got %q", stderr.String())
		}
	})

	t.Run("survives a nil command", func(t *testing.T) {
		warnAboutSecretsOnTheCommandLine(nil)
	})
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

func TestLoginKeepsTheCommandLineWarningOffStdout(t *testing.T) {
	cmd, stdout, stderr := newLoginCommand(t)
	cmd.SetArgs([]string{"login", "https://flag-login.example.invalid", "--token", "tok", "--discover-aliases=false"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("login failed: %v (stderr: %s)", err, stderr.String())
	}

	// The warning must not reach stdout: under --json that is a machine
	// contract, and prose there makes the envelope unparseable.
	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not a clean envelope: %q (%v)", stdout.String(), err)
	}
	if !strings.Contains(stderr.String(), "process list") {
		t.Fatalf("expected the warning on stderr, got %q", stderr.String())
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
	if !strings.Contains(output, "--require-keyring") {
		t.Fatalf("expected the remedy named, got %q", output)
	}
}

func TestDescribeCredentialStorage(t *testing.T) {
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
	if got := storedConfigLocation(); strings.TrimSpace(got) == "" {
		t.Fatal("expected a non-empty location for the warning message")
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
