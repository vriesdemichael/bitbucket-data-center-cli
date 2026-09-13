package mcp

import (
	"context"
	"encoding/json"
	"slices"
	"sort"
	"strings"
	"testing"
)

// TestEveryLimitedToolSaysWhenItStopped is #573 on the MCP server.
//
// Nine tools take a limit and none said whether it was reached, so an agent
// that asked for 25 and received 25 could not tell whether that was all of
// them. The CLI answers with meta.limitReached; a tool answers with
// limit_reached.
//
// Asked of the published schemas rather than the Go types: the output schema
// is what a client reads and what the SDK validates a result against, so a
// field that is required there cannot be silently omitted from a result.
func TestEveryLimitedToolSaysWhenItStopped(t *testing.T) {
	t.Parallel()

	session := connect(t, testClients(t), nil, nil, true)
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}

	var silent []string
	limited := 0
	for _, tool := range listed.Tools {
		input := schemaOf(t, tool.Name, tool.InputSchema)
		if _, takesLimit := input.Properties["limit"]; !takesLimit {
			continue
		}
		limited++

		output := schemaOf(t, tool.Name, tool.OutputSchema)
		reached, declared := output.Properties["limit_reached"]
		switch {
		case !declared:
			silent = append(silent, tool.Name+": takes limit, output has no limit_reached")
		case reached.Type != "boolean":
			silent = append(silent, tool.Name+": limit_reached is "+string(reached.Type)+", not boolean")
		case !slices.Contains(output.Required, "limit_reached"):
			// An optional field reads as "not truncated" when it is absent.
			silent = append(silent, tool.Name+": limit_reached is optional, so its absence reads as not truncated")
		}
	}

	// A listing that stopped matching would report perfect compliance.
	if limited < 5 {
		t.Fatalf("found only %d tools taking a limit, expected nine.\nThe schema walk is probably broken, not the tools.", limited)
	}

	if len(silent) > 0 {
		sort.Strings(silent)
		t.Fatalf("%d of %d tools that take a limit do not say when they reach it:\n  %s\n\n"+
			"Return the collection through capped and set limit_reached from it.",
			len(silent), limited, strings.Join(silent, "\n  "))
	}
}

func TestCapped(t *testing.T) {
	t.Parallel()

	five := []int{1, 2, 3, 4, 5}

	testCases := []struct {
		name        string
		limit       int
		results     []int
		wantLen     int
		wantReached bool
	}{
		{name: "under the limit", limit: 10, results: five, wantLen: 5, wantReached: false},
		{name: "at the limit", limit: 5, results: five, wantLen: 5, wantReached: true},
		{name: "over the limit is cut", limit: 2, results: five, wantLen: 2, wantReached: true},
		{name: "empty", limit: 25, results: nil, wantLen: 0, wantReached: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, reached := capped(testCase.limit, testCase.results)
			if len(got) != testCase.wantLen || reached != testCase.wantReached {
				t.Fatalf("capped(%d, %d results) = %d results, reached %v; want %d, %v",
					testCase.limit, len(testCase.results), len(got), reached, testCase.wantLen, testCase.wantReached)
			}
		})
	}
}

type toolSchema struct {
	Properties map[string]struct {
		Type schemaType `json:"type"`
	} `json:"properties"`
	Required []string `json:"required"`
}

// schemaType reads a JSON Schema type, which may be one name or a list of them.
type schemaType string

func (value *schemaType) UnmarshalJSON(raw []byte) error {
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		*value = schemaType(single)
		return nil
	}

	var several []string
	if err := json.Unmarshal(raw, &several); err != nil {
		return err
	}
	*value = schemaType(strings.Join(several, "|"))

	return nil
}

func schemaOf(t *testing.T, tool string, schema any) toolSchema {
	t.Helper()

	raw, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("%s: marshal schema: %v", tool, err)
	}

	var decoded toolSchema
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("%s: decode schema %s: %v", tool, raw, err)
	}

	return decoded
}
