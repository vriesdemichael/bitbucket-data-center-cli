package main

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// messageOfDirective declares that the next heading or table quotes messages a
// Go program prints, and names the programs: `<!-- docs-lint: message-of bb -->`.
//
// A troubleshooting entry is found by pasting an error into a search, so a quote
// the binary no longer prints is an entry nobody reaches, and nothing else
// notices: the invocation check reads what a reader types and the schema check
// reads what a command returns, never the text it fails with.
//
// Declared rather than inferred, as output-of is. The same pages put commands,
// configuration keys and Go's own TLS errors in code spans, and guessing which
// of those are bb's messages either misses them or fails on quotes bb never
// claimed. See ADR-087.
const messageOfDirective = "docs-lint: message-of"

// ownProducer names this module's Go source under cmd/ and internal/. Any other
// producer is a Go import path, such as crypto/x509 for a TLS error bb passes
// through, and is read from the toolchain.
const ownProducer = "bb"

// anchorWords is how many consecutive words of a quote must fall on literal
// source text.
//
// A template made mostly of verbs -- `invalid %s`, `%s: %s` -- can print nearly
// anything, so without an anchor it would vouch for a quote whose message is
// gone. A quote whose stretches between placeholders are all shorter must match
// its longest stretch whole instead.
const anchorWords = 3

type messageQuote struct {
	line      int
	text      string
	producers []string
}

var (
	headingLine    = regexp.MustCompile(`^#{1,6}\s+(.*?)\s*#*$`)
	singleCodeSpan = regexp.MustCompile("^`([^`]+)`$")
	delimiterCell  = regexp.MustCompile(`^:?-+:?$`)
	// printfVerb is one fmt verb with its flags, width, precision and argument
	// index, as in %s, %-10v, %.2f or %[1]q.
	printfVerb = regexp.MustCompile(`^%[-+# 0]*(?:\[\d+\])?(?:\d+|\*)?(?:\.(?:\d+|\*)?)?(?:\[\d+\])?[a-zA-Z]`)
)

// lintMessageQuotes checks every quote a document declares against the source of
// the producers its directive names.
func lintMessageQuotes(file, contents string) []finding {
	quotes, findings := extractMessageQuotes(file, contents)

	for _, quote := range quotes {
		if problem := checkMessageQuote(quote); problem != "" {
			findings = append(findings, finding{
				File:    file,
				Line:    quote.line,
				Command: quote.text,
				Problem: problem,
			})
		}
	}

	return findings
}

func checkMessageQuote(quote messageQuote) string {
	loaded := make([][]messageTemplate, 0, len(quote.producers))
	for _, producer := range quote.producers {
		templates, err := templatesOf(producer)
		if err != nil {
			return err.Error()
		}
		loaded = append(loaded, templates)
	}

	pattern := quotePattern(quote.text)
	if len(anchorWindows(pattern)) == 0 {
		return "the quote has no words, so there is nothing to check it against"
	}

	for _, templates := range loaded {
		if quoteMatches(pattern, templates) {
			return ""
		}
	}

	return fmt.Sprintf(
		"no message in %s prints this quote; if the message changed, quote what the source prints now rather than rewording the source to match",
		describeProducers(quote.producers))
}

func describeProducers(producers []string) string {
	described := make([]string, 0, len(producers))
	for _, producer := range producers {
		if producer == ownProducer {
			described = append(described, "bb's source under cmd/ and internal/")
			continue
		}
		described = append(described, producer)
	}

	return strings.Join(described, " or ")
}

