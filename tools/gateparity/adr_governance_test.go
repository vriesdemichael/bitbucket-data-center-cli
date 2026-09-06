package gateparity

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// governanceRecord is the decision record that lists the governance tests.
const governanceRecord = "docs/decisions/067-governance-tests-are-verified-by-breaking-them.yaml"

// TestGovernanceTestsNamedInThisRecordExist keeps ADR-067's list honest.
//
// The list is worth having only because something checks it. Three guards were
// missed when the set was first inventoried by grepping for TestEvery and
// TestAll, because they follow neither convention -- so the record enumerates
// them instead, and a record that names a test which no longer exists is the
// ADR-039 failure with a different filename.
//
// This lives in tools/gateparity rather than in internal/cli because it reads
// the repository as text, like the gate parity tests beside it, and belongs
// with them rather than with any package it happens to name.
func TestGovernanceTestsNamedInThisRecordExist(t *testing.T) {
	t.Parallel()

	root := repositoryRoot(t)

	named := governanceTestsNamedIn(t, filepath.Join(root, governanceRecord))
	if len(named) < 10 {
		t.Fatalf("expected the record to name at least ten guards, found %d: %v", len(named), named)
	}

	declared := testFunctionsInTree(t, root)

	for _, name := range named {
		if _, ok := declared[name]; !ok {
			t.Errorf(
				"ADR-067 names %q, which is not declared anywhere.\n"+
					"Either the test was renamed or removed and the record was not updated,\n"+
					"or the name in the record is a typo.",
				name,
			)
		}
	}
}

// governanceTestsNamedIn reads the list entries, not the whole record.
//
// The prose legitimately names tests that no longer exist -- the rationale
// discusses TestAllMutatingCommandsHaveDryRunProfile, which this branch
// deleted, and mentions the TestEvery and TestAll prefixes that made three
// guards easy to miss. Only a list entry, "- TestName: what it asserts", is a
// claim that the test is present.
func governanceTestsNamedIn(t *testing.T, path string) []string {
	t.Helper()

	entry := regexp.MustCompile(`^\s+- (Test[A-Za-z0-9_]+):`)

	names := []string{}
	for _, line := range readLines(t, path) {
		if match := entry.FindStringSubmatch(line); match != nil {
			names = append(names, match[1])
		}
	}

	return normalise(names)
}

// testFunctionsInTree collects every Test function declared in the repository.
func testFunctionsInTree(t *testing.T, root string) map[string]struct{} {
	t.Helper()

	declaration := regexp.MustCompile(`^func (Test[A-Za-z0-9_]+)\(`)
	declared := map[string]struct{}{}

	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			// Agent tooling keeps checkouts of other branches under dot
			// directories; their tests are not this tree's tests.
			if path != root && strings.HasPrefix(info.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}

		for _, line := range readLines(t, path) {
			if match := declaration.FindStringSubmatch(line); match != nil {
				declared[match[1]] = struct{}{}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	if len(declared) == 0 {
		t.Fatal("found no test functions; the walk or the parser has stopped matching")
	}

	return declared
}
