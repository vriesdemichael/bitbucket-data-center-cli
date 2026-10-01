package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTheIndexListsEveryRecordInNumberOrder(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	records := map[string]string{
		"001-first.md": "# ADR-001: First\n\nUse thing A.\n",
		// A gap where a deleted record was: numbers are never reused.
		"003-third.md": "# ADR-003: Third\n\nUse thing C.\n",
		"010-tenth.md": "# ADR-010: Tenth\n\nUse thing J.\n",
	}
	for name, content := range records {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := writeIndex(directory); err != nil {
		t.Fatalf("writeIndex: %v", err)
	}
	index, err := os.ReadFile(filepath.Join(directory, "index.md"))
	if err != nil {
		t.Fatal(err)
	}

	want := "- [ADR-001: First](001-first.md)\n- [ADR-003: Third](003-third.md)\n- [ADR-010: Tenth](010-tenth.md)\n"
	if !strings.Contains(string(index), want) {
		t.Errorf("the index does not list the records in number order:\n%s", index)
	}
}
