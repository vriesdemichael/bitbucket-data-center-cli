package refcmd

import (
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/result"
)

// Refs is what `bb ref list` returns.
type Refs struct {
	Repository result.Repository `json:"repository"`
	Refs       []result.Ref      `json:"refs" jsonschema:"Matching branches and tags. Empty rather than absent when nothing matched."`
}

// Resolution is what `bb ref resolve` returns for a name that exists.
//
// A name that does not resolve is a not_found failure, so this payload never
// carries an absent ref.
type Resolution struct {
	Repository result.Repository `json:"repository"`
	Ref        result.Ref        `json:"ref"`
}

func init() {
	result.Declare("ref list", result.For[Refs](map[string][]string{"refs.type": result.RefTypes}))
	result.Declare("ref resolve", result.For[Resolution](map[string][]string{"ref.type": result.RefTypes}))
}
