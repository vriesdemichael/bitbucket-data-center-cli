package outline_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/outline"
)

// write renders members, failing the test if Write does, and checks what every
// outline must be whatever it holds: no line ending in whitespace, and the
// whole ending in a newline.
func write(t *testing.T, members ...outline.Member) string {
	t.Helper()

	var out strings.Builder
	if err := outline.Write(&out, members...); err != nil {
		t.Fatalf("Write: %v", err)
	}

	text := out.String()
	if text != "" && !strings.HasSuffix(text, "\n") {
		t.Errorf("the outline does not end with a newline:\n%s", text)
	}
	for number, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		if strings.TrimRight(line, " \t") != line {
			t.Errorf("line %d ends in whitespace: %q", number+1, line)
		}
	}

	return text
}

// expect compares an outline with the text a test spells out. The text may
// start on the line after the opening backquote, so its columns read as they
// are printed.
func expect(t *testing.T, got, want string) {
	t.Helper()

	if want = strings.TrimPrefix(want, "\n"); got != want {
		t.Errorf("the outline differs\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// columnBreak separates the columns of a printed row. A name, a type phrase and
// a description each hold single spaces at most.
var columnBreak = regexp.MustCompile(` {2,}`)

// property and object build a schema the way the reflector does: properties in
// declaration order, and the names of the required ones.
type property struct {
	name   string
	schema *jsonschema.Schema
}

func object(required []string, properties ...property) *jsonschema.Schema {
	schema := &jsonschema.Schema{
		Type:       "object",
		Properties: make(map[string]*jsonschema.Schema, len(properties)),
		Required:   required,
	}
	for _, each := range properties {
		schema.Properties[each.name] = each.schema
		schema.PropertyOrder = append(schema.PropertyOrder, each.name)
	}

	return schema
}

// TestAFieldThatCanBeAbsentIsMarked covers the ? suffix, on a field its parent
// does not require and on a member the caller marks optional.
func TestAFieldThatCanBeAbsentIsMarked(t *testing.T) {
	t.Parallel()

	type payload struct {
		ID   int64  `json:"id"`
		Note string `json:"note,omitempty"`
	}
	schema, err := jsonschema.For[payload](nil)
	if err != nil {
		t.Fatal(err)
	}

	expect(t, write(t,
		outline.Member{Name: "data", Schema: schema},
		outline.Member{Name: "error", Optional: true, Schema: &jsonschema.Schema{Type: "string"}},
	), `
data
  id     integer
  note?  string
error?   string
`)
}

// TestTheTypeColumn covers each phrase the type column can hold.
func TestTheTypeColumn(t *testing.T) {
	t.Parallel()

	withFields := object([]string{"id"}, property{"id", &jsonschema.Schema{Type: "integer"}})

	for _, testCase := range []struct {
		name   string
		schema *jsonschema.Schema
		want   string
	}{
		{"no schema", nil, "any"},
		{"no type", &jsonschema.Schema{}, "any"},
		{"string", &jsonschema.Schema{Type: "string"}, "string"},
		{"number", &jsonschema.Schema{Type: "number"}, "number"},
		{"boolean", &jsonschema.Schema{Type: "boolean"}, "boolean"},
		{
			"an integer without the range of the Go type holding it",
			&jsonschema.Schema{Type: "integer", Minimum: jsonschema.Ptr(float64(math.MinInt32)), Maximum: jsonschema.Ptr(float64(math.MaxInt32))},
			"integer",
		},
		{"an object without fields", &jsonschema.Schema{Type: "object"}, "object"},
		{
			"an object whose additionalProperties is false",
			&jsonschema.Schema{Type: "object", AdditionalProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}}},
			"object",
		},
		{"a list", &jsonschema.Schema{Type: "array", Items: &jsonschema.Schema{Type: "string"}}, "list of string"},
		{"a list of objects", &jsonschema.Schema{Type: "array", Items: withFields}, "list of object"},
		{
			"a list of enum values",
			&jsonschema.Schema{Type: "array", Items: &jsonschema.Schema{Type: "string", Enum: []any{"OPEN", "MERGED"}}},
			"list of OPEN|MERGED",
		},
		{
			"a list of lists",
			&jsonschema.Schema{Type: "array", Items: &jsonschema.Schema{Type: "array", Items: &jsonschema.Schema{Type: "integer"}}},
			"list of list of integer",
		},
		{"a list without items", &jsonschema.Schema{Type: "array"}, "list of any"},
		{"a map", &jsonschema.Schema{Type: "object", AdditionalProperties: &jsonschema.Schema{Type: "integer"}}, "map of integer"},
		{"a map of anything", &jsonschema.Schema{Type: "object", AdditionalProperties: &jsonschema.Schema{}}, "map of any"},
		{"a map of objects", &jsonschema.Schema{Type: "object", AdditionalProperties: withFields}, "map of object"},
		{
			"a list that can be null, as the reflector types a slice",
			&jsonschema.Schema{Types: []string{"null", "array"}, Items: withFields},
			"list of object or null",
		},
		{
			"a list whose elements can be null",
			&jsonschema.Schema{Type: "array", Items: &jsonschema.Schema{Types: []string{"null", "object"}}},
			"list of (object or null)",
		},
		{"a string that can be null", &jsonschema.Schema{Types: []string{"null", "string"}}, "string or null"},
		{"null listed after the type", &jsonschema.Schema{Types: []string{"string", "null"}}, "string or null"},
		{
			"a map that can be null",
			&jsonschema.Schema{Types: []string{"null", "object"}, AdditionalProperties: &jsonschema.Schema{Type: "string"}},
			"map of string or null",
		},
		{"two types", &jsonschema.Schema{Types: []string{"string", "integer"}}, "string or integer"},
		{"null alone", &jsonschema.Schema{Type: "null"}, "null"},
		{"an enum", &jsonschema.Schema{Type: "string", Enum: []any{"OPEN", "MERGED", "DECLINED"}}, "OPEN|MERGED|DECLINED"},
		{"an enum of numbers", &jsonschema.Schema{Type: "integer", Enum: []any{1, 2, 3}}, "1|2|3"},
		{"an enum of mixed values", &jsonschema.Schema{Enum: []any{"on", 1.5, true, nil}}, "on|1.5|true|null"},
		{
			"an enum of strings that would read as something else bare",
			&jsonschema.Schema{Type: "string", Enum: []any{"", "a|b", "c"}},
			`""|"a|b"|c`,
		},
		{
			"an enum that can be null names null as a value",
			&jsonschema.Schema{Types: []string{"null", "string"}, Enum: []any{"OPEN", nil}},
			"OPEN|null",
		},
		{"a const string", &jsonschema.Schema{Type: "string", Const: jsonschema.Ptr[any]("object")}, `"object"`},
		{"a const number", &jsonschema.Schema{Const: jsonschema.Ptr[any](3)}, "3"},
		{"a const null", &jsonschema.Schema{Const: new(any)}, "null"},
		{"oneOf", &jsonschema.Schema{OneOf: []*jsonschema.Schema{{Type: "string"}, {Type: "integer"}}}, "one of"},
		{"anyOf", &jsonschema.Schema{AnyOf: []*jsonschema.Schema{{Type: "string"}, {Type: "integer"}}}, "one of"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			first, _, _ := strings.Cut(write(t, outline.Member{Name: "f", Schema: testCase.schema}), "\n")
			if got := columnBreak.Split(first, -1); len(got) != 2 || got[1] != testCase.want {
				t.Errorf("printed %q, want the type %q", first, testCase.want)
			}
		})
	}
}

