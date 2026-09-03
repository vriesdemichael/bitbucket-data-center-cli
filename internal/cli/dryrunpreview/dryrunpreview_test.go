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

// TestNewDerivesTheSummaryFromTheItems is the point of the builder.
//
// The summary was written by hand at every site and could disagree with the
// items it summarised. An agent reads this report to decide whether to
// proceed, so a preview claiming one supported update while carrying a blocked
// item is worse than no preview at all.
func TestNewDerivesTheSummaryFromTheItems(t *testing.T) {
	t.Parallel()

	preview := New(PlanningModeStateful, CapabilityFull,
		Item{Intent: "a", PredictedAction: PredictedCreate, Supported: true},
		Item{Intent: "b", PredictedAction: PredictedUpdate, Supported: true},
		Item{Intent: "c", PredictedAction: PredictedUpdate, Supported: true},
		Item{Intent: "d", PredictedAction: PredictedDelete, Supported: true},
		Item{Intent: "e", PredictedAction: PredictedNoop, Supported: true},
		Item{Intent: "f", PredictedAction: PredictedConflict, Supported: true},
		Item{Intent: "g", PredictedAction: PredictedBlocked, Supported: false},
	)

	if !preview.DryRun {
		t.Error("a preview exists because --dry-run was passed; DryRun must be true")
	}
	want := Summary{
		Total: 7, Supported: 6, Unsupported: 1,
		CreateCount: 1, UpdateCount: 2, DeleteCount: 1, NoopCount: 1,
		// conflict and blocked both mean no mutation happens.
		UnknownCount: 2,
	}
	if preview.Summary != want {
		t.Errorf("summary = %+v, want %+v", preview.Summary, want)
	}
}

// TestNewCountsAnUnrecognisedPredictionAsUnknown keeps a typo from reading as a
// mutation that will not happen.
//
// "noop" instead of "no-op" looks the same to a person. It must not silently
// land in NoopCount, and it must not be dropped from the tally either -- the
// totals have to keep adding up.
func TestNewCountsAnUnrecognisedPredictionAsUnknown(t *testing.T) {
	t.Parallel()

	preview := New(PlanningModeStateful, CapabilityPartial,
		Item{Intent: "typo", PredictedAction: "noop", Supported: true},
	)

	if preview.Summary.NoopCount != 0 {
		t.Errorf("a misspelled prediction counted as a no-op: %+v", preview.Summary)
	}
	if preview.Summary.UnknownCount != 1 {
		t.Errorf("unknownCount = %d, want the unrecognised prediction counted", preview.Summary.UnknownCount)
	}
	counted := preview.Summary.CreateCount + preview.Summary.UpdateCount +
		preview.Summary.DeleteCount + preview.Summary.NoopCount + preview.Summary.UnknownCount
	if counted != preview.Summary.Total {
		t.Errorf("the per-action counts sum to %d but total is %d", counted, preview.Summary.Total)
	}
}

// TestNewOfNothingIsAnEmptyPreview covers the command that finds nothing to do.
func TestNewOfNothingIsAnEmptyPreview(t *testing.T) {
	t.Parallel()

	preview := New(PlanningModeStatic, CapabilityFull)
	if preview.Summary != (Summary{}) {
		t.Errorf("summary = %+v, want zero", preview.Summary)
	}
	if len(preview.Items) != 0 {
		t.Errorf("items = %+v", preview.Items)
	}
}

// TestConfidenceIsDerivedFromTheTier is #483.
//
// Capability and Confidence were free-text labels written at each of a hundred
// and three construction sites, so nothing related the claim to what the code
// had done. #479 is what that permitted: the strongest claim the contract
// offers, on a prediction made from one state field, on the irreversible
// pull request operation.
func TestConfidenceIsDerivedFromTheTier(t *testing.T) {
	t.Parallel()

	t.Run("only a checked tier earns full", func(t *testing.T) {
		t.Parallel()

		for tier, want := range map[Tier]string{
			TierServerValidated:      CapabilityFull,
			TierPreconditionsChecked: CapabilityFull,
			TierPredicted:            CapabilityPartial,
		} {
			if got := tier.Confidence(); got != want {
				t.Errorf("%s.Confidence() = %q, want %q", tier, got, want)
			}
		}
	})

	t.Run("an unstated tier is predicted, not full", func(t *testing.T) {
		t.Parallel()

		// The default has to be the modest one. A site that cannot say what it
		// checked did not check enough to claim full.
		preview := New(PlanningModeStateful, CapabilityFull, Item{Intent: "x", Action: "update", PredictedAction: PredictedUpdate, Supported: true})
		if preview.Items[0].Tier != TierPredicted {
			t.Errorf("tier = %q, want predicted", preview.Items[0].Tier)
		}
		if preview.Items[0].Confidence != CapabilityPartial {
			t.Errorf("confidence = %q, want partial", preview.Items[0].Confidence)
		}
	})

	t.Run("a hand-written confidence cannot outrank its tier", func(t *testing.T) {
		t.Parallel()

		// The defect this closes: claiming full for a prediction the tier does
		// not support. The label is computed, so the claim cannot be typed.
		preview := New(PlanningModeStateful, CapabilityFull, Item{
			Intent:          "x",
			Action:          "update",
			PredictedAction: PredictedUpdate,
			Supported:       true,
			Tier:            TierPredicted,
			Confidence:      CapabilityFull,
		})
		if preview.Items[0].Confidence != CapabilityPartial {
			t.Errorf("confidence = %q; a predicted item claimed full", preview.Items[0].Confidence)
		}
	})

	t.Run("an unrecognised tier is treated as predicted", func(t *testing.T) {
		t.Parallel()

		if got := Tier("invented-later").Confidence(); got != CapabilityPartial {
			t.Errorf("confidence = %q, want partial for an unknown tier", got)
		}
	})
}

// TestNewDoesNotReachIntoTheCallersSlice pins that building a preview leaves
// the caller's items alone.
//
// New derives each item's confidence from its tier. Doing that in place would
// edit the caller's backing array when called as New(..., items...), and leave
// preview.Items aliasing it -- so a later append would reach inside a preview
// already built.
func TestNewDoesNotReachIntoTheCallersSlice(t *testing.T) {
	t.Parallel()

	items := []Item{{Intent: "x", Action: "update", PredictedAction: PredictedUpdate, Supported: true}}

	preview := New(PlanningModeStateful, CapabilityFull, items...)

	if items[0].Tier != "" {
		t.Errorf("the caller's item gained a tier: %q", items[0].Tier)
	}
	if items[0].Confidence != "" {
		t.Errorf("the caller's item gained a confidence: %q", items[0].Confidence)
	}
	if preview.Items[0].Confidence != CapabilityPartial {
		t.Errorf("the preview's own item was not completed: %+v", preview.Items[0])
	}

	// And the preview does not share storage with the caller.
	items[0].Intent = "mutated after the preview was built"
	if preview.Items[0].Intent != "x" {
		t.Errorf("preview.Items aliases the caller's slice: %q", preview.Items[0].Intent)
	}
}
