package tagcmd

import (
	"encoding/json"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// validate compiles a declared schema and checks a real payload against it.
//
// This is where output validation belongs: at build time, against a value the
// command actually produces. Doing it at run time would only ever catch a bug
// in the derivation, and would do it after the command had already acted --
// turning a documentation defect into a failed run (ADR-010 scopes validation
// to input boundaries for the same reason).
func validate(t *testing.T, commandPath string, payload any) {
	t.Helper()

	schema, ok := result.SchemaFor(commandPath)
	if !ok {
		t.Fatalf("no schema declared for %q", commandPath)
	}

	rawSchema, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("encoding the schema failed: %v", err)
	}
	var schemaDocument any
	if err := json.Unmarshal(rawSchema, &schemaDocument); err != nil {
		t.Fatalf("decoding the schema failed: %v", err)
	}

	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("schema.json", schemaDocument); err != nil {
		t.Fatalf("adding the schema failed: %v", err)
	}
	compiled, err := compiler.Compile("schema.json")
	if err != nil {
		t.Fatalf("compiling the schema failed: %v", err)
	}

	rawPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encoding the payload failed: %v", err)
	}
	var payloadDocument any
	if err := json.Unmarshal(rawPayload, &payloadDocument); err != nil {
		t.Fatalf("decoding the payload failed: %v", err)
	}

	if err := compiled.Validate(payloadDocument); err != nil {
		t.Errorf("%s payload fails its own declared schema: %v\n%s", commandPath, err, rawPayload)
	}
}

// TestEveryTagPayloadMatchesItsDeclaredSchema is the check that replaces
// runtime validation.
//
// Both halves matter. A schema derived from the type cannot describe a shape
// the type cannot hold, but it can still be wrong about what the command
// *builds*: an enum that omits a value, a field the converter never fills, a
// list that turns out nil.
func TestEveryTagPayloadMatchesItsDeclaredSchema(t *testing.T) {
	t.Parallel()

	annotated := openapigenerated.RestTagType("ANNOTATED_TAG")
	lightweight := openapigenerated.RestTagType("TAG")
	name := "v1.0.0"
	ref := "refs/tags/v1.0.0"
	commit := "0123456789abcdef0123456789abcdef01234567"
	hash := "fedcba9876543210fedcba9876543210fedcba98"

	full := openapigenerated.RestTag{
		Id: &ref, DisplayId: &name, Type: &annotated,
		LatestCommit: &commit, LatestChangeset: &commit, Hash: &hash,
	}
	minimal := openapigenerated.RestTag{DisplayId: &name, Type: &lightweight}

	validate(t, "tag view", tagFrom(full))
	validate(t, "tag view", tagFrom(minimal))
	validate(t, "tag create", tagFrom(full))
	validate(t, "tag list", tagsFrom([]openapigenerated.RestTag{full, minimal}))
	validate(t, "tag delete", Deletion{Status: "ok", Tag: name})

	// An empty listing is the case the schema promises most about: it must be
	// [] and not null, or every caller iterating the result breaks.
	empty := tagsFrom(nil)
	if empty == nil {
		t.Fatal("an empty tag list is nil, so it marshals to null rather than []")
	}
	validate(t, "tag list", empty)
}

// TestAnUnmodelledFieldIsCaught is the sabotage, kept as a test (ADR-067).
//
// The schema forbids additional properties, so a payload carrying a field the
// model does not declare must fail. Without that, the derived schema would be
// advice rather than a contract.
func TestAnUnmodelledFieldIsCaught(t *testing.T) {
	t.Parallel()

	schema, _ := result.SchemaFor("tag view")
	rawSchema, _ := json.Marshal(schema)
	var schemaDocument any
	_ = json.Unmarshal(rawSchema, &schemaDocument)

	compiler := jsonschema.NewCompiler()
	_ = compiler.AddResource("schema.json", schemaDocument)
	compiled, err := compiler.Compile("schema.json")
	if err != nil {
		t.Fatalf("compiling failed: %v", err)
	}

	if err := compiled.Validate(map[string]any{"displayId": "v1", "surprise": true}); err == nil {
		t.Error("a payload with an undeclared field passed; the schema is not a contract")
	}
	if err := compiled.Validate(map[string]any{"type": "NOT_A_TAG_TYPE"}); err == nil {
		t.Error("a payload with an unknown tag type passed; the enum is not enforced")
	}
}
