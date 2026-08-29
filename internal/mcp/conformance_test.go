package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// toolArguments is a valid argument set for every tool in AllSpecs.
//
// It exists so the conformance test can reach each handler body rather than
// bouncing off input validation. TestEveryToolHasConformanceArguments fails
// when a tool is added without an entry, which is what stops a new tool from
// quietly opting out of the output contract.
var toolArguments = map[string]map[string]any{
	"get_pull_request":          {"project": "TEST", "repo": "demo", "id": "1"},
	"list_pull_requests":        {"project": "TEST", "repo": "demo"},
	"create_pull_request":       {"project": "TEST", "repo": "demo", "from_ref": "feature", "to_ref": "main", "title": "demo"},
	"update_pull_request":       {"project": "TEST", "repo": "demo", "pr_id": "1", "version": 1, "title": "renamed"},
	"list_pr_comments":          {"project": "TEST", "repo": "demo", "pr_id": "1"},
	"get_pr_diff":               {"project": "TEST", "repo": "demo", "pr_id": "1"},
	"get_file_content":          {"project": "TEST", "repo": "demo", "path": "README.md"},
	"add_pr_comment":            {"project": "TEST", "repo": "demo", "pr_id": "1", "text": "hello"},
	"submit_pr_review":          {"project": "TEST", "repo": "demo", "pr_id": "1", "action": "approve"},
	"merge_pull_request":        {"project": "TEST", "repo": "demo", "pr_id": "1"},
	"enable_auto_merge":         {"project": "TEST", "repo": "demo", "pr_id": "1"},
	"disable_auto_merge":        {"project": "TEST", "repo": "demo", "pr_id": "1"},
	"search_repositories":       {"project": "TEST"},
	"get_repository_clone_info": {"project": "TEST", "repo": "demo"},
	"list_branches":             {"project": "TEST", "repo": "demo"},
	"resolve_ref":               {"project": "TEST", "repo": "demo", "ref": "main"},
	"list_tags":                 {"project": "TEST", "repo": "demo"},
	"create_tag":                {"project": "TEST", "repo": "demo", "name": "v1.0.0", "start_point": "main"},
	"get_build_status":          {"commit_id": "abc123"},
	"set_build_status":          {"commit_id": "abc123", "key": "ci/unit", "state": "SUCCESSFUL", "url": "https://ci.example.com/1"},
	"list_required_builds":      {"project": "TEST", "repo": "demo"},
	"list_commits":              {"project": "TEST", "repo": "demo"},
	"get_commit":                {"project": "TEST", "repo": "demo", "commit_id": "abc123"},
	"compare_refs":              {"project": "TEST", "repo": "demo", "from": "main", "to": "feature"},
}

