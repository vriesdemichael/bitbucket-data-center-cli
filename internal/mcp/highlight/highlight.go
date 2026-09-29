// Package highlight tokenizes code for the views to colour. A view never
// parses a string as HTML, so it cannot take highlighted markup; it takes
// each line's spans instead, a class and a length each, and colours the text
// it already has.
//
// A line's spans are joined by single spaces. Each is a class letter followed
// by its length in UTF-16 code units, the length JavaScript gives a string,
// and together they cover the line in order, with no two neighbours of one
// class: "k7 t1 f4 t1 s5" is a keyword of seven units, a unit of plain text, a
// function name of four, and so on. An empty line is "".
//
// Lines are split at each \n, and a text that ends in \n has no empty line
// after it. A line that ends in \r\n is described without its \r, so the
// spans cover what a person sees; a view that kept the \r in a line's text
// finds the spans one unit short of it, and draws that unit, which shows as
// nothing, plain. Text that is not valid UTF-8 is measured as JSON carries it
// to the view: each invalid byte as U+FFFD, one unit.
package highlight

import (
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2"
)

// MaxBytes is the most text tokenized at once: the most of one file a view
// carries (maxViewFileBytes in internal/mcp). A larger text is not
// highlighted, so one file's cost stays small.
const MaxBytes = 64 << 10

// The classes of a span, one letter each. Every chroma token type maps to one
// of them in classOf.
const (
	// ClassKeyword is a keyword, a word used as an operator such as and or
	// instanceof, or a heading in markup.
	ClassKeyword = 'k'
	// ClassType is the name of a type, a class or an exception.
	ClassType = 'y'
	// ClassConstant is a constant or a builtin: true and nil, len and self, a
	// symbol or atom such as Ruby's :name, a magic name such as __file__.
	ClassConstant = 'b'
	// ClassFunction is the name of a function.
	ClassFunction = 'f'
	// ClassString is a string, or a literal written as one, such as a plain
	// YAML value or the file an #include names.
	ClassString = 's'
	// ClassEscape is an escape or an interpolation inside a string, or a
	// character reference such as &amp;, which is markup's escape.
	ClassEscape = 'e'
	// ClassNumber is a number or a date.
	ClassNumber = 'n'
	// ClassComment is a comment.
	ClassComment = 'c'
	// ClassMeta is a preprocessor directive, a decorator or annotation, or a
	// console prompt: text about the code rather than in it.
	ClassMeta = 'm'
	// ClassTag is a markup tag, or what lexers mark as one, such as a JSON key
	// or a CSS selector.
	ClassTag = 'g'
	// ClassAttribute is an attribute of a tag, or a property of an object.
	ClassAttribute = 'a'
	// ClassOperator is an operator written with symbols.
	ClassOperator = 'o'
	// ClassText is everything else: plain names and variables, punctuation,
	// whitespace, and text a lexer could not read.
	ClassText = 't'
)

