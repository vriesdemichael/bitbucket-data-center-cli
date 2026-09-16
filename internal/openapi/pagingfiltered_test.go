package openapi_test

import (
	"context"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
)

// filteringSource answers the way the dashboard listing does: Bitbucket sizes
// the page, and the caller's fetch drops the rows the caller did not ask about.
type filteringSource struct {
	total int
	keep  func(int) bool

	requests []int
}

func (source *filteringSource) fetch(_ context.Context, start, limit int) (openapi.Page[int], error) {
	source.requests = append(source.requests, limit)

	end := min(start+limit, source.total)

	kept := []int{}
	for item := start; item < end; item++ {
		if source.keep(item) {
			kept = append(kept, item)
		}
	}

	last := end >= source.total
	next := end

	return openapi.Page[int]{Values: kept, IsLastPage: &last, NextPageStart: &next}, nil
}

// TestPageThroughDoesNotWalkAFilteredListingOneRowAtATime covers the request
// size when the fetch filters.
//
// `bb pr status --project PROJ` and the MCP dashboard tool filter by project
// after Bitbucket has sized the window. Asking for exactly what is still
// missing then asks for one row, has it filtered out, and asks for one row
// again: a listing eight pages long took a request per entry, and a dashboard
// of any size made the tool look hung.
func TestPageThroughDoesNotWalkAFilteredListingOneRowAtATime(t *testing.T) {
	t.Parallel()

	// Everything the caller may see sits at the front, so the walk reaches the
	// last few entries early and spends the rest of the listing there.
	source := &filteringSource{total: 200, keep: func(item int) bool { return item < 20 }}

	got, err := openapi.PageThrough(context.Background(), 0, 25, source.fetch)
	if err != nil {
		t.Fatalf("PageThrough: %v", err)
	}
	if len(got) != 20 {
		t.Fatalf("returned %d items, want the 20 the filter keeps", len(got))
	}

	// Eight windows cover two hundred entries. Anything near two hundred
	// requests is the row-at-a-time walk.
	if len(source.requests) > 10 {
		t.Fatalf("walked 200 entries in %d requests: %v", len(source.requests), source.requests)
	}
	for index, limit := range source.requests {
		if limit < 2 {
			t.Errorf("request %d asked for %d row(s), which is a round trip per entry", index, limit)
		}
	}
}

// TestPageThroughStillAsksForWhatIsMissingWhenNothingIsFiltered keeps the fix
// above from becoming "always ask for a full window".
//
// A server whose own page is smaller than the request also hands back fewer
// rows than were asked for, and that is not filtering: nextPageStart counts the
// rows it returned, so the loop can tell the two apart and must, or every
// caller with a small --limit over-fetches a full page to throw most of it
// away.
func TestPageThroughStillAsksForWhatIsMissingWhenNothingIsFiltered(t *testing.T) {
	t.Parallel()

	source := &filteringSource{total: 100, keep: func(int) bool { return true }}

	if _, err := openapi.PageThrough(context.Background(), 0, 30, source.fetch); err != nil {
		t.Fatalf("PageThrough: %v", err)
	}

	if len(source.requests) != 2 {
		t.Fatalf("expected two requests, got %v", source.requests)
	}
	if source.requests[1] != 5 {
		t.Errorf("the second request asked for %d, want the 5 still missing", source.requests[1])
	}
}
