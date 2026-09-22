package completion

import (
	"context"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/reposel"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
)

// Environment is what the invocation being completed resolves to.
//
// Every part of it is worked out on demand and kept for the rest of the press:
// completing a branch needs the repository and therefore the configuration and
// therefore the keyring, while completing a project key needs none of them,
// and a tab press has no budget for work nothing asked for.
//
// Nothing here re-derives a rule. The functions in Dependencies are the ones
// the root command runs before a command executes, so what a source sees is
// what the command would have acted on.
type Environment struct {
	dependencies Dependencies
	command      *cobra.Command

	// bindings are the values already typed on the line, keyed by what the
	// declaration says they are: `bb pr comment get 42 <tab>` binds
	// KindPullRequest to "42", which is how the comment source knows which
	// pull request to list without a line of its own about argument order.
	bindings map[Kind]string

	configOnce sync.Once
	config     config.AppConfig
	configErr  error

	clientOnce sync.Once
	client     *openapigenerated.ClientWithResponses
	clientErr  error

	repositoryOnce sync.Once
	repository     Repository
	repositoryErr  error
}

// Config resolves the configuration this invocation would use.
//
// It mirrors the root hook: the global flags first, then the repository the
// git remote implies -- which carries the host as well, so a checkout of an
// instance other than the default one completes against the instance it
// belongs to. What it does not do is print: the hook's notice and the
// plaintext-credential warning are for a run somebody asked for.
func (environment *Environment) Config(ctx context.Context) (config.AppConfig, error) {
	environment.configOnce.Do(func() {
		overrides := environment.dependencies.Overrides(environment.command)

		if environment.ambientRepositoryAllowed() {
			base, err := config.LoadFromEnv()
			if err == nil {
				inferred, inferErr := environment.dependencies.InferRepository(ctx, base)
				if inferErr == nil && inferred != nil {
					overrides.Host = inferred.Host
					overrides.ProjectKey = inferred.ProjectKey
					overrides.RepoSlug = inferred.Slug
					environment.repository = *inferred
				}
			}
		}

		cfg, err := environment.dependencies.LoadConfig(overrides)
		environment.config, environment.configErr = narrow(cfg), err
	})

	return environment.config, environment.configErr
}

// narrow puts the completion budget into the configuration every client is
// built from, so no source has to remember either half of it.
//
// Retries are the half that surprises: ADR-009 retries a GET, and a listing
// retried twice with backoff spends the whole deadline on a server that is
// already failing. A tab press has no second chance to spend -- the user
// presses tab again.
func narrow(cfg config.AppConfig) config.AppConfig {
	cfg.RetryCount = 0
	cfg.RequestTimeout = deadline()
	cfg.DiagnosticsEnabled = false

	return cfg
}

// APIClient is the generated client, for the services that take one.
func (environment *Environment) APIClient(ctx context.Context) (*openapigenerated.ClientWithResponses, error) {
	environment.clientOnce.Do(func() {
		cfg, err := environment.Config(ctx)
		if err != nil {
			environment.clientErr = err

			return
		}

		environment.client, environment.clientErr = openapi.NewClientWithResponsesFromConfig(cfg)
	})

	return environment.client, environment.clientErr
}

// HTTPClient is the raw JSON client, for the services that take that one
// instead.
func (environment *Environment) HTTPClient(ctx context.Context) (*httpclient.Client, error) {
	cfg, err := environment.Config(ctx)
	if err != nil {
		return nil, err
	}

	return httpclient.NewFromConfig(cfg), nil
}

// Repository is the repository the command would act on.
//
// Precedence is the command's: an explicit --repo, then a repository named as
// a positional argument, then the configuration -- which by then already holds
// whatever the git remote implied, because Config put it there.
func (environment *Environment) Repository(ctx context.Context) (Repository, error) {
	environment.repositoryOnce.Do(func() {
		cfg, err := environment.Config(ctx)
		if err != nil {
			environment.repositoryErr = err

			return
		}

		selector := environment.selectorFromLine()

		projectKey, slug, resolveErr := reposel.Resolve(selector, cfg)
		if resolveErr != nil {
			environment.repositoryErr = resolveErr

			return
		}

		// Keep the remote name when the inference is what produced this, and
		// drop it when the caller named something else: a source uses it to
		// decide whether the local checkout describes this repository.
		remote := environment.repository.RemoteName
		if selector != "" || !strings.EqualFold(projectKey, environment.repository.ProjectKey) ||
			!strings.EqualFold(slug, environment.repository.Slug) {
			remote = ""
		}

		environment.repository = Repository{
			Host:       cfg.BitbucketURL,
			ProjectKey: projectKey,
			Slug:       slug,
			RemoteName: remote,
		}
	})

	return environment.repository, environment.repositoryErr
}

