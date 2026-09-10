package cli

import (
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestEveryDestructiveCommandCanBeConfirmed is ADR-073's rule, enforced.
//
// The ADR says it without qualification -- "Destructive commands confirm when a
// person is present and require --yes when not" -- and the binary kept it for
// two commands out of thirty-seven. The helper existed, worked, and was wired
// to repo delete and auth gpg-key clear and nothing else, so deleting a branch,
// a tag or a webhook took no confirmation at all and resolved the repository
// from the git remote while doing it.
//
// A count in an issue does not stop that happening again; this does.
func TestEveryDestructiveCommandCanBeConfirmed(t *testing.T) {
	t.Parallel()

	root := NewRootCommand()

	var unguarded []string
	var found int

	var visit func(*cobra.Command)
	visit = func(cmd *cobra.Command) {
		if cmd.Hidden || cmd.Name() == "help" || cmd.Name() == "completion" {
			return
		}

		if cmd.Runnable() {
			if _, destructive := destructiveVerbs[cmd.Name()]; destructive {
				found++
				if cmd.Flags().Lookup("yes") == nil {
					unguarded = append(unguarded, dryRunCommandPath(cmd))
				}
			}
		}

		for _, child := range cmd.Commands() {
			visit(child)
		}
	}
	visit(root)

	// A detector that stopped matching would report perfect compliance, which
	// is the failure mode ADR-067 exists to catch. This tree is known to carry
	// dozens of destructive leaves; finding a handful means the walk broke
	// rather than that the commands went away.
	if found < 20 {
		t.Fatalf("found only %d destructive commands, expected dozens.\nThe detector is probably broken, not the command tree.", found)
	}

	if len(unguarded) > 0 {
		sort.Strings(unguarded)
		t.Fatalf(
			"%d destructive command(s) cannot be confirmed:\n  %s\n\n"+
				"registerDestructiveConfirmations installs --yes and the question on every leaf\n"+
				"named delete, remove, clear or revoke. A command here means the walk missed it,\n"+
				"or that it is listed in confirmsItself without asking on its own.",
			len(unguarded), strings.Join(unguarded, "\n  "),
		)
	}
}
