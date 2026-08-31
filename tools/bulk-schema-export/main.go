package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/docsite"
	bulkworkflow "github.com/vriesdemichael/bitbucket-server-cli/internal/workflows/bulk"
)

func main() {
	outputDir := flag.String("out", "docs/reference/schemas", "directory for exported bulk JSON schemas")
	siteVersion := flag.String("site-version", docsite.LatestVersion, "documentation site version the exported $id values claim")
	flag.Parse()

	if err := exportSchemas(*outputDir, *siteVersion); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func exportSchemas(outputDir, siteVersion string) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	schemas := bulkworkflow.SchemasFor(siteVersion)
	fileNames := make([]string, 0, len(schemas))
	for name := range schemas {
		fileNames = append(fileNames, name)
	}
	sort.Strings(fileNames)

	for _, name := range fileNames {
		encoded, err := json.MarshalIndent(schemas[name], "", "  ")
		if err != nil {
			return fmt.Errorf("encode %s: %w", name, err)
		}
		encoded = append(encoded, '\n')
		if err := os.WriteFile(filepath.Join(outputDir, name), encoded, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
		fmt.Printf("wrote %s\n", filepath.Join(outputDir, name))
	}

	return nil
}
