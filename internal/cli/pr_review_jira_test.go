package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

func TestPRJiraCommand(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/jira/latest/projects/TEST/repos/demo/pull-requests/42/issues":
			_, _ = w.Write([]byte(`[{"key":"JIRA-123","url":"http://jira/JIRA-123"},{"key":"JIRA-456","url":"http://jira/JIRA-456"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	// 1. Text format
	out, err := executeTestCLI(t, "pr", "jira", "42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "JIRA-123") || !strings.Contains(out, "http://jira/JIRA-456") {
		t.Fatalf("unexpected output: %s", out)
	}

	// 2. JSON format
	out, err = executeTestCLI(t, "--json", "pr", "jira", "42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, `"key": "JIRA-123"`) {
		t.Fatalf("unexpected JSON output: %s", out)
	}
}

func TestPRCommentPendingCommand(t *testing.T) {
	var capturedBody openapigenerated.RestComment
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Logf("PRCommentPending mock request: %s %s", r.Method, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/repos":
			_, _ = w.Write([]byte(`{"values":[{"slug":"demo","name":"test-repo","project":{"key":"TEST"}}],"isLastPage":true}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/42/comments":
			decoder := json.NewDecoder(r.Body)
			if err := decoder.Decode(&capturedBody); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusCreated)
			p := true
			id := int64(99)
			created := openapigenerated.RestComment{
				Id:      &id,
				Pending: &p,
			}
			bz, _ := json.Marshal(created)
			_, _ = w.Write(bz)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	// 1. Text format
	out, err := executeTestCLI(t, "pr", "comment", "add", "42", "--text", "hello", "--pending")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Created pending comment 99") {
		t.Fatalf("unexpected output: %s", out)
	}
	// The server treats pending as readOnly and reads the state instead, so this
	// asserts the field that actually makes the comment a draft. Asserting the
	// other one is what let a no-op flag ship.
	if capturedBody.State == nil || *capturedBody.State != "PENDING" {
		t.Fatalf("expected state PENDING in request payload, got %v", capturedBody.State)
	}

	// 2. Dry-run
	out, err = executeTestCLI(t, "--dry-run", "--json", "pr", "comment", "add", "42", "--text", "hello", "--pending")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "dryRun") || !strings.Contains(out, `"pending": true`) {
		t.Fatalf("unexpected dry-run output: %s", out)
	}
}

func TestPRReviewCommands(t *testing.T) {
	var capturedFinishBody openapigenerated.RestPullRequestFinishReviewRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Logf("PRReview mock request: %s %s", r.Method, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/repos":
			_, _ = w.Write([]byte(`{"values":[{"slug":"demo","name":"test-repo","project":{"key":"TEST"}}],"isLastPage":true}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/42/review":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"values":[{"id":10,"text":"draft1","pending":true}],"isLastPage":true}`))
		case r.Method == http.MethodPut && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/42/review":
			decoder := json.NewDecoder(r.Body)
			if err := decoder.Decode(&capturedFinishBody); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/42/review":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	// 1. review get
	out, err := executeTestCLI(t, "pr", "review", "get", "42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// v0, not v? -- the line is rendered from the published model now, and the
	// model states a comment Bitbucket sent no version for is at version 0.
	// The two renderings said different things about the same comment while the
	// human one read the raw object.
	if !strings.Contains(out, "[10 v0] draft1") {
		t.Fatalf("unexpected output: %s", out)
	}

	out, err = executeTestCLI(t, "--json", "pr", "review", "get", "42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, `"comments"`) || !strings.Contains(out, "draft1") {
		t.Fatalf("unexpected json output: %s", out)
	}

	// 2. review complete
	out, err = executeTestCLI(t, "pr", "review", "complete", "42", "--status", "APPROVED", "--comment", "good job")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Completed review for pull request #42") {
		t.Fatalf("unexpected output: %s", out)
	}
	if capturedFinishBody.ParticipantStatus == nil || *capturedFinishBody.ParticipantStatus != "APPROVED" {
		t.Errorf("expected APPROVED status, got: %v", capturedFinishBody.ParticipantStatus)
	}
	if capturedFinishBody.CommentText == nil || *capturedFinishBody.CommentText != "good job" {
		t.Errorf("expected comment, got: %v", capturedFinishBody.CommentText)
	}

	// 2a. review complete JSON
	out, err = executeTestCLI(t, "--json", "pr", "review", "complete", "42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, `"review": "completed"`) || !strings.Contains(out, `"status": "ok"`) {
		t.Fatalf("unexpected JSON: %s", out)
	}

	// 2b. review complete dry-run
	out, err = executeTestCLI(t, "--dry-run", "pr", "review", "complete", "42", "--status", "NEEDS_WORK")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Dry-run") || !strings.Contains(out, "pr.review.complete") {
		t.Fatalf("unexpected dry-run output: %s", out)
	}

	// 3. review discard
	out, err = executeTestCLI(t, "pr", "review", "discard", "42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Discarded review for pull request #42") {
		t.Fatalf("unexpected output: %s", out)
	}

	// 3a. review discard JSON
	out, err = executeTestCLI(t, "--json", "pr", "review", "discard", "42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, `"review": "discarded"`) {
		t.Fatalf("unexpected JSON: %s", out)
	}

	out, err = executeTestCLI(t, "--dry-run", "pr", "review", "discard", "42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Dry-run") || !strings.Contains(out, "pr.review.discard") {
		t.Fatalf("unexpected dry-run output: %s", out)
	}
}

