package outputschemas

import (
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/jsonoutput"
)

// restBranchSchema returns a partial JSON schema for a single RestBranch.
func restBranchSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": true,
		"properties": map[string]any{
			"id":              map[string]any{"type": "string", "description": "Full branch ref name (e.g. refs/heads/main)."},
			"displayId":       map[string]any{"type": "string", "description": "Short branch name (e.g. main)."},
			"type":            map[string]any{"type": "string"},
			"latestCommit":    map[string]any{"type": "string"},
			"latestChangeset": map[string]any{"type": "string"},
			"default":         map[string]any{"type": "boolean"},
		},
	}
}

func branchSchemas() map[string]map[string]any {
	return map[string]map[string]any{
		"output.branch.create.schema.json": jsonoutput.EnvelopeSchemaFor(
			"output.branch.create.schema.json",
			"bb branch create output",
			"JSON output schema for `bb branch create --json`. Data contains the repository and the newly created branch.",
			map[string]any{
				"type":                 "object",
				"additionalProperties": true,
				"properties": map[string]any{
					"repository": repositoryRefSchema(),
					"branch":     restBranchSchema(),
				},
				"required": []any{"repository", "branch"},
			},
		),
		"output.branch.delete.schema.json": jsonoutput.EnvelopeSchemaFor(
			"output.branch.delete.schema.json",
			"bb branch delete output",
			"JSON output schema for `bb branch delete --json`. Data confirms the deleted branch.",
			map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"status":     map[string]any{"const": "ok"},
					"repository": repositoryRefSchema(),
					"branch":     map[string]any{"type": "string", "description": "Name of the deleted branch."},
				},
				"required": []any{"status", "repository", "branch"},
			},
		),
		"output.branch.default.get.schema.json": jsonoutput.EnvelopeSchemaFor(
			"output.branch.default.get.schema.json",
			"bb branch default get output",
			"JSON output schema for `bb branch default get --json`. Data contains the repository and its current default branch name.",
			map[string]any{
				"type":                 "object",
				"additionalProperties": true,
				"properties": map[string]any{
					"repository":     repositoryRefSchema(),
					"default_branch": map[string]any{"type": "string", "description": "Current default branch name."},
				},
				"required": []any{"repository", "default_branch"},
			},
		),
		"output.branch.default.set.schema.json": jsonoutput.EnvelopeSchemaFor(
			"output.branch.default.set.schema.json",
			"bb branch default set output",
			"JSON output schema for `bb branch default set --json`. Data confirms the new default branch.",
			map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"status":         map[string]any{"const": "ok"},
					"repository":     repositoryRefSchema(),
					"default_branch": map[string]any{"type": "string"},
				},
				"required": []any{"status", "repository", "default_branch"},
			},
		),
	}
}
