package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
)

func main() {
	outputDir := flag.String("out", "docs/reference/schemas", "directory for exported configuration JSON schema")
	flag.Parse()

	if err := exportSchema(*outputDir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func exportSchema(outputDir string) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	schema := config.ConfigJSONSchema()
	encoded, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config.schema.json: %w", err)
	}
	encoded = append(encoded, '\n')

	target := filepath.Join(outputDir, "config.schema.json")
	if err := os.WriteFile(target, encoded, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", target, err)
	}
	fmt.Printf("wrote %s\n", target)
	return nil
}
