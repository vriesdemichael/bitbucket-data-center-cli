package completion

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	bulkworkflow "github.com/vriesdemichael/bitbucket-data-center-cli/internal/workflows/bulk"
)

// TestTheBulkRunsOnThisMachineAreOfferedNewestFirst covers `bb bulk status
// <operation-id>`, where the id is random and the description is the only way
// to tell one run from the next.
func TestTheBulkRunsOnThisMachineAreOfferedNewestFirst(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("BB_BULK_STATUS_DIR", directory)

	store := bulkworkflow.NewStatusStore(directory)
	earlier := time.Date(2026, 9, 20, 9, 30, 0, 0, time.Local)
	later := time.Date(2026, 9, 21, 16, 5, 0, 0, time.Local)

	for _, run := range []struct {
		id      string
		at      time.Time
		summary bulkworkflow.ApplySummary
	}{
		{id: "op-1111", at: earlier, summary: bulkworkflow.ApplySummary{TargetCount: 12, FailedTargets: 3}},
		{id: "op-2222", at: later, summary: bulkworkflow.ApplySummary{TargetCount: 4}},
	} {
		status := bulkworkflow.ApplyStatus{
			APIVersion:  bulkworkflow.APIVersion,
			Kind:        bulkworkflow.ApplyStatusKind,
			OperationID: run.id,
			Summary:     run.summary,
		}
		if err := store.Save(status); err != nil {
			t.Fatalf("saving %s failed: %v", run.id, err)
		}
		if err := os.Chtimes(filepath.Join(directory, run.id+".json"), run.at, run.at); err != nil {
			t.Fatalf("dating %s failed: %v", run.id, err)
		}
	}

	result, err := bulkOperationSource(context.Background(), nil, Request{})
	if err != nil {
		t.Fatalf("listing the saved runs failed: %v", err)
	}

	if len(result.Candidates) != 2 || result.Candidates[0].Value != "op-2222" {
		t.Fatalf("expected the newest run first, got %v", result.Candidates)
	}
	if !result.KeepOrder {
		t.Error("expected the order kept; a shell sorting random hex would bury the run just applied")
	}
	if !strings.Contains(result.Candidates[0].Description, "4 targets succeeded") {
		t.Errorf("expected the clean run described as such, got %q", result.Candidates[0].Description)
	}
	if !strings.Contains(result.Candidates[1].Description, "3 of 12 targets failed") {
		t.Errorf("expected the partial failure counted, got %q", result.Candidates[1].Description)
	}
}

// TestARunIsDescribedByWhenAndHowItWent covers the wording at its edges: one
// target, every target failing, and a run from today, which names the time
// rather than the date.
func TestARunIsDescribedByWhenAndHowItWent(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 22, 18, 0, 0, 0, time.Local)
	run := func(at time.Time, targets, failed int) bulkworkflow.SavedStatus {
		return bulkworkflow.SavedStatus{
			ApplyStatus: bulkworkflow.ApplyStatus{Summary: bulkworkflow.ApplySummary{TargetCount: targets, FailedTargets: failed}},
			SavedAt:     at,
		}
	}

	for _, testCase := range []struct {
		name string
		run  bulkworkflow.SavedStatus
		want string
	}{
		{name: "one target", run: run(now.Add(-24*time.Hour), 1, 0), want: "2026-09-21 18:00, 1 target succeeded"},
		{name: "every target failed", run: run(now.Add(-48*time.Hour), 5, 5), want: "2026-09-20 18:00, all 5 targets failed"},
		{name: "today names the time", run: run(now.Add(-time.Hour), 2, 0), want: "today 17:00, 2 targets succeeded"},
	} {
		if got := describeRun(testCase.run, now); got != testCase.want {
			t.Errorf("%s: got %q, want %q", testCase.name, got, testCase.want)
		}
	}
}
