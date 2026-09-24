package jsonoutput

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// yamlHazards are strings that some YAML parser would not read back as
// themselves if they were written plain, so they must be quoted as a key and as
// a value.
var yamlHazards = []string{
	// Empty, null and the booleans, in the letter cases Psych also accepts.
	"", "~", "null", "Null", "NULL", "nuLL",
	"true", "True", "TRUE", "tRUe", "false", "False", "FALSE",
	"yes", "Yes", "YES", "no", "No", "NO", "on", "On", "ON", "off", "Off", "OFF", "y", "Y", "n", "N",
	// Numbers, as YAML 1.1, 1.2, yaml.v3 and Psych read them.
	"0", "123", "012", "0x1F", "0X1F", "0o17", "0b101", "1_000", "1,000", "0_x1F", "1:20", "12:30:45", "1:20.5",
	"+1", "-1", ".5", "1.", "1.5", "1e3", "1E3", "1.0e+3", ".e+5",
	".inf", ".Inf", "-.inf", "-.Inf", "+.INF", ".nan", ".NaN", ".NAN", ".iNf",
	// yaml.v3 drops every _ before it parses, and takes a sign after 0b or 0o.
	"1_e5", "1e_+5", "1.5_e_+_5", "0b+0", "0b_+1", "0o-7",
	// Timestamps.
	"2001-12-14", "2001-1-2", "2001-12-14t21:59:43.10-05:00", "2001-12-14 21:59:43.10 -5", "2001-12-14T21:59:43Z",
	// The merge and value keys.
	"<<", "=",
	// Whitespace a parser would strip, and tabs.
	" leading", "trailing ", " ", "\t", "\ttab", "tab\t", "tab\there",
	// An indicator at the start.
	"-", "- item", "-dash", "--flag", "?", "? key", "?x", ":", ":symbol", ",", "[", "]", "{", "}", "#", "#comment",
	"&anchor", "*alias", "!tag", "|", ">", "'", "\"", "%YAML 1.2", "@", "`",
	// A mapping value or a comment, anywhere.
	"key: value", "text #comment", "ends with:",
	// Document markers.
	"---", "...", "...more",
	// Control, non-printable and BOM characters, and the line breaks only
	// YAML 1.1 knows.
	"nul\x00", "bell\a", "esc\x1b", "del\x7f", "c1\xc2\x80", "nel\xc2\x85", "a\r\nb", "cr\r",
	"line\xe2\x80\xa8separator", "paragraph\xe2\x80\xa9separator", "\xef\xbb\xbfbom", "bom\xef\xbb\xbf", "nonchar\xef\xbf\xbe",
	// Outside the Basic Multilingual Plane, which yaml.v3 only writes escaped.
	"fix \xf0\x9f\x90\x9b",
}

// yamlPlainStrings are ordinary strings that must stay unquoted, since
// readability is the point of --yaml. Several are close to a hazard without
// being one.
var yamlPlainStrings = []string{
	"PROJ/app", "feature/x", "Hello world.", "pr merge", "refs/heads/main",
	"5.0.0", "10.0.0.1", "1.2.3-rc1", "2001-12-14-release", "12:345",
	"4c4e3bcf2b0e", "0bad1dea", "0e5a", "1st", "10MB", "404 Not Found",
	"C#", "a:b", "http://example.com/x?y=1#z", "~/path", "a, b", "a[0]", "100%", "x{y}", "it's", `say "hi"`, `C:\path`,
	"yes-please", "null_value", "Norway", "offline", "no way", "True story", "Infinity", "NaN", "inf",
	"<", "<<x", "=x", "~x", "_1",
	"caf\xc3\xa9", "\xe6\x97\xa5\xe6\x9c\xac\xe8\xaa\x9e", "no\xc2\xa0break",
}

func TestEncodeYAMLQuotesStringsAParserWouldMisread(t *testing.T) {
	t.Parallel()

	for _, hazard := range yamlHazards {
		t.Run(strconv.Quote(hazard), func(t *testing.T) {
			t.Parallel()

			document := mustMarshalJSON(t, map[string]string{hazard: hazard})
			encoded := mustEncodeYAML(t, document)

			key, value := onlyPair(t, encoded)
			if key.Style != yaml.DoubleQuotedStyle || value.Style != yaml.DoubleQuotedStyle {
				t.Errorf("%q is not double-quoted as both key and value:\n%s", hazard, encoded)
			}
			assertYAMLMeans(t, document, encoded)
		})
	}
}

func TestEncodeYAMLLeavesOrdinaryStringsPlain(t *testing.T) {
	t.Parallel()

	for _, plain := range yamlPlainStrings {
		t.Run(strconv.Quote(plain), func(t *testing.T) {
			t.Parallel()

			document := mustMarshalJSON(t, map[string]string{plain: plain})
			encoded := mustEncodeYAML(t, document)

			key, value := onlyPair(t, encoded)
			if key.Style != 0 || value.Style != 0 {
				t.Errorf("%q is quoted, though no parser would misread it:\n%s", plain, encoded)
			}
			assertYAMLMeans(t, document, encoded)
		})
	}
}

