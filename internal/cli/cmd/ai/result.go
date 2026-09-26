package ai

import (
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
)

// Tool is one MCP tool the server exposes.
//
// writes and asks are separate facts. Several tools write without asking,
// opening a pull request or commenting, and create_tag asks though it only
// adds.
//
// safe and exposure described exposure without --yolo. Every tool has it now,
// so they are constant, and deprecated until the next major (ADR-084).
type Tool struct {
	Name        string `json:"name" jsonschema:"Tool name, as an MCP client sees it."`
	Description string `json:"description,omitempty" jsonschema:"What the tool does."`
	Writes      bool   `json:"writes" jsonschema:"Whether the tool changes anything in Bitbucket."`
	Asks        string `json:"asks" jsonschema:"Whether a call asks the person to confirm it in the MCP client before it runs: always, never, or when-draft-changes for a call that changes the pull request's draft flag."`
	Safe        bool   `json:"safe" jsonschema:"Deprecated: always true, since every tool is exposed without --yolo. Read asks and writes instead."`
	Exposure    string `json:"exposure" jsonschema:"Deprecated: always SAFE, since every tool is exposed without --yolo. Read asks and writes instead."`
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
		"asks":     askingValues,
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
