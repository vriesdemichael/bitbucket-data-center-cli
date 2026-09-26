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

	// Advice names what to do instead. Not every deprecation has a successor,
	// so this is prose rather than a replacement identifier.
	Advice string
}

// Entries is the registry.
var Entries = []Entry{
	// The bb update fields that described a swap left for a helper to finish
	// after bb had exited. bb update installs the new binary before it exits
	// on every operating system (ADR-092), so they are always false or empty.
	// An output field warns nobody at runtime (ADR-084): its schema description
	// and the release notes say it is deprecated.
	{
		Name:         "bb update --json field scheduled",
		DeprecatedIn: "v5.0.0",
		Reason:       "bb update replaces the binary itself before it exits, on every operating system",
		Advice:       "Read the applied field instead.",
	},
	{
		Name:         "bb update --json field staged",
		DeprecatedIn: "v5.0.0",
		Reason:       "bb update replaces the binary itself before it exits, on every operating system",
		Advice:       "Read the applied field instead.",
	},
	{
		Name:         "bb update --json field paths.staged",
		DeprecatedIn: "v5.0.0",
		Reason:       "bb update replaces the binary itself before it exits, on every operating system",
		Advice:       "Read the applied field instead.",
	},
	{
		Name:         "bb update --json field paths.swapResult",
		DeprecatedIn: "v5.0.0",
		Reason:       "bb update replaces the binary itself before it exits, on every operating system",
		Advice:       "Read the applied field instead.",
	},

	// bb ai mcp serve withheld the tools that change whether a pull request
	// merges unless --yolo was passed. Every tool is exposed now, and those
	// tools ask the person to confirm each call through the MCP client (#620),
	// so the flags that lifted the gate and the fields that reported it no
	// longer have anything to say.
	{
		Name:         "bb ai mcp serve --yolo",
		DeprecatedIn: "v5.0.0",
		Reason:       "every tool is exposed, and the ones that change whether a pull request merges ask the person to confirm each call in the MCP client",
		Advice:       "Remove it. Pass --read-only to expose only the tools that read.",
	},
	{
		Name:         "bb ai mcp serve --allow-writes",
		DeprecatedIn: "v5.0.0",
		Reason:       "every tool is exposed, and the ones that change whether a pull request merges ask the person to confirm each call in the MCP client",
		Advice:       "Remove it. Pass --read-only to expose only the tools that read.",
	},
	{
		Name:         "bb ai mcp tools --safe-only",
		DeprecatedIn: "v5.0.0",
		Reason:       "every tool is exposed without --yolo, so it no longer narrows the list",
		Advice:       "Pass --read-only to list the tools a read-only server exposes.",
	},
	{
		Name:         "bb ai mcp tools --json field safe",
		DeprecatedIn: "v5.0.0",
		Reason:       "every tool is exposed without --yolo, so it is always true",
		Advice:       "Read asks for whether a tool asks first, and writes for whether it changes anything.",
	},
	{
		Name:         "bb ai mcp tools --json field exposure",
		DeprecatedIn: "v5.0.0",
		Reason:       "every tool is exposed without --yolo, so it is always SAFE",
		Advice:       "Read asks for whether a tool asks first, and writes for whether it changes anything.",
	},
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
