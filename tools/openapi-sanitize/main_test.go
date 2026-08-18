package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// collidingSpec has many endpoints sharing the operationId "get", plus one that
// already carries the suffix the sanitizer would otherwise hand out.
func collidingSpec(t *testing.T) string {
	t.Helper()

	paths := map[string]any{}
	for index := 0; index < 12; index++ {
		paths[fmt.Sprintf("/api/latest/thing%02d", index)] = map[string]any{
			"get": map[string]any{"operationId": "get", "responses": map[string]any{}},
		}
	}
	// Already named get_2, so blind suffixing collides with it.
	paths["/api/latest/preclaimed"] = map[string]any{
		"get": map[string]any{"operationId": "get_2", "responses": map[string]any{}},
	}

	spec := map[string]any{
		"openapi":    "3.0.1",
		"info":       map[string]any{"title": "test", "version": "10.2"},
		"paths":      paths,
		"components": map[string]any{"schemas": map[string]any{}},
	}

	dir := t.TempDir()
	input := filepath.Join(dir, "in.json")
	encoded, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	if err := os.WriteFile(input, encoded, 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	return input
}

func sanitizeToMapping(t *testing.T, input string, output string) map[string]string {
	t.Helper()

	if err := sanitize(input, output); err != nil {
		t.Fatalf("sanitize: %v", err)
	}

	content, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}

	var spec struct {
		Paths map[string]map[string]struct {
			OperationID string `json:"operationId"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(content, &spec); err != nil {
		t.Fatalf("decode output: %v", err)
	}

	mapping := map[string]string{}
	for path, methods := range spec.Paths {
		for method, op := range methods {
			mapping[op.OperationID] = method + " " + path
		}
	}

	return mapping
}

// Suffixes used to be assigned while ranging over the paths map, and Go
// randomises map iteration, so the same spec produced a different
// operationId-to-endpoint mapping on every run. That silently decided which
// endpoint a generated method such as Get3WithResponse actually called.
func TestSanitizeIsDeterministic(t *testing.T) {
	input := collidingSpec(t)
	dir := t.TempDir()

	first := sanitizeToMapping(t, input, filepath.Join(dir, "out1.json"))
	if len(first) < 13 {
		t.Fatalf("expected every operation to survive, got %d", len(first))
	}

	for run := 2; run <= 8; run++ {
		next := sanitizeToMapping(t, input, filepath.Join(dir, fmt.Sprintf("out%d.json", run)))
		if len(next) != len(first) {
			t.Fatalf("run %d produced %d operations, first run produced %d", run, len(next), len(first))
		}
		for id, endpoint := range first {
			if next[id] != endpoint {
				t.Fatalf("run %d mapped %q to %q, first run mapped it to %q", run, id, next[id], endpoint)
			}
		}
	}
}

// Every operation must end up with a distinct operationId, including when the
// spec already uses the suffix the sanitizer would hand out.
func TestSanitizeProducesUniqueOperationIDs(t *testing.T) {
	input := collidingSpec(t)
	output := filepath.Join(t.TempDir(), "out.json")

	if err := sanitize(input, output); err != nil {
		t.Fatalf("sanitize: %v", err)
	}

	content, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}

	var spec struct {
		Paths map[string]map[string]struct {
			OperationID string `json:"operationId"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(content, &spec); err != nil {
		t.Fatalf("decode output: %v", err)
	}

	seen := map[string]string{}
	total := 0
	for path, methods := range spec.Paths {
		for method, op := range methods {
			total++
			canonical := canonicalOperationID(op.OperationID)
			if previous, duplicate := seen[canonical]; duplicate {
				t.Fatalf("operationId %q (canonical %q) reused by %s %s and %s", op.OperationID, canonical, method, path, previous)
			}
			seen[canonical] = method + " " + path
		}
	}

	if total != 13 {
		t.Fatalf("expected 13 operations, got %d", total)
	}
}

func TestUniqueOperationIDSkipsClaimedSuffixes(t *testing.T) {
	seen := map[string]int{
		canonicalOperationID("get"):   1,
		canonicalOperationID("get_2"): 1,
		canonicalOperationID("get_3"): 1,
	}

	if got := uniqueOperationID("get", seen); got != "get_4" {
		t.Fatalf("uniqueOperationID = %q, want %q", got, "get_4")
	}
}

// Path parameters present in the URL template but missing from the operation
// are what OPENAPI-001 exists to repair.
func TestSanitizeInjectsMissingPathParameters(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "in.json")
	spec := map[string]any{
		"openapi": "3.0.1",
		"info":    map[string]any{"title": "test", "version": "10.2"},
		"paths": map[string]any{
			"/api/latest/projects/{projectKey}/repos/{repositorySlug}/thing": map[string]any{
				"get": map[string]any{"operationId": "getThing", "responses": map[string]any{}},
			},
		},
	}
	encoded, _ := json.Marshal(spec)
	if err := os.WriteFile(input, encoded, 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	output := filepath.Join(dir, "out.json")
	if err := sanitize(input, output); err != nil {
		t.Fatalf("sanitize: %v", err)
	}

	content, _ := os.ReadFile(output)
	var decoded struct {
		Paths map[string]map[string]struct {
			Parameters []struct {
				Name     string `json:"name"`
				In       string `json:"in"`
				Required bool   `json:"required"`
			} `json:"parameters"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(content, &decoded); err != nil {
		t.Fatalf("decode output: %v", err)
	}

	params := decoded.Paths["/api/latest/projects/{projectKey}/repos/{repositorySlug}/thing"]["get"].Parameters
	names := map[string]bool{}
	for _, p := range params {
		if p.In == "path" && p.Required {
			names[p.Name] = true
		}
	}
	for _, want := range []string{"projectKey", "repositorySlug"} {
		if !names[want] {
			t.Fatalf("expected required path parameter %q, got %#v", want, params)
		}
	}
}

func TestFixEpochMillisFieldsRewritesNestedAndArrayShapes(t *testing.T) {
	// createdDate appears inline inside request and response bodies as well as
	// in named schemas, so the walk has to reach through nested objects and
	// arrays. Missing one occurrence leaves otherwise-identical anonymous
	// structs incompatible and generation stops compiling.
	spec := map[string]any{
		"components": map[string]any{
			"schemas": map[string]any{
				"Outer": map[string]any{
					"properties": map[string]any{
						"createdDate": map[string]any{"type": "string", "format": "date-time"},
						"nested": map[string]any{
							"properties": map[string]any{
								"createdDate": map[string]any{"type": "string", "format": "date-time"},
							},
						},
					},
				},
			},
		},
		"paths": []any{
			map[string]any{
				"createdDate": map[string]any{"type": "string", "format": "date-time"},
			},
		},
	}

	if fixed := fixEpochMillisFields(spec); fixed != 3 {
		t.Fatalf("expected 3 rewrites across nested and array shapes, got %d", fixed)
	}
}

func TestFixEpochMillisFieldsLeavesCorrectDeclarationsAlone(t *testing.T) {
	// The rewrite is guarded on the broken shape so an upstream correction is
	// respected rather than forced back to the workaround.
	spec := map[string]any{
		"createdDate": map[string]any{"type": "integer", "format": "int64"},
		"other": map[string]any{
			"expiryDate": map[string]any{"type": "string", "format": "date-time"},
		},
	}

	if fixed := fixEpochMillisFields(spec); fixed != 0 {
		t.Fatalf("expected no rewrites, got %d", fixed)
	}

	// expiryDate must be untouched: its encoding has not been verified.
	other := spec["other"].(map[string]any)
	expiry := other["expiryDate"].(map[string]any)
	if expiry["type"] != "string" {
		t.Fatalf("expiryDate was rewritten despite being out of scope: %v", expiry)
	}
}

func TestFixContentEncodedPathParamsRewritesToSchema(t *testing.T) {
	spec := map[string]any{
		"paths": map[string]any{
			"/branches/info/{commitId}": map[string]any{
				"get": map[string]any{
					"parameters": []any{
						map[string]any{
							"in":   "path",
							"name": "commitId",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"type": "string", "maxLength": float64(40)},
								},
							},
						},
					},
				},
			},
		},
	}

	if fixed := fixContentEncodedPathParams(spec); fixed != 1 {
		t.Fatalf("fixed = %d, want 1", fixed)
	}

	param := spec["paths"].(map[string]any)["/branches/info/{commitId}"].(map[string]any)["get"].(map[string]any)["parameters"].([]any)[0].(map[string]any)
	if _, present := param["content"]; present {
		t.Fatal("content map should be removed, or oapi-codegen still emits a json.Marshal")
	}
	schema, ok := param["schema"].(map[string]any)
	if !ok {
		t.Fatalf("schema = %v, want the schema lifted out of the content map", param["schema"])
	}
	if schema["type"] != "string" || schema["maxLength"] != float64(40) {
		t.Fatalf("schema = %v, want the original schema preserved intact", schema)
	}
}

// Parameters that already declare a schema, and parameters outside the path, are
// left exactly as they are: the rewrite is only sound for a path segment.
func TestFixContentEncodedPathParamsLeavesOtherParamsAlone(t *testing.T) {
	spec := map[string]any{
		"paths": map[string]any{
			"/thing/{id}": map[string]any{
				"get": map[string]any{
					"parameters": []any{
						map[string]any{
							"in":      "path",
							"name":    "id",
							"schema":  map[string]any{"type": "string"},
							"content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"type": "integer"}}},
						},
						map[string]any{
							"in":      "query",
							"name":    "filter",
							"content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"type": "string"}}},
						},
					},
				},
			},
		},
	}

	if fixed := fixContentEncodedPathParams(spec); fixed != 0 {
		t.Fatalf("fixed = %d, want 0", fixed)
	}

	params := spec["paths"].(map[string]any)["/thing/{id}"].(map[string]any)["get"].(map[string]any)["parameters"].([]any)
	pathParam := params[0].(map[string]any)
	if schema := pathParam["schema"].(map[string]any); schema["type"] != "string" {
		t.Fatalf("existing schema was overwritten: %v", schema)
	}
	if _, present := params[1].(map[string]any)["content"]; !present {
		t.Fatal("query parameter content map should be untouched")
	}
}

func TestFixArrayTypedPropertiesWrapsObjectInArray(t *testing.T) {
	spec := map[string]any{
		"components": map[string]any{
			"schemas": map[string]any{
				"RestRefSyncStatus": map[string]any{
					"properties": map[string]any{
						"aheadRefs": map[string]any{
							"type":       "object",
							"readOnly":   true,
							"properties": map[string]any{"id": map[string]any{"type": "string"}},
						},
						"enabled": map[string]any{"type": "boolean"},
					},
				},
			},
		},
	}

	if fixed := fixArrayTypedProperties(spec); fixed != 1 {
		t.Fatalf("fixed = %d, want 1 (only the listed property)", fixed)
	}

	properties := spec["components"].(map[string]any)["schemas"].(map[string]any)["RestRefSyncStatus"].(map[string]any)["properties"].(map[string]any)
	wrapped, ok := properties["aheadRefs"].(map[string]any)
	if !ok || wrapped["type"] != "array" {
		t.Fatalf("aheadRefs = %v, want an array schema", properties["aheadRefs"])
	}
	// readOnly describes the field, not the element, so it must not be left behind
	// on the item schema where it would describe each ref instead.
	items, ok := wrapped["items"].(map[string]any)
	if !ok || items["type"] != "object" {
		t.Fatalf("items = %v, want the original object schema", wrapped["items"])
	}
	if wrapped["readOnly"] != true {
		t.Fatal("readOnly should move up to the array")
	}
	if _, present := items["readOnly"]; present {
		t.Fatal("readOnly should not remain on the item schema")
	}
	if enabled := properties["enabled"].(map[string]any); enabled["type"] != "boolean" {
		t.Fatalf("unlisted property was rewritten: %v", enabled)
	}
}

func TestFixArrayTypedResponsesWrapsSchemaInArray(t *testing.T) {
	spec := map[string]any{
		"paths": map[string]any{
			"/gpg/latest/keys": map[string]any{
				"post": map[string]any{
					"operationId": "addKey",
					"responses": map[string]any{
						"200": map[string]any{
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/RestGpgKey"},
								},
							},
						},
						"400": map[string]any{
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"type": "object"},
								},
							},
						},
					},
				},
				"get": map[string]any{
					"operationId": "getKeys",
					"responses": map[string]any{
						"200": map[string]any{
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/RestGpgKey"},
								},
							},
						},
					},
				},
			},
		},
	}

	if fixed := fixArrayTypedResponses(spec); fixed != 1 {
		t.Fatalf("fixed = %d, want 1", fixed)
	}

	post := spec["paths"].(map[string]any)["/gpg/latest/keys"].(map[string]any)["post"].(map[string]any)
	responses := post["responses"].(map[string]any)
	ok200 := responses["200"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	if ok200["type"] != "array" {
		t.Fatalf("200 schema = %v, want an array", ok200)
	}
	if items, _ := ok200["items"].(map[string]any); items["$ref"] != "#/components/schemas/RestGpgKey" {
		t.Fatalf("items = %v, want the original ref", ok200["items"])
	}

	// Only the listed status on the listed operation moves; error responses and
	// other operations on the same path stay as they are.
	bad400 := responses["400"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	if bad400["type"] != "object" {
		t.Fatalf("400 schema was rewritten: %v", bad400)
	}
	get := spec["paths"].(map[string]any)["/gpg/latest/keys"].(map[string]any)["get"].(map[string]any)
	getSchema := get["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	if getSchema["$ref"] != "#/components/schemas/RestGpgKey" {
		t.Fatalf("an unlisted operation was rewritten: %v", getSchema)
	}
}
