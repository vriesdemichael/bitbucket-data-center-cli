package openapi

import "context"

// Page is one answer from a paged Bitbucket endpoint, reduced to the three
// fields the convention turns on.
//
// The generated client gives every endpoint its own page type, so a caller
// adapts its response into this rather than the loop knowing about any of them.
type Page[T any] struct {
	Values []T
	// IsLastPage and NextPageStart are pointers because Bitbucket omits them,
	// and an omitted IsLastPage is not "there is more": a page that does not
	// say it has a successor is treated as the end, which is the safe reading.
	IsLastPage    *bool
	NextPageStart *int32
}

// PageThrough follows Bitbucket's paging convention until it has maxResults or
// the server says there is no more.
//
// Every service used to carry its own copy of this loop -- eight of them, each
// with its own reading of when to stop, whether to trim an overshooting page,
// and what an absent nextPageStart means. Nothing kept them in step, and each
// needed its own test to say it worked, which is how the same assertion ended
// up written eight times over eight fixtures.
//
// The stopping rules, in one place:
//
//   - ask only for what is still missing, so the last page is not oversized;
//   - a page with no values ends the listing, however the flags read;
//   - isLastPage, an absent isLastPage, or an absent nextPageStart all end it;
//   - a start that does not advance ends it, because a server repeating itself
//     would otherwise loop forever;
//   - the result is trimmed to maxResults, because a server may return more
//     than the limit asked for.
// The start is where to begin, for the callers that expose an offset of their
// own; zero is the beginning. It is absolute, and so is every value the server
// answers with, which is why it is a parameter rather than something added to
// the loop's own counter.
func PageThrough[T any](
	ctx context.Context,
	start, maxResults int,
	fetch func(ctx context.Context, start, limit int) (Page[T], error),
) ([]T, error) {
	if maxResults <= 0 {
		return []T{}, nil
	}
	if start < 0 {
		start = 0
	}

	results := make([]T, 0, maxResults)

	for {
		remaining := maxResults - len(results)
		if remaining <= 0 {
			break
		}

		page, err := fetch(ctx, start, remaining)
		if err != nil {
			return nil, err
		}
		if len(page.Values) == 0 {
			break
		}

		results = append(results, page.Values...)

		if len(results) >= maxResults || page.IsLastPage == nil || *page.IsLastPage || page.NextPageStart == nil {
			break
		}

		next := int(*page.NextPageStart)
		// A server that answers with the same start again would otherwise be
		// followed forever, one identical page at a time.
		if next <= start {
			break
		}
		start = next
	}

	if len(results) > maxResults {
		results = results[:maxResults]
	}

	return results, nil
}
