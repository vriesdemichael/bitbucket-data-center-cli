package dryrunpreview

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
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

// merge is a pull request merge as pr merge checks it, with the outcome
// varied by each test.
func merge(predicted string, tier Tier) Item {
	return Item{
		Intent:          "pr.merge",
		Target:          map[string]any{"repository": "PROJ/app", "id": 42},
		Action:          PredictedUpdate,
		PredictedAction: predicted,
		Reason:          "pull request cannot be merged",
		Tier:            tier,
		BlockingReasons: []string{"Requires 2 approvals; it has 1", "Build \"ci\" has not passed"},
	}
}

// TestDocumentReportsOneEffectPerItemWithItsOutcome is the mapping from what a
// command checked onto what the document says (ADR-096).
func TestDocumentReportsOneEffectPerItemWithItsOutcome(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		predicted string
		want      jsonoutput.Outcome
	}{
		{PredictedCreate, jsonoutput.OutcomeWouldApply},
		{PredictedUpdate, jsonoutput.OutcomeWouldApply},
		{PredictedDelete, jsonoutput.OutcomeWouldApply},
		{PredictedNoop, jsonoutput.OutcomeNoOp},
		{PredictedBlocked, jsonoutput.OutcomeWouldFail},
		{PredictedConflict, jsonoutput.OutcomeWouldFail},
		// A prediction nobody taught this mapping is not claimed to fail.
		{"replace", jsonoutput.OutcomeWouldApply},
	} {
		t.Run(testCase.predicted, func(t *testing.T) {
			t.Parallel()

			document := New(merge(testCase.predicted, TierPreconditionsChecked)).Document()
			if len(document.Effects) != 1 {
				t.Fatalf("effects = %v, want one", document.Effects)
			}
			effect := document.Effects[0]
			if effect.Outcome != testCase.want || effect.Action != "update" || effect.Target["repository"] != "PROJ/app" {
				t.Fatalf("effect = %+v, want outcome %s on the pull request", effect, testCase.want)
			}
			if failed := document.Error != nil; failed != (testCase.want == jsonoutput.OutcomeWouldFail) {
				t.Fatalf("error = %+v; present exactly when the run would fail", document.Error)
			}
		})
	}
}

// TestAWouldFailEffectCarriesWhatStopsIt: the reasons of a run that would fail
// are its blockers, and the preview's error is what the real run fails with.
func TestAWouldFailEffectCarriesWhatStopsIt(t *testing.T) {
	t.Parallel()

	document := New(merge(PredictedBlocked, TierPreconditionsChecked)).Document()

	reasons := document.Effects[0].Reasons
	if len(reasons) != 2 || reasons[0] != "Requires 2 approvals; it has 1" {
		t.Fatalf("reasons = %q, want the blockers", reasons)
	}
	if document.Error == nil || document.Error.Kind != "conflict" || document.Error.ExitCode != 5 ||
		document.Error.Message != "pull request cannot be merged" {
		t.Fatalf("error = %+v, want the refusal as a conflict, exit 5", document.Error)
	}

	missing := merge(PredictedBlocked, TierPreconditionsChecked)
	missing.Fails = apperrors.KindNotFound
	missing.BlockingReasons = nil
	document = New(missing).Document()
	if document.Error == nil || document.Error.Kind != "not_found" || document.Error.ExitCode != 4 {
		t.Fatalf("error = %+v, want the kind the item names", document.Error)
	}
	if reasons := document.Effects[0].Reasons; len(reasons) != 1 || reasons[0] != "pull request cannot be merged" {
		t.Fatalf("reasons = %q, want the reason when nothing blocks by name", reasons)
	}
}

// TestTheTierIsTheWeakestCheckBehindTheVerdict: one prediction from partial
// state makes the whole preview a prediction, and an item that states no tier
// claims nothing (ADR-078).
func TestTheTierIsTheWeakestCheckBehindTheVerdict(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name  string
		items []Item
		want  Tier
	}{
		{"one server-validated", []Item{merge(PredictedUpdate, TierServerValidated)}, TierServerValidated},
		{"weakest wins", []Item{merge(PredictedUpdate, TierServerValidated), merge(PredictedUpdate, TierPredicted)}, TierPredicted},
		{"checked beside validated", []Item{merge(PredictedUpdate, TierPreconditionsChecked), merge(PredictedUpdate, TierServerValidated)}, TierPreconditionsChecked},
		{"no tier stated", []Item{merge(PredictedUpdate, "")}, TierPredicted},
		{"nothing checked", nil, TierPredicted},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := New(testCase.items...).Document().Tier; got != testCase.want {
				t.Fatalf("tier = %s, want %s", got, testCase.want)
			}
		})
	}
}

