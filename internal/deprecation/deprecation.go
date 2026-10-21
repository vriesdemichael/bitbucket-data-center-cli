// Package deprecation is the one list of things on their way out.
//
// Three consumers read it: the command that warns a user at runtime, the CI
// step that reminds the project on next, and the promotion check that reports
// what is still outstanding when a major is about to ship. Sharing the list is
// the point -- a warning that says one thing while a gate believes another is
// worse than neither.
package deprecation

import (
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Entry is one deprecated surface.
//
// RemoveIn is derived rather than declared. ADR-084 gives a deprecation exactly
// one major, so the removal major follows from DeprecatedIn and there is no
// second number to get wrong or to quietly extend.
type Entry struct {
	// Name is what the reader recognises: a command path, a flag, a field.
	Name string

	// DeprecatedIn is the release the warning first shipped in, as vMAJOR.MINOR.PATCH.
	DeprecatedIn string

	// Reason says why, in one sentence, because a warning that only says
	// "deprecated" leaves the reader to guess whether it still works.
	Reason string

	// Advice names what to do instead. Not every deprecation has a successor,
	// so this is prose rather than a replacement identifier.
	Advice string
}

// Entries is the registry.
var Entries = []Entry{
	// bb ai mcp serve offers only the tools that read unless --allow-writes is
	// passed, so --read-only says what the server does anyway.
	{
		Name:         "bb ai mcp serve --read-only",
		DeprecatedIn: "v6.0.0",
		Reason:       "the server offers only the tools that read unless --allow-writes is passed",
		Advice:       "Remove it.",
	},
	{
		Name:         "bb ai mcp tools --read-only",
		DeprecatedIn: "v6.0.0",
		Reason:       "it lists the tools a read-only server exposes unless --allow-writes is passed",
		Advice:       "Remove it.",
	},
	// get_pull_request named its pull request id while every other pull
	// request tool said pr_id (#768). It still takes id, which warns nobody: the
	// tool's schema describes it as the former name.
	{
		Name:         "bb ai mcp serve tool get_pull_request argument id",
		DeprecatedIn: "v6.0.0",
		Reason:       "pr_id names the pull request, as it does on every pull request tool",
		Advice:       "Send pr_id.",
	},

	// Flags that mirrored a Bitbucket request parameter and changed nothing a
	// caller could see.
	//
	// --include-unique asked Bitbucket to name the build when only one had
	// failed, was running or had passed. bb printed the five counts either
	// way, and bb build status get and set, which inherited the flag, never
	// sent it.
	{
		Name:         "bb build status stats --include-unique",
		DeprecatedIn: "v5.0.0",
		Reason:       "bb prints the same counts with it and without it",
		Advice:       "Remove it. bb build status get lists each build with its state.",
	},
	{
		Name:         "bb build status get --include-unique",
		DeprecatedIn: "v5.0.0",
		Reason:       "the command never read it",
		Advice:       "Remove it.",
	},
	{
		Name:         "bb build status set --include-unique",
		DeprecatedIn: "v5.0.0",
		Reason:       "the command never read it",
		Advice:       "Remove it.",
	},
	// bb build required delete took --limit and --all for its preview, which
	// looked the check up among as many as they allowed. A check past the
	// cap was predicted not found and deleted by the real run (#728), so the
	// preview reads every check and the flags bound nothing.
	{
		Name:         "bb build required delete --limit",
		DeprecatedIn: "v5.0.0",
		Reason:       "the preview reads every required build check to find the one it deletes",
		Advice:       "Remove it.",
	},
	{
		Name:         "bb build required delete --all",
		DeprecatedIn: "v5.0.0",
		Reason:       "the preview reads every required build check to find the one it deletes",
		Advice:       "Remove it.",
	},
}

// WarnFlags writes the registered warning for each of a command's deprecated
// flags that the invocation passed, one line each.
//
// command is the command as a person types it, bb included, and passed reports
// whether a flag was given: a Cobra command's Flags().Changed. The warnings go
// to the command's stderr, so a --json document on stdout is unchanged
// (ADR-084), and they are read from the registry, so they say what the reports
// listing outstanding deprecations say. A flag with no entry is skipped: the
// registry is what makes a flag deprecated, not the call that mentions it.
func WarnFlags(stderr io.Writer, passed func(flag string) bool, command string, flags ...string) {
	for _, flag := range flags {
		if !passed(flag) {
			continue
		}
		if entry, ok := Named(command + " --" + flag); ok {
			fmt.Fprintln(stderr, entry.Warning())
		}
	}
}

// Named returns the registered entry called name.
//
// A command that warns about a form it still accepts reads the entry from
// here, so the warning and the reports that list outstanding deprecations
// cannot disagree about it.
func Named(name string) (Entry, bool) {
	for _, entry := range Entries {
		if entry.Name == name {
			return entry, true
		}
	}

	return Entry{}, false
}

// RemoveIn is the major this entry is due to be removed in: the one after the
// release that started warning about it.
func (entry Entry) RemoveIn() (int, error) {
	major, err := majorOf(entry.DeprecatedIn)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", entry.Name, err)
	}

	return major + 1, nil
}

// Warning is the line a user sees, and the line the reports repeat.
func (entry Entry) Warning() string {
	removeIn, err := entry.RemoveIn()
	if err != nil {
		return fmt.Sprintf("warning: %s is deprecated. %s", entry.Name, entry.Advice)
	}

	return fmt.Sprintf(
		"warning: %s is deprecated and will be removed in v%d.0.0 — %s. %s",
		entry.Name, removeIn, entry.Reason, entry.Advice,
	)
}

// Outstanding returns the entries due for removal in pendingMajor or earlier.
//
// An entry deprecated during the cycle that is now shipping is not outstanding:
// deprecated in v5.2.0 means due in v6, so a pending v5 leaves it alone.
func Outstanding(pendingMajor int) ([]Entry, error) {
	return outstandingIn(Entries, pendingMajor)
}

// outstandingIn is the same question asked of an arbitrary list, so a test can
// pose it without swapping the registry out from under a parallel reader.
func outstandingIn(entries []Entry, pendingMajor int) ([]Entry, error) {
	var due []Entry
	for _, entry := range entries {
		removeIn, err := entry.RemoveIn()
		if err != nil {
			return nil, err
		}
		if removeIn <= pendingMajor {
			due = append(due, entry)
		}
	}

	return due, nil
}

func majorOf(version string) (int, error) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(version), "v")
	head, _, found := strings.Cut(trimmed, ".")
	if !found {
		return 0, fmt.Errorf("DeprecatedIn %q is not vMAJOR.MINOR.PATCH", version)
	}

	major, err := strconv.Atoi(head)
	if err != nil {
		return 0, fmt.Errorf("DeprecatedIn %q is not vMAJOR.MINOR.PATCH", version)
	}

	return major, nil
}