func TestEncodeYAMLWritesMultilineStringsAsLiteralBlocks(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		json string
		yaml string
	}{
		{"no final line break", `{"v":"line one\nline two"}`, "v: |-\n  line one\n  line two\n"},
		{"one final line break", `{"v":"line one\nline two\n"}`, "v: |\n  line one\n  line two\n"},
		{"several final line breaks", `{"v":"line one\n\n","w":1}`, "v: |+\n  line one\n\nw: 1\n"},
		{"blank and indented lines", `{"v":"a\n\n  b"}`, "v: |-\n  a\n\n    b\n"},
		{"a tab after the first line", `{"v":"a\n\tb"}`, "v: |-\n  a\n  \tb\n"},
		{"lines that look like syntax", `{"v":"a\n# b\n--- c\n- d: e"}`, "v: |-\n  a\n  # b\n  --- c\n  - d: e\n"},
		{"in a sequence", `{"v":["a\nb\n",{"k":"c\nd"}]}`, "v:\n  - |\n    a\n    b\n  - k: |-\n      c\n      d\n"},
		{"the whole document", `"a\nb\n"`, "|\n  a\n  b\n"},

		// Double quotes wherever a block would not read back the same.
		{"blank lines ending the document", `{"v":"line one\n\n"}`, "v: \"line one\\n\\n\"\n"},
		{"a leading line break", `{"v":"\na"}`, "v: \"\\na\"\n"},
		{"a leading space", `{"v":" a\nb"}`, "v: \" a\\nb\"\n"},
		{"a leading tab", `{"v":"\ta\nb"}`, "v: \"\\ta\\nb\"\n"},
		{"a space ending a line", `{"v":"a \nb"}`, "v: \"a \\nb\"\n"},
		{"a tab ending a line", `{"v":"a\t\nb"}`, "v: \"a\\t\\nb\"\n"},
		{"a space ending the string", `{"v":"a\nb "}`, "v: \"a\\nb \"\n"},
		{"CRLF line breaks", `{"v":"a\r\nb"}`, "v: \"a\\r\\nb\"\n"},
		{"a control character", `{"v":"a\u0007\nb"}`, "v: \"a\\a\\nb\"\n"},
		{"a line separator", `{"v":"a\u2028b\nc"}`, "v: \"a\\Lb\\nc\"\n"},
		{"an emoji", `{"v":"a\ud83d\udc1b\nb"}`, "v: \"a\\U0001F41B\\nb\"\n"},
		{"a key", `{"a\nb":1}`, "? \"a\\nb\"\n: 1\n"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			encoded := mustEncodeYAML(t, []byte(c.json))
			if string(encoded) != c.yaml {
				t.Errorf("encoding %s\ngot:\n%s\nwant:\n%s", c.json, encoded, c.yaml)
			}
			assertYAMLMeans(t, []byte(c.json), encoded)
		})
	}
}

func TestEncodeYAMLWritesNumbersBothVersionsReadAlike(t *testing.T) {
	t.Parallel()

	cases := []struct {
		json string
		yaml string
	}{
		{"0", "0"},
		{"-7", "-7"},
		{"9007199254740993", "9007199254740993"},
		// Past int64 and uint64 the literal is kept whole: yaml.v3 decodes it
		// into a float64, and PyYAML and Psych keep every digit.
		{"123456789012345678901234567890", "123456789012345678901234567890"},
		{"-123456789012345678901234567890", "-123456789012345678901234567890"},
		{"1.5", "1.5"},
		{"-2.25", "-2.25"},
		{"0.1", "0.1"},
		// YAML 1.1 needs a dot in the mantissa and a sign in the exponent.
		{"1e+21", "1.0e+21"},
		{"1E5", "1.0e+5"},
		{"1e-7", "1.0e-7"},
		{"2.5E-3", "2.5e-3"},
		{"1.5e10", "1.5e+10"},
		{"0e0", "0.0e+0"},
		// Negative zero stays a float, which keeps its sign.
		{"-0", "-0.0"},
		{"-0.0", "-0.0"},
		{"-0e0", "-0.0e+0"},
		// Past float64's range, integer or not, the literal is kept as well, and
		// yaml.v3 decodes it as a string.
		{"1e400", "1.0e+400"},
		{"2" + strings.Repeat("0", 309), "2" + strings.Repeat("0", 309)},
	}

	for _, c := range cases {
		t.Run(c.json, func(t *testing.T) {
			t.Parallel()

			encoded := mustEncodeYAML(t, []byte(c.json))
			if string(encoded) != c.yaml+"\n" {
				t.Errorf("%s is written %q, want %q", c.json, encoded, c.yaml+"\n")
			}
			assertYAMLMeans(t, []byte(c.json), encoded)
		})
	}
}

func TestEncodeYAMLKeepsStructureAndKeyOrder(t *testing.T) {
	t.Parallel()

	document := `{"zebra":1,"apple":{"nested":{"deeper":[1,"two",null,true,false]}},"empty":{},"none":[],` +
		`"lists":[[],{},[1,[2]],[{"a":1,"b":[{"c":"d"}]}]],"last":"end"}`
	want := `zebra: 1
apple:
  nested:
    deeper:
      - 1
      - two
      - null
      - true
      - false
empty: {}
none: []
lists:
  - []
  - {}
  - - 1
    - - 2
  - - a: 1
      b:
        - c: d
last: end
`

	encoded := mustEncodeYAML(t, []byte(document))
	if string(encoded) != want {
		t.Errorf("got:\n%s\nwant:\n%s", encoded, want)
	}
	assertYAMLMeans(t, []byte(document), encoded)
}

