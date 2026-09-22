// Package completion answers a shell's tab press with live Bitbucket values.
//
// Three rules shape everything here.
//
// The kind of a slot is declared by its name. A positional argument is what
// its placeholder in the Use line says it is (<pr-id> is a pull request), and
// a flag is what its name says it is (--repo is a repository, wherever it is
// declared). vocabulary.go holds both tables and the handful of exceptions, so
// a command declares what it accepts by naming it well rather than by wiring
// anything up. TestEveryCompletionSlotIsDeclared fails on a slot no table
// covers.
//
// The context is resolved by the same code the command will run. A completion
// that resolves the repository differently from the invocation it is
// completing would offer a pull request from one repository for a command that
// acts on another, which for `bb pr merge` is a wrong merge rather than a
// wrong suggestion. The resolution therefore stays in internal/cli, is handed
// to this package as Dependencies, and this package never re-derives it.
//
// A tab press must end. Everything a source does -- reading the keyring,
// asking git, calling Bitbucket -- runs under one deadline, and whatever has
// not finished is abandoned rather than waited for. A failure completes
// nothing at all: a shell is not a place to report errors.
package completion

import (
	"context"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
)

// Kind is what a slot accepts. It is the key the vocabulary maps a placeholder
// or a flag name to, and the key the source registry answers.
type Kind string

const (
	// KindFree is a value bb cannot list: a title, a description, a URL, the
	// name of something being created. Declared rather than omitted, so the
	// vocabulary test can tell a considered "nothing to offer" from a slot
	// somebody forgot.
	KindFree Kind = "free"

	// KindEnum is a flag that carries its own values. enumflag validates
	// against the same list it completes from, so neither table below has
	// anything to say about it.
	KindEnum Kind = "enum"

	KindAccessKey         Kind = "access-key"
	KindAccessToken       Kind = "access-token"
	KindBranch            Kind = "branch"
	KindBuildKey          Kind = "build-key"
	KindBulkOperation     Kind = "bulk-operation"
	KindCommit            Kind = "commit"
	KindDefaultTask       Kind = "default-task"
	KindDeploymentKey     Kind = "deployment-key"
	KindEmoji             Kind = "emoji"
	KindEndpoint          Kind = "endpoint"
	KindEnvironmentKey    Kind = "environment-key"
	KindGitHelperOp       Kind = "git-helper-op"
	KindGPGKey            Kind = "gpg-key"
	KindGroup             Kind = "group"
	KindHost              Kind = "host"
	KindHostAlias         Kind = "host-alias"
	KindInsightReport     Kind = "insight-report"
	KindLabel             Kind = "label"
	KindLocalDir          Kind = "local-dir"
	KindLocalFile         Kind = "local-file"
	KindLogFormat         Kind = "log-format"
	KindLogLevel          Kind = "log-level"
	KindMatcherID         Kind = "matcher-id"
	KindMCPTool           Kind = "mcp-tool"
	KindMergeStrategy     Kind = "merge-strategy"
	KindPermission        Kind = "permission"
	KindProject           Kind = "project"
	KindPRComment         Kind = "pr-comment"
	KindPRDiffPath        Kind = "pr-diff-path"
	KindPullRequest       Kind = "pull-request"
	KindRef               Kind = "ref"
	KindRepoComment       Kind = "repo-comment"
	KindRepoPath          Kind = "repo-path"
	KindRepository        Kind = "repository"
	KindRequiredBuild     Kind = "required-build"
	KindRestriction       Kind = "restriction"
	KindReviewerCondition Kind = "reviewer-condition"
	KindReviewerGroup     Kind = "reviewer-group"
	KindReviewStatus      Kind = "review-status"
	KindSkill             Kind = "skill"
	KindSSHKey            Kind = "ssh-key"
	KindTag               Kind = "tag"
	KindTokenPermission   Kind = "token-permission"
	KindUser              Kind = "user"
	KindUserOrGroup       Kind = "user-or-group"
	KindWebhook           Kind = "webhook"
	KindWebhookEvent      Kind = "webhook-event"
)

// Candidate is one suggestion. Description is what zsh, fish and PowerShell
// show beside it, and bash shows when more than one candidate matches.
type Candidate struct {
	Value       string
	Description string
}

