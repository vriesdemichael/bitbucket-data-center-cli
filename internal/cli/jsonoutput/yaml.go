package jsonoutput

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// encodeYAML renders one JSON document as a YAML document with the same meaning.
//
// It starts from the JSON, not from the Go value, so that --yaml prints the
// document --json prints: the same keys in the same order, and the same
// numbers. The JSON is read token by token because decoding it into maps would
// lose the key order, and decoding numbers into float64 would lose digits.
//
// yaml.v3 only lays the tree out: it writes each node in the style the node
// carries, and a plain scalar stays plain whatever it spells. So every style is
// chosen here, and a scalar is plain only when no resolver of YAML 1.1 or 1.2
// reads it as anything else. YAML 1.1, which PyYAML and Ruby's Psych follow,
// reads yes, 012, 1:20 and 2001-12-14 as a boolean, two numbers and a date, and
// each parser adds cases of its own. The document then decodes to the JSON
// value whichever parser reads it.
func encodeYAML(jsonDocument []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(jsonDocument))
	decoder.UseNumber()

	root, err := yamlValue(decoder)
	if err == nil {
		// Anything after the value, even a second value, is not one document.
		var token json.Token
		if token, err = decoder.Token(); err == nil {
			err = fmt.Errorf("unexpected %v after the value", token)
		} else if errors.Is(err, io.EOF) {
			err = nil
		}
	} else if errors.Is(err, io.EOF) {
		err = io.ErrUnexpectedEOF
	}
	if err != nil {
		return nil, fmt.Errorf("reading the JSON document: %w", err)
	}

	quoteKeptFinalBlock(root)

	buffer := &bytes.Buffer{}
	encoder := yaml.NewEncoder(buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(root); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}

	return buffer.Bytes(), nil
}

// yamlValue reads the next JSON value from decoder as a YAML node.
func yamlValue(decoder *json.Decoder) (*yaml.Node, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}

	switch token := token.(type) {
	case json.Delim:
		// Where a value starts, Token returns only an opening delimiter; a
		// closing one there is a syntax error it reports itself.
		if token == '{' {
			return yamlMapping(decoder)
		}
		return yamlSequence(decoder)
	case string:
		return yamlString(token, false), nil
	case json.Number:
		return plainScalar(yamlNumber(string(token))), nil
	case bool:
		return plainScalar(strconv.FormatBool(token)), nil
	case nil:
		return plainScalar("null"), nil
	}

	return nil, fmt.Errorf("unexpected JSON token %v", token)
}

// yamlMapping reads the members of an object whose opening brace has been read.
//
// JSON allows a key to repeat and YAML does not: yaml.v3 refuses to read such
// a mapping at all. A repeated key keeps its first position and its last value,
// which is what JSON.parse and encoding/json make of it.
//
// An empty object, like an empty array, needs nothing here: yaml.v3 writes an
// empty mapping as {} and an empty sequence as [], the only forms they have,
// since a key with nothing under it reads as null.
func yamlMapping(decoder *json.Decoder) (*yaml.Node, error) {
	mapping := &yaml.Node{Kind: yaml.MappingNode}
	positions := map[string]int{}

	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key, ok := token.(string)
		if !ok {
			return nil, fmt.Errorf("unexpected object key %v", token)
		}
		value, err := yamlValue(decoder)
		if err != nil {
			return nil, err
		}

		if at, repeated := positions[key]; repeated {
			mapping.Content[at+1] = value
			continue
		}
		positions[key] = len(mapping.Content)
		mapping.Content = append(mapping.Content, yamlString(key, true), value)
	}

	// The closing brace.
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}

	return mapping, nil
}

// yamlSequence reads the items of an array whose opening bracket has been read.
func yamlSequence(decoder *json.Decoder) (*yaml.Node, error) {
	sequence := &yaml.Node{Kind: yaml.SequenceNode}

	for decoder.More() {
		item, err := yamlValue(decoder)
		if err != nil {
			return nil, err
		}
		sequence.Content = append(sequence.Content, item)
	}

	// The closing bracket.
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}

	return sequence, nil
}

// quoteKeptFinalBlock double-quotes the document's last scalar if it is a block
// ending in more than one line break. Such a block keeps them all (|+), so as
// the last node it would end the document with blank lines.
func quoteKeptFinalBlock(node *yaml.Node) {
	for len(node.Content) > 0 {
		node = node.Content[len(node.Content)-1]
	}
	if node.Style == yaml.LiteralStyle && strings.HasSuffix(node.Value, "\n\n") {
		node.Style = yaml.DoubleQuotedStyle
	}
}

