package cli

import (
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestAMissingArgumentIsNamed is #587: 150 of 233 leaf commands answered a
// missing positional with Cobra's own arity message, which names neither the
// command nor the value it wanted. ADR-073 asks for the opposite.
func TestAMissingArgumentIsNamed(t *testing.T) {
	t.Parallel()

	root := NewRootCommand()

	var bare []string
	var checked int

	var visit func(*cobra.Command)
	visit = func(cmd *cobra.Command) {
		placeholders := positionalPlaceholders(cmd.Use)
		if cmd.Runnable() && cmd.Args != nil && len(placeholders) > 0 {
			checked++
			// No arguments at all, which is the case a user hits by typing the
			// command and pressing return.
			err := cmd.Args(cmd, nil)
			if err == nil {
				return
			}

			message := err.Error()
			named := strings.Contains(message, placeholders[0])
			if !named || strings.Contains(message, "arg(s), received") {
				bare = append(bare, dryRunCommandPath(cmd))
			}
		}

		for _, child := range cmd.Commands() {
			visit(child)
		}
	}
	visit(root)

	// A walk that stopped finding commands would report perfect compliance,
	// which is the failure mode ADR-067 exists to catch. This tree has well
	// over a hundred leaves that take a positional.
	if checked < 100 {
		t.Fatalf(
			"only %d commands take a positional, expected over a hundred.\nThe walk is probably broken, not the command tree.",
			checked,
		)
	}

	if len(bare) > 0 {
		sort.Strings(bare)
		t.Fatalf(
			"%d of %d commands answer a missing argument without naming it:\n  %s\n\n"+
				"nameTheMissingArgument builds the message from the placeholders in Use, so a\n"+
				"command here either lost its Use line or had its Args replaced afterwards.",
			len(bare), checked, strings.Join(bare, "\n  "),
		)
	}
}
