package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cc "github.com/vriesdemichael/bitbucket-data-center-cli/tools/conventionalcommits"
)

const repositoryURL = "https://github.com/vriesdemichael/bitbucket-data-center-cli"

func commitsFrom(pairs ...[2]string) []cc.Commit {
	built := make([]cc.Commit, 0, len(pairs))
	for index, pair := range pairs {
		built = append(built, cc.Classify(fmt.Sprintf("%040x", index+1), pair[0], pair[1]))
	}

	return built
}

func settingsFor(t *testing.T, version, previousTag string) settings {
	t.Helper()

	return settings{
		version:       version,
		previousTag:   previousTag,
		repositoryURL: repositoryURL,
		preambleDir:   t.TempDir(),
	}
}

// The whole body for a small release, written out. The release body is the
// document adopters read, and the pieces that make it readable -- the compare
// link, breaking changes above the ledger, the scope prefix -- are easier to
// keep by pinning the output than by asserting on fragments of it.
func TestRenderWritesTheWholeBody(t *testing.T) {
	t.Parallel()

	config := settingsFor(t, "v3.5.0", "v3.4.5")
	markdown, _, err := render(config, commitsFrom(
		[2]string{"feat(auth): read the token from stdin", ""},
		[2]string{"fix: repair the paging guard", ""},
		[2]string{"chore: tidy the makefile", ""},
	))
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	want := strings.Join([]string{
		"## v3.5.0",
		"",
		"Changes since v3.4.5.",
		"",
		"Compare: [v3.4.5...v3.5.0](" + repositoryURL + "/compare/v3.4.5...v3.5.0)",
		"",
		"### Features",
		"- auth: read the token from stdin ([0000000](" + repositoryURL + "/commit/0000000000000000000000000000000000000001))",
		"",
		"### Fixes",
		"- repair the paging guard ([0000000](" + repositoryURL + "/commit/0000000000000000000000000000000000000002))",
		"",
		"### Chores",
		"- tidy the makefile ([0000000](" + repositoryURL + "/commit/0000000000000000000000000000000000000003))",
		"",
	}, "\n")

	if markdown != want {
		t.Errorf("body:\n--- got ---\n%s\n--- want ---\n%s", markdown, want)
	}
}

func TestRenderPutsBreakingChangesAboveTheLedgerWithTheirNotes(t *testing.T) {
	t.Parallel()

	config := settingsFor(t, "v4.0.0", "v3.9.1")
	markdown, data, err := render(config, commitsFrom(
		[2]string{"feat(auth)!: retire --token", "BREAKING CHANGE: --token is gone;\nread it from stdin."},
		[2]string{"fix: something ordinary", ""},
	))
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	if !strings.Contains(markdown, "### ⚠ Breaking Changes\n- retire --token ([0000000]") {
		t.Errorf("the breaking section is missing or misshapen:\n%s", markdown)
	}
	if !strings.Contains(markdown, " — --token is gone; read it from stdin.") {
		t.Errorf("the footer should be appended, collapsed onto one line:\n%s", markdown)
	}

	breakingAt := strings.Index(markdown, "### ⚠ Breaking Changes")
	featuresAt := strings.Index(markdown, "### Features")
	if breakingAt < 0 || featuresAt < 0 || breakingAt > featuresAt {
		t.Errorf("breaking changes must come before the ledger:\n%s", markdown)
	}

	if len(data.BreakingChanges) != 1 {
		t.Fatalf("got %d breaking changes, want 1", len(data.BreakingChanges))
	}
}

// A commit marked breaking only by its ! has no footer to quote, and the bullet
// must not trail an empty dash.
func TestRenderOmitsTheDashWhenThereIsNoFooter(t *testing.T) {
	t.Parallel()

	config := settingsFor(t, "v4.0.0", "v3.9.1")
	markdown, _, err := render(config, commitsFrom([2]string{"feat!: retire the flag", ""}))
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	if strings.Contains(markdown, ") — ") || strings.Contains(markdown, ")—") {
		t.Errorf("expected no note separator:\n%s", markdown)
	}
}

func TestRenderSaysInitialWhenThereIsNoPreviousTag(t *testing.T) {
	t.Parallel()

	config := settingsFor(t, "v0.1.0", "")
	markdown, data, err := render(config, commitsFrom([2]string{"feat: the first thing", ""}))
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	if !strings.Contains(markdown, "Initial release changes.") {
		t.Errorf("expected the initial wording:\n%s", markdown)
	}
	if strings.Contains(markdown, "Compare:") {
		t.Errorf("there is nothing to compare against:\n%s", markdown)
	}
	if data.CompareURL != "" {
		t.Errorf("compare url: got %q, want empty", data.CompareURL)
	}
}

