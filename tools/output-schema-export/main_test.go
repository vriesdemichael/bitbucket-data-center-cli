package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/outputschemas"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/docsite"
)

func TestExportSchemas(t *testing.T) {
	dir := t.TempDir()

	if err := exportSchemas(dir, docsite.LatestVersion); err != nil {
		t.Fatalf("exportSchemas: %v", err)
	}

	schemas := outputschemas.Schemas()
	for name := range schemas {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("expected file %s to exist: %v", name, err)
			continue
		}

		var parsed map[string]any
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Errorf("file %s is not valid JSON: %v", name, err)
		}
	}
}

// A release re-exports the schemas under the version it is publishing, so the
// snapshot on disk claims that version rather than the moving alias.
func TestExportSchemasWritesTheRequestedSiteVersionIntoID(t *testing.T) {
	dir := t.TempDir()

	if err := exportSchemas(dir, "v4.0.0"); err != nil {
		t.Fatalf("exportSchemas: %v", err)
	}

	const name = outputschemas.ErrorSchemaFileName
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read exported schema: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("exported schema is not valid JSON: %v", err)
	}

	want := "https://vriesdemichael.github.io/bitbucket-data-center-cli/v4.0.0/reference/schemas/output/" + name
	if parsed["$id"] != want {
		t.Fatalf("$id = %v, want %q", parsed["$id"], want)
	}
}
