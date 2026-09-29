package highlight

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// patchOf joins lines into a patch, each ending in \n.
func patchOf(lines ...string) string {
	return strings.Join(lines, "\n") + "\n"
}

// codeLines is the text of each code line of each file of a patch, as the
// view reads them: every line inside a hunk that starts with +, - or a
// space, without that mark and without the \r of a \r\n.
func codeLines(patch string) map[int][]string {
	out := map[int][]string{}
	file, inHunk := -1, false
	for _, line := range strings.Split(patch, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			file, inHunk = file+1, false
		case file < 0:
		case hunkHeader.MatchString(line):
			inHunk = true
		case inHunk && line != "" && strings.ContainsRune("+- ", rune(line[0])):
			out[file] = append(out[file], strings.TrimSuffix(line[1:], "\r"))
		}
	}
	return out
}

// checkPatchCovers fails unless each file with spans has one line of spans for
// each of its code lines, covering it exactly.
func checkPatchCovers(t *testing.T, patch string, spans map[int][]string) {
	t.Helper()
	lines := codeLines(patch)
	for file, fileSpans := range spans {
		checkCovers(t, "file "+strconv.Itoa(file), lines[file], fileSpans)
	}
}

var greetPatch = patchOf(
	"diff --git a/greet.go b/greet.go",
	"index 1111111..2222222 100644",
	"--- a/greet.go",
	"+++ b/greet.go",
	"@@ -1,7 +1,10 @@",
	" package greet",
	" ",
	"-// Greet says hello.",
	"-func Greet(name string) {",
	"+/* Greet says hello",
	"+to whoever is named,",
	"+in one line. */",
	"+func Greet(name string) int {",
	" \tfmt.Println(\"hello\", name)",
	"+\treturn 42",
	" }",
	"@@ -20,5 +23,3 @@ func Other() {",
	"-/*",
	"-x := 1",
	"-*/",
	"+x := 1",
	" y := \"é😀\"",
)

// A file's code lines, and only they, get spans, in patch order, from the
// side of their hunk they belong to.
func TestPatchHighlightsEachCodeLine(t *testing.T) {
	t.Parallel()

	spans := Patch(greetPatch, later())
	file, ok := spans[0]
	if !ok || len(spans) != 1 {
		t.Fatalf("Patch = %v, want spans for file 0 alone", spans)
	}
	checkPatchCovers(t, greetPatch, spans)
	lines := codeLines(greetPatch)[0]
	if len(file) != 16 {
		t.Fatalf("%d lines of spans, want 16, one per code line", len(file))
	}

	for _, want := range []struct {
		line  int // among the file's code lines
		word  string
		class byte
	}{
		{1, "package", ClassKeyword},
		{3, "// Greet says hello.", ClassComment},
		{4, "func", ClassKeyword},
		{4, "Greet", ClassFunction},
		// The block comment the new side opens colours all three lines.
		{5, "/* Greet says hello", ClassComment},
		{6, "to whoever is named,", ClassComment},
		{7, "in one line. */", ClassComment},
		{8, "func", ClassKeyword},
		{8, "int", ClassType},
		{9, `"hello"`, ClassString},
		{10, "return", ClassKeyword},
		{10, "42", ClassNumber},
		// The removed x := 1 was inside the old side's comment; the added one
		// is code on the new side.
		{13, "x := 1", ClassComment},
		{15, "x", ClassText},
		{15, "1", ClassNumber},
		{16, `"é😀"`, ClassString},
	} {
		if want.line > len(file) {
			t.Fatalf("no spans for code line %d", want.line)
		}
		if got := classOfWord(t, lines[want.line-1], file[want.line-1], want.word); got != want.class {
			t.Errorf("code line %d %q: %q is %c, want %c (spans %q)", want.line, lines[want.line-1], want.word, got, want.class, file[want.line-1])
		}
	}
}

// git's a/ and b/ and Bitbucket's src:// and dst:// are both headers, and a
// file's index counts every file of the patch, highlighted or not.
func TestPatchReadsBothHeaderStyles(t *testing.T) {
	t.Parallel()

	patch := patchOf(
		"diff --git src://notes.unknownext dst://notes.unknownext",
		"--- src://notes.unknownext",
		"+++ dst://notes.unknownext",
		"@@ -1 +1 @@",
		"-a",
		"+b",
		"diff --git src://app/main.py dst://app/main.py",
		"index 1111111..2222222 100644",
		"--- src://app/main.py",
		"+++ dst://app/main.py",
		"@@ -1,2 +1,2 @@",
		" def f():",
		"-    return None",
		"+    return True",
		"diff --git a/web/app.ts b/web/app.ts",
		"--- a/web/app.ts",
		"+++ b/web/app.ts",
		"@@ -1 +1 @@",
		"-let a = 1;",
		"+const a = 2;",
	)
	spans := Patch(patch, later())
	if _, ok := spans[0]; ok {
		t.Errorf("file 0, of no known language, has spans %q", spans[0])
	}
	if len(spans[1]) != 3 || len(spans[2]) != 2 {
		t.Fatalf("Patch = %v, want 3 lines of spans for file 1 and 2 for file 2", spans)
	}
	checkPatchCovers(t, patch, spans)
	if got := classOfWord(t, "    return True", spans[1][2], "True"); got != ClassConstant {
		t.Errorf("True in main.py is %c, want %c", got, ClassConstant)
	}
	if got := classOfWord(t, "const a = 2;", spans[2][1], "const"); got != ClassKeyword {
		t.Errorf("const in app.ts is %c, want %c", got, ClassKeyword)
	}
}