// TestTheFieldsOfListElementsAndMapValuesArePrintedBeneathThem covers the rows
// under a list or a map: the fields of its elements or values, one level down,
// with no row of their own in between.
func TestTheFieldsOfListElementsAndMapValuesArePrintedBeneathThem(t *testing.T) {
	t.Parallel()

	reviewer := object([]string{"name"},
		property{"name", &jsonschema.Schema{Type: "string"}},
		property{"status", &jsonschema.Schema{Type: "string", Enum: []any{"APPROVED", "NEEDS_WORK"}}},
	)
	label := object([]string{"color"}, property{"color", &jsonschema.Schema{Type: "string"}})
	schema := object([]string{"reviewers"},
		property{"reviewers", &jsonschema.Schema{Type: "array", Items: reviewer}},
		property{"labels", &jsonschema.Schema{Type: "object", AdditionalProperties: label}},
	)

	expect(t, write(t, outline.Member{Name: "data", Schema: schema}), `
data
  reviewers  list of object
    name     string
    status?  APPROVED|NEEDS_WORK
  labels?    map of object
    color    string
`)
}

// TestBranchesArePrintedAsNumberedRows covers oneOf and anyOf: one of in the
// type column, and a row per branch beneath it, each with its own fields.
func TestBranchesArePrintedAsNumberedRows(t *testing.T) {
	t.Parallel()

	branches := []*jsonschema.Schema{
		object([]string{"branch"}, property{"branch", &jsonschema.Schema{Type: "string"}}),
		{Type: "string", Description: "A commit id."},
	}

	for keyword, schema := range map[string]*jsonschema.Schema{
		"oneOf": {OneOf: branches},
		"anyOf": {AnyOf: branches},
	} {
		t.Run(keyword, func(t *testing.T) {
			t.Parallel()

			expect(t, write(t, outline.Member{Name: "data", Schema: object(nil, property{"target", schema})}), `
data
  target?     one of
    (1)       object
      branch  string
    (2)       string  A commit id.
`)
		})
	}
}

