package refcmd

import (
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/result"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

// Ref is a branch or tag, as `bb ref` reports it.
//
// One type for both commands. `ref list` used to publish the upstream
// RestMinimalRef and `ref resolve` a map built by hand with the same field
// names -- two descriptions of one shape, which is how they come to differ.
type Ref struct {
	ID        string `json:"id,omitempty" jsonschema:"Full ref name, for example refs/heads/main."`
	DisplayID string `json:"displayId,omitempty" jsonschema:"Short ref name, for example main."`
	Type      string `json:"type,omitempty" jsonschema:"BRANCH or TAG."`
}

// Refs is what `bb ref list` returns.
type Refs struct {
	Repository result.Repository `json:"repository"`
	Refs       []Ref             `json:"refs" jsonschema:"Matching branches and tags. Empty rather than absent when nothing matched."`
}

// Resolution is what `bb ref resolve` returns for a name that exists.
//
// A name that does not resolve is a not_found failure, so this payload never
// carries an absent ref.
type Resolution struct {
	Repository result.Repository `json:"repository"`
	Ref        Ref               `json:"ref"`
}

// refTypes is the closed set Bitbucket uses for a minimal ref. The path differs
// per payload because the field sits at a different depth in each.
var refTypes = []string{"BRANCH", "TAG"}

func init() {
	result.Declare("ref list", result.For[Refs](map[string][]string{"refs.type": refTypes}))
	result.Declare("ref resolve", result.For[Resolution](map[string][]string{"ref.type": refTypes}))
}

// refFrom converts one upstream ref.
func refFrom(upstream openapigenerated.RestMinimalRef) Ref {
	converted := Ref{
		ID:        safeString(upstream.Id),
		DisplayID: safeString(upstream.DisplayId),
	}
	if upstream.Type != nil {
		converted.Type = string(*upstream.Type)
	}

	return converted
}

// refsFrom converts a list, preserving order and never returning nil.
func refsFrom(upstream []openapigenerated.RestMinimalRef) []Ref {
	converted := make([]Ref, 0, len(upstream))
	for _, one := range upstream {
		converted = append(converted, refFrom(one))
	}

	return converted
}
