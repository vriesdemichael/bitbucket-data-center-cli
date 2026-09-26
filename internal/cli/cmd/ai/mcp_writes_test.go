package ai

import (
	"strings"
	"testing"
)

// Writing and asking are separate facts, and the listing says both. Several
// tools write without asking, and create_tag asks though it only adds, so
// neither can be read off the other (#576 found SAFE read as read-only).
func TestMCPToolsSayWhichWriteApartFromWhichAsk(t *testing.T) {
	t.Parallel()

	byName := map[string]listedTool{}
	for _, tool := range listToolsJSON(t) {
		byName[tool.Name] = tool
	}

	for _, name := range []string{"create_pull_request", "add_pr_comment"} {
		if tool, ok := byName[name]; !ok || !tool.Writes || tool.Asks != "never" {
			t.Errorf("%s: %+v, want it to write without asking", name, tool)
		}
	}
	if tool, ok := byName["create_tag"]; !ok || !tool.Writes || tool.Asks != "always" {
		t.Errorf("create_tag: %+v, want it to write and ask", tool)
	}
	for _, name := range []string{"list_pull_requests", "get_file_content"} {
		if tool, ok := byName[name]; !ok || tool.Writes || tool.Asks != "never" {
			t.Errorf("%s: %+v, want it read-only", name, tool)
		}
	}

	stdout, _ := runAI(t, "mcp", "tools")
	for _, line := range strings.Split(stdout, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "create_tag" && fields[1] != "writes" {
			t.Errorf("the text listing does not say create_tag writes: %q", line)
		}
	}
}