// TestADescriptionThatOnlyListsTheValuesIsLeftOut covers the one description the
// outline drops, because the type column already says it, and the near misses
// it keeps because they say something more.
func TestADescriptionThatOnlyListsTheValuesIsLeftOut(t *testing.T) {
	t.Parallel()

	states := []any{"OPEN", "MERGED", "DECLINED"}

	for _, testCase := range []struct {
		name   string
		schema *jsonschema.Schema
		want   string
	}{
		{"the values in order", &jsonschema.Schema{Enum: states, Description: "OPEN, MERGED or DECLINED."}, ""},
		{"the values in another order", &jsonschema.Schema{Enum: states, Description: "DECLINED, OPEN or MERGED."}, ""},
		{
			"the values with a serial comma",
			&jsonschema.Schema{Enum: []any{"installed", "removed", "not_found"}, Description: "installed, removed, or not_found."},
			"",
		},
		{"the values across lines, without a full stop", &jsonschema.Schema{Enum: states, Description: "OPEN,\n\tMERGED  or DECLINED"}, ""},
		{"numbers", &jsonschema.Schema{Type: "integer", Enum: []any{1, 2, 3}, Description: "1, 2 or 3."}, ""},
		{"a const", &jsonschema.Schema{Const: jsonschema.Ptr[any]("ok"), Description: "ok."}, ""},
		{
			"only some of the values",
			&jsonschema.Schema{Enum: []any{"AUTHOR", "REVIEWER", "PARTICIPANT"}, Description: "REVIEWER or PARTICIPANT."},
			"REVIEWER or PARTICIPANT.",
		},
		{
			// The sentence listing the values says nothing the type column
			// does not, so the one after it is shown.
			"the values and something more",
			&jsonschema.Schema{Enum: []any{"PASS", "FAIL"}, Description: "PASS or FAIL. Absent when the reporter did not state one."},
			"Absent when the reporter did not state one.",
		},
		{
			"the values, introduced",
			&jsonschema.Schema{Enum: []any{"token", "basic", "none"}, Description: "How bb authenticates: token, basic, or none."},
			"How bb authenticates: token, basic, or none.",
		},
		{
			"values on a field without an enum",
			&jsonschema.Schema{Type: "string", Description: "OPEN, MERGED or DECLINED."},
			"OPEN, MERGED or DECLINED.",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			printed := columnBreak.Split(strings.TrimSuffix(write(t, outline.Member{Name: "f", Schema: testCase.schema}), "\n"), -1)
			got := ""
			if len(printed) == 3 {
				got = printed[2]
			}
			if got != testCase.want {
				t.Errorf("printed the description %q, want %q", got, testCase.want)
			}
		})
	}
}

// TestADescriptionIsPrintedAsOneLineOfSingleSpaces covers a description written
// across lines, as a long struct tag or a hand-written schema can be.
func TestADescriptionIsPrintedAsOneLineOfSingleSpaces(t *testing.T) {
	t.Parallel()

	schema := &jsonschema.Schema{Type: "string", Description: "  Body text,\n\twhen one   was\r\nwritten.  "}

	expect(t, write(t, outline.Member{Name: "f", Schema: schema}), `
f  string  Body text, when one was written.
`)
}

// TestALongDescriptionWrapsAtColumnOneHundredTwenty covers wrapping: no line
// runs past column 120, and each continuation starts under the first line.
func TestALongDescriptionWrapsAtColumnOneHundredTwenty(t *testing.T) {
	t.Parallel()

	// The description column is 11, so twenty-two four-letter words fill a
	// line to exactly column 120, and the twenty-third starts the next one.
	schema := &jsonschema.Schema{Type: "string", Description: strings.Repeat("word ", 30)}
	first := "f  string  " + strings.TrimSpace(strings.Repeat("word ", 22))
	if len(first) != 120 {
		t.Fatalf("the expected first line is %d columns; the test needs it to end at exactly 120", len(first))
	}

	expect(t, write(t, outline.Member{Name: "f", Schema: schema}),
		first+"\n"+
			strings.Repeat(" ", 11)+strings.TrimSpace(strings.Repeat("word ", 8))+"\n")
}