// Lines returns the spans of each line of text, tokenized as the language
// path names by its file name. ok is false when no lexer matches the name, the
// text is larger than MaxBytes, or tokenizing fails or runs past the deadline.
// A plain-text file has no lexer here: its every span would be ClassText.
func Lines(path, text string, deadline time.Time) (lines []string, ok bool) {
	if len(text) > MaxBytes {
		return nil, false
	}
	lexer := lexerFor(path)
	if lexer == nil {
		return nil, false
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if text == "" {
		return []string{}, true
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return tokenize(lexer, text, deadline)
}

// tokenize returns the spans of each line of text, which ends in \n.
//
// Chroma's lexers are regular expressions run by dlclark/regexp2, which
// backtracks, so one input can make a match slow, and then the next one along
// as slow. Chroma gives each match a timeout of 250 ms, set on every rule it
// compiles and not exposed to change, which regexp2 enforces with a clock
// that ticks every 100 ms, so a match stops within about 450 ms. A match that
// times out counts as no match, and the lexer goes on to its next rule and
// its next position: the timeout bounds a match, not a text. The deadline
// bounds the text. It is checked after every token, so the work stops within
// one token of it, and a token costs the rules tried until one yields it,
// each within its timeout. The lexers that lex a whole text before their
// first token are replaced in the registry (see replaceLexAhead), so every
// lexer here yields as it goes. Chroma's Coalesce is not used either: it reads
// on through a run of tokens of one type, up to 8 KiB of them, before it
// yields one. The spans are coalesced here instead, between deadline checks.
func tokenize(lexer chroma.Lexer, text string, deadline time.Time) (lines []string, ok bool) {
	if time.Now().After(deadline) {
		return nil, false
	}
	// Chroma reads text as runes, and gives back an invalid byte as U+FFFD,
	// as JSON does. Reading it that way first keeps the tokens equal to the
	// text they are checked against below.
	if !utf8.ValidString(text) {
		text = string([]rune(text))
	}
	defer func() {
		// A lexer that cannot follow its input can panic, as chroma's own
		// iterator does for a state its rules do not define. That leaves a
		// file uncoloured, not a view broken.
		if recover() != nil {
			lines, ok = nil, false
		}
	}()
	// EnsureLF is off: chroma would turn a \r into \n, a line break the view
	// does not have. The \r of a \r\n is gone already; any other is text.
	iterator, err := lexer.Tokenise(&chroma.TokeniseOptions{State: "root"}, text)
	if err != nil {
		return nil, false
	}
	spans := spanWriter{}
	offset := 0
	for token := iterator(); token != chroma.EOF; token = iterator() {
		if time.Now().After(deadline) {
			return nil, false
		}
		// A lexer that drops or rewrites text would leave the spans
		// describing something other than the line; such a text is left
		// uncoloured rather than coloured wrongly.
		if !strings.HasPrefix(text[offset:], token.Value) {
			return nil, false
		}
		offset += len(token.Value)
		spans.write(classOf(token.Type), token.Value)
	}
	if offset != len(text) {
		return nil, false
	}
	return spans.lines, true
}

// spanWriter encodes tokens as the spans of each line.
type spanWriter struct {
	lines []string
	line  []byte // the current line's spans, without the open one
	class byte   // the open span's class, or 0 when none is open
	units int    // the open span's length
}

// write adds a token of one class, which may end lines.
func (w *spanWriter) write(class byte, value string) {
	for {
		end := strings.IndexByte(value, '\n')
		part := value
		if end >= 0 {
			part = value[:end]
		}
		if part != "" {
			if class != w.class {
				w.closeSpan()
				w.class = class
			}
			w.units += utf16Len(part)
		}
		if end < 0 {
			return
		}
		w.closeSpan()
		w.lines = append(w.lines, string(w.line))
		w.line = w.line[:0]
		value = value[end+1:]
	}
}

func (w *spanWriter) closeSpan() {
	if w.class == 0 {
		return
	}
	if len(w.line) > 0 {
		w.line = append(w.line, ' ')
	}
	w.line = append(w.line, w.class)
	w.line = strconv.AppendInt(w.line, int64(w.units), 10)
	w.class, w.units = 0, 0
}

// utf16Len is the length of s in JavaScript, where a character beyond the
// Basic Multilingual Plane, such as an emoji, is two units.
func utf16Len(s string) int {
	units := 0
	for _, r := range s {
		if r > 0xFFFF {
			units += 2
		} else {
			units++
		}
	}
	return units
}

// classOf is the class of a chroma token type. Chroma groups its types in
// categories of a thousand and subcategories of a hundred: KeywordType is in
// the keywords at 1000, NameBuiltinPseudo in the builtins at 2100. The types
// whose class differs from their group's are named first, so a type chroma
// adds later lands with its group.
func classOf(t chroma.TokenType) byte {
	switch t {
	case chroma.KeywordType, chroma.NameClass, chroma.NameException:
		return ClassType
	case chroma.KeywordConstant, chroma.NameConstant, chroma.NamePseudo, chroma.NameVariableMagic,
		chroma.LiteralStringBoolean, chroma.LiteralStringSymbol, chroma.LiteralStringAtom:
		return ClassConstant
	case chroma.LiteralStringEscape, chroma.LiteralStringInterpol, chroma.NameEntity:
		return ClassEscape
	// Word operators and headings read as keywords.
	case chroma.OperatorWord, chroma.OperatorReserved, chroma.NameKeyword, chroma.GenericHeading, chroma.GenericSubheading:
		return ClassKeyword
	case chroma.NameDecorator, chroma.GenericPrompt:
		return ClassMeta
	case chroma.NameTag:
		return ClassTag
	case chroma.NameAttribute, chroma.NameProperty:
		return ClassAttribute
	case chroma.NameOperator:
		return ClassOperator
	case chroma.LiteralDate:
		return ClassNumber
	// The file an #include names is written as a string.
	case chroma.CommentPreprocFile:
		return ClassString
	}
	switch t.SubCategory() {
	case chroma.NameBuiltin:
		return ClassConstant
	case chroma.NameFunction:
		return ClassFunction
	case chroma.LiteralString:
		return ClassString
	case chroma.LiteralNumber:
		return ClassNumber
	case chroma.CommentPreproc:
		return ClassMeta
	}
	switch t.Category() {
	case chroma.Keyword:
		return ClassKeyword
	// Literal and LiteralOther are values written as strings, such as YAML's
	// plain scalars.
	case chroma.Literal:
		return ClassString
	case chroma.Operator:
		return ClassOperator
	case chroma.Comment:
		return ClassComment
	}
	// Plain names, namespaces, labels and variables; punctuation, text and
	// whitespace; the generic types of markup and console output, whose
	// emphasis a colour cannot carry; and chroma's Error and Other.
	return ClassText
}
