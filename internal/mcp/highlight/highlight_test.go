package highlight

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/alecthomas/chroma/v2"
)

// later is a deadline no test reaches.
func later() time.Time { return time.Now().Add(time.Minute) }

// jsLength is the length JavaScript gives s once JSON has carried it to the
// view: its UTF-16 code units, with each invalid byte one U+FFFD.
func jsLength(s string) int { return len(utf16.Encode([]rune(s))) }

// classes reads a line's spans back into one class per UTF-16 unit, failing
// on spans the view could not read: an unknown class, a length that is not
// positive, two neighbours of one class, or anything but single spaces
// between them.
func classes(t *testing.T, spans string) []byte {
	t.Helper()
	if spans == "" {
		return nil
	}
	var out []byte
	var previous byte
	for _, span := range strings.Split(spans, " ") {
		if span == "" || !strings.ContainsRune("kybfsencmgaot", rune(span[0])) {
			t.Fatalf("spans %q: %q is not a class and a length", spans, span)
		}
		length, err := strconv.Atoi(span[1:])
		if err != nil || length <= 0 || span[1] == '0' || span[1] == '+' {
			t.Fatalf("spans %q: %q has no positive length", spans, span)
		}
		if span[0] == previous {
			t.Fatalf("spans %q: two neighbouring spans of class %c", spans, previous)
		}
		previous = span[0]
		for range length {
			out = append(out, span[0])
		}
	}
	return out
}

// classOfWord is the class of word, the first time it appears in line, whose
// spans are given; every unit of the word must have that class.
func classOfWord(t *testing.T, line, spans, word string) byte {
	t.Helper()
	at := strings.Index(line, word)
	if at < 0 {
		t.Fatalf("%q is not in %q", word, line)
	}
	units := classes(t, spans)
	start, end := jsLength(line[:at]), jsLength(line[:at+len(word)])
	if end > len(units) {
		t.Fatalf("spans %q cover %d units of %q, which has %d", spans, len(units), line, jsLength(line))
	}
	class := units[start]
	for _, other := range units[start:end] {
		if other != class {
			t.Fatalf("%q in %q has classes %q", word, line, units[start:end])
		}
	}
	return class
}

// checkCovers fails unless each line's spans cover it exactly.
func checkCovers(t *testing.T, name string, textLines, spans []string) {
	t.Helper()
	if len(spans) != len(textLines) {
		t.Fatalf("%s: %d lines of spans for %d lines", name, len(spans), len(textLines))
	}
	for i, line := range textLines {
		if got, want := len(classes(t, spans[i])), jsLength(line); got != want {
			t.Errorf("%s line %d %q: spans %q cover %d units, want %d", name, i+1, line, spans[i], got, want)
		}
	}
}

const goSource = "package greet\n" +
	"\n" +
	"import \"fmt\"\n" +
	"\n" +
	"// Greet says hello.\n" +
	"func Greet(name string) int {\n" +
	"\tfmt.Println(\"hello\", name, 42)\n" +
	"\treturn len(name) /* the\n" +
	"length\n" +
	"of it */\n" +
	"}\n"

// Keywords, strings, comments, numbers, function names and the rest land in
// their classes.
func TestLinesClassifiesGo(t *testing.T) {
	t.Parallel()

	spans, ok := Lines("greet.go", goSource, later())
	if !ok {
		t.Fatal("greet.go was not highlighted")
	}
	lines := strings.Split(strings.TrimSuffix(goSource, "\n"), "\n")
	checkCovers(t, "greet.go", lines, spans)

	for _, want := range []struct {
		line  int
		word  string
		class byte
	}{
		{1, "package", ClassKeyword},
		{3, `"fmt"`, ClassString},
		{5, "// Greet says hello.", ClassComment},
		{6, "func", ClassKeyword},
		{6, "Greet", ClassFunction},
		{6, "string", ClassType},
		{6, "(", ClassText},
		{7, "Println", ClassFunction},
		{7, `"hello"`, ClassString},
		{7, "42", ClassNumber},
		{8, "return", ClassKeyword},
		{8, "len", ClassConstant},
		{8, "/* the", ClassComment},
		{9, "length", ClassComment},
		{10, "of it */", ClassComment},
	} {
		if got := classOfWord(t, lines[want.line-1], spans[want.line-1], want.word); got != want.class {
			t.Errorf("line %d: %q is %c, want %c (spans %q)", want.line, want.word, got, want.class, spans[want.line-1])
		}
	}
	if spans[1] != "" {
		t.Errorf("the empty line has spans %q, want none", spans[1])
	}
}