// extractMessageQuotes returns the quotes a document declares, and a finding for
// every directive that declares none.
//
// A declared heading must be one code span, which is the quote. In a declared
// table, each first-column cell that is one code span is a quote, and a cell
// describing a symptom in prose is not. A directive followed by anything else
// fails rather than checking nothing, so a table moved away from its directive
// does not go quietly unchecked.
func extractMessageQuotes(file, contents string) ([]messageQuote, []finding) {
	var (
		quotes      []messageQuote
		findings    []finding
		fence       string
		declared    bool
		producers   []string
		pendingLine int
		pendingText string
	)

	fail := func(line int, command, problem string) {
		findings = append(findings, finding{File: file, Line: line, Command: command, Problem: problem})
	}

	lines := strings.Split(strings.ReplaceAll(contents, "\r\n", "\n"), "\n")

	for index := 0; index < len(lines); index++ {
		trimmed := strings.TrimSpace(lines[index])

		if fence != "" {
			if strings.HasPrefix(trimmed, fence) && strings.TrimSpace(strings.TrimPrefix(trimmed, fence)) == "" {
				fence = ""
			}
			continue
		}

		if named, ok := messageOfProducers(trimmed); ok {
			if declared {
				fail(pendingLine, pendingText, "directive is followed by another directive rather than the heading or table it declares")
			}
			declared = len(named) > 0
			if !declared {
				fail(index+1, trimmed, "directive names no producer; write message-of bb, adding the Go import path of any other package whose message the block quotes")
				continue
			}
			producers, pendingLine, pendingText = named, index+1, trimmed
			continue
		}

		marker, _, isFence := openingFence(trimmed)
		if isFence {
			fence = marker
		}

		if !declared || trimmed == "" {
			continue
		}
		declared = false

		switch {
		case isFence:
			fail(pendingLine, pendingText, "directive applies to the next heading or table, and a code block follows it")
		case headingLine.MatchString(trimmed):
			span := singleCodeSpan.FindStringSubmatch(headingLine.FindStringSubmatch(trimmed)[1])
			if span == nil {
				fail(index+1, trimmed, "a declared heading must be one code span quoting the message")
				continue
			}
			quotes = append(quotes, messageQuote{line: index + 1, text: span[1], producers: producers})
		case strings.HasPrefix(trimmed, "|"):
			start, found := index, 0
			for ; index < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[index]), "|"); index++ {
				// The header row names the columns rather than quoting anything.
				if index == start {
					continue
				}
				cells := tableCells(strings.TrimSpace(lines[index]))
				if delimiterCell.MatchString(cells[0]) {
					continue
				}
				span := singleCodeSpan.FindStringSubmatch(cells[0])
				if span == nil {
					continue
				}
				quotes = append(quotes, messageQuote{
					line:      index + 1,
					text:      strings.ReplaceAll(span[1], `\|`, "|"),
					producers: producers,
				})
				found++
			}
			// Leave the last row to the loop's own increment.
			index--
			if found == 0 {
				fail(start+1, trimmed, "declared table has no first-column cell that is one code span, so it quotes no message")
			}
		default:
			fail(pendingLine, pendingText, "directive applies to the next heading or table, and prose follows it")
		}
	}

	if declared {
		fail(pendingLine, pendingText, "directive is not followed by the heading or table it declares")
	}

	return quotes, findings
}

// messageOfProducers reads a message-of directive, returning the producers it
// names and whether the line is one at all.
func messageOfProducers(trimmed string) ([]string, bool) {
	inner, ok := directiveBody(trimmed)
	if !ok || (inner != messageOfDirective && !strings.HasPrefix(inner, messageOfDirective+" ")) {
		return nil, false
	}

	return strings.Fields(strings.TrimPrefix(inner, messageOfDirective)), true
}

// tableCells splits a table row on the pipes that separate cells, keeping an
// escaped \| inside the cell it belongs to.
func tableCells(row string) []string {
	row = strings.TrimPrefix(row, "|")
	if strings.HasSuffix(row, "|") && !strings.HasSuffix(row, `\|`) {
		row = strings.TrimSuffix(row, "|")
	}

	var (
		cells   []string
		current strings.Builder
	)
	for index := 0; index < len(row); index++ {
		switch {
		case row[index] == '\\' && index+1 < len(row) && row[index+1] == '|':
			current.WriteString(`\|`)
			index++
		case row[index] == '|':
			cells = append(cells, strings.TrimSpace(current.String()))
			current.Reset()
		default:
			current.WriteByte(row[index])
		}
	}

	return append(cells, strings.TrimSpace(current.String()))
}

// patternToken is one character of a quote or a template, or a wildcard
// standing for any text, including none.
type patternToken struct {
	char     rune
	wildcard bool
}

type pattern []patternToken

func (p pattern) withWildcard() pattern {
	if len(p) > 0 && p[len(p)-1].wildcard {
		return p
	}

	return append(p, patternToken{wildcard: true})
}

// key spells a pattern as a string, for deduplication.
func (p pattern) key() string {
	var builder strings.Builder
	for _, token := range p {
		if token.wildcard {
			builder.WriteRune(0)
			continue
		}
		builder.WriteRune(token.char)
	}

	return builder.String()
}

// quotePattern reads a documented quote, where `...` and `…` stand for text the
// message carries and the documentation elides.
func quotePattern(quote string) pattern {
	var result pattern
	for quote != "" {
		switch {
		case strings.HasPrefix(quote, "..."):
			result = result.withWildcard()
			quote = quote[len("..."):]
		case strings.HasPrefix(quote, "…"):
			result = result.withWildcard()
			quote = quote[len("…"):]
		default:
			char, size := utf8.DecodeRuneInString(quote)
			result = append(result, patternToken{char: char})
			quote = quote[size:]
		}
	}

	return result
}

