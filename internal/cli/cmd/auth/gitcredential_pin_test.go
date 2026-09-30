package auth

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
)

// writePinLogin writes a configuration file with one login for
// bitbucket.example.com, its secret in the file.
func writePinLogin(t *testing.T, username, token string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), username+".yaml")
	stored := strings.Join([]string{
		"hosts:",
		"  https://bitbucket.example.com:",
		"    url: https://bitbucket.example.com",
		"    username: " + username,
		"insecure_secrets:",
		"  https://bitbucket.example.com:",
		"    token: " + token,
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(stored), 0o600); err != nil {
		t.Fatalf("write login: %v", err)
	}

	return path
}

func runGitCredentialGet(t *testing.T, args ...string) (string, string) {
	t.Helper()

	cmd := newGitCredentialCommand()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetIn(strings.NewReader("protocol=https\nhost=bitbucket.example.com\n\n"))
	cmd.SetArgs(append([]string{"get"}, args...))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("git-credential get %v: %v", args, err)
	}

	return stdout.String(), stderr.String()
}

// TestGitCredentialAnswersFromTheConfigurationItNames is #733 from the
// helper's side: --config decides the login, whatever BB_CONFIG_PATH says.
func TestGitCredentialAnswersFromTheConfigurationItNames(t *testing.T) {
	alice := writePinLogin(t, "alice", "alice-token")
	bob := writePinLogin(t, "bob", "bob-token")

	// Set first, so the test's cleanup puts back what the process had: the
	// command sets it for the one request it answers.
	t.Setenv("BB_CONFIG_PATH", bob)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "0")

	if got, _ := runGitCredentialGet(t); !strings.Contains(got, "username=bob\n") {
		t.Fatalf("without --config the helper does not answer from BB_CONFIG_PATH, so the check below proves nothing: %q", got)
	}

	got, _ := runGitCredentialGet(t, "--config", alice)
	if !strings.Contains(got, "username=alice\n") || !strings.Contains(got, "password=alice-token\n") {
		t.Fatalf("with --config naming alice's file, the helper answered: %q", got)
	}
}

// A pinned file that has gone away gets silence, so git prompts rather than
// taking another login, and a line on stderr, which git shows, saying why.
func TestGitCredentialSaysWhenTheFileItNamesIsGone(t *testing.T) {
	t.Setenv("BB_CONFIG_PATH", writePinLogin(t, "bob", "bob-token"))
	t.Setenv("BB_DISABLE_STORED_CONFIG", "0")

	missing := filepath.Join(t.TempDir(), "gone.yaml")
	stdout, stderr := runGitCredentialGet(t, "--config", missing)

	if stdout != "" {
		t.Fatalf("a helper pinned to a missing file answered: %q", stdout)
	}
	if !strings.Contains(stderr, missing) || !strings.Contains(stderr, "bb auth setup-git") {
		t.Errorf("stderr does not name the file and the remedy: %q", stderr)
	}
}

// TestSetupGitNamesTheConfigurationInTheHelper is the line setup-git writes:
// the configuration this run reads, made absolute, since git runs the helper
// from wherever the repository is.
func TestSetupGitNamesTheConfigurationInTheHelper(t *testing.T) {
	t.Setenv("BB_CONFIG_PATH", "relative.yaml")
	absolute, err := filepath.Abs("relative.yaml")
	if err != nil {
		t.Fatalf("resolve the expected path: %v", err)
	}

	var captured string
	cmd := newSetupGitCommand(Dependencies{
		LoadConfig: func() (config.AppConfig, error) { return config.AppConfig{}, nil },
		ConfigureGitCredentialHelper: func(_ context.Context, _, value string, _, _ bool) error {
			captured = value
			return nil
		},
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--host", "https://bitbucket.example.com"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup-git: %v", err)
	}

	if want := fmt.Sprintf(" auth git-credential --config %q", absolute); !strings.HasSuffix(captured, want) {
		t.Fatalf("helper %q does not end with %q", captured, want)
	}
	if !strings.Contains(out.String(), absolute) {
		t.Errorf("the confirmation does not say which login git will use: %q", out.String())
	}
}

func TestReplaceableHelper(t *testing.T) {
	t.Parallel()

	const pinned = `!"/usr/local/bin/bb" auth git-credential --config "/home/a/.config/bb/config.yaml"`

	for _, testCase := range []struct {
		existing string
		want     bool
	}{
		{"", true},
		{pinned, true},
		{`!"/usr/local/bin/bb" auth git-credential`, true},
		{`!"C:\\Program Files\\bb\\bb.exe" auth git-credential`, true},
		{`!"/usr/local/bin/bb" auth git-credential --config "/home/b/other.yaml"`, false},
		{"!bb auth git-credential", true},
		{"manager", false},
		{`!"/usr/local/bin/bb" auth git-credential get`, false},
	} {
		if got := replaceableHelper(testCase.existing, pinned); got != testCase.want {
			t.Errorf("replaceableHelper(%q) = %v, want %v", testCase.existing, got, testCase.want)
		}
	}
}
