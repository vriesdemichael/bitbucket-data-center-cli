package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// reviewVisibilityActivities models a pull request mid-review: one unresolved
// inline comment with a reply, one resolved comment, one open task and one
// resolved task.
const reviewVisibilityActivities = `{"isLastPage":true,"values":[
  {"id":1,"action":"COMMENTED","comment":{"id":10,"text":"this should handle nil","version":2,"state":"OPEN","createdDate":100,
    "author":{"name":"alice","displayName":"Alice A"},
    "anchor":{"line":42,"lineType":"ADDED","path":{"parent":"internal/cli","name":"root.go"},
      "pullRequest":{"id":7,"title":"the entire pull request payload"}},
    "comments":[{"id":11,"text":"fixed in abc123","createdDate":120,"author":{"name":"bob"}}]}},
  {"id":2,"action":"COMMENTED","comment":{"id":20,"text":"nit: rename","version":1,"state":"RESOLVED","createdDate":200,
    "author":{"name":"carol"}}},
  {"id":3,"action":"COMMENTED","comment":{"id":30,"text":"add a regression test","version":1,"state":"OPEN","severity":"BLOCKER","createdDate":300,
    "author":{"name":"dave"}}},
  {"id":4,"action":"COMMENTED","comment":{"id":40,"text":"covered already","version":1,"state":"RESOLVED","severity":"BLOCKER","createdDate":400,
    "author":{"name":"erin"}}}
]}`

