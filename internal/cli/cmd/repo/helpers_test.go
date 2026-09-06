package repocmd

import (
	"bytes"
	"io"
	"testing"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/permissionchecker"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/git"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

func executeTestCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	t.Setenv("NO_COLOR", "1")
	root := NewRootCommand()
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(args)
	err := root.Execute()
	return buf.String(), err
}

// testSetup is everything a test used to put in the process: the configuration
// it wanted the command to see, the git backend it wanted the command to use,
// and whether a clone was allowed to prompt.
//
// All three used to be process-wide -- BITBUCKET_* through t.Setenv, the backend
// and the prompt through package-level variables the test swapped and restored.
// That is what made every test in this package run on its own: two of them
// setting BITBUCKET_PROJECT_KEY, or substituting a backend, would each see the
// other's. Passing them per invocation is what lets them run together.
type testSetup struct {
	Host       string
	Token      string
	ProjectKey string
	RepoSlug   string

	Backend   git.Backend
	CanPrompt func(io.Reader, io.Writer) bool
	// Stdin is what the command reads, for the clone paths that prompt.
	Stdin io.Reader

	// Inferred reports the repository as resolved from a git remote rather
	// than named, which is the difference the destructive commands read.
	Inferred bool
	// ConfigPath points the stored-config layer somewhere a test owns. Empty
	// means the stored config is disabled outright, which is what the tests
	// that set BB_DISABLE_STORED_CONFIG wanted.
	ConfigPath string

	// Retries is how often a failing request is repeated before the command
	// gives up. Nil means none.
	//
	// The shipped policy is two retries at 250ms and 500ms, which is right for
	// a user and wrong for a test whose subject is the failure: every one of
	// them waited 750ms for an answer it had already decided about. That is
	// where the whole clone suite's per-test 830ms went -- a stub git backend
	// and no network in sight, sleeping through a backoff.
	Retries *int
}

// dependencies turns a setup into the Dependencies a command is built with.
func (setup testSetup) dependencies(jsonEnabled, dryRunEnabled func() bool) Dependencies {
	deps := Dependencies{
		JSONEnabled:       jsonEnabled,
		DryRunEnabled:     dryRunEnabled,
		PermissionChecker: func(c *openapigenerated.ClientWithResponses) PermissionChecker { return permissionchecker.New(c) },
		LoadConfig: func() (config.AppConfig, error) {
			retries := 0
			if setup.Retries != nil {
				retries = *setup.Retries
			}

			return config.LoadWithOverrides(config.Overrides{
				Host:       setup.Host,
				Token:      setup.Token,
				ProjectKey: setup.ProjectKey,
				RepoSlug:   setup.RepoSlug,
				RetryCount: &retries,
			})
		},
	}
	if setup.Backend != nil {
		deps.GitBackend = func() git.Backend { return setup.Backend }
	}
	if setup.CanPrompt != nil {
		deps.CanPromptForCloneLogin = setup.CanPrompt
	}
	if setup.Inferred {
		deps.RepositoryWasInferred = func() bool { return true }
	}

	return deps
}

// executeTestCLIWith runs the CLI with configuration passed rather than
// published, so the test may declare itself parallel.
func executeTestCLIWith(t *testing.T, setup testSetup, args ...string) (string, error) {
	t.Helper()

	root := &cobra.Command{Use: "bb"}
	jsonFlag := root.PersistentFlags().Bool("json", false, "")
	dryRunFlag := root.PersistentFlags().Bool("dry-run", false, "")
	root.PersistentFlags().Bool("no-input", false, "")

	deps := setup.dependencies(
		func() bool { return *jsonFlag },
		func() bool { return *dryRunFlag },
	)
	root.AddCommand(New(deps))
	root.AddCommand(NewClone(deps))

	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	if setup.Stdin != nil {
		root.SetIn(setup.Stdin)
	}
	// --no-color as an argument rather than NO_COLOR in the environment: the
	// variable is read process-wide and would be one more thing two parallel
	// tests share.
	root.SetArgs(append([]string{"--no-color"}, args...))
	root.PersistentFlags().Bool("no-color", false, "")

	err := root.Execute()

	return buf.String(), err
}

func NewRootCommand() *cobra.Command {
	root := &cobra.Command{Use: "bb"}
	jsonFlag := root.PersistentFlags().Bool("json", false, "")
	dryRunFlag := root.PersistentFlags().Bool("dry-run", false, "")
	// Mirrors the real root (internal/cli/root.go). prompt.RequestFor reads this
	// flag off the command, so a harness without it makes the flag untestable and
	// the command behave differently here than it does for a user.
	root.PersistentFlags().Bool("no-input", false, "")
	deps := Dependencies{
		JSONEnabled:       func() bool { return *jsonFlag },
		DryRunEnabled:     func() bool { return *dryRunFlag },
		PermissionChecker: func(c *openapigenerated.ClientWithResponses) PermissionChecker { return permissionchecker.New(c) },
	}
	root.AddCommand(New(deps))
	root.AddCommand(NewClone(deps))
	return root
}

// NewRootCommandWithInference is NewRootCommand with the repository reported as
// inferred, which is what the real root does after resolving it from a git
// remote.
func NewRootCommandWithInference(inferred bool) *cobra.Command {
	root := &cobra.Command{Use: "bb"}
	jsonFlag := root.PersistentFlags().Bool("json", false, "")
	dryRunFlag := root.PersistentFlags().Bool("dry-run", false, "")
	root.PersistentFlags().Bool("no-input", false, "")
	deps := Dependencies{
		JSONEnabled:           func() bool { return *jsonFlag },
		DryRunEnabled:         func() bool { return *dryRunFlag },
		PermissionChecker:     func(c *openapigenerated.ClientWithResponses) PermissionChecker { return permissionchecker.New(c) },
		RepositoryWasInferred: func() bool { return inferred },
	}
	root.AddCommand(New(deps))
	root.AddCommand(NewClone(deps))
	return root
}
