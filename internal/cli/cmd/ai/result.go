package ai

import (
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/result"
)

// Tool is one MCP tool the server exposes.
//
// safe and exposure are the same fact twice on purpose: safe is the boolean the
// server gates on, exposure is that classification as a stable string, so a
// consumer can render it without re-deriving the vocabulary.
type Tool struct {
	Name        string `json:"name" jsonschema:"Tool name, as an MCP client sees it."`
	Description string `json:"description,omitempty" jsonschema:"What the tool does."`
	Safe        bool   `json:"safe" jsonschema:"Whether the tool is read-only. A tool that writes is exposed only with --yolo."`
	Exposure    string `json:"exposure" jsonschema:"The same classification as a stable string."`
}

// SkillFile is what `bb ai skill install` and `remove` report.
//
// The path is the value a caller cannot compute: project scope resolves against
// the working directory and global scope against the home directory, so the
// command chose it rather than the caller.
type SkillFile struct {
	Status string `json:"status" jsonschema:"installed, removed, or not_found when removing something already absent."`
	Skill  string `json:"skill" jsonschema:"Which skill this concerns."`
	Path   string `json:"path" jsonschema:"Absolute path the file was written to or removed from."`
	Scope  string `json:"scope" jsonschema:"project or global, which is what decided the path."`
}

func init() {
	result.Declare("ai mcp tools", result.List[Tool](map[string][]string{
		"exposure": {exposureSafe, exposureYolo},
	}))
	result.Declare("ai skill install", result.For[SkillFile](map[string][]string{
		"status": {"installed"},
		"skill":  {"bb", "bb-bulk"},
		"scope":  {"project", "global"},
	}))
	result.Declare("ai skill remove", result.For[SkillFile](map[string][]string{
		"status": {"removed", "not_found"},
		"skill":  {"bb", "bb-bulk"},
		"scope":  {"project", "global"},
	}))
}
