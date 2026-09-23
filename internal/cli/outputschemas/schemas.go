// Package outputschemas holds what the --json output contract says about every
// command at once: the failure envelope, which is the same for all of them, and
// the lists of commands that are exempt from a data contract, each with the
// reason.
//
// A command's own payload schema is not here. It is derived from the result
// type the command fills in (internal/cli/result), and --describe serves it.
//
// These schemas are a published contract, and ADR-064 is the record that
// governs them. Read it before changing one.
//
// Adding a field to data is additive and needs no ceremony. Removing a field,
// renaming one, changing its type, or changing whether it can be null is a
// BREAKING CHANGE: mark the commit with a ! or a BREAKING CHANGE footer so the
// release automation cuts a major. The release version is the only
// compatibility signal consumers have, so an unmarked break reaches them
// silently through package managers and bb update.
package outputschemas

import (
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/docsite"
)

// Schemas returns the published output JSON schemas keyed by their published
// file name, identified against the "latest" alias.
func Schemas() map[string]map[string]any {
	return SchemasFor(docsite.LatestVersion)
}

// SchemasFor returns the same schemas, each claiming the identity it has when
// published under siteVersion.
func SchemasFor(siteVersion string) map[string]map[string]any {
	all := make(map[string]map[string]any)

	// Failure envelope — one schema for every command, since the shape of a
	// failure does not vary by command.
	all[ErrorSchemaFileName] = jsonoutput.ErrorEnvelopeSchema(ErrorSchemaFileName)

	// Stamped once, here, rather than threaded through every builder: a
	// schema is identified by where it is published, and the map key is that
	// location.
	for name, schema := range all {
		schema["$id"] = jsonoutput.SchemaID(siteVersion, name)
	}

	return all
}

// ErrorSchemaFileName is the published name of the failure envelope schema.
const ErrorSchemaFileName = "output.error.schema.json"
