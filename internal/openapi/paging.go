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
	IsLastPage *bool
	// NextPageStart is an int rather than the generated client's int32, so that
	// every adapter widens and none narrows. The endpoints that are decoded by
	// hand count the next offset themselves, and writing it back into an int32
	// is a conversion the compiler cannot prove safe -- five of them, each an
	// offset no Bitbucket will ever return, each needing its own suppression.
	NextPageStart *int
}

// Offset adapts the generated client's next-page start for a Page.
//
// A nil start stays nil: it is how an endpoint says it has no successor, which
// is not the same as starting at zero.
func Offset(start *int32) *int {
	if start == nil {
		return nil
	}

	next := int(*start)

	return &next
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
//   - a page with no values ends the listing only if the server does not say
//     there is more behind it: Bitbucket filters some windows after sizing
//     them, so an empty page can sit in front of a full one;
//   - isLastPage, an absent isLastPage, or an absent nextPageStart all end it;
//   - a start that does not advance ends it, because a server repeating itself
//     would otherwise loop forever;
//   - the result is trimmed to maxResults, because a server may return more
//     than the limit asked for.
//
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
		// An empty page is not the end of the listing unless the server agrees.
		//
		// Bitbucket filters some windows after sizing them -- entries the caller
		// may not see are removed from a page that still counts toward the
		// offset -- so a page can come back with nothing in it while there is
		// more behind it. Stopping there returns an empty answer for a listing
		// that has plenty, and the caller cannot tell that from "there is
		// nothing".
		//
		// The same shape arises for any caller whose fetch filters: a page that
		// contributes nothing is not a page that ends anything.
		if len(page.Values) == 0 {
			next, ok := advance(page, start)
			if !ok {
				break
			}
			start = next

			continue
		}

		results = append(results, page.Values...)

		if len(results) >= maxResults {
			break
		}

		next, ok := advance(page, start)
		if !ok {
			break
		}
		start = next
	}

	if len(results) > maxResults {
		results = results[:maxResults]
	}

	return results, nil
}

// advance reports where the next page starts, and whether there is one.
//
// A page that does not say it has a successor ends the listing, and so does one
// whose successor is not ahead of where this page began -- a server repeating
// an offset would otherwise be followed forever, one identical page at a time.
func advance[T any](page Page[T], start int) (int, bool) {
	if page.IsLastPage == nil || *page.IsLastPage || page.NextPageStart == nil {
		return 0, false
	}

	next := *page.NextPageStart
	if next <= start {
		return 0, false
	}

	return next, true
}
