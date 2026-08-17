// Package paging owns the --limit and --all flags shared by every list command.
//
// It exists because those flags were registered 32 times by hand and described
// 21 different ways, most of them as a "page size". That was wrong twice over.
// The value is a cap on results, not on requests. And a page is not something a
// CLI caller can navigate: there is no cursor to advance, so a "page size" names
// an HTTP detail the services already handle rather than anything the caller can
// act on. See ADR-050.
package paging

import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// DefaultLimit is the cap a list command applies when the caller says nothing.
const DefaultLimit = 25

// unlimitedLimit is the cap --all hands to a service.
//
// Every list service paginates until it holds this many results or the server
// reports the last page, so a value no real collection reaches means "keep going
// until Bitbucket says there is no more". Deliberately not math.MaxInt: services
// derive a per-page size from the remaining count, and the generated client
// carries page limits as float32.
const unlimitedLimit = 1_000_000

// Options is the resolved --limit/--all pair for one command.
type Options struct {
	limit int
	all   bool
}

// Register adds --limit and --all to the command's own flags.
func (options *Options) Register(command *cobra.Command, defaultLimit int) {
	options.register(command, command.Flags(), defaultLimit)
}

// RegisterPersistent adds them to the command's persistent flags, for a parent
// whose subcommands all list something.
func (options *Options) RegisterPersistent(command *cobra.Command, defaultLimit int) {
	options.register(command, command.PersistentFlags(), defaultLimit)
}

func (options *Options) register(command *cobra.Command, flags *pflag.FlagSet, defaultLimit int) {
	if defaultLimit <= 0 {
		defaultLimit = DefaultLimit
	}

	flags.IntVar(&options.limit, "limit", defaultLimit, "Maximum number of results to return")
	flags.BoolVar(&options.all, "all", false, "Return every result rather than the first --limit")

	// Declarative rather than a check inside every RunE: Cobra validates the
	// group during Execute, and the resulting error is classified as a usage
	// error, so it exits 2 like any other bad invocation.
	command.MarkFlagsMutuallyExclusive("all", "limit")
}

// ServiceLimit is the cap to pass to a service.
//
// One more than the caller asked for. That extra result is what makes
// truncation detectable, and Truncate drops it. Every call site using this must
// pass its results through Truncate — enforced by
// TestEveryServiceLimitCallSiteTruncates.
func (options Options) ServiceLimit() int {
	if options.all {
		return unlimitedLimit
	}

	return options.effectiveLimit()
}

func (options Options) effectiveLimit() int {
	if options.limit <= 0 {
		return DefaultLimit
	}

	return options.limit
}

// LimitReached reports that a result set came back at the cap, so there may be
// more behind it.
//
// Deliberately this rather than a precise "there is definitely more". Knowing
// precisely would mean fetching one extra result at every call site and
// dropping it, and a site that forgot the drop would silently return one row
// too many — a correctness bug traded for a nicety. Reaching the cap is the
// signal a caller acts on either way: ask again with a higher --limit or --all.
//
// Always false under --all, which has no cap to reach.
func LimitReached(options Options, count int) bool {
	if options.all {
		return false
	}

	return count >= options.effectiveLimit()
}
