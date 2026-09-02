package outputschemas

import (
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/jsonoutput"
)

// bulkOutputSchemas returns envelope-wrapped output schemas for the bb bulk
// commands.  The data payload is described with the key top-level fields from
// the bulk plan / apply-status artifacts; the full artifact schemas are
// published separately under reference/schemas/bulk-*.schema.json.
func bulkOutputSchemas(_, _ map[string]any) map[string]map[string]any {
	return map[string]map[string]any{
		"output.bulk.plan.schema.json": jsonoutput.EnvelopeSchemaFor(
			"output.bulk.plan.schema.json",
			"bb bulk plan output",
			"JSON output schema for `bb bulk plan --json`. Data is a bulk plan artifact. "+
				"Full artifact schema: reference/schemas/bulk-plan.schema.json.",
			map[string]any{
				"type":                 "object",
				"additionalProperties": true,
				"properties": map[string]any{
					"apiVersion": map[string]any{"type": "string"},
					"kind":       map[string]any{"const": "BulkPlan"},
					"planHash":   map[string]any{"type": "string"},
					"status":     map[string]any{"type": "string"},
					"summary":    map[string]any{"type": "object"},
					"targets":    map[string]any{"type": "array"},
				},
				"required": []any{"apiVersion", "kind", "planHash"},
			},
		),
		"output.bulk.apply.schema.json": jsonoutput.EnvelopeSchemaFor(
			"output.bulk.apply.schema.json",
			"bb bulk apply output",
			"JSON output schema for `bb bulk apply --json`. Data is a bulk apply-status artifact. "+
				"Full artifact schema: reference/schemas/bulk-apply-status.schema.json.",
			bulkApplyStatusDataSchema(),
		),
		"output.bulk.status.schema.json": jsonoutput.EnvelopeSchemaFor(
			"output.bulk.status.schema.json",
			"bb bulk status output",
			"JSON output schema for `bb bulk status --json`. Data is a bulk apply-status artifact. "+
				"Full artifact schema: reference/schemas/bulk-apply-status.schema.json.",
			bulkApplyStatusDataSchema(),
		),
	}
}

func bulkApplyStatusDataSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": true,
		"properties": map[string]any{
			"apiVersion":  map[string]any{"type": "string"},
			"kind":        map[string]any{"const": "BulkApplyStatus"},
			"operationId": map[string]any{"type": "string"},
			"planHash":    map[string]any{"type": "string"},
			"status":      map[string]any{"type": "string", "enum": []any{"success", "failed", "partial_failure", "cancelled"}},
			"summary":     map[string]any{"type": "object"},
			"targets":     map[string]any{"type": "array"},
		},
		"required": []any{"apiVersion", "kind", "operationId", "planHash", "status"},
	}
}
