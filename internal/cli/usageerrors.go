package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// cobraUsageErrorMarkers are the message fragments Cobra uses for the usage
// errors it raises itself.
//
// Cobra returns plain fmt.Errorf values for these, with no sentinel or type to
// match on, so recognising them means matching text. That coupling is pinned by
// TestClassifyUsageErrorMatchesCobrasRealMessages, which drives the real
// conditions through a command tree and fails if a Cobra upgrade changes the
// wording — a silent reclassification back to internal would be far worse than
// a failing build.
//
// pflag's errors need none of this: since v1.0.10 they are typed, and are
// matched with errors.As below.
var cobraUsageErrorMarkers = []string{
	"unknown command",
	"unknown shorthand flag",
	"arg(s), received",
	"arg(s), only received",
	"invalid argument",
	// Cobra ValidateRequiredFlags. The most common usage error of all, and
	// the one this list was missing: sixteen commands reported a forgotten
	// flag as a defect in bb (#475).
	"required flag(s)",
	// Flag-group violations from MarkFlagsMutuallyExclusive and friends, e.g.
	// "if any flags in the group [all listPaging.ServiceLimit()] are set none of the others can be".
	"flags in the group",
}

// ClassifyUsageError maps an error caused by a malformed invocation onto the
// validation kind.
//
// An unknown flag is the caller's mistake, not a CLI defect, but Cobra and
// pflag raise these outside the taxonomy, so KindOf fell through to internal
// and the exit code to 1. A consumer branching on kind would then retry or
// escalate a failure it should have fixed in its own invocation — which is the
// opposite of what ADR-011 exists for.
//
// Errors already carrying a kind are returned untouched, so a command's own
// classification always wins.
func ClassifyUsageError(err error) error {
	if err == nil {
		return nil
	}

	var appError *apperrors.AppError
	if errors.As(err, &appError) {
		return err
	}

	if !isUsageError(err) {
		return err
	}

	return apperrors.New(apperrors.KindValidation, err.Error(), nil)
}

func isUsageError(err error) bool {
	var (
		notExist      *pflag.NotExistError
		valueRequired *pflag.ValueRequiredError
		invalidValue  *pflag.InvalidValueError
		invalidSyntax *pflag.InvalidSyntaxError
	)

	if errors.As(err, &notExist) ||
		errors.As(err, &valueRequired) ||
		errors.As(err, &invalidValue) ||
		errors.As(err, &invalidSyntax) {
		return true
	}

	message := err.Error()
	for _, marker := range cobraUsageErrorMarkers {
		if strings.Contains(message, marker) {
			return true
		}
	}

	return false
}

// UnknownSubcommandError reports the invocation Cobra answers with help and a
// zero exit: a command group handed something that is not one of its
// subcommands.
//
// Cobra returns flag.ErrHelp for any command with no RunE before it validates
// arguments, and ExecuteC turns that into "print help, return nil". So `bb pr
// bogus` matched no subcommand, fell through to the group's help and exited 0 --
// in 64 of 65 groups, and under --json too, where a caller reading stdout got
// two kilobytes of prose instead of an envelope. The v4 release notes promised
// exit 2 for invalid arguments; this is the part that never shipped.
//
// Setting Args on the group does not help, because the ErrHelp bail happens
// first. Giving every group a RunE would work and would make 66 groups Runnable,
// which is the predicate the dry-run classification, the result declarations and
// command reach all use to decide what must be registered.
//
// So the check runs after Execute instead. Cobra parses the group's flags before
// it bails, so Flags().Args() holds exactly the positional arguments it could not
// consume -- which is why this is accurate where inspecting os.Args would not be:
// it cannot mistake a flag's value for a subcommand.
func UnknownSubcommandError(root *cobra.Command, args []string) error {
	if root == nil {
		return nil
	}

	cmd, _, err := root.Find(args)
	if err != nil || cmd == nil || cmd.Runnable() || !cmd.HasSubCommands() {
		return nil
	}

	unconsumed := cmd.Flags().Args()
	if len(unconsumed) == 0 {
		return nil
	}

	return apperrors.New(
		apperrors.KindValidation,
		fmt.Sprintf("unknown command %q for %q", unconsumed[0], cmd.CommandPath()),
		nil,
	)
}