func TestEncodeYAMLWritesUnicodeAndControlCharacters(t *testing.T) {
	t.Parallel()

	cases := []struct {
		json string
		yaml string
	}{
		{`"caf\u00e9 \u65e5\u672c"`, "caf\xc3\xa9 \xe6\x97\xa5\xe6\x9c\xac"},
		{`"no\u00a0break"`, "no\xc2\xa0break"},
		{`"fix \ud83d\udc1b"`, `"fix \U0001F41B"`},
		{`"a\u0000b"`, `"a\0b"`},
		{`"a\tb"`, `"a\tb"`},
		{`"a\r\nb"`, `"a\r\nb"`},
		{`"esc\u001b"`, `"esc\e"`},
		{`"del\u007f"`, `"del\x7F"`},
		{`"nel\u0085"`, `"nel\N"`},
		{`"ls\u2028"`, `"ls\L"`},
		{`"bom\uFEFF"`, `"bom\uFEFF"`},
		// yaml.v3 escapes every character of a string that starts with a byte
		// order mark: its check for one looks at the start of the string, not at
		// the character being written. It reads back all the same.
		{`"\uFEFFbom"`, `"\uFEFF\x62\x6F\x6D"`},
		{`"\uFFFE"`, `"\uFFFE"`},
	}

	for _, c := range cases {
		t.Run(c.json, func(t *testing.T) {
			t.Parallel()

			document := []byte(`{"v":` + c.json + `}`)
			encoded := mustEncodeYAML(t, document)
			if want := "v: " + c.yaml + "\n"; string(encoded) != want {
				t.Errorf("%s is written %q, want %q", c.json, encoded, want)
			}
			assertYAMLMeans(t, document, encoded)
		})
	}
}

// TestEncodeYAMLRendersTheEnvelopes is what --yaml prints for a list and for a
// failure: the documents --json prints, laid out as YAML.
func TestEncodeYAMLRendersTheEnvelopes(t *testing.T) {
	t.Parallel()

	type ref struct {
		ID           string `json:"id"`
		DisplayID    string `json:"displayId"`
		LatestCommit string `json:"latestCommit"`
	}
	type pullRequest struct {
		ID          int      `json:"id"`
		Title       string   `json:"title"`
		Description string   `json:"description,omitempty"`
		State       string   `json:"state"`
		Draft       bool     `json:"draft"`
		FromRef     ref      `json:"fromRef"`
		Reviewers   []string `json:"reviewers"`
		UpdatedDate int64    `json:"updatedDate"`
	}

	limitReached := true
	list, err := marshalEnvelope(Envelope{
		Data: map[string]any{"pullRequests": []pullRequest{
			{
				ID:          101,
				Title:       "Fix the thing",
				Description: "Fixes #12.\n\nSee the release notes.\n",
				State:       "OPEN",
				FromRef:     ref{ID: "refs/heads/feature/x", DisplayID: "feature/x", LatestCommit: "0e5a1b2c3d4e"},
				Reviewers:   []string{"alice", "no"},
				UpdatedDate: 1727170000000,
			},
			{
				ID:          102,
				Title:       "2.0",
				State:       "MERGED",
				Draft:       true,
				FromRef:     ref{ID: "refs/heads/release/2.0", DisplayID: "release/2.0", LatestCommit: "12e4567"},
				Reviewers:   []string{},
				UpdatedDate: 1727180000000,
			},
		}},
		Meta: EnvelopeMeta{LimitReached: &limitReached, BBVersion: "5.0.0"},
	})
	if err != nil {
		t.Fatalf("encoding the list envelope: %v", err)
	}

	failure, err := marshalEnvelope(ErrorEnvelope{
		Error: EnvelopeError{
			Kind:     "not_found",
			Message:  "repository PROJ/app not found",
			ExitCode: 4,
			Details: map[string]string{
				"upstreamStatus":    "404",
				"upstreamException": "com.atlassian.bitbucket.repository.NoSuchRepositoryException",
			},
		},
		Meta: EnvelopeMeta{BBVersion: "5.0.0"},
	})
	if err != nil {
		t.Fatalf("encoding the error envelope: %v", err)
	}

	cases := []struct {
		name string
		json []byte
		yaml string
	}{
		{"list", list, `data:
  pullRequests:
    - id: 101
      title: Fix the thing
      description: |
        Fixes #12.

        See the release notes.
      state: OPEN
      draft: false
      fromRef:
        id: refs/heads/feature/x
        displayId: feature/x
        latestCommit: 0e5a1b2c3d4e
      reviewers:
        - alice
        - "no"
      updatedDate: 1727170000000
    - id: 102
      title: "2.0"
      state: MERGED
      draft: true
      fromRef:
        id: refs/heads/release/2.0
        displayId: release/2.0
        latestCommit: "12e4567"
      reviewers: []
      updatedDate: 1727180000000
meta:
  limitReached: true
  bbVersion: 5.0.0
`},
		{"failure", failure, `error:
  kind: not_found
  message: repository PROJ/app not found
  exitCode: 4
  details:
    upstreamException: com.atlassian.bitbucket.repository.NoSuchRepositoryException
    upstreamStatus: "404"
meta:
  bbVersion: 5.0.0
`},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			encoded := mustEncodeYAML(t, c.json)
			if string(encoded) != c.yaml {
				t.Errorf("got:\n%s\nwant:\n%s", encoded, c.yaml)
			}
			assertYAMLMeans(t, c.json, encoded)
		})
	}
}

