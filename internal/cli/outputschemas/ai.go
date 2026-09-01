package outputschemas

import (
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/jsonoutput"
)

// aiSchemas publishes the output schemas for the ai command group.
//
// bb ai skill install and remove report a path bb chose -- project scope
// resolves against the working directory, global against the home directory --
// so the payload carries something the caller cannot compute, which is what
// makes it data rather than a message.
func aiSchemas() map[string]map[string]any {
	skillFileData := func(statuses []any) map[string]any {
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"status": map[string]any{"type": "string", "enum": statuses},
				"skill":  map[string]any{"type": "string", "enum": []any{"bb", "bb-bulk"}},
				"path":   map[string]any{"type": "string", "minLength": 1},
				"scope":  map[string]any{"type": "string", "enum": []any{"project", "global"}},
			},
			"required": []any{"status", "skill", "path", "scope"},
		}
	}

	return map[string]map[string]any{
		"output.ai.skill.install.schema.json": jsonoutput.EnvelopeSchemaFor(
			"output.ai.skill.install.schema.json",
			"bb ai skill install output",
			"JSON output schema for `bb ai skill install --json`. Data names the skill written, the absolute path it was written to, and the scope that resolved that path.",
			skillFileData([]any{"installed"}),
		),
		"output.ai.skill.remove.schema.json": jsonoutput.EnvelopeSchemaFor(
			"output.ai.skill.remove.schema.json",
			"bb ai skill remove output",
			"JSON output schema for `bb ai skill remove --json`. Data names the skill, the absolute path considered, and the scope. Removing a skill that is already absent is not an error: status distinguishes the two outcomes.",
			skillFileData([]any{"removed", "not_found"}),
		),
	}
}
