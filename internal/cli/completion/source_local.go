package completion

import (
	"context"
	"sort"
	"strings"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/diagnostics"
)

func init() {
	register(KindLogLevel, describedSource(logLevels))
	register(KindLogFormat, describedSource(logFormats))
	register(KindReviewStatus, fixedSource(ReviewStatuses))
	register(KindGitHelperOp, fixedSource(gitHelperOperations))
	register(KindHost, hostSource)
	register(KindHostAlias, hostAliasSource)
}

// logLevels and logFormats are the vocabularies diagnostics validates against.
//
// Taken from the constants rather than written out, because the two flags that
// carry them are the pair enumflag cannot hold: an empty value means "unset
// BB_LOG_LEVEL" rather than "not given", and enumflag has no way to allow an
// empty string for one flag without allowing it everywhere.
//
// Each value says what it adds, rather than all four repeating what the flag
// is for. Seen in a real terminal, one description on every value is worse
// than none: bash and fish print it beside each candidate, so four levels
// became four copies of the same sentence across two lines, and the thing a
// reader wants -- which level to pick -- was the part nobody wrote down. zsh
// collapses them onto one line, which hides the problem rather than fixing it.
var (
	logLevels = []Candidate{
		{Value: string(diagnostics.LevelError), Description: "failures only"},
		{Value: string(diagnostics.LevelWarn), Description: "and warnings"},
		{Value: string(diagnostics.LevelInfo), Description: "and what bb is doing"},
		{Value: string(diagnostics.LevelDebug), Description: "and every request"},
	}
	logFormats = []Candidate{
		{Value: string(diagnostics.FormatText), Description: "lines for a person"},
		{Value: string(diagnostics.FormatJSONL), Description: "one JSON object per line"},
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

// fixedSource answers from a list of values that need no describing: the three
// words git calls a credential helper with, the statuses a review can be set
// to. A value whose name is the whole explanation is better left alone than
// given a description that repeats it.
func fixedSource(values []string) Source {
	candidates := make([]Candidate, 0, len(values))
	for _, value := range values {
		candidates = append(candidates, Candidate{Value: value})
	}

	return describedSource(candidates)
}

// describedSource answers from a list that needs nothing asked.
func describedSource(candidates []Candidate) Source {
	return func(context.Context, *Environment, Request) (Result, error) {
		answer := make([]Candidate, len(candidates))
		copy(answer, candidates)

		return Result{Candidates: answer, KeepOrder: true}, nil
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
		candidates = append(candidates, Candidate{Value: host, Description: describeHost(profile, host == stored.DefaultHost)})
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

// describeHost says who this machine signs in to an instance as, and which
// instance is the default.
//
// A token login stores no user name, so describing an instance by its user
// alone left every one logged in with a token blank beside the default -- and
// a shell lays out a list in which only some values carry a description with
// the columns out of line.
func describeHost(profile config.StoredProfile, isDefault bool) string {
	description := strings.TrimSpace(profile.Username)
	if description == "" && strings.TrimSpace(profile.AuthMode) == "token" {
		description = "access token"
	}

	if isDefault {
		description = strings.TrimSpace(description + " (default)")
	}

	return description
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
