package openapi_test

import (
	"context"
	"errors"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
)

// pagedSource answers like a server holding items, one page at a time, and
// records what it was asked for.
type pagedSource struct {
	items    []int
	pageSize int
	// stuckAt makes the source answer with a nextPageStart that does not
	// advance, which is the shape that would spin a loop forever.
	stuckAt int
	// omitFlags drops isLastPage and nextPageStart, which Bitbucket does on
	// some endpoints.
	omitFlags bool

	requests []struct{ start, limit int }
}

func (source *pagedSource) fetch(_ context.Context, start, limit int) (openapi.Page[int], error) {
	source.requests = append(source.requests, struct{ start, limit int }{start, limit})

	if start >= len(source.items) {
		return openapi.Page[int]{}, nil
	}

	end := min(start+min(limit, source.pageSize), len(source.items))
	page := openapi.Page[int]{Values: source.items[start:end]}

	if source.omitFlags {
		return page, nil
	}

	last := end >= len(source.items)
	page.IsLastPage = &last
	if !last {
		next := int32(end)
		if source.stuckAt > 0 && end >= source.stuckAt {
			next = int32(start)
		}
		page.NextPageStart = &next
	}

	return page, nil
}

func items(count int) []int {
	values := make([]int, count)
	for index := range values {
		values[index] = index
	}

	return values
}

// The rules the eight hand-written copies each had their own reading of.
func TestPageThrough(t *testing.T) {
	cases := []struct {
		name       string
		source     *pagedSource
		start      int
		maxResults int
		want       int
		wantCalls  int
	}{
		{
			name:       "follows pages to the end",
			source:     &pagedSource{items: items(10), pageSize: 3},
			maxResults: 100,
			want:       10,
			wantCalls:  4,
		},
		{
			name:       "stops at the cap without asking for more",
			source:     &pagedSource{items: items(10), pageSize: 3},
			maxResults: 5,
			want:       5,
			wantCalls:  2,
		},
		{
			name:       "a cap inside one page costs one request",
			source:     &pagedSource{items: items(10), pageSize: 10},
			maxResults: 4,
			want:       4,
			wantCalls:  1,
		},
		{
			// An endpoint that sends neither flag: one page is all there is to
			// go on, and inventing a successor would loop.
			name:       "an omitted isLastPage ends the listing",
			source:     &pagedSource{items: items(10), pageSize: 3, omitFlags: true},
			maxResults: 100,
			want:       3,
			wantCalls:  1,
		},
		{
			// The shape that would spin forever: a server answering with a
			// start it has already served.
			name:       "a start that does not advance ends the listing",
			source:     &pagedSource{items: items(10), pageSize: 3, stuckAt: 3},
			maxResults: 100,
			want:       3,
			wantCalls:  1,
		},
		{
			name:       "an empty first page ends the listing",
			source:     &pagedSource{items: nil, pageSize: 3},
			maxResults: 100,
			want:       0,
			wantCalls:  1,
		},
		{
			name:       "a start offset begins where it says",
			source:     &pagedSource{items: items(10), pageSize: 3},
			start:      7,
			maxResults: 100,
			want:       3,
			wantCalls:  1,
		},
		{
			name:       "a zero cap asks for nothing",
			source:     &pagedSource{items: items(10), pageSize: 3},
			maxResults: 0,
			want:       0,
			wantCalls:  0,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := openapi.PageThrough(context.Background(),
				testCase.start, testCase.maxResults, testCase.source.fetch)
			if err != nil {
				t.Fatalf("PageThrough: %v", err)
			}

			if len(got) != testCase.want {
				t.Errorf("returned %d items, want %d (%v)", len(got), testCase.want, got)
			}
			if len(testCase.source.requests) != testCase.wantCalls {
				t.Errorf("made %d requests, want %d: %v",
					len(testCase.source.requests), testCase.wantCalls, testCase.source.requests)
			}
		})
	}
}

// Asking for more than is left would let a server return past the cap, which
// the old copies handled by trimming afterwards -- some of them.
func TestPageThroughNeverAsksForMoreThanItStillNeeds(t *testing.T) {
	source := &pagedSource{items: items(10), pageSize: 4}

	if _, err := openapi.PageThrough(context.Background(), 0, 6, source.fetch); err != nil {
		t.Fatalf("PageThrough: %v", err)
	}

	if len(source.requests) != 2 {
		t.Fatalf("expected two requests, got %v", source.requests)
	}
	if source.requests[0].limit != 6 {
		t.Errorf("first request asked for %d, want the full 6", source.requests[0].limit)
	}
	if source.requests[1].limit != 2 {
		t.Errorf("second request asked for %d, want the 2 still missing", source.requests[1].limit)
	}
}

// A server that overshoots the limit must not overshoot the caller.
func TestPageThroughTrimsAnOversizedPage(t *testing.T) {
	generous := func(_ context.Context, _, _ int) (openapi.Page[int], error) {
		last := true

		return openapi.Page[int]{Values: items(50), IsLastPage: &last}, nil
	}

	got, err := openapi.PageThrough(context.Background(), 0, 5, generous)
	if err != nil {
		t.Fatalf("PageThrough: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("returned %d items, want the 5 asked for", len(got))
	}
}

func TestPageThroughSurfacesTheFetchError(t *testing.T) {
	wanted := errors.New("the page could not be read")

	_, err := openapi.PageThrough(context.Background(), 0, 10,
		func(_ context.Context, _, _ int) (openapi.Page[int], error) {
			return openapi.Page[int]{}, wanted
		})

	if !errors.Is(err, wanted) {
		t.Fatalf("err = %v, want it to carry the fetch failure", err)
	}
}
