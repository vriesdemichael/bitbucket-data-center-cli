package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTheIndexListsEveryRecordAndSaysWhichNoLongerHold(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	records := map[string]string{
		"001-first.md":  "# ADR-001: First\n\nUse thing A.\n",
		"002-second.md": "# ADR-002: Second\n\n> Replaced by [ADR-003](003-third.md).\n\nUse thing B.\n",
		"003-third.md":  "# ADR-003: Third\n\n> Replaces [ADR-002](002-second.md).\n\nUse thing C.\n",
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

	for _, want := range []string{
		"- [ADR-001: First](001-first.md)\n",
		"- [ADR-002: Second](002-second.md) (replaced by ADR-003)\n",
		"- [ADR-003: Third](003-third.md)\n",
	} {
		if !strings.Contains(string(index), want) {
			t.Errorf("the index lacks %q:\n%s", want, index)
		}
	}
	if strings.Index(string(index), "ADR-001") > strings.Index(string(index), "ADR-003: Third") {
		t.Error("the index is not in number order")
	}
}
