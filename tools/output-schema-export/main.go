// Command output-schema-export writes the --json envelope schemas to disk.
//
// They already existed, derived from the same declarations the CLI emits from,
// and nothing wrote them anywhere. ADR-046 says the failure shape "is published
// once as docs/reference/schemas/output/output.error.schema.json"; that file did
// not exist, so meta.limitReached, meta.bbVersion and error.kind were a contract
// a consumer could only discover by provoking it (#573).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/outputschemas"
)

func main() {
	outputDir := flag.String("out", "docs/reference/schemas/output", "directory for the exported envelope schemas")
	flag.Parse()

	if err := export(*outputDir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func export(outputDir string) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	schemas := outputschemas.Schemas()
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
