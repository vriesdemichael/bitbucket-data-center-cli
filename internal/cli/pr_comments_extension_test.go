package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPRCommentListAndAddCommands(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/repos":
			_, _ = w.Write([]byte(`{"values":[{"slug":"demo","name":"test-repo","project":{"key":"TEST"}}],"isLastPage":true}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/7/blocker-comments":
			_, _ = w.Write([]byte(`{"values":[{"id":100,"text":"my blocker","version":1}],"isLastPage":true}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/7/blocker-comments":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":101,"text":"created blocker","version":0}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	// 1. List blocker comments
	listOutput, err := executeTestCLI(t, "pr", "comment", "list", "7", "--blocker")
	if err != nil {
		t.Fatalf("unexpected error listing comments: %v", err)
	}
	if !strings.Contains(listOutput, "my blocker") {
		t.Fatalf("expected comment list output to contain 'my blocker', got: %s", listOutput)
	}

	// 2. Add blocker comment
	addOutput, err := executeTestCLI(t, "pr", "comment", "add", "7", "--text", "created blocker", "--blocker")
	if err != nil {
		t.Fatalf("unexpected error adding blocker comment: %v", err)
	}
	if !strings.Contains(addOutput, "Created blocker comment 101") {
		t.Fatalf("expected success message, got: %s", addOutput)
	}

	// 3. Add comment dry-run
	dryRunOutput, err := executeTestCLI(t, "--dry-run", "pr", "comment", "add", "7", "--text", "dry blocker", "--blocker")
	if err != nil {
		t.Fatalf("unexpected error on dry run add: %v", err)
	}
	if !strings.Contains(dryRunOutput, "Dry-run") || !strings.Contains(dryRunOutput, "pr.comment.add") {
		t.Fatalf("expected dry-run format, got: %s", dryRunOutput)
	}
}

func TestPRCommentReactAndApplySuggestion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/repos":
			_, _ = w.Write([]byte(`{"values":[{"slug":"demo","name":"test-repo","project":{"key":"TEST"}}],"isLastPage":true}`))
		case r.Method == http.MethodPut && r.URL.Path == "/rest/comment-likes/latest/projects/TEST/repos/demo/pull-requests/7/comments/100/reactions/thumbsup":
			_, _ = w.Write([]byte(`{"emoticon":{"shortcut":"thumbsup","value":"👍"}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/comment-likes/latest/projects/TEST/repos/demo/pull-requests/7/comments/100/reactions/thumbsup":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/7/comments/100/apply-suggestion":
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	// 1. React (add)
	reactOutput, err := executeTestCLI(t, "pr", "comment", "react", "7", "100", ":thumbsup:")
	if err != nil {
		t.Fatalf("unexpected error reacting: %v", err)
	}
	if !strings.Contains(reactOutput, "Added reaction :thumbsup: to comment 100") {
		t.Fatalf("expected react output message, got: %s", reactOutput)
	}

	// 2. React (remove)
	unreactOutput, err := executeTestCLI(t, "pr", "comment", "react", "7", "100", ":thumbsup:", "--remove")
	if err != nil {
		t.Fatalf("unexpected error unreacting: %v", err)
	}
	if !strings.Contains(unreactOutput, "Removed reaction :thumbsup: from comment 100") {
		t.Fatalf("expected unreact output message, got: %s", unreactOutput)
	}

	// 3. Apply suggestion
	applyOutput, err := executeTestCLI(t, "pr", "comment", "apply-suggestion", "7", "100", "--commit-message", "apply suggest")
	if err != nil {
		t.Fatalf("unexpected error applying suggestion: %v", err)
	}
	if !strings.Contains(applyOutput, "Applied suggestion on comment 100 for pull request 7") {
		t.Fatalf("expected apply-suggestion success message, got: %s", applyOutput)
	}

	// 4. React dry-run
	dryReact, err := executeTestCLI(t, "--dry-run", "pr", "comment", "react", "7", "100", "thumbsup")
	if err != nil {
		t.Fatalf("unexpected error on dry run react: %v", err)
	}
	if !strings.Contains(dryReact, "Dry-run") || !strings.Contains(dryReact, "pr.comment.react") {
		t.Fatalf("expected dry-run for react, got: %s", dryReact)
	}

	// 5. Apply suggestion dry-run
	dryApply, err := executeTestCLI(t, "--dry-run", "pr", "comment", "apply-suggestion", "7", "100")
	if err != nil {
		t.Fatalf("unexpected error on dry run apply: %v", err)
	}
	if !strings.Contains(dryApply, "Dry-run") || !strings.Contains(dryApply, "pr.comment.apply-suggestion") {
		t.Fatalf("expected dry-run for apply-suggestion, got: %s", dryApply)
	}
}

func TestPRCommentCommandsJSONAndErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/repos":
			_, _ = w.Write([]byte(`{"values":[{"slug":"demo","name":"test-repo","project":{"key":"TEST"}}],"isLastPage":true}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/7/blocker-comments":
			_, _ = w.Write([]byte(`{"values":[{"id":100,"text":"my blocker","version":1}],"isLastPage":true}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/7/blocker-comments":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":101,"text":"created blocker","version":0}`))
		case r.Method == http.MethodPut && r.URL.Path == "/rest/comment-likes/latest/projects/TEST/repos/demo/pull-requests/7/comments/100/reactions/thumbsup":
			_, _ = w.Write([]byte(`{"emoticon":{"shortcut":"thumbsup","value":"👍"}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/comment-likes/latest/projects/TEST/repos/demo/pull-requests/7/comments/100/reactions/thumbsup":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/7/comments/100/apply-suggestion":
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	// 1. List blocker comments with --json
	out, err := executeTestCLI(t, "pr", "comment", "list", "7", "--blocker", "--json")
	if err != nil || !strings.Contains(out, `"text": "my blocker"`) {
		t.Fatalf("JSON list failed, out: %s, err: %v", out, err)
	}

	// 2. Add blocker comment with --json
	out, err = executeTestCLI(t, "pr", "comment", "add", "7", "--text", "created blocker", "--blocker", "--json")
	if err != nil || !strings.Contains(out, `"id": 101`) {
		t.Fatalf("JSON add failed, out: %s, err: %v", out, err)
	}

	// 3. React (add) with --json
	out, err = executeTestCLI(t, "pr", "comment", "react", "7", "100", ":thumbsup:", "--json")
	if err != nil || !strings.Contains(out, `"thumbsup"`) {
		t.Fatalf("JSON react failed, out: %s, err: %v", out, err)
	}

	// 4. React (remove) with --json
	out, err = executeTestCLI(t, "pr", "comment", "react", "7", "100", ":thumbsup:", "--remove", "--json")
	if err != nil || !strings.Contains(out, `"removed"`) {
		t.Fatalf("JSON unreact failed, out: %s, err: %v", out, err)
	}

	// 5. Apply suggestion with --json
	out, err = executeTestCLI(t, "pr", "comment", "apply-suggestion", "7", "100", "--json")
	if err != nil || !strings.Contains(out, `"status": "ok"`) {
		t.Fatalf("JSON apply-suggestion failed, out: %s, err: %v", out, err)
	}

	// 6. Test dry-run with json
	out, err = executeTestCLI(t, "--dry-run", "pr", "comment", "add", "7", "--text", "hello", "--json")
	if err != nil || !strings.Contains(out, `"dry_run": true`) {
		t.Fatalf("JSON dry-run failed, out: %s, err: %v", out, err)
	}

	// 7. React dry-run with remove
	out, err = executeTestCLI(t, "--dry-run", "pr", "comment", "react", "7", "100", "thumbsup", "--remove")
	if err != nil || !strings.Contains(out, "delete") {
		t.Fatalf("React remove dry-run failed, out: %s, err: %v", out, err)
	}

	// 8. Apply suggestion with flags
	out, err = executeTestCLI(t, "pr", "comment", "apply-suggestion", "7", "100", "--index", "2", "--comment-version", "5", "--pr-version", "9")
	if err != nil || !strings.Contains(out, "Applied suggestion") {
		t.Fatalf("Apply suggestion flags failed, out: %s, err: %v", out, err)
	}

	// 9. Apply suggestion dry-run with flags
	out, err = executeTestCLI(t, "--dry-run", "pr", "comment", "apply-suggestion", "7", "100", "--index", "2", "--comment-version", "5", "--pr-version", "9")
	if err != nil || !strings.Contains(out, "Dry-run") {
		t.Fatalf("Apply suggestion dry-run flags failed, out: %s, err: %v", out, err)
	}
}

// TestPRCommentResolveAndReopenCommands covers the two commands that replaced
// marking a pull request task done.
func TestPRCommentResolveAndReopenCommands(t *testing.T) {
	var sentBodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/repos":
			_, _ = w.Write([]byte(`{"values":[{"slug":"demo","name":"test-repo","project":{"key":"TEST"}}],"isLastPage":true}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/7/comments/100":
			// The version the update needs, which the command reads rather than
			// asking the caller for.
			_, _ = w.Write([]byte(`{"id":100,"version":3,"state":"OPEN","severity":"BLOCKER","text":"blocker"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/7/comments/100":
			body := make([]byte, r.ContentLength)
			_, _ = r.Body.Read(body)
			sentBodies = append(sentBodies, string(body))
			_, _ = w.Write([]byte(`{"id":100,"version":4,"state":"RESOLVED","severity":"BLOCKER"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	resolveOutput, err := executeTestCLI(t, "pr", "comment", "resolve", "7", "100")
	if err != nil {
		t.Fatalf("unexpected error resolving comment: %v", err)
	}
	if !strings.Contains(resolveOutput, "Resolved comment 100") {
		t.Fatalf("expected a resolve confirmation, got: %s", resolveOutput)
	}
	if len(sentBodies) != 1 || !strings.Contains(sentBodies[0], `"state":"RESOLVED"`) {
		t.Fatalf("expected the request to carry state RESOLVED, got: %v", sentBodies)
	}
	// The version comes from the read, not from the caller: the endpoint refuses
	// an update without one.
	if !strings.Contains(sentBodies[0], `"version":3`) {
		t.Fatalf("expected the request to carry the version read from the server, got: %v", sentBodies)
	}

	reopenOutput, err := executeTestCLI(t, "pr", "comment", "reopen", "7", "100")
	if err != nil {
		t.Fatalf("unexpected error reopening comment: %v", err)
	}
	if !strings.Contains(reopenOutput, "Reopened comment 100") {
		t.Fatalf("expected a reopen confirmation, got: %s", reopenOutput)
	}
	if len(sentBodies) != 2 || !strings.Contains(sentBodies[1], `"state":"OPEN"`) {
		t.Fatalf("expected the second request to carry state OPEN, got: %v", sentBodies)
	}

	jsonOutput, err := executeTestCLI(t, "--json", "pr", "comment", "resolve", "7", "100")
	if err != nil {
		t.Fatalf("unexpected error resolving comment as JSON: %v", err)
	}
	if !strings.Contains(jsonOutput, `"comment"`) || !strings.Contains(jsonOutput, `"pullRequestId"`) {
		t.Fatalf("expected the machine payload to carry the comment, got: %s", jsonOutput)
	}

	for _, name := range []string{"resolve", "reopen"} {
		dryRunOutput, err := executeTestCLI(t, "--dry-run", "pr", "comment", name, "7", "100")
		if err != nil {
			t.Fatalf("unexpected error on %s dry run: %v", name, err)
		}
		if !strings.Contains(dryRunOutput, "Dry-run") || !strings.Contains(dryRunOutput, "pr.comment."+name) {
			t.Fatalf("expected a %s dry-run preview, got: %s", name, dryRunOutput)
		}
	}
}

// A comment that does not exist fails on the read, before anything is written.
func TestPRCommentResolveMissingComment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/repos" {
			_, _ = w.Write([]byte(`{"values":[{"slug":"demo","name":"test-repo","project":{"key":"TEST"}}],"isLastPage":true}`))
			return
		}
		if r.Method == http.MethodPut {
			t.Error("expected no update when the comment cannot be read")
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[{"message":"Comment 999 does not exist."}]}`))
	}))
	defer server.Close()
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	if _, err := executeTestCLI(t, "pr", "comment", "resolve", "7", "999"); err == nil {
		t.Fatal("expected an error resolving a comment that does not exist")
	}
}
