package compat

import (
	"strings"

	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// NoCreatesRestriction is the no-creates branch restriction, which stops a
// matching branch being created.
//
// It arrived in 9.4. An earlier release refuses the type with a 400 "Invalid
// type", in a restriction and in a listing filtered by it (observed on 9.2.1
// and 9.3.2). A restriction is refused before it is sent; a listing filtered
// by the type answers with none, which is what such a release holds.
var NoCreatesRestriction = Difference{
	What:  "a no-creates branch restriction",
	Since: Release{Major: 9, Minor: 4},
}

// IsNoCreates reports whether a restriction type is no-creates.
func IsNoCreates(restrictionType string) bool {
	return strings.EqualFold(strings.TrimSpace(restrictionType), "no-creates")
}

// OnlyNoCreates is the no-creates restrictions among these, which is how a
// listing a release would not filter is narrowed after the fact. On a release
// without the type there are none, and saying so this way leaves the request
// itself -- and what the scope answers to it -- alone.
func OnlyNoCreates(restrictions []openapigenerated.RestRefRestriction) []openapigenerated.RestRefRestriction {
	narrowed := make([]openapigenerated.RestRefRestriction, 0, len(restrictions))
	for _, restriction := range restrictions {
		if restriction.Type != nil && IsNoCreates(*restriction.Type) {
			narrowed = append(narrowed, restriction)
		}
	}

	return narrowed
}
