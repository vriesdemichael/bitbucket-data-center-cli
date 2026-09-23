package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/enumflag"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/completionsetup"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// CompletionSetup is what `bb completion install` and `remove` report.
type CompletionSetup struct {
	Shell   string             `json:"shell" jsonschema:"The shell set up or taken out."`
	Scope   string             `json:"scope" jsonschema:"user for the person running bb, all-users for everyone on the machine."`
	Targets []CompletionTarget `json:"targets" jsonschema:"Each place the setup went. PowerShell has one for every edition installed."`
}

// CompletionTarget is one place a setup went.
type CompletionTarget struct {
	Edition string `json:"edition,omitempty" jsonschema:"Which PowerShell: PowerShell 7 or Windows PowerShell 5.1. Absent for the other shells."`
	Path    string `json:"path,omitempty" jsonschema:"The file written, changed or removed. Absent when the shell could not say where its profile is."`
	Status  string `json:"status" jsonschema:"installed, updated, unchanged, removed, not_found, or blocked when the shell would not run it."`
	Note    string `json:"note,omitempty" jsonschema:"What the status does not say: why a target is blocked, what the setup replaced, or that the script there is a package's, which is left to the package."`
}

func init() {
	shells := make([]string, 0, len(completionsetup.Shells))
	for _, shell := range completionsetup.Shells {
		shells = append(shells, string(shell))
	}
	scopes := []string{string(completionsetup.CurrentUser), string(completionsetup.AllUsers)}

	result.Declare("completion install", result.For[CompletionSetup](map[string][]string{
		"shell":          shells,
		"scope":          scopes,
		"targets.status": {string(completionsetup.Installed), string(completionsetup.Updated), string(completionsetup.Unchanged), string(completionsetup.Blocked)},
	}))
	result.Declare("completion remove", result.For[CompletionSetup](map[string][]string{
		"shell":          shells,
		"scope":          scopes,
		"targets.status": {string(completionsetup.Removed), string(completionsetup.NotFound)},
	}))
}

// addCompletionSetup puts `bb completion install` and `remove` beside the
// script generators Cobra adds.
//
// Called before the walks over the tree, which Cobra's own command would
// otherwise miss: it is added when the tree is executed, after they have run.
// These two write files, so they need what the walks give every such command
// -- the --dry-run preview, and remove's --yes.
func addCompletionSetup(root *cobra.Command, options *rootOptions) {
	root.InitDefaultCompletionCmd()

	completionCmd, _, err := root.Find([]string{"completion"})
	if err != nil || completionCmd == nil || completionCmd == root {
		return
	}

	completionCmd.AddCommand(
		newCompletionSetupCommand(options, completionsetup.Install, "install",
			"Set completion up so every new shell completes bb",
			`Set shell completion up, so every new shell completes bb without anything
added by hand.

What it writes loads the script from bb each time, so an upgrade of bb needs
nothing done here:

  bash        ~/.local/share/bash-completion/completions/bb, which
              bash-completion reads the first time bb is completed
  zsh         a marked block at the end of ~/.zshrc
  fish        ~/.config/fish/completions/bb.fish
  powershell  a marked block in the all-hosts profile of each PowerShell
              installed: Windows PowerShell 5.1 and PowerShell 7 alike

With --all-users it is set up for everyone on this machine, which needs an
administrator:

  bash        /usr/local/share/bash-completion/completions/bb
  zsh         /usr/local/share/zsh/site-functions/_bb, when zsh reads it
  fish        completions/bb.fish in fish's configuration directory
  powershell  the all-users profile of each PowerShell

On Windows, only PowerShell has a place every user's shell reads. Homebrew
and the .deb and .rpm packages install completion for bash, zsh and fish
themselves.

Without --shell it sets up the shell bb was started from. Running it again
changes nothing, and bb completion remove takes it out.

Windows PowerShell 5.1 runs no profile script under its default execution
policy, Restricted. bb then says so rather than write a profile that would not
run.`),
		newCompletionSetupCommand(options, completionsetup.Remove, "remove",
			"Take out what bb completion install set up",
			`Take out what bb completion install set up: its file, or its block in a file
it shares with you. Nothing else in a shared file is touched.

A script saved from bb completion <shell>, as the documentation once said to
do, is bb's too and is removed with it. A file bb did not write is left alone,
and so is a script a package linked where --all-users writes.`),
	)
}

