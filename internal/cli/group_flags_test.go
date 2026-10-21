package cli

import (
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// groupFlagReaders are the flags declared on a command group, each with every
// command it reaches, all of which read it.
//
// A flag on a group reaches every command beneath it, and a command that never
// reads it took it and dropped it without a word: bb tag create --filter, bb
// branch delete --start (#722). So each entry names the commands it reaches,
// and a command added under the group fails the test below until it reads the
// flag and is listed, or the flag moves onto the commands that read it.
var groupFlagReaders = map[string][]string{
	"bb auth token --user":    {"bb auth token create", "bb auth token get", "bb auth token list", "bb auth token revoke", "bb auth token update"},
	"bb auth token --project": {"bb auth token create", "bb auth token get", "bb auth token list", "bb auth token revoke", "bb auth token update"},
	// Read to warn: deprecated on all three, and removed with the v5 deprecations.
	"bb build status --include-unique": {"bb build status get", "bb build status set", "bb build status stats"},
	"bb repo comment --commit":         {"bb repo comment create", "bb repo comment delete", "bb repo comment list", "bb repo comment update"},
	"bb repo comment --pr":             {"bb repo comment create", "bb repo comment delete", "bb repo comment list", "bb repo comment update"},
	"bb repo ssh-key --project":        {"bb repo ssh-key add", "bb repo ssh-key list", "bb repo ssh-key remove"},
	"bb reviewer --project":            {"bb reviewer condition create", "bb reviewer condition delete", "bb reviewer condition list", "bb reviewer condition update"},
	"bb reviewer-group --project":      {"bb reviewer-group create", "bb reviewer-group delete", "bb reviewer-group list", "bb reviewer-group update", "bb reviewer-group users"},
}

// TestAGroupFlagReachesOnlyTheCommandsThatReadIt walks the command tree for
// flags declared on a group below the root. --repo is the exception, on the
// groups where nearly every command needs a repository.
func TestAGroupFlagReachesOnlyTheCommandsThatReadIt(t *testing.T) {
	t.Parallel()

	root := NewRootCommand()
	seen := map[string]bool{}

	var walk func(command *cobra.Command)
	walk = func(command *cobra.Command) {
		if command != root {
			command.PersistentFlags().VisitAll(func(flag *pflag.Flag) {
				if flag.Name == "repo" {
					return
				}
				key := command.CommandPath() + " --" + flag.Name
				seen[key] = true

				want, listed := groupFlagReaders[key]
				if !listed {
					t.Errorf("%s reaches every command beneath it; declare it on the commands that read it", key)
					return
				}
				if got := runnableBeneath(command); !slices.Equal(got, want) {
					t.Errorf("%s reaches %v, and is listed as read by %v; a command it reaches either reads it and is listed, or the flag moves", key, got, want)
				}
			})
		}
		for _, child := range command.Commands() {
			walk(child)
		}
	}
	walk(root)

	for key := range groupFlagReaders {
		if !seen[key] {
			t.Errorf("%s is listed but no longer declared on its group; remove it from groupFlagReaders", key)
		}
	}
}

func runnableBeneath(command *cobra.Command) []string {
	paths := []string{}
	var collect func(*cobra.Command)
	collect = func(current *cobra.Command) {
		if current.Runnable() && current != command {
			paths = append(paths, current.CommandPath())
		}
		for _, child := range current.Commands() {
			collect(child)
		}
	}
	collect(command)
	slices.Sort(paths)

	return paths
}

// TestAFlagACommandNeverReadIsRefused holds #722's removals: each command took
// the flag from its group and dropped it. Refused at parsing, before any
// request, so no server is involved.
func TestAFlagACommandNeverReadIsRefused(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{"tag", "create", "v1", "--start-point", "main", "--filter", "v"},
		{"tag", "view", "v1", "--filter", "v"},
		{"tag", "delete", "v1", "--filter", "v"},
		{"branch", "delete", "feature", "--start", "5"},
		{"branch", "create", "feature", "--start-point", "main", "--start", "5"},
		{"branch", "default", "get", "--start", "5"},
		{"branch", "restriction", "list", "--start", "5"},
		{"commit", "get", "a1b2c3d", "--start", "5"},
		{"commit", "compare", "main", "feature", "--start", "5"},
		{"commit", "prs", "a1b2c3d", "--start", "5"},
		{"reviewer", "condition", "list", "--config-file", "condition.json"},
		{"reviewer", "condition", "delete", "7", "--config-file", "condition.json"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Parallel()

			root := NewRootCommand()
			// The command has to be there: one renamed away refuses every flag
			// as unknown, and the refusal would pass for this one.
			if command, _, err := root.Find(args); err != nil || !command.Runnable() || command.HasSubCommands() {
				t.Fatalf("bb %s names no command: found %q (%v)", strings.Join(args, " "), command.CommandPath(), err)
			}
			root.SetArgs(args)
			root.SetOut(&strings.Builder{})
			root.SetErr(&strings.Builder{})
			err := root.Execute()
			if err == nil || !strings.Contains(err.Error(), "unknown flag") {
				t.Fatalf("bb %s: want the flag refused as unknown, got %v", strings.Join(args, " "), err)
			}
		})
	}
}