// TestEncodeYAMLKeepsTheLastValueOfARepeatedKey: YAML forbids a repeated key,
// so the YAML carries the value JSON.parse and encoding/json read, at the
// position where the key first appeared.
func TestEncodeYAMLKeepsTheLastValueOfARepeatedKey(t *testing.T) {
	t.Parallel()

	document := []byte(`{"a":1,"b":2,"a":{"c":3}}`)
	encoded := mustEncodeYAML(t, document)
	if want := "a:\n  c: 3\nb: 2\n"; string(encoded) != want {
		t.Errorf("got:\n%s\nwant:\n%s", encoded, want)
	}
	assertYAMLMeans(t, document, encoded)
}

func TestEncodeYAMLReadsDeepNesting(t *testing.T) {
	t.Parallel()

	document := `"bottom"`
	for depth := 0; depth < 500; depth++ {
		if depth%2 == 0 {
			document = `[` + document + `]`
		} else {
			document = `{"k":` + document + `}`
		}
	}

	assertYAMLMeans(t, []byte(document), mustEncodeYAML(t, []byte(document)))
}

func TestEncodeYAMLRejectsAnythingButOneJSONValue(t *testing.T) {
	t.Parallel()

	for _, document := range []string{"", "  ", "{", `{"a":1`, "[1,]", `{"a" 1}`, `{1:2}`, "{} {}", "1 2", "nul", `"open`, "[1]]"} {
		if encoded, err := encodeYAML([]byte(document)); err == nil {
			t.Errorf("encodeYAML(%q) = %q, want an error", document, encoded)
		}
	}
}

// TestYAMLResolversMatchTheirSpecifications keeps the resolvers these tests
// judge the encoder by honest: a resolver that called everything a string
// would pass every plain scalar.
func TestYAMLResolversMatchTheirSpecifications(t *testing.T) {
	t.Parallel()

	cases := []struct{ plain, yaml11, yaml12 string }{
		{"true", "bool", "bool"}, {"yes", "bool", "str"}, {"Off", "bool", "str"}, {"y", "bool", "str"}, {"tRUe", "bool", "str"},
		{"null", "null", "null"}, {"~", "null", "null"}, {"NuLL", "null", "str"},
		{"12", "int", "int"}, {"+1", "int", "int"}, {"012", "int", "int"}, {"0x1F", "int", "int"}, {"0o17", "str", "int"},
		{"0b101", "int", "str"}, {"1_000", "int", "str"}, {"1,000", "int", "str"}, {"1:20", "int", "str"},
		{"1.5", "float", "float"}, {"1.0e+3", "float", "float"}, {"1e3", "str", "float"}, {".5", "float", "float"},
		{"1:20.5", "float", "str"}, {".inf", "float", "float"}, {"-.Inf", "float", "float"}, {".NaN", "float", "float"}, {".iNf", "float", "str"},
		{"2001-12-14", "timestamp", "str"}, {"2001-1-2", "timestamp", "str"}, {"2001-12-14t21:59:43.10-05:00", "timestamp", "str"},
		{"<<", "merge", "str"}, {"=", "value", "str"},
		{"5.0.0", "str", "str"}, {"10.0.0.1", "str", "str"}, {"PROJ/app", "str", "str"}, {"0bad1dea", "str", "str"},
	}

	for _, c := range cases {
		if got := resolveYAMLTag(yaml11Resolvers, c.plain); got != c.yaml11 {
			t.Errorf("YAML 1.1 resolves %q as %s, want %s", c.plain, got, c.yaml11)
		}
		if got := resolveYAMLTag(yaml12CoreResolvers, c.plain); got != c.yaml12 {
			t.Errorf("YAML 1.2 resolves %q as %s, want %s", c.plain, got, c.yaml12)
		}
	}
}

