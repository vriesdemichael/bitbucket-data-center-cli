package cli

import (
	"strings"
	"testing"

	pullrequestservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/pullrequest"
	pullrequestactivityservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/pullrequestactivity"
)

func countPointer(value int) *int {
	return &value
}

func TestFormatThreadCounts(t *testing.T) {
	cases := []struct {
		name    string
		summary pullrequestactivityservice.Summary
		want    string
	}{
		{name: "empty", summary: pullrequestactivityservice.Summary{}, want: "No comments"},
		{
			name:    "mixed",
			summary: pullrequestactivityservice.Summary{Unresolved: 3, OpenTasks: 1, Resolved: 5, Pending: 2},
			want:    "3 unresolved, 1 open task, 5 resolved, 2 pending",
		},
		{
			name:    "plural tasks",
			summary: pullrequestactivityservice.Summary{Unresolved: 2, OpenTasks: 2},
			want:    "2 unresolved, 2 open tasks",
		},
		{
			name:    "resolved only",
			summary: pullrequestactivityservice.Summary{Resolved: 4},
			want:    "4 resolved",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := formatThreadCounts(testCase.summary); got != testCase.want {
				t.Fatalf("formatThreadCounts() = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestFormatThread(t *testing.T) {
	cases := []struct {
		name        string
		thread      pullrequestactivityservice.Thread
		wantLines   []string
		unwantLines []string
	}{
		{
			name: "unresolved inline comment with replies",
			thread: pullrequestactivityservice.Thread{
				ID:         118,
				Kind:       pullrequestactivityservice.ThreadKindComment,
				State:      pullrequestactivityservice.ThreadStateOpen,
				Author:     "Alice A",
				Text:       "handle nil",
				ReplyCount: 2,
				Anchor:     &pullrequestactivityservice.Anchor{Path: "internal/cli/root.go", Line: 42},
			},
			wantLines: []string{"! [118] Alice A  internal/cli/root.go:42  (2 replies)", "    handle nil"},
		},
		{
			name: "resolved task is unmarked and labelled",
			thread: pullrequestactivityservice.Thread{
				ID: 131, Kind: pullrequestactivityservice.ThreadKindTask,
				State: pullrequestactivityservice.ThreadStateResolved, Resolved: true,
				Author: "Carol", Text: "add a test",
			},
			wantLines:   []string{"  [131] Carol  (task, resolved)", "    add a test"},
			unwantLines: []string{"!"},
		},
		{
			name: "pending draft",
			thread: pullrequestactivityservice.Thread{
				ID: 140, Kind: pullrequestactivityservice.ThreadKindComment,
				State: pullrequestactivityservice.ThreadStatePending, Author: "Erin", Text: "draft",
			},
			wantLines: []string{"? [140] Erin  (pending)"},
		},
		{
			name: "outdated anchor and suggestion",
			thread: pullrequestactivityservice.Thread{
				ID: 150, Kind: pullrequestactivityservice.ThreadKindComment, Author: "Dave",
				Text:          "try this",
				HasSuggestion: true,
				Anchor:        &pullrequestactivityservice.Anchor{Path: "main.go", Line: 9, Orphaned: true},
			},
			wantLines: []string{"main.go:9 (outdated)", "(suggestion)"},
		},
		{
			name: "anchor without a line",
			thread: pullrequestactivityservice.Thread{
				ID: 160, Author: "Frank", Text: "file level",
				Anchor: &pullrequestactivityservice.Anchor{Path: "docs/README.md"},
			},
			wantLines:   []string{"docs/README.md"},
			unwantLines: []string{"docs/README.md:"},
		},
		{
			name: "single reply is singular",
			thread: pullrequestactivityservice.Thread{
				ID: 170, Author: "Gail", Text: "hi", ReplyCount: 1,
			},
			wantLines: []string{"(1 reply)"},
		},
		{
			name: "multi-line body is indented throughout",
			thread: pullrequestactivityservice.Thread{
				ID: 180, Author: "Hana", Text: "first\nsecond",
			},
			wantLines: []string{"    first", "    second"},
		},
		{
			name: "replies are rendered when requested",
			thread: pullrequestactivityservice.Thread{
				ID: 190, Author: "Ivan", Text: "look",
				Replies: []pullrequestactivityservice.Reply{{Author: "Jo", Text: "done\nreally"}},
			},
			wantLines: []string{"    > Jo: done really"},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := formatThread(testCase.thread)
			for _, want := range testCase.wantLines {
				if !strings.Contains(got, want) {
					t.Fatalf("expected output to contain %q, got:\n%s", want, got)
				}
			}
			for _, unwanted := range testCase.unwantLines {
				if strings.Contains(got, unwanted) {
					t.Fatalf("expected output not to contain %q, got:\n%s", unwanted, got)
				}
			}
		})
	}
}

func TestFormatReviewSummaryLines(t *testing.T) {
	cases := []struct {
		name    string
		summary pullrequestservice.ReviewSummary
		want    []string
	}{
		{
			name:    "unmeasured says so rather than claiming none",
			summary: pullrequestservice.ReviewSummary{CountsSource: pullrequestservice.CountsSourceNone},
			want:    []string{"Open items: not checked"},
		},
		{
			name: "measured and clear",
			summary: pullrequestservice.ReviewSummary{
				CountsSource:      pullrequestservice.CountsSourceActivities,
				UnresolvedThreads: countPointer(0),
				OpenTasks:         countPointer(0),
			},
			want: []string{"Open items: none"},
		},
		{
			name: "comments and tasks",
			summary: pullrequestservice.ReviewSummary{
				CountsSource:      pullrequestservice.CountsSourceActivities,
				UnresolvedThreads: countPointer(3),
				OpenTasks:         countPointer(1),
				NeedsWork:         []string{"carol", "dave"},
			},
			want: []string{"Open items: 3 unresolved comments, 1 open task", "Needs work: carol, dave"},
		},
		{
			name: "singular comment and plural tasks",
			summary: pullrequestservice.ReviewSummary{
				CountsSource:      pullrequestservice.CountsSourceActivities,
				UnresolvedThreads: countPointer(1),
				OpenTasks:         countPointer(2),
			},
			want: []string{"Open items: 1 unresolved comment, 2 open tasks"},
		},
		{
			name: "task tally only",
			summary: pullrequestservice.ReviewSummary{
				CountsSource: pullrequestservice.CountsSourceBlockerComments,
				OpenTasks:    countPointer(2),
			},
			want: []string{"Open items: 2 open tasks"},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := strings.Join(formatReviewSummaryLines(testCase.summary), "\n")
			for _, want := range testCase.want {
				if !strings.Contains(got, want) {
					t.Fatalf("expected %q in:\n%s", want, got)
				}
			}
		})
	}
}

func TestFormatPullRequestCounts(t *testing.T) {
	cases := []struct {
		name        string
		pullRequest pullrequestservice.PullRequest
		want        string
	}{
		{name: "no counters reported", pullRequest: pullrequestservice.PullRequest{}, want: ""},
		{
			name:        "zero counters are not noise",
			pullRequest: pullrequestservice.PullRequest{OpenTaskCount: countPointer(0), CommentCount: countPointer(0)},
			want:        "",
		},
		{
			name:        "tasks only",
			pullRequest: pullrequestservice.PullRequest{OpenTaskCount: countPointer(2)},
			want:        "[tasks:2]",
		},
		{
			name:        "both",
			pullRequest: pullrequestservice.PullRequest{OpenTaskCount: countPointer(1), CommentCount: countPointer(4)},
			want:        "[tasks:1 comments:4]",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := formatPullRequestCounts(testCase.pullRequest); got != testCase.want {
				t.Fatalf("formatPullRequestCounts() = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestFormatReviewStatusIndicator(t *testing.T) {
	cases := []struct {
		name    string
		summary pullrequestservice.ReviewSummary
		want    string
	}{
		{name: "clear", summary: pullrequestservice.ReviewSummary{}, want: ""},
		{
			name:    "unresolved and tasks",
			summary: pullrequestservice.ReviewSummary{UnresolvedThreads: countPointer(3), OpenTasks: countPointer(1)},
			want:    "[unresolved:3 tasks:1]",
		},
		{
			name:    "needs work only",
			summary: pullrequestservice.ReviewSummary{NeedsWork: []string{"carol"}},
			want:    "[needs-work]",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := formatReviewStatusIndicator(testCase.summary); got != testCase.want {
				t.Fatalf("formatReviewStatusIndicator() = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestSingleLineTruncatesLongText(t *testing.T) {
	if got := singleLine("  a   b\nc  "); got != "a b c" {
		t.Fatalf("expected whitespace to collapse, got %q", got)
	}

	long := strings.Repeat("x", 200)
	got := singleLine(long)
	if len(got) != 120 || !strings.HasSuffix(got, "...") {
		t.Fatalf("expected truncation to 120 chars ending in an ellipsis, got %d chars", len(got))
	}
}

func TestPluralize(t *testing.T) {
	cases := map[string]string{
		"1 reply":   pluralize(1, "reply"),
		"2 replies": pluralize(2, "reply"),
		"0 tasks":   pluralize(0, "task"),
		"1 task":    pluralize(1, "task"),
	}

	for want, got := range cases {
		if got != want {
			t.Fatalf("pluralize produced %q, want %q", got, want)
		}
	}
}
