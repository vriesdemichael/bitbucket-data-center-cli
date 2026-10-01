// Command output-schema-export writes the --json envelope schemas to disk.
//
// They are derived from the same declarations the CLI emits from. The failure
// shape is published once, as docs/reference/schemas/output/output.error.schema.json
// (ADR-046), so a consumer reads meta.limitReached, meta.bbVersion and error.kind
// from a schema rather than discovering them by provoking a failure.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/outputschemas"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/docsite"
)

func main() {
	outputDir := flag.String("out", "docs/reference/schemas/output", "directory for the exported envelope schemas")
	// A release publishes its own copy of every schema, so each has to claim the
	// version it is published under rather than the latest alias. Without this
	// flag every versioned snapshot identified its output schemas as /latest/.
	siteVersion := flag.String("site-version", docsite.LatestVersion, "documentation site version the exported $id values claim")
	flag.Parse()

	if err := export(*outputDir, *siteVersion); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func export(outputDir, siteVersion string) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	schemas := outputschemas.SchemasFor(siteVersion)
	if len(schemas) == 0 {
		return fmt.Errorf("no output schemas are registered, which means the registry broke rather than that the CLI stopped declaring its output")
	}

	names := make([]string, 0, len(schemas))
	for name := range schemas {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		encoded, err := json.MarshalIndent(schemas[name], "", "  ")
		if err != nil {
			return fmt.Errorf("encode %s: %w", name, err)
		}
		encoded = append(encoded, '\n')

		target := filepath.Join(outputDir, name)
		if err := os.WriteFile(target, encoded, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", target, err)
		}
	}

	fmt.Printf("wrote %d envelope schemas to %s\n", len(names), outputDir)

	return nil
}
