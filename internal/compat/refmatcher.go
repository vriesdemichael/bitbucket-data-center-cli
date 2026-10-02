package compat

import "strings"

// DefaultBranchMatcher is a ref matcher of type DEFAULT_BRANCH, which matches
// the repository's default branch.
//
// It arrived in 10.2. An earlier release refuses a required build naming it
// with a 400 "The ref matcher provider of type DEFAULT_BRANCH could not be
// found", and fails on a default reviewer condition naming it with a 500
// (observed on every supported release from 9.3.2 to 10.1.5; 10.2.7 stores
// both). bb refuses both before sending.
var DefaultBranchMatcher = Difference{
	What:  "a DEFAULT_BRANCH ref matcher",
	Since: Release{Major: 10, Minor: 2},
}

// NamesDefaultBranchMatcher reports whether any of the named matchers in a
// request body is of type DEFAULT_BRANCH.
func NamesDefaultBranchMatcher(body map[string]any, fields ...string) bool {
	for _, field := range fields {
		matcher, _ := body[field].(map[string]any)
		kind, _ := matcher["type"].(map[string]any)
		if id, _ := kind["id"].(string); strings.EqualFold(strings.TrimSpace(id), "DEFAULT_BRANCH") {
			return true
		}
	}

	return false
}