func newReviewVisibilityServer(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/repos":
			_, _ = w.Write([]byte(`{"values":[{"slug":"demo","name":"test-repo","project":{"key":"TEST"}}],"isLastPage":true}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/7/activities":
			_, _ = w.Write([]byte(reviewVisibilityActivities))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/7/comments":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":10,"text":"this should handle nil","version":2,"state":"OPEN",
              "author":{"name":"alice","displayName":"Alice A"},
              "anchor":{"line":42,"lineType":"ADDED","path":{"parent":"internal/cli","name":"root.go"}}}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/7":
			_, _ = w.Write([]byte(`{"id":7,"title":"Feature","state":"OPEN","open":true,"version":3,
              "fromRef":{"displayId":"feature/a"},"toRef":{"displayId":"main"},
              "reviewers":[{"user":{"name":"alice","displayName":"Alice A"},"role":"REVIEWER","status":"APPROVED","approved":true},
                           {"user":{"name":"carol","displayName":"Carol C"},"role":"REVIEWER","status":"NEEDS_WORK","approved":false}],
              "properties":{"commentCount":4,"openTaskCount":1,"resolvedTaskCount":1}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":7,"title":"Feature","state":"OPEN","open":true,
              "fromRef":{"displayId":"feature/a"},"toRef":{"displayId":"main"},
              "properties":{"commentCount":4,"openTaskCount":1,"resolvedTaskCount":1}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	return server
}

func decodeCLIData(t *testing.T, output string) map[string]any {
	t.Helper()

	envelope := map[string]any{}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("decode CLI output: %v\noutput: %s", err, output)
	}
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected a data object, got: %s", output)
	}

	return data
}

func TestPRGetReportsOutstandingReviewFeedback(t *testing.T) {
	server := newReviewVisibilityServer(t)
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	output, err := executeTestCLI(t, "--json", "pr", "get", "7")
	if err != nil {
		t.Fatalf("pr get failed: %v\noutput: %s", err, output)
	}

	summary, ok := decodeCLIData(t, output)["review_summary"].(map[string]any)
	if !ok {
		t.Fatalf("expected review_summary in pr get output, got: %s", output)
	}
	if summary["action_required"] != true {
		t.Fatalf("expected action_required, got: %#v", summary)
	}
	if summary["unresolved_threads"] != float64(2) {
		t.Fatalf("expected 2 unresolved threads, got: %#v", summary["unresolved_threads"])
	}
	if summary["open_tasks"] != float64(1) {
		t.Fatalf("expected 1 open task, got: %#v", summary["open_tasks"])
	}
	if summary["counts_source"] != "activities" {
		t.Fatalf("expected counts from the activity feed, got: %#v", summary["counts_source"])
	}
	needsWork, ok := summary["needs_work"].([]any)
	if !ok || len(needsWork) != 1 || needsWork[0] != "carol" {
		t.Fatalf("expected carol to have requested changes, got: %#v", summary["needs_work"])
	}

	humanOutput, err := executeTestCLI(t, "pr", "get", "7")
	if err != nil {
		t.Fatalf("pr get (human) failed: %v\noutput: %s", err, humanOutput)
	}
	if !strings.Contains(humanOutput, "Open items: 2 unresolved comments, 1 open task") {
		t.Fatalf("expected open item summary line, got: %s", humanOutput)
	}
	if !strings.Contains(humanOutput, "Needs work: carol") {
		t.Fatalf("expected needs-work line, got: %s", humanOutput)
	}
}

// --no-review-summary must skip the activity lookup and fall back to the
// counters Bitbucket ships on the pull request itself.
func TestPRGetNoReviewSummaryFallsBackToProperties(t *testing.T) {
	activityRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/rest/api/latest/repos":
			_, _ = w.Write([]byte(`{"values":[{"slug":"demo","name":"test-repo","project":{"key":"TEST"}}],"isLastPage":true}`))
		case strings.HasSuffix(r.URL.Path, "/activities"):
			activityRequests++
			_, _ = w.Write([]byte(reviewVisibilityActivities))
		case r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/7":
			_, _ = w.Write([]byte(`{"id":7,"title":"Feature","state":"OPEN","open":true,
              "fromRef":{"displayId":"a"},"toRef":{"displayId":"b"},
              "properties":{"commentCount":4,"openTaskCount":1,"resolvedTaskCount":1}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	output, err := executeTestCLI(t, "--json", "pr", "get", "7", "--no-review-summary")
	if err != nil {
		t.Fatalf("pr get failed: %v\noutput: %s", err, output)
	}
	if activityRequests != 0 {
		t.Fatalf("expected no activity request with --no-review-summary, got %d", activityRequests)
	}

	summary := decodeCLIData(t, output)["review_summary"].(map[string]any)
	if summary["counts_source"] != "properties" {
		t.Fatalf("expected the property counters to be used, got: %#v", summary["counts_source"])
	}
	if summary["open_tasks"] != float64(1) {
		t.Fatalf("expected 1 open task from properties, got: %#v", summary["open_tasks"])
	}
}

// An unreadable activity timeline is an enrichment failure, not a reason to
// fail `pr get`. The summary degrades to the pull request's own counters.
func TestPRGetDegradesWhenActivityTimelineUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/rest/api/latest/repos":
			_, _ = w.Write([]byte(`{"values":[{"slug":"demo","name":"test-repo","project":{"key":"TEST"}}],"isLastPage":true}`))
		case r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/7":
			_, _ = w.Write([]byte(`{"id":7,"title":"Feature","state":"OPEN","open":true,
              "fromRef":{"displayId":"a"},"toRef":{"displayId":"b"},
              "properties":{"commentCount":4,"openTaskCount":3,"resolvedTaskCount":1}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	output, err := executeTestCLI(t, "--json", "pr", "get", "7")
	if err != nil {
		t.Fatalf("expected pr get to succeed without the activity timeline: %v\noutput: %s", err, output)
	}

	summary := decodeCLIData(t, output)["review_summary"].(map[string]any)
	if summary["counts_source"] != "properties" {
		t.Fatalf("expected a fallback to the property counters, got: %#v", summary["counts_source"])
	}
	if summary["open_tasks"] != float64(3) || summary["action_required"] != true {
		t.Fatalf("expected the fallback counters to drive the summary, got: %#v", summary)
	}
}

// Bitbucket 10.x omits the property counters on the single pull request
// endpoint, so when the timeline is unreadable the summary falls back to the
// blocker-comment tally rather than reporting nothing.
func TestPRGetFallsBackToBlockerCommentCounts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/rest/api/latest/repos":
			_, _ = w.Write([]byte(`{"values":[{"slug":"demo","name":"test-repo","project":{"key":"TEST"}}],"isLastPage":true}`))
		case r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/7/blocker-comments" && r.URL.Query().Get("count") == "true":
			_, _ = w.Write([]byte(`{"OPEN":2,"RESOLVED":4}`))
		case r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/7":
			_, _ = w.Write([]byte(`{"id":7,"title":"Feature","state":"OPEN","open":true,"fromRef":{"displayId":"a"},"toRef":{"displayId":"b"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	output, err := executeTestCLI(t, "--json", "pr", "get", "7")
	if err != nil {
		t.Fatalf("pr get failed: %v\noutput: %s", err, output)
	}

	summary := decodeCLIData(t, output)["review_summary"].(map[string]any)
	if summary["counts_source"] != "blocker_comments" {
		t.Fatalf("expected the blocker comment tally to be used, got: %#v", summary["counts_source"])
	}
	if summary["open_tasks"] != float64(2) || summary["resolved_tasks"] != float64(4) {
		t.Fatalf("unexpected task counts: %#v", summary)
	}
	if summary["action_required"] != true {
		t.Fatalf("expected open tasks to require action: %#v", summary)
	}
	// Thread counts were never measured, so they must be absent rather than 0.
	if _, present := summary["unresolved_threads"]; present {
		t.Fatalf("expected unmeasured thread counts to be omitted, got: %#v", summary)
	}
}

// When nothing can be measured the summary must say so. Reporting zero open
// items would be worse than reporting nothing.
func TestPRGetReportsUnmeasuredCounts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/rest/api/latest/repos":
			_, _ = w.Write([]byte(`{"values":[{"slug":"demo","name":"test-repo","project":{"key":"TEST"}}],"isLastPage":true}`))
		case r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/7":
			_, _ = w.Write([]byte(`{"id":7,"title":"Feature","state":"OPEN","open":true,"fromRef":{"displayId":"a"},"toRef":{"displayId":"b"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	output, err := executeTestCLI(t, "--json", "pr", "get", "7")
	if err != nil {
		t.Fatalf("pr get failed: %v\noutput: %s", err, output)
	}

	summary := decodeCLIData(t, output)["review_summary"].(map[string]any)
	if summary["counts_source"] != "none" {
		t.Fatalf("expected counts_source none, got: %#v", summary["counts_source"])
	}
	if _, present := summary["open_tasks"]; present {
		t.Fatalf("expected unmeasured counts to be omitted, got: %#v", summary)
	}
	if summary["action_required"] != false {
		t.Fatalf("expected action_required false when nothing is known, got: %#v", summary)
	}

	humanOutput, err := executeTestCLI(t, "pr", "get", "7")
	if err != nil {
		t.Fatalf("pr get (human) failed: %v\noutput: %s", err, humanOutput)
	}
	if !strings.Contains(humanOutput, "Open items: not checked") {
		t.Fatalf("expected the human output to admit nothing was measured, got: %s", humanOutput)
	}
}

// A failing activity timeline must not be swallowed into a clean-looking
// summary. Degrading is only correct when the timeline is genuinely unreadable.
func TestPRGetReportsTimelineFailures(t *testing.T) {
	// Content types match Bitbucket Data Center 10.2.1: a route the application
	// never registered is answered by the servlet container as XML, while its
	// own errors come back as JSON.
	cases := []struct {
		name        string
		status      int
		contentType string
		body        string
		wantFailure bool
	}{
		{
			name:        "removed endpoint degrades",
			status:      http.StatusNotFound,
			contentType: "application/xml",
			body: `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
				`<status><status-code>404</status-code><message>HTTP 404 Not Found</message></status>`,
		},
		{
			name:        "forbidden degrades",
			status:      http.StatusForbidden,
			contentType: "application/json;charset=UTF-8",
			body:        `{"errors":[{"message":"no permission"}]}`,
		},
		{
			name:        "server error is reported",
			status:      http.StatusInternalServerError,
			contentType: "application/json;charset=UTF-8",
			body:        `{"errors":[{"message":"boom"}]}`,
			wantFailure: true,
		},
		{
			name:        "bad token is reported",
			status:      http.StatusUnauthorized,
			contentType: "application/json;charset=UTF-8",
			body:        `{"errors":[{"message":"bad token"}]}`,
			wantFailure: true,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/rest/api/latest/repos":
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"values":[{"slug":"demo","name":"test-repo","project":{"key":"TEST"}}],"isLastPage":true}`))
				case strings.HasSuffix(r.URL.Path, "/activities"):
					w.Header().Set("Content-Type", testCase.contentType)
					w.WriteHeader(testCase.status)
					_, _ = w.Write([]byte(testCase.body))
				case r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/7":
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"id":7,"title":"Feature","state":"OPEN","open":true,"fromRef":{"displayId":"a"},"toRef":{"displayId":"b"}}`))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			configureDryRunEnv(t, server.URL, "TEST", "demo")

			output, err := executeTestCLI(t, "--json", "pr", "get", "7")

			if testCase.wantFailure {
				if err == nil {
					t.Fatalf("expected status %d to fail the command rather than degrade, got: %s", testCase.status, output)
				}
				return
			}

			if err != nil {
				t.Fatalf("expected status %d to degrade gracefully, got error: %v", testCase.status, err)
			}
			summary := decodeCLIData(t, output)["review_summary"].(map[string]any)
			if summary["counts_source"] != "none" {
				t.Fatalf("expected the summary to report nothing measured, got: %#v", summary["counts_source"])
			}
		})
	}
}

func TestPRCommentListReturnsSlimThreads(t *testing.T) {
	server := newReviewVisibilityServer(t)
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	output, err := executeTestCLI(t, "--json", "pr", "comment", "list", "7")
	if err != nil {
		t.Fatalf("pr comment list failed: %v\noutput: %s", err, output)
	}

	// The nested pull request payload is what made the raw output unusable.
	if strings.Contains(output, "the entire pull request payload") {
		t.Fatalf("expected the nested pull request payload to be dropped, got: %s", output)
	}

	data := decodeCLIData(t, output)
	summary, ok := data["summary"].(map[string]any)
	if !ok {
		t.Fatalf("expected a summary block, got: %s", output)
	}
	if summary["unresolved"] != float64(2) || summary["open_tasks"] != float64(1) || summary["resolved"] != float64(2) {
		t.Fatalf("unexpected summary: %#v", summary)
	}

	threads, ok := data["threads"].([]any)
	if !ok || len(threads) != 4 {
		t.Fatalf("expected 4 threads, got: %s", output)
	}

	first, ok := threads[0].(map[string]any)
	if !ok {
		t.Fatalf("expected a thread object, got: %#v", threads[0])
	}
	// Unresolved threads sort first.
	if first["id"] != float64(10) || first["resolved"] != false {
		t.Fatalf("expected the unresolved thread first, got: %#v", first)
	}
	anchor, ok := first["anchor"].(map[string]any)
	if !ok || anchor["path"] != "internal/cli/root.go" || anchor["line"] != float64(42) {
		t.Fatalf("expected the file anchor to be preserved, got: %#v", first["anchor"])
	}
	if first["reply_count"] != float64(1) {
		t.Fatalf("expected the reply to be counted, got: %#v", first["reply_count"])
	}
	lastReply, ok := first["last_reply"].(map[string]any)
	if !ok || lastReply["text"] != "fixed in abc123" {
		t.Fatalf("expected the last reply, got: %#v", first["last_reply"])
	}
}

func TestPRCommentListFullRestoresRawPayload(t *testing.T) {
	server := newReviewVisibilityServer(t)
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	output, err := executeTestCLI(t, "--json", "pr", "comment", "list", "7", "--full")
	if err != nil {
		t.Fatalf("pr comment list --full failed: %v\noutput: %s", err, output)
	}

	data := decodeCLIData(t, output)
	if _, ok := data["comments"]; !ok {
		t.Fatalf("expected the raw comments array with --full, got: %s", output)
	}
	if _, ok := data["threads"]; ok {
		t.Fatalf("expected no thread view with --full, got: %s", output)
	}
	if !strings.Contains(output, "the entire pull request payload") {
		t.Fatalf("expected --full to carry the raw payload, got: %s", output)
	}
}

func TestPRCommentListHumanOutputVariants(t *testing.T) {
	server := newReviewVisibilityServer(t)
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	t.Run("full human output keeps the raw comment rendering", func(t *testing.T) {
		output, err := executeTestCLI(t, "pr", "comment", "list", "7", "--full")
		if err != nil {
			t.Fatalf("pr comment list --full failed: %v\noutput: %s", err, output)
		}
		if !strings.Contains(output, "[10 v2] this should handle nil") {
			t.Fatalf("expected the raw comment summary rendering, got: %s", output)
		}
	})

	t.Run("filter matching nothing still reports the counts", func(t *testing.T) {
		output, err := executeTestCLI(t, "pr", "comment", "list", "7", "--state", "pending")
		if err != nil {
			t.Fatalf("pr comment list failed: %v\noutput: %s", err, output)
		}
		if !strings.Contains(output, "2 unresolved") {
			t.Fatalf("expected the pull request counts even when the filter matched nothing, got: %s", output)
		}
		if !strings.Contains(output, "No comments match the current filter") {
			t.Fatalf("expected an empty-filter message, got: %s", output)
		}
	})

	t.Run("with replies renders reply bodies", func(t *testing.T) {
		output, err := executeTestCLI(t, "pr", "comment", "list", "7", "--with-replies")
		if err != nil {
			t.Fatalf("pr comment list --with-replies failed: %v\noutput: %s", err, output)
		}
		if !strings.Contains(output, "> bob: fixed in abc123") {
			t.Fatalf("expected the reply body, got: %s", output)
		}
	})
}

// A pull request with no comments at all should say so once, not print an empty
// count header followed by a second empty message.
func TestPRCommentListWithNoComments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/rest/api/latest/repos":
			_, _ = w.Write([]byte(`{"values":[{"slug":"demo","name":"test-repo","project":{"key":"TEST"}}],"isLastPage":true}`))
		default:
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[]}`))
		}
	}))
	defer server.Close()
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	output, err := executeTestCLI(t, "pr", "comment", "list", "7")
	if err != nil {
		t.Fatalf("pr comment list failed: %v\noutput: %s", err, output)
	}
	if strings.Count(output, "No comments") != 1 {
		t.Fatalf("expected a single empty message, got: %s", output)
	}

	fullOutput, err := executeTestCLI(t, "pr", "comment", "list", "7", "--full")
	if err != nil {
		t.Fatalf("pr comment list --full failed: %v\noutput: %s", err, fullOutput)
	}
	if !strings.Contains(fullOutput, "No comments found") {
		t.Fatalf("expected an empty message from --full too, got: %s", fullOutput)
	}
}