// yamlFuzzSeeds start the fuzzer from real envelopes and from every class of
// value the encoder treats specially.
var yamlFuzzSeeds = []string{
	`{"data":{"pullRequests":[{"id":1,"title":"Fix the thing","fromRef":{"id":"refs/heads/feature/x","latestCommit":"0e5a1b2c"},` +
		`"description":"Line one\n\nLine two\n","reviewers":[]}]},"meta":{"limitReached":true,"bbVersion":"5.0.0"}}`,
	`{"error":{"kind":"not_found","message":"repository PROJ/app not found","exitCode":4,"details":{"upstreamStatus":"404"}},"meta":{"bbVersion":"dev"}}`,
	`["","~","null","NULL","yes","No","on","Y","n","012","0x1F","0o17","0b101","1_000","1,000","1:20","+1","-1",".5","1.","1e3",` +
		`".inf","-.Inf",".nan","2001-12-14","2001-12-14t21:59:43.10-05:00","<<","="," x","x ","\t","- x","? x",": x","a: b","a #b",` +
		`"a:","#","&a","*a","!a","|",">","'","\"","%","@","` + "`" + `","[","]","{","}",",","---","...","5.0.0","PROJ/app"]`,
	`{"":1,"~":2,"yes":3,"012":4,"1:20":5,"<<":{"a":1},"=":6," x":7,"a: b":8,"2001-12-14":9,"a\nb":10}`,
	`[0,-0,1.5,-0.0,1e+21,1E5,2.5E-3,123456789012345678901234567890,-9223372036854775809,18446744073709551615,1e400,1e-400,0.1]`,
	`{"a":"x\ny","b":"x\ny\n","c":"x\ny\n\n","d":" x\ny","e":"x \ny","f":"x\r\ny","g":"\tx\ny","h":"x\n\ty","i":"\nx","j":"last\n\n"}`,
	`{"nul":"a\u0000b","tab":"a\tb","del":"\u007f","nel":"\u0085","ls":"a\u2028b","bom":"\ufeffx","nbsp":"\u00a0","emoji":"\ud83d\udc1b"}`,
	`{"z":1,"a":{"y":[],"x":{}},"m":[[],[{}],[[1]]],"a":2}`,
	`"yes"`, `null`, `-0`, `"a\n\n"`, `[]`, `{}`,
}

// FuzzEncodeYAMLFaithful: whatever valid JSON goes in, the YAML that comes out
// means the same, to yaml.v3 and to the YAML 1.1 and 1.2 resolvers.
func FuzzEncodeYAMLFaithful(f *testing.F) {
	for _, seed := range yamlFuzzSeeds {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, document []byte) {
		if !json.Valid(document) {
			return
		}
		tree, err := parseJSONTree(document)
		if err != nil {
			t.Fatalf("reading valid JSON %q: %v", document, err)
		}
		// Nesting changes nothing the encoder decides, and each level indents
		// the next, so a deep document only costs the fuzzer quadratic time.
		// TestEncodeYAMLReadsDeepNesting covers depth.
		if tree.depth() > 200 {
			return
		}

		encoded, err := encodeYAML(document)
		if err != nil {
			t.Fatalf("encodeYAML rejected valid JSON %q: %v", document, err)
		}
		assertYAMLMeans(t, document, encoded)
	})
}

func mustEncodeYAML(t *testing.T, document []byte) []byte {
	t.Helper()

	encoded, err := encodeYAML(document)
	if err != nil {
		t.Fatalf("encodeYAML(%s): %v", document, err)
	}

	return encoded
}

func mustMarshalJSON(t *testing.T, value any) []byte {
	t.Helper()

	document, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshalling %v: %v", value, err)
	}

	return document
}

// onlyPair returns the key and value of a document that is a one-key mapping.
func onlyPair(t *testing.T, encoded []byte) (*yaml.Node, *yaml.Node) {
	t.Helper()

	var document yaml.Node
	if err := yaml.Unmarshal(encoded, &document); err != nil {
		t.Fatalf("yaml.v3 cannot read %q: %v", encoded, err)
	}
	mapping := document.Content[0]
	if mapping.Kind != yaml.MappingNode || len(mapping.Content) != 2 {
		t.Fatalf("want a mapping with one key, got %q", encoded)
	}

	return mapping.Content[0], mapping.Content[1]
}

// assertYAMLMeans checks that yamlDocument means jsonDocument.
//
// It reads the YAML twice. As yaml.v3 nodes, it checks key order and every
// scalar's tag, runs each plain scalar past the YAML 1.1 and 1.2 core
// resolvers, and checks that yaml.v3 kept the style encodeYAML chose. As the Go
// values yaml.v3 decodes into, it compares with what encoding/json decodes.
func assertYAMLMeans(t *testing.T, jsonDocument, yamlDocument []byte) {
	t.Helper()

	if !bytes.HasSuffix(yamlDocument, []byte("\n")) || bytes.HasSuffix(yamlDocument, []byte("\n\n")) {
		t.Errorf("the document does not end with exactly one line break: %q", yamlDocument)
	}
	if bytes.HasPrefix(yamlDocument, []byte("---")) {
		t.Errorf("the document starts with a document marker: %q", yamlDocument)
	}

	tree, err := parseJSONTree(jsonDocument)
	if err != nil {
		t.Fatalf("reading the JSON document %q: %v", jsonDocument, err)
	}

	var document yaml.Node
	if err := yaml.Unmarshal(yamlDocument, &document); err != nil {
		t.Fatalf("yaml.v3 cannot read the document: %v\n%s", err, yamlDocument)
	}
	if document.Kind != yaml.DocumentNode || len(document.Content) != 1 {
		t.Fatalf("want one document, got %q", yamlDocument)
	}
	root := document.Content[0]
	compareYAMLNode(t, "$", tree, root, lastScalar(root))

	var decoded, expected any
	if err := yaml.Unmarshal(yamlDocument, &decoded); err != nil {
		t.Fatalf("yaml.v3 cannot decode the document: %v\n%s", err, yamlDocument)
	}
	decoder := json.NewDecoder(bytes.NewReader(jsonDocument))
	decoder.UseNumber()
	if err := decoder.Decode(&expected); err != nil {
		t.Fatalf("decoding the JSON document %q: %v", jsonDocument, err)
	}
	if problem := compareDecoded("$", expected, decoded); problem != "" {
		t.Errorf("yaml.v3 decodes another value: %s\njson: %s\nyaml:\n%s", problem, jsonDocument, yamlDocument)
	}
}