// The ledger folds away once it is long enough to bury the breaking changes
// above it. The threshold is a boundary, so both sides of it are checked.
func TestRenderCollapsesTheLedgerOnlyOnceItIsLong(t *testing.T) {
	t.Parallel()

	build := func(count int) []cc.Commit {
		pairs := make([][2]string, 0, count)
		for index := range count {
			pairs = append(pairs, [2]string{fmt.Sprintf("fix: thing %d", index), ""})
		}

		return commitsFrom(pairs...)
	}

	config := settingsFor(t, "v3.5.0", "v3.4.5")

	atThreshold, _, err := render(config, build(collapseLedgerAbove))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(atThreshold, "<details>") {
		t.Error("a ledger at the threshold should stay open")
	}

	overThreshold, _, err := render(config, build(collapseLedgerAbove+1))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(overThreshold, "<details>") || !strings.Contains(overThreshold, "</details>") {
		t.Errorf("a longer ledger should fold:\n%s", overThreshold)
	}
	if !strings.Contains(overThreshold, fmt.Sprintf("<summary>All %d changes</summary>", collapseLedgerAbove+1)) {
		t.Errorf("the summary should count the changes:\n%s", overThreshold)
	}
}

// A written introduction has to be in the body at creation time: the same run
// renders it into the versioned docs snapshot, which no later commit reaches.
func TestRenderCarriesTheWrittenPreamble(t *testing.T) {
	t.Parallel()

	config := settingsFor(t, "v4.0.0", "v3.9.1")
	if err := os.WriteFile(filepath.Join(config.preambleDir, "v4.0.0.md"), []byte("\nThis release retires the flags.\n\n"), 0o600); err != nil {
		t.Fatalf("seed the preamble: %v", err)
	}

	markdown, _, err := render(config, commitsFrom([2]string{"feat: a thing", ""}))
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	want := "## v4.0.0\n\nThis release retires the flags.\n\nChanges since v3.9.1."
	if !strings.HasPrefix(markdown, want) {
		t.Errorf("the preamble should follow the heading:\n%s", markdown)
	}
}

func TestRenderIgnoresAMissingPreamble(t *testing.T) {
	t.Parallel()

	config := settingsFor(t, "v4.0.0", "v3.9.1")
	markdown, _, err := render(config, commitsFrom([2]string{"feat: a thing", ""}))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.HasPrefix(markdown, "## v4.0.0\n\nChanges since v3.9.1.") {
		t.Errorf("an ordinary release should read as before:\n%s", markdown)
	}
}

// An unconventional subject still belongs in the ledger, under Other, with the
// whole subject as its description.
func TestRenderFilesAnUnconventionalSubjectUnderOther(t *testing.T) {
	t.Parallel()

	config := settingsFor(t, "v3.5.0", "v3.4.5")
	markdown, _, err := render(config, commitsFrom(
		[2]string{"feat: a thing", ""},
		[2]string{"Revert to a project per test", ""},
	))
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	if !strings.Contains(markdown, "### Other\n- Revert to a project per test (") {
		t.Errorf("expected an Other section:\n%s", markdown)
	}
}

func TestEncodeKeepsTheChangelogShape(t *testing.T) {
	t.Parallel()

	config := settingsFor(t, "v4.0.0", "v3.9.1")
	_, data, err := render(config, commitsFrom(
		[2]string{"feat(auth)!: retire --token", "BREAKING CHANGE: gone"},
		[2]string{"fix: a thing", ""},
	))
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	encoded, err := encode(data)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	text := string(encoded)

	// A commit with no scope reports one, as null rather than as an absent key.
	if !strings.Contains(text, `"scope": null`) {
		t.Errorf("an absent scope should be null:\n%s", text)
	}
	if !strings.Contains(text, `"scope": "auth"`) {
		t.Errorf("a scope should be reported:\n%s", text)
	}
	// breakingNote belongs only to the breakingChanges entries.
	if strings.Count(text, `"breakingNote"`) != 1 {
		t.Errorf("expected exactly one breakingNote:\n%s", text)
	}
	if !strings.HasSuffix(text, "}\n") {
		t.Errorf("the file should end with a newline:\n%q", text[len(text)-10:])
	}
}

// Nothing to report is an empty list, not a null: a reader indexing the
// changelog should not have to tell the two apart.
func TestEncodeReportsEmptyListsRatherThanNull(t *testing.T) {
	t.Parallel()

	config := settingsFor(t, "v3.4.6", "v3.4.5")
	_, data, err := render(config, nil)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	encoded, err := encode(data)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if strings.Contains(string(encoded), "null,") || strings.Contains(string(encoded), ": null\n") {
		t.Errorf("expected empty lists:\n%s", encoded)
	}
	if !strings.Contains(string(encoded), `"commits": []`) {
		t.Errorf("expected an empty commits list:\n%s", encoded)
	}
}
