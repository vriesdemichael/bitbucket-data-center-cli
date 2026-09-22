package cli

import (
	"bytes"
	"io"
	"strings"

	"github.com/spf13/cobra"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// brokenEmptyCompletion is what Cobra's PowerShell script does when a
// completion has nothing to offer and has asked the shell not to fall back to
// file names.
//
// PowerShell turns a returned string into a CompletionResult, and rejects an
// empty one: "Cannot process argument because the value of argument
// completionText is null". The exception reaches the prompt on every tab press
// with no candidates -- which, for a CLI whose values come from a server, is
// most of them: an empty repository, a filter that matched nothing, an
// instance that is not reachable.
const brokenEmptyCompletion = `        if ($Values.Length -eq 0) {
            # Just print an empty string here so the
            # shell does not start to complete paths.
            # We cannot use CompletionResult here because
            # it does not accept an empty string as argument.
            ""
            return
        }`

// workingEmptyCompletion completes the word with itself.
//
// Nothing changes on the line, and PowerShell does not go looking for files
// because it did get a completion. With nothing typed yet there is no word to
// echo and no value PowerShell accepts -- a single space would be inserted
// into the line -- so that one case is left to the shell's own behaviour,
// which lists the directory. Wrong, and better than an exception.
const workingEmptyCompletion = `        if ($Values.Length -eq 0) {
            # Cobra prints an empty string here, which PowerShell rejects:
            # "the value of argument completionText is null", on the prompt,
            # for every tab press that has nothing to offer. Completing the
            # word with itself leaves the line as it was and still keeps the
            # shell from listing the directory.
            if ($WordToComplete -ne '') {
                [System.Management.Automation.CompletionResult]::new($WordToComplete, $WordToComplete, 'ParameterValue', ' ')
            }
            return
        }`

// fixPowerShellCompletion repairs the script Cobra generates, in place.
//
// Cobra owns this script and bb does not want to own a copy of it: a fork
// would stop tracking the improvements that arrive with the dependency, and
// there is one line wrong rather than a design to disagree with. So the
// generated text is rewritten on its way out, and the rewrite asserts that it
// found what it was looking for -- when the upstream script changes, `bb
// completion powershell` fails loudly rather than emitting a script that
// quietly throws at every prompt.
func fixPowerShellCompletion(root *cobra.Command) {
	root.InitDefaultCompletionCmd()

	completionCmd, _, err := root.Find([]string{"completion"})
	if err != nil || completionCmd == nil {
		return
	}

	for _, shell := range completionCmd.Commands() {
		generate := generatorFor(shell.Name())
		if generate == nil {
			continue
		}

		patch := patchFor(shell.Name())

		// Cobra's own RunE writes to a writer it captured when it built these
		// commands, which is not the one the caller sets afterwards. Keeping
		// its Use, Short, Long and flags and replacing only the body fixes
		// that as well: what a test captures is what the shell would get.
		shell.RunE = func(cmd *cobra.Command, args []string) error {
			withDescriptions := true
			if flag := cmd.Flags().Lookup("no-descriptions"); flag != nil {
				withDescriptions = flag.Value.String() != "true"
			}

			script := &bytes.Buffer{}
			if err := generate(cmd.Root(), script, withDescriptions); err != nil {
				return err
			}

			text := script.String()
			if patch != nil {
				fixed, err := patch(text)
				if err != nil {
					return err
				}

				text = fixed
			}

			_, err := cmd.OutOrStdout().Write([]byte(text))

			return err
		}
	}
}

// generatorFor is Cobra's generator for a shell, behind one signature.
func generatorFor(shell string) func(*cobra.Command, io.Writer, bool) error {
	switch shell {
	case "bash":
		return func(root *cobra.Command, writer io.Writer, withDescriptions bool) error {
			return root.GenBashCompletionV2(writer, withDescriptions)
		}
	case "zsh":
		return func(root *cobra.Command, writer io.Writer, withDescriptions bool) error {
			if !withDescriptions {
				return root.GenZshCompletionNoDesc(writer)
			}

			return root.GenZshCompletion(writer)
		}
	case "fish":
		return func(root *cobra.Command, writer io.Writer, withDescriptions bool) error {
			return root.GenFishCompletion(writer, withDescriptions)
		}
	case "powershell":
		return func(root *cobra.Command, writer io.Writer, withDescriptions bool) error {
			if !withDescriptions {
				return root.GenPowerShellCompletion(writer)
			}

			return root.GenPowerShellCompletionWithDesc(writer)
		}
	default:
		return nil
	}
}

func patchFor(shell string) func(string) (string, error) {
	if shell == "powershell" {
		return withWorkingEmptyCompletion
	}

	return nil
}

func withWorkingEmptyCompletion(script string) (string, error) {
	normalized := strings.ReplaceAll(script, "\r\n", "\n")
	if !strings.Contains(normalized, brokenEmptyCompletion) {
		return "", apperrors.New(
			apperrors.KindInternal,
			"the PowerShell completion script Cobra generates no longer contains the empty-completion branch bb corrects; "+
				"check internal/cli/completionpowershell.go against the upstream script",
			nil,
		)
	}

	return strings.Replace(normalized, brokenEmptyCompletion, workingEmptyCompletion, 1), nil
}
