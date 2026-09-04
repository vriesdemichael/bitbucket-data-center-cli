package pullrequest

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	pullrequestactivity "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequestactivity"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
)

// Bitbucket ships comment and task counters in an undocumented "properties"
// object. They are decoded defensively, so both the present and absent cases
// need to hold.
func TestGetPullRequestDecodesPropertyCounters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/rest/api/latest/projects/TEST/repos/demo/pull-requests/22" {
			http.NotFound(w, request)
			return
		}
		_, _ = fmt.Fprint(w, `{"id":22,"title":"Feature","state":"OPEN","open":true,
          "fromRef":{"displayId":"feature/a"},"toRef":{"displayId":"master"},
          "properties":{"commentCount":7,"openTaskCount":2,"resolvedTaskCount":5}}`)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")

	cfg, err := config.LoadFromEnv()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	fetched, err := NewService(httpclient.NewFromConfig(cfg)).Get(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, "22")
	if err != nil {
		t.Fatalf("get pull request: %v", err)
	}

	if fetched.CommentCount == nil || *fetched.CommentCount != 7 {
		t.Fatalf("expected comment count 7, got %#v", fetched.CommentCount)
	}
	if fetched.OpenTaskCount == nil || *fetched.OpenTaskCount != 2 {
		t.Fatalf("expected open task count 2, got %#v", fetched.OpenTaskCount)
	}
	if fetched.ResolvedTaskCount == nil || *fetched.ResolvedTaskCount != 5 {
		t.Fatalf("expected resolved task count 5, got %#v", fetched.ResolvedTaskCount)
	}
}

func TestGetPullRequestWithoutPropertyCounters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/rest/api/latest/projects/TEST/repos/demo/pull-requests/22" {
			http.NotFound(w, request)
			return
		}
		_, _ = fmt.Fprint(w, `{"id":22,"title":"Feature","state":"OPEN","open":true,"fromRef":{"displayId":"a"},"toRef":{"displayId":"b"}}`)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")

	cfg, err := config.LoadFromEnv()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	fetched, err := NewService(httpclient.NewFromConfig(cfg)).Get(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, "22")
	if err != nil {
		t.Fatalf("get pull request: %v", err)
	}

	if fetched.CommentCount != nil || fetched.OpenTaskCount != nil || fetched.ResolvedTaskCount != nil {
		t.Fatalf("expected counters to stay nil when the server omits them, got %#v", fetched)
	}
}

func TestBuildReviewSummaryFromThreads(t *testing.T) {
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
