package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/outline"
)

// writeDescriptionText prints what --describe answers for a person (ADR-097):
// an outline of the document a command writes, or a group's commands with what
// --dry-run does for each. --json and --yaml print the schemas themselves.
func writeDescriptionText(out io.Writer, cmd *cobra.Command, description Description) error {
	if !cmd.Runnable() {
		return writeCatalogueText(out, cmd, description)
	}

	path := commandPathWithoutRoot(cmd)
	var text strings.Builder

	data, reason, written := dataContract(path)
	if !written {
		text.WriteString(outline.Paragraph(fmt.Sprintf("bb %s --json %s.", path, strings.TrimPrefix(reason, "this command "))))
	} else {
		text.WriteString(outline.Paragraph(fmt.Sprintf("bb %s --json prints data, or error when it fails:", path)) + "\n")
		if err := outline.Write(&text,
			outline.Member{Name: "data", Schema: data},
			outline.Member{Name: "meta", Schema: openMetaSchema()},
		); err != nil {
			return err
		}
		if reason != "" {
			text.WriteString("\n" + outline.Paragraph(fmt.Sprintf("data is left open: %s.", reason)))
		}
		text.WriteString("\n" + outline.Paragraph("? marks a field that can be absent. --json or --yaml prints the JSON Schema, with each field's full description."))
	}

	if line := dryRunHelpLine(cmd); line != "" {
		behaviour, takes := dryRunBehaviourOf(path)
		switch {
		case !takes:
			line = "--dry-run: " + line
		case behaviour.Behaviour == dryRunRuns:
			line = "--dry-run: " + line + ". Under --json its data is in preview.data: see bb help dry-run."
		default:
			line = "--dry-run: " + line + ". It prints preview instead of data: see bb help dry-run."
		}
		text.WriteString("\n" + outline.Paragraph(line))
	}

	_, err := io.WriteString(out, text.String())

	return err
}

// writeCatalogueText prints a group's commands, or all of bb's, with what
// --dry-run does for each, in aligned columns.
func writeCatalogueText(out io.Writer, group *cobra.Command, description Description) error {
	var text strings.Builder

	scope := "bb"
	if path := commandPathWithoutRoot(group); path != "" {
		scope = "bb " + path
	}
	fmt.Fprintf(&text, "Commands in %s, and what --dry-run does for each:\n\n", scope)

	paths := sortedCommands(description.Commands)
	width := 0
	for _, path := range paths {
		width = max(width, len(path))
	}

	for _, path := range paths {
		answer := "does not take --dry-run"
		if dryRun := description.Commands[path].DryRun; dryRun != nil {
			answer = dryRun.Behaviour
			// Only a preview that verifies has a tier worth naming: a read's
			// answer is Bitbucket's own, and a prediction is predicted.
			if dryRun.Behaviour == dryRunVerifies {
				answer += fmt.Sprintf(" (%s)", dryRun.Tier)
			}
		}
		fmt.Fprintf(&text, "%-*s  %s\n", width, path, answer)
	}

	text.WriteString("\nbb <command> --describe says what one of them returns.\n")

	_, err := io.WriteString(out, text.String())

	return err
}

// openMetaSchema is meta as the document carries it: derived from its type,
// and open, since it may gain fields in a minor release.
func openMetaSchema() *jsonschema.Schema {
	meta := metaDeclaration.Schema().CloneSchemas()
	meta.AdditionalProperties = nil

	return meta
}
