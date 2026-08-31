package gateparity

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"testing"
)

// decisionsDirectory holds the decision records this test reads.
const decisionsDirectory = "docs/decisions"

// TestEveryADRCrossReferenceResolves fails when a record points at a record
// that does not exist.
//
// The records cite each other constantly -- to defer to an earlier decision, to
// note what superseded them, to say where a value actually lives. Each citation
// is a claim that the cited record is there to be read, and nothing checked it.
//
// ADR-065 shipped saying "Tracked as issue 462; ADR-064 depends on it" when
// ADR-064 had never existed: the number is a gap in the sequence, never
// allocated and never committed. A reader following that pointer finds nothing
// and cannot tell whether the record was withdrawn, renumbered, or never
// written. This test was written against that defect and failed on it before
// it was fixed.
//
// That is the ADR-039 failure -- a record naming something absent -- in the one
// direction the existing guards do not look.
// TestGovernanceTestsNamedInThisRecordExist checks that ADR-067 names real
// tests, and TestADRDoesNotNameToolsThatDoNotExist checks that a record does
// not name a removed MCP tool. Neither checks that a record names a real
// record.
//
// Both spellings are checked because both are load-bearing: the prose form
// ADR-NNN is how a reader is sent somewhere, and superseded_by is what the
// export tool follows when it renders the site.
//
// Every ADR-NNN in a record is read as a live citation. The check cannot tell
// one from a record that is discussing an absent record, so a record needing to
// do the latter describes it rather than spelling the number. If that case
// becomes common, an exemption keyed by file and number is the pattern this
// repository already uses elsewhere.
func TestEveryADRCrossReferenceResolves(t *testing.T) {
	records := filepath.Join(repositoryRoot(t), decisionsDirectory)

	existing := recordNumbersIn(t, records)
	if len(existing) < 50 {
		t.Fatalf("found %d records, expected at least fifty; the parser has stopped matching", len(existing))
	}

	for _, dangling := range unresolved(existing, crossReferencesIn(t, records)) {
		t.Errorf(
			"%s cites ADR-%03d (%s), which does not exist.\n"+
				"Either the number is wrong, or the record it points at was never written.\n"+
				"A pointer to a missing record tells a reader nothing and cannot be followed.",
			dangling.file, dangling.number, dangling.form,
		)
	}
}

// TestCrossReferenceCheckDetectsAMissingRecord is the sabotage, kept as a test.
//
// ADR-067 requires breaking a guard before trusting it. The guard above was
// broken by the repository itself and observed to fail, but that defect is now
// fixed, so nothing would notice if the check stopped checking. This holds a
// citation of a record that was never written and asserts it is still caught.
func TestCrossReferenceCheckDetectsAMissingRecord(t *testing.T) {
	directory := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(directory, name), []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	write("001-present.yaml", "number: 1\ntitle: Present\n")
	write("002-cites-a-gap.yaml", "number: 2\ntitle: Cites a gap\ndecision: >\n  See ADR-099 for the rest.\n")
	write("003-superseded-by-a-gap.yaml", "number: 3\ntitle: Superseded by a gap\nsuperseded_by: 098\n")

	existing := recordNumbersIn(t, directory)
	dangling := unresolved(existing, crossReferencesIn(t, directory))

	caught := map[int]bool{}
	for _, item := range dangling {
		caught[item.number] = true
	}

	if !caught[99] {
		t.Error("a prose citation of a record that does not exist was not caught")
	}
	if !caught[98] {
		t.Error("a superseded_by pointing at a record that does not exist was not caught")
	}
	if len(dangling) != 2 {
		t.Errorf("caught %d dangling references, want exactly the two planted: %v", len(dangling), dangling)
	}
}

// reference is one citation of a record by another.
type reference struct {
	file   string
	number int
	form   string
}

// unresolved returns the citations with no record behind them.
func unresolved(existing map[int]struct{}, references []reference) []reference {
	dangling := []reference{}
	for _, item := range references {
		if _, ok := existing[item.number]; !ok {
			dangling = append(dangling, item)
		}
	}
	return dangling
}

// recordNumbersIn collects the number every record declares for itself.
//
// The declared number is used rather than the filename prefix because the
// number field is what the export tool and the site index read; a record whose
// filename and number disagree is a separate problem and not this test's.
func recordNumbersIn(t *testing.T, directory string) map[int]struct{} {
	t.Helper()

	declaration := regexp.MustCompile(`^number:\s*([0-9]+)\s*$`)
	numbers := map[int]struct{}{}

	for _, path := range recordPaths(t, directory) {
		for _, line := range readLines(t, path) {
			if match := declaration.FindStringSubmatch(line); match != nil {
				numbers[recordNumber(t, path, match[1])] = struct{}{}
				break
			}
		}
	}

	return numbers
}

// crossReferencesIn collects every citation of one record by another.
func crossReferencesIn(t *testing.T, directory string) []reference {
	t.Helper()

	prose := regexp.MustCompile(`ADR-([0-9]{2,3})`)
	superseded := regexp.MustCompile(`^superseded_by:\s*([0-9]+)\s*$`)

	found := []reference{}
	for _, path := range recordPaths(t, directory) {
		name := filepath.Base(path)
		for _, line := range readLines(t, path) {
			if match := superseded.FindStringSubmatch(line); match != nil {
				found = append(found, reference{name, recordNumber(t, path, match[1]), "superseded_by"})
				continue
			}
			for _, match := range prose.FindAllStringSubmatch(line, -1) {
				found = append(found, reference{name, recordNumber(t, path, match[1]), fmt.Sprintf("ADR-%s", match[1])})
			}
		}
	}

	return found
}

// recordNumber parses a record number, failing loudly rather than skipping.
func recordNumber(t *testing.T, path, raw string) int {
	t.Helper()

	number, err := strconv.Atoi(raw)
	if err != nil {
		t.Fatalf("%s carries an unparseable record number %q: %v", path, raw, err)
	}
	return number
}

// recordPaths lists the decision records in a stable order, so a diff of the
// failure output is readable.
func recordPaths(t *testing.T, directory string) []string {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join(directory, "*.yaml"))
	if err != nil {
		t.Fatalf("glob %s: %v", directory, err)
	}
	if len(paths) == 0 {
		t.Fatalf("found no decision records under %s", directory)
	}

	sort.Strings(paths)
	return paths
}
