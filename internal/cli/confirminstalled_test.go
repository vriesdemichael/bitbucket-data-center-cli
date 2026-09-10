package cli

import (
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// destructiveVerbs name a command that destroys something.
//
// By name, deliberately. The alternative was to ride the dry-run registry,
// which already classifies every command by what it does to the server and
// would have caught a destructive command that is not named like one. The name
// is what a user reads before typing, and the one real gap -- bulk apply, which
// mutates many repositories and is called neither delete nor remove -- leaves
// with bb bulk in v5 (#608).
var destructiveVerbs = map[string]struct{}{
	"delete": {},
	"remove": {},
	"clear":  {},
	"revoke": {},
}

// notYetGuarded is the backlog, and it shrinks only.
//
// ADR-073 was written unconditionally and implemented on two commands, so
// thirty-four were unguarded when this test was written. Landing them all in one
// change would be a single unreviewable diff across a dozen packages; landing
// them without a list would lose track of which were left.
//
// The list is checked in both directions. A command not on it must be guarded,
// and a command on it must NOT be -- so guarding one fails this test until its
// entry is deleted, and the list cannot quietly outlive the work. Empty is the
// finished state, and then this map and its check go away with it.
var notYetGuarded = map[string]struct{}{
	"ai skill remove":                                  {},
	"auth alias remove":                                {},
	"auth gpg-key remove":                              {},
	"auth token revoke":                                {},
	"branch restriction delete":                        {},
	"build delete":                                     {},
	"build required delete":                            {},
	"deployment delete":                                {},
	"insights annotation delete":                       {},
	"insights report delete":                           {},
	"pr review reviewer remove":                        {},
	"project branch-restriction delete":                {},
	"project default-task delete":                      {},
	"project delete":                                   {},
	"project permissions groups revoke":                {},
	"project permissions revoke":                       {},
	"project permissions users revoke":                 {},
	"project webhook delete":                           {},
	"repo comment delete":                              {},
	"repo default-task delete":                         {},
	"repo label remove":                                {},
	"repo permissions revoke":                          {},
	"repo settings auto-decline delete":                {},
	"repo settings auto-merge delete":                  {},
	"repo settings security permissions groups revoke": {},
	"repo settings security permissions users revoke":  {},
	"repo settings workflow webhooks delete":           {},
	"repo ssh-key remove":                              {},
	"reviewer condition delete":                        {},
	"reviewer-group delete":                            {},
	"ssh-key remove":                                   {},
}

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

	var stillUnguarded, nowGuarded []string
	for _, path := range unguarded {
		if _, known := notYetGuarded[path]; !known {
			stillUnguarded = append(stillUnguarded, path)
		}
	}
	for path := range notYetGuarded {
		if !slices.Contains(unguarded, path) {
			nowGuarded = append(nowGuarded, path)
		}
	}

	if len(stillUnguarded) > 0 {
		sort.Strings(stillUnguarded)
		t.Errorf(
			"%d destructive command(s) cannot be confirmed:\n  %s\n\n"+
				"Each needs a --yes flag and a prompt.ConfirmDeleteOf (single named target) or\n"+
				"prompt.ConfirmAction (no single target) call. ADR-073 requires both: confirm\n"+
				"when a person is present, require the flag when not.",
			len(stillUnguarded), strings.Join(stillUnguarded, "\n  "),
		)
	}

	if len(nowGuarded) > 0 {
		sort.Strings(nowGuarded)
		t.Errorf(
			"%d command(s) are guarded but still listed in notYetGuarded:\n  %s\n\n"+
				"Delete those entries. The list exists to shrink, and an entry that outlives\n"+
				"its command is how a backlog becomes a permanent exemption.",
			len(nowGuarded), strings.Join(nowGuarded, "\n  "),
		)
	}
}
