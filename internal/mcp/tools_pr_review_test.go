package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

func decodeToolJSON(t *testing.T, text string) map[string]any {
	t.Helper()

	payload := map[string]any{}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("decode tool result: %v\nresult: %s", err, text)
	}

	return payload
}

// With the timeline unavailable the handler must still answer, falling back to
// the blocker-comment tally rather than reporting nothing outstanding.
//
// mock-inventory: unreachable-state — an instance whose activity timeline is not there while the pull request is, which cannot be arranged; the subject is that the fallback counts rather than reporting nothing outstanding.
func TestGetPullRequestFallsBackToBlockerCommentCounts(t *testing.T) {
	t.Parallel()

	// The timeline is deliberately unrouted, so asking for it 404s: that is the
	// state under test, and it is the only reason a server is here.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json;charset=UTF-8")
		switch {
		case strings.HasSuffix(r.URL.Path, "/pull-requests/7/blocker-comments"):
			_, _ = w.Write([]byte(`{"OPEN":2,"RESOLVED":1}`))
		case strings.HasSuffix(r.URL.Path, "/pull-requests/7"):
			_, _ = w.Write([]byte(`{"id":7,"title":"Feature","state":"OPEN","open":true,"fromRef":{"displayId":"a"},"toRef":{"displayId":"b"}}`))
		default:
			http.NotFound(w, r)
		}
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

// The state is checked before anything is fetched, so the clients point at a
// listener that fails the test if it is reached.
func TestListPRCommentsRejectsUnknownState(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(testsupport.UnreachedHandler(t))
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

	result := callTool(t, specListPRComments(), clients, map[string]any{"project": "TEST", "repo": "demo", "pr_id": "7", "state": "nonsense"})
	if !result.IsError {
		t.Fatalf("expected an error result for an unknown state, got: %s", resultText(result))
	}
}

// TestGetPullRequestSkipReviewSummary is live now, in
// TestLiveMCPReadOnlyToolsAgreeWithCLI.
//
// It counted requests to a handler it had written. Counting is not something
// a caller can do and a server does not report it; counts_source is what the
// contract offers, and the live version asks for the pull request both ways
// and requires the field to differ.

// Four suites went live with mcpReviewActivities, the timeline they all read.
//
// It carried three comments this file had marked OPEN, RESOLVED and BLOCKER,
// and each suite then required the counts, the ordering and the filters to
// agree with those marks -- a tally of a fixture, checked against the fixture.
//
// TestLiveMCPReadOnlyToolsAgreeWithCLI now calls list_pr_comments against a
// pull request with a real comment on it, an inline one posted to a real path,
// and a thread that is resolved because `pr comment resolve` resolved it. The
// nested-payload guard went with them and is asserted there too: the activity
// timeline repeats the whole pull request inside every entry, and forwarding
// that to an agent spends its context on the same object over and over.
