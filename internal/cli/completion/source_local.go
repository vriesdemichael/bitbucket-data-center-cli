package completion

import (
	"context"
	"sort"
	"strings"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/diagnostics"
)

func init() {
	register(KindLogLevel, fixedSource(logLevels, "how much diagnostics bb writes"))
	register(KindLogFormat, fixedSource(logFormats, "how diagnostics are written"))
	register(KindReviewStatus, fixedSource(ReviewStatuses, ""))
	register(KindGitHelperOp, fixedSource(gitHelperOperations, ""))
	register(KindHost, hostSource)
	register(KindHostAlias, hostAliasSource)
}

// logLevels and logFormats are the vocabularies diagnostics validates against.
//
// Taken from the constants rather than written out, because the two flags that
// carry them are the pair enumflag cannot hold: an empty value means "unset
// BB_LOG_LEVEL" rather than "not given", and enumflag has no way to allow an
// empty string for one flag without allowing it everywhere.
var (
	logLevels = []string{
		string(diagnostics.LevelError),
		string(diagnostics.LevelWarn),
		string(diagnostics.LevelInfo),
		string(diagnostics.LevelDebug),
	}
	logFormats = []string{
		string(diagnostics.FormatText),
		string(diagnostics.FormatJSONL),
	}
)

// ReviewStatuses is what `bb pr review set <pr-id> <status>` accepts.
//
// Exported so TestThePositionalStatusMatchesTheFlagThatTakesTheSameValues can
// hold it against the --status flag on bb pr review complete, which is an enum
// flag and therefore the authority. A positional cannot be an enum flag, so
// this is the one vocabulary in the package that exists twice, and the test is
// what keeps the copies from drifting.
var ReviewStatuses = []string{"APPROVED", "NEEDS_WORK", "UNAPPROVED"}

// gitHelperOperations are git's, not bb's: git calls the credential helper
// with one of these three words.
var gitHelperOperations = []string{"get", "store", "erase"}

// fixedSource answers from a list that needs nothing asked.
func fixedSource(values []string, description string) Source {
	return func(context.Context, *Environment, Request) (Result, error) {
		candidates := make([]Candidate, 0, len(values))
		for _, value := range values {
			candidates = append(candidates, Candidate{Value: value, Description: description})
		}

		return Result{Candidates: candidates, KeepOrder: true}, nil
	}
}

// hostSource offers the Bitbucket instances this machine is logged in to.
//
// From the stored configuration alone: which instances exist is a question the
// configuration answers, and asking one of them would be both slower and
// wrong -- an instance that is down is still one you may want to name.
func hostSource(_ context.Context, _ *Environment, _ Request) (Result, error) {
	stored, err := config.LoadStoredConfig()
	if err != nil {
		return Result{}, err
	}

	candidates := make([]Candidate, 0, len(stored.Hosts))
	for host, profile := range stored.Hosts {
		description := profile.Username
		if host == stored.DefaultHost {
			description = strings.TrimSpace(description + " (default)")
		}

		candidates = append(candidates, Candidate{Value: host, Description: description})
	}

	// The map has no order of its own, and a list that reshuffles between two
	// presses of the same key is worse than an arbitrary order held still.
	sort.Slice(candidates, func(left, right int) bool {
		if candidates[left].Value == stored.DefaultHost {
			return true
		}
		if candidates[right].Value == stored.DefaultHost {
			return false
		}

		return candidates[left].Value < candidates[right].Value
	})

	return Result{Candidates: candidates, KeepOrder: true}, nil
}

// hostAliasSource offers the alternative names a stored host answers to.
//
// An alias exists because a git remote spells the host differently from the
// configuration -- a vanity name, an SSH host, a proxy -- so the ones worth
// offering are the ones already recorded.
func hostAliasSource(_ context.Context, _ *Environment, _ Request) (Result, error) {
	stored, err := config.LoadStoredConfig()
	if err != nil {
		return Result{}, err
	}

	candidates := []Candidate{}
	for host, profile := range stored.Hosts {
		for _, alias := range profile.Aliases {
			candidates = append(candidates, Candidate{Value: alias, Description: host})
		}
	}

	sort.Slice(candidates, func(left, right int) bool {
		return candidates[left].Value < candidates[right].Value
	})

	return Result{Candidates: candidates, KeepOrder: true}, nil
}
