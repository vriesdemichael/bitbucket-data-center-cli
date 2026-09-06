package pullrequest

import (
	"testing"

	pullrequestactivity "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequestactivity"
)

func TestBuildReviewSummaryFromThreads(t *testing.T) {
	t.Parallel()

	pullRequest := PullRequest{
		Reviewers: []Reviewer{
			{Name: "alice", Approved: true, Status: "APPROVED"},
			{Name: "bob", Status: "NEEDS_WORK"},
			{Name: "carol", Status: "UNAPPROVED"},
		},
		CommentCount:  intPtr(9),
		OpenTaskCount: intPtr(99), // ignored: the thread counts are authoritative
	}
	threads := pullrequestactivity.Summary{
		TotalThreads:  6,
		Unresolved:    2,
		Resolved:      3,
		Pending:       1,
		OpenTasks:     1,
		ResolvedTasks: 2,
	}

	summary := BuildReviewSummary(pullRequest, ReviewCounts{Threads: &threads})

	if summary.CountsSource != CountsSourceActivities {
		t.Fatalf("expected activities counts source, got %q", summary.CountsSource)
	}
	assertCount(t, "unresolved_threads", summary.UnresolvedThreads, 2)
	assertCount(t, "open_tasks", summary.OpenTasks, 1)
	assertCount(t, "resolved_threads", summary.ResolvedThreads, 3)
	assertCount(t, "resolved_tasks", summary.ResolvedTasks, 2)
	assertCount(t, "pending_comments", summary.PendingComments, 1)
	if summary.Approvals != 1 || summary.Reviewers != 3 {
		t.Fatalf("expected 1 approval of 3 reviewers, got %#v", summary)
	}
	if len(summary.NeedsWork) != 1 || summary.NeedsWork[0] != "bob" {
		t.Fatalf("expected bob to request changes, got %#v", summary.NeedsWork)
	}
	if summary.CommentCount == nil || *summary.CommentCount != 9 {
		t.Fatalf("expected the raw comment counter to be carried through, got %#v", summary.CommentCount)
	}
	assertActionRequired(t, summary.ActionRequired, boolPtr(true))
}

// Bitbucket 10.x drops the task counters from the single pull request payload,
// so the blocker-comment tally is the fallback that keeps `pr get` informative.
func TestBuildReviewSummaryFallsBackToTaskCounts(t *testing.T) {
	t.Parallel()

	pullRequest := PullRequest{Reviewers: []Reviewer{{Name: "alice", Approved: true, Status: "APPROVED"}}}

	summary := BuildReviewSummary(pullRequest, ReviewCounts{Tasks: &TaskCounts{Open: 2, Resolved: 5}})

	if summary.CountsSource != CountsSourceBlockerComments {
		t.Fatalf("expected blocker comment counts source, got %q", summary.CountsSource)
	}
	assertCount(t, "open_tasks", summary.OpenTasks, 2)
	assertCount(t, "resolved_tasks", summary.ResolvedTasks, 5)
	if summary.UnresolvedThreads != nil {
		t.Fatalf("expected thread counts to stay unmeasured, got %#v", summary.UnresolvedThreads)
	}
	assertActionRequired(t, summary.ActionRequired, boolPtr(true))
}

// The property counters still apply on listings, where Bitbucket does send them.
func TestBuildReviewSummaryFallsBackToProperties(t *testing.T) {
	t.Parallel()

	pullRequest := PullRequest{
		Reviewers:         []Reviewer{{Name: "alice", Approved: true, Status: "APPROVED"}},
		OpenTaskCount:     intPtr(2),
		ResolvedTaskCount: intPtr(5),
	}

	summary := BuildReviewSummary(pullRequest, ReviewCounts{})

	if summary.CountsSource != CountsSourceProperties {
		t.Fatalf("expected properties counts source, got %q", summary.CountsSource)
	}
	assertCount(t, "open_tasks", summary.OpenTasks, 2)
	assertCount(t, "resolved_tasks", summary.ResolvedTasks, 5)
	if summary.UnresolvedThreads != nil {
		t.Fatalf("expected no thread count without the activity feed, got %#v", summary.UnresolvedThreads)
	}
	assertActionRequired(t, summary.ActionRequired, boolPtr(true))
}

