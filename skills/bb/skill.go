// Package skill embeds the bb agent skill template so it can be accessed
// from anywhere in the binary without requiring the source tree at runtime.
package skill

import _ "embed"

// Content holds the embedded SKILL.md bytes.
//
// The file is complete and correct as committed: it contains no placeholders,
// because this same file is what `npx skills add` distributes. Callers that
// render it for a specific binary append a version stamp rather than
// substituting into it.
//
//go:embed SKILL.md
var Content []byte
