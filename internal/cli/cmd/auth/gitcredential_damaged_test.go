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
