package ai

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// Exposure is drawn by consequence, so a tool exposed by default can write.
// The listing says which do, rather than leaving SAFE to be read as read-only
// (#576).
func TestMCPToolsSayWhichWriteApartFromExposure(t *testing.T) {
	t.Parallel()

	run := func(t *testing.T, args ...string) string {
		t.Helper()
		cmd := New(testMCPDeps())
		if len(args) > 0 && args[len(args)-1] == "--json" {
			cmd.PersistentFlags().Bool("json", true, "")
		}
		buf := &bytes.Buffer{}
		cmd.SetOut(buf)
		cmd.SetErr(buf)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("bb ai %s: %v", strings.Join(args, " "), err)
		}
		return buf.String()
	}

	var envelope struct {
		Data []struct {
			Name   string `json:"name"`
			Safe   bool   `json:"safe"`
			Writes bool   `json:"writes"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(run(t, "mcp", "tools", "--json")), &envelope); err != nil {
		t.Fatalf("output is not a parseable envelope: %v", err)
	}
	byName := map[string]struct{ Safe, Writes bool }{}
	for _, tool := range envelope.Data {
		byName[tool.Name] = struct{ Safe, Writes bool }{tool.Safe, tool.Writes}
	}

	for _, name := range []string{"create_pull_request", "update_pull_request", "add_pr_comment", "create_tag", "disable_auto_merge"} {
		if tool, ok := byName[name]; !ok || !tool.Safe || !tool.Writes {
			t.Errorf("%s: %+v, want exposed by default and writing", name, tool)
		}
	}
	for _, name := range []string{"list_pull_requests", "get_file_content"} {
		if tool, ok := byName[name]; !ok || !tool.Safe || tool.Writes {
			t.Errorf("%s: %+v, want exposed by default and read-only", name, tool)
		}
	}

	for _, line := range strings.Split(run(t, "mcp", "tools"), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "create_tag" && fields[2] != "writes" {
			t.Errorf("the text listing does not say create_tag writes: %q", line)
		}
	}
}