// A length is in UTF-16 code units, as JavaScript counts: é is one unit in two
// bytes, and an emoji two units in four bytes.
func TestLinesCountsUTF16Units(t *testing.T) {
	t.Parallel()

	text := "s := \"é😀\" // café 😀\n"
	spans, ok := Lines("a.go", text, later())
	if !ok {
		t.Fatal("a.go was not highlighted")
	}
	line := strings.TrimSuffix(text, "\n")
	if got := len(classes(t, spans[0])); got != 21 {
		t.Errorf("spans %q cover %d units, want 21 (the line is %d bytes)", spans[0], got, len(line))
	}
	if !strings.Contains(spans[0], "s5") {
		t.Errorf("spans %q have no string of 5 units for \"é😀\" (8 bytes)", spans[0])
	}
	if got := classOfWord(t, line, spans[0], "// café 😀"); got != ClassComment {
		t.Errorf("the comment is %c, want %c", got, ClassComment)
	}
	if !strings.HasSuffix(spans[0], "c10") {
		t.Errorf("spans %q do not end in a comment of 10 units for \"// café 😀\" (13 bytes)", spans[0])
	}
}

// Spans cover every line exactly, whatever the language: this package's own
// source, and a text in each of a range of languages.
func TestLinesCoverEveryLine(t *testing.T) {
	t.Parallel()

	texts := map[string]string{
		"a.py":       "@cache\ndef f(x):\n    \"\"\"Doc\n    string.\"\"\"\n    return f\"{x!r}\\n\" and None  # done\n",
		"a.ts":       "const x: number = typeof y; // é\nlet s = `a ${b} c`;\n/* 😀\n */\n",
		"a.html":     "<a href=\"x\" class='y'>&amp; é</a>\n<script>\nlet a = 1 < 2;\n</script>\n",
		"a.json":     "{\"k\": [1, 2.5e3, true, null, \"😀\"]}\n",
		"a.css":      "div > .x:hover { color: #fff; margin: 0 auto; }\n",
		"a.c":        "#include <stdio.h>\nint main(void) { return 0; }\n",
		"a.md":       "# Title\n\nSome *text* with `code`.\n\n```go\nfunc main() {}\n```\n",
		"a.yaml":     "key: value\nlist:\n  - 1\n  - \"two\"\nblock: |\n  a\n  b\n",
		"a.sh":       "#!/bin/sh\nfor f in *; do echo \"$f\" | tr a-z A-Z; done\n",
		"a.sql":      "SELECT a, COUNT(*) FROM t WHERE b = 'x' GROUP BY a;\n",
		"a.rb":       "x = :sym\ndef y = \"#{x}\"\n",
		"a.java":     "@Override\npublic final class A<T> extends B { int x = 0x1F; }\n",
		"Dockerfile": "FROM golang:1.27 AS build\nRUN go build ./...\n",
		"Makefile":   "all: build\n\tgo build -o bb ./cmd/bb\n",
	}
	for _, name := range []string{"highlight.go", "patch.go", "lexers.go"} {
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		texts[name] = strings.ReplaceAll(string(source), "\r\n", "\n")
	}
	for name, text := range texts {
		spans, ok := Lines(name, text, later())
		if !ok {
			t.Errorf("%s was not highlighted", name)
			continue
		}
		checkCovers(t, name, strings.Split(strings.TrimSuffix(text, "\n"), "\n"), spans)
	}
}