// TestNewDoesNotReachIntoTheCallersSlice pins that building a preview leaves
// the caller's items alone, and the preview unchanged by what the caller does
// with them afterwards.
func TestNewDoesNotReachIntoTheCallersSlice(t *testing.T) {
	t.Parallel()

	items := []Item{merge(PredictedUpdate, TierServerValidated)}
	preview := New(items...)
	items[0].PredictedAction = PredictedBlocked

	if document := preview.Document(); document.Error != nil {
		t.Fatalf("the preview changed with the caller's slice: %+v", document)
	}
}

// TestWriteMachineIsThePreviewMemberAndExitsZero: under --json the verdict is
// in the document, so even a predicted failure exits 0 (ADR-096).
func TestWriteMachineIsThePreviewMemberAndExitsZero(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	writer := jsonoutput.Bind(&out, jsonoutput.Settings{Machine: true, Mode: jsonoutput.ModeDryRun, Command: "pr merge"})
	if err := Write(writer, true, New(merge(PredictedBlocked, TierPreconditionsChecked))); err != nil {
		t.Fatalf("Write returned %v, want nil: the verdict is in the document", err)
	}

	var document map[string]json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &document); err != nil {
		t.Fatalf("not one JSON document: %v\n%s", err, out.String())
	}
	if _, present := document["preview"]; !present || len(document) != 2 {
		t.Fatalf("members = %v, want preview and meta only", document)
	}
	if !strings.Contains(string(document["meta"]), `"command": "pr merge"`) {
		t.Fatalf("meta = %s, want the command", document["meta"])
	}
}

// TestWriteTextSaysWhyAndExitsWithTheRealRunsCode: a person sees the verdict
// and every reason, and `bb pr merge 42 --dry-run && bb pr merge 42` stops.
func TestWriteTextSaysWhyAndExitsWithTheRealRunsCode(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	writer := jsonoutput.Bind(&out, jsonoutput.Settings{Mode: jsonoutput.ModeDryRun, Command: "pr merge"})
	err := Write(writer, false, New(merge(PredictedBlocked, TierPreconditionsChecked)))

	var state *apperrors.StateExit
	if !errors.As(err, &state) || state.Code != 5 {
		t.Fatalf("Write returned %v, want an exit with the real run's code, 5", err)
	}

	text := out.String()
	for _, want := range []string{
		"Dry run: bb pr merge would fail (preconditions-checked)",
		"update PROJ/app id=42: would fail",
		"Requires 2 approvals; it has 1",
		`Build "ci" has not passed`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text lacks %q:\n%s", want, text)
		}
	}

	out.Reset()
	if err := Write(writer, false, New(merge(PredictedNoop, TierPreconditionsChecked))); err != nil {
		t.Fatalf("a no-op returned %v, want nil", err)
	}
	if !strings.Contains(out.String(), "would change nothing") {
		t.Fatalf("a no-op reads:\n%s", out.String())
	}
}

// TestWriteTextOfAFailureFoundBeforeAnyEffect prints its message, since there
// is no effect to hang it on.
func TestWriteTextOfAFailureFoundBeforeAnyEffect(t *testing.T) {
	t.Parallel()

	failure := jsonoutput.EnvelopeErrorOf(apperrors.New(apperrors.KindNotFound, "pull request 42 not found", nil))
	var out bytes.Buffer
	if err := WriteText(&out, "pr merge", jsonoutput.Preview{Tier: TierServerValidated, Effects: []jsonoutput.Effect{}, Error: &failure}); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	if !strings.Contains(out.String(), "would fail (server-validated)") || !strings.Contains(out.String(), "pull request 42 not found") {
		t.Fatalf("text:\n%s", out.String())
	}
}

func TestWriteTextReportsWriteErrors(t *testing.T) {
	t.Parallel()

	for after := 0; after < 3; after++ {
		err := Write(&errWriter{errAfter: after}, false, New(merge(PredictedBlocked, TierPreconditionsChecked)))
		var state *apperrors.StateExit
		if err == nil || errors.As(err, &state) {
			t.Fatalf("a failed write after %d bytes returned %v, want the write error", after, err)
		}
	}
}
