package cli

import (
	"context"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/completion"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
)

// installCompletions gives the tree its shell completions.
//
// What this file is for is the four closures: they are the resolution the root
// hook and the commands already perform, handed to a package that must not
// perform its own. Cobra answers a completion request through a hidden command
// of its own, which means PersistentPreRunE has run for that command and not
// for the one being completed -- no global flags applied, no repository
// inferred. Completion therefore has to resolve the invocation itself, and the
// only safe way to do that is with this code rather than a second copy of it:
// a completion that resolved a different repository would offer `bb pr merge`
// a pull request number that means something else in the repository the merge
// will reach.
func installCompletions(root *cobra.Command, options *rootOptions) {
	completion.Install(root, completion.Dependencies{
		Overrides: func(cmd *cobra.Command) config.Overrides {
			return runtimeOverridesFromFlags(cmd, options.runtime)
		},

		// config.LoadWithOverrides rather than options.loadConfigWithOverrides:
		// the same resolution without the warnings, which belong on the stderr
		// of a command somebody ran and not under a half-typed line.
		LoadConfig: func(overrides config.Overrides) (config.AppConfig, error) {
			return config.LoadWithOverrides(options.merge(overrides))
		},

		InferRepository: func(_ context.Context, cfg config.AppConfig) (*completion.Repository, error) {
			inferred, err := inferRepositoryContextFromGit(cfg)
			if err != nil || inferred == nil {
				return nil, err
			}

			return &completion.Repository{
				Host:       inferred.Host,
				ProjectKey: inferred.ProjectKey,
				Slug:       inferred.Slug,
				RemoteName: inferred.RemoteName,
			}, nil
		},

		AmbientInferenceAllowed: func(cmd *cobra.Command) bool {
			if cmd == nil {
				return true
			}

			return cmd.Annotations[annotationNoAmbientRepoInference] != "true"
		},
	})
}
