package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/usage"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// nameTheMissingArgument replaces Cobra's arity message with one that says
// what was expected.
//
// Cobra answers a missing positional with
//
//	Error: accepts 1 arg(s), received 0
//
// which names neither the command nor the thing it wanted, and 151 of 163
// commands that take a positional answered that way -- 31 of them destructive. ADR-073 requires
// a missing required value to fail with a message naming what would have
// supplied it (#587).
//
// The placeholders are already in Use, because that is what the help text
// prints, so the message is built from the command's own signature rather
// than from a second list that could disagree with it.
func nameTheMissingArgument(root *cobra.Command) {
	var visit func(*cobra.Command)
	visit = func(cmd *cobra.Command) {
		placeholders := positionalPlaceholders(cmd.Use)
		if cmd.Runnable() && cmd.Args != nil && len(placeholders) > 0 {
			original := cmd.Args
			command := cmd
			cmd.Args = func(c *cobra.Command, args []string) error {
				if err := original(c, args); err != nil {
					return apperrors.New(
						apperrors.KindValidation,
						fmt.Sprintf(
							"%s takes %s; %s given. See 'bb %s --help'",
							dryRunCommandPath(command),
							strings.Join(placeholders, " "),
							countOfArguments(len(args)),
							dryRunCommandPath(command),
						),
						// Not wrapped: Cobra's own text is what this replaces, and
						// AppError appends a cause, so keeping it would print the
						// arity message this exists to hide.
						nil,
					)
				}

				return nil
			}
		}

		for _, child := range cmd.Commands() {
			visit(child)
		}
	}

	visit(root)
}

// positionalPlaceholders are the <required> and [optional] words in a Use
// line, which is where the command already declares them for its help text.
//
// The parsing lives in internal/cli/usage because shell completion reads the
// same declaration to learn what each slot accepts, and a second copy of this
// is exactly the disagreement the Use line is being used to avoid.
func positionalPlaceholders(use string) []string {
	return usage.Placeholders(use)
}

func countOfArguments(count int) string {
	if count == 1 {
		return "1 argument"
	}

	return fmt.Sprintf("%d arguments", count)
}
