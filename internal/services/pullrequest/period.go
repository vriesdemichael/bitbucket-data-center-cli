package pullrequest

import (
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
)

// The dates a period can apply to.
const (
	DateCreated = "created"
	DateUpdated = "updated"
	DateClosed  = "closed"
)

// Period bounds a listing by one of a pull request's dates.
//
// Bitbucket has no such parameter, so each page is filtered as it arrives,
// inside the walk, and MaxResults counts what is in the period.
type Period struct {
	// Since and Until are the bounds, both inclusive, in milliseconds since
	// the epoch. Zero leaves that side open.
	Since int64
	Until int64
	// Field is the date the bounds apply to: created, updated or closed.
	// Empty is created.
	Field string
}

func (period Period) bounded() bool {
	return period.Since > 0 || period.Until > 0
}

// date is the date the period looks at, and zero when the pull request has
// none -- one that is still open has no closed date.
func (period Period) date(pullRequest PullRequest) int64 {
	switch period.Field {
	case DateUpdated:
		return pullRequest.UpdatedDate
	case DateClosed:
		return pullRequest.ClosedDate
	default:
		return pullRequest.CreatedDate
	}
}

// includes reports whether a pull request falls in the period.
func (period Period) includes(pullRequest PullRequest) bool {
	if !period.bounded() {
		return true
	}

	date := period.date(pullRequest)
	if date == 0 {
		return false
	}
	if period.Since > 0 && date < period.Since {
		return false
	}

	return period.Until == 0 || date <= period.Until
}

// past reports whether a pull request, and so every one listed after it, was
// last updated before the period began.
//
// Bitbucket lists pull requests newest first by the date they were last
// updated, and one is never created or closed after it was last updated. So
// whichever date the period looks at, nothing behind a pull request updated
// before Since can be in it, and the walk can stop there rather than page back
// to the first pull request ever opened.
func (period Period) past(pullRequest PullRequest) bool {
	return period.Since > 0 && pullRequest.UpdatedDate > 0 && pullRequest.UpdatedDate < period.Since
}

// within is one page of a listing reduced to the period: the pull requests that
// keep accepts and the period includes, up to the first one past it. ended
// says that one was met, so the walk has nothing further to find.
func (period Period) within(page []PullRequest, keep func(PullRequest) bool) (kept []PullRequest, ended bool) {
	kept = make([]PullRequest, 0, len(page))
	for _, pullRequest := range page {
		if period.past(pullRequest) {
			return kept, true
		}
		if keep(pullRequest) && period.includes(pullRequest) {
			kept = append(kept, pullRequest)
		}
	}

	return kept, false
}

// periodPage is a page of the walk, marked as the last one when the period
// ended in it.
func periodPage(response pagedPullRequestResponse, kept []PullRequest, ended bool) openapi.Page[PullRequest] {
	page := pullRequestPage(response, kept)
	if ended {
		last := true
		page.IsLastPage = &last
		page.NextPageStart = nil
	}

	return page
}
