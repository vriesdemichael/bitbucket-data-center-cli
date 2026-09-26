// Command mcp-docs-export writes the MCP tool reference from the specs the
// server actually registers.
//
// A hand-written table of these goes stale the first time a tool is added, and
// the columns that matter most are the ones nobody would think to update:
// whether a tool writes, and whether it asks the person before it runs.
// Reading mcp.AllSpecs() makes the page a projection of the registry rather
// than a second copy of it, and docs:verify-generated fails when the two
// disagree.
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
	Asks        mcp.Asking
}

func exportToolReference(outputPath string) error {
	var rows []toolRow
	asking := 0

	for _, spec := range mcp.AllSpecs() {
		if spec.Tool == nil {
			continue
		}
		rows = append(rows, toolRow{
			Name:        spec.Tool.Name,
			Description: collapse(spec.Tool.Description),
			ReadOnly:    spec.ReadOnly(),
			Asks:        spec.Asks,
		})
		if spec.Asks != mcp.AsksNever {
			asking++
		}
	}

	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })

	var out strings.Builder
	out.WriteString("---\nsearch:\n  boost: 1.2\n---\n\n")
	out.WriteString("# MCP Tools\n\n")
	out.WriteString("This page is generated from the server's tool registry by `task docs:export-mcp-tools`. Do not edit manually.\n\n")

	fmt.Fprintf(&out, "`bb ai mcp serve` registers %d tools and exposes every one of them unless `--read-only`, `--tools`, `--exclude` or a scope withholds it. %d ask the person to confirm a call in the MCP client before they run.\n\n",
		len(rows), asking)

	out.WriteString("| Tool | Access | Asks | What it does |\n|---|---|---|---|\n")
	for _, row := range rows {
		access := "writes"
		if row.ReadOnly {
			access = "read-only"
		}
		fmt.Fprintf(&out, "| `%s` | %s | %s | %s |\n", row.Name, access, row.Asks, row.Description)
	}

	out.WriteString("\n## Tools that ask\n\n")
	out.WriteString("A tool asks when it merges, changes whether or when a pull request merges, or feeds a check that decides whether one may. `create_tag` asks too, since release pipelines commonly act on a new tag. `update_pull_request` asks only for a call that sets the draft flag: a draft cannot be merged, and making a pull request a draft cancels its auto-merge.\n\n")
	out.WriteString("The confirmation is an MCP elicitation. The client shows what the call will do, with one box to tick, and the tool acts only once the person accepts. A client that cannot show a confirmation gets error -32021 (missing required client capability) for those tools, and nothing reaches Bitbucket. Whether a client puts the question to the person or answers it itself is the client's to decide.\n\n")
	out.WriteString("## Read-only\n\n")
	out.WriteString("`bb ai mcp serve --read-only` exposes only the read-only tools. It is for a client you do not trust with the tool annotations and the confirmations: a client that cannot be trusted with them should not make changes in Bitbucket, so make them yourself.\n\n")
	out.WriteString("See [Enterprise Hardening](../advanced/enterprise-hardening.md#5-ai-ide-mcp-server-governance-bb-ai-mcp-serve) for scoping a server to a project or repository, restricting it with a read-only token, and mandating an audit trail by policy.\n")

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o750); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if err := os.WriteFile(outputPath, []byte(out.String()), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", outputPath, err)
	}

	fmt.Printf("wrote %s (%d tools, %d that ask)\n", outputPath, len(rows), asking)

	return nil
}

// collapse folds a description onto one line. Several are written as multi-line
// Go string concatenations, and a newline inside a Markdown table cell ends the
// row early.
func collapse(text string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(text, "|", `\|`)), " ")
}