// --blocker lists tasks through the blocker-comment endpoint rather than the
// activity timeline, and must still produce the thread view.
func TestPRCommentListBlockerProducesThreads(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/rest/api/latest/repos":
			_, _ = w.Write([]byte(`{"values":[{"slug":"demo","name":"test-repo","project":{"key":"TEST"}}],"isLastPage":true}`))
		case r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/7/blocker-comments":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":30,"text":"add a regression test","version":1,"state":"OPEN","severity":"BLOCKER","author":{"name":"dave"}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	output, err := executeTestCLI(t, "--json", "pr", "comment", "list", "7", "--blocker")
	if err != nil {
		t.Fatalf("pr comment list --blocker failed: %v\noutput: %s", err, output)
	}

	data := decodeCLIData(t, output)
	if data["source"] != "blocker_comments" {
		t.Fatalf("expected the blocker comment source, got: %#v", data["source"])
	}
	threads, ok := data["threads"].([]any)
	if !ok || len(threads) != 1 {
		t.Fatalf("expected one task thread, got: %s", output)
	}
	if threads[0].(map[string]any)["kind"] != "task" {
		t.Fatalf("expected the blocker comment to map to a task, got: %#v", threads[0])
	}
}

func TestPRCommentListFilters(t *testing.T) {
	server := newReviewVisibilityServer(t)
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	cases := []struct {
		name    string
		args    []string
		wantIDs []float64
	}{
		{name: "unresolved", args: []string{"--unresolved"}, wantIDs: []float64{10, 30}},
		{name: "state resolved", args: []string{"--state", "resolved"}, wantIDs: []float64{20, 40}},
		{name: "tasks only", args: []string{"--tasks-only"}, wantIDs: []float64{30, 40}},
		{name: "open tasks", args: []string{"--tasks-only", "--unresolved"}, wantIDs: []float64{30}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			args := append([]string{"--json", "pr", "comment", "list", "7"}, testCase.args...)
			output, err := executeTestCLI(t, args...)
			if err != nil {
				t.Fatalf("pr comment list failed: %v\noutput: %s", err, output)
			}

			threads, ok := decodeCLIData(t, output)["threads"].([]any)
			if !ok || len(threads) != len(testCase.wantIDs) {
				t.Fatalf("expected %d threads, got: %s", len(testCase.wantIDs), output)
			}
			for index, want := range testCase.wantIDs {
				if got := threads[index].(map[string]any)["id"]; got != want {
					t.Fatalf("thread %d: got id %#v, want %v", index, got, want)
				}
			}
		})
	}
}

