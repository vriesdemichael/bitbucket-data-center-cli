package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newDraftAutoMergeServer returns a mock Bitbucket server covering the endpoints
// exercised by the draft and auto-merge CLI commands: the permission-filtered
// repository listing used by dry-run prechecks, pull request reads/writes, and
// the per-PR auto-merge resource.
//
// PR 30 has auto-merge disabled (GET auto-merge → 404) and draft=false.
// PR 31 has auto-merge enabled with strategy no-ff and draft=true.
func newDraftAutoMergeServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		// Permission precheck (dry-run): repository list filtered by permission.
		case r.Method == http.MethodGet && path == "/rest/api/latest/repos":
			_, _ = w.Write([]byte(`{"values":[{"slug":"demo","name":"demo","project":{"key":"TEST"}}],"isLastPage":true}`))

		// Open PR list used by pr create dry-run conflict detection.
		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[]}`))

		// Create PR (execution path).
		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests":
			body := readRequestBody(t, r)
			if !strings.Contains(body, `"draft":true`) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"errors":[{"message":"expected draft flag"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":40,"title":"Draft feature","state":"OPEN","open":true,"closed":false,"draft":true,"fromRef":{"displayId":"feature/x"},"toRef":{"displayId":"master"}}`))

		// PR 30 detail (draft=false, open).
		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/30":
			_, _ = w.Write([]byte(`{"id":30,"title":"Same","description":"Same desc","state":"OPEN","open":true,"closed":false,"draft":false,"version":1,"fromRef":{"displayId":"feature/x"},"toRef":{"displayId":"master"}}`))
		// PR 31 detail (draft=true, open).
		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/31":
			_, _ = w.Write([]byte(`{"id":31,"title":"Draft","description":"Draft desc","state":"OPEN","open":true,"closed":false,"draft":true,"version":2,"fromRef":{"displayId":"feature/y"},"toRef":{"displayId":"master"}}`))

		// Mergeability lookups are optional; 404 is tolerated by the service layer.
		case r.Method == http.MethodGet && strings.HasSuffix(path, "/merge"):
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":[{"message":"no mergeability"}]}`))

		// Update PR (execution path).
		case r.Method == http.MethodPut && path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/30":
			body := readRequestBody(t, r)
			draftFlag := "false"
			if strings.Contains(body, `"draft":true`) {
				draftFlag = "true"
			}
			_, _ = w.Write([]byte(`{"id":30,"title":"Same","state":"OPEN","open":true,"closed":false,"draft":` + draftFlag + `,"version":2,"fromRef":{"displayId":"feature/x"},"toRef":{"displayId":"master"}}`))

		// Auto-merge for PR 30: not configured.
		case path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/30/auto-merge":
			switch r.Method {
			case http.MethodGet:
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"errors":[{"message":"auto-merge not configured"}]}`))
			case http.MethodPost:
				body := readRequestBody(t, r)
				strategy := "no-ff"
				if strings.Contains(body, "rebase-ff-only") {
					strategy = "rebase-ff-only"
				}
				_, _ = w.Write([]byte(`{"strategyId":"` + strategy + `"}`))
			default:
				http.NotFound(w, r)
			}

		// Auto-merge for PR 31: configured with no-ff.
		case path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/31/auto-merge":
			switch r.Method {
			case http.MethodGet:
				_, _ = w.Write([]byte(`{"strategyId":"no-ff"}`))
			case http.MethodDelete:
				w.WriteHeader(http.StatusNoContent)
			default:
				http.NotFound(w, r)
			}

		default:
			http.NotFound(w, r)
		}
	}))
}

func readRequestBody(t *testing.T, r *http.Request) string {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("failed to read request body: %v", err)
	}
	return string(body)
}

func TestPRCreateDraftFlag(t *testing.T) {
	server := newDraftAutoMergeServer(t)
	defer server.Close()
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	// Dry-run: target metadata records the draft intent and predicts create.
	out, err := executeTestCLI(t, "--json", "--dry-run", "pr", "create", "--from-ref", "feature/x", "--to-ref", "master", "--title", "Draft feature", "--draft")
	if err != nil {
		t.Fatalf("unexpected error: %v output=%s", err, out)
	}
	if !strings.Contains(out, `"predicted_action": "create"`) {
		t.Fatalf("expected create prediction, output=%s", out)
	}
	if !strings.Contains(out, `"draft": true`) {
		t.Fatalf("expected draft:true in dry-run target, output=%s", out)
	}

	// Execution: the POST payload must carry draft:true (asserted by the server).
	out, err = executeTestCLI(t, "pr", "create", "--from-ref", "feature/x", "--to-ref", "master", "--title", "Draft feature", "--draft")
	if err != nil {
		t.Fatalf("unexpected error creating draft PR: %v output=%s", err, out)
	}
	if !strings.Contains(out, "Created pull request #40") {
		t.Fatalf("expected created PR message, output=%s", out)
	}
}

func TestPRUpdateDraftToggle(t *testing.T) {
	server := newDraftAutoMergeServer(t)
	defer server.Close()
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	// Dry-run: converting a non-draft PR to draft predicts an update.
	out, err := executeTestCLI(t, "--json", "--dry-run", "pr", "update", "30", "--version", "1", "--draft")
	if err != nil {
		t.Fatalf("unexpected error: %v output=%s", err, out)
	}
	if !strings.Contains(out, `"predicted_action": "update"`) {
		t.Fatalf("expected update prediction when toggling draft on, output=%s", out)
	}

	// Dry-run: requesting the existing state with matching metadata is a no-op.
	out, err = executeTestCLI(t, "--json", "--dry-run", "pr", "update", "30", "--version", "1", "--title", "Same", "--description", "Same desc", "--draft=false")
	if err != nil {
		t.Fatalf("unexpected error: %v output=%s", err, out)
	}
	if !strings.Contains(out, `"predicted_action": "no-op"`) {
		t.Fatalf("expected no-op prediction when draft already matches, output=%s", out)
	}

	// Execution: mark the PR ready for review (draft=false).
	out, err = executeTestCLI(t, "pr", "update", "30", "--version", "1", "--draft=false")
	if err != nil {
		t.Fatalf("unexpected error updating draft state: %v output=%s", err, out)
	}
	if !strings.Contains(out, "Updated pull request #30") {
		t.Fatalf("expected updated PR message, output=%s", out)
	}
}

func TestPRAutoMergeGet(t *testing.T) {
	server := newDraftAutoMergeServer(t)
	defer server.Close()
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	// PR 30: auto-merge disabled.
	out, err := executeTestCLI(t, "pr", "auto-merge", "get", "30")
	if err != nil {
		t.Fatalf("unexpected error: %v output=%s", err, out)
	}
	if !strings.Contains(out, "Auto-merge: disabled") {
		t.Fatalf("expected disabled output, got=%s", out)
	}

	// PR 31: auto-merge enabled with no-ff.
	out, err = executeTestCLI(t, "pr", "auto-merge", "get", "31")
	if err != nil {
		t.Fatalf("unexpected error: %v output=%s", err, out)
	}
	if !strings.Contains(out, "Auto-merge: enabled") || !strings.Contains(out, "no-ff") {
		t.Fatalf("expected enabled no-ff output, got=%s", out)
	}

	// JSON output path.
	out, err = executeTestCLI(t, "--json", "pr", "auto-merge", "get", "31")
	if err != nil {
		t.Fatalf("unexpected error: %v output=%s", err, out)
	}
	if !strings.Contains(out, `"enabled": true`) {
		t.Fatalf("expected enabled:true in JSON, got=%s", out)
	}
}

func TestPRAutoMergeEnable(t *testing.T) {
	server := newDraftAutoMergeServer(t)
	defer server.Close()
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	// Dry-run: enabling on a PR without auto-merge predicts an update.
	out, err := executeTestCLI(t, "--json", "--dry-run", "pr", "auto-merge", "enable", "30", "--strategy", "rebase-ff-only")
	if err != nil {
		t.Fatalf("unexpected error: %v output=%s", err, out)
	}
	if !strings.Contains(out, `"predicted_action": "update"`) {
		t.Fatalf("expected update prediction enabling auto-merge, output=%s", out)
	}

	// Dry-run: enabling with the same strategy already set is a no-op.
	out, err = executeTestCLI(t, "--json", "--dry-run", "pr", "auto-merge", "enable", "31", "--strategy", "no-ff")
	if err != nil {
		t.Fatalf("unexpected error: %v output=%s", err, out)
	}
	if !strings.Contains(out, `"predicted_action": "no-op"`) {
		t.Fatalf("expected no-op prediction for unchanged strategy, output=%s", out)
	}

	// Execution: enable auto-merge with an explicit strategy.
	out, err = executeTestCLI(t, "pr", "auto-merge", "enable", "30", "--strategy", "rebase-ff-only")
	if err != nil {
		t.Fatalf("unexpected error enabling auto-merge: %v output=%s", err, out)
	}
	if !strings.Contains(out, "Enabled auto-merge on pull request #30") || !strings.Contains(out, "rebase-ff-only") {
		t.Fatalf("expected enable confirmation, output=%s", out)
	}

	// Execution (JSON): the auto_merge object is emitted as a machine envelope.
	out, err = executeTestCLI(t, "--json", "pr", "auto-merge", "enable", "30", "--strategy", "rebase-ff-only")
	if err != nil {
		t.Fatalf("unexpected error enabling auto-merge (json): %v output=%s", err, out)
	}
	if !strings.Contains(out, `"enabled": true`) {
		t.Fatalf("expected enabled:true in JSON enable output, got=%s", out)
	}
}

func TestPRAutoMergeDisable(t *testing.T) {
	server := newDraftAutoMergeServer(t)
	defer server.Close()
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	// Dry-run: disabling a configured PR predicts a delete.
	out, err := executeTestCLI(t, "--json", "--dry-run", "pr", "auto-merge", "disable", "31")
	if err != nil {
		t.Fatalf("unexpected error: %v output=%s", err, out)
	}
	if !strings.Contains(out, `"predicted_action": "delete"`) {
		t.Fatalf("expected delete prediction disabling auto-merge, output=%s", out)
	}

	// Dry-run: disabling a PR without auto-merge is a no-op.
	out, err = executeTestCLI(t, "--json", "--dry-run", "pr", "auto-merge", "disable", "30")
	if err != nil {
		t.Fatalf("unexpected error: %v output=%s", err, out)
	}
	if !strings.Contains(out, `"predicted_action": "no-op"`) {
		t.Fatalf("expected no-op prediction for unconfigured auto-merge, output=%s", out)
	}

	// Execution: disable auto-merge.
	out, err = executeTestCLI(t, "pr", "auto-merge", "disable", "31")
	if err != nil {
		t.Fatalf("unexpected error disabling auto-merge: %v output=%s", err, out)
	}
	if !strings.Contains(out, "Disabled auto-merge on pull request #31") {
		t.Fatalf("expected disable confirmation, output=%s", out)
	}

	// Execution (JSON): emits an ok status envelope.
	out, err = executeTestCLI(t, "--json", "pr", "auto-merge", "disable", "31")
	if err != nil {
		t.Fatalf("unexpected error disabling auto-merge (json): %v output=%s", err, out)
	}
	if !strings.Contains(out, `"status": "ok"`) {
		t.Fatalf("expected ok status in JSON disable output, got=%s", out)
	}
}

// TestPRAutoMergeErrorPaths exercises the error-return branches of the
// auto-merge commands: invalid pull request IDs (service validation errors in
// both execution and dry-run paths) and a failing permission precheck.
func TestPRAutoMergeErrorPaths(t *testing.T) {
	server := newDraftAutoMergeServer(t)
	defer server.Close()
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	// Execution paths: a non-numeric ID fails service-layer ID validation.
	for _, sub := range [][]string{
		{"pr", "auto-merge", "get", "not-a-number"},
		{"pr", "auto-merge", "enable", "not-a-number"},
		{"pr", "auto-merge", "disable", "not-a-number"},
	} {
		if _, err := executeTestCLI(t, sub...); err == nil {
			t.Fatalf("expected validation error for %v", sub)
		}
	}

	// Dry-run paths: permission precheck passes, then GetAutoMerge rejects the ID.
	for _, sub := range [][]string{
		{"--dry-run", "pr", "auto-merge", "enable", "not-a-number"},
		{"--dry-run", "pr", "auto-merge", "disable", "not-a-number"},
	} {
		if _, err := executeTestCLI(t, sub...); err == nil {
			t.Fatalf("expected dry-run validation error for %v", sub)
		}
	}
}

// TestPRAutoMergeDryRunPermissionFailure verifies the dry-run permission
// precheck error branch: when the caller lacks REPO_WRITE the command fails
// before any auto-merge prediction.
func TestPRAutoMergeDryRunPermissionFailure(t *testing.T) {
	// Server returns an empty permission-filtered repo list, so the precheck
	// concludes the caller lacks the required permission.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/repos" {
			_, _ = w.Write([]byte(`{"values":[],"isLastPage":true}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	if _, err := executeTestCLI(t, "--dry-run", "pr", "auto-merge", "enable", "30"); err == nil {
		t.Fatal("expected permission precheck failure for auto-merge enable dry-run")
	}
	if _, err := executeTestCLI(t, "--dry-run", "pr", "auto-merge", "disable", "30"); err == nil {
		t.Fatal("expected permission precheck failure for auto-merge disable dry-run")
	}
}