// TestADescriptionColumnPastEightyStillGetsFortyColumns covers a type column so
// wide that wrapping at 120 would leave a word or two per line: the description
// gets 40 columns anyway, and its lines run long.
func TestADescriptionColumnPastEightyStillGetsFortyColumns(t *testing.T) {
	t.Parallel()

	// Eight ten-letter values make a type 87 columns wide, which puts the
	// description at column 92, with 28 left before 120. Forty columns hold
	// eight four-letter words; a ninth would need 44.
	values := []any{"aaaaaaaaaa", "bbbbbbbbbb", "cccccccccc", "dddddddddd", "eeeeeeeeee", "ffffffffff", "gggggggggg", "hhhhhhhhhh"}
	schema := &jsonschema.Schema{Type: "string", Enum: values, Description: strings.Repeat("word ", 20)}
	eight := strings.TrimSpace(strings.Repeat("word ", 8))
	indent := strings.Repeat(" ", 92)

	expect(t, write(t, outline.Member{Name: "f", Schema: schema}),
		"f  aaaaaaaaaa|bbbbbbbbbb|cccccccccc|dddddddddd|eeeeeeeeee|ffffffffff|gggggggggg|hhhhhhhhhh  "+eight+"\n"+
			indent+eight+"\n"+
			indent+strings.TrimSpace(strings.Repeat("word ", 4))+"\n")
}

// TestAWordLongerThanALineIsNotBroken covers a URL or an identifier too long for
// the description column: it gets a line of its own and runs long, since half
// of one is worse than a long line.
func TestAWordLongerThanALineIsNotBroken(t *testing.T) {
	t.Parallel()

	address := "https://example.com/" + strings.Repeat("a", 100)
	schema := &jsonschema.Schema{Type: "string", Description: "See " + address + " for more."}
	indent := strings.Repeat(" ", 11)

	expect(t, write(t, outline.Member{Name: "f", Schema: schema}),
		"f  string  See\n"+indent+address+"\n"+indent+"for more.\n")
}

// TestColumnsAlignAcrossMembers covers the layout being worked out for the whole
// document rather than per member: the longest name under meta decides where
// the type column under data starts.
func TestColumnsAlignAcrossMembers(t *testing.T) {
	t.Parallel()

	data := object([]string{"id"}, property{"id", &jsonschema.Schema{Type: "integer", Description: "Pull request number."}})
	meta := object([]string{"bbVersion"}, property{"bbVersion", &jsonschema.Schema{Type: "string", Description: "The bb version that wrote it."}})

	expect(t, write(t, outline.Member{Name: "data", Schema: data}, outline.Member{Name: "meta", Schema: meta}), `
data
  id         integer  Pull request number.
meta
  bbVersion  string   The bb version that wrote it.
`)
}

// TestFieldsFollowTheDeclaredOrderThenTheRestSorted covers PropertyOrder. It is
// the Go struct's field order, so the outline reads like the type; a property
// it leaves out still gets a row, in a place that is the same on every run.
func TestFieldsFollowTheDeclaredOrderThenTheRestSorted(t *testing.T) {
	t.Parallel()

	text := &jsonschema.Schema{Type: "string"}
	schema := &jsonschema.Schema{
		Type:          "object",
		Properties:    map[string]*jsonschema.Schema{"a": text, "b": text, "c": text, "d": text},
		PropertyOrder: []string{"d", "gone", "b"},
		Required:      []string{"a", "b", "c", "d"},
	}

	expect(t, write(t, outline.Member{Name: "data", Schema: schema}), `
data
  d   string
  b   string
  a   string
  c   string
`)
}

// TestAdditionalPropertiesMakesAMapOnlyWhenItIsASchema covers the keyword as JSON
// spells it. false, which the reflector gives every struct and jsonschema-go
// reads back as {"not": {}}, says there are no other keys and adds nothing;
// true or a schema makes the object a map.
func TestAdditionalPropertiesMakesAMapOnlyWhenItIsASchema(t *testing.T) {
	t.Parallel()

	for document, want := range map[string]string{
		`{"type": "object", "additionalProperties": false}`:               "object",
		`{"type": "object", "additionalProperties": {"not": {}}}`:         "object",
		`{"type": "object", "additionalProperties": true}`:                "map of any",
		`{"type": "object", "additionalProperties": {"type": "integer"}}`: "map of integer",
	} {
		var schema jsonschema.Schema
		if err := json.Unmarshal([]byte(document), &schema); err != nil {
			t.Fatalf("%s: %v", document, err)
		}
		if got := write(t, outline.Member{Name: "f", Schema: &schema}); got != "f  "+want+"\n" {
			t.Errorf("%s printed %q, want the type %q", document, got, want)
		}
	}
}