// Result is what a source answers with.
type Result struct {
	Candidates []Candidate
	// NoSpace keeps the cursor against the value, for a value completed in
	// stages: PROJ/ before the slug, a directory before the rest of the path.
	NoSpace bool
	// KeepOrder asks the shell to show the candidates in the order given,
	// for a source that ranks rather than sorts -- the pull requests you
	// touched most recently, the branch you are standing on.
	KeepOrder bool
}

// Request is one tab press, narrowed to the slot being completed.
type Request struct {
	// Command is the command the shell is completing, with its flags already
	// parsed by Cobra. Its persistent-flag values are readable here even
	// though the root's hooks have not run.
	Command *cobra.Command
	// Args are the positional arguments already typed, not counting the word
	// being completed.
	Args []string
	// ToComplete is the word being completed. For a comma-separated flag it
	// is the element after the last comma, because that is the part the
	// source is being asked about.
	ToComplete string
	// Flag is the flag whose value is being completed, empty for a positional.
	Flag string
	// Position is the index of the positional being completed, -1 for a flag.
	Position int
}

// Source lists what a kind can offer for one request.
//
// A source returns an error rather than an empty result when it could not
// answer -- unreachable server, no credentials, no repository in context. The
// caller turns both into no candidates, but the difference decides whether an
// Active Help line explains the silence.
type Source func(ctx context.Context, env *Environment, request Request) (Result, error)

// Repository is a repository the invocation resolved to, and where it came
// from.
type Repository struct {
	Host       string
	ProjectKey string
	Slug       string
	// RemoteName is the git remote the context was inferred from, empty when
	// the caller named the repository or the environment did.
	RemoteName string
}

// Inferred reports a repository that came from the git remote rather than
// from the caller. Sources use it to decide whether the local checkout is a
// valid source of candidates: refs from .git only describe this repository
// when this repository is the one being completed for.
func (repository Repository) Inferred() bool { return repository.RemoteName != "" }

// Dependencies are the parts of an invocation this package must not reimplement.
//
// Each one is the function the root command already uses, handed over as a
// closure. internal/cli cannot be imported here (it imports this package), and
// duplicating the resolution is the failure this arrangement exists to
// prevent.
type Dependencies struct {
	// Overrides reads the global flags typed on the line being completed.
	Overrides func(*cobra.Command) config.Overrides
	// LoadConfig resolves the configuration without printing anything. The
	// warnings the command path prints -- plaintext credentials, disabled TLS
	// verification -- belong to a run the user asked for, not to a tab press.
	LoadConfig func(config.Overrides) (config.AppConfig, error)
	// InferRepository is the git-remote inference the root hook performs,
	// minus the flag mutation and the notice it prints.
	InferRepository func(context.Context, config.AppConfig) (*Repository, error)
	// LocalRepositories is every Bitbucket repository this checkout's remotes
	// name, origin first.
	//
	// The inference answers with one and refuses when the remotes disagree,
	// because a command acts on one repository. A completion is not choosing:
	// in a fork, both the fork and its upstream are worth offering, and a
	// checkout the inference calls ambiguous is exactly where a completion
	// helps most.
	LocalRepositories func(context.Context, config.AppConfig) ([]Repository, error)
	// AmbientInferenceAllowed reports whether a command accepts an inferred
	// repository at all; bb ai mcp serve and bb auth token require the scope
	// to be named (ADR-039).
	AmbientInferenceAllowed func(*cobra.Command) bool
}

// budget is how long a tab press may take, in total.
//
// It covers the keyring read and the git subprocesses as well as the request,
// because a deadline on the HTTP call alone would not bound either: go-keyring
// takes no context and can sit on a locked Secret Service collection until
// somebody types a password.
//
// 1.2s is long enough for a LAN instance to answer a filtered listing and
// short enough that a wedged one is over before a person reaches for ^C. The
// environment variable is for an instance that is simply slower than that; it
// is read per press because the process is new on every press anyway.
const budget = 1200 * time.Millisecond

func deadline() time.Duration {
	raw := os.Getenv("BB_COMPLETION_TIMEOUT")
	if raw == "" {
		return budget
	}

	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed <= 0 {
		return budget
	}

	return parsed
}