// compareYAMLNode checks node against the JSON value it was written for.
func compareYAMLNode(t *testing.T, path string, want *jsonTree, node, last *yaml.Node) {
	t.Helper()

	if node.Anchor != "" || node.Kind == yaml.AliasNode || node.Style&yaml.TaggedStyle != 0 {
		t.Errorf("%s: the document carries an anchor, an alias or a tag: %s", path, describeYAMLNode(node))
		return
	}

	switch want.kind {
	case "object":
		if node.Kind != yaml.MappingNode || len(node.Content) != 2*len(want.keys) {
			t.Errorf("%s: want a mapping of %d keys, got %s", path, len(want.keys), describeYAMLNode(node))
			return
		}
		for i, key := range want.keys {
			keyPath := path + "." + strconv.Quote(key)
			compareYAMLScalar(t, keyPath+" (key)", "string", key, node.Content[2*i], true, false)
			compareYAMLNode(t, keyPath, want.members[key], node.Content[2*i+1], last)
		}
	case "array":
		if node.Kind != yaml.SequenceNode || len(node.Content) != len(want.items) {
			t.Errorf("%s: want a sequence of %d items, got %s", path, len(want.items), describeYAMLNode(node))
			return
		}
		for i, item := range want.items {
			compareYAMLNode(t, fmt.Sprintf("%s[%d]", path, i), item, node.Content[i], last)
		}
	default:
		compareYAMLScalar(t, path, want.kind, want.text, node, false, node == last)
	}
}

// compareYAMLScalar checks one scalar against the JSON string, number, boolean
// or null it was written for.
//
// yaml.v3 has already resolved the tag. A plain scalar is also run past the
// YAML 1.1 and 1.2 core resolvers, because yaml.v3 follows neither exactly: it
// reads 1_000 as a number, as YAML 1.1 does, but not yes or 1:20.
func compareYAMLScalar(t *testing.T, path, kind, text string, node *yaml.Node, key, last bool) {
	t.Helper()

	if node.Kind != yaml.ScalarNode {
		t.Errorf("%s: want a scalar, got %s", path, describeYAMLNode(node))
		return
	}
	plain := node.Style == 0
	yaml11 := resolveYAMLTag(yaml11Resolvers, node.Value)
	yaml12 := resolveYAMLTag(yaml12CoreResolvers, node.Value)

	switch kind {
	case "string":
		if node.Tag != "!!str" || node.Value != text {
			t.Errorf("%s: want the string %q, got %s", path, text, describeYAMLNode(node))
		}
		if plain && (yaml11 != "str" || yaml12 != "str") {
			t.Errorf("%s: the plain %q is a %s in YAML 1.1 and a %s in YAML 1.2", path, node.Value, yaml11, yaml12)
		}
		assertYAMLStringStyle(t, path, node, key, last)
	case "number":
		want := "int"
		if strings.ContainsAny(text, ".eE") || text == "-0" {
			want = "float"
		}
		if !plain || yaml11 != want || yaml12 != want {
			t.Errorf("%s: the %s %s is written %s, which YAML 1.1 reads as %s and YAML 1.2 as %s",
				path, want, text, describeYAMLNode(node), yaml11, yaml12)
		}
		if !sameDecimal(node.Value, text) {
			t.Errorf("%s: the number %s is written %q, another number", path, text, node.Value)
		}
	default:
		if !plain || node.Value != text || node.Tag != "!!"+kind || yaml11 != kind || yaml12 != kind {
			t.Errorf("%s: want the %s %s, got %s, which YAML 1.1 reads as %s and YAML 1.2 as %s",
				path, kind, text, describeYAMLNode(node), yaml11, yaml12)
		}
	}
}

// assertYAMLStringStyle checks that yaml.v3 wrote a string in the style
// encodeYAML chose. The emitter substitutes a style of its own for one it will
// not write, which keeps the meaning but hides a disagreement: single quotes
// where a string was meant to be plain, double quotes where a block was.
func assertYAMLStringStyle(t *testing.T, path string, node *yaml.Node, key, last bool) {
	t.Helper()

	want := yamlString(node.Value, key).Style
	if last && want == yaml.LiteralStyle && strings.HasSuffix(node.Value, "\n\n") {
		want = yaml.DoubleQuotedStyle
	}
	if node.Style != want {
		t.Errorf("%s: %q was chosen style %d and written in style %d", path, node.Value, want, node.Style)
	}
}

