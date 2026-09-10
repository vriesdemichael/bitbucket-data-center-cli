package deprecation

import "testing"

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

	saved := Entries
	t.Cleanup(func() { Entries = saved })

	Entries = []Entry{
		{Name: "old", DeprecatedIn: "v4.1.0"},
		{Name: "added during v5", DeprecatedIn: "v5.2.0"},
	}

	due, err := Outstanding(5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(due) != 1 || due[0].Name != "old" {
		t.Fatalf("expected only the v4 deprecation to be due for v5, got %+v", due)
	}

	none, err := Outstanding(4)
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
