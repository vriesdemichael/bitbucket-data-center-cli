package outputschemas

import (
	"reflect"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/docsite"
	bulkworkflow "github.com/vriesdemichael/bitbucket-data-center-cli/internal/workflows/bulk"
)

// TestTheBulkOutputSchemasAreTheArtifacts is #577's "fill in the three bulk
// placeholder schemas from the committed JSON Schemas". The placeholders
// declared a field the plan never carries and left fields apply and status
// always carry optional; the payload is the artifact, so the schema is too.
func TestTheBulkOutputSchemasAreTheArtifacts(t *testing.T) {
	t.Parallel()

	artifacts := bulkworkflow.SchemasFor(docsite.LatestVersion)
	schemas := Schemas()

	for output, artifact := range map[string]string{
		"output.bulk.plan.schema.json":   "bulk-plan.schema.json",
		"output.bulk.apply.schema.json":  "bulk-apply-status.schema.json",
		"output.bulk.status.schema.json": "bulk-apply-status.schema.json",
	} {
		data, ok := schemas[output]["properties"].(map[string]any)["data"].(map[string]any)
		if !ok {
			t.Fatalf("%s has no data schema", output)
		}

		want := embedded(artifacts[artifact])
		if !reflect.DeepEqual(data, want) {
			t.Errorf("%s does not carry the %s artifact schema as its payload", output, artifact)
		}
		if _, root := data["$schema"]; root {
			t.Errorf("%s embeds $schema, which belongs to a document's root", output)
		}
	}
}