func TestPRCommentListRejectsConflictingStateFlags(t *testing.T) {
	server := newReviewVisibilityServer(t)
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	if _, err := executeTestCLI(t, "pr", "comment", "list", "7", "--unresolved", "--state", "resolved"); err == nil {
		t.Fatalf("expected --unresolved and --state resolved to conflict")
	}
	if _, err := executeTestCLI(t, "pr", "comment", "list", "7", "--state", "nonsense"); err == nil {
		t.Fatalf("expected an unknown state to be rejected")
	}
}

func TestPRCommentListHumanOutputMarksUnresolved(t *testing.T) {
	server := newReviewVisibilityServer(t)
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	output, err := executeTestCLI(t, "pr", "comment", "list", "7")
	if err != nil {
		t.Fatalf("pr comment list failed: %v\noutput: %s", err, output)
	}

	if !strings.Contains(output, "2 unresolved, 1 open task, 2 resolved") {
		t.Fatalf("expected a counts header, got: %s", output)
	}
	if !strings.Contains(output, "! [10] Alice A  internal/cli/root.go:42") {
		t.Fatalf("expected the unresolved thread to be marked and located, got: %s", output)
	}
	if !strings.Contains(output, "(task)") {
		t.Fatalf("expected the task thread to be labelled, got: %s", output)
	}
	if !strings.Contains(output, "this should handle nil") {
		t.Fatalf("expected the comment body, got: %s", output)
	}
}