func TestPRReviewJiraEdgeCases(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/repos":
			if r.URL.Query().Get("projectkey") == "NOPERR" {
				_, _ = w.Write([]byte(`{"values":[],"isLastPage":true}`))
				return
			}
			_, _ = w.Write([]byte(`{"values":[{"slug":"demo","name":"test-repo","project":{"key":"TEST"}}],"isLastPage":true}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/jira/latest/projects/TEST/repos/demo/pull-requests/42/issues":
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/jira/latest/projects/ERR/repos/demo/pull-requests/42/issues":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"jira error"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/42/review":
			_, _ = w.Write([]byte(`{"values":[],"isLastPage":true}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/ERR/repos/demo/pull-requests/42/review":
			w.WriteHeader(http.StatusInternalServerError)
		case r.Method == http.MethodPut && r.URL.Path == "/rest/api/latest/projects/ERR/repos/demo/pull-requests/42/review":
			w.WriteHeader(http.StatusInternalServerError)
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/api/latest/projects/ERR/repos/demo/pull-requests/42/review":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	// 1. The empty-issue-list case moved to the live suite, because it stopped
	// being answerable here. Bitbucket answers 200 [] for a pull request that
	// does not exist (OPENAPI-029), so an empty list is only meaningful once
	// the pull request is known to be there -- and a mock that serves the Jira
	// route but not the pull request is a server no Bitbucket resembles.
	// See TestLiveJiraIssuesOnARealPullRequest.
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	// 2. pr jira error state
	configureDryRunEnv(t, server.URL, "ERR", "demo")
	_, err := executeTestCLI(t, "pr", "jira", "42")
	if err == nil {
		t.Error("expected error but got nil")
	}

	// 3. review get empty state
	configureDryRunEnv(t, server.URL, "TEST", "demo")
	out, err := executeTestCLI(t, "pr", "review", "get", "42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "No draft comments found in review") {
		t.Errorf("expected empty message, got: %s", out)
	}

	// 4. review get error state
	configureDryRunEnv(t, server.URL, "ERR", "demo")
	_, err = executeTestCLI(t, "pr", "review", "get", "42")
	if err == nil {
		t.Error("expected error but got nil")
	}

	// 5. review complete error state
	_, err = executeTestCLI(t, "pr", "review", "complete", "42")
	if err == nil {
		t.Error("expected error but got nil")
	}

	// 6. review complete dry-run permission failure
	configureDryRunEnv(t, server.URL, "NOPERR", "demo")
	_, err = executeTestCLI(t, "--dry-run", "pr", "review", "complete", "42")
	if err == nil || !strings.Contains(err.Error(), "insufficient permission") {
		t.Errorf("expected permission error, got: %v", err)
	}

	// 7. review discard error state
	configureDryRunEnv(t, server.URL, "ERR", "demo")
	_, err = executeTestCLI(t, "pr", "review", "discard", "42")
	if err == nil {
		t.Error("expected error but got nil")
	}

	// 8. review discard dry-run permission failure
	configureDryRunEnv(t, server.URL, "NOPERR", "demo")
	_, err = executeTestCLI(t, "--dry-run", "pr", "review", "discard", "42")
	if err == nil || !strings.Contains(err.Error(), "insufficient permission") {
		t.Errorf("expected permission error, got: %v", err)
	}

	// 9. Unset repository slug/key so repository resolution fails
	t.Setenv("BITBUCKET_PROJECT_KEY", "")
	t.Setenv("BITBUCKET_REPO_SLUG", "")
	_, err = executeTestCLI(t, "pr", "jira", "42")
	if err == nil {
		t.Error("expected error on empty repo info in jira cmd, got nil")
	}
	_, err = executeTestCLI(t, "pr", "review", "get", "42")
	if err == nil {
		t.Error("expected error on empty repo info in review get cmd, got nil")
	}
	_, err = executeTestCLI(t, "pr", "review", "complete", "42")
	if err == nil {
		t.Error("expected error on empty repo info in review complete cmd, got nil")
	}
	_, err = executeTestCLI(t, "pr", "review", "discard", "42")
	if err == nil {
		t.Error("expected error on empty repo info in review discard cmd, got nil")
	}

	// 10. Unset URL to trigger loadConfig / loadConfigAndClient error
	t.Setenv("BITBUCKET_URL", "")
	_, err = executeTestCLI(t, "pr", "jira", "42")
	if err == nil {
		t.Error("expected error on empty URL in jira cmd, got nil")
	}
	_, err = executeTestCLI(t, "pr", "review", "get", "42")
	if err == nil {
		t.Error("expected error on empty URL in review get cmd, got nil")
	}
	_, err = executeTestCLI(t, "pr", "review", "complete", "42")
	if err == nil {
		t.Error("expected error on empty URL in review complete cmd, got nil")
	}
	_, err = executeTestCLI(t, "pr", "review", "discard", "42")
	if err == nil {
		t.Error("expected error on empty URL in review discard cmd, got nil")
	}
}
