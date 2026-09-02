package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
)

const mcpReviewActivities = `{"isLastPage":true,"values":[
  {"id":1,"action":"COMMENTED","comment":{"id":10,"text":"handle nil","version":2,"state":"OPEN","createdDate":100,
    "author":{"name":"alice","displayName":"Alice A"},
    "anchor":{"line":42,"lineType":"ADDED","path":{"parent":"internal/cli","name":"root.go"},
      "pullRequest":{"id":7,"title":"the entire pull request payload"}},
    "comments":[{"id":11,"text":"fixed","createdDate":120,"author":{"name":"bob"}}]}},
  {"id":2,"action":"COMMENTED","comment":{"id":20,"text":"nit","version":1,"state":"RESOLVED","createdDate":200,"author":{"name":"carol"}}},
  {"id":3,"action":"COMMENTED","comment":{"id":30,"text":"add a test","version":1,"state":"OPEN","severity":"BLOCKER","createdDate":300,"author":{"name":"dave"}}}
]}`

// newReviewClients serves the endpoints the review-visibility tools touch, so
// the handlers can be exercised end to end.
func newReviewClients(t *testing.T, routes map[string]string) Clients {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json;charset=UTF-8")
		for suffix, body := range routes {
			if strings.HasSuffix(r.URL.Path, suffix) {
				_, _ = w.Write([]byte(body))
				return
			}
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	clients, err := ClientsFromConfig(config.AppConfig{
		BitbucketURL:   server.URL,
		RequestTimeout: 5 * time.Second,
		RetryCount:     0,
		RetryBackoff:   time.Millisecond,
	})
	if err != nil {
		t.Fatalf("ClientsFromConfig failed: %v", err)
	}

	return clients
}

func decodeToolJSON(t *testing.T, text string) map[string]any {
	t.Helper()

	payload := map[string]any{}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("decode tool result: %v\nresult: %s", err, text)
	}

	return payload
}

func TestGetPullRequestIncludesReviewSummary(t *testing.T) {
	clients := newReviewClients(t, map[string]string{
		"/pull-requests/7/activities": mcpReviewActivities,
		"/pull-requests/7":            `{"id":7,"title":"Feature","state":"OPEN","open":true,"fromRef":{"displayId":"a"},"toRef":{"displayId":"b"},"reviewers":[{"user":{"name":"carol"},"role":"REVIEWER","status":"NEEDS_WORK"}]}`,
	})

	result := callTool(t, specGetPullRequest(), clients, map[string]any{"project": "TEST", "repo": "demo", "id": "7"})
	if result.IsError {
		t.Fatalf("unexpected error result: %s", resultText(result))
	}

	payload := decodeToolJSON(t, resultText(result))
	summary, ok := payload["review_summary"].(map[string]any)
	if !ok {
		t.Fatalf("expected review_summary, got: %s", resultText(result))
	}
	if summary["action_required"] != true {
		t.Fatalf("expected action_required, got: %#v", summary)
	}
	if summary["unresolved_threads"] != float64(2) || summary["open_tasks"] != float64(1) {
		t.Fatalf("unexpected counts: %#v", summary)
	}
	if _, ok := payload["pull_request"]; !ok {
		t.Fatalf("expected the pull request alongside the summary, got: %s", resultText(result))
	}
}

// With the timeline unavailable the handler must still answer, falling back to
// the blocker-comment tally rather than reporting nothing outstanding.
func TestGetPullRequestFallsBackToBlockerCommentCounts(t *testing.T) {
	clients := newReviewClients(t, map[string]string{
		"/pull-requests/7/blocker-comments": `{"OPEN":2,"RESOLVED":1}`,
		"/pull-requests/7":                  `{"id":7,"title":"Feature","state":"OPEN","open":true,"fromRef":{"displayId":"a"},"toRef":{"displayId":"b"}}`,
	})

	result := callTool(t, specGetPullRequest(), clients, map[string]any{"project": "TEST", "repo": "demo", "id": "7"})
	if result.IsError {
		t.Fatalf("unexpected error result: %s", resultText(result))
	}

	summary := decodeToolJSON(t, resultText(result))["review_summary"].(map[string]any)
	if summary["counts_source"] != "blocker_comments" {
		t.Fatalf("expected the blocker comment fallback, got: %#v", summary["counts_source"])
	}
	if summary["open_tasks"] != float64(2) {
		t.Fatalf("expected 2 open tasks, got: %#v", summary["open_tasks"])
	}
}

