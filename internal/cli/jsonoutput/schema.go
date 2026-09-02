package jsonoutput

import (
	"sort"

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
	kinds := make([]any, 0, len(apperrors.Kinds()))
	exitCodes := map[int]struct{}{}
	for _, kind := range apperrors.Kinds() {
		kinds = append(kinds, string(kind))
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

	return map[string]any{
		"$schema":              jsonSchemaVersion,
		"$id":                  SchemaID(docsite.LatestVersion, schemaFileName),
		"title":                "bb command failure",
		"description":          "Emitted on stdout by any bb command that fails while --json is set. The presence of error rather than data marks the run as failed; exitCode matches the process exit status.",
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"error": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"kind": map[string]any{
						"description": "Error classification from the ADR-011 taxonomy.",
						"enum":        kinds,
					},
					"message": map[string]any{
						"type":        "string",
						"description": "Human-readable failure description, without the kind prefix shown on stderr.",
					},
					"exitCode": map[string]any{
						"description": "Process exit status, determined by kind.",
						"type":        "integer",
						"enum":        codeValues,
					},
					"details": map[string]any{
						"description": "Machine-readable handles the caller needs to act on the failure, keyed by name. Absent when there are none. bb bulk apply sets operationId, which `bb bulk status <id>` takes.",
						"type":        "object",
						"additionalProperties": map[string]any{
							"type":      "string",
							"minLength": 1,
						},
						"minProperties": 1,
					},
				},
				"required": []any{"kind", "message", "exitCode"},
			},
			"meta": metaSchema(),
		},
		"required": []any{"error", "meta"},
	}
}

func metaSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"bbVersion": map[string]any{
				"type":        "string",
				"minLength":   1,
				"description": "Version of the bb binary that produced this document. Provenance for stored output, not a compatibility switch: pin the binary to pin the contract (ADR-064).",
			},
			"limitReached": map[string]any{
				"type":        "boolean",
				"description": "Present on listing commands: true when the result set came back at --limit and there may be more behind it.",
			},
		},
		"required": []any{"bbVersion"},
	}
}
