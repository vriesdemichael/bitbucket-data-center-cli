package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/docsite"
	bulkworkflow "github.com/vriesdemichael/bitbucket-data-center-cli/internal/workflows/bulk"
)

func TestExportSchemasWritesAllExpectedFiles(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()

	if err := exportSchemas(outputDir, docsite.LatestVersion); err != nil {
		t.Fatalf("exportSchemas failed: %v", err)
	}

	expected := make([]string, 0, len(bulkworkflow.Schemas()))
	for name := range bulkworkflow.Schemas() {
		expected = append(expected, name)
	}
	sort.Strings(expected)

	entries, err := os.ReadDir(outputDir)
	if err != nil {
		t.Fatalf("readdir failed: %v", err)
	}
	if len(entries) != len(expected) {
		t.Fatalf("expected %d schema files, got %d", len(expected), len(entries))
	}

	for _, name := range expected {
		path := filepath.Join(outputDir, name)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("expected exported schema %s: %v", name, err)
		}
		if !strings.HasSuffix(string(raw), "\n") {
			t.Fatalf("expected trailing newline in %s", name)
		}
		if !strings.Contains(string(raw), `"$schema": "https://json-schema.org/draft/2020-12/schema"`) {
			t.Fatalf("expected draft schema marker in %s", name)
		}
	}
}

func TestExportSchemasReturnsErrorForInvalidOutputPath(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	filePath := filepath.Join(base, "not-a-dir")
	if err := os.WriteFile(filePath, []byte("x"), 0o600); err != nil {
		t.Fatalf("setup file failed: %v", err)
	}

	err := exportSchemas(filePath, docsite.LatestVersion)
	if err == nil {
		t.Fatal("expected exportSchemas to fail when output path is a file")
	}
	if !strings.Contains(err.Error(), "create output directory") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExportSchemasWritesTheRequestedSiteVersionIntoID(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()

	if err := exportSchemas(outputDir, "v4.0.0"); err != nil {
		t.Fatalf("exportSchemas failed: %v", err)
	}

	const name = "bulk-policy.schema.json"
	raw, err := os.ReadFile(filepath.Join(outputDir, name))
	if err != nil {
		t.Fatalf("read exported schema: %v", err)
	}

	want := `"$id": "https://vriesdemichael.github.io/bitbucket-data-center-cli/v4.0.0/reference/schemas/` + name + `"`
	if !strings.Contains(string(raw), want) {
		t.Fatalf("exported %s does not carry %s", name, want)
	}
}
