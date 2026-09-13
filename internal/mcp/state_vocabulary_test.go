package mcp

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
)

// TestListPullRequestsAdvertisesTheStatesItAccepts is #577 on the MCP server.
//
// list_pull_requests described its state as "OPEN (default), MERGED, DECLINED,
// ALL". The service accepts open, closed and all, so an agent that followed the
// description and asked for merged pull requests was refused, with an error
// naming a CLI flag it never used -- and closed, which works, was not mentioned.
func TestListPullRequestsAdvertisesTheStatesItAccepts(t *testing.T) {
	t.Parallel()

	var description string
	for _, spec := range AllSpecs() {
		if spec.Tool.Name != "list_pull_requests" {
			continue
		}
		schema := spec.Tool.InputSchema
		if schema == nil {
			t.Fatal("list_pull_requests declares no input schema of its own, so its state description is a struct tag")
		}
		described, ok := schemaDescriptions(schema)["state"]
		if !ok {
			t.Fatal("list_pull_requests has no state property")
		}
		description = described
	}
	if description == "" {
		t.Fatal("list_pull_requests is not in the catalogue")
	}

	words := map[string]bool{}
	for _, word := range regexp.MustCompile(`[A-Za-z]+`).FindAllString(description, -1) {
		words[strings.ToLower(word)] = true
	}

	for _, accepted := range openapi.PullRequestStateFilters {
		if !words[accepted] {
			t.Errorf("the state description does not name %q, which the service accepts: %q", accepted, description)
		}
	}
	for _, rejected := range []string{"merged", "declined"} {
		if words[rejected] {
			t.Errorf("the state description names %q, which the service rejects: %q", rejected, description)
		}
	}
}

// schemaDescriptions reads each top-level property's description from a tool's
// input schema, whatever Go value holds it.
func schemaDescriptions(schema any) map[string]string {
	raw, err := json.Marshal(schema)
	if err != nil {
		return nil
	}

	var decoded struct {
		Properties map[string]struct {
			Description string `json:"description"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil
	}

	descriptions := make(map[string]string, len(decoded.Properties))
	for name, property := range decoded.Properties {
		descriptions[name] = property.Description
	}

	return descriptions
}