// A line starting with \ is no code line, nor is any line before a file's
// first hunk, nor a line inside a hunk that starts with none of +, - and a
// space.
func TestPatchSkipsWhatIsNotCode(t *testing.T) {
	t.Parallel()

	patch := patchOf(
		"diff --git a/a.go b/a.go",
		"old mode 100644",
		"new mode 100755",
		"index 1111111..2222222",
		"--- a/a.go",
		"+++ b/a.go",
		"@@ -1,2 +1,2 @@",
		" package a",
		"-var x = 1",
		"\\ No newline at end of file",
		"+var x = 2",
		"\\ No newline at end of file",
		"",
	)
	spans := Patch(patch, later())
	if len(spans[0]) != 3 {
		t.Fatalf("Patch = %v, want 3 lines of spans", spans)
	}
	checkPatchCovers(t, patch, spans)
}

// An added file takes its language from its new path, a deleted one from its
// old path, as the view names it, whatever its header says of a new one, and
// a renamed one from its new name; a binary file, a rename without changes and
// a change of mode alone have no code lines, so no spans.
func TestPatchReadsEachKindOfFile(t *testing.T) {
	t.Parallel()

	patch := patchOf(
		"diff --git a/new.py b/new.py",
		"new file mode 100644",
		"index 0000000..1111111",
		"--- /dev/null",
		"+++ b/new.py",
		"@@ -0,0 +1,2 @@",
		"+import os",
		"+print(os.name)",
		"diff --git a/old.rb b/gone.unknownext",
		"deleted file mode 100644",
		"index 1111111..0000000",
		"--- a/old.rb",
		"+++ /dev/null",
		"@@ -1 +0,0 @@",
		"-puts :gone",
		"diff --git a/notes.unknownext b/main.go",
		"similarity index 90%",
		"rename from notes.unknownext",
		"rename to main.go",
		"index 1111111..2222222 100644",
		"--- a/notes.unknownext",
		"+++ b/main.go",
		"@@ -1 +1 @@",
		"-package notes",
		"+package main",
		"diff --git a/logo.png b/logo.png",
		"index 1111111..2222222 100644",
		"Binary files a/logo.png and b/logo.png differ",
		"diff --git a/same.go b/moved.go",
		"similarity index 100%",
		"rename from same.go",
		"rename to moved.go",
		"diff --git a/run.sh b/run.sh",
		"old mode 100644",
		"new mode 100755",
	)
	spans := Patch(patch, later())
	for file, want := range map[int]int{0: 2, 1: 1, 2: 2} {
		if len(spans[file]) != want {
			t.Errorf("file %d has %d lines of spans, want %d", file, len(spans[file]), want)
		}
	}
	for _, file := range []int{3, 4, 5} {
		if _, ok := spans[file]; ok {
			t.Errorf("file %d has spans %q, want none", file, spans[file])
		}
	}
	checkPatchCovers(t, patch, spans)
	if got := classOfWord(t, "puts :gone", spans[1][0], ":gone"); got != ClassConstant {
		t.Errorf(":gone in the deleted old.rb is %c, want %c", got, ClassConstant)
	}
	if got := classOfWord(t, "package main", spans[2][1], "package"); got != ClassKeyword {
		t.Errorf("package in the renamed main.go is %c, want %c", got, ClassKeyword)
	}
}

// git quotes a path with unusual characters, and ends a --- or +++ path that
// has a space in it with a tab; the language is read from the name inside.
// A quoted header gives no paths, so an added or deleted file's name comes
// from its one --- or +++ line, as it does in the view.
func TestPatchReadsQuotedAndSpacedNames(t *testing.T) {
	t.Parallel()

	patch := patchOf(
		`diff --git "a/caf\303\251.go" "b/caf\303\251.go"`,
		`--- "a/caf\303\251.go"`,
		`+++ "b/caf\303\251.go"`,
		"@@ -1 +1 @@",
		"-package a",
		"+package b",
		"diff --git a/my file.go b/my file.go",
		"--- a/my file.go\t",
		"+++ b/my file.go\t",
		"@@ -1 +1 @@",
		"-package a",
		"+package b",
		`diff --git "a/n\303\251w.py" "b/n\303\251w.py"`,
		"new file mode 100644",
		"--- /dev/null",
		`+++ "b/n\303\251w.py"`,
		"@@ -0,0 +1 @@",
		"+import os",
		`diff --git "a/\303\251t\303\251.rb" "b/\303\251t\303\251.rb"`,
		"deleted file mode 100644",
		`--- "a/\303\251t\303\251.rb"`,
		"+++ /dev/null",
		"@@ -1 +0,0 @@",
		"-puts :gone",
	)
	spans := Patch(patch, later())
	for file, want := range map[int]int{0: 2, 1: 2, 2: 1, 3: 1} {
		if len(spans[file]) != want {
			t.Errorf("file %d has %d lines of spans, want %d", file, len(spans[file]), want)
		}
	}
}

