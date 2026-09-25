package cli

import (
	"slices"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// The parts every document shares, derived from the types that write them, as
// a command's data schema is derived from its result type (ADR-010): a schema
// written beside the type is a second copy, and the copy is what drifts.
var (
	metaDeclaration = result.For[jsonoutput.EnvelopeMeta](nil)

	errorDeclaration = result.For[jsonoutput.EnvelopeError](map[string][]string{
		"kind": kindNames(apperrors.Kinds()...),
	})

	// Under --dry-run a top-level error means no verdict was reached, so it
	// can only be one of these (ADR-096).
	noVerdictErrorDeclaration = result.For[jsonoutput.EnvelopeError](map[string][]string{
		"kind": kindNames(apperrors.KindTransient, apperrors.KindCancelled, apperrors.KindInternal, apperrors.KindUnknownOutcome),
	})

	previewDeclaration = result.For[jsonoutput.Preview](map[string][]string{
		"tier":            {string(jsonoutput.TierServerValidated), string(jsonoutput.TierPreconditionsChecked), string(jsonoutput.TierPredicted)},
		"effects.action":  {"create", "update", "delete"},
		"effects.outcome": {string(jsonoutput.OutcomeWouldApply), string(jsonoutput.OutcomeNoOp), string(jsonoutput.OutcomeWouldFail)},
		"error.kind":      kindNames(apperrors.Kinds()...),
	})
)

func kindNames(kinds ...apperrors.Kind) []string {
	names := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		names = append(names, string(kind))
	}

	return names
}

// runDocumentSchema is the JSON Schema of the whole document a run writes:
// data and meta, or error and meta when it fails.
func runDocumentSchema(data *jsonschema.Schema) *jsonschema.Schema {
	return &jsonschema.Schema{OneOf: []*jsonschema.Schema{
		documentSchema("data", data),
		documentSchema("error", errorDeclaration.Schema().CloneSchemas()),
	}}
}

// dryRunDocumentSchema is the JSON Schema of the whole document --dry-run
// writes: preview and meta, or, when no verdict was reached, error and meta.
// A command that only reads runs, and its data is in the preview, as is the
// report of a command in dryRunReports; any other has no data there.
func dryRunDocumentSchema(carriesData bool, data *jsonschema.Schema) *jsonschema.Schema {
	preview := previewDeclaration.Schema().CloneSchemas()
	if carriesData {
		preview.Properties["data"] = data
	} else {
		delete(preview.Properties, "data")
		preview.PropertyOrder = slices.DeleteFunc(slices.Clone(preview.PropertyOrder), func(name string) bool { return name == "data" })
	}

	return &jsonschema.Schema{OneOf: []*jsonschema.Schema{
		documentSchema("preview", preview),
		documentSchema("error", noVerdictErrorDeclaration.Schema().CloneSchemas()),
	}}
}

// documentSchema is one shape of document: its member and meta, and nothing
// else, since the set of top-level members is closed (ADR-064).
func documentSchema(member string, schema *jsonschema.Schema) *jsonschema.Schema {
	meta := metaDeclaration.Schema().CloneSchemas()

	// The reflector closes every object it derives. The document is closed,
	// and its closing is taken from there rather than spelled a second way
	// here; meta is not, since it may gain fields in a minor release.
	closed := meta.AdditionalProperties
	meta.AdditionalProperties = nil

	return &jsonschema.Schema{
		Type:                 "object",
		Properties:           map[string]*jsonschema.Schema{member: schema, "meta": meta},
		PropertyOrder:        []string{member, "meta"},
		Required:             []string{member, "meta"},
		AdditionalProperties: closed,
	}
}
