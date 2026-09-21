package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/compat"
)

// TestDifferencesAreReadFromTheSource covers the reading the whole check rests
// on: a difference the package declares, however the literal is written.
func TestDifferencesAreReadFromTheSource(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := `package compat

var Keyed = Difference{
	What:  "a keyed capability",
	Since: Release{Major: 10, Minor: 2},
}

var Positional = Difference{What: "a positional capability", Since: Release{9, 5, 0}}

// Not a difference, and not to be read as one.
var Elsewhere = "9.9"
`
	if err := os.WriteFile(filepath.Join(dir, "declared.go"), []byte(source), 0o600); err != nil {
		t.Fatalf("write the source: %v", err)
	}
	// A test file in the same package, which declares differences of its own
	// that the catalogue does not describe.
	if err := os.WriteFile(filepath.Join(dir, "declared_test.go"),
		[]byte("package compat\n\nvar Fixture = Difference{What: \"a fixture\", Since: Release{Major: 1, Minor: 1}}\n"), 0o600); err != nil {
		t.Fatalf("write the test source: %v", err)
	}

	found, err := differencesIn(dir)
	if err != nil {
		t.Fatalf("read the differences: %v", err)
	}

	want := []difference{
		{Name: "Keyed", What: "a keyed capability", Since: compat.Release{Major: 10, Minor: 2}},
		{Name: "Positional", What: "a positional capability", Since: compat.Release{Major: 9, Minor: 5}},
	}
	if len(found) != len(want) {
		t.Fatalf("read %d differences, want %d: %+v", len(found), len(want), found)
	}
	for index, declared := range want {
		if found[index] != declared {
			t.Errorf("difference %d is %+v, want %+v", index, found[index], declared)
		}
	}
}

// TestTheCatalogueIsComparedByRelease covers what the gate is for: a difference
// bb acts on that no reader is told about, and a row that no call declares.
func TestTheCatalogueIsComparedByRelease(t *testing.T) {
	t.Parallel()

	recorded := window{OldestServed: "9.2", Releases: []string{"9.2.1", "10.4.3"}}
	oldest := compat.Release{Major: 9, Minor: 2, Patch: 1}
	newest := compat.Release{Major: 10, Minor: 4, Patch: 3}
	page := `# Bitbucket Versions

` + "`bb`" + ` works with Bitbucket Data Center 9.2 and every release after it.

| Capability | From | On an earlier release |
|---|---|---|
| A required build that spares pull requests | 10.2 | Refused. |
`
	declared := []difference{{Name: "RequiredBuildScope", What: "a required build", Since: compat.Release{Major: 10, Minor: 2}}}

	if problems := compareToPage(declared, page, recorded, oldest, newest); len(problems) != 0 {
		t.Fatalf("a catalogue that matches the code reported %v", problems)
	}

	undocumented := append(declared, difference{Name: "ReviewerGroups", Since: compat.Release{Major: 9, Minor: 5}})
	problems := compareToPage(undocumented, page, recorded, oldest, newest)
	if len(problems) != 1 || !strings.Contains(problems[0], "9.5") {
		t.Errorf("a difference missing from the catalogue reported %v, want one problem naming 9.5", problems)
	}

	if problems := compareToPage(nil, page, recorded, oldest, newest); len(problems) != 1 ||
		!strings.Contains(problems[0], "internal/compat does not declare") {
		t.Errorf("a catalogue row no call declares reported %v", problems)
	}
}

// TestADifferenceOutsideTheWindowIsReported covers the boundary a reader cannot
// check: a release check against a release no release served lacks, or one no
// release served has.
func TestADifferenceOutsideTheWindowIsReported(t *testing.T) {
	t.Parallel()

	recorded := window{OldestServed: "9.2", Releases: []string{"9.2.1", "10.4.3"}}
	oldest := compat.Release{Major: 9, Minor: 2, Patch: 1}
	newest := compat.Release{Major: 10, Minor: 4, Patch: 3}
	page := "`bb` works with Bitbucket Data Center 9.2 and every release after it.\n\n| C | From | E |\n|---|---|---|\n| A capability | 8.14 | Refused. |\n"

	problems := compareToPage([]difference{{Name: "Ancient", Since: compat.Release{Major: 8, Minor: 14}}}, page, recorded, oldest, newest)
	if len(problems) != 1 || !strings.Contains(problems[0], "before the oldest release served") {
		t.Fatalf("a difference older than the window reported %v", problems)
	}
}

// TestTheEndsAreTheOldestAndNewest covers what `task test:live:matrix` runs
// when it is not asked for the whole window.
func TestTheEndsAreTheOldestAndNewest(t *testing.T) {
	t.Parallel()

	window := []string{"9.2.1", "9.4.24", "10.4.3"}
	if ends := endsOf(window); len(ends) != 2 || ends[0] != "9.2.1" || ends[1] != "10.4.3" {
		t.Errorf("the ends of %v are %v", window, ends)
	}
	// A window of one release is its own ends, rather than that release twice.
	if ends := endsOf([]string{"10.4.3"}); len(ends) != 1 || ends[0] != "10.4.3" {
		t.Errorf("the ends of one release are %v", ends)
	}
}

// TestTheCommittedWindowHoldsTogether runs the gate over the repository, so the
// committed window, the declared differences and the page are checked here too
// rather than only by the task.
func TestTheCommittedWindowHoldsTogether(t *testing.T) {
	// The paths the gate reads are relative to the repository root, which is
	// where the task runs it. t.Chdir puts it back afterwards.
	t.Chdir(filepath.Join("..", ".."))

	recorded, err := readWindow(windowPath)
	if err != nil {
		t.Fatalf("read %s: %v", windowPath, err)
	}
	problems, err := check(recorded)
	if err != nil {
		t.Fatalf("check the window: %v", err)
	}
	if len(problems) != 0 {
		t.Errorf("the committed window does not hold together:\n%s", strings.Join(problems, "\n"))
	}
}
