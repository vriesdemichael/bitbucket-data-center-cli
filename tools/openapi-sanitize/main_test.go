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
