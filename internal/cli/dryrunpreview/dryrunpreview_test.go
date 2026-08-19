package dryrunpreview

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteText(t *testing.T) {
	preview := Preview{
		DryRun:       true,
		PlanningMode: PlanningModeStateful,
		Capability:   CapabilityFull,
		Items: []Item{
			{
				Intent:          "pr.create",
				Target:          map[string]any{"repository": "PROJ/repo", "args": []string{"--title", "Test"}},
				Action:          "create",
				PredictedAction: "create",
				Supported:       true,
				Reason:          "pr will be created",
			},
		},
		Summary: Summary{Total: 1, Supported: 1, CreateCount: 1},
	}

	buf := &bytes.Buffer{}
	if err := Write(buf, false, preview); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Dry-run (stateful, capability=full)") {
		t.Fatalf("unexpected header in output: %s", output)
	}
	if !strings.Contains(output, "intent=pr.create") {
		t.Fatalf("missing intent in output: %s", output)
	}
	if !strings.Contains(output, "repository=PROJ/repo") {
		t.Fatalf("missing repo in output: %s", output)
	}
}

func TestWriteJSON(t *testing.T) {
	preview := Preview{
		DryRun:       true,
		PlanningMode: PlanningModeStateful,
		Capability:   CapabilityFull,
		Items: []Item{
			{
				Intent: "pr.merge",
				Action: "update",
			},
		},
		Summary: Summary{Total: 1, Supported: 1},
	}

	buf := &bytes.Buffer{}
	if err := Write(buf, true, preview); err != nil {
		t.Fatalf("Write JSON failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, `"intent": "pr.merge"`) {
		t.Fatalf("missing json intent: %s", output)
	}
}
