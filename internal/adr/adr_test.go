package adr

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const aRecord = `---
search:
  boost: 0.3
---

# ADR-084: A removed flag warns for one major before it stops working

> Changed in part by [ADR-096](096-x.md).

A flag keeps working until the next major.

It warns on stderr.

## Not chosen

- **Keep it forever**: the shim outlives the migration.
`

func TestParseReadsEachPart(t *testing.T) {
	t.Parallel()

	record, err := Parse("docs/site/adr/084-a-removed-flag.md", []byte(aRecord))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if record.Number != 84 || record.Title != "A removed flag warns for one major before it stops working" {
		t.Errorf("number and title are %d and %q", record.Number, record.Title)
	}
	if record.Standing != "Changed in part by [ADR-096](096-x.md)." {
		t.Errorf("standing is %q", record.Standing)
	}
	if record.Body != "A flag keeps working until the next major.\n\nIt warns on stderr." {
		t.Errorf("body is %q", record.Body)
	}
	if record.NotChosen != "- **Keep it forever**: the shim outlives the migration." {
		t.Errorf("not chosen is %q", record.NotChosen)
	}
	if !record.InForce() {
		t.Error("a record changed in part is still in force")
	}
}

func TestParseRefusesWhatIsNotARecord(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct{ path, content string }{
		"a name without a number":          {"docs/site/adr/removed-flag.md", aRecord},
		"a heading with another number":    {"docs/site/adr/085-a-removed-flag.md", aRecord},
		"no heading":                       {"docs/site/adr/084-x.md", "A flag keeps working.\n"},
		"a heading without a number":       {"docs/site/adr/084-x.md", "# A removed flag\n\nText.\n"},
		"a heading and no rule":            {"docs/site/adr/084-x.md", "# ADR-084: A title\n\n## Not chosen\n\n- **x**: y\n"},
		"an upper-case letter in the name": {"docs/site/adr/084-A-flag.md", aRecord},
	} {
		if _, err := Parse(testCase.path, []byte(testCase.content)); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
}

func TestInForceLeavesOutReplacedAndProposedRecords(t *testing.T) {
	t.Parallel()

	for standing, want := range map[string]bool{
		"":                                    true,
		"Changed in part by [ADR-096](x.md).": true,
		"Replaced by [ADR-065](x.md).":        false,
		"Proposed, and not yet in force.":     false,
	} {
		if got := (Record{Standing: standing}).InForce(); got != want {
			t.Errorf("%q: in force = %t, want %t", standing, got, want)
		}
	}
}

func TestLoadRefusesTwoRecordsWithOneNumber(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	for _, name := range []string{"084-one.md", "084-two.md"} {
		content := strings.Replace(aRecord, "A removed flag", name, 1)
		if err := os.WriteFile(filepath.Join(directory, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := Load(directory); err == nil || !strings.Contains(err.Error(), "both claim ADR-084") {
		t.Errorf("err = %v, want both files named", err)
	}
}

func TestTheRepositorysRecordsLoad(t *testing.T) {
	t.Parallel()

	records, err := Load(filepath.Join("..", "..", Directory))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(records) < 50 {
		t.Fatalf("loaded %d records; the reader has stopped matching them", len(records))
	}
}