// A line ends at \n, a last \n adds no empty line, a text with no last \n
// still has its last line, and a \r\n is read as \n: the spans cover the line
// without its \r. A \r anywhere else is text.
func TestLinesSplitsAtNewlines(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		text  string
		lines []string
	}{
		{"", nil},
		{"\n", []string{""}},
		{"x", []string{"x"}},
		{"x\n", []string{"x"}},
		{"x\n\n", []string{"x", ""}},
		{"x := 1\r\ny\r\n", []string{"x := 1", "y"}},
		{"x\r\r\n", []string{"x\r"}},
		{"a\rb\n", []string{"a\rb"}},
	} {
		spans, ok := Lines("a.go", test.text, later())
		if !ok {
			t.Errorf("%q was not highlighted", test.text)
			continue
		}
		checkCovers(t, strconv.Quote(test.text), test.lines, spans)
	}
}

// Text that is not UTF-8 is measured as JSON carries it: each invalid byte as
// one U+FFFD.
func TestLinesMeasuresInvalidUTF8AsJSONDoes(t *testing.T) {
	t.Parallel()

	spans, ok := Lines("a.go", "s := \"\xff\xfe\" // \xe2\x82\n", later())
	if !ok {
		t.Fatal("a.go was not highlighted")
	}
	if got := len(classes(t, spans[0])); got != 15 {
		t.Errorf("spans %q cover %d units, want 15: each invalid byte one unit", spans[0], got)
	}
}

// No lexer, a plain-text file, too much text or a deadline gone by: no spans.
func TestLinesDeclines(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name, path, text string
		deadline         time.Time
	}{
		{"no lexer for the name", "data.unknownext", "x\n", later()},
		{"plain text", "notes.txt", "x\n", later()},
		{"no name", "", "x\n", later()},
		{"larger than MaxBytes", "a.go", strings.Repeat("x", MaxBytes+1), later()},
		{"deadline passed", "a.go", "x\n", time.Now().Add(-time.Second)},
	} {
		if lines, ok := Lines(test.path, test.text, test.deadline); ok || lines != nil {
			t.Errorf("%s: got %q, %v; want nil, false", test.name, lines, ok)
		}
	}
	if _, ok := Lines("a.go", strings.Repeat("x", MaxBytes), later()); !ok {
		t.Error("a text of MaxBytes was not highlighted")
	}
}

// A pattern that backtracks without end stops at its match timeout and is
// tried again at the next position, and the next: unbounded, each of the
// first positions here costs a whole timeout, seconds in all. The deadline
// stops the text within one token of it.
func TestTokenizeStopsWithinOneTokenOfTheDeadline(t *testing.T) {
	t.Parallel()

	lexer := chroma.MustNewLexer(&chroma.Config{Name: "backtracking"}, func() chroma.Rules {
		return chroma.Rules{"root": {
			{Pattern: `(a+)+b`, Type: chroma.Keyword},
			{Pattern: `.|\n`, Type: chroma.Text},
		}}
	})
	start := time.Now()
	lines, ok := tokenize(lexer, strings.Repeat("a", 40)+"\n", time.Now().Add(50*time.Millisecond))
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("tokenizing past the deadline took %v", elapsed)
	}
	if ok || lines != nil {
		t.Errorf("got %q, %v past the deadline; want nil, false", lines, ok)
	}
}

// A lexer that panics, or yields tokens that are not the text, leaves the text
// uncoloured. One that rewrites text without changing its length is caught
// too: chroma lexes a Markdown code block with its default options, which
// turn a \r into \n, a line break the text does not have.
func TestTokenizeDeclinesWhatItCannotTrust(t *testing.T) {
	t.Parallel()

	for name, rules := range map[string]chroma.Rules{
		"panics on an undefined state": {"root": {
			{Pattern: `a`, Type: chroma.Keyword, Mutator: chroma.Push("undefined")},
		}},
		"drops text": {"root": {
			{Pattern: `(a)b`, Type: chroma.ByGroups(chroma.Keyword)},
			{Pattern: `.|\n`, Type: chroma.Text},
		}},
		"rewrites text": {"root": {
			{Pattern: `a`, Type: chroma.EmitterFunc(func([]string, *chroma.LexerState) chroma.Iterator {
				return chroma.Literator(chroma.Token{Type: chroma.Keyword, Value: "\n"})
			})},
			{Pattern: `.|\n`, Type: chroma.Text},
		}},
		"drops its last text": {"root": {
			{Pattern: `[^\n]+`, Type: chroma.Text},
			{Pattern: `\n`},
		}},
		"does not compile": {"root": {
			{Pattern: `(`, Type: chroma.Text},
		}},
	} {
		lexer := chroma.MustNewLexer(&chroma.Config{Name: name}, func() chroma.Rules { return rules })
		if lines, ok := tokenize(lexer, "abab\n", later()); ok || lines != nil {
			t.Errorf("a lexer that %s: got %q, %v; want nil, false", name, lines, ok)
		}
	}
	if lines, ok := Lines("README.md", "```go\nx\ry\n```\n", later()); ok || lines != nil {
		t.Errorf("a Markdown code block holding a \\r: got %q, %v; want nil, false", lines, ok)
	}
}