// appendFormat appends a Go string's value, reading each fmt verb as a wildcard.
//
// Every literal is read this way, whether or not it reaches a format call. A
// stray %s in a string nothing formats only makes the template more permissive,
// and telling the two apart would need the types of every call.
func appendFormat(result pattern, text string) pattern {
	for text != "" {
		if text[0] == '%' {
			if strings.HasPrefix(text, "%%") {
				result = append(result, patternToken{char: '%'})
				text = text[2:]
				continue
			}
			if verb := printfVerb.FindString(text); verb != "" {
				result = result.withWildcard()
				text = text[len(verb):]
				continue
			}
		}
		char, size := utf8.DecodeRuneInString(text)
		result = append(result, patternToken{char: char})
		text = text[size:]
	}

	return result
}

// messageTemplate is a message the source can print: literal text with
// wildcards where a verb or a non-literal operand supplies the rest.
type messageTemplate struct {
	tokens pattern
	// segments are the runs of literal text between wildcards, which is where an
	// anchor can land.
	segments []templateSegment
}

type templateSegment struct {
	start int
	text  string
}

func newMessageTemplate(tokens pattern) messageTemplate {
	template := messageTemplate{tokens: tokens}

	var builder strings.Builder
	start := 0
	flush := func() {
		if builder.Len() > 0 {
			template.segments = append(template.segments, templateSegment{start: start, text: builder.String()})
			builder.Reset()
		}
	}
	for index, token := range tokens {
		if token.wildcard {
			flush()
			start = index + 1
			continue
		}
		builder.WriteRune(token.char)
	}
	flush()

	return template
}

// templatesFromGo reads every message a Go file can print: each string literal,
// and each + chain containing one, with the chain's other operands as wildcards.
func templatesFromGo(filename string, source []byte) ([]pattern, error) {
	parsed, err := parser.ParseFile(token.NewFileSet(), filename, source, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}

	var (
		templates []pattern
		visit     func(ast.Node) bool
	)
	visit = func(node ast.Node) bool {
		switch expression := node.(type) {
		case *ast.ImportSpec:
			return false
		case *ast.BinaryExpr:
			if expression.Op != token.ADD {
				return true
			}
			var (
				template   pattern
				hasLiteral bool
			)
			for _, operand := range concatenationOperands(expression, nil) {
				if value, ok := stringLiteral(operand); ok {
					template = appendFormat(template, value)
					hasLiteral = true
					continue
				}
				template = template.withWildcard()
				// A call inside the chain can carry messages of its own.
				ast.Inspect(operand, visit)
			}
			if hasLiteral {
				templates = append(templates, template)
			}
			return false
		case *ast.BasicLit:
			if value, ok := stringLiteral(expression); ok {
				templates = append(templates, appendFormat(nil, value))
			}
		}
		return true
	}
	ast.Inspect(parsed, visit)

	return templates, nil
}

func concatenationOperands(expression ast.Expr, operands []ast.Expr) []ast.Expr {
	switch typed := expression.(type) {
	case *ast.ParenExpr:
		if inner, ok := typed.X.(*ast.BinaryExpr); ok && inner.Op == token.ADD {
			return concatenationOperands(inner, operands)
		}
	case *ast.BinaryExpr:
		if typed.Op == token.ADD {
			return concatenationOperands(typed.Y, concatenationOperands(typed.X, operands))
		}
	}

	return append(operands, expression)
}

func stringLiteral(expression ast.Expr) (string, bool) {
	literal, ok := expression.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(literal.Value)

	return value, err == nil
}

// anchorWindow is a stretch of a quote, between placeholders, spanning the
// required number of whole words.
type anchorWindow struct {
	start, end int
	text       string
}

func anchorWindows(quote pattern) []anchorWindow {
	type span struct{ start, end int }

	var fragments [][]span
	var words []span
	inWord := false
	for index, token := range quote {
		isWordChar := !token.wildcard && (unicode.IsLetter(token.char) || unicode.IsDigit(token.char))
		switch {
		case isWordChar && !inWord:
			words = append(words, span{start: index, end: index + 1})
		case isWordChar:
			words[len(words)-1].end = index + 1
		}
		inWord = isWordChar
		if token.wildcard {
			fragments = append(fragments, words)
			words = nil
		}
	}
	fragments = append(fragments, words)

	required := 0
	for _, fragment := range fragments {
		required = max(required, min(anchorWords, len(fragment)))
	}
	if required == 0 {
		return nil
	}

	var windows []anchorWindow
	for _, fragment := range fragments {
		for first := 0; first+required <= len(fragment); first++ {
			start, end := fragment[first].start, fragment[first+required-1].end
			windows = append(windows, anchorWindow{start: start, end: end, text: quote[start:end].key()})
		}
	}

	return windows
}

// quoteMatches reports whether one of the templates can print the quote.
//
// It can when some text for the wildcards on both sides makes the quote part of
// the message, and an anchor window of the quote lies on the template's literal
// text. The window is found by plain search; what remains on either side only
// has to be compatible, which is where the quote's placeholders and the
// template's verbs absorb each other and the words an argument supplies.
func quoteMatches(quote pattern, templates []messageTemplate) bool {
	windows := anchorWindows(quote)

	for _, template := range templates {
		for _, window := range windows {
			if matchesAround(quote, window, template) {
				return true
			}
		}
	}

	return false
}

