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

	// Advice names what to do instead. Not every deprecation has a successor --
	// bb bulk has none -- so this is prose rather than a replacement identifier.
	Advice string
}

// Entries is the registry.
var Entries = []Entry{
	{
		Name:         "bb bulk",
		DeprecatedIn: "v4.1.0",
		Reason:       "its operations are repository configuration, and four of the nine are settable once at the project level, where Bitbucket already cascades them",
		Advice:       "set project-wide policy with bb project permissions, webhook, default-task and branch-restriction; script bb directly for anything else",
	},
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
