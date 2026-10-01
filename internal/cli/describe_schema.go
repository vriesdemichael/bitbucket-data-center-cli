package cli

import (
	"slices"
	"sync"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// The parts every document shares, derived from the types that write them, as
// a command's data schema is derived from its result type (ADR-010): a schema
// written beside the type is a second copy, and the copy is what drifts.
var (
	// The meta and error members are jsonoutput's, which the published failure
	// schema is built from too, so --describe and that schema cannot disagree
	// about them.
	metaSchema = sync.OnceValue(jsonoutput.MetaSchema)

	errorSchema = sync.OnceValue(func() *jsonschema.Schema {
		return jsonoutput.ErrorSchema(apperrors.Kinds()...)
	})

	// Under --dry-run a top-level error means no verdict was reached, so it
	// can only be one of these (ADR-096).
	noVerdictErrorSchema = sync.OnceValue(func() *jsonschema.Schema {
		return jsonoutput.ErrorSchema(apperrors.KindTransient, apperrors.KindCancelled, apperrors.KindInternal, apperrors.KindUnknownOutcome)
	})

	previewDeclaration = result.For[jsonoutput.Preview](map[string][]string{
		"tier":            {string(jsonoutput.TierServerValidated), string(jsonoutput.TierPreconditionsChecked), string(jsonoutput.TierPredicted)},
		"effects.action":  {"create", "update", "delete"},
		"effects.outcome": {string(jsonoutput.OutcomeWouldApply), string(jsonoutput.OutcomeNoOp), string(jsonoutput.OutcomeWouldFail)},
	})
)

// runDocumentSchema is the JSON Schema of the whole document a run writes:
// data and meta, or error and meta when it fails.
func runDocumentSchema(data *jsonschema.Schema) *jsonschema.Schema {
	return &jsonschema.Schema{OneOf: []*jsonschema.Schema{
		documentSchema("data", data),
		documentSchema("error", errorSchema().CloneSchemas()),
	}}
}

// dryRunDocumentSchema is the JSON Schema of the whole document --dry-run
// writes: preview and meta, or, when no verdict was reached, error and meta.
// A command that only reads runs, and its data is in the preview, as is the
// report of a command in dryRunReports; any other has no data there.
func dryRunDocumentSchema(carriesData bool, data *jsonschema.Schema) *jsonschema.Schema {
	preview := previewDeclaration.Schema().CloneSchemas()

	// The verdict's error is the same member a failed run carries, under the
	// preview's own description of it.
	verdict := errorSchema().CloneSchemas()
	verdict.Description = preview.Properties["error"].Description
	preview.Properties["error"] = verdict

	if carriesData {
		preview.Properties["data"] = data
	} else {
		delete(preview.Properties, "data")
		preview.PropertyOrder = slices.DeleteFunc(slices.Clone(preview.PropertyOrder), func(name string) bool { return name == "data" })
	}

	return &jsonschema.Schema{OneOf: []*jsonschema.Schema{
		documentSchema("preview", preview),
		documentSchema("error", noVerdictErrorSchema().CloneSchemas()),
	}}
}

// documentSchema is one shape of document: its member and meta, and nothing
// else, since the set of top-level members is closed (ADR-064).
func documentSchema(member string, schema *jsonschema.Schema) *jsonschema.Schema {
	return &jsonschema.Schema{
		Type:          "object",
		Properties:    map[string]*jsonschema.Schema{member: schema, "meta": metaSchema().CloneSchemas()},
		PropertyOrder: []string{member, "meta"},
		Required:      []string{member, "meta"},
		// The false schema, as jsonschema-go spells it.
		AdditionalProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}},
	}
}
