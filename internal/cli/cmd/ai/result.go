package ai

import (
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
)

// Tool is one MCP tool the server exposes.
//
// safe and exposure are the same fact twice on purpose: safe is the boolean the
// server gates on, exposure is that classification as a stable string, so a
// consumer can render it without re-deriving the vocabulary.
//
// writes is a different fact, and the one safe was taken for: the gate is drawn
// by consequence, so some of the tools exposed by default write (#576).
type Tool struct {
	Name        string `json:"name" jsonschema:"Tool name, as an MCP client sees it."`
	Description string `json:"description,omitempty" jsonschema:"What the tool does."`
	Safe        bool   `json:"safe" jsonschema:"Whether the server exposes the tool without --yolo. Not whether it writes: see writes."`
	Exposure    string `json:"exposure" jsonschema:"The same classification as a stable string."`
	Writes      bool   `json:"writes" jsonschema:"Whether the tool changes anything in Bitbucket. Some tools exposed by default do: opening a pull request, commenting, tagging."`
}

// SkillFile is what `bb ai skill install` and `remove` report.
//
// The paths are the values a caller cannot compute: project scope resolves
// against the working directory and global scope against the home directory,
// so the command chose them rather than the caller.
type SkillFile struct {
	Status string   `json:"status" jsonschema:"installed, removed, or not_found when removing something already absent."`
	Skill  string   `json:"skill" jsonschema:"Which skill this concerns."`
	Path   string   `json:"path" jsonschema:"Absolute path of the copy in .agents/skills, which most agents read; when removing, the first file removed."`
	Paths  []string `json:"paths" jsonschema:"Every file written or removed: the .agents/skills copy and the .claude/skills copy Claude Code reads. For not_found, where it looked."`
	Scope  string   `json:"scope" jsonschema:"project or global, which is what decided the paths."`
}

func init() {
	result.Declare("ai mcp tools", result.List[Tool](map[string][]string{
		"exposure": {exposureSafe, exposureYolo},
	}))
	result.Declare("ai skill install", result.For[SkillFile](map[string][]string{
		"status": {"installed"},
		"skill":  {"bb"},
		"scope":  {"project", "global"},
	}))
	result.Declare("ai skill remove", result.For[SkillFile](map[string][]string{
		"status": {"removed", "not_found"},
		"skill":  {"bb"},
		"scope":  {"project", "global"},
	}))
}
