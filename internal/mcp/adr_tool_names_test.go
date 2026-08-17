package mcp

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// toolNamePattern matches the snake_case shape every MCP tool name uses, with
// enough segments to avoid catching ordinary prose.
var toolNamePattern = regexp.MustCompile(`\b[a-z]+(?:_[a-z]+){1,3}\b`)

// prosePermitted are the YAML keys of the decision-record schema, which share
// the snake_case shape of a tool name without being one.
var prosePermitted = map[string]bool{
	"agent_instructions":    true,
	"rejected_alternatives": true,
	"superseded_by":         true,
}

// TestADRDoesNotNameToolsThatDoNotExist guards the drift that made ADR-039
// wrong for several releases.
//
// The record carried three hand-maintained tiers of tool names. It listed
// clone_repository, which has never existed; omitted five tools that do; and
// described merge_pull_request and set_build_status as enabled by default when
// the server withholds them without --yolo. An accepted ADR that contradicts
// the code is worse than no ADR, because readers — including agents, which this
// project explicitly targets — treat it as authoritative.
//
// The fix was to stop enumerating. This keeps it that way: any snake_case name
// in the MCP decision records must be a tool the server actually implements.
func TestADRDoesNotNameToolsThatDoNotExist(t *testing.T) {
	implemented := map[string]bool{}
	for _, spec := range AllSpecs() {
		implemented[spec.Tool.Name] = true
	}

	records, err := filepath.Glob(filepath.Join("..", "..", "docs", "decisions", "*mcp*.yaml"))
	if err != nil {
		t.Fatalf("glob decision records: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("no MCP decision records found; has the filename changed?")
	}

	var offenders []string

	for _, record := range records {
		contents, err := os.ReadFile(record)
		if err != nil {
			t.Fatalf("read %s: %v", record, err)
		}

		for _, candidate := range toolNamePattern.FindAllString(string(contents), -1) {
			if implemented[candidate] || prosePermitted[candidate] {
				continue
			}

			offenders = append(offenders, filepath.Base(record)+": "+candidate)
		}
	}

	if len(offenders) > 0 {
		sort.Strings(offenders)
		t.Fatalf(
			"decision records name %d tool(s) the server does not implement:\n  %s\n\nDo not enumerate tools in an ADR; read mcp.AllSpecs() or run bb ai mcp tools.",
			len(offenders), strings.Join(offenders, "\n  "),
		)
	}
}
