// Command mcp-docs-export writes the MCP reference from the tools, resource
// templates and prompts the server actually registers.
//
// A hand-written table of these goes stale the first time a tool is added, and
// the columns that matter most are the ones nobody would think to update:
// whether a tool writes, and whether it asks the person before it runs.
// Reading the registries makes the page a projection of them rather than a
// second copy, and docs:verify-generated fails when the two disagree.
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
	out.WriteString("# MCP Tools, Resources and Prompts\n\n")
	out.WriteString("This page is generated from the server's registries by `task docs:export-mcp-tools`. Do not edit manually.\n\n")

	fmt.Fprintf(&out, "`bb ai mcp serve` registers %d tools and exposes every one of them unless `--read-only`, `--tools`, `--exclude` or a scope withholds it. %d ask the person to confirm a call in the MCP client before they run.\n\n",
		len(rows), asking)
	out.WriteString("The [MCP server guide](../ai-and-llms.md#the-mcp-server) wires it into a client, with examples of the [views](../ai-and-llms.md#views-in-your-agent) `show` puts in front of you.\n\n")
	out.WriteString("The tools answer the agent, and `show` is for the person: an agent uses the tools to find, read and change things, and `show` for someone who is there to see the result, never when it works on its own. [Tools and views](../ai-and-llms.md#tools-and-views) says which for what.\n\n")
	out.WriteString("`refresh_view` and `suggest_form_values` are for those views: MCP Apps offers them to views and not to the model, and they go with `show` whether or not `--tools` names them. A click in a view is a call of a tool, through the client, never a connection to Bitbucket ([how views work](../advanced/mcp-governance.md#views-stay-inside-the-mcp-server)).\n\n")

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
	writeResources(&out)
	writePrompts(&out)
	out.WriteString("See [MCP Server Governance](../advanced/mcp-governance.md) for scoping a server to a project or repository, restricting it with a read-only token, and mandating an audit trail by policy.\n")

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o750); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if err := os.WriteFile(outputPath, []byte(out.String()), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", outputPath, err)
	}

	fmt.Printf("wrote %s (%d tools, %d that ask, %d resource templates, %d prompts)\n",
		outputPath, len(rows), asking, len(mcp.AllResourceSpecs()), len(mcp.AllPromptSpecs()))

	return nil
}

// writeResources lists the resource templates, and says what the resource list
// holds and which tool results link a resource.
func writeResources(out *strings.Builder) {
	out.WriteString("## Resources\n\n")
	out.WriteString("Pull requests, their diffs and open threads, files and commits are also resources. The person attaches them in the client, and a model in a client that reads resources can read them itself. A resource URI is a name bb resolves with its own credentials, not a link: the client asks bb for it, never Bitbucket, and the scope and the audit trail cover a resource read as they cover a tool call.\n\n")
	out.WriteString("| Resource | URI template | Served while | What it reads |\n|---|---|---|---|\n")
	for _, spec := range mcp.AllResourceSpecs() {
		fmt.Fprintf(out, "| `%s` | `%s` | `%s` | %s |\n", spec.Template.Name, spec.Template.URITemplate, spec.Tool, collapse(spec.Template.Description))
	}
	out.WriteString("\nA template is served while the tool it answers like is exposed, so `--tools` and `--exclude` decide the resources as they decide the tools: a server that leaves `get_file_content` out reads no files.\n\n")
	fmt.Fprintf(out, "The resource list holds your open pull requests and those waiting on your review, up to %d of each, while `list_pull_requests` and `get_pull_request` are exposed. A client can complete a template's project, repository, pull request, path and ref as the person types.\n\n", mcp.ListedPullRequests)

	linked := mcp.ResourceLinkedTools()
	names := make([]string, len(linked))
	for index, tool := range linked {
		names[index] = "`" + tool + "`"
	}
	listed := strings.Join(names, ", ")
	if len(names) > 1 {
		listed = strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
	}
	fmt.Fprintf(out, "The results of %s link the resource they came from, beside their content, so a client can offer to attach it or read it again.\n\n", listed)
}

// writePrompts lists the prompts and their arguments.
func writePrompts(out *strings.Builder) {
	out.WriteString("## Prompts\n\n")
	out.WriteString("A prompt is a request the person picks in the client, often as a slash command, with the content it is about attached. Its arguments complete like a template's, and it is served while every tool whose answer it attaches is exposed.\n\n")
	out.WriteString("| Prompt | Arguments | Served while | What it does |\n|---|---|---|---|\n")
	for _, spec := range mcp.AllPromptSpecs() {
		arguments := make([]string, len(spec.Prompt.Arguments))
		for index, argument := range spec.Prompt.Arguments {
			arguments[index] = "`" + argument.Name + "`"
		}
		tools := make([]string, len(spec.Tools))
		for index, tool := range spec.Tools {
			tools[index] = "`" + tool + "`"
		}
		fmt.Fprintf(out, "| `%s` | %s | %s | %s |\n", spec.Prompt.Name, strings.Join(arguments, ", "), strings.Join(tools, ", "), collapse(spec.Prompt.Description))
	}
	out.WriteString("\n")
}

// collapse folds a description onto one line. Several are written as multi-line
// Go string concatenations, and a newline inside a Markdown table cell ends the
// row early.
func collapse(text string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(text, "|", `\|`)), " ")
}
