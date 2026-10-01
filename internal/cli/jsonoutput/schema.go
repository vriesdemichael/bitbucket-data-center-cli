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

// EnvelopeSchemaFor builds a full bb.machine envelope schema whose data
// field is constrained to the supplied dataSchema.  title and description are
// shown in documentation tooling.
func EnvelopeSchemaFor(schemaFileName, title, description string, dataSchema map[string]any) map[string]any {
	return map[string]any{
		"$schema":              jsonSchemaVersion,
		"$id":                  SchemaID(docsite.LatestVersion, schemaFileName),
		"title":                title,
		"description":          description,
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"data": dataSchema,
			"meta": metaSchema(),
		},
		"required": []any{"data", "meta"},
	}
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
			"meta":  metaSchema(),
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

func metaSchema() map[string]any {
	return map[string]any{
		"type": "object",
		// Open, unlike the envelope around it. meta is provenance, and it is
		// expected to grow: a field added to it lands in a minor release only
		// if a document carrying a field this schema does not name still
		// validates. Closed, the first published version of this schema would
		// have turned every later meta field into a breaking change for anyone
		// validating strictly. The set of top-level members stays closed; only
		// what sits inside meta may widen.
		"additionalProperties": true,
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "The command that wrote this document, by its canonical path (bb pr view reports pr get), so a document held on its own says which --describe describes it. Absent when no command resolved.",
			},
			"bbVersion": map[string]any{
				"type":        "string",
				"minLength":   1,
				"description": "Version of the bb binary that produced this document. Provenance for stored output, not a compatibility switch: pin the binary to pin the contract (ADR-064).",
			},
			"limitReached": map[string]any{
				"type":        "boolean",
				"description": "Present on listing commands: true when the result set came back at --limit and there may be more behind it.",
			},
			"encoding": map[string]any{
				"type":        "string",
				"enum":        []any{"base64"},
				"description": "Present when data is a body that is not text, carried as a string in this encoding.",
			},
			"contentType": map[string]any{
				"type":        "string",
				"description": "Present with encoding: the media type of the body data carries.",
			},
		},
		"required": []any{"bbVersion"},
	}
}