// Every chroma type has a class: the types the view's classes are named for
// have the class named for them, each type classed apart from its group has
// the class chosen for it, and the rest have their group's.
func TestClassOf(t *testing.T) {
	t.Parallel()

	for _, tokenType := range chroma.TokenTypeValues() {
		if class := classOf(tokenType); !strings.ContainsRune("kybfsencmgaot", rune(class)) {
			t.Errorf("%s has class %q", tokenType, class)
		}
	}
	for tokenType, want := range map[chroma.TokenType]byte{
		chroma.Keyword:               ClassKeyword,
		chroma.KeywordDeclaration:    ClassKeyword,
		chroma.KeywordNamespace:      ClassKeyword,
		chroma.OperatorWord:          ClassKeyword,
		chroma.OperatorReserved:      ClassKeyword,
		chroma.NameKeyword:           ClassKeyword,
		chroma.GenericHeading:        ClassKeyword,
		chroma.GenericSubheading:     ClassKeyword,
		chroma.KeywordType:           ClassType,
		chroma.NameClass:             ClassType,
		chroma.NameException:         ClassType,
		chroma.KeywordConstant:       ClassConstant,
		chroma.NameBuiltin:           ClassConstant,
		chroma.NameBuiltinPseudo:     ClassConstant,
		chroma.NameConstant:          ClassConstant,
		chroma.NamePseudo:            ClassConstant,
		chroma.NameVariableMagic:     ClassConstant,
		chroma.LiteralStringBoolean:  ClassConstant,
		chroma.LiteralStringSymbol:   ClassConstant,
		chroma.LiteralStringAtom:     ClassConstant,
		chroma.NameFunction:          ClassFunction,
		chroma.NameFunctionMagic:     ClassFunction,
		chroma.LiteralString:         ClassString,
		chroma.LiteralStringDouble:   ClassString,
		chroma.LiteralStringDoc:      ClassString,
		chroma.Literal:               ClassString,
		chroma.LiteralOther:          ClassString,
		chroma.CommentPreprocFile:    ClassString,
		chroma.LiteralStringEscape:   ClassEscape,
		chroma.LiteralStringInterpol: ClassEscape,
		chroma.NameEntity:            ClassEscape,
		chroma.LiteralNumberHex:      ClassNumber,
		chroma.LiteralDate:           ClassNumber,
		chroma.CommentSingle:         ClassComment,
		chroma.CommentHashbang:       ClassComment,
		chroma.CommentPreproc:        ClassMeta,
		chroma.NameDecorator:         ClassMeta,
		chroma.GenericPrompt:         ClassMeta,
		chroma.NameTag:               ClassTag,
		chroma.NameAttribute:         ClassAttribute,
		chroma.NameProperty:          ClassAttribute,
		chroma.Operator:              ClassOperator,
		chroma.NameOperator:          ClassOperator,
		chroma.Name:                  ClassText,
		chroma.NameVariable:          ClassText,
		chroma.NameNamespace:         ClassText,
		chroma.NameLabel:             ClassText,
		chroma.Punctuation:           ClassText,
		chroma.TextWhitespace:        ClassText,
		chroma.GenericEmph:           ClassText,
		chroma.Error:                 ClassText,
		chroma.Other:                 ClassText,
	} {
		if got := classOf(tokenType); got != want {
			t.Errorf("%s has class %c, want %c", tokenType, got, want)
		}
	}
}
