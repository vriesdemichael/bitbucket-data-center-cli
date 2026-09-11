package testsupport_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// clockDerivedName matches a fixture name built from the clock: a Sprintf whose
// arguments reach time.Now(), in any of the shapes this suite has used.
var clockDerivedName = regexp.MustCompile(`(?s)fmt\.Sprintf\([^)]*time\.Now\(\)`)

// clockPlusCounter matches the other form: a low-resolution clock made unique
// by a counter beside it, which repeats from the start on the next run.
var clockPlusCounter = regexp.MustCompile(`time\.Now\(\)[^\n]*(Add\(1\)|counter)`)

// TestNoFixtureIsNamedFromTheClock is ADR-085.
//
// Clock-derived names collided three ways here, each found only after the
// failure was misread as a product bug: truncated to five digits so they
// repeated every 100 microseconds, coarse because time.Now() is not guaranteed
// finer than a millisecond, and restarted because a process-local counter
// begins again on the next run.
func TestNoFixtureIsNamedFromTheClock(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..")

	var offenders []string
	var scanned int

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			// Dotted directories hold agent worktrees with whole other branches
			// of this repository in them (ADR-065).
			if path != root && strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}

		contents, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		scanned++

		source := string(contents)
		for _, match := range clockDerivedName.FindAllString(source, -1) {
			offenders = append(offenders, filepath.ToSlash(path)+": "+strings.Join(strings.Fields(match), " "))
		}
		for _, match := range clockPlusCounter.FindAllString(source, -1) {
			offenders = append(offenders, filepath.ToSlash(path)+": "+strings.Join(strings.Fields(match), " "))
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk the repository: %v", err)
	}

	// A walk that stopped finding test files would report perfect compliance,
	// which is the failure mode ADR-067 exists to catch.
	if scanned < 50 {
		t.Fatalf("scanned only %d test files, expected dozens.\nThe walk is probably broken, not the suite.", scanned)
	}

	if len(offenders) > 0 {
		sort.Strings(offenders)
		t.Fatalf(
			"%d fixture name(s) are built from the clock:\n  %s\n\n"+
				"ADR-085: use testsupport.UniqueSuffix or testsupport.UniqueName. A timestamp\n"+
				"repeats when it is truncated, when the clock is coarse, and across runs when a\n"+
				"counter is what makes it unique.",
			len(offenders), strings.Join(offenders, "\n  "),
		)
	}
}
