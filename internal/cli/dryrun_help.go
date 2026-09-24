package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/dryrunpreview"
)

// dryRunHelpOverrides are commands whose --dry-run does something no
// classification describes, so their help says it in their own words.
var dryRunHelpOverrides = map[string]string{
	"update": "Reports the release it would install and whether it verifies, without installing it",
	"api":    "Sends a GET or HEAD as usual, since it only reads; any other request is shown as what it would send, without sending it (predicted)",
}

// dryRunHelpLine says what --dry-run does for one command, from its
// classification and its tier (ADR-096), or "" for a command group.
//
// The flag is global, so its own usage can only say what it does in general;
// what a person deciding whether to trust it needs is what it does here --
// whether anything is checked, and against what.
func dryRunHelpLine(cmd *cobra.Command) string {
	if cmd == nil || !cmd.Runnable() {
		return ""
	}

	path := dryRunCommandPath(cmd)
	if line, ok := dryRunHelpOverrides[path]; ok {
		return line
	}

	switch classifyCommand(path) {
	case classificationMutating:
		profile := dryRunProfiles[path]
		tier, _ := DeclaredDryRunTier(path)
		if !profile.Stateful {
			return fmt.Sprintf("Shows the change it would make, without making it; nothing is checked first (%s)", tier)
		}

		switch tier {
		case dryrunpreview.TierServerValidated:
			return fmt.Sprintf("Asks Bitbucket whether this would go through, without changing anything (%s)", tier)
		case dryrunpreview.TierPreconditionsChecked:
			return fmt.Sprintf("Checks your permission and the current state this depends on, without changing anything (%s)", tier)
		default:
			return fmt.Sprintf("Predicts the outcome from what it can read, without changing anything (%s)", tier)
		}
	case classificationReadOnly, classificationLocal:
		return "Runs as usual: this command changes nothing, so there is nothing to hold back"
	case classificationLocalMutating:
		profile := clientLocalMutatingCommands[path]
		return fmt.Sprintf("Shows that it would %s, without doing it; nothing is checked first (%s)", profile.Change, dryrunpreview.TierPredicted)
	case classificationWithoutDryRun:
		return "Not accepted: " + strings.TrimPrefix(firstSentence(commandsWithoutDryRun[path]), "bb "+path+" does not take --dry-run: ")
	default:
		return ""
	}
}

// firstSentence is text up to its first full stop.
func firstSentence(text string) string {
	if index := strings.Index(text, ". "); index >= 0 {
		return text[:index+1]
	}

	return text
}

// Registered once, at package load: Cobra keeps template functions in one
// process-wide map, and adding to it from every root command raced in the
// tests that build command trees side by side.
func init() {
	cobra.AddTemplateFunc("dryRunHelp", dryRunHelpLine)
}

// installDryRunHelp gives every command's help a "Dry run:" section, and adds
// the bb help dry-run topic.
//
// The section goes in the usage template rather than in the flag's own usage:
// the flag is global, so Cobra prints it among the flags every command shares,
// and the command reference prints that shared block once for the whole tool.
func installDryRunHelp(root *cobra.Command) {
	const inherited = "{{if .HasAvailableInheritedFlags}}"
	template := root.UsageTemplate()
	if strings.Contains(template, inherited) {
		template = strings.Replace(template, inherited,
			"{{with dryRunHelp .}}\n\nDry run:\n  {{.}}{{end}}"+inherited, 1)
		root.SetUsageTemplate(template)
	}

	root.AddCommand(&cobra.Command{
		Use:   "dry-run",
		Short: "What --dry-run answers, and how far to trust it",
		Long:  dryRunTopic,
	})
}

const dryRunTopic = `--dry-run asks what the real run would do, without doing it. Each command's
help says, under "Dry run", what it checks.

A command that changes something answers with a verdict, one effect per
change it would make:

  would-apply   the change would be made
  no-op         nothing needs to change
  would-fail    the run would be refused; the reasons say why

How far to trust a verdict is its tier, the weakest check behind it:

  server-validated        Bitbucket answered the exact question
  preconditions-checked   bb fetched your permission and the current state
                          and checked them
  predicted               bb predicted it from partial state, or checked
                          nothing

A command that only reads runs as usual, since reading changes nothing.

Exit codes

In text, a dry run exits with the code the real run would: 0 when it would
go through, and the failure's code when it would fail. So
"bb pr merge 42 --dry-run && bb pr merge 42" stops at the check.

Under --json or --yaml it exits 0 whenever it reached a verdict, which is in
the document, and non-zero only when it could not: 10 when Bitbucket did not
answer, 12 when it was interrupted, 1 for a bug in bb.

The document

Under --json the answer is the preview member:

  {"preview": {"tier": "...",
               "effects": [{"action", "target", "outcome", "reasons"}],
               "error": {...}},
   "meta": {"command": "...", "bbVersion": "..."}}

error is present exactly when the run would fail, and is what it would fail
with. A command that only reads puts what it returned in preview.data.`
