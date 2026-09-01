package result

import (
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

// Ref is a branch or tag.
//
// Shared rather than declared per package: `bb ref list`, `bb ref resolve`,
// `bb branch default get` and `bb branch model inspect` all answer with the
// same three fields, and before this each of them published its own idea of
// them -- two of them by handing the upstream struct straight to the encoder,
// which is how a payload acquires a contract nobody wrote.
type Ref struct {
	ID        string `json:"id,omitempty" jsonschema:"Full ref name, for example refs/heads/main."`
	DisplayID string `json:"displayId,omitempty" jsonschema:"Short ref name, for example main."`
	Type      string `json:"type,omitempty" jsonschema:"BRANCH or TAG."`
}

// RefTypes is the closed set Bitbucket uses for a minimal ref.
//
// Exported because the enum path differs per payload -- the field sits at a
// different depth in each -- so every caller passes it under its own path.
var RefTypes = []string{"BRANCH", "TAG"}

// RefFrom converts one upstream ref.
func RefFrom(upstream openapigenerated.RestMinimalRef) Ref {
	converted := Ref{
		ID:        stringValue(upstream.Id),
		DisplayID: stringValue(upstream.DisplayId),
	}
	if upstream.Type != nil {
		converted.Type = string(*upstream.Type)
	}

	return converted
}

// RefsFrom converts a list, preserving order and never returning nil.
func RefsFrom(upstream []openapigenerated.RestMinimalRef) []Ref {
	converted := make([]Ref, 0, len(upstream))
	for _, one := range upstream {
		converted = append(converted, RefFrom(one))
	}

	return converted
}
