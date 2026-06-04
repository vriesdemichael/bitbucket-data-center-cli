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
