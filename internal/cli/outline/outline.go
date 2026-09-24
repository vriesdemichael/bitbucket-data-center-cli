// Package outline prints a JSON Schema as an outline of its fields, for a
// person.
//
// It backs `bb <command> --describe` without --json (ADR-097). The schema is
// the exact answer to what a command returns, and the wrong one to hand a
// person: pr merge's runs to hundreds of lines of braces and keywords, and
// whether a field can be absent is stated in a required list on its parent,
// away from the field. The outline gives each field one line -- its name, its
// type or allowed values, and its description -- and marks a field that can be
// absent where it is named:
//
//	data
//	  pullRequest     object                The pull request as it stands after the change.
//	    description?  string                Body text, when one was written.
//	    state         OPEN|MERGED|DECLINED
//
// Everything in it is read from the schema, so it cannot describe a field the
// schema does not have. ADR-097 asks for one renderer for every outline for the
// reason the schemas are derived rather than written: a second description of
// the same shape drifts from the first.
package outline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/google/jsonschema-go/jsonschema"
)

// A Member is one top-level key of the document being outlined.
//
// The caller composes the document, so it names the keys and gives each its
// own schema: data and meta for a run, say. Optional marks a key the document
// can go without, as a field outside its parent's required list is marked.
type Member struct {
	Name     string
	Optional bool
	Schema   *jsonschema.Schema
}

const (
	// lineWidth is where descriptions wrap. It is fixed rather than read from
	// a terminal: the outline is the same wherever it goes -- a terminal, a
	// pipe, a file, a test -- so what a person sees is what a test pins.
	lineWidth = 100

	// minDescriptionWidth is the least room a description gets. A deep outline
	// or a long enum pushes the description column far right, and wrapping to
	// lineWidth there would leave a word or two per line; a line that runs long
	// is easier to read than a column of single words.
	minDescriptionWidth = 40

	// columnGap keeps two columns from ever touching.
	columnGap = 2

	// maxDepth bounds how far the outline follows a schema. A derived schema
	// never refers to itself -- the reflector refuses a recursive type -- but a
	// *jsonschema.Schema built by hand can, and following one would not end.
	maxDepth = 32
)

// row is one field of the outline before layout. The columns are aligned
// across every row, so none can be printed until all of them are known.
type row struct {
	depth       int
	name        string
	kind        string
	description string
}

// Write prints members as one aligned outline.
//
// The columns line up across all members rather than per member, so the
// document reads as one table rather than one per key.
//
// A member that is an object with fields is printed as a heading, by name
// alone: the fields beneath it already say it is an object. Any other member
// keeps its type, because no other row says it. A data that is a list prints
// its elements' fields beneath it, and only its own row says they are in a
// list.
func Write(w io.Writer, members ...Member) error {
	var rows []row
	for _, member := range members {
		name := member.Name
		if member.Optional {
			name += "?"
		}

		heading := len(rows)
		rows = appendField(rows, 0, name, member.Schema)
		if rows[heading].kind == "object" && len(rows) > heading+1 {
			rows[heading].kind = ""
		}
	}

	_, err := io.WriteString(w, layout(rows))

	return err
}

// appendField adds the row for one field, then the rows for the fields beneath
// it, one level deeper.
func appendField(rows []row, depth int, name string, schema *jsonschema.Schema) []row {
	rows = append(rows, row{
		depth:       depth,
		name:        name,
		kind:        phrase(schema, 0),
		description: describe(schema),
	})

	beneath := fields(schema)
	if len(beneath) > 0 && depth >= maxDepth {
		// Say that the outline stops here rather than stopping silently.
		return append(rows, row{depth: depth + 1, name: "..."})
	}
	for _, child := range beneath {
		rows = appendField(rows, depth+1, child.name, child.schema)
	}

	return rows
}

// field is a row to be: its name as printed, and the schema describing it.
type field struct {
	name   string
	schema *jsonschema.Schema
}

// fields returns what is printed one level beneath a schema: the properties of
// an object, or the alternatives of a oneOf or anyOf, numbered.
//
// A list or a map has no fields of its own, so its elements' or values' fields
// are printed beneath it. They get no row in between, because the type column
// already says list of or map of, and that row would only repeat it.
func fields(schema *jsonschema.Schema) []field {
	for hop := 0; schema != nil && hop <= maxDepth; hop++ {
		if branches := branchesOf(schema); len(branches) > 0 {
			numbered := make([]field, len(branches))
			for index, branch := range branches {
				numbered[index] = field{name: fmt.Sprintf("(%d)", index+1), schema: branch}
			}

			return numbered
		}

		if len(schema.Properties) > 0 {
			return properties(schema)
		}

		switch types := typesOf(schema); {
		case slices.Contains(types, "array"):
			schema = schema.Items
		case slices.Contains(types, "object") && mapValues(schema) != nil:
			schema = mapValues(schema)
		default:
			return nil
		}
	}

	return nil
}

