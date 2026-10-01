package jsonoutput

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/docsite"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

const jsonSchemaVersion = "https://json-schema.org/draft/2020-12/schema"

// SchemaBaseURL is the directory under which output schemas are published for
// the given version of the documentation site.
func SchemaBaseURL(siteVersion string) string {
	return docsite.URL(siteVersion, "reference/schemas/output/")
}

// SchemaID is the canonical identity a published output schema claims.
func SchemaID(siteVersion, schemaFileName string) string {
	return SchemaBaseURL(siteVersion) + schemaFileName
}

// ErrorEnvelopeSchema describes the envelope written to stdout when any command
// fails under --json.
//
// It is published once rather than per command: the failure shape does not vary
// by command, and a consumer that validates against it can handle an error from
// a command it has never seen.
func ErrorEnvelopeSchema(schemaFileName string) map[string]any {
	return map[string]any{
		"$schema":              jsonSchemaVersion,
		"$id":                  SchemaID(docsite.LatestVersion, schemaFileName),
		"title":                "bb command failure",
		"description":          "Emitted on stdout by any bb command that fails while --json is set. The presence of error rather than data marks the run as failed; exitCode matches the process exit status.",
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"error": schemaValue(ErrorSchema(apperrors.Kinds()...)),
			"meta":  schemaValue(MetaSchema()),
		},
		"required": []any{"error", "meta"},
	}
}

// ErrorSchema is the JSON Schema of the error member of a failed run's
// document, for a failure of one of kinds.
//
// It is derived from EnvelopeError, as every part of a document --describe
// serves is derived from the type that writes it (ADR-010), and then narrowed
// by what a Go type cannot say: kind is one of kinds, exitCode one of the codes
// they map to, and details, when present, holds at least one value and none
// empty. The published failure schema and --describe both take the member from
// here, so they cannot describe it two ways.
func ErrorSchema(kinds ...apperrors.Kind) *jsonschema.Schema {
	schema, err := jsonschema.For[EnvelopeError](nil)
	if err != nil {
		panic(fmt.Sprintf("deriving the error schema from EnvelopeError: %v", err))
	}

	names := make([]any, 0, len(kinds))
	exitCodes := map[int]struct{}{}
	for _, kind := range kinds {
		names = append(names, string(kind))
		exitCodes[apperrors.ExitCode(apperrors.New(kind, "", nil))] = struct{}{}
	}

	codes := make([]int, 0, len(exitCodes))
	for code := range exitCodes {
		codes = append(codes, code)
	}
	sort.Ints(codes)

	codeValues := make([]any, 0, len(codes))
	for _, code := range codes {
		codeValues = append(codeValues, code)
	}

	schema.Properties["kind"].Enum = names
	schema.Properties["exitCode"].Enum = codeValues

	// EnvelopeErrorOf omits an empty map, and a detail is a handle to act on,
	// which an empty string is not.
	nonEmpty := 1
	details := schema.Properties["details"]
	details.MinProperties = &nonEmpty
	details.AdditionalProperties.MinLength = &nonEmpty

	return schema
}

// schemaValue is a derived schema as the plain JSON value a published schema
// is built from, so it is written with its keys in the order the rest of the
// document has.
func schemaValue(schema *jsonschema.Schema) map[string]any {
	encoded, err := json.Marshal(schema)
	if err != nil {
		panic(fmt.Sprintf("encoding a derived schema: %v", err))
	}

	value := map[string]any{}
	if err := json.Unmarshal(encoded, &value); err != nil {
		panic(fmt.Sprintf("decoding a derived schema: %v", err))
	}

	return value
}

// MetaSchema is the JSON Schema of the meta member every document carries.
//
// It is derived from EnvelopeMeta, as ErrorSchema derives the error member,
// and then narrowed by what a Go type cannot say: bbVersion is never empty,
// encoding is only ever base64, and limitReached, a pointer so that false is
// told from absent, is omitted rather than written as null. The published
// failure schema and --describe both take the member from here, so they cannot
// describe it two ways.
//
// It is open, unlike the document around it. meta is provenance and expected
// to grow: a field added to it lands in a minor release only if a document
// carrying a field this schema does not name still validates. The set of
// top-level members stays closed; only what sits inside meta may widen.
func MetaSchema() *jsonschema.Schema {
	schema, err := jsonschema.For[EnvelopeMeta](nil)
	if err != nil {
		panic(fmt.Sprintf("deriving the meta schema from EnvelopeMeta: %v", err))
	}

	nonEmpty := 1
	schema.Properties["bbVersion"].MinLength = &nonEmpty
	schema.Properties["encoding"].Enum = []any{"base64"}

	limitReached := schema.Properties["limitReached"]
	limitReached.Type, limitReached.Types = "boolean", nil

	// The empty schema, which jsonschema-go writes as true.
	schema.AdditionalProperties = &jsonschema.Schema{}

	return schema
}
