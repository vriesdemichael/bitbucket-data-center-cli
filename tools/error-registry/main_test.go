package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestTheRegistryShowsTheKindBBDecides checks every committed row's bbKind
// against the mapping it documents.
//
// The column is written when the registry is regenerated, which takes a live
// run, and the mapping changes between runs. When bb began reading the
// exception on a 400, the committed row for a branch name that is already taken
// went on saying validation while bb reported conflict. A row that disagrees
// with bb fails here, named by its status and exception.
func TestTheRegistryShowsTheKindBBDecides(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "quality", "bitbucket-error-registry.json"))
	if err != nil {
		t.Fatalf("read the registry: %v", err)
	}

	var committed registry
	if err := json.Unmarshal(raw, &committed); err != nil {
		t.Fatalf("decode the registry: %v", err)
	}
	if len(committed.Entries) == 0 {
		t.Fatal("the registry has no entries, so nothing was checked")
	}

	for _, row := range committed.Entries {
		if want := kindFor(row.Status, row.Exception); row.Kind != want {
			t.Errorf("%d %s: the registry says %s, bb decides %s", row.Status, row.Exception, row.Kind, want)
		}
	}
}