// fixtureServer answers every Bitbucket request with a well-formed but empty
// response.
//
// The conformance test is about the shape a tool returns, not about the data
// Bitbucket holds, so the thinnest valid response for each endpoint family is
// exactly right: it reaches the handler's success path without a fixture corpus
// that would need maintaining alongside the real API.
func fixtureServer(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		w.Header().Set("Content-Type", "application/json;charset=UTF-8")

		switch {
		case strings.HasSuffix(path, "/diff") || strings.Contains(path, "/raw/"):
			// Diff and raw file reads are text, not JSON.
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("diff --git a/x b/x\n"))
		case (r.Method == http.MethodPost || r.Method == http.MethodPut) && strings.Contains(path, "/tags"):
			// A tag's id is a ref name, so it is a string where a pull
			// request's is a number. Getting this wrong fails to unmarshal
			// rather than failing validation, which is why it is split out.
			_, _ = w.Write([]byte(`{"id":"refs/tags/v1.0.0","displayId":"v1.0.0","hash":"abc123"}`))
		case r.Method == http.MethodPost || r.Method == http.MethodPut:
			// Creates and updates echo back a minimal entity.
			_, _ = w.Write([]byte(`{"id":1,"version":1,"title":"demo","state":"OPEN"}`))
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			// Everything else is a Bitbucket paged collection.
			_, _ = w.Write([]byte(`{"isLastPage":true,"size":0,"values":[]}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// TestEveryToolHasConformanceArguments fails when a tool is added to AllSpecs
// without an argument fixture, so it cannot escape the conformance run below.
func TestEveryToolHasConformanceArguments(t *testing.T) {
	for _, spec := range AllSpecs() {
		if _, ok := toolArguments[spec.Tool.Name]; !ok {
			t.Errorf("tool %q has no entry in toolArguments; add one so it is covered by the conformance test", spec.Tool.Name)
		}
	}
	for name := range toolArguments {
		found := false
		for _, spec := range AllSpecs() {
			if spec.Tool.Name == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("toolArguments has an entry for %q, which is not in AllSpecs", name)
		}
	}
}

// TestEveryToolDeclaresAnOutputSchema asserts the contract exists at all.
//
// Before the SDK migration not one of the 24 tools declared an output schema,
// so nothing validated what a handler returned and a wire-shape bug reached a
// user (issue #416). The schema is read from tools/list rather than from the
// Spec because the SDK derives it during registration: tools/list is where a
// client sees it, so it is where the test looks.
func TestEveryToolDeclaresAnOutputSchema(t *testing.T) {
	session := connect(t, Clients{}, nil, nil, true)

	result, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	if len(result.Tools) != len(AllSpecs()) {
		t.Fatalf("tools/list returned %d tools, want %d", len(result.Tools), len(AllSpecs()))
	}

	for _, tool := range result.Tools {
		if tool.OutputSchema == nil {
			t.Errorf("tool %q declares no output schema", tool.Name)
			continue
		}
		encoded, marshalErr := json.Marshal(tool.OutputSchema)
		if marshalErr != nil {
			t.Errorf("tool %q: marshal output schema: %v", tool.Name, marshalErr)
			continue
		}
		// Every payload is named inside an object, so a client validating
		// against a pre-SEP-2106 revision never sees a bare array.
		var probe struct {
			Type string `json:"type"`
		}
		if unmarshalErr := json.Unmarshal(encoded, &probe); unmarshalErr != nil {
			t.Errorf("tool %q: unmarshal output schema: %v", tool.Name, unmarshalErr)
			continue
		}
		if probe.Type != "object" {
			t.Errorf("tool %q declares a %q-rooted output schema; every tool must name its payload inside an object", tool.Name, probe.Type)
		}
	}
}

// TestEveryToolResultConformsToItsOutputSchema drives every tool through a real
// client-to-server round trip and validates what comes back against the schema
// the same server advertises.
//
// The SDK already validates output before it leaves the server, so a
// non-conforming result surfaces as an error rather than reaching a client.
// Validating independently here proves that rather than trusting it, and pins
// the two halves of the contract — the advertised schema and the returned
// value — to each other.
func TestEveryToolResultConformsToItsOutputSchema(t *testing.T) {
	session := connect(t, clientsForURL(t, fixtureServer(t)), nil, nil, true)
	ctx := context.Background()

	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	schemas := make(map[string]*jsonschema.Schema, len(listed.Tools))
	for _, tool := range listed.Tools {
		schemas[tool.Name] = compileSchema(t, tool.Name, tool.OutputSchema)
	}

	for _, spec := range AllSpecs() {
		name := spec.Tool.Name
		t.Run(name, func(t *testing.T) {
			result, callErr := session.CallTool(ctx, &mcp.CallToolParams{
				Name:      name,
				Arguments: toolArguments[name],
			})
			if callErr != nil {
				t.Fatalf("tools/call returned a protocol error: %v", callErr)
			}
			if result.IsError {
				t.Fatalf("tools/call returned an error result: %s", contentText(result))
			}
			if result.StructuredContent == nil {
				t.Fatal("result carries no structuredContent")
			}

			// Re-encode and decode so validation runs against the JSON a client
			// actually receives, not against a Go value that might marshal
			// differently.
			encoded, marshalErr := json.Marshal(result.StructuredContent)
			if marshalErr != nil {
				t.Fatalf("marshal structuredContent: %v", marshalErr)
			}
			var decoded any
			if err := json.Unmarshal(encoded, &decoded); err != nil {
				t.Fatalf("structuredContent is not valid JSON: %v", err)
			}

			// A pre-SEP-2106 client rejects the whole response when
			// structuredContent is not a record, before it can fall back to the
			// text content. Naming every payload makes that unreachable.
			if _, ok := decoded.(map[string]any); !ok {
				t.Fatalf("structuredContent is %T, want a JSON object: %s", decoded, encoded)
			}

			if err := schemas[name].Validate(decoded); err != nil {
				t.Errorf("structuredContent does not conform to the advertised output schema: %v\npayload: %s", err, encoded)
			}

			// The spec requires a text fallback for clients that do not read
			// structuredContent at all. The SDK supplies the JSON encoding of
			// the output unless the handler sets its own, which get_pr_diff and
			// get_file_content do.
			if len(result.Content) == 0 {
				t.Error("result carries no text content fallback")
			}
		})
	}
}

// compileSchema turns an advertised output schema into a validator.
func compileSchema(t *testing.T, toolName string, schema any) *jsonschema.Schema {
	t.Helper()
	if schema == nil {
		t.Fatalf("tool %q declares no output schema", toolName)
	}
	encoded, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("tool %q: marshal output schema: %v", toolName, err)
	}
	doc, err := jsonschema.UnmarshalJSON(strings.NewReader(string(encoded)))
	if err != nil {
		t.Fatalf("tool %q: parse output schema: %v", toolName, err)
	}
	compiler := jsonschema.NewCompiler()
	resource := "https://bb.invalid/" + toolName + ".schema.json"
	if err := compiler.AddResource(resource, doc); err != nil {
		t.Fatalf("tool %q: add output schema: %v", toolName, err)
	}
	compiled, err := compiler.Compile(resource)
	if err != nil {
		t.Fatalf("tool %q: compile output schema: %v", toolName, err)
	}
	return compiled
}

// contentText flattens a result's text content for failure messages.
func contentText(result *mcp.CallToolResult) string {
	var parts []string
	for _, item := range result.Content {
		if text, ok := item.(*mcp.TextContent); ok {
			parts = append(parts, text.Text)
		}
	}
	return strings.Join(parts, "\n")
}