func newCompletionSetupCommand(
	options *rootOptions,
	change func(completionsetup.Target) (completionsetup.Outcome, error),
	verb, short, long string,
) *cobra.Command {
	var shellName string
	var allUsers bool

	shells := make([]string, 0, len(completionsetup.Shells))
	for _, shell := range completionsetup.Shells {
		shells = append(shells, string(shell))
	}

	cmd := &cobra.Command{
		Use:   verb,
		Short: short,
		Long:  long,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			system := completionsetup.Real()

			shell, err := chooseShell(system, shellName)
			if err != nil {
				return err
			}

			scope := completionsetup.CurrentUser
			if allUsers {
				scope = completionsetup.AllUsers
			}

			targets, err := completionsetup.Targets(cmd.Context(), system, shell, scope)
			if err != nil {
				return err
			}

			outcomes := make([]completionsetup.Outcome, 0, len(targets))
			for _, target := range targets {
				outcome, err := change(target)
				if err != nil {
					return err
				}
				outcomes = append(outcomes, outcome)
			}

			if failure := nothingSetUp(outcomes); failure != nil {
				return failure
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), completionSetupFrom(shell, scope, outcomes))
			}

			writeCompletionSetup(cmd.OutOrStdout(), outcomes)

			return nil
		},
	}

	enumflag.Register(cmd.Flags(), &shellName, "shell", "", shells, "Shell to "+verb+" completion for (default: the shell bb was started from)")
	cmd.Flags().BoolVar(&allUsers, "all-users", false, "For everyone on this machine rather than you; needs an administrator")

	return cmd
}

// chooseShell is the shell --shell names, or else the one bb was started from.
func chooseShell(system completionsetup.System, name string) (completionsetup.Shell, error) {
	if strings.TrimSpace(name) != "" {
		return completionsetup.ParseShell(name)
	}

	return completionsetup.DetectShell(system)
}

// nothingSetUp fails an install that could not set completion up anywhere,
// because every shell asked would not run it -- Windows PowerShell 5.1 alone,
// under Restricted, is the common case. Some set up and some blocked is a
// success that says which were blocked.
func nothingSetUp(outcomes []completionsetup.Outcome) error {
	reasons := []string{}
	for _, outcome := range outcomes {
		if outcome.Status != completionsetup.Blocked {
			return nil
		}
		reasons = append(reasons, labelOf(outcome.Target)+": "+outcome.Note)
	}
	if len(reasons) == 0 {
		return nil
	}

	return apperrors.New(apperrors.KindPermanent, "completion was not set up: "+strings.Join(reasons, "; ")+unblockAdvice(outcomes), nil)
}

func completionSetupFrom(shell completionsetup.Shell, scope completionsetup.Scope, outcomes []completionsetup.Outcome) CompletionSetup {
	report := CompletionSetup{Shell: string(shell), Scope: string(scope), Targets: make([]CompletionTarget, 0, len(outcomes))}
	for _, outcome := range outcomes {
		report.Targets = append(report.Targets, CompletionTarget{
			Edition: outcome.Target.Edition,
			Path:    outcome.Target.Path,
			Status:  string(outcome.Status),
			Note:    outcome.Note,
		})
	}

	return report
}

func writeCompletionSetup(w io.Writer, outcomes []completionsetup.Outcome) {
	changed := false

	for _, outcome := range outcomes {
		label := labelOf(outcome.Target)
		path := outcome.Target.Path

		switch outcome.Status {
		case completionsetup.Installed:
			fmt.Fprintf(w, "%s: set up in %s\n", label, path)
			changed = true
		case completionsetup.Updated:
			fmt.Fprintf(w, "%s: updated in %s\n", label, path)
			changed = true
		case completionsetup.Unchanged:
			fmt.Fprintf(w, "%s: already set up in %s\n", label, path)
		case completionsetup.Blocked:
			fmt.Fprintf(w, "%s: not set up, because %s\n", label, outcome.Note)
		case completionsetup.Removed:
			fmt.Fprintf(w, "%s: removed from %s\n", label, path)
		case completionsetup.NotFound:
			fmt.Fprintf(w, "%s: nothing to remove in %s\n", label, path)
		}

		if outcome.Note != "" && outcome.Status != completionsetup.Blocked {
			fmt.Fprintf(w, "  %s\n", outcome.Note)
		}
	}

	if advice := unblockAdvice(outcomes); advice != "" {
		fmt.Fprintln(w, strings.TrimPrefix(advice, " "))
	}
	if changed {
		fmt.Fprintln(w, "Open a new shell to use it.")
	}
}

// unblockAdvice says how to let a PowerShell run its profile, when one would
// not. bb does not change the execution policy itself: it is a security
// setting, and not bb's to relax.
func unblockAdvice(outcomes []completionsetup.Outcome) string {
	for _, outcome := range outcomes {
		if outcome.Status == completionsetup.Blocked && strings.Contains(outcome.Note, "execution policy") {
			return " To let it run a profile, run Set-ExecutionPolicy -Scope CurrentUser RemoteSigned in that PowerShell, then this again."
		}
	}

	return ""
}

func labelOf(target completionsetup.Target) string {
	if target.Edition != "" {
		return target.Edition
	}

	return string(target.Shell)
}