// TestAMemberThatIsAnObjectIsAHeading covers the one row that leaves its type
// out: a member whose fields follow it. A member of any other kind keeps its
// type, since nothing else would say it, and a heading keeps its description.
func TestAMemberThatIsAnObjectIsAHeading(t *testing.T) {
	t.Parallel()

	fields := object([]string{"id"}, property{"id", &jsonschema.Schema{Type: "integer"}})
	expect(t, write(t,
		outline.Member{Name: "data", Schema: &jsonschema.Schema{Type: "array", Items: fields}},
		outline.Member{Name: "meta", Schema: &jsonschema.Schema{Type: "object"}},
		outline.Member{Name: "error", Optional: true},
	), `
data    list of object
  id    integer
meta    object
error?  any
`)

	described := object([]string{"id"}, property{"id", &jsonschema.Schema{Type: "integer"}})
	described.Description = "What the command returns."
	expect(t, write(t, outline.Member{Name: "data", Schema: described}), `
data           What the command returns.
  id  integer
`)
}

// TestASchemaThatContainsItselfStops covers the depth guard. A derived schema
// cannot contain itself, but one built by hand can, through a property or
// through a list's elements, and the outline has to end either way.
func TestASchemaThatContainsItselfStops(t *testing.T) {
	t.Parallel()

	node := &jsonschema.Schema{Type: "object", Properties: map[string]*jsonschema.Schema{}, Required: []string{"child"}}
	node.Properties["child"] = node

	lines := strings.Split(strings.TrimSuffix(write(t, outline.Member{Name: "data", Schema: node}), "\n"), "\n")
	last, before := lines[len(lines)-1], lines[len(lines)-2]
	depth := func(line string) int { return (len(line) - len(strings.TrimLeft(line, " "))) / 2 }
	if strings.TrimSpace(last) != "..." || depth(last) != depth(before)+1 || !strings.HasPrefix(strings.TrimSpace(before), "child") {
		t.Errorf("the outline should end with ... one level under the deepest child, got:\n%s\n%s", before, last)
	}

	list := &jsonschema.Schema{Type: "array"}
	list.Items = list
	if got := write(t, outline.Member{Name: "data", Schema: list}); strings.Count(got, "\n") != 1 || !strings.HasSuffix(got, " list of ...\n") {
		t.Errorf("a list of itself should print one row whose type ends in ..., got:\n%s", got)
	}
}

// TestWriteReportsAWriterThatFails covers the error Write returns: the only way
// a caller learns that the outline never reached its reader.
func TestWriteReportsAWriterThatFails(t *testing.T) {
	t.Parallel()

	err := outline.Write(failingWriter{}, outline.Member{Name: "data", Schema: &jsonschema.Schema{Type: "string"}})
	if !errors.Is(err, errWriteFailed) {
		t.Errorf("Write returned %v, want %v", err, errWriteFailed)
	}
}

var errWriteFailed = errors.New("write failed")

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errWriteFailed
}

// TestNoMembersPrintNothing covers the empty outline: no rows, and so not even a
// newline.
func TestNoMembersPrintNothing(t *testing.T) {
	t.Parallel()

	if got := write(t); got != "" {
		t.Errorf("an outline of nothing printed %q", got)
	}
}

// TestADescriptionShowsItsFirstSentence: the outline is for scanning, so each
// field says what it is in one sentence; the rest is in the schema.
func TestADescriptionShowsItsFirstSentence(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct{ description, want string }{
		{"Optimistic-locking version. Pass it back when updating.", "Optimistic-locking version."},
		{"One sentence only.", "One sentence only."},
		{"No full stop", "No full stop"},
		// A full stop not followed by a capital does not end the sentence.
		{"Version 8.0 and later. More.", "Version 8.0 and later."},
	} {
		var out bytes.Buffer
		if err := outline.Write(&out, outline.Member{Name: "field", Schema: &jsonschema.Schema{Type: "string", Description: testCase.description}}); err != nil {
			t.Fatalf("Write: %v", err)
		}
		if got := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(out.String()), "field")); !strings.HasSuffix(got, testCase.want) || strings.Contains(got, "More") || strings.Contains(got, "Pass it back") {
			t.Errorf("%q printed %q, want it to end at %q", testCase.description, got, testCase.want)
		}
	}
}
