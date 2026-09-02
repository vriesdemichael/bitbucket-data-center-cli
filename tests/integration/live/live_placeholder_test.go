//go:build live

package live_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli"
)

func TestLiveAuthStatusAndAdminHealth(t *testing.T) {
	t.Setenv("BITBUCKET_URL", "http://localhost:7990")

	command := cli.NewRootCommand()
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs([]string{"auth", "status"})

	if err := command.Execute(); err != nil {
		t.Fatalf("auth status failed: %v", err)
	}

	text := output.String()
	if !strings.Contains(text, "Target Bitbucket:") {
		t.Fatalf("expected auth status output, got: %s", text)
	}

	healthCommand := cli.NewRootCommand()
	healthOutput := &bytes.Buffer{}
	healthCommand.SetOut(healthOutput)
	healthCommand.SetErr(healthOutput)
	healthCommand.SetArgs([]string{"--json", "admin", "health"})

	if err := healthCommand.Execute(); err != nil {
		t.Fatalf("admin health failed: %v\noutput: %s", err, healthOutput.String())
	}

	var payload map[string]any
	if err := decodeJSONEnvelopeData(healthOutput.String(), &payload); err != nil {
		t.Fatalf("admin health returned invalid JSON: %v\noutput: %s", err, healthOutput.String())
	}

	healthy, ok := payload["healthy"].(bool)
	if !ok || !healthy {
		t.Fatalf("expected healthy=true, got: %#v", payload["healthy"])
	}
}
