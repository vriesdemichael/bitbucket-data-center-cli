package compat

import (
	"testing"

	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// TestIsNoCreatesReadsTheTypeAsBitbucketSpellsIt covers the one type a release
// before 9.4 refuses, however the caller spelled it. A flag's value arrives as
// the caller typed it, and the comparison decides whether the release is asked
// about at all.
func TestIsNoCreatesReadsTheTypeAsBitbucketSpellsIt(t *testing.T) {
	t.Parallel()

	for _, spelling := range []string{"no-creates", "NO-CREATES", " no-creates "} {
		if !IsNoCreates(spelling) {
			t.Errorf("%q is not read as the no-creates type", spelling)
		}
	}
	for _, other := range []string{"no-deletes", "read-only", "fast-forward-only", "", "creates"} {
		if IsNoCreates(other) {
			t.Errorf("%q is read as the no-creates type", other)
		}
	}
}

// TestOnlyNoCreatesNarrowsAListingTheReleaseWouldNotFilter covers what bb does
// with a listing a release before 9.4 cannot filter: the request goes
// unfiltered and the answer is narrowed here.
//
// A release that has the type never holds one, so the narrowing has to leave
// nothing of another type behind -- a caller asking for no-creates restrictions
// and reading back a read-only one would act on a restriction they did not ask
// about.
func TestOnlyNoCreatesNarrowsAListingTheReleaseWouldNotFilter(t *testing.T) {
	t.Parallel()

	restriction := func(kind string) openapigenerated.RestRefRestriction {
		return openapigenerated.RestRefRestriction{Type: &kind}
	}
	listing := []openapigenerated.RestRefRestriction{
		restriction("read-only"),
		restriction("no-creates"),
		{},
		restriction("no-deletes"),
		restriction("NO-CREATES"),
	}

	narrowed := OnlyNoCreates(listing)
	if len(narrowed) != 2 {
		t.Fatalf("narrowed to %d restrictions, want the 2 that are no-creates: %+v", len(narrowed), narrowed)
	}
	for _, kept := range narrowed {
		if kept.Type == nil || !IsNoCreates(*kept.Type) {
			t.Errorf("narrowing kept %+v", kept)
		}
	}

	// A listing with none is an empty answer rather than a nil one, which is
	// what the caller encodes as [].
	if narrowed := OnlyNoCreates([]openapigenerated.RestRefRestriction{restriction("read-only")}); narrowed == nil || len(narrowed) != 0 {
		t.Errorf("a listing holding no no-creates restriction narrowed to %#v, want an empty listing", narrowed)
	}
}
