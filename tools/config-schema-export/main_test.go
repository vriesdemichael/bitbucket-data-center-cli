package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestExportSchema(t *testing.T) {
	tempDir := t.TempDir()
	outDir := filepath.Join(tempDir, "schemas")

	if err := exportSchema(outDir); err != nil {
		t.Fatalf("exportSchema failed: %v", err)
	}

	target := filepath.Join(outDir, "config.schema.json")
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read exported schema: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal exported schema: %v", err)
	}

	if parsed["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
		t.Errorf("unexpected $schema: %v", parsed["$schema"])
	}
	if parsed["title"] != "Bitbucket Server CLI Configuration" {
		t.Errorf("unexpected title: %v", parsed["title"])
	}
}
