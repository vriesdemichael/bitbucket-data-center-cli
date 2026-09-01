// Package result holds the typed values commands return, and derives their
// published schemas from those types.
//
// A command builds one result value and renders it: serialised for --json,
// formatted for a human. Both read the same value, so the two cannot disagree
// about what happened -- which they have, silently: cancelledTargets reached
// the JSON payload, the schema and the tests, while the human summary kept
// printing "successful=0 failed=0" for three cancelled repositories.
//
// The schema is derived from the type rather than written beside it. A
// hand-written schema is a second description of the same shape, and the second
// one drifts: two published schemas described `bb branch get-default`, a
// command that has never existed, and the meta schema forbade a field every
// listing command emits. ADR-010 asked for this -- "keep schemas generated from
// the model source of truth rather than hand-maintained files" -- and output
// schemas were the one place it was not done.
//
// # Writing a result type
//
// Use value fields, not pointers. The reflector maps a pointer to a nullable
// type, and encoding/json with omitempty omits a nil pointer rather than
// writing null -- so a pointer field publishes a null the command cannot
// actually emit. Reach for a pointer only where null is a value the caller must
// tell apart from absent.
//
// Mark optional fields omitempty. Everything else becomes required in the
// schema, which is a promise the command has to keep on every run.
//
// Describe fields with the jsonschema struct tag. It is the only description a
// consumer gets, since there is no prose file beside the schema any more.
package result

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/google/jsonschema-go/jsonschema"
)

// For derives the schema for a result type, with optional enums by property
// name.
//
// Enums are applied after derivation because Go has no enum: a string field is
// a string to the reflector, and the set of values it actually takes is
// knowledge the command has. Naming a property the type does not have is a
// wiring mistake, so it panics rather than silently doing nothing -- the same
// choice internal/mcp makes for tool input schemas.
func For[T any](enums map[string][]string) *jsonschema.Schema {
	schema, err := jsonschema.For[T](nil)
	if err != nil {
		panic(fmt.Sprintf("deriving output schema for %T: %v", *new(T), err))
	}

	tightenOptionalNullables(schema)
	applyEnums(schema, enums, fmt.Sprintf("%T", *new(T)))

	return schema
}

// tightenOptionalNullables drops null from optional fields that can never be
// null.
//
// The reflector types a Go pointer as null-or-something, which is right for a
// field that marshals nil as null. It is wrong for every field carrying
// omitempty, because encoding/json omits a nil pointer there rather than
// writing null -- so the schema would publish a null the command cannot emit,
// and a consumer would write a branch that never runs.
//
// The reflector marks exactly the omitempty and omitzero fields optional, so
// "not required" is the signal. A pointer field without omitempty keeps its
// null, because that one really does marshal to null.
func tightenOptionalNullables(schema *jsonschema.Schema) {
	if schema == nil {
		return
	}

	required := make(map[string]bool, len(schema.Required))
	for _, name := range schema.Required {
		required[name] = true
	}

	for name, property := range schema.Properties {
		if !required[name] {
			dropNull(property)
		}
		tightenOptionalNullables(property)
	}

	tightenOptionalNullables(schema.Items)
}

// dropNull removes the null member of a type union, collapsing what is left to
// a single type when only one remains.
func dropNull(schema *jsonschema.Schema) {
	if schema == nil || len(schema.Types) == 0 {
		return
	}

	kept := make([]string, 0, len(schema.Types))
	for _, candidate := range schema.Types {
		if candidate != "null" {
			kept = append(kept, candidate)
		}
	}

	switch len(kept) {
	case 0:
		return
	case 1:
		schema.Type = kept[0]
		schema.Types = nil
	default:
		schema.Types = kept
	}
}

// List derives the schema for a command whose payload is a list of T.
//
// Not For[[]T]. A Go slice can be nil, so the reflector types it as null-or-array
// -- and a consumer reading that has to handle a null the command never sends,
// because a list command builds its slice with make and writes [] when there is
// nothing to report. Saying array here is the narrower claim and the true one.
//
// It is also the claim worth keeping true: a command that starts returning null
// for an empty list breaks every caller iterating the result, and the schema is
// what says that must not happen.
func List[T any](enums map[string][]string) *jsonschema.Schema {
	return &jsonschema.Schema{
		Type:  "array",
		Items: For[T](enums),
	}
}

// applyEnums sets the allowed values on properties named by a dotted path.
//
// "type" reaches a field on the payload itself; "refs.type" reaches one on
// every element of the refs list. Nesting is the normal case -- most payloads
// wrap their subject in a repository and a list -- so a flat lookup would only
// ever work for the shallowest of them.
func applyEnums(schema *jsonschema.Schema, enums map[string][]string, typeName string) {
	for path, values := range enums {
		field := resolveProperty(schema, path)
		if field == nil {
			panic(fmt.Sprintf("enum declared for %q which %s does not have", path, typeName))
		}

		field.Enum = make([]any, 0, len(values))
		for _, value := range values {
			field.Enum = append(field.Enum, value)
		}
	}
}

// resolveProperty walks a dotted path, stepping through list items wherever it
// meets one, and returns nil if any segment is missing.
func resolveProperty(schema *jsonschema.Schema, path string) *jsonschema.Schema {
	current := schema

	for _, segment := range strings.Split(path, ".") {
		// A list is transparent: naming a property means naming it on the
		// element, since a list has no properties of its own.
		for current != nil && current.Items != nil {
			current = current.Items
		}
		if current == nil {
			return nil
		}

		next, ok := current.Properties[segment]
		if !ok {
			return nil
		}
		current = next
	}

	for current != nil && current.Items != nil {
		current = current.Items
	}

	return current
}

var (
	declaredMutex sync.RWMutex
	declared      = map[string]*jsonschema.Schema{}
)

// Declare records the schema for a command's --json data payload, keyed by the
// command path as `bb` spells it: "tag list", "pr get".
//
// Called from an init in the command's own package, so the declaration sits
// beside the type it describes and happens once per process rather than once
// per constructed command tree.
func Declare(commandPath string, schema *jsonschema.Schema) {
	declaredMutex.Lock()
	defer declaredMutex.Unlock()

	declared[commandPath] = schema
}

// SchemaFor returns the declared schema for a command path.
func SchemaFor(commandPath string) (*jsonschema.Schema, bool) {
	declaredMutex.RLock()
	defer declaredMutex.RUnlock()

	schema, ok := declared[commandPath]

	return schema, ok
}

// DeclaredPaths returns every command path with a declared result, sorted.
//
// Used by the tests that hold the declarations to the command tree: a path that
// resolves to nothing describes a command that does not exist, and a command
// with no declaration is one whose payload nothing has modelled yet.
func DeclaredPaths() []string {
	declaredMutex.RLock()
	defer declaredMutex.RUnlock()

	paths := make([]string, 0, len(declared))
	for path := range declared {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	return paths
}
