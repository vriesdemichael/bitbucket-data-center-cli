package tagcmd

import (
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// Tag is one repository tag, as `bb` reports it.
//
// A presentation model, not the wire model. The commands used to hand
// openapigenerated.RestTag straight to the JSON writer, which published the
// upstream struct as bb's own contract: every field a *string, so a derived
// schema says each one may be null -- while encoding/json with omitempty omits
// a nil pointer and never writes null. The published shape would have promised
// something the command cannot emit.
//
// Field names are kept exactly as the payload already spelled them. Renaming
// displayId to name, or latestCommit to commit, is a defensible change and a
// separate one; doing it here would hide a contract change inside a refactor.
type Tag struct {
	ID              string `json:"id,omitempty" jsonschema:"Full tag ref name, for example refs/tags/v1.0.0."`
	DisplayID       string `json:"displayId,omitempty" jsonschema:"Short tag name, for example v1.0.0."`
	Type            string `json:"type,omitempty" jsonschema:"TAG for a lightweight tag, ANNOTATED_TAG for one carrying its own object."`
	LatestCommit    string `json:"latestCommit,omitempty" jsonschema:"SHA1 of the commit the tag points at."`
	LatestChangeset string `json:"latestChangeset,omitempty" jsonschema:"SHA1 of the tagged changeset. Bitbucket reports it alongside latestCommit and the two agree."`
	Hash            string `json:"hash,omitempty" jsonschema:"SHA1 of the tag object itself. Present only for annotated tags."`
}

// Deletion is what `bb tag delete` reports.
//
// It used to be map[string]any{"status": "ok", "tag": name} written at the call
// site, which is a shape nothing named and nothing checked.
type Deletion struct {
	Status string `json:"status" jsonschema:"Always ok when the command succeeds; a failure reports an error envelope instead."`
	Tag    string `json:"tag" jsonschema:"Short name of the tag that was deleted."`
}

// tagTypes is the closed set Bitbucket uses for Tag.Type.
//
// Applied after derivation because Go has no enum: to the reflector the field
// is a string, and which strings it takes is knowledge this package holds.
var tagTypes = map[string][]string{"type": {"TAG", "ANNOTATED_TAG"}}

func init() {
	result.Declare("tag list", result.List[Tag](tagTypes))
	result.Declare("tag view", result.For[Tag](tagTypes))
	result.Declare("tag create", result.For[Tag](tagTypes))
	result.Declare("tag delete", result.For[Deletion](nil))
}

// tagFrom converts the upstream struct into the reported shape.
//
// The nil-pointer dereferences live here, once per field, rather than at each
// render site -- which is what safeString was doing at every call.
func tagFrom(upstream openapigenerated.RestTag) Tag {
	converted := Tag{
		ID:              safeString(upstream.Id),
		DisplayID:       safeString(upstream.DisplayId),
		LatestCommit:    safeString(upstream.LatestCommit),
		LatestChangeset: safeString(upstream.LatestChangeset),
		Hash:            safeString(upstream.Hash),
	}
	if upstream.Type != nil {
		converted.Type = string(*upstream.Type)
	}

	return converted
}

// tagsFrom converts a list, preserving order.
func tagsFrom(upstream []openapigenerated.RestTag) []Tag {
	converted := make([]Tag, 0, len(upstream))
	for _, one := range upstream {
		converted = append(converted, tagFrom(one))
	}

	return converted
}
