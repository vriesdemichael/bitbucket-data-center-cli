package completion

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// The failure paths, which are most of what a source is.
//
// A completion runs on a laptop that is not on the VPN, in a directory that is
// not a checkout, against an instance nobody has logged in to. Those are not
// edge cases -- they are Tuesday -- and each one has to end the same way: no
// candidates, no output, no hang, and no file names offered in place of the
// branch that could not be listed.
//
// Nothing here mocks Bitbucket. What is substituted is bb's own resolution,
// which is handed to this package as functions precisely so the command path
// and the completion path cannot disagree; a test can therefore make it fail
// without inventing server behaviour to fail with.

// unresolvable is an Environment whose configuration cannot be loaded, which
// is what a source meets when nobody has logged in to the instance the line
// names.
func unresolvable(command *cobra.Command, reason error) *Environment {
	return &Environment{
		command: command,
		dependencies: Dependencies{
			Overrides:  func(*cobra.Command) config.Overrides { return config.Overrides{} },
			LoadConfig: func(config.Overrides) (config.AppConfig, error) { return config.AppConfig{}, reason },
			InferRepository: func(context.Context, config.AppConfig) (*Repository, error) {
				return nil, nil
			},
			LocalRepositories: func(context.Context, config.AppConfig) ([]Repository, error) {
				return nil, nil
			},
			AmbientInferenceAllowed: func(*cobra.Command) bool { return true },
		},
	}
}

// compiledIn are the kinds whose values ship with the binary, so an instance
// nobody can reach is no reason for them to answer with nothing.
//
// Each says where the values come from. A kind listed here wrongly would be
// one that quietly stopped needing the server; a kind missing from it would be
// one that quietly started needing it.
var compiledIn = map[Kind]string{
	KindGitHelperOp:     "the three words git calls a credential helper with",
	KindLogFormat:       "the diagnostics vocabulary",
	KindLogLevel:        "the diagnostics vocabulary",
	KindMCPTool:         "the tools this binary serves",
	KindMergeStrategy:   "the strategies the flag validates against",
	KindPermission:      "the command's own permission set",
	KindReviewStatus:    "the statuses the flag validates against",
	KindSkill:           "the skills this binary ships",
	KindTokenPermission: "the permissions a token can carry",
	KindWebhookEvent:    "the event keys this instance's version accepts",
}

// readThisMachine are the kinds that answer from the stored configuration
// rather than from the server or from the binary.
//
// What they offer depends on what this machine has logged in to, and nothing
// is a legitimate answer: a fresh checkout, a CI runner, anyone using
// BITBUCKET_URL and a token from the environment. So the promise held here is
// only that they answer at all. That they offer the right thing when there is
// something to offer is source_local_test.go's job, with a configuration it
// writes itself.
var readThisMachine = map[Kind]bool{
	KindHost:      true,
	KindHostAlias: true,
}

// TestEverySourceCompletesNothingWhenTheContextCannotBeResolved is the promise
// ADR-088 makes, held against every kind at once.
//
// A source that panicked, blocked, or returned candidates it invented would
// fail here. The registry is walked rather than listed so a kind added later
// is covered by having been registered.
func TestEverySourceCompletesNothingWhenTheContextCannotBeResolved(t *testing.T) {
	t.Parallel()

	kinds := make([]string, 0, len(sources))
	for kind := range sources {
		kinds = append(kinds, string(kind))
	}
	sort.Strings(kinds)

	for _, name := range kinds {
		kind := Kind(name)

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			command := &cobra.Command{Use: "probe"}
			source := sources[kind]
			reason := apperrors.New(apperrors.KindAuthentication, "no credentials for this instance", nil)

			done := make(chan Result, 1)
			go func() {
				result, _ := source(context.Background(), unresolvable(command, reason), Request{Command: command})
				done <- result
			}()

			select {
			case result := <-done:
				shipped, isCompiledIn := compiledIn[kind]
				switch {
				case readThisMachine[kind]:
					// Whatever this machine has logged in to, including
					// nothing. Answering at all is the promise.
				case isCompiledIn && len(result.Candidates) == 0:
					t.Errorf("%s completes nothing without a server, though its values are %s", kind, shipped)
				case !isCompiledIn && len(result.Candidates) != 0:
					t.Errorf("%s offered %d candidates with nothing resolved: %v", kind, len(result.Candidates), result.Candidates)
				}
			case <-time.After(10 * time.Second):
				t.Fatalf("%s did not answer when the configuration could not be loaded", kind)
			}
		})
	}
}

