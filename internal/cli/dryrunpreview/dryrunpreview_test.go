package dryrunpreview

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

type errWriter struct {
	errAfter int
	written  int
}

func (w *errWriter) Write(p []byte) (n int, err error) {
	if w.written >= w.errAfter {
		return 0, errors.New("write error")
	}
	w.written += len(p)
	return len(p), nil
}

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
			{
				Intent:    "simple.action",
				Target:    map[string]any{},
				Action:    "delete",
				Supported: true,
			},
		},
		Summary: Summary{Total: 2, Supported: 2, CreateCount: 1, DeleteCount: 1},
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
	if !strings.Contains(output, "args=--title Test") {
		t.Fatalf("missing args in output: %s", output)
	}
}

func TestWriteErrors(t *testing.T) {
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
	}

	// Test failures at various write stages
	for i := 0; i <= 200; i += 20 {
		ew := &errWriter{errAfter: i}
		_ = Write(ew, false, preview)
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