// LocalRepositories are the Bitbucket repositories this checkout's remotes
// name, origin first, and none when there is no checkout or none of its
// remotes is an instance bb is logged in to.
//
// A source uses these to put what you are standing in ahead of what the server
// happens to return first. The reading is the invocation's own, so a
// completion cannot decide this checkout is one repository while the command
// decides it is another.
func (environment *Environment) LocalRepositories(ctx context.Context) []Repository {
	if environment.dependencies.LocalRepositories == nil {
		return nil
	}

	cfg, err := environment.Config(ctx)
	if err != nil {
		return nil
	}

	repositories, err := environment.dependencies.LocalRepositories(ctx, cfg)
	if err != nil {
		return nil
	}

	return repositories
}

// Project is the project in scope: one named on the line, or the project of
// the repository in scope.
func (environment *Environment) Project(ctx context.Context) (string, error) {
	if bound := strings.TrimSpace(environment.bindings[KindProject]); bound != "" {
		return bound, nil
	}

	if flagValue := environment.flag("project"); flagValue != "" {
		return flagValue, nil
	}

	repository, err := environment.Repository(ctx)
	if err != nil {
		return "", err
	}

	return repository.ProjectKey, nil
}

// PullRequest is the pull request in scope, for the slots that belong to one:
// a comment id, a reviewer being removed.
func (environment *Environment) PullRequest(context.Context) (string, error) {
	if bound := strings.TrimSpace(environment.bindings[KindPullRequest]); bound != "" {
		return strings.TrimPrefix(bound, "#"), nil
	}

	if flagValue := environment.flag("pr"); flagValue != "" {
		return strings.TrimPrefix(flagValue, "#"), nil
	}

	return "", apperrors.New(apperrors.KindValidation, "no pull request in context", nil)
}

// Commit is the commit in scope, for the slots that hang off one: a build
// status key, an insights report.
func (environment *Environment) Commit(context.Context) (string, error) {
	if bound := strings.TrimSpace(environment.bindings[KindCommit]); bound != "" {
		return bound, nil
	}

	for _, flagName := range []string{"commit", "at", "ref"} {
		if flagValue := environment.flag(flagName); flagValue != "" {
			return flagValue, nil
		}
	}

	return "", apperrors.New(apperrors.KindValidation, "no commit in context", nil)
}

// Command is the command being completed, for a source that needs to know
// which one asked -- the permission levels differ between bb project and bb
// repo, and both spell the argument <permission>.
func (environment *Environment) Command() *cobra.Command { return environment.command }

// Flag reads a flag already typed on the line, empty when it was not.
func (environment *Environment) Flag(name string) string { return environment.flag(name) }

func (environment *Environment) flag(name string) string {
	if environment.command == nil {
		return ""
	}

	flag := environment.command.Flags().Lookup(name)
	if flag == nil || !flag.Changed {
		return ""
	}

	return strings.TrimSpace(flag.Value.String())
}

// selectorFromLine is the repository the caller named, by flag or as an
// argument, and empty when they named none.
func (environment *Environment) selectorFromLine() string {
	if flagValue := environment.flag("repo"); flagValue != "" {
		return flagValue
	}

	return strings.TrimSpace(environment.bindings[KindRepository])
}

// ambientRepositoryAllowed refuses the git remote where the command refuses
// it, so completion scopes itself the way the command would.
func (environment *Environment) ambientRepositoryAllowed() bool {
	if environment.selectorFromLine() != "" {
		return false
	}

	if environment.dependencies.AmbientInferenceAllowed == nil {
		return true
	}

	return environment.dependencies.AmbientInferenceAllowed(environment.command)
}
