// Command mcp-docs-export writes the MCP tool reference from the specs the
// server actually registers.
//
// A hand-written table of these goes stale the first time a tool is added, and
// the column that matters most is the one nobody would think to update: whether
// a tool is exposed by default or held behind --yolo. Reading mcp.AllSpecs()
// makes the page a projection of the registry rather than a second copy of it,
// and docs:verify-generated fails when the two disagree.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/mcp"
)

func main() {
	outputPath := flag.String("out", "docs/site/reference/mcp-tools.md", "Path to the generated MCP tool reference")
	flag.Parse()

	if err := exportToolReference(*outputPath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type toolRow struct {
	Name        string
	Description string
	ReadOnly    bool
}

func exportToolReference(outputPath string) error {
	var defaults, gated []toolRow

	for _, spec := range mcp.AllSpecs() {
		if spec.Tool == nil {
			continue
		}
		row := toolRow{
			Name:        spec.Tool.Name,
			Description: collapse(spec.Tool.Description),
		}
		if spec.Tool.Annotations != nil {
			row.ReadOnly = spec.Tool.Annotations.ReadOnlyHint
		}
		if spec.Safe {
			defaults = append(defaults, row)
			continue
		}
		gated = append(gated, row)
	}

	sortRows(defaults)
	sortRows(gated)

	var out strings.Builder
	out.WriteString("---\nsearch:\n  boost: 1.2\n---\n\n")
	out.WriteString("# MCP Tools\n\n")
	out.WriteString("This page is generated from the server's tool registry by `task docs:export-mcp-tools`. Do not edit manually.\n\n")

	fmt.Fprintf(&out, "`bb ai mcp serve` registers %d tools. %d are available to any connected client; %d are withheld unless the server is started with `--yolo`.\n\n",
		len(defaults)+len(gated), len(defaults), len(gated))

	out.WriteString("## Available by default\n\n")
	out.WriteString("Exposed to every client `bb ai mcp serve` accepts. **Not all of them are read-only** — the column says which write.\n\n")
	writeTable(&out, defaults, true)

	out.WriteString("\n## Requires `--yolo`\n\n")
	out.WriteString("Withheld unless the server is started with `--yolo`, because each either cannot be undone or influences whether a pull request may merge — and an agent that can do those takes part in a control it is meant to be subject to.\n\n")
	writeTable(&out, gated, false)

	out.WriteString("\n## What the split means\n\n")
	out.WriteString("The line is drawn by consequence, not by whether a tool writes. Opening a pull request or tagging a commit changes no branch and gates nothing, so both are available by default even though they write. Merging, enabling auto-merge, submitting a review and reporting a build status are held back: the first two are irreversible or cause a later merge, and the last two feed the checks that decide whether a merge is allowed.\n\n")
	out.WriteString("See [Enterprise Hardening](../advanced/enterprise-hardening.md#5-ai-ide-mcp-server-governance-bb-ai-mcp-serve) for scoping a server to a project or repository, restricting it with a read-only token, and mandating an audit trail by policy.\n")

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o750); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if err := os.WriteFile(outputPath, []byte(out.String()), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", outputPath, err)
	}

	fmt.Printf("wrote %s (%d tools: %d default, %d gated)\n", outputPath, len(defaults)+len(gated), len(defaults), len(gated))

	return nil
}

func writeTable(out *strings.Builder, rows []toolRow, showAccess bool) {
	if showAccess {
		out.WriteString("| Tool | Access | What it does |\n|---|---|---|\n")
	} else {
		out.WriteString("| Tool | What it does |\n|---|---|\n")
	}

	for _, row := range rows {
		if showAccess {
			access := "writes"
			if row.ReadOnly {
				access = "read-only"
			}
			fmt.Fprintf(out, "| `%s` | %s | %s |\n", row.Name, access, row.Description)
			continue
		}
		fmt.Fprintf(out, "| `%s` | %s |\n", row.Name, row.Description)
	}
}

// collapse folds a description onto one line. Several are written as multi-line
// Go string concatenations, and a newline inside a Markdown table cell ends the
// row early.
func collapse(text string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(text, "|", "\\|")), " ")
}

func sortRows(rows []toolRow) {
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
}
