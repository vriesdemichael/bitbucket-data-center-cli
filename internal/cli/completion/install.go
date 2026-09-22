package completion

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/enumflag"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/usage"
)

// Install gives every command in the tree the completions its declaration
// asks for.
//
// It runs once, over the finished tree, for the same reason nameTheMissingArgument
// does: what a command accepts is readable from the command, and a pass that
// reads it cannot be forgotten by the next command's author the way a call at
// each definition site can.
func Install(root *cobra.Command, dependencies Dependencies) {
	if root == nil {
		return
	}

	// Without this, a slot with nothing to offer falls through to the shell's
	// own file completion: `bb pr merge <tab>` listed the working directory.
	// Anything that really takes a path asks for it back, below.
	root.CompletionOptions.SetDefaultShellCompDirective(cobra.ShellCompDirectiveNoFileComp)

	walk(root, func(command *cobra.Command) {
		if commandOwnsItsCompletion(command) {
			return
		}

		installPositionals(command, dependencies)
		installFlags(command, dependencies)
	})
}

func walk(command *cobra.Command, visit func(*cobra.Command)) {
	visit(command)

	for _, child := range command.Commands() {
		walk(child, visit)
	}
}

func installPositionals(command *cobra.Command, dependencies Dependencies) {
	placeholders := usage.Placeholders(command.Use)
	if len(placeholders) == 0 || command.ValidArgsFunction != nil {
		return
	}

	kinds := positionalKinds(command)
	repeating := usage.Variadic(placeholders[len(placeholders)-1])

	command.ValidArgsFunction = func(
		invoked *cobra.Command,
		args []string,
		toComplete string,
	) ([]cobra.Completion, cobra.ShellCompDirective) {
		// Everything after -- belongs to another program: bb repo clone hands
		// it to git, and bb has nothing to say about git's flags.
		if dash := invoked.ArgsLenAtDash(); dash >= 0 && len(args) >= dash {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		position := len(args)
		if position >= len(kinds) {
			if !repeating {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}

			position = len(kinds) - 1
		}

		return answer(
			invoked.Context(),
			dependencies,
			slot{kind: kinds[position], position: position},
			invoked,
			bind(kinds, args),
			args,
			toComplete,
		)
	}
}

// bind records what the arguments already typed are, by kind, so a slot that
// hangs off an earlier one -- a comment inside a pull request, a report on a
// commit -- can find it without knowing which position it was in.
func bind(kinds []Kind, args []string) map[Kind]string {
	bindings := make(map[Kind]string, len(args))

	for index, value := range args {
		if index >= len(kinds) {
			break
		}
		if kinds[index] == KindFree || strings.TrimSpace(value) == "" {
			continue
		}

		bindings[kinds[index]] = strings.TrimSpace(value)
	}

	return bindings
}

func installFlags(command *cobra.Command, dependencies Dependencies) {
	path := commandPath(command)

	register := func(flag *pflag.Flag) {
		kind, _ := DeclaredFlag(path, flag)

		switch kind {
		case KindEnum:
			// The flag carries the values it accepts, and validates against
			// the same list, so it answers for itself.
			allowed, _ := enumflag.Allowed(flag)
			registerFlag(command, flag, fixed(allowed, multiValued(flag)))
		case KindFree, "":
			return
		case KindLocalFile:
			registerFlag(command, flag, func(*cobra.Command, []string, string) ([]cobra.Completion, cobra.ShellCompDirective) {
				return nil, cobra.ShellCompDirectiveDefault
			})
		case KindLocalDir:
			registerFlag(command, flag, func(*cobra.Command, []string, string) ([]cobra.Completion, cobra.ShellCompDirective) {
				return nil, cobra.ShellCompDirectiveFilterDirs
			})
		default:
			target := slot{kind: kind, flagName: flag.Name, position: -1, multi: multiValued(flag)}
			registerFlag(command, flag, func(
				invoked *cobra.Command,
				args []string,
				toComplete string,
			) ([]cobra.Completion, cobra.ShellCompDirective) {
				return answer(
					invoked.Context(),
					dependencies,
					target,
					invoked,
					bind(positionalKinds(invoked), args),
					args,
					toComplete,
				)
			})
		}
	}

	// Each flag is registered where it is declared, which is once: a
	// persistent --repo on bb pr covers every command under it, and Cobra
	// refuses a second registration of the same flag.
	//
	// Flags() and PersistentFlags() rather than LocalNonPersistentFlags(),
	// which looks equivalent and is not: it calls mergePersistentFlags, which
	// copies every parent's persistent flags into this command's own set.
	// Cobra does that at execute time anyway, but doing it while the tree is
	// being built leaves --log-level looking like a flag declared on all 309
	// commands -- which is what TestNoFlagEnumeratesValuesWithoutEnforcingThem
	// then reported, 600 times.
	command.PersistentFlags().VisitAll(register)
	command.Flags().VisitAll(register)
}

func registerFlag(command *cobra.Command, flag *pflag.Flag, completion cobra.CompletionFunc) {
	// The error cases are a flag that does not exist and one already
	// registered, neither of which this pass can produce: it registers the
	// flags it was handed, once each.
	_ = command.RegisterFlagCompletionFunc(flag.Name, completion)
}

// fixed answers from a list known without asking anything -- an enum's values,
// a vocabulary Bitbucket fixes.
func fixed(values []string, multi bool) cobra.CompletionFunc {
	return func(_ *cobra.Command, _ []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		chosen, prefix, word := splitList(toComplete, multi)

		candidates := make([]Candidate, 0, len(values))
		for _, value := range values {
			candidates = append(candidates, Candidate{Value: value})
		}

		directive := cobra.ShellCompDirectiveNoFileComp
		if multi {
			directive |= cobra.ShellCompDirectiveNoSpace
		}

		return format(candidates, word, prefix, chosen), directive
	}
}

// multiValued reports a flag that takes a list, which pflag spells in the
// type: stringSlice and stringArray accept the flag repeatedly, and the slice
// forms also split a single value on commas.
func multiValued(flag *pflag.Flag) bool {
	return strings.HasSuffix(flag.Value.Type(), "Slice") || strings.HasSuffix(flag.Value.Type(), "Array")
}

// positionalKinds is what a command's arguments accept, in order. A flag
// completion reads it too, to learn what the arguments already typed were.
func positionalKinds(command *cobra.Command) []Kind {
	placeholders := usage.Placeholders(command.Use)
	if len(placeholders) == 0 {
		return nil
	}

	path := commandPath(command)
	kinds := make([]Kind, len(placeholders))
	for index, placeholder := range placeholders {
		kinds[index], _ = DeclaredPositional(path, placeholder)
	}

	return kinds
}

// commandPath is the command without the program name, which is how the
// exception table names one.
func commandPath(command *cobra.Command) string {
	return strings.TrimSpace(strings.TrimPrefix(command.CommandPath(), command.Root().Name()))
}
