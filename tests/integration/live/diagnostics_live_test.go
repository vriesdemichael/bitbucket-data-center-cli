//go:build live

package live_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli"
)

func TestLiveDiagnosticsJSONLOnStderr(t *testing.T) {
	harness := newLiveHarness(t)
	configureLiveCLIEnv(t, harness, harness.config.ProjectKey, "")
	t.Setenv("BB_LOG_LEVEL", "debug")
	t.Setenv("BB_LOG_FORMAT", "jsonl")

	command := cli.NewRootCommand()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	command.SetOut(stdout)
	command.SetErr(stderr)
	command.SetArgs([]string{"admin", "health"})

	if err := command.Execute(); err != nil {
		t.Fatalf("admin health failed: %v\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}

	if !strings.Contains(stdout.String(), "Bitbucket health: OK") {
		t.Fatalf("expected command result on stdout, got: %s", stdout.String())
	}

	lines := strings.Split(strings.TrimSpace(stderr.String()), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		t.Fatalf("expected diagnostics output on stderr, got empty output")
	}

	foundRequestEvent := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var payload map[string]any
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			t.Fatalf("expected jsonl diagnostics line, got %q (%v)", line, err)
		}

		if endpoint, ok := payload["endpoint"].(string); ok && endpoint == "/rest/api/1.0/projects" {
			if _, ok := payload["duration_ms"]; !ok {
				t.Fatalf("expected duration_ms in diagnostics payload: %v", payload)
			}
			if _, ok := payload["attempt"]; !ok {
				t.Fatalf("expected attempt in diagnostics payload: %v", payload)
			}
			foundRequestEvent = true
		}
	}

	if !foundRequestEvent {
		t.Fatalf("expected diagnostics event for health endpoint, stderr=%s", stderr.String())
	}
}
