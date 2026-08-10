// Package outputschemas defines and exports JSON schemas for each bb command's
// --json output.  Each schema describes the full bb.machine v2 envelope
// (version, data, meta fields) that the command emits to stdout.
//
// Schema is organized by command group.  A central Schemas() function merges
// all group schemas and is consumed by the output-schema-export tool.
package outputschemas

import (
	authschemas "github.com/vriesdemichael/bitbucket-server-cli/internal/cli/cmd/auth"
	bulkworkflow "github.com/vriesdemichael/bitbucket-server-cli/internal/workflows/bulk"
)

// Schemas returns all per-command output JSON schemas keyed by their published
// file name.  The tool tools/output-schema-export writes them to disk.
func Schemas() map[string]map[string]any {
	all := make(map[string]map[string]any)

	// Auth command group schemas
	for k, v := range authschemas.Schemas() {
		all[k] = v
	}

	// Tag command group schemas
	for k, v := range tagSchemas() {
		all[k] = v
	}

	// Repo command group schemas
	for k, v := range repoSchemas() {
		all[k] = v
	}

	// Commit command group schemas
	for k, v := range commitSchemas() {
		all[k] = v
	}

	// Branch command group schemas (subset of branch_build_commands.go)
	for k, v := range branchSchemas() {
		all[k] = v
	}

	// Pull request command group schemas
	for k, v := range pullRequestSchemas() {
		all[k] = v
	}

	for k, v := range updateSchemas() {
		all[k] = v
	}

	// Bulk command group — envelope-wrapped versions of the existing bulk schemas
	for k, v := range bulkOutputSchemas(bulkworkflow.PlanJSONSchema(), bulkworkflow.ApplyStatusJSONSchema()) {
		all[k] = v
	}

	return all
}