func plainScalar(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Value: value}
}

// yamlString renders a JSON string: plain when no parser can misread it, as a
// literal block when it spans lines and a block reproduces it, and
// double-quoted otherwise. Double quotes can carry any string, because every
// character they cannot hold as itself goes in as an escape.
//
// A key never becomes a block. A key that spans lines needs yaml.v3's explicit
// "? " form either way, and there one quoted line reads better than a block.
func yamlString(value string, key bool) *yaml.Node {
	node := &yaml.Node{Kind: yaml.ScalarNode, Value: value, Style: yaml.DoubleQuotedStyle}
	switch {
	case canBePlain(value):
		node.Style = 0
	case !key && canBeLiteral(value):
		node.Style = yaml.LiteralStyle
	}

	return node
}

// yamlNumber spells a JSON number so that YAML 1.1 and 1.2 read the same number.
//
// An integer keeps its literal, however long, except that -0 becomes the float
// -0.0, since as an integer it would lose its sign. A float needs a dot in its
// mantissa and a sign in its exponent, or YAML 1.1 reads it as a string, so
// 1e+21 becomes 1.0e+21 and 1E5 becomes 1.0e+5. The digits never change, so
// neither does the value.
//
// YAML sets no limit on a number, but a reader may. yaml.v3 decodes an integer
// that fits neither int64 nor uint64 into a float64, as JavaScript does any
// integer beyond 2^53, while PyYAML and Psych keep every digit. yaml.v3 decodes
// a number beyond float64's range, integer or not, into a string, because its
// resolver rejects anything ParseFloat reports out of range; JavaScript reads
// infinity, and so does PyYAML for a float. encoding/json never writes such a
// number from a float64 or a fixed-size integer.
func yamlNumber(literal string) string {
	mantissa, exponent := literal, ""
	if at := strings.IndexAny(literal, "eE"); at >= 0 {
		mantissa, exponent = literal[:at], literal[at+1:]
	}
	fraction := strings.Contains(mantissa, ".")

	if !fraction && exponent == "" {
		if literal == "-0" {
			return "-0.0"
		}
		return literal
	}

	if !fraction {
		mantissa += ".0"
	}
	if exponent == "" {
		return mantissa
	}
	if exponent[0] != '+' && exponent[0] != '-' {
		exponent = "+" + exponent
	}

	return mantissa + "e" + exponent
}

// yamlIndicators are the characters that make a plain scalar start something
// else: a sequence entry, a key, a comment, an anchor, an alias, a tag, a flow
// collection, a block scalar, a quoted scalar or a directive; @ and ` are
// reserved. + is not an indicator, but like - it signs a number in every
// numeric syntax, and quoting both is simpler than matching each grammar.
const yamlIndicators = "-?:,[]{}#&*!|>'\"%@`+"

// yamlReservedWords are the plain scalars some resolver reads as null, a
// boolean, infinity, NaN, a merge key (<<) or YAML 1.1's value key (=).
//
// They are compared in lower case because Psych ignores case, so tRUe is true
// there and NuLL is null. y and n are booleans in the YAML 1.1 type repository
// and in yaml.v2.
//
// Quoting a << key stops every parser but Psych from merging it: Psych merges
// any << key that has no explicit !!str tag, and this encoder writes no tags.
// So in Ruby, a key << holding an object or an array is merged into the
// mapping around it. No key bb defines is <<.
var yamlReservedWords = map[string]bool{
	"~": true, "null": true,
	"true": true, "false": true, "yes": true, "no": true, "on": true, "off": true, "y": true, "n": true,
	".inf": true, ".nan": true,
	"<<": true, "=": true,
}

