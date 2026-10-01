package cli

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/giturl"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/prompt"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/reposel"
)

// destructiveVerbs name a leaf command that destroys something.
//
// By name. The alternative was to ride the dry-run registry, which classifies
// every command by what it does to the server and would also catch a
// destructive command that is not named like one. The name is what a user reads
// before typing, so it is what the question keys on.
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
			usage := "Confirm without being asked"
			if naming := repositoryNaming(command); naming != "" {
				usage += "; applies only when the target is named " + naming
			}

			var confirmed bool
			command.Flags().BoolVarP(&confirmed, "yes", "y", false, usage)

			originalRun := command.RunE
			command.RunE = func(cmd *cobra.Command, args []string) error {
				// --dry-run changes nothing, so there is nothing to confirm.
				if options.DryRun {
					return originalRun(cmd, args)
				}

				naming := repositoryNaming(cmd)
				if err := prompt.ConfirmDeleteOf(
					cmd,
					options.machineOutput(),
					confirmed,
					targetWasNamed(cmd, args, options, naming),
					naming,
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

// targetWasNamed reports whether the caller wrote down, for this invocation,
// the whole of what --yes would destroy (ADR-073). naming is what
// repositoryNaming says names the target's repository, "" when it has none.
//
// Only a repository can be part of a target without being written down, so a
// target without one was named by what was typed. A scope named another way,
// with --project, leaves no repository in the target, and a pull request's URL
// names the repository itself: the command reads neither --repo nor the
// environment for it. Otherwise the repository has to be named with --repo, as
// prompt.TargetNamed decides for repo delete too: one taken from the git remote
// or from BITBUCKET_PROJECT_KEY and BITBUCKET_REPO_SLUG is not, and naming the
// branch does not rescue it -- `bb branch delete main` names the branch and not
// the repository it is in.
func targetWasNamed(cmd *cobra.Command, args []string, options *rootOptions, naming string) bool {
	if naming == "" || reposel.NamedInsteadOfRepo(cmd.Flags()) != "" {
		return true
	}

	for _, arg := range args {
		if _, _, _, _, isURL := giturl.ParseBitbucketPR(strings.TrimSpace(arg)); isURL {
			return true
		}
	}

	return prompt.TargetNamed(cmd, func() bool { return options.repositoryInferred })
}

// repositoryNaming is what names the repository of the target cmd destroys,
// as the refusal of --yes offers it, or "" when the target has none.
//
// A command without --repo acts on no repository. Nor does one whose --repo
// nothing but the caller fills in: auth token's picks which token, and absent
// it acts on none. Where a flag names the scope in place of --repo, the
// refusal offers it too, since following a remedy that named only --repo
// would destroy something in a repository rather than in the project meant.
func repositoryNaming(cmd *cobra.Command) string {
	flags := flagsOf(cmd)
	if flags.Lookup("repo") == nil || cmd.Annotations[annotationNoAmbientRepoInference] == "true" {
		return ""
	}

	naming := "with --repo PROJECT/slug"
	for _, name := range reposel.InsteadOfRepo(flags) {
		naming += " or --" + name
	}

	return naming
}

// flagsOf is every flag cmd takes: its own, and those its parents hand down.
//
// Read without cmd.InheritedFlags, which merges the parents' flags into cmd as
// a side effect. Cobra does that when it parses; done while the tree is still
// being wired, it changed which flags other walks over the tree see as cmd's
// own.
func flagsOf(cmd *cobra.Command) *pflag.FlagSet {
	flags := pflag.NewFlagSet(cmd.Name(), pflag.ContinueOnError)
	flags.AddFlagSet(cmd.Flags())
	for command := cmd; command != nil; command = command.Parent() {
		flags.AddFlagSet(command.PersistentFlags())
	}

	return flags
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
	parts = append(parts, identifyingFlags(cmd)...)

	if len(parts) == 0 {
		return dryRunCommandPath(cmd)
	}

	return strings.Join(parts, " ")
}

// controlFlags say how a command runs rather than what it acts on, so they are
// no part of what is about to be destroyed.
var controlFlags = map[string]struct{}{
	"yes": {}, "repo": {}, "json": {}, "yaml": {}, "dry-run": {}, "describe": {},
	"no-input": {}, "no-color": {}, "full-error-body": {}, "host": {},
	"log-level": {}, "log-format": {}, "request-timeout": {}, "retry-count": {},
	"retry-backoff": {}, "ca-file": {}, "insecure-skip-verify": {},
	"client-cert": {}, "client-key": {}, "limit": {}, "all": {},
}

// identifyingFlags are the values that say which thing is going, for a command
// that takes it as a flag rather than as an argument.
//
// `bb pr review reviewer remove 1 --user bob` asked to confirm "PROJ/repo
// reviewer 1", which names the pull request and not bob; `bb repo comment
// delete --id 7` named nothing at all. Only flags the caller actually passed
// are read, so nothing that was left at its default appears.
func identifyingFlags(cmd *cobra.Command) []string {
	var values []string

	cmd.Flags().Visit(func(flag *pflag.Flag) {
		if _, control := controlFlags[flag.Name]; control {
			return
		}
		if value := strings.TrimSpace(flag.Value.String()); value != "" && value != "false" {
			values = append(values, value)
		}
	})

	return values
}
