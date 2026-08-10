package pullrequestactivity

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// activitiesFromJSON decodes an activity page body the same way the service
// does, so tests exercise the real decode path including the Raw payload that
// orphaned-anchor detection depends on.
func activitiesFromJSON(t *testing.T, body string) []Activity {
	t.Helper()

	page, err := decodeActivityPage([]byte(body))
	if err != nil {
		t.Fatalf("decode activity page: %v", err)
	}

	return page.Values
}

func findThread(threads []Thread, id int64) (Thread, bool) {
	for _, thread := range threads {
		if thread.ID == id {
			return thread, true
		}
	}

	return Thread{}, false
}

const threadFixture = `{"isLastPage":true,"values":[
  {"id":1,"action":"COMMENTED","comment":{"id":10,"text":"needs a nil check","version":2,"state":"OPEN","createdDate":100,"updatedDate":110,
    "author":{"name":"alice","displayName":"Alice A"},
    "anchor":{"line":42,"lineType":"ADDED","orphaned":true,"path":{"parent":"internal/cli","name":"root.go"}},
    "comments":[{"id":11,"text":"fixed in abc123","createdDate":120,"author":{"name":"bob","displayName":"Bob B"}}]}},
  {"id":2,"action":"COMMENTED","comment":{"id":20,"text":"rename this","version":1,"state":"RESOLVED","createdDate":200,
    "author":{"name":"carol","displayName":"Carol C"}}},
  {"id":3,"action":"COMMENTED","comment":{"id":30,"text":"add a test for the error path","version":1,"state":"OPEN","severity":"BLOCKER","createdDate":300,
    "author":{"name":"dave"}}},
  {"id":4,"action":"COMMENTED","comment":{"id":40,"text":"draft note","version":1,"state":"PENDING","createdDate":400,
    "author":{"name":"erin"}}},
  {"id":5,"action":"COMMENTED","comment":{"id":50,"text":"already done","version":3,"state":"RESOLVED","severity":"BLOCKER","createdDate":500,
    "author":{"name":"frank"}}},
  {"id":6,"action":"APPROVED"}
]}`

func TestExtractThreadsMapsActionableFields(t *testing.T) {
	activities := activitiesFromJSON(t, threadFixture)

	threads, summary := ExtractThreads(activities, ThreadOptions{
		BaseURL:       "https://bitbucket.example.com/",
		ProjectKey:    "TEST",
		Slug:          "demo",
		PullRequestID: "12",
	})

	if len(threads) != 5 {
		t.Fatalf("expected 5 threads, got %d", len(threads))
	}

	inline, ok := findThread(threads, 10)
	if !ok {
		t.Fatalf("expected thread 10 in %#v", threads)
	}
	if inline.Kind != ThreadKindComment {
		t.Fatalf("expected comment kind, got %q", inline.Kind)
	}
	if inline.Resolved {
		t.Fatalf("expected thread 10 to be unresolved")
	}
	if inline.Author != "Alice A" {
		t.Fatalf("expected display name author, got %q", inline.Author)
	}
	if inline.Version != 2 {
		t.Fatalf("expected version 2, got %d", inline.Version)
	}
	if inline.Anchor == nil {
		t.Fatalf("expected an anchor on thread 10")
	}
	if inline.Anchor.Path != "internal/cli/root.go" {
		t.Fatalf("expected joined anchor path, got %q", inline.Anchor.Path)
	}
	if inline.Anchor.Line != 42 || inline.Anchor.LineType != "ADDED" {
		t.Fatalf("expected line 42/ADDED, got %d/%q", inline.Anchor.Line, inline.Anchor.LineType)
	}
	if !inline.Anchor.Orphaned {
		t.Fatalf("expected orphaned anchor to be detected from the raw payload")
	}
	if inline.ReplyCount != 1 {
		t.Fatalf("expected 1 reply, got %d", inline.ReplyCount)
	}
	if inline.LastReply == nil || inline.LastReply.Author != "Bob B" {
		t.Fatalf("expected last reply from Bob B, got %#v", inline.LastReply)
	}
	if len(inline.Replies) != 0 {
		t.Fatalf("expected replies to be collapsed without WithReplies, got %#v", inline.Replies)
	}
	if inline.URL != "https://bitbucket.example.com/projects/TEST/repos/demo/pull-requests/12/overview?commentId=10" {
		t.Fatalf("unexpected thread url: %q", inline.URL)
	}

	task, ok := findThread(threads, 30)
	if !ok {
		t.Fatalf("expected thread 30")
	}
	if task.Kind != ThreadKindTask {
		t.Fatalf("expected blocker comment to map to a task, got %q", task.Kind)
	}

	general, ok := findThread(threads, 20)
	if !ok {
		t.Fatalf("expected thread 20")
	}
	if general.Anchor != nil {
		t.Fatalf("expected no anchor on a pull-request-level comment, got %#v", general.Anchor)
	}
	if !general.Resolved {
		t.Fatalf("expected thread 20 to be resolved")
	}

	expected := Summary{
		TotalThreads:     5,
		Unresolved:       2,
		Resolved:         2,
		Pending:          1,
		OpenTasks:        1,
		ResolvedTasks:    1,
		UnresolvedInline: 1,
	}
	if summary != expected {
		t.Fatalf("unexpected summary\n got: %#v\nwant: %#v", summary, expected)
	}
}

