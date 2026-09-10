package deprecation

import (
	"slices"
	"strings"
	"testing"
)

func TestRemoveInIsTheMajorAfterTheOneThatWarned(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		deprecatedIn string
		want         int
	}{
		{deprecatedIn: "v4.1.0", want: 5},
		{deprecatedIn: "v4.0.0", want: 5},
		{deprecatedIn: "v12.3.4", want: 13},
	}

	for _, testCase := range testCases {
		entry := Entry{Name: "x", DeprecatedIn: testCase.deprecatedIn}
		got, err := entry.RemoveIn()
		if err != nil {
			t.Fatalf("%s: %v", testCase.deprecatedIn, err)
		}
		if got != testCase.want {
			t.Fatalf("%s: removal major %d, want %d", testCase.deprecatedIn, got, testCase.want)
		}
	}
}

// The case a declared removal number would get wrong: something deprecated
// during the cycle that is now shipping is not due in that cycle.
func TestOutstandingIgnoresDeprecationsFromTheCycleBeingShipped(t *testing.T) {
	t.Parallel()

	registry := []Entry{
		{Name: "old", DeprecatedIn: "v4.1.0"},
		{Name: "added during v5", DeprecatedIn: "v5.2.0"},
	}

	due, err := outstandingIn(registry, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(due) != 1 || due[0].Name != "old" {
		t.Fatalf("expected only the v4 deprecation to be due for v5, got %+v", due)
	}

	none, err := outstandingIn(registry, 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("nothing is due while v4 is still the pending major, got %+v", none)
	}
}

func TestEveryRegisteredEntryIsWellFormed(t *testing.T) {
	t.Parallel()

	for _, entry := range Entries {
		if entry.Name == "" || entry.Reason == "" || entry.Advice == "" {
			t.Fatalf("entry %+v is missing a field a reader needs", entry)
		}
		if _, err := entry.RemoveIn(); err != nil {
			t.Fatalf("entry %q: %v", entry.Name, err)
		}
	}
}

// Outstanding is the wrapper the tools call; this is the one place it reads the
// real registry.
func TestOutstandingReadsTheRegistry(t *testing.T) {
	t.Parallel()

	for _, entry := range Entries {
		removeIn, err := entry.RemoveIn()
		if err != nil {
			t.Fatalf("entry %q: %v", entry.Name, err)
		}

		due, err := Outstanding(removeIn)
		if err != nil {
			t.Fatalf("entry %q: %v", entry.Name, err)
		}
		if !slices.ContainsFunc(due, func(due Entry) bool { return due.Name == entry.Name }) {
			t.Fatalf("%q is due in v%d but Outstanding(%d) did not list it", entry.Name, removeIn, removeIn)
		}
	}
}

// A malformed DeprecatedIn must be loud. The alternative -- guessing a major --
// would put a removal deadline on a date nobody chose.
func TestAMalformedDeprecatedInIsRejectedRatherThanGuessed(t *testing.T) {
	t.Parallel()

	rejected := []string{"", "v4", "4", "vx.1.0", "v.1.0", "latest"}
	for _, version := range rejected {
		entry := Entry{Name: "x", DeprecatedIn: version}
		if _, err := entry.RemoveIn(); err == nil {
			t.Fatalf("DeprecatedIn %q was accepted", version)
		}
	}

	// The v prefix is optional; the three components are not.
	entry := Entry{Name: "x", DeprecatedIn: "4.1.0"}
	got, err := entry.RemoveIn()
	if err != nil {
		t.Fatalf("unprefixed version rejected: %v", err)
	}
	if got != 5 {
		t.Fatalf("removal major %d, want 5", got)
	}
}

// Outstanding cannot report on a list it cannot read, so it surfaces the error
// rather than reporting a shorter list than the truth.
func TestOutstandingRefusesAListItCannotRead(t *testing.T) {
	t.Parallel()

	_, err := outstandingIn([]Entry{{Name: "broken", DeprecatedIn: "someday"}}, 5)
	if err == nil {
		t.Fatal("expected an error for an unparseable entry")
	}
	if !strings.Contains(err.Error(), "broken") {
		t.Fatalf("error does not name the offending entry: %v", err)
	}
}

// Warning still has to say something useful when the version is unusable --
// it is printed on a user's terminal, not parsed.
func TestWarningFallsBackWhenTheVersionIsUnusable(t *testing.T) {
	t.Parallel()

	broken := Entry{Name: "bb x", DeprecatedIn: "someday", Advice: "use bb y"}
	warning := broken.Warning()
	if !strings.Contains(warning, "bb x") || !strings.Contains(warning, "use bb y") {
		t.Fatalf("fallback warning lost the name or the advice: %q", warning)
	}
	if strings.Contains(warning, "v0.0.0") {
		t.Fatalf("fallback warning invented a removal version: %q", warning)
	}

	sound := Entry{Name: "bb x", DeprecatedIn: "v4.1.0", Reason: "r", Advice: "use bb y"}
	if !strings.Contains(sound.Warning(), "v5.0.0") {
		t.Fatalf("warning omits the removal release: %q", sound.Warning())
	}
}