func TestPRListShowsOpenItemIndicator(t *testing.T) {
	server := newReviewVisibilityServer(t)
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	output, err := executeTestCLI(t, "pr", "list")
	if err != nil {
		t.Fatalf("pr list failed: %v\noutput: %s", err, output)
	}
	if !strings.Contains(output, "[tasks:1 comments:4]") {
		t.Fatalf("expected the free property counters in the listing, got: %s", output)
	}

	statusOutput, err := executeTestCLI(t, "pr", "list", "--with-review-status")
	if err != nil {
		t.Fatalf("pr list --with-review-status failed: %v\noutput: %s", err, statusOutput)
	}
	if !strings.Contains(statusOutput, "[unresolved:2 tasks:1]") {
		t.Fatalf("expected resolved thread counts, got: %s", statusOutput)
	}

	jsonOutput, err := executeTestCLI(t, "--json", "pr", "list", "--with-review-status")
	if err != nil {
		t.Fatalf("pr list --json failed: %v\noutput: %s", err, jsonOutput)
	}
	summaries, ok := decodeCLIData(t, jsonOutput)["review_summaries"].([]any)
	if !ok || len(summaries) != 1 {
		t.Fatalf("expected one review summary, got: %s", jsonOutput)
	}
	if summaries[0].(map[string]any)["unresolved_threads"] != float64(2) {
		t.Fatalf("expected 2 unresolved threads, got: %#v", summaries[0])
	}
}