func matchesAround(quote pattern, window anchorWindow, template messageTemplate) bool {
	windowLength := window.end - window.start
	wildcard := pattern{{wildcard: true}}

	for _, segment := range template.segments {
		for offset := 0; offset <= len(segment.text); {
			found := strings.Index(segment.text[offset:], window.text)
			if found < 0 {
				break
			}
			byteAt := offset + found
			at := segment.start + utf8.RuneCountInString(segment.text[:byteAt])

			// The quote may begin and end anywhere in the message, so what comes
			// before the window only has to end the message's opening, and what
			// comes after it only has to begin the rest.
			before := append(append(pattern{}, wildcard...), quote[:window.start]...)
			after := append(append(pattern{}, quote[window.end:]...), wildcard...)
			if patternsIntersect(before, template.tokens[:at]) && patternsIntersect(after, template.tokens[at+windowLength:]) {
				return true
			}

			offset = byteAt + 1
		}
	}

	return false
}

// patternsIntersect reports whether some text matches both patterns, each
// wildcard standing for any text, including none.
func patternsIntersect(left, right pattern) bool {
	rows, columns := len(left), len(right)
	// possible[i][j]: left[i:] and right[j:] can produce the same text.
	possible := make([][]bool, rows+1)
	for index := range possible {
		possible[index] = make([]bool, columns+1)
	}

	for i := rows; i >= 0; i-- {
		for j := columns; j >= 0; j-- {
			switch {
			case i == rows && j == columns:
				possible[i][j] = true
			case i < rows && left[i].wildcard:
				possible[i][j] = possible[i+1][j] || (j < columns && possible[i][j+1])
			case j < columns && right[j].wildcard:
				possible[i][j] = possible[i][j+1] || (i < rows && possible[i+1][j])
			case i < rows && j < columns:
				possible[i][j] = left[i].char == right[j].char && possible[i+1][j+1]
			}
		}
	}

	return possible[0][0]
}

type producerSource struct {
	once      sync.Once
	templates []messageTemplate
	err       error
}

var (
	producerSourcesMu sync.Mutex
	producerSources   = map[string]*producerSource{}
)

// templatesOf loads a producer's templates once per process, however many
// quotes and documents name it.
func templatesOf(producer string) ([]messageTemplate, error) {
	producerSourcesMu.Lock()
	source, ok := producerSources[producer]
	if !ok {
		source = &producerSource{}
		producerSources[producer] = source
	}
	producerSourcesMu.Unlock()

	source.once.Do(func() {
		source.templates, source.err = loadProducer(producer)
	})

	return source.templates, source.err
}

func loadProducer(producer string) ([]messageTemplate, error) {
	if producer == ownProducer {
		root, err := moduleRoot()
		if err != nil {
			return nil, err
		}
		return templatesUnder([]string{filepath.Join(root, "cmd"), filepath.Join(root, "internal")}, true)
	}

	// Named in documentation, and passed to go list as an argument: a leading
	// dash would be read as a flag instead.
	if strings.HasPrefix(producer, "-") {
		return nil, fmt.Errorf("producer %q is neither bb nor a Go import path", producer)
	}

	output, err := exec.Command("go", "list", "-f", "{{.Dir}}", producer).Output()
	dir := strings.TrimSpace(string(output))
	if err != nil || dir == "" {
		return nil, fmt.Errorf("producer %q is neither bb nor a Go package the toolchain can find", producer)
	}

	return templatesUnder([]string{dir}, false)
}

// moduleRoot finds the directory holding go.mod, from wherever the linter runs:
// the repository root under task, or tools/docs-lint under go test.
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("docs-lint: no go.mod above the working directory, so bb's source cannot be read")
		}
		dir = parent
	}
}

// templatesUnder reads the templates of every non-test Go file in dirs, and in
// their subdirectories when recursive.
func templatesUnder(dirs []string, recursive bool) ([]messageTemplate, error) {
	var (
		templates []messageTemplate
		seen      = map[string]bool{}
	)

	for _, dir := range dirs {
		walkErr := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if path != dir && (!recursive || entry.Name() == "testdata") {
					return fs.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				return nil
			}

			source, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			patterns, err := templatesFromGo(path, source)
			if err != nil {
				return fmt.Errorf("parse %s: %w", path, err)
			}
			for _, tokens := range patterns {
				if key := tokens.key(); !seen[key] {
					seen[key] = true
					templates = append(templates, newMessageTemplate(tokens))
				}
			}
			return nil
		})
		if walkErr != nil {
			return nil, walkErr
		}
	}

	return templates, nil
}