// A server that reports neither properties nor an activity feed must not be
// mistaken for a pull request with nothing outstanding: the counts have to come
// back absent, not zero.
func TestBuildReviewSummaryWithoutAnyCounts(t *testing.T) {
	t.Parallel()

	summary := BuildReviewSummary(PullRequest{Reviewers: []Reviewer{{Name: "alice", Approved: true}}}, ReviewCounts{})

	if summary.CountsSource != CountsSourceNone {
		t.Fatalf("expected counts source none, got %q", summary.CountsSource)
	}
	if summary.Measured() {
		t.Fatalf("expected the summary to report that nothing was measured")
	}
	if summary.OpenTasks != nil || summary.UnresolvedThreads != nil || summary.ResolvedTasks != nil {
		t.Fatalf("expected unmeasured counts to be absent rather than zero, got %#v", summary)
	}
	// Nothing was measured, so "no action required" is not something this
	// summary is entitled to say. Absent, not false.
	assertActionRequired(t, summary.ActionRequired, nil)
}

func assertCount(t *testing.T, name string, got *int, want int) {
	t.Helper()

	if got == nil {
		t.Fatalf("%s: expected %d, got an unmeasured count", name, want)
	}
	if *got != want {
		t.Fatalf("%s: expected %d, got %d", name, want, *got)
	}
}

func TestBuildReviewSummaryActionRequiredTriggers(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		threads pullrequestactivity.Summary
		review  []Reviewer
		want    bool
	}{
		{name: "clean", want: false},
		{name: "unresolved thread", threads: pullrequestactivity.Summary{Unresolved: 1}, want: true},
		{name: "open task", threads: pullrequestactivity.Summary{OpenTasks: 1, Unresolved: 1}, want: true},
		{name: "needs work", review: []Reviewer{{Name: "bob", Status: "needs_work"}}, want: true},
		{name: "pending only", threads: pullrequestactivity.Summary{Pending: 3}, want: false},
		{name: "resolved only", threads: pullrequestactivity.Summary{Resolved: 4, ResolvedTasks: 2}, want: false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			threads := testCase.threads
			summary := BuildReviewSummary(PullRequest{Reviewers: testCase.review}, ReviewCounts{Threads: &threads})
			assertActionRequired(t, summary.ActionRequired, boolPtr(testCase.want))
		})
	}
}

func TestBuildReviewSummaryNeedsWorkFallsBackToDisplayName(t *testing.T) {
	t.Parallel()

	summary := BuildReviewSummary(PullRequest{
		Reviewers: []Reviewer{{DisplayName: "Bob B", Status: "NEEDS_WORK"}},
	}, ReviewCounts{})

	if len(summary.NeedsWork) != 1 || summary.NeedsWork[0] != "Bob B" {
		t.Fatalf("expected the display name to be used, got %#v", summary.NeedsWork)
	}
}

// assertActionRequired compares a tri-state answer: true, false, or "not
// something this summary can say".
func assertActionRequired(t *testing.T, got, want *bool) {
	t.Helper()

	switch {
	case want == nil && got != nil:
		t.Fatalf("ActionRequired = %v, want it absent: a partial measurement must not answer", *got)
	case want != nil && got == nil:
		t.Fatalf("ActionRequired is absent, want %v", *want)
	case want != nil && *got != *want:
		t.Fatalf("ActionRequired = %v, want %v", *got, *want)
	}
}

// Both property-counter suites are live now, in
// TestLivePullRequestReviewVisibility, and moving them made the point of them
// sharper.
//
// Get and List share mapPullRequest, so the counters the live listing carries
// prove the decode against a payload Bitbucket really sends. What the unit
// tests could not notice is that the two endpoints disagree: Bitbucket 10.x
// reports properties on the listing and omits them on the single pull
// request, which is the entire reason `pr get` falls back to the
// blocker-comment tally. Asserting both halves against payloads written here
// would have gone on passing whichever way the server went.
