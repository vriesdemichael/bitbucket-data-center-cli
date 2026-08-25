// Package bulk embeds the bb-bulk agent skill template so it can be accessed
// from anywhere in the binary without requiring the source tree at runtime.
package bulk

import _ "embed"

// Content holds the embedded SKILL.md bytes for the bulk operations skill.
//
// The file is complete and correct as committed: it contains no placeholders,
// because this same file is what `npx skills add` distributes. Callers that
// render it for a specific binary append a version stamp rather than
// substituting into it.
//
//go:embed SKILL.md
var Content []byte