// lastScalar is the node quoteKeptFinalBlock looks at.
func lastScalar(node *yaml.Node) *yaml.Node {
	for len(node.Content) > 0 {
		node = node.Content[len(node.Content)-1]
	}

	return node
}

func describeYAMLNode(node *yaml.Node) string {
	return fmt.Sprintf("kind %d, tag %s, style %d, value %q", node.Kind, node.Tag, node.Style, node.Value)
}

// compareDecoded compares what encoding/json and yaml.v3 decode the two
// documents into, and describes the first difference.
func compareDecoded(path string, want, got any) string {
	switch want := want.(type) {
	case map[string]any:
		members, ok := got.(map[string]any)
		if !ok || len(members) != len(want) {
			return fmt.Sprintf("%s: want an object of %d keys, got %#v", path, len(want), got)
		}
		for key, value := range want {
			member, present := members[key]
			if !present {
				return fmt.Sprintf("%s: key %q is missing", path, key)
			}
			if problem := compareDecoded(path+"."+strconv.Quote(key), value, member); problem != "" {
				return problem
			}
		}
	case []any:
		items, ok := got.([]any)
		if !ok || len(items) != len(want) {
			return fmt.Sprintf("%s: want an array of %d items, got %#v", path, len(want), got)
		}
		for i, item := range want {
			if problem := compareDecoded(fmt.Sprintf("%s[%d]", path, i), item, items[i]); problem != "" {
				return problem
			}
		}
	case json.Number:
		if !sameDecodedNumber(want, got) {
			return fmt.Sprintf("%s: want the number %s, got %T %v", path, want, got, got)
		}
	case string:
		if value, ok := got.(string); !ok || value != want {
			return fmt.Sprintf("%s: want the string %q, got %#v", path, want, got)
		}
	case bool:
		if value, ok := got.(bool); !ok || value != want {
			return fmt.Sprintf("%s: want %v, got %#v", path, want, got)
		}
	case nil:
		if got != nil {
			return fmt.Sprintf("%s: want null, got %#v", path, got)
		}
	default:
		return fmt.Sprintf("%s: unexpected JSON value %#v", path, want)
	}

	return ""
}

// sameDecodedNumber compares a JSON number with what yaml.v3 decoded from the
// YAML written for it, by value. yaml.v3 decodes an integer that fits neither
// int64 nor uint64 into a float64, and any number beyond float64's range into a
// string (see yamlNumber), so those compare as what yaml.v3 can hold.
func sameDecodedNumber(want json.Number, got any) bool {
	literal := string(want)
	integer := !strings.ContainsAny(literal, ".eE") && literal != "-0"
	_, intErr := strconv.ParseInt(literal, 10, 64)
	_, uintErr := strconv.ParseUint(literal, 10, 64)
	fitsInteger := integer && (intErr == nil || uintErr == nil)

	switch got := got.(type) {
	case int:
		return fitsInteger && literal == strconv.Itoa(got)
	case int64:
		return fitsInteger && literal == strconv.FormatInt(got, 10)
	case uint64:
		return fitsInteger && literal == strconv.FormatUint(got, 10)
	case float64:
		value, err := strconv.ParseFloat(literal, 64)
		return !fitsInteger && err == nil && value == got && math.Signbit(value) == math.Signbit(got)
	case string:
		value, err := strconv.ParseFloat(literal, 64)
		return errors.Is(err, strconv.ErrRange) && math.IsInf(value, 0) && sameDecimal(got, literal)
	}

	return false
}

// sameDecimal reports whether two decimal literals spell the same number, sign
// of zero included, however large their exponents.
func sameDecimal(a, b string) bool {
	aNegative, aDigits, aExponent := canonicalDecimal(a)
	bNegative, bDigits, bExponent := canonicalDecimal(b)

	return aNegative == bNegative && aDigits == bDigits && aExponent.Cmp(bExponent) == 0
}

// canonicalDecimal takes a decimal literal apart into its sign, its significant
// digits and the power of ten of the last of them, so that 15, 15.0 and 1.50e1
// come out alike. Zero has no digits.
func canonicalDecimal(literal string) (bool, string, *big.Int) {
	negative := strings.HasPrefix(literal, "-")
	unsigned := strings.TrimLeft(literal, "+-")

	mantissa, exponentText := unsigned, "0"
	if at := strings.IndexAny(unsigned, "eE"); at >= 0 {
		mantissa, exponentText = unsigned[:at], unsigned[at+1:]
	}
	exponent, ok := new(big.Int).SetString(exponentText, 10)
	if !ok {
		return negative, "not a number: " + literal, new(big.Int)
	}

	whole, fraction, _ := strings.Cut(mantissa, ".")
	digits := strings.TrimLeft(whole+fraction, "0")
	significant := strings.TrimRight(digits, "0")
	if significant == "" {
		return negative, "", new(big.Int)
	}
	exponent.Sub(exponent, big.NewInt(int64(len(fraction))))
	exponent.Add(exponent, big.NewInt(int64(len(digits)-len(significant))))

	return negative, significant, exponent
}

// jsonTree is a JSON document as these tests read it, independently of the
// encoder: token by token, so that object keys keep their order, and with a
// repeated key keeping its first position and its last value.
type jsonTree struct {
	kind    string // object, array, string, number, bool or null
	text    string // the string, the number's literal, true, false or null
	keys    []string
	members map[string]*jsonTree
	items   []*jsonTree
}

