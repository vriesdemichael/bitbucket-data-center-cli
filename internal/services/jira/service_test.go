package jira

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
)

func newJiraTestService(t *testing.T, handler http.HandlerFunc) *Service {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	cfg := config.AppConfig{
		BitbucketURL: server.URL,
	}
	client := httpclient.NewFromConfig(cfg)
	return NewService(client)
}

// TestGetIssueCommitsUnwrapsToCommit covers the one thing about this endpoint
// that cannot be reached against the live stack.
//
// The Jira integration answers with commits nested under "toCommit" rather than
// as commits, and unwrapping them is what the service adds. Reaching a payload
// with anything in it needs a Jira instance linked to Bitbucket; without one the
// endpoint answers 200 with an empty page for any issue key at all, which is
// what TestLiveJiraIssueCommitsAnswerEmpty pins.
//
// The two tests that stood beside this one are gone rather than moved. One
// asserted limit=25 reached the wire for a caller passing zero and the other
// that two values came back as one under a cap of one; both are
// openapi.PageThrough's rules now, and they are tested where the loop lives
// instead of once per service that uses it.
//
// mock-inventory: unreachable-state — a linked Jira, which this stack does not have; TestLiveJiraIssueCommitsAnswerEmpty covers what the endpoint answers without one.
func TestGetIssueCommitsUnwrapsToCommit(t *testing.T) {
	t.Parallel()

	service := newJiraTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/rest/jira/latest/issues/TEST-101/commits" {
			http.NotFound(w, r)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"size": 2,
			"isLastPage": true,
			"values": [
				{"toCommit": {"id": "commit1", "displayId": "c1", "message": "fix commit 1"}},
				{"toCommit": {"id": "commit2", "displayId": "c2", "message": "fix commit 2"}}
			]
		}`))
	})

	commits, err := service.GetIssueCommits(context.Background(), "TEST-101", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(commits) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(commits))
	}
	// The wrapper is gone and the commit is what is left, which is the whole
	// transformation: a caller that got the wrapper back would see every field
	// nil.
	if *commits[0].Id != "commit1" || *commits[0].DisplayId != "c1" || *commits[0].Message != "fix commit 1" {
		t.Errorf("unexpected commit 0: %+v", commits[0])
	}
	if *commits[1].Id != "commit2" {
		t.Errorf("unexpected commit 1: %+v", commits[1])
	}
}
