package cli

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
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
	if len(unconsumed) == 0 || helpWasAskedFor(cmd, unconsumed) {
		return nil
	}

	return apperrors.New(
		apperrors.KindValidation,
		fmt.Sprintf("unknown command %q for %q", unconsumed[0], cmd.CommandPath()),
		nil,
	)
}

// helpWasAskedFor reports whether the argument a group could not match was a
// request for its help rather than a misspelled subcommand.
//
// Two spellings land here. `bb pr help` is Cobra's own spelling of `bb help
// pr`, and it exited 0 until groups started reporting what they could not
// match; `bb pr bogus --help` asks to be told what exists, which the help it
// prints answers. Reporting either as a usage failure -- exit 2, the help on
// stderr, an error envelope under --json -- makes asking for help something bb
// refuses to do.
func helpWasAskedFor(cmd *cobra.Command, unconsumed []string) bool {
	if requested, err := cmd.Flags().GetBool("help"); err == nil && requested {
		return true
	}

	return len(unconsumed) > 0 && unconsumed[0] == "help"
}

// ghFieldList matches what gh takes after --json: field names separated by
// commas, such as number,title,url.
var ghFieldList = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*(,[A-Za-z][A-Za-z0-9]*)*$`)

// HintGHFieldList adds to a validation failure that a gh-style field list after
// --json caused, saying what bb does instead.
//
// gh's --json takes the fields to print; bb's takes nothing and prints the
// whole document (ADR-095). So `bb pr list --json number,title` parses the list
// as an argument, and the command rejects it as an unknown command or an
// argument too many: true, and no help to somebody typing what gh taught them.
//
// Only a failure the list caused gets the hint: one naming it, or one about
// the number of arguments. A list that happened to be a valid argument, or a
// failure about something else, is left alone.
func HintGHFieldList(err error, args []string, command *cobra.Command) error {
	if !apperrors.IsKind(err, apperrors.KindValidation) {
		return err
	}

	fields, without, asValue := fieldListAfterJSON(args)
	if fields == "" {
		return err
	}

	// A word after --json is a field list only if the command took it as an
	// argument. In `bb --json pr get`, pr names the command, and the failure
	// is the missing pull request id, which no hint about --json helps with.
	if !asValue && (command == nil || !slices.Contains(command.Flags().Args(), fields)) {
		return err
	}

	message := apperrors.MessageOf(err)
	if !strings.Contains(message, fields) && !strings.Contains(message, "argument") {
		return err
	}

	// For a command, the example is the caller's own invocation with the list
	// taken out, so it is one they can run as it stands. A group has nothing to
	// run, so it gets the rule without an example.
	hint := "bb's --json takes no field list: it prints the whole document, so pick fields from .data with jq, and see them with --describe"
	if command != nil && command.Runnable() {
		hint = fmt.Sprintf("bb's --json takes no field list: it prints the whole document, so pick fields with jq, as in bb %s | jq '.data', and see them with bb %s --describe",
			strings.Join(without, " "), commandPathWithoutRoot(command))
	}

	return apperrors.New(apperrors.KindValidation, message+". "+hint, nil)
}

// fieldListAfterJSON returns a gh-style field list given to --json, as the next
// argument or as its value, the arguments without it, and whether it was the
// value; or "" when there is none.
func fieldListAfterJSON(args []string) (string, []string, bool) {
	for index, arg := range args {
		if arg == "--" {
			return "", nil, false
		}

		if value, ok := strings.CutPrefix(arg, "--json="); ok {
			if _, notBool := strconv.ParseBool(value); notBool != nil && ghFieldList.MatchString(value) {
				without := append(append(append([]string(nil), args[:index]...), "--json"), args[index+1:]...)
				return value, without, true
			}
			continue
		}

		if arg == "--json" && index+1 < len(args) && ghFieldList.MatchString(args[index+1]) {
			without := append(append([]string(nil), args[:index+1]...), args[index+2:]...)
			return args[index+1], without, false
		}
	}

	return "", nil, false
}