func parseJSONTree(document []byte) (*jsonTree, error) {
	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.UseNumber()

	return readJSONTree(decoder)
}

func readJSONTree(decoder *json.Decoder) (*jsonTree, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}

	switch token := token.(type) {
	case json.Delim:
		tree := &jsonTree{kind: "array"}
		if token == '{' {
			tree.kind, tree.members = "object", map[string]*jsonTree{}
		}
		for decoder.More() {
			var key string
			if tree.kind == "object" {
				keyToken, err := decoder.Token()
				if err != nil {
					return nil, err
				}
				var ok bool
				if key, ok = keyToken.(string); !ok {
					return nil, fmt.Errorf("object key %v", keyToken)
				}
			}
			child, err := readJSONTree(decoder)
			if err != nil {
				return nil, err
			}
			if tree.kind == "array" {
				tree.items = append(tree.items, child)
				continue
			}
			if _, repeated := tree.members[key]; !repeated {
				tree.keys = append(tree.keys, key)
			}
			tree.members[key] = child
		}
		if _, err := decoder.Token(); err != nil {
			return nil, err
		}
		return tree, nil
	case string:
		return &jsonTree{kind: "string", text: token}, nil
	case json.Number:
		return &jsonTree{kind: "number", text: string(token)}, nil
	case bool:
		return &jsonTree{kind: "bool", text: strconv.FormatBool(token)}, nil
	case nil:
		return &jsonTree{kind: "null", text: "null"}, nil
	}

	return nil, fmt.Errorf("unexpected JSON token %v", token)
}

func (tree *jsonTree) depth() int {
	deepest := 0
	for _, child := range tree.items {
		deepest = max(deepest, child.depth())
	}
	for _, child := range tree.members {
		deepest = max(deepest, child.depth())
	}
	if tree.kind == "object" || tree.kind == "array" {
		deepest++
	}

	return deepest
}

type yamlResolver struct {
	tag     string
	pattern *regexp.Regexp
}

// resolveYAMLTag returns the tag the first matching resolver gives the plain
// scalar, and str when none matches.
func resolveYAMLTag(resolvers []yamlResolver, plain string) string {
	for _, resolver := range resolvers {
		if resolver.pattern.MatchString(plain) {
			return resolver.tag
		}
	}

	return "str"
}

// yaml11Resolvers are YAML 1.1's implicit types: the patterns of the YAML 1.1
// type repository (yaml.org/type) as PyYAML implements them, widened to what
// Ruby's Psych also accepts -- any letter case, commas between digits, months
// and days of one digit -- and to the repository's y and n, which PyYAML leaves
// out. The repository's float pattern is not used as written: its [0-9.]*
// would make 5.0.0 a float, which neither PyYAML nor Psych does.
var yaml11Resolvers = []yamlResolver{
	{"bool", regexp.MustCompile(`^(?i:y|yes|n|no|true|false|on|off)$`)},
	{"null", regexp.MustCompile(`^(?:~|(?i:null)|)$`)},
	{"int", regexp.MustCompile(`^[-+]?(?:0b[01_,]+|0[0-7_,]+|0|[1-9][0-9_,]*|0x[0-9a-fA-F_,]+|[0-9][0-9_]*(?::[0-5]?[0-9])+)$`)},
	{"float", regexp.MustCompile(`^(?:[-+]?(?:[0-9][0-9_,]*)?\.[0-9_]*(?:[eE][-+][0-9]+)?|` +
		`[-+]?[0-9][0-9_]*(?::[0-5]?[0-9])+\.[0-9_]*|[-+]?\.(?i:inf)|\.(?i:nan))$`)},
	{"timestamp", regexp.MustCompile(`^-?[0-9]{4}-[0-9]{1,2}-[0-9]{1,2}(?:(?:[Tt]|[ \t]+)[0-9]{1,2}:[0-9]{2}:[0-9]{2}` +
		`(?:\.[0-9]*)?(?:[ \t]*(?:Z|[-+][0-9]{1,2}(?::?[0-9]{2})?))?)?$`)},
	{"merge", regexp.MustCompile(`^<<$`)},
	{"value", regexp.MustCompile(`^=$`)},
}

// yaml12CoreResolvers are the YAML 1.2 core schema's, in the schema's order,
// so that 12 is an int before it can be a float.
var yaml12CoreResolvers = []yamlResolver{
	{"null", regexp.MustCompile(`^(?:null|Null|NULL|~|)$`)},
	{"bool", regexp.MustCompile(`^(?:true|True|TRUE|false|False|FALSE)$`)},
	{"int", regexp.MustCompile(`^(?:[-+]?[0-9]+|0o[0-7]+|0x[0-9a-fA-F]+)$`)},
	{"float", regexp.MustCompile(`^(?:[-+]?(?:\.[0-9]+|[0-9]+(?:\.[0-9]*)?)(?:[eE][-+]?[0-9]+)?|` +
		`[-+]?\.(?:inf|Inf|INF)|\.(?:nan|NaN|NAN))$`)},
}
