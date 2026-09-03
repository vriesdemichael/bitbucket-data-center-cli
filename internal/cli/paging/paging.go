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
// Exactly what the caller asked for, not one more: an earlier design fetched an
// extra result to make truncation precisely detectable, and the comment here
// described it long after it was abandoned. See LimitReached for why it was.
//
// What a service does with this number is not uniform. Eight take it as a total
// cap (MaxResults, #471); the rest take it as a page size and read to
// exhaustion. Pass the results through Truncate either way -- on a service that
// already capped it changes nothing, and on one that did not it is what makes
// --limit mean what its help text says.
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

// Truncate caps a result set to what --limit asked for.
//
// Needed because ServiceLimit means a page size to some services, which then
// page to exhaustion: `project permissions users list --limit 5` fetched every
// entry in the project, in pages of five, and printed all of them. A smaller
// --limit produced more requests and the same complete answer (#473).
//
// Safe at any call site. Truncating a result set a service already capped is a
// no-op, so this does not require knowing which semantic applies -- which is
// the knowledge that was missing when those call sites were written.
//
// A no-op under --all, which asked for everything.
func Truncate[T any](options Options, results []T) []T {
	if options.all {
		return results
	}

	limit := options.effectiveLimit()
	if len(results) <= limit {
		return results
	}

	return results[:limit]
}
