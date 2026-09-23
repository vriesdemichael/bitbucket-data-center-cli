package bulk

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestRecentListsRunsNewestFirst covers the order shell completion shows runs
// in, which is the whole of what makes one random operation id tell apart from
// another: the one you just applied is the one you are about to ask about.
func TestRecentListsRunsNewestFirst(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	store := NewStatusStore(directory)

	base := time.Date(2026, 9, 22, 14, 0, 0, 0, time.UTC)
	for _, id := range []string{"op-oldest", "op-newest", "op-middle"} {
		if err := store.Save(ApplyStatus{APIVersion: APIVersion, Kind: ApplyStatusKind, OperationID: id}); err != nil {
			t.Fatalf("saving %s failed: %v", id, err)
		}

		stamp := map[string]time.Time{
			"op-oldest": base,
			"op-middle": base.Add(time.Hour),
			"op-newest": base.Add(2 * time.Hour),
		}[id]
		if err := os.Chtimes(filepath.Join(directory, id+".json"), stamp, stamp); err != nil {
			t.Fatalf("dating %s failed: %v", id, err)
		}
	}

	saved, err := store.Recent(0)
	if err != nil {
		t.Fatalf("listing the saved runs failed: %v", err)
	}

	got := []string{}
	for _, run := range saved {
		got = append(got, run.OperationID)
	}
	want := []string{"op-newest", "op-middle", "op-oldest"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}

	limited, err := store.Recent(2)
	if err != nil || len(limited) != 2 || limited[0].OperationID != "op-newest" {
		t.Errorf("expected the two newest runs, got %v (err %v)", limited, err)
	}
}

// TestRecentLeavesOutWhatDoesNotLoad keeps one bad file from hiding every
// good one.
//
// The directory can hold a status written by a bb with another schema, or a
// file that is not ours at all. Neither is a reason to stop offering the runs
// that do load.
func TestRecentLeavesOutWhatDoesNotLoad(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	store := NewStatusStore(directory)

	if err := store.Save(ApplyStatus{APIVersion: APIVersion, Kind: ApplyStatusKind, OperationID: "op-good"}); err != nil {
		t.Fatalf("saving failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "op-garbled.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("writing the garbled file failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "notes.txt"), []byte("not a status"), 0o600); err != nil {
		t.Fatalf("writing the stray file failed: %v", err)
	}

	saved, err := store.Recent(0)
	if err != nil {
		t.Fatalf("expected the loadable run despite the bad files, got error %v", err)
	}
	if len(saved) != 1 || saved[0].OperationID != "op-good" {
		t.Errorf("expected only op-good, got %v", saved)
	}
}

// TestRecentBeforeTheFirstApplyIsNothing covers a machine that has never run a
// bulk apply, where the directory does not exist yet.
func TestRecentBeforeTheFirstApplyIsNothing(t *testing.T) {
	t.Parallel()

	saved, err := NewStatusStore(filepath.Join(t.TempDir(), "never-created")).Recent(10)
	if err != nil {
		t.Fatalf("expected no runs and no error, got %v", err)
	}
	if len(saved) != 0 {
		t.Errorf("expected no runs, got %v", saved)
	}
}
