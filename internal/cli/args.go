package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
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
// A bracketed group is one placeholder however many spaces it holds. browse
// declares [<number> | <path> | <commit-sha>], one argument that is any of
// three, and splitting it on spaces dropped the bars that say so.
func positionalPlaceholders(use string) []string {
	var words []string
	var word strings.Builder
	depth := 0

	for _, character := range use {
		switch {
		case character == ' ' && depth == 0:
			if word.Len() > 0 {
				words = append(words, word.String())
				word.Reset()
			}

			continue
		case character == '[' || character == '<':
			depth++
		case (character == ']' || character == '>') && depth > 0:
			depth--
		}
		word.WriteRune(character)
	}
	if word.Len() > 0 {
		words = append(words, word.String())
	}

	if len(words) <= 1 {
		return nil
	}

	var placeholders []string
	for _, candidate := range words[1:] {
		if strings.HasPrefix(candidate, "<") || strings.HasPrefix(candidate, "[") {
			placeholders = append(placeholders, candidate)
		}
	}

	return placeholders
}

func countOfArguments(count int) string {
	if count == 1 {
		return "1 argument"
	}

	return fmt.Sprintf("%d arguments", count)
}