func TestGetPullRequestSkipReviewSummary(t *testing.T) {
	activityRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json;charset=UTF-8")
		if strings.HasSuffix(r.URL.Path, "/activities") {
			activityRequests++
		}
		_, _ = w.Write([]byte(`{"id":7,"title":"Feature","state":"OPEN","open":true,"fromRef":{"displayId":"a"},"toRef":{"displayId":"b"}}`))
	}))
	defer server.Close()

	clients, err := ClientsFromConfig(config.AppConfig{BitbucketURL: server.URL, RequestTimeout: 5 * time.Second, RetryBackoff: time.Millisecond})
	if err != nil {
		t.Fatalf("ClientsFromConfig failed: %v", err)
	}

	result := callTool(t, specGetPullRequest(), clients, map[string]any{"project": "TEST", "repo": "demo", "id": "7", "skip_review_summary": true})
	if result.IsError {
		t.Fatalf("unexpected error result: %s", resultText(result))
	}
	if activityRequests != 0 {
		t.Fatalf("expected no activity request, got %d", activityRequests)
	}

	summary := decodeToolJSON(t, resultText(result))["review_summary"].(map[string]any)
	if summary["counts_source"] != "none" {
		t.Fatalf("expected no counts, got: %#v", summary["counts_source"])
	}
}

func TestListPRCommentsReturnsThreads(t *testing.T) {
	clients := newReviewClients(t, map[string]string{"/pull-requests/7/activities": mcpReviewActivities})

	result := callTool(t, specListPRComments(), clients, map[string]any{"project": "TEST", "repo": "demo", "pr_id": "7"})
	if result.IsError {
		t.Fatalf("unexpected error result: %s", resultText(result))
	}

	text := resultText(result)
	if strings.Contains(text, "the entire pull request payload") {
		t.Fatalf("expected the nested pull request payload to be dropped, got: %s", text)
	}

	payload := decodeToolJSON(t, text)
	summary, ok := payload["summary"].(map[string]any)
	if !ok || summary["unresolved"] != float64(2) || summary["open_tasks"] != float64(1) {
		t.Fatalf("unexpected summary: %s", text)
	}
	threads, ok := payload["threads"].([]any)
	if !ok || len(threads) != 3 {
		t.Fatalf("expected 3 threads, got: %s", text)
	}
	first := threads[0].(map[string]any)
	if first["id"] != float64(10) || first["resolved"] != false {
		t.Fatalf("expected the unresolved thread first, got: %#v", first)
	}
	if first["url"] == "" || first["url"] == nil {
		t.Fatalf("expected a browser link built from the client base url, got: %#v", first["url"])
	}
}

func TestListPRCommentsFilters(t *testing.T) {
	clients := newReviewClients(t, map[string]string{"/pull-requests/7/activities": mcpReviewActivities})

	cases := []struct {
		name    string
		args    map[string]any
		wantIDs []float64
	}{
		{name: "open", args: map[string]any{"state": "open"}, wantIDs: []float64{10, 30}},
		{name: "resolved", args: map[string]any{"state": "resolved"}, wantIDs: []float64{20}},
		{name: "tasks only", args: map[string]any{"tasks_only": true}, wantIDs: []float64{30}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			args := map[string]any{"project": "TEST", "repo": "demo", "pr_id": "7"}
			for key, value := range testCase.args {
				args[key] = value
			}

			result := callTool(t, specListPRComments(), clients, args)
			if result.IsError {
				t.Fatalf("unexpected error result: %s", resultText(result))
			}

			threads := decodeToolJSON(t, resultText(result))["threads"].([]any)
			if len(threads) != len(testCase.wantIDs) {
				t.Fatalf("expected %d threads, got: %s", len(testCase.wantIDs), resultText(result))
			}
			for index, want := range testCase.wantIDs {
				if got := threads[index].(map[string]any)["id"]; got != want {
					t.Fatalf("thread %d: got %#v, want %v", index, got, want)
				}
			}
		})
	}
}

func TestListPRCommentsRejectsUnknownState(t *testing.T) {
	clients := newReviewClients(t, map[string]string{"/pull-requests/7/activities": mcpReviewActivities})

	result := callTool(t, specListPRComments(), clients, map[string]any{"project": "TEST", "repo": "demo", "pr_id": "7", "state": "nonsense"})
	if !result.IsError {
		t.Fatalf("expected an error result for an unknown state, got: %s", resultText(result))
	}
}

func TestListPRCommentsWithPathUsesCommentEndpoint(t *testing.T) {
	clients := newReviewClients(t, map[string]string{
		"/pull-requests/7/comments": `{"isLastPage":true,"values":[{"id":50,"text":"path comment","version":1,"state":"OPEN","author":{"name":"alice"},"anchor":{"line":3,"path":{"name":"main.go"}}}]}`,
	})

	result := callTool(t, specListPRComments(), clients, map[string]any{"project": "TEST", "repo": "demo", "pr_id": "7", "path": "main.go"})
	if result.IsError {
		t.Fatalf("unexpected error result: %s", resultText(result))
	}

	threads := decodeToolJSON(t, resultText(result))["threads"].([]any)
	if len(threads) != 1 || threads[0].(map[string]any)["id"] != float64(50) {
		t.Fatalf("expected the path-scoped comment, got: %s", resultText(result))
	}
}