// properties returns an object's fields in the order its Go struct declares
// them, which is PropertyOrder, followed by any that PropertyOrder leaves out,
// sorted so that the outline is the same on every run.
//
// A field its parent does not require is marked with a ?. Whether a field can
// be absent is what a caller most needs to know before reading it, and the
// schema says so in a list on the parent, away from the field.
func properties(schema *jsonschema.Schema) []field {
	names := make([]string, 0, len(schema.Properties))
	listed := make(map[string]bool, len(schema.Properties))
	for _, name := range schema.PropertyOrder {
		if _, declared := schema.Properties[name]; declared && !listed[name] {
			names = append(names, name)
			listed[name] = true
		}
	}

	unordered := make([]string, 0, len(schema.Properties)-len(names))
	for name := range schema.Properties {
		if !listed[name] {
			unordered = append(unordered, name)
		}
	}
	sort.Strings(unordered)

	required := make(map[string]bool, len(schema.Required))
	for _, name := range schema.Required {
		required[name] = true
	}

	named := make([]field, 0, len(schema.Properties))
	for _, name := range append(names, unordered...) {
		label := name
		if !required[name] {
			label += "?"
		}
		named = append(named, field{name: label, schema: schema.Properties[name]})
	}

	return named
}

// phrase is what the type column says about a schema.
//
// minimum and maximum are left out. A derived schema carries them only as the
// range of the Go integer a field is held in -- every one today is int32's --
// which says how bb stores the value, not a limit anyone reading it works to.
func phrase(schema *jsonschema.Schema, level int) string {
	switch {
	case level > maxDepth:
		return "..."
	case schema == nil:
		return "any"
	}

	var kinds []string
	switch {
	case len(branchesOf(schema)) > 0:
		kinds = append(kinds, "one of")

	// An enum or a const is the whole set of values, so it stands alone,
	// without "or null": JSON Schema accepts null there only when the enum
	// lists it, whatever the type allows, and then it is printed as a value.
	//
	// A const is printed as JSON, quotes and all. A lone string printed bare
	// could read as a type: a field whose only value is "object" is not an
	// object.
	case schema.Const != nil:
		return encode(*schema.Const)
	case len(schema.Enum) > 0:
		return enumPhrase(schema.Enum)

	default:
		for _, name := range typesOf(schema) {
			switch name {
			case "null":
				// Added last, below, wherever the type list puts it.
			case "array":
				kinds = append(kinds, "list of "+nested(phrase(schema.Items, level+1)))
			case "object":
				if values := mapValues(schema); values != nil {
					kinds = append(kinds, "map of "+nested(phrase(values, level+1)))
				} else {
					kinds = append(kinds, "object")
				}
			default:
				kinds = append(kinds, name)
			}
		}
	}

	if slices.Contains(typesOf(schema), "null") {
		kinds = append(kinds, "null")
	}
	if len(kinds) == 0 {
		return "any"
	}

	return strings.Join(kinds, " or ")
}

// nested puts an element's phrase in parentheses when it has alternatives of
// its own. Without them, list of object or null would mean both a list that
// can be null and a list whose elements can be.
func nested(phrase string) string {
	if strings.Contains(phrase, " or ") {
		return "(" + phrase + ")"
	}

	return phrase
}

// enumPhrase joins an enum's values with |.
//
// A string is printed bare, because OPEN|MERGED|DECLINED is what a reader
// scans for and quotes would only get in the way. Anything else is printed as
// the JSON it appears as in a document -- null, true, 3 -- and so is a string
// that bare would read as something else: an empty one, or one holding a |.
func enumPhrase(values []any) string {
	texts := make([]string, len(values))
	for index, value := range values {
		text, isString := value.(string)
		if !isString || text == "" || strings.Contains(text, "|") {
			text = encode(value)
		}
		texts[index] = text
	}

	return strings.Join(texts, "|")
}

// mapValues returns the schema of a map's values, or nil when the schema is
// not a map.
//
// The reflector gives every struct additionalProperties: false, which
// jsonschema-go spells {"not": {}}. It says the object has no keys beyond its
// fields, which the fields already say, so it makes nothing a map. A
// map[string]any derives the empty schema instead: a map whose values can be
// anything.
//
// An object with properties is printed as its properties even when it takes
// other keys as well. The reflector never derives one, and a row cannot be
// both an object and a map without inventing notation for the rest.
func mapValues(schema *jsonschema.Schema) *jsonschema.Schema {
	values := schema.AdditionalProperties
	if values == nil || len(schema.Properties) > 0 || rejectsEverything(values) {
		return nil
	}

	return values
}

// rejectsEverything reports the false schema. A schema whose not is the empty
// schema accepts no value, whatever else it says.
func rejectsEverything(schema *jsonschema.Schema) bool {
	return schema.Not != nil && reflect.DeepEqual(*schema.Not, jsonschema.Schema{})
}