// yamlNumberOrTimestamp matches the plain scalars some YAML 1.1 or 1.2
// resolver reads as a number or a timestamp.
//
// It is the union of their grammars, widened only where that stays clear of
// ordinary text. YAML 1.1 allows _ between digits and base 60 (1:20 is 80).
// yaml.v3 drops every _ before it parses, so 1e_+5 is a float there, and takes
// a sign after 0b or 0o, so 0b+1 is 1. Psych takes commas as separators (1,000
// is 1000) and months of one digit, and js-yaml and ruamel still resolve
// timestamps under YAML 1.2. A leading sign never gets here, because + and -
// are quoted as indicators.
//
// A version such as 5.0.0 stays plain: no parser reads a second dot as part of
// a number, although the float pattern published for YAML 1.1 would.
var yamlNumberOrTimestamp = regexp.MustCompile(`^(?:` +
	`[0-9][0-9_,]*(?:\.[0-9_,]*)?(?:[eE][-+_]*[0-9_]*)?` + // 12, 012, 1_000, 1,000, 1.5, 1., 1e3, 1e_+3
	`|\.[0-9_,]*(?:[eE][-+_]*[0-9_]*)?` + // .5, .5e3, and Psych's .e+5, which it cannot parse
	`|[0-9][0-9_,]*(?::[0-9]{1,2})+(?:\.[0-9_,]*)?` + // base 60: 1:20, 1:20:30.5
	`|0_*(?:[xX][0-9a-fA-F_,]*|[oO][-+_]*[0-7_,]*|[bB][-+_]*[01_,]*)` + // 0x1F, 0o17, 0b101, 0b+1
	`|[0-9]{4}-[0-9]{1,2}-[0-9]{1,2}(?:[Tt ].*)?` + // 2001-12-14, 2001-12-14t21:59:43.10-05:00
	`)$`)

// canBePlain reports whether s can be written without quotes: no YAML 1.1 or
// 1.2 resolver reads the plain scalar as anything but s, and yaml.v3 writes it
// plain rather than choosing a style of its own.
func canBePlain(s string) bool {
	switch {
	case s == "", s[0] == ' ', s[len(s)-1] == ' ':
		// An empty plain scalar is null, and a parser strips the spaces
		// around one.
		return false
	case strings.IndexByte(yamlIndicators, s[0]) >= 0, strings.HasPrefix(s, "..."):
		// ... ends a document, as --- starts one.
		return false
	case strings.Contains(s, ": "), strings.Contains(s, " #"), s[len(s)-1] == ':':
		// A mapping value or a comment would begin there.
		return false
	case yamlReservedWords[strings.ToLower(s)], yamlNumberOrTimestamp.MatchString(s):
		return false
	}

	for _, r := range s {
		if !yamlUnescaped(r) {
			return false
		}
	}

	return true
}

// canBeLiteral reports whether s, which spans lines, reads back exactly from a
// literal block (|). yaml.v3 writes the chomping indicator itself: |- when there
// is no final line break, | for one and |+ for more.
//
// Some strings go in double quotes instead, because a block would not
// reproduce them:
//   - one starting with a line break: yaml.v3 drops that line break, so every
//     parser, yaml.v3 included, reads the string without it;
//   - one starting with a space, which needs an indentation indicator, and at
//     the top of a document js-yaml counts it from another column than PyYAML;
//   - one starting with a tab, which libyaml and yaml.v3 take for indentation,
//     so yaml.v3 rejects its own output;
//   - whitespace ending a line, which yaml.v3 will not put in a block, and which
//     would be invisible there and lost to the first editor that trims lines;
//   - a carriage return, which a parser reads as a line break, and any
//     character that cannot appear unescaped.
func canBeLiteral(s string) bool {
	if !strings.Contains(s, "\n") || strings.IndexByte(" \t\n", s[0]) >= 0 {
		return false
	}

	previous := rune(0)
	for _, r := range s {
		switch {
		case r == '\n':
			if previous == ' ' || previous == '\t' {
				return false
			}
		case r != '\t' && !yamlUnescaped(r):
			return false
		}
		previous = r
	}

	return previous != ' ' && previous != '\t'
}

// yamlUnescaped reports whether r can appear as itself in a plain or block
// scalar, on one line.
//
// It is what yaml.v3 counts as printable: it escapes every other character,
// which only double quotes allow, so a string holding an emoji -- outside the
// Basic Multilingual Plane -- comes out double-quoted with a \U escape. The
// line and paragraph separators are printable to yaml.v3 but line breaks to a
// YAML 1.1 parser and not to a 1.2 one, so only an escape reads the same in
// both.
func yamlUnescaped(r rune) bool {
	switch {
	case r == 0x2028, r == 0x2029:
		return false
	case r >= 0x20 && r <= 0x7e, r >= 0xa0 && r <= 0xd7ff:
		return true
	default:
		return r >= 0xe000 && r <= 0xfffd && r != 0xfeff
	}
}