func TestExtractThreadsOrdersUnresolvedFirst(t *testing.T) {
	threads, _ := ExtractThreads(activitiesFromJSON(t, threadFixture), ThreadOptions{})

	seenResolved := false
	for _, thread := range threads {
		if thread.Resolved {
			seenResolved = true
			continue
		}
		if seenResolved {
			t.Fatalf("expected unresolved threads before resolved ones, got %#v", threads)
		}
	}
}

func TestExtractThreadsFilters(t *testing.T) {
	activities := activitiesFromJSON(t, threadFixture)

	openThreads, summary := ExtractThreads(activities, ThreadOptions{State: "open"})
	if len(openThreads) != 2 {
		t.Fatalf("expected 2 open threads, got %d", len(openThreads))
	}
	// The summary describes the pull request, not the filter.
	if summary.TotalThreads != 5 || summary.Resolved != 2 {
		t.Fatalf("expected summary over the unfiltered set, got %#v", summary)
	}

	resolvedThreads, _ := ExtractThreads(activities, ThreadOptions{State: "resolved"})
	if len(resolvedThreads) != 2 {
		t.Fatalf("expected 2 resolved threads, got %d", len(resolvedThreads))
	}

	pendingThreads, _ := ExtractThreads(activities, ThreadOptions{State: "pending"})
	if len(pendingThreads) != 1 || pendingThreads[0].ID != 40 {
		t.Fatalf("expected only the pending thread, got %#v", pendingThreads)
	}

	tasks, _ := ExtractThreads(activities, ThreadOptions{TasksOnly: true})
	if len(tasks) != 2 {
		t.Fatalf("expected 2 task threads, got %d", len(tasks))
	}

	openTasks, _ := ExtractThreads(activities, ThreadOptions{State: "open", TasksOnly: true})
	if len(openTasks) != 1 || openTasks[0].ID != 30 {
		t.Fatalf("expected only the open task, got %#v", openTasks)
	}
}

func TestExtractThreadsWithReplies(t *testing.T) {
	threads, _ := ExtractThreads(activitiesFromJSON(t, threadFixture), ThreadOptions{WithReplies: true})

	inline, ok := findThread(threads, 10)
	if !ok {
		t.Fatalf("expected thread 10")
	}
	if len(inline.Replies) != 1 || inline.Replies[0].Text != "fixed in abc123" {
		t.Fatalf("expected full reply text, got %#v", inline.Replies)
	}
}

// A missing state field means the server did not report one, so the thread flags
// decide. This keeps older Bitbucket responses from reading as unresolved.
func TestThreadResolutionFallsBackToThreadFlags(t *testing.T) {
	body := `{"isLastPage":true,"values":[
      {"id":1,"action":"COMMENTED","comment":{"id":10,"text":"a","threadResolved":true}},
      {"id":2,"action":"COMMENTED","comment":{"id":20,"text":"b","resolvedDate":1700}},
      {"id":3,"action":"COMMENTED","comment":{"id":30,"text":"c"}}
    ]}`

	threads, summary := ExtractThreads(activitiesFromJSON(t, body), ThreadOptions{})

	if summary.Resolved != 2 || summary.Unresolved != 1 {
		t.Fatalf("expected 2 resolved / 1 unresolved, got %#v", summary)
	}
	if unresolved, _ := findThread(threads, 30); unresolved.Resolved {
		t.Fatalf("expected thread 30 to stay unresolved")
	}
}

func TestThreadSuggestionDetection(t *testing.T) {
	body := "{\"isLastPage\":true,\"values\":[" +
		"{\"id\":1,\"action\":\"COMMENTED\",\"comment\":{\"id\":10,\"text\":\"try\\n```suggestion\\nx := 1\\n```\"}}," +
		"{\"id\":2,\"action\":\"COMMENTED\",\"comment\":{\"id\":20,\"text\":\"just prose about ```suggestion``` inline\"}}" +
		"]}"

	threads, _ := ExtractThreads(activitiesFromJSON(t, body), ThreadOptions{})

	withSuggestion, _ := findThread(threads, 10)
	if !withSuggestion.HasSuggestion {
		t.Fatalf("expected a fenced suggestion block to be detected")
	}
	withoutSuggestion, _ := findThread(threads, 20)
	if withoutSuggestion.HasSuggestion {
		t.Fatalf("expected inline prose not to count as a suggestion")
	}
}

