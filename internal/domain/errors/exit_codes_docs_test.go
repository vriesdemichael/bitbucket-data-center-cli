package errors

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The exit code of each error kind is written out by hand twice: in the
// machine-mode page, for a script author, and in troubleshooting, for a person
// looking at a failure. Both are held to ExitCode here, so a kind added, or a
// code moved, cannot leave either page saying something else.

// siteDocument reads a page of the documentation site.
func siteDocument(t *testing.T, path string) string {
	t.Helper()

	directory, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatal("no go.mod above the test's directory")
		}
		directory = parent
	}

	content, err := os.ReadFile(filepath.Join(directory, "docs", "site", filepath.FromSlash(path)))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return strings.ReplaceAll(string(content), "\r\n", "\n")
}

// linesUnder returns the lines between a heading and the next heading of the
// same level.
func linesUnder(t *testing.T, page, heading string) []string {
	t.Helper()

	marker := heading[:strings.Index(heading, " ")+1]
	var lines []string
	inside := false
	for _, line := range strings.Split(page, "\n") {
		if inside && strings.HasPrefix(line, marker) {
			break
		}
		if inside {
			lines = append(lines, line)
		}
		if line == heading {
			inside = true
		}
	}
	if !inside {
		t.Fatalf("no heading %q", heading)
	}

	return lines
}

var backticked = regexp.MustCompile("`([a-z_]+)`")

// assertExitCodes compares the kind-to-code pairs a page states with
// ExitCode's.
func assertExitCodes(t *testing.T, page string, documented map[string]int) {
	t.Helper()

	want := map[string]int{}
	for _, kind := range Kinds() {
		want[string(kind)] = ExitCode(New(kind, "x", nil))
	}

	for kind, code := range want {
		got, stated := documented[kind]
		switch {
		case !stated:
			t.Errorf("%s: the %s kind is missing; it exits %d", page, kind, code)
		case got != code:
			t.Errorf("%s: %s is said to exit %d; it exits %d", page, kind, got, code)
		}
	}
	for kind := range documented {
		if _, known := want[kind]; !known {
			t.Errorf("%s: names a kind %q that bb does not have", page, kind)
		}
	}
}

func TestMachineModePageStatesEachKindsExitCode(t *testing.T) {
	t.Parallel()

	const page = "advanced/machine-mode-diagnostics.md"
	code := regexp.MustCompile("exit code `([0-9]+)`")

	documented := map[string]int{}
	for _, line := range linesUnder(t, siteDocument(t, page), "## Error kinds and exit codes") {
		kinds, rest, found := strings.Cut(line, "->")
		if !strings.HasPrefix(line, "- ") || !found {
			continue
		}
		match := code.FindStringSubmatch(rest)
		if match == nil {
			t.Fatalf("%s: a line with no exit code: %q", page, line)
		}
		number, _ := strconv.Atoi(match[1])
		for _, kind := range backticked.FindAllStringSubmatch(kinds, -1) {
			documented[kind[1]] = number
		}
	}

	assertExitCodes(t, page, documented)
}

func TestTroubleshootingPageStatesEachKindsExitCode(t *testing.T) {
	t.Parallel()

	const page = "troubleshooting.md"
	documented := map[string]int{}
	for _, line := range linesUnder(t, siteDocument(t, page), "## A command exits non-zero and I need to know why") {
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if !strings.HasPrefix(line, "| `") || len(cells) != 2 {
			continue
		}
		number, err := strconv.Atoi(strings.Trim(strings.TrimSpace(cells[0]), "`"))
		if err != nil {
			t.Fatalf("%s: a row whose code is not a number: %q", page, line)
		}
		for _, kind := range backticked.FindAllStringSubmatch(cells[1], -1) {
			documented[kind[1]] = number
		}
	}

	assertExitCodes(t, page, documented)
}
