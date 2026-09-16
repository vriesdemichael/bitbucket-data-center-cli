package cli

import (
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// destructiveLeafPaths is every command ADR-073 guards, as a caller types it.
func destructiveLeafPaths(root *cobra.Command) []string {
	var paths []string

	var visit func(*cobra.Command)
	visit = func(cmd *cobra.Command) {
		if cmd.Runnable() && !cmd.Hidden {
			if _, destructive := destructiveVerbs[cmd.Name()]; destructive {
				paths = append(paths, dryRunCommandPath(cmd))
			}
		}
		for _, child := range cmd.Commands() {
			visit(child)
		}
	}
	visit(root)

	sort.Strings(paths)

	return paths
}

// TestEveryDestructiveCommandRefusesWhenNobodyConfirmed runs the guard rather
// than checking that a flag was registered.
//
// TestEveryDestructiveCommandCanBeConfirmed asserts --yes exists on each of
// these commands, which a no-op confirmation would satisfy just as well:
// deleting the flag's effect -- returning nil from ConfirmDeleteOf, or having
// targetWasNamed always say yes -- leaves every package green while bb deletes
// whatever it was pointed at. The live suite cannot catch it either, because it
// passes --yes everywhere on purpose.
//
// So each command is invoked here without --yes, with nobody to ask, at a host
// nothing is listening on: a validation refusal naming the flag proves the
// question was asked and the answer could not be had. A confirmation that stops
// working reaches the network instead, and a connection error is not exit 2.
func TestEveryDestructiveCommandRefusesWhenNobodyConfirmed(t *testing.T) {
	paths := destructiveLeafPaths(NewRootCommand())
	if len(paths) < 20 {
		t.Fatalf("found only %d destructive commands; the walk is not seeing the tree", len(paths))
	}

	// Placeholders for whatever positionals a command takes. Cobra validates
	// the count before RunE, so the guard is only reached with the right
	// number, and which number that is differs per command.
	placeholders := []string{"placeholder-one", "placeholder-two", "placeholder-three"}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			configureUnreachableEnv(t)

			fields := strings.Fields(path)
			var attempts []string

			// Naming the repository keeps the refusal about the missing
			// confirmation rather than about an inferred target -- on the
			// commands that have somewhere to put it. auth and ai skill
			// commands do not.
			namesRepository := false
			var required []string
			if found, _, err := NewRootCommand().Find(fields); err == nil && found != nil {
				namesRepository = found.Flags().Lookup("repo") != nil || found.InheritedFlags().Lookup("repo") != nil
				// Cobra checks required flags before RunE, so the guard is not
				// reached without them. What they are set to does not matter:
				// nothing gets as far as using the value.
				required = requiredFlagArguments(found)
			}

			for count := 0; count <= len(placeholders); count++ {
				args := append([]string{"--json", "--no-input"}, fields...)
				args = append(args, placeholders[:count]...)
				if namesRepository {
					args = append(args, "--repo", "PRJ/demo")
				}
				args = append(args, required...)

				_, err := executeTestCLI(t, args...)
				if err == nil {
					t.Fatalf("%s ran to completion with no confirmation and no server:\n%v", path, args)
				}

				message := err.Error()
				attempts = append(attempts, message)

				// The refusal ADR-073 requires: the flag, and why nobody was
				// asked. Anything else is this invocation being wrong rather
				// than the guard being absent, so try the next shape.
				if strings.Contains(message, "--yes is required") || strings.Contains(message, "--yes only applies") {
					if code := apperrors.ExitCode(err); code != 2 {
						t.Fatalf("exit code = %d, want 2 (validation): %v", code, err)
					}

					return
				}
			}

			t.Fatalf("%s never refused for want of confirmation.\nAttempts:\n%s", path, strings.Join(attempts, "\n"))
		})
	}
}

// TestYesDoesNotApplyToARepositoryNobodyNamed is #472, kept from coming back.
//
// --yes is honoured only when the caller named the target. Inference fills in
// --repo and marks it Changed so every command can resolve one, so a guard that
// reads the flag rather than the inference lets --yes apply to whichever
// repository the caller happens to be standing in -- which is how a probe
// deleted a main branch.
func TestYesDoesNotApplyToARepositoryNobodyNamed(t *testing.T) {
	t.Parallel()

	for _, inferred := range []bool{true, false} {
		root := &cobra.Command{Use: "bb"}
		root.PersistentFlags().Bool("no-input", false, "")
		group := &cobra.Command{Use: "branch"}

		var ran bool
		leaf := &cobra.Command{
			Use:  "delete",
			Args: cobra.ExactArgs(1),
			RunE: func(*cobra.Command, []string) error {
				ran = true

				return nil
			},
		}
		leaf.Flags().String("repo", "", "")
		root.AddCommand(group)
		group.AddCommand(leaf)

		options := &rootOptions{repositoryInferred: inferred}
		registerDestructiveConfirmations(root, options)

		root.SetArgs([]string{"branch", "delete", "main", "--repo", "PRJ/demo", "--yes"})
		err := root.Execute()

		if inferred {
			if err == nil || ran {
				t.Errorf("--yes was honoured for a repository bb inferred (ran=%v, err=%v)", ran, err)
			}
			if err != nil && !strings.Contains(err.Error(), "--repo") {
				t.Errorf("the refusal does not say how to name the repository: %v", err)
			}

			continue
		}

		if err != nil || !ran {
			t.Errorf("--yes on a repository the caller named was refused (ran=%v, err=%v)", ran, err)
		}
	}
}

// requiredFlagArguments sets every flag Cobra requires, so an invocation
// reaches the command rather than stopping at its flag validation.
func requiredFlagArguments(cmd *cobra.Command) []string {
	var args []string

	cmd.Flags().VisitAll(func(flag *pflag.Flag) {
		if flag.Name == "repo" {
			return
		}

		values, marked := flag.Annotations[cobra.BashCompOneRequiredFlag]
		if !marked || len(values) == 0 || values[0] != "true" {
			return
		}

		// 1 parses as a number, a string and an id alike, which is all this
		// needs: the confirmation is asked before anything reads it.
		args = append(args, "--"+flag.Name+"=1")
	})

	return args
}