func TestThreadURLOmittedWithoutContext(t *testing.T) {
	threads, _ := ExtractThreads(activitiesFromJSON(t, threadFixture), ThreadOptions{ProjectKey: "TEST"})

	for _, thread := range threads {
		if thread.URL != "" {
			t.Fatalf("expected no url without a base url, got %q", thread.URL)
		}
	}
}

// The whole point of the thread view is that it does not drag the pull request
// payload along with every comment.
func TestThreadJSONDropsNestedPullRequest(t *testing.T) {
	body := `{"isLastPage":true,"values":[{"id":1,"action":"COMMENTED","comment":{"id":10,"text":"x",
      "anchor":{"line":1,"path":{"name":"a.go"},"pullRequest":{"id":12,"title":"a pull request"}}}}]}`

	threads, _ := ExtractThreads(activitiesFromJSON(t, body), ThreadOptions{})

	encoded, err := json.Marshal(threads)
	if err != nil {
		t.Fatalf("marshal threads: %v", err)
	}
	if got := string(encoded); strings.Contains(got, "pullRequest") || strings.Contains(got, "a pull request") {
		t.Fatalf("expected the nested pull request to be dropped, got %s", got)
	}
}

func TestNormalizeThreadState(t *testing.T) {
	for input, want := range map[string]string{
		"":           "all",
		"all":        "all",
		"OPEN":       "open",
		"unresolved": "open",
		"Resolved":   "resolved",
		"pending":    "pending",
	} {
		got, err := NormalizeThreadState(input)
		if err != nil {
			t.Fatalf("NormalizeThreadState(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("NormalizeThreadState(%q) = %q, want %q", input, got, want)
		}
	}

	if _, err := NormalizeThreadState("nonsense"); err == nil {
		t.Fatalf("expected an error for an unknown state")
	}
}

func TestThreadsFromComments(t *testing.T) {
	activities := activitiesFromJSON(t, threadFixture)
	comments := ExtractComments(activities)

	threads, summary := ThreadsFromComments(comments, ThreadOptions{State: "open"})

	if len(threads) != 2 {
		t.Fatalf("expected 2 open threads, got %d", len(threads))
	}
	if summary.TotalThreads != 5 {
		t.Fatalf("expected the summary over all comments, got %#v", summary)
	}
	// Orphaned state only travels with the raw activity payload.
	if inline, ok := findThread(threads, 10); ok && inline.Anchor.Orphaned {
		t.Fatalf("expected orphaned to be unset when mapping bare comments")
	}
}

// Bitbucket serialises an inline comment's anchor path as a plain string on the
// activity timeline, while the generated model expects the object form used
// elsewhere. A single inline comment used to make the whole page fail to decode.
func TestExtractThreadsHandlesStringAnchorPaths(t *testing.T) {
	body := `{"isLastPage":true,"values":[
      {"id":1,"action":"COMMENTED","comment":{"id":10,"text":"needs a guard","state":"OPEN",
        "anchor":{"line":7,"lineType":"ADDED","path":"internal/cli/root.go","srcPath":"internal/cli/old.go"},
        "comments":[{"id":11,"text":"reply","anchor":{"line":8,"path":"nested/file.go"}}]}},
      {"id":2,"action":"COMMENTED","comment":{"id":20,"text":"object form still works","state":"OPEN",
        "anchor":{"line":3,"path":{"parent":"docs","name":"README.md"}}}}
    ]}`

	threads, summary := ExtractThreads(activitiesFromJSON(t, body), ThreadOptions{})

	if summary.TotalThreads != 2 {
		t.Fatalf("expected both comments to decode, got %#v", summary)
	}

	stringAnchor, ok := findThread(threads, 10)
	if !ok {
		t.Fatalf("expected thread 10")
	}
	if stringAnchor.Anchor == nil || stringAnchor.Anchor.Path != "internal/cli/root.go" {
		t.Fatalf("expected the string anchor path to be preserved, got %#v", stringAnchor.Anchor)
	}
	if stringAnchor.Anchor.Line != 7 {
		t.Fatalf("expected line 7, got %d", stringAnchor.Anchor.Line)
	}

	objectAnchor, ok := findThread(threads, 20)
	if !ok {
		t.Fatalf("expected thread 20")
	}
	if objectAnchor.Anchor == nil || objectAnchor.Anchor.Path != "docs/README.md" {
		t.Fatalf("expected the object anchor path to still work, got %#v", objectAnchor.Anchor)
	}
}

func TestPathObjectFromString(t *testing.T) {
	cases := []struct {
		input      string
		name       string
		parent     string
		extension  any
		components int
	}{
		{input: "a/b/c.go", name: "c.go", parent: "a/b", extension: "go", components: 3},
		{input: "README.md", name: "README.md", parent: "", extension: "md", components: 1},
		{input: "/leading/slash.txt", name: "slash.txt", parent: "leading", extension: "txt", components: 2},
		{input: "Makefile", name: "Makefile", parent: "", extension: nil, components: 1},
		{input: ".gitignore", name: ".gitignore", parent: "", extension: nil, components: 1},
	}

	for _, testCase := range cases {
		t.Run(testCase.input, func(t *testing.T) {
			got := pathObjectFromString(testCase.input)
			if got["name"] != testCase.name {
				t.Fatalf("name = %#v, want %q", got["name"], testCase.name)
			}
			if got["parent"] != testCase.parent {
				t.Fatalf("parent = %#v, want %q", got["parent"], testCase.parent)
			}
			if got["extension"] != testCase.extension {
				t.Fatalf("extension = %#v, want %#v", got["extension"], testCase.extension)
			}
			if components, _ := got["components"].([]string); len(components) != testCase.components {
				t.Fatalf("components = %#v, want %d entries", got["components"], testCase.components)
			}
		})
	}

	if got := pathObjectFromString("  /  "); len(got) != 0 {
		t.Fatalf("expected an empty object for a blank path, got %#v", got)
	}
}

// TrySummarize must degrade only when the timeline is genuinely unreadable.
// Swallowing every failure would turn a broken token or a failing server into a
// silent "nothing outstanding", which is the failure mode the review summary
// exists to prevent.
func TestTrySummarizeDegradesOnlyWhenTimelineUnavailable(t *testing.T) {
	const containerStatusDocument = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<status><status-code>404</status-code><message>HTTP 404 Not Found</message></status>`
	const missingPullRequest = `{"errors":[{"message":"Pull request 12 does not exist in TEST/demo.",` +
		`"exceptionName":"com.atlassian.bitbucket.pull.NoSuchPullRequestException"}]}`

	cases := []struct {
		name        string
		status      int
		body        string
		wantDegrade bool
	}{
		{name: "removed endpoint degrades", status: http.StatusNotFound, body: containerStatusDocument, wantDegrade: true},
		{name: "missing pull request degrades", status: http.StatusNotFound, body: missingPullRequest, wantDegrade: true},
		{name: "forbidden degrades", status: http.StatusForbidden, body: `{"errors":[{"message":"nope"}]}`, wantDegrade: true},
		{name: "unauthorized is reported", status: http.StatusUnauthorized, body: `{"errors":[{"message":"bad token"}]}`, wantDegrade: false},
		{name: "server error is reported", status: http.StatusInternalServerError, body: `{"errors":[{"message":"boom"}]}`, wantDegrade: false},
		{name: "bad gateway is reported", status: http.StatusBadGateway, body: "", wantDegrade: false},
		{name: "bad request is reported", status: http.StatusBadRequest, body: `{"errors":[{"message":"bad"}]}`, wantDegrade: false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			service := newActivityTestService(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(testCase.status)
				_, _ = w.Write([]byte(testCase.body))
			})

			summary, err := service.TrySummarize(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, "12", 25)

			if testCase.wantDegrade {
				if err != nil {
					t.Fatalf("expected a silent degrade, got error: %v", err)
				}
				if summary != nil {
					t.Fatalf("expected a nil summary when degrading, got %#v", summary)
				}
				return
			}

			if err == nil {
				t.Fatalf("expected status %d to be reported, got a silent degrade", testCase.status)
			}
			if summary != nil {
				t.Fatalf("expected no summary alongside an error, got %#v", summary)
			}
		})
	}
}

func TestTrySummarizeReportsCancellation(t *testing.T) {
	service := newActivityTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := service.TrySummarize(ctx, RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, "12", 25); err == nil {
		t.Fatal("expected a cancelled context to be reported rather than degraded")
	}
}

func TestTrySummarizeReturnsCountsOnSuccess(t *testing.T) {
	service := newActivityTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(threadFixture))
	})

	summary, err := service.TrySummarize(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, "12", 25)
	if err != nil {
		t.Fatalf("TrySummarize: %v", err)
	}
	if summary == nil {
		t.Fatal("expected a summary")
	}
	if summary.Unresolved != 2 || summary.OpenTasks != 1 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
}
