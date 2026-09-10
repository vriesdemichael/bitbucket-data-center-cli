package cli

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/prompt"
)

// destructiveVerbs name a leaf command that destroys something.
//
// By name. The alternative was to ride the dry-run registry, which classifies
// every command by what it does to the server and would also catch a
// destructive command that is not named like one. The name is what a user reads
// before typing, and the one real gap -- bulk apply, which mutates many
// repositories and is called neither delete nor remove -- leaves with bb bulk in
// v5 (#608).
var destructiveVerbs = map[string]struct{}{
	"delete": {},
	"remove": {},
	"clear":  {},
	"revoke": {},
}

// confirmsItself names the commands that ask on their own.
//
// They were written before this interceptor and ask a better question than a
// generic one can: repo delete makes you type the repository name back, and
// gpg-key clear asks about every key at once rather than naming one. Wrapping
// them would ask twice.
var confirmsItself = map[string]struct{}{
	"repo delete":        {},
	"repo admin delete":  {},
	"auth gpg-key clear": {},
}

// registerDestructiveConfirmations is ADR-073, applied to the command tree
// rather than to thirty-four call sites.
//
// The rule was implemented once, correctly, on repo delete, and stayed there:
// deleting a branch, a tag or a webhook took no confirmation and no --yes at
// all. Adding seven lines to every destructive command would have worked and
// would have been wrong -- the thirty-fifth would be written without them, the
// way the first thirty-four were.
//
// So the flag and the question are installed by the same walk that installs the
// dry-run interceptors, and a new destructive command inherits them by being
// named delete.
func registerDestructiveConfirmations(root *cobra.Command, options *rootOptions) {
	if root == nil || options == nil {
		return
	}

	var visit func(*cobra.Command)
	visit = func(command *cobra.Command) {
		if command == nil {
			return
		}

		path := dryRunCommandPath(command)
		_, destructive := destructiveVerbs[command.Name()]
		_, asksItself := confirmsItself[path]

		if destructive && !asksItself && command.RunE != nil {
			var confirmed bool
			command.Flags().BoolVarP(&confirmed, "yes", "y", false, "Confirm without being asked")

			originalRun := command.RunE
			command.RunE = func(cmd *cobra.Command, args []string) error {
				// --dry-run changes nothing, so there is nothing to confirm.
				if options.DryRun {
					return originalRun(cmd, args)
				}

				if err := prompt.ConfirmDeleteOf(
					cmd,
					options.JSON,
					confirmed,
					targetWasNamed(options),
					destructiveTarget(cmd, args),
				); err != nil {
					return err
				}

				return originalRun(cmd, args)
			}
		}

		for _, child := range command.Commands() {
			visit(child)
		}
	}

	visit(root)
}

// targetWasNamed reports whether the caller said what to destroy, rather than
// having part of it guessed.
//
// It asks one question -- did bb infer a repository for this invocation -- and
// not whether --repo was set. Inference sets --repo and marks it Changed so
// every command can resolve a target, which silently made an inferred
// repository count as explicit and let --yes apply to the one you happened to
// be standing in (#472). Naming the branch does not rescue it: `bb branch
// delete main` names the branch and infers the repository, which is how a probe
// deleted main.
//
// Keying on the flag's presence was worse than keying on inference: auth token
// revoke carries a --repo flag it never uses, so a token could not be revoked
// with --yes at all.
func targetWasNamed(options *rootOptions) bool {
	return !options.repositoryInferred
}

// destructiveTarget is what the person has to type back, and what the refusal
// names when there is nobody to ask.
//
// It is built from what was typed rather than from what the command resolves,
// because the question is asked before the command runs. The repository is
// included when there is one: it is the part the caller may not have chosen.
func destructiveTarget(cmd *cobra.Command, args []string) string {
	var parts []string

	if flag := cmd.Flags().Lookup("repo"); flag != nil && strings.TrimSpace(flag.Value.String()) != "" {
		parts = append(parts, strings.TrimSpace(flag.Value.String()))
	}

	// The parent names what kind of thing this is: `bb branch delete X` deletes
	// a branch, and the leaf is only ever called delete.
	if parent := cmd.Parent(); parent != nil && parent.Parent() != nil {
		parts = append(parts, parent.Name())
	}

	parts = append(parts, args...)

	if len(parts) == 0 {
		return dryRunCommandPath(cmd)
	}

	return strings.Join(parts, " ")
}
