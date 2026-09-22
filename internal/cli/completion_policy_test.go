package cli

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/completion"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/enumflag"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/usage"
)

// TestEveryCompletionSlotIsDeclared is the guard that makes completion a
// property of the tree rather than of whoever remembered to wire it up.
//
// Every place a value can be typed -- a positional argument, a flag that takes
// one -- has to resolve to a kind through internal/cli/completion's tables. A
// new command whose argument is called something the vocabulary does not know
// fails here, with the name it used, rather than silently completing nothing
// for the rest of its life.
//
// Sabotage that proved it guards: renaming `pr merge <pr-id>` to `<id>` fails
// with "pr merge <id>", and adding a --source flag to bb pr create fails with
// "pr create --source". Both pass again once the name is one the tables know,
// or once the table learns it.
func TestEveryCompletionSlotIsDeclared(t *testing.T) {
	t.Parallel()

	var undeclared []string

	walkCommands(NewRootCommand(), func(command *cobra.Command) {
		if completionIsCobrasOwn(command) {
			return
		}

		path := completionPath(command)

		for _, placeholder := range usage.Placeholders(command.Use) {
			if _, declared := completion.DeclaredPositional(path, placeholder); !declared {
				undeclared = append(undeclared, strings.TrimSpace(path+" "+placeholder))
			}
		}

		declareFlags := func(flag *pflag.Flag) {
			if _, declared := completion.DeclaredFlag(path, flag); !declared {
				undeclared = append(undeclared, strings.TrimSpace(path+" --"+flag.Name))
			}
		}

		// The same two sets the installer reads, for the same reason:
		// LocalNonPersistentFlags merges every parent's persistent flags into
		// this command, which would ask here about --log-level once per
		// command rather than once.
		command.PersistentFlags().VisitAll(declareFlags)
		command.Flags().VisitAll(declareFlags)
	})

	if len(undeclared) == 0 {
		return
	}

	sort.Strings(undeclared)
	t.Errorf(
		"%d slots have no completion kind:\n\t%s\n\n"+
			"Name the argument after what it accepts (<pr-id>, <branch>, <project-key>) and the\n"+
			"vocabulary in internal/cli/completion/vocabulary.go covers it. A value bb cannot list --\n"+
			"a title, a URL, the name of something being created -- belongs there as KindFree, and a\n"+
			"name that means something different in one command than everywhere else belongs in the\n"+
			"exceptions table beside it.",
		len(undeclared),
		strings.Join(undeclared, "\n\t"),
	)
}

// TestEveryDeclaredArgumentCanBeCompleted checks the other half: that the
// installer acted on the declaration.
//
// Declaring a kind and never reaching it is the failure mode a table invites,
// and it is invisible -- an argument that completes nothing looks exactly like
// an argument whose source has not been written yet. So every command that
// declares a positional must come out of NewRootCommand with a completion
// function on it.
//
// Sabotage that proved it guards: returning early from installPositionals
// fails this with every command that takes an argument.
func TestEveryDeclaredArgumentCanBeCompleted(t *testing.T) {
	t.Parallel()

	var missing []string

	walkCommands(NewRootCommand(), func(command *cobra.Command) {
		if completionIsCobrasOwn(command) || len(usage.Placeholders(command.Use)) == 0 {
			return
		}

		if command.ValidArgsFunction == nil && len(command.ValidArgs) == 0 {
			missing = append(missing, completionPath(command))
		}
	})

	if len(missing) == 0 {
		return
	}

	sort.Strings(missing)
	t.Errorf("%d commands take an argument that nothing completes:\n\t%s", len(missing), strings.Join(missing, "\n\t"))
}

// TestThePositionalStatusMatchesTheFlagThatTakesTheSameValues ties the one
// vocabulary completion holds twice to the enum that owns it.
//
// `bb pr review set <pr-id> <status>` takes as an argument what `bb pr review
// complete --status` takes as a flag. The flag is an enumflag and validates
// against its own list; a positional cannot be, so completion carries a copy.
// Two lists that must agree and no test that they do is how the --state
// vocabulary in #577 came to advertise values the flag refused.
//
// Sabotage that proved it guards: dropping UNAPPROVED from
// completion.ReviewStatuses fails here naming it.
func TestThePositionalStatusMatchesTheFlagThatTakesTheSameValues(t *testing.T) {
	t.Parallel()

	command, _, err := NewRootCommand().Find([]string{"pr", "review", "complete"})
	if err != nil {
		t.Fatalf("finding bb pr review complete failed: %v", err)
	}

	flag := command.Flags().Lookup("status")
	if flag == nil {
		t.Fatal("bb pr review complete no longer has a --status flag; the positional <status> now has no authority to match")
	}

	allowed, isEnum := enumflag.Allowed(flag)
	if !isEnum {
		t.Fatal("bb pr review complete --status is no longer an enum flag")
	}

	if !reflect.DeepEqual(allowed, completion.ReviewStatuses) {
		t.Errorf(
			"the values completed for <status> are %v; --status accepts %v",
			completion.ReviewStatuses,
			allowed,
		)
	}
}

func walkCommands(command *cobra.Command, visit func(*cobra.Command)) {
	visit(command)

	for _, child := range command.Commands() {
		walkCommands(child, visit)
	}
}

func completionPath(command *cobra.Command) string {
	return strings.TrimSpace(strings.TrimPrefix(command.CommandPath(), command.Root().Name()))
}

func completionIsCobrasOwn(command *cobra.Command) bool {
	switch command.Name() {
	case "help", "completion", cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd:
		return true
	default:
		return false
	}
}
