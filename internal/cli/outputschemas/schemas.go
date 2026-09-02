// Package outputschemas defines and exports JSON schemas for each bb command's
// --json output. Each schema describes the full bb.machine envelope (data and
// meta) that the command emits to stdout.
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
//
// If the command you are changing has no schema here, add one in the same
// change. A schema diff cannot see a payload it does not have, so an
// unschema'd command has no guarantee at all.
//
// Schemas are organized by command group. Schemas() merges all group schemas
// and is consumed by --describe.
package outputschemas

import (
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/docsite"
	bulkworkflow "github.com/vriesdemichael/bitbucket-server-cli/internal/workflows/bulk"
)

// Schemas returns all per-command output JSON schemas keyed by their published
// file name, identified against the "latest" alias.
func Schemas() map[string]map[string]any {
	return SchemasFor(docsite.LatestVersion)
}

// SchemasFor returns the same schemas, each claiming the identity it has when
// published under siteVersion.
func SchemasFor(siteVersion string) map[string]map[string]any {
	all := make(map[string]map[string]any)

	// Bulk command group — envelope-wrapped versions of the existing bulk schemas
	for k, v := range bulkOutputSchemas(bulkworkflow.PlanJSONSchema(), bulkworkflow.ApplyStatusJSONSchema()) {
		all[k] = v
	}

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
