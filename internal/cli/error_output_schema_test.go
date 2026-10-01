package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/outputschemas"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// TestErrorEnvelopeMatchesPublishedSchema validates a real emitted failure
// envelope for every kind in the taxonomy against the schema the project
// publishes.
//
// The schema derives its enums from the same taxonomy, so this catches the case
// the derivation cannot: a change to the envelope struct or to how a kind is
// rendered that leaves the CLI emitting a document failing its own contract.
func TestErrorEnvelopeMatchesPublishedSchema(t *testing.T) {
	t.Parallel()

	for _, kind := range apperrors.Kinds() {
		t.Run(string(kind), func(t *testing.T) {
			buffer := &bytes.Buffer{}
			err := apperrors.New(kind, "something went wrong with <host>", nil)
			if writeErr := jsonoutput.WriteError(buffer, err); writeErr != nil {
				t.Fatalf("WriteError returned %v", writeErr)
			}

			validateAgainstOutputSchema(t, outputschemas.ErrorSchemaFileName, buffer.String())
		})
	}
}

// TestErrorEnvelopeMatchesPublishedSchemaForPlainErrors covers the fallback
// path, where an unclassified error still has to produce a valid document.
func TestErrorEnvelopeMatchesPublishedSchemaForPlainErrors(t *testing.T) {
	t.Parallel()

	buffer := &bytes.Buffer{}
	if err := jsonoutput.WriteError(buffer, errUnclassified); err != nil {
		t.Fatalf("WriteError returned %v", err)
	}

	validateAgainstOutputSchema(t, outputschemas.ErrorSchemaFileName, buffer.String())
}

var errUnclassified = errors.New("upstream returned an unexpected response")

// TestThePublishedFailureSchemaAndDescribeAgreeOnEveryMember: error and meta
// are the same members in the published failure schema as in what --describe
// gives, so the two cannot describe one document two ways (ADR-010).
func TestThePublishedFailureSchemaAndDescribeAgreeOnEveryMember(t *testing.T) {
	published := schemaJSON(t, outputschemas.Schemas()[outputschemas.ErrorSchemaFileName])["properties"].(map[string]any)

	description := schemaJSON(t, DescribeCommand("tag list"))
	for _, mode := range []string{"run", "dryRun"} {
		for index, branch := range schemaAt(t, description, mode, "outputSchema")["oneOf"].([]any) {
			members := branch.(map[string]any)["properties"].(map[string]any)
			if !reflect.DeepEqual(members["meta"], published["meta"]) {
				t.Errorf("%s shape %d: meta is not the published one\n--describe: %v\npublished:  %v", mode, index, members["meta"], published["meta"])
			}
		}
	}

	// Under --dry-run a top-level error is narrowed to the kinds that reach no
	// verdict, so only a run's error is the whole taxonomy the published one is.
	runError := schemaAt(t, description, "run", "outputSchema")["oneOf"].([]any)[1].(map[string]any)["properties"].(map[string]any)["error"]
	if !reflect.DeepEqual(runError, published["error"]) {
		t.Errorf("error is not the published one\n--describe: %v\npublished:  %v", runError, published["error"])
	}
}

// validateAgainstOutputSchema compiles a published output schema and validates
// a real document against it.
//
// The failure schema is the one published output schema: it is one document for
// every command, its members derived from the types that write them. Data
// payloads are validated against the schema their command declares -- see
// validateAgainstDeclaredSchema.
func validateAgainstOutputSchema(t *testing.T, schemaName string, output string) {
	t.Helper()

	schemaMap, ok := outputschemas.Schemas()[schemaName]
	if !ok {
		t.Fatalf("no published schema named %q", schemaName)
	}

	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(schemaName, schemaMap); err != nil {
		t.Fatalf("add schema resource: %v", err)
	}
	schema, err := compiler.Compile(schemaName)
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}

	var decoded any
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("decode command output: %v\noutput: %s", err, output)
	}

	if err := schema.Validate(decoded); err != nil {
		t.Fatalf("%s output does not match its published schema: %v\noutput: %s", schemaName, err, output)
	}
}