// TestASourceThatCannotFinishOffersNothing covers the deadline, which is the
// only thing standing between a wedged instance and a terminal that has
// stopped responding to the tab key.
func TestASourceThatCannotFinishOffersNothing(t *testing.T) {
	// Not parallel: it sets the budget for the press it measures.

	t.Setenv("BB_COMPLETION_TIMEOUT", "50ms")

	blocked := func(ctx context.Context, _ *Environment, _ Request) (Result, error) {
		<-ctx.Done()

		return Result{Candidates: []Candidate{{Value: "too-late"}}}, nil
	}

	started := time.Now()
	candidates, directive := answerWith(t, blocked, "")

	if len(candidates) != 0 {
		t.Errorf("expected nothing from a source that did not finish, got %v", candidates)
	}
	if directive&cobra.ShellCompDirectiveNoFileComp == 0 {
		t.Error("expected the shell to be told not to fall back to file names")
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Errorf("the press took %s; the deadline is meant to end it", elapsed)
	}
}

// TestASourceThatPanicsDoesNotTakeTheProcessWithIt covers the guard around a
// bug in a source.
//
// A panic would otherwise print a stack trace the shell has nowhere to put and
// exit non-zero in the middle of somebody's command line.
func TestASourceThatPanicsDoesNotTakeTheProcessWithIt(t *testing.T) {
	t.Parallel()

	panicking := func(context.Context, *Environment, Request) (Result, error) {
		panic("a source with a bug in it")
	}

	candidates, directive := answerWith(t, panicking, "")

	if len(candidates) != 0 {
		t.Errorf("expected nothing from a source that panicked, got %v", candidates)
	}
	if directive&cobra.ShellCompDirectiveNoFileComp == 0 {
		t.Error("expected the shell to be told not to fall back to file names")
	}
}

// TestAReasonWorthActingOnReachesThePrompt covers the one thing that is said
// out loud, and the one thing that is not.
//
// Active Help is how bash and zsh explain an empty completion. A missing
// credential will not fix itself and is worth a line; a server that timed out
// says nothing anybody can act on mid-word, and a line about it under every
// prompt is noise.
func TestAReasonWorthActingOnReachesThePrompt(t *testing.T) {
	t.Parallel()

	actionable := func(context.Context, *Environment, Request) (Result, error) {
		return Result{}, apperrors.New(apperrors.KindAuthentication, "not logged in to bitbucket.example.com", nil)
	}
	candidates, _ := answerWith(t, actionable, "")
	if len(candidates) != 1 || !strings.Contains(candidates[0], "not logged in") {
		t.Errorf("expected the reason as an Active Help line, got %v", candidates)
	}

	transient := func(context.Context, *Environment, Request) (Result, error) {
		return Result{}, apperrors.New(apperrors.KindTransient, "connection reset", nil)
	}
	if candidates, _ := answerWith(t, transient, ""); len(candidates) != 0 {
		t.Errorf("expected silence for a failure nobody can act on, got %v", candidates)
	}
}

// TestTheAnswerIsFilteredCappedAndDeduplicated covers what every source would
// otherwise have to remember, and what a shell would otherwise be handed.
func TestTheAnswerIsFilteredCappedAndDeduplicated(t *testing.T) {
	t.Parallel()

	generous := func(context.Context, *Environment, Request) (Result, error) {
		candidates := []Candidate{
			{Value: "feature/one", Description: "matches"},
			{Value: "feature/one", Description: "the same branch twice"},
			{Value: "hotfix/two", Description: "does not match the prefix"},
			{Value: "", Description: "no value at all"},
			{Value: "feature/three\twith a tab", Description: "not a value a shell can take"},
		}
		for index := 0; index < maxCandidates*2; index++ {
			candidates = append(candidates, Candidate{Value: "feature/filler-" + strings.Repeat("x", index%7) + string(rune('a'+index%26)) + string(rune('0'+index%10))})
		}

		return Result{Candidates: candidates}, nil
	}

	candidates, _ := answerWith(t, generous, "feature/")

	if len(candidates) > maxCandidates {
		t.Errorf("expected at most %d candidates, got %d", maxCandidates, len(candidates))
	}

	seen := map[string]int{}
	for _, candidate := range candidates {
		value, _, _ := strings.Cut(candidate, "\t")
		seen[value]++

		if !strings.HasPrefix(value, "feature/") {
			t.Errorf("%q does not start with what was typed", value)
		}
		if strings.ContainsAny(value, "\t\n\r") {
			t.Errorf("%q carries a character the protocol uses", value)
		}
	}
	if seen["feature/one"] != 1 {
		t.Errorf("expected one feature/one, got %d", seen["feature/one"])
	}
}

// answerWith runs one source through the wrapper, with a context that cannot
// be resolved -- the sources written here do not use it.
func answerWith(t *testing.T, source Source, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
	t.Helper()

	command := &cobra.Command{Use: "probe"}

	return answerFrom(
		context.Background(),
		unresolvable(command, errors.New("not resolvable")).dependencies,
		slot{kind: "probe-kind", position: 0},
		command,
		nil,
		nil,
		toComplete,
		source,
	)
}
