package auth

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTheCredentialHelperSaysWhyItCannotHelp is #567 in git's flows. With a
// config bb could not read, the helper printed nothing anywhere, and git simply
// prompted for a username -- "not logged in" again, for a damaged file.
//
// The protocol still holds: nothing on stdout and no failure, so git falls
// through to its other helpers. The reason goes to stderr, which git shows.
func TestTheCredentialHelperSaysWhyItCannotHelp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("hosts:\n\tbroken: {url: https://x}\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	t.Setenv("BB_CONFIG_PATH", path)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")

	cmd := newGitCredentialCommand()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetIn(strings.NewReader("protocol=https\nhost=bitbucket.example\n\n"))
	cmd.SetArgs([]string{"get"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("the helper failed git's lookup outright: %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("the helper wrote to the protocol stream: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), path) {
		t.Fatalf("the helper did not say which file it could not read; stderr: %q", stderr.String())
	}
}

// TestTheCredentialHelperRefusesPlaintextWhenKeyringRequired holds ADR-047's
// requirement where git reads a credential: under BB_REQUIRE_KEYRING or the
// require_keyring policy, a credential held in the plaintext fallback is not
// handed to git. The refusal is said on stderr, and the protocol still holds.
func TestTheCredentialHelperRefusesPlaintextWhenKeyringRequired(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	stored := strings.Join([]string{
		"hosts:",
		"  https://bitbucket.example.com:",
		"    url: https://bitbucket.example.com",
		"    username: alice",
		"insecure_secrets:",
		"  https://bitbucket.example.com:",
		"    token: plaintext-token",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(stored), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	t.Setenv("BB_CONFIG_PATH", path)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")
	t.Setenv("BB_SYSTEM_CONFIG_PATH", filepath.Join(t.TempDir(), "absent.yaml"))

	answer := func() (string, string) {
		cmd := newGitCredentialCommand()
		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}
		cmd.SetOut(stdout)
		cmd.SetErr(stderr)
		cmd.SetIn(strings.NewReader("protocol=https\nhost=bitbucket.example.com\n\n"))
		cmd.SetArgs([]string{"get"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("the helper failed git's lookup outright: %v", err)
		}
		return stdout.String(), stderr.String()
	}

	if out, _ := answer(); !strings.Contains(out, "password=plaintext-token") {
		t.Fatal("without the requirement the helper does not answer, so the check below proves nothing")
	}

	t.Setenv("BB_REQUIRE_KEYRING", "1")

	out, errOut := answer()
	if out != "" {
		t.Fatalf("the helper handed git a plaintext credential under the requirement (%d bytes on stdout)", len(out))
	}
	if !strings.Contains(errOut, "keyring") {
		t.Fatalf("the helper did not say why it cannot help; stderr: %q", errOut)
	}
	if strings.Contains(errOut, "plaintext-token") {
		t.Fatal("the refusal on stderr carries the credential")
	}
}
