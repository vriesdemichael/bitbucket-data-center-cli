package cli

import (
	"sort"
	"strings"
	"testing"

	bbmcp "github.com/vriesdemichael/bitbucket-server-cli/internal/mcp"
)

// mcpToCLI records, for every MCP tool, the CLI command performing the same
// operation.
//
// The two surfaces are written and maintained independently, and they have
// already drifted apart in naming: get_pr_diff against `bb diff pr`,
// get_file_content against `bb repo cat`. That is tolerable — an MCP tool name
// is part of a different vocabulary — but only while it is deliberate. Nothing
// previously detected a rename on either side.
//
// This is the mapping, not an assertion that the names should match. Its value
// is that renaming or removing a command on either side fails the build, and
// that the intended relationship is written down where the skill and the docs
// can point at it rather than re-deriving it by hand.
var mcpToCLI = map[string]string{
	"get_pull_request":    "pr get",
	"list_pull_requests":  "pr list",
	"create_pull_request": "pr create",
	"list_pr_comments":    "pr comment list",
	"get_pr_diff":         "diff pr",
	"get_file_content":    "repo cat",
	"add_pr_comment":      "pr comment add",
	// `pr review complete --status APPROVED|NEEDS_WORK|UNAPPROVED`, not
	// `pr review approve`: the tool submits any of the three outcomes, and only
	// complete takes all of them.
	"submit_pr_review":     "pr review complete",
	"update_pull_request":  "pr update",
	"merge_pull_request":   "pr merge",
	"enable_auto_merge":    "pr auto-merge enable",
	"disable_auto_merge":   "pr auto-merge disable",
	"search_repositories":  "search repos",
	"list_branches":        "branch list",
	"resolve_ref":          "ref resolve",
	"list_tags":            "tag list",
	"create_tag":           "tag create",
	"get_build_status":     "build status get",
	"set_build_status":     "build status set",
	"list_required_builds": "build required list",
	"list_commits":         "commit list",
	"get_commit":           "commit get",
	"compare_refs":         "commit compare",
}

// mcpOnly records tools with no CLI counterpart, and why.
//
// Stated rather than left as a gap, so that a missing mapping is always a
// defect rather than possibly a decision someone forgot to write down.
var mcpOnly = map[string]string{
	"get_repository_clone_info": "returns clone URLs for the agent to use with git itself; " +
		"`bb repo clone` performs the clone, which an MCP server cannot do on the agent's behalf " +
		"because it does not share the agent's working tree",
}

// TestEveryMCPToolIsAccountedFor fails when a tool is added without deciding
// whether it mirrors a CLI command.
func TestEveryMCPToolIsAccountedFor(t *testing.T) {
	var unaccounted []string

	implemented := map[string]bool{}
	for _, spec := range bbmcp.AllSpecs() {
		name := spec.Tool.Name
		implemented[name] = true

		_, mapped := mcpToCLI[name]
		_, only := mcpOnly[name]

		switch {
		case mapped && only:
			t.Errorf("tool %q is both mapped to a CLI command and declared MCP-only", name)
		case !mapped && !only:
			unaccounted = append(unaccounted, name)
		}
	}

	if len(unaccounted) > 0 {
		sort.Strings(unaccounted)
		t.Errorf(
			"%d MCP tool(s) are in neither mcpToCLI nor mcpOnly:\n  %s\n\nAdd the CLI command each one mirrors, or record why it has none.",
			len(unaccounted), strings.Join(unaccounted, "\n  "),
		)
	}

	// Stale entries matter as much as missing ones: a mapping for a tool that
	// no longer exists is the same drift in the other direction.
	for name := range mcpToCLI {
		if !implemented[name] {
			t.Errorf("mcpToCLI maps %q, which the server does not implement", name)
		}
	}
	for name := range mcpOnly {
		if !implemented[name] {
			t.Errorf("mcpOnly declares %q, which the server does not implement", name)
		}
	}
}

// TestEveryMappedCLICommandExists resolves each mapping against the real
// command tree, so a CLI rename fails here rather than silently leaving the MCP
// tool as the only way to reach an operation.
func TestEveryMappedCLICommandExists(t *testing.T) {
	root := NewRootCommand()

	names := make([]string, 0, len(mcpToCLI))
	for name := range mcpToCLI {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, tool := range names {
		path := mcpToCLI[tool]

		t.Run(tool, func(t *testing.T) {
			args := strings.Fields(path)

			target, remaining, err := root.Find(args)
			if err != nil {
				t.Fatalf("%q maps to %q, which does not resolve: %v", tool, path, err)
			}

			// Find returns the deepest command it matched plus whatever it could
			// not place. Leftovers mean the path named a command that does not
			// exist, and Find fell back to an ancestor.
			if len(remaining) > 0 {
				t.Fatalf("%q maps to %q, but %q is not a subcommand of %q", tool, path, remaining[0], target.CommandPath())
			}

			if target.HasAvailableSubCommands() && !target.Runnable() {
				t.Fatalf("%q maps to %q, which is a command group rather than an operation", tool, path)
			}
		})
	}
}
