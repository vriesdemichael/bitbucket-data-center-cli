//go:build live

package live_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// TestLiveDoctorReportsEveryProblemWithoutUsingTheHost runs bb doctor through
// the same command tree a user runs, over a configuration with nothing wrong
// and then over files broken on purpose. It asks nothing of Bitbucket: the
// command exists for the moment the configuration does not load, and needing
// the server would defeat it.
func TestLiveDoctorReportsEveryProblemWithoutUsingTheHost(t *testing.T) {
	const secret = "live-doctor-token-7e3b"

	directory := t.TempDir()
	emptySystem := filepath.Join(directory, "empty-system.yaml")
	stored := filepath.Join(directory, "config.yaml")
	system := filepath.Join(directory, "system.yaml")
	for path, content := range map[string]string{
		emptySystem: "",
		stored: strings.Join([]string{
			"default_host: https://bitbucket.invalid",
			"policies:",
			"  require_keyrng: true",
			"insecure_secrets:",
			"  https://bitbucket.invalid:",
			"    token: " + secret,
			"unknown_key: 1",
		}, "\n"),
		system: "allowed_hosts: not-a-list\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("BB_WORKSPACE_CONFIG_PATH", filepath.Join(directory, "absent.yaml"))

	// bb doctor also reports the shell completion and agent skills set up on
	// this machine, which a test about configuration files keeps out: a home
	// of its own, a working directory outside any checkout, whose skills
	// would be the project's, and a PATH with no shell on it. PowerShell finds
	// its profiles through the Documents folder rather than the home
	// directory, so it is kept from being asked at all.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, variable := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "ZDOTDIR", "BASH_COMPLETION_USER_DIR"} {
		t.Setenv(variable, "")
	}
	t.Chdir(t.TempDir())
	t.Setenv("PATH", t.TempDir())

	// "without using the host" was in the name and in nothing else. The host
	// bb doctor is pointed at for this test is a listener that fails the test
	// if anything arrives, which is the only way to assert that no request was
	// made: a server cannot be asked whether it was left alone (ADR-079).
	unreached := httptest.NewServer(testsupport.UnreachedHandler(t))
	t.Cleanup(unreached.Close)
	t.Setenv("BITBUCKET_URL", unreached.URL)

	// Nothing to fix: the report, and exit zero.
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BB_SYSTEM_CONFIG_PATH", emptySystem)

	output, err := executeLiveCLI(t, "--json", "doctor")
	if err != nil {
		t.Fatalf("a configuration with nothing to fix exits zero, got %v (details %v)\n%s", err, apperrors.DetailsOf(err), output)
	}
	var report struct {
		Data struct {
			Files []struct {
				Tier string `json:"tier"`
			} `json:"files"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &report); err != nil || len(report.Data.Files) != 3 {
		t.Fatalf("bb doctor --json did not report the three files: %v\n%s", err, output)
	}

	// Broken files: exit 1, and under --json nothing but the failure envelope.
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")
	t.Setenv("BB_CONFIG_PATH", stored)
	t.Setenv("BB_SYSTEM_CONFIG_PATH", system)

	output, err = executeLiveCLI(t, "--json", "doctor")
	var failure *apperrors.AppError
	if !errors.As(err, &failure) || failure.Kind != apperrors.KindPermanent || apperrors.ExitCode(err) != 1 {
		t.Fatalf("issues exit 1 as permanent, got %v", err)
	}
	if strings.TrimSpace(output) != "" {
		t.Errorf("under --json a run with issues wrote a report beside its failure:\n%s", output)
	}

	written := &bytes.Buffer{}
	if err := jsonoutput.WriteError(written, failure); err != nil {
		t.Fatal(err)
	}
	var envelope jsonoutput.ErrorEnvelope
	if err := json.Unmarshal(written.Bytes(), &envelope); err != nil {
		t.Fatalf("the failure envelope is not JSON: %v\n%s", err, written)
	}
	for _, key := range []string{
		"violation/stored/policies/require_keyrng",
		"violation/stored/unknown_key",
		"ignored/stored/policies",
		"violation/system/allowed_hosts",
	} {
		if envelope.Error.Details[key] == "" {
			t.Errorf("error.details does not name %s: %#v", key, envelope.Error.Details)
		}
	}
	if strings.Contains(written.String(), secret) {
		t.Errorf("the failure envelope carries the plaintext token:\n%s", written)
	}

	output, err = executeLiveCLI(t, "doctor")
	if !apperrors.IsKind(err, apperrors.KindPermanent) {
		t.Fatalf("issues exit 1 as permanent, got %v\n%s", err, output)
	}
	for _, line := range []string{
		"line 3: policies.require_keyrng: unknown key",
		"line 7: unknown_key: unknown key",
		"line 1: allowed_hosts: got string, want array",
		"4 issues to fix.",
	} {
		if !strings.Contains(output, line) {
			t.Errorf("the report lacks %q:\n%s", line, output)
		}
	}
	if strings.Contains(output, secret) {
		t.Errorf("bb doctor printed the plaintext token:\n%s", output)
	}
}
