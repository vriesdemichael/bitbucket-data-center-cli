package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, root, relative, contents string) {
	t.Helper()

	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// The rule has to fail before it is worth having. These are the three shapes
// the retired host actually took: a documentation link, a Go constant, and the
// $id of a published schema.
func TestLintRetiredHostReportsEveryFileTypeItReached(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	writeFile(t, root, "docs/site/llms.txt", "- [Docs home]("+retiredPagesHost+"/latest/)\n")
	writeFile(t, root, "internal/cli/jsonoutput/schema.go",
		"package jsonoutput\n\nconst SchemaBaseURL = \""+retiredPagesHost+"/latest/reference/schemas/output/\"\n")
	writeFile(t, root, "docs/reference/schemas/output/output.pr.get.schema.json",
		"{\n  \"$id\": \""+retiredPagesHost+"/latest/reference/schemas/output/output.pr.get.schema.json\"\n}\n")

	findings, err := lintRetiredHost(root)
	if err != nil {
		t.Fatalf("lintRetiredHost: %v", err)
	}

	wantLines := map[string]int{
		"docs/site/llms.txt":                                      1,
		"internal/cli/jsonoutput/schema.go":                       3,
		"docs/reference/schemas/output/output.pr.get.schema.json": 2,
	}

	if len(findings) != len(wantLines) {
		t.Fatalf("expected %d findings, got %d: %+v", len(wantLines), len(findings), findings)
	}

	for _, item := range findings {
		wantLine, known := wantLines[item.File]
		if !known {
			t.Errorf("unexpected file in findings: %s", item.File)
			continue
		}
		if item.Line != wantLine {
			t.Errorf("%s: reported line %d, want %d", item.File, item.Line, wantLine)
		}
		if !strings.Contains(item.Problem, "bitbucket-data-center-cli") {
			t.Errorf("%s: problem does not name the replacement host: %s", item.File, item.Problem)
		}
	}
}

func TestLintRetiredHostPassesOnTheCurrentHost(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	writeFile(t, root, "docs/site/llms.txt",
		"- [Docs home](https://vriesdemichael.github.io/bitbucket-data-center-cli/latest/)\n")

	findings, err := lintRetiredHost(root)
	if err != nil {
		t.Fatalf("lintRetiredHost: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("expected no findings, got %+v", findings)
	}
}

// A github.com link to the former repository name still resolves, because
// GitHub redirects a renamed repository. Only the Pages host is dead, and a
// rule that fired on both would block the module path until it is renamed too.
func TestLintRetiredHostIgnoresTheRedirectedRepositoryHost(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	writeFile(t, root, "go.mod", "module github.com/vriesdemichael/bitbucket-data-center-cli\n")

	findings, err := lintRetiredHost(root)
	if err != nil {
		t.Fatalf("lintRetiredHost: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("expected no findings, got %+v", findings)
	}
}

func TestLintRetiredHostSkipsItsOwnDefinition(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	writeFile(t, root, "tools/docs-lint/retiredhost.go",
		"package main\n\nconst retiredPagesHost = \""+retiredPagesHost+"\"\n")

	findings, err := lintRetiredHost(root)
	if err != nil {
		t.Fatalf("lintRetiredHost: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("expected the rule to skip its own definition, got %+v", findings)
	}
}

func TestLintRetiredHostSkipsFileTypesItDoesNotRead(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	writeFile(t, root, "assets/logo.svg", retiredPagesHost+"\n")

	findings, err := lintRetiredHost(root)
	if err != nil {
		t.Fatalf("lintRetiredHost: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("expected no findings, got %+v", findings)
	}
}

// The published schemas are one long line each; the default scanner buffer
// would stop partway through and report nothing rather than fail.
func TestLintRetiredHostReadsLinesLongerThanTheDefaultBuffer(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	padding := strings.Repeat("x", 128*1024)
	writeFile(t, root, "docs/reference/schemas/output/wide.schema.json",
		"{\"description\":\""+padding+"\",\"$id\":\""+retiredPagesHost+"/latest/\"}\n")

	findings, err := lintRetiredHost(root)
	if err != nil {
		t.Fatalf("lintRetiredHost: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
}

// TestLintRetiredHostReportsTheBareSlug covers the name on its own.
//
// The host rule caught the dead URL, and the URL kept coming back because the
// bare name was still in front of everyone who read the code -- a heading, a
// module path, a test fixture. Each of those is where a link to the dead host
// gets written next.
func TestLintRetiredHostReportsTheBareSlug(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	writeFile(t, root, "AGENTS.md", "# Agent Instructions — "+retiredSlug+"\n")
	writeFile(t, root, "go.mod", "module github.com/vriesdemichael/"+retiredSlug+"\n")
	writeFile(t, root, "internal/thing/thing_test.go",
		"package thing\n\nconst repo = \""+retiredSlug+"\"\n")

	findings, err := lintRetiredHost(root)
	if err != nil {
		t.Fatalf("lintRetiredHost: %v", err)
	}
	if len(findings) != 3 {
		t.Fatalf("expected the slug reported in all three files, got %d: %+v", len(findings), findings)
	}
	for _, item := range findings {
		if item.Command != retiredSlug {
			t.Errorf("%s reported %q, want the bare slug", item.File, item.Command)
		}
	}
}

// TestLintRetiredHostNamesOneMistakeOnce keeps the two rules from both firing
// on the same line.
//
// Every line carrying the dead host also carries the slug it is built from, and
// reporting both would tell a reader to fix two things where there is one. The
// host wins, because it is the more specific finding and its remedy is exact.
func TestLintRetiredHostNamesOneMistakeOnce(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	writeFile(t, root, "docs/site/index.md", "[Docs]("+retiredPagesHost+"/latest/)\n")

	findings, err := lintRetiredHost(root)
	if err != nil {
		t.Fatalf("lintRetiredHost: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("expected one finding for one line, got %d: %+v", len(findings), findings)
	}
	if findings[0].Command != retiredPagesHost {
		t.Errorf("reported %q, want the host rather than the slug inside it", findings[0].Command)
	}
}

// TestLintRetiredHostSkipsIgnoredBuildOutput keeps the rule from failing on a
// developer's own leftovers.
//
// The rule walks the filesystem rather than the index, so it sees what git
// ignores. A coverage profile records import paths, so one written before the
// module was renamed still carries the old one, and a docs virtualenv embeds
// the absolute path of whatever directory the checkout sits in. A lint that
// fails on those is a lint that gets turned off.
func TestLintRetiredHostSkipsIgnoredBuildOutput(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	writeFile(t, root, ".tmp/coverage.out", "github.com/vriesdemichael/"+retiredSlug+"/internal/cli/root.go:1.1,2.2 1 1\n")
	writeFile(t, root, "docs/.venv/Scripts/activate_this.py", "prompt = \""+retiredSlug+"-docs\"\n")

	findings, err := lintRetiredHost(root)
	if err != nil {
		t.Fatalf("lintRetiredHost: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("ignored build output was reported: %+v", findings)
	}
}
