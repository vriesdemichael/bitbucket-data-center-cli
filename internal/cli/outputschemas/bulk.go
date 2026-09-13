package outputschemas

import (
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
)

// bulkOutputSchemas returns envelope-wrapped output schemas for the bb bulk
// commands, with the published artifact schema as the data payload.
//
// They were placeholders: a few top-level fields, typed as bare objects and
// arrays, beside an artifact schema that already described the same payload
// in full. The two disagreed -- the plan's declared a field it never carries,
// apply and status left two it always carries optional -- and #577 asked for
// the placeholders to be filled from the artifacts. So they are the artifacts.
//
// Each keeps its own $id, which makes it a schema resource inside the envelope:
// its references into $defs resolve against it wherever it is embedded, and
// --describe, which hands back the data schema alone, hands back something
// complete.
func bulkOutputSchemas(planSchema, applyStatusSchema map[string]any) map[string]map[string]any {
	return map[string]map[string]any{
		"output.bulk.plan.schema.json": jsonoutput.EnvelopeSchemaFor(
			"output.bulk.plan.schema.json",
			"bb bulk plan output",
			"JSON output schema for `bb bulk plan --json`. Data is a bulk plan artifact, "+
				"described by reference/schemas/bulk-plan.schema.json.",
			embedded(planSchema),
		),
		"output.bulk.apply.schema.json": jsonoutput.EnvelopeSchemaFor(
			"output.bulk.apply.schema.json",
			"bb bulk apply output",
			"JSON output schema for `bb bulk apply --json`. Data is a bulk apply-status artifact, "+
				"described by reference/schemas/bulk-apply-status.schema.json.",
			embedded(applyStatusSchema),
		),
		"output.bulk.status.schema.json": jsonoutput.EnvelopeSchemaFor(
			"output.bulk.status.schema.json",
			"bb bulk status output",
			"JSON output schema for `bb bulk status --json`. Data is a bulk apply-status artifact, "+
				"described by reference/schemas/bulk-apply-status.schema.json.",
			embedded(applyStatusSchema),
		),
	}
}

// embedded is an artifact schema as a resource inside another document:
// $schema belongs to a document's root, and everything else, $id and $defs
// with it, is what lets it resolve.
func embedded(artifact map[string]any) map[string]any {
	resource := make(map[string]any, len(artifact))
	for key, value := range artifact {
		if key != "$schema" {
			resource[key] = value
		}
	}

	return resource
}