// branchesOf returns the alternatives a schema offers, oneOf's then anyOf's.
//
// Both are printed as one of. What tells them apart is whether a value may
// match more than one branch, which matters to a validator and not to a reader
// working out which shape to expect.
func branchesOf(schema *jsonschema.Schema) []*jsonschema.Schema {
	return slices.Concat(schema.OneOf, schema.AnyOf)
}

// typesOf returns a schema's types, from whichever of Type and Types holds
// them.
func typesOf(schema *jsonschema.Schema) []string {
	if schema.Type != "" {
		return []string{schema.Type}
	}

	return schema.Types
}

// describe returns a schema's description as one line, or nothing when all it
// does is list the values the type column already shows.
//
// Most enum fields describe themselves by naming their values, and "OPEN,
// MERGED or DECLINED." beside OPEN|MERGED|DECLINED says the same thing twice
// on one line. Only a description that does nothing else is dropped: "PASS or
// FAIL. Absent when the reporter did not state one." keeps what it adds, and
// "REVIEWER or PARTICIPANT." beside AUTHOR|REVIEWER|PARTICIPANT says something
// the type column does not.
func describe(schema *jsonschema.Schema) string {
	if schema == nil {
		return ""
	}

	description := strings.Join(strings.Fields(schema.Description), " ")
	if len(branchesOf(schema)) == 0 && restates(description, listedValues(schema)) {
		return ""
	}

	return description
}

// listedValues returns the values a schema allows when it names them all: its
// const, or its enum.
func listedValues(schema *jsonschema.Schema) []any {
	if schema.Const != nil {
		return []any{*schema.Const}
	}

	return schema.Enum
}

// listSeparator splits "A, B or C", "A, B, or C" and "A or B" into their
// items.
var listSeparator = regexp.MustCompile(`\s*,\s*(?:or\s+)?|\s+or\s+`)

// restates reports whether a description names exactly the listed values, in
// any order, and nothing else.
func restates(description string, values []any) bool {
	if description == "" || len(values) == 0 {
		return false
	}

	listed := make(map[string]bool, len(values))
	for _, value := range values {
		text, isString := value.(string)
		if !isString {
			text = encode(value)
		}
		listed[text] = true
	}

	named := make(map[string]bool, len(listed))
	for _, item := range listSeparator.Split(strings.TrimSuffix(description, "."), -1) {
		if !listed[item] {
			return false
		}
		named[item] = true
	}

	return len(named) == len(listed)
}

// encode prints a value as the JSON it appears as in a document. HTML escaping
// is off, since the reader is a person and not a browser.
func encode(value any) string {
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		// Only a value built in Go rather than read from JSON can get here.
		return fmt.Sprint(value)
	}

	return strings.TrimSuffix(encoded.String(), "\n")
}

// layout aligns the rows into columns and wraps their descriptions.
//
// Descriptions wrap at lineWidth, each continuation starting under the first
// line. When the description column starts so far right that fewer than
// minDescriptionWidth columns remain, a description gets that many anyway and
// its lines run long.
func layout(rows []row) string {
	nameWidth, kindWidth := 0, 0
	for _, entry := range rows {
		nameWidth = max(nameWidth, 2*entry.depth+columns(entry.name))
		kindWidth = max(kindWidth, columns(entry.kind))
	}
	kindColumn := nameWidth + columnGap
	descriptionColumn := kindColumn + kindWidth + columnGap
	wrapWidth := max(lineWidth-descriptionColumn, minDescriptionWidth)

	var out strings.Builder
	for _, entry := range rows {
		line := strings.Repeat("  ", entry.depth) + entry.name
		if entry.kind != "" {
			line = padTo(line, kindColumn) + entry.kind
		}
		for index, text := range wrap(entry.description, wrapWidth) {
			if index > 0 {
				out.WriteString(line + "\n")
				line = ""
			}
			line = padTo(line, descriptionColumn) + text
		}
		out.WriteString(line + "\n")
	}

	return out.String()
}

// wrap breaks text into lines of at most width columns, between words. A word
// longer than that gets a line to itself rather than being cut, since half an
// identifier or a URL is worse than a long line.
func wrap(text string, width int) []string {
	var lines []string
	for _, word := range strings.Fields(text) {
		last := len(lines) - 1
		if last >= 0 && columns(lines[last])+1+columns(word) <= width {
			lines[last] += " " + word
			continue
		}
		lines = append(lines, word)
	}

	return lines
}

// padTo extends text with spaces to the given column.
func padTo(text string, column int) string {
	return text + strings.Repeat(" ", max(column-columns(text), 0))
}

// columns counts the columns text takes, one per character rather than per
// byte, so a name or description with an accent or a dash still lines up.
func columns(text string) int {
	return utf8.RuneCountInString(text)
}