// A \r\n in a patch ends the line before it: the spans cover the code line
// without the \r.
func TestPatchReadsCRLFLines(t *testing.T) {
	t.Parallel()

	patch := strings.ReplaceAll(patchOf(
		"diff --git a/a.go b/a.go",
		"--- a/a.go",
		"+++ b/a.go",
		"@@ -1,2 +1,2 @@",
		" package a",
		"-var x = 1",
		"+var x = 2",
	), " a\n", " a\r\n")
	patch = strings.ReplaceAll(patch, "= 1\n", "= 1\r\n")
	spans := Patch(patch, later())
	if len(spans[0]) != 3 {
		t.Fatalf("Patch = %v, want 3 lines of spans", spans)
	}
	checkPatchCovers(t, patch, spans)
	if got := len(classes(t, spans[0][1])); got != len("var x = 1") {
		t.Errorf("spans %q of \"var x = 1\\r\" cover %d units, want %d", spans[0][1], got, len("var x = 1"))
	}
}

// The view's parser takes a header only as JavaScript's . reads it, which
// stops at \r, U+2028 and U+2029: a file whose only hunk header has one after
// its @@ has no hunk in the view, so none here.
func TestPatchReadsHeadersAsTheViewDoes(t *testing.T) {
	t.Parallel()

	for _, header := range []string{"@@ -1 +1 @@ func x", "@@ -1 +1 @@\r"} {
		patch := patchOf("diff --git a/a.go b/a.go", header, "-var x = 1", "+var x = 2")
		if spans := Patch(patch, later()); len(spans) != 0 {
			t.Errorf("header %q: Patch = %v, want nothing", header, spans)
		}
	}
}

// A file whose part of the patch is larger than MaxBytes has no spans, nor
// does one with a side that cannot be tokenized faithfully, such as a Markdown
// code block holding a \r on either side; the files after them keep their
// indexes. A deadline gone by leaves nothing.
func TestPatchDeclines(t *testing.T) {
	t.Parallel()

	large := patchOf(
		"diff --git a/big.go b/big.go",
		"--- a/big.go",
		"+++ b/big.go",
		"@@ -1 +1 @@",
		"+var s = \""+strings.Repeat("x", MaxBytes)+"\"",
	)
	untrusted := func(mark string) string {
		return patchOf(
			"diff --git a/README.md b/README.md",
			"--- a/README.md",
			"+++ b/README.md",
			"@@ -1,3 +1,3 @@",
			mark+"```go",
			mark+"x\ry",
			mark+"```",
		)
	}
	patch := large + untrusted("-") + untrusted("+") + greetPatch
	spans := Patch(patch, later())
	for _, file := range []int{0, 1, 2} {
		if _, ok := spans[file]; ok {
			t.Errorf("file %d has spans %q, want none", file, spans[file])
		}
	}
	if len(spans[3]) != 16 {
		t.Errorf("the file after them has %d lines of spans, want 16", len(spans[3]))
	}
	if spans := Patch(greetPatch, time.Now().Add(-time.Second)); len(spans) != 0 {
		t.Errorf("Patch past its deadline = %v, want nothing", spans)
	}
	if spans := Patch("", later()); len(spans) != 0 {
		t.Errorf("Patch of nothing = %v, want nothing", spans)
	}
}

// Spans cover every code line of a larger patch: this package's own source,
// added as new files and then changed line by line.
func TestPatchCoversALargerPatch(t *testing.T) {
	t.Parallel()

	var patch strings.Builder
	files := 0
	for _, name := range []string{"highlight.go", "patch.go", "lexers.go", "highlight_test.go"} {
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSuffix(strings.ReplaceAll(string(source), "\r\n", "\n"), "\n"), "\n")
		patch.WriteString("diff --git a/" + name + " b/" + name + "\nnew file mode 100644\n--- /dev/null\n+++ b/" + name + "\n")
		patch.WriteString("@@ -0,0 +1," + strconv.Itoa(len(lines)) + " @@\n")
		for _, line := range lines {
			patch.WriteString("+" + line + "\n")
		}
		patch.WriteString("diff --git a/" + name + " b/" + name + "\n--- a/" + name + "\n+++ b/" + name + "\n")
		patch.WriteString("@@ -1," + strconv.Itoa(len(lines)) + " +1," + strconv.Itoa(len(lines)) + " @@\n")
		for i, line := range lines {
			switch i % 3 {
			case 0:
				patch.WriteString(" " + line + "\n")
			case 1:
				patch.WriteString("-" + line + "\n+" + strings.ToUpper(line) + "\n")
			default:
				patch.WriteString("+" + line + "\n-" + line + "\n")
			}
		}
		files += 2
	}
	spans := Patch(patch.String(), later())
	if len(spans) != files {
		t.Fatalf("%d of %d files have spans", len(spans), files)
	}
	checkPatchCovers(t, patch.String(), spans)
}
