package result

import (
	"encoding/json"
	"strings"
	"testing"
)

type sample struct {
	Name            string   `json:"name"`
	Kind            string   `json:"kind,omitempty"`
	OptionalPointer *int     `json:"optionalPointer,omitempty"`
	RequiredPointer *int     `json:"requiredPointer"`
	Nested          nested   `json:"nested,omitzero"`
	Items           []nested `json:"items,omitempty"`
}

type nested struct {
	Flavour string `json:"flavour,omitempty"`
}

// TestOptionalPointersAreNotPublishedAsNullable is the rule that makes a
// derived schema true rather than merely derived.
//
// The reflector types a Go pointer as null-or-something. With omitempty,
// encoding/json omits a nil pointer instead of writing null -- so publishing
// the union would describe a value the command cannot emit, and a consumer
// would write a branch that never runs. A pointer without omitempty does
// marshal to null, and keeps it.
func TestOptionalPointersAreNotPublishedAsNullable(t *testing.T) {
	t.Parallel()

	schema := For[sample](nil)

	if got := schema.Properties["optionalPointer"].Type; got != "integer" {
		t.Errorf("optionalPointer type = %q, want integer without null", got)
	}
	if types := schema.Properties["requiredPointer"].Types; len(types) != 2 {
		t.Errorf("requiredPointer types = %v, want the null union kept", types)
	}
}

// TestOnlyNonOptionalFieldsAreRequired pins the other half: a schema that
// required everything would fail on the first payload that legitimately omits
// something.
func TestOnlyNonOptionalFieldsAreRequired(t *testing.T) {
	t.Parallel()

	schema := For[sample](nil)

	required := strings.Join(schema.Required, ",")
	if !strings.Contains(required, "name") {
		t.Errorf("required = %v, want name", schema.Required)
	}
	for _, optional := range []string{"kind", "optionalPointer", "nested", "items"} {
		if strings.Contains(required, optional) {
			t.Errorf("%q is optional but required = %v", optional, schema.Required)
		}
	}
}

// TestListIsAnArrayNotANullableOne covers the list helper.
//
// For[[]T] types the payload null-or-array, because a Go slice can be nil. A
// list command builds with make and emits [] when empty, so array is the
// narrower and true claim -- and the one worth holding, since a command that
// starts returning null breaks every caller iterating the result.
func TestListIsAnArrayNotANullableOne(t *testing.T) {
	t.Parallel()

	schema := List[nested](nil)

	if schema.Type != "array" || len(schema.Types) != 0 {
		t.Errorf("list type = %q / %v, want a plain array", schema.Type, schema.Types)
	}
	if schema.Items == nil {
		t.Fatal("list schema has no items")
	}
	if _, ok := schema.Items.Properties["flavour"]; !ok {
		t.Error("list items do not describe the element type")
	}
}

// TestEnumsReachNestedAndListedFields covers the dotted path.
//
// A flat lookup only ever reaches the shallowest payload, and nesting is the
// normal case: most payloads wrap their subject in a repository and a list.
func TestEnumsReachNestedAndListedFields(t *testing.T) {
	t.Parallel()

	schema := For[sample](map[string][]string{
		"nested.flavour": {"sweet", "sour"},
		"items.flavour":  {"salt"},
	})

	if got := schema.Properties["nested"].Properties["flavour"].Enum; len(got) != 2 {
		t.Errorf("nested enum = %v, want two values", got)
	}
	if got := schema.Properties["items"].Items.Properties["flavour"].Enum; len(got) != 1 {
		t.Errorf("listed enum = %v, want one value", got)
	}
}

// TestAnEnumForAMissingFieldIsAWiringError is the sabotage, kept as a test
// (ADR-067). Silently doing nothing would leave a field documented as open
// when the command only ever emits three values.
func TestAnEnumForAMissingFieldIsAWiringError(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Error("declaring an enum for a field that does not exist was accepted")
		}
	}()

	_ = For[sample](map[string][]string{"nested.nothing": {"x"}})
}

// TestDeclarationsRoundTripThroughJSON guards the path --describe actually
// takes: the schema is marshalled before a caller ever sees it.
func TestDeclarationsRoundTripThroughJSON(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(For[sample](nil))
	if err != nil {
		t.Fatalf("encoding failed: %v", err)
	}

	var document map[string]any
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatalf("decoding failed: %v", err)
	}
	if document["properties"] == nil {
		t.Errorf("the encoded schema has no properties: %s", encoded)
	}
}
