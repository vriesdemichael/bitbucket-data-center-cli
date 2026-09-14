//go:build live

package live_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
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
