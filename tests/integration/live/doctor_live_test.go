//go:build live

package live_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// TestLiveDoctorReportsEveryProblemWithoutUsingTheHost runs bb doctor through
// the same command tree a user runs, against configuration files broken on
// purpose. It asks nothing of Bitbucket: the command exists for the moment the
// configuration does not load, and needing the server would defeat it.
func TestLiveDoctorReportsEveryProblemWithoutUsingTheHost(t *testing.T) {
	const secret = "live-doctor-token-7e3b"

	directory := t.TempDir()
	stored := filepath.Join(directory, "config.yaml")
	system := filepath.Join(directory, "system.yaml")
	for path, content := range map[string]string{
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

	t.Setenv("BB_DISABLE_STORED_CONFIG", "")
	t.Setenv("BB_CONFIG_PATH", stored)
	t.Setenv("BB_SYSTEM_CONFIG_PATH", system)
	t.Setenv("BB_WORKSPACE_CONFIG_PATH", filepath.Join(directory, "absent.yaml"))

	output, err := executeLiveCLI(t, "--json", "doctor")
	if err != nil {
		t.Fatalf("bb doctor --json reports its verdict in ok and exits zero, got %v\n%s", err, output)
	}
	if strings.Contains(output, secret) {
		t.Fatalf("bb doctor --json printed the plaintext token:\n%s", output)
	}

	var envelope struct {
		Data struct {
			OK    bool `json:"ok"`
			Files []struct {
				Tier       string `json:"tier"`
				Valid      bool   `json:"valid"`
				Violations []struct {
					Key string `json:"key"`
				} `json:"violations"`
				Secrets []struct {
					Host  string `json:"host"`
					Token bool   `json:"token"`
				} `json:"secrets"`
			} `json:"files"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("bb doctor --json did not emit one document: %v\n%s", err, output)
	}
	if envelope.Data.OK {
		t.Errorf("two invalid files reported ok:\n%s", output)
	}

	want := map[string][]string{
		"stored":    {"policies.require_keyrng", "unknown_key"},
		"workspace": {},
		"system":    {"allowed_hosts"},
	}
	for _, file := range envelope.Data.Files {
		keys := []string{}
		for _, violation := range file.Violations {
			keys = append(keys, violation.Key)
		}
		if !slices.Equal(keys, want[file.Tier]) || file.Valid != (len(want[file.Tier]) == 0) {
			t.Errorf("%s: valid=%v violations=%q, want %q", file.Tier, file.Valid, keys, want[file.Tier])
		}
		if file.Tier == "stored" && (len(file.Secrets) != 1 || !file.Secrets[0].Token) {
			t.Errorf("the stored file's plaintext token was not reported as present: %+v", file.Secrets)
		}
	}

	output, err = executeLiveCLI(t, "doctor")
	if !apperrors.IsKind(err, apperrors.KindPermanent) {
		t.Fatalf("an invalid file exits 1 as permanent, got %v\n%s", err, output)
	}
	for _, line := range []string{
		"line 3: policies.require_keyrng: unknown key",
		"line 7: unknown_key: unknown key",
		"line 1: allowed_hosts: got string, want array",
	} {
		if !strings.Contains(output, line) {
			t.Errorf("the report lacks %q:\n%s", line, output)
		}
	}
	if strings.Contains(output, secret) {
		t.Errorf("bb doctor printed the plaintext token:\n%s", output)
	}
}
