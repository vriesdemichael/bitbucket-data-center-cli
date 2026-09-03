package prcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

type testChecker struct{}

func (testChecker) CheckRepoPermission(ctx context.Context, projectKey, repoSlug string, permission openapigenerated.GetRepositories1ParamsPermission) error {
	return nil
}

func newMockPRServer(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":42,"title":"Test PR","state":"OPEN","open":true,"fromRef":{"displayId":"feature/x"},"toRef":{"displayId":"main"}}]}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42":
			_, _ = w.Write([]byte(`{"id":42,"title":"Test PR","description":"Desc","state":"OPEN","open":true,"version":1,"author":{"user":{"name":"authoruser","displayName":"Author User"}},"fromRef":{"displayId":"feature/x","latestCommit":"c123"},"toRef":{"displayId":"main"},"reviewers":[{"user":{"name":"alice","displayName":"Alice"},"approved":false,"status":"UNAPPROVED"}]}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/settings/reviewer-groups":
			_, _ = w.Write([]byte(`{"values":[{"id":10,"name":"core-team"},{"id":30,"name":"go-team"}]}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/settings/reviewer-groups/10/users":
			_, _ = w.Write([]byte(`[{"name":"bob"},{"name":"charlie"}]`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/settings/reviewer-groups/30/users":
			_, _ = w.Write([]byte(`[{"name":"gopher"}]`))

		case r.Method == http.MethodGet && strings.Contains(path, "/raw/.bitbucket/CODEOWNERS"):
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("*.go @go-team\n"))

		case r.Method == http.MethodGet && (strings.HasSuffix(path, "/diff") || strings.Contains(path, "/diff/")):
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("diff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go\n@@ -1 +1 @@\n"))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/settings/reviewer-groups":
			_, _ = w.Write([]byte(`{"values":[{"id":20,"name":"arch-team"}]}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/settings/reviewer-groups/20":
			_, _ = w.Write([]byte(`{"id":20,"name":"arch-team","users":[{"name":"david"}]}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/users":
			w.Header().Set("X-AUSERNAME", "currentuser")
			_, _ = w.Write([]byte(`{"values":[]}`))

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests":
			var payload struct {
				Title     string `json:"title"`
				Reviewers []struct {
					User struct {
						Name string `json:"name"`
					} `json:"user"`
				} `json:"reviewers"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			var respReviewers []map[string]any
			for _, rev := range payload.Reviewers {
				respReviewers = append(respReviewers, map[string]any{
					"user":   map[string]any{"name": rev.User.Name, "displayName": rev.User.Name},
					"role":   "REVIEWER",
					"status": "UNAPPROVED",
				})
			}
			resp := map[string]any{
				"id":        43,
				"title":     payload.Title,
				"state":     "OPEN",
				"open":      true,
				"fromRef":   map[string]any{"displayId": "feature/y"},
				"toRef":     map[string]any{"displayId": "main"},
				"reviewers": respReviewers,
			}
			_ = json.NewEncoder(w).Encode(resp)

		case r.Method == http.MethodPut && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42":
			_, _ = w.Write([]byte(`{"id":42,"title":"Updated PR","description":"New Desc","state":"OPEN","open":true,"version":2,"fromRef":{"displayId":"feature/x"},"toRef":{"displayId":"main"}}`))

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/merge":
			_, _ = w.Write([]byte(`{"id":42,"title":"Test PR","state":"MERGED","open":false,"closed":true}`))

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/decline":
			_, _ = w.Write([]byte(`{"id":42,"title":"Test PR","state":"DECLINED","open":false,"closed":true}`))

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/reopen":
			_, _ = w.Write([]byte(`{"id":42,"title":"Test PR","state":"OPEN","open":true,"closed":false}`))

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/approve":
			_, _ = w.Write([]byte(`{"id":42,"title":"Test PR","state":"OPEN","open":true,"reviewers":[{"user":{"name":"alice"},"approved":true,"status":"APPROVED"}]}`))

		case r.Method == http.MethodDelete && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/approve":
			_, _ = w.Write([]byte(`{"id":42,"title":"Test PR","state":"OPEN","open":true,"reviewers":[{"user":{"name":"alice"},"approved":false,"status":"UNAPPROVED"}]}`))

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/participants":
			// Bitbucket answers this endpoint with a RestPullRequestParticipant,
			// not with the pull request. Mirroring that here keeps the command
			// honest about where it gets the pull request it reports on.
			var payload struct {
				User struct {
					Name string `json:"name"`
				} `json:"user"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			_, _ = w.Write([]byte(`{"user":{"name":"` + payload.User.Name + `","displayName":"` + payload.User.Name + `"},"role":"REVIEWER","approved":false,"status":"UNAPPROVED"}`))

		case r.Method == http.MethodDelete && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/participants/bob":
			_, _ = w.Write([]byte(`{"id":42,"title":"Test PR","state":"OPEN","open":true}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/activities":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"action":"COMMENTED","comment":{"id":101,"version":1,"text":"Comment 1","state":"OPEN","author":{"name":"alice"}}}]}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/comments":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":101,"version":1,"text":"Comment 1","state":"OPEN","author":{"name":"alice"}}]}`))

		case r.Method == http.MethodGet && strings.Contains(path, "blocker-comments"):
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":103,"version":1,"text":"Blocker comment","severity":"BLOCKER"}]}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/comments/101":
			_, _ = w.Write([]byte(`{"id":101,"version":1,"text":"Comment 1","state":"OPEN","author":{"name":"alice"}}`))

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/comments":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":102,"version":1,"text":"New comment","state":"OPEN","author":{"name":"alice"}}`))

		case r.Method == http.MethodPut && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/comments/101":
			_, _ = w.Write([]byte(`{"id":101,"version":2,"text":"Comment 1","state":"RESOLVED","author":{"name":"alice"}}`))

		case (r.Method == http.MethodPut || r.Method == http.MethodDelete) && strings.Contains(path, "/comment-likes/latest/"):
			_, _ = w.Write([]byte(`{"emoticon":{"value":"thumbsup"}}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/commits":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":"c123","displayId":"c123","message":"Commit 1"}]}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/changes":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"path":{"toString":"file1.go"}}]}`))

		case r.Method == http.MethodGet && (strings.HasSuffix(path, "42.diff") || strings.HasSuffix(path, "/diff")):
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("diff --git a/file1.go b/file1.go\n--- a/file1.go\n+++ b/file1.go\n@@ -1 +1 @@\n-old\n+new\n"))

		case r.Method == http.MethodGet && (strings.HasSuffix(path, "42.patch") || strings.HasSuffix(path, "/patch")):
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("From c123\nSubject: Patch\n---\ndiff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go\n@@ -1 +1 @@\n"))

		case r.Method == http.MethodGet && strings.Contains(path, "diff-stats-summary"):
			// What Bitbucket sends: three totals and no per-file rows. The old
			// fixture invented a files array, so the assertion below tested
			// output the endpoint cannot produce (#526).
			_, _ = w.Write([]byte(`{"filesChanged":1,"totalInsertions":10,"totalDeletions":2}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/auto-merge":
			_, _ = w.Write([]byte(`{"enabled":true,"strategyId":"no-ff"}`))

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/auto-merge":
			_, _ = w.Write([]byte(`{"enabled":true,"strategyId":"no-ff"}`))

		case r.Method == http.MethodDelete && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/auto-merge":
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/watch":
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodDelete && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/watch":
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodGet && path == "/rest/jira/latest/projects/PRJ/repos/demo/pull-requests/42/issues":
			_, _ = w.Write([]byte(`[{"key":"PROJ-123","url":"https://jira.example.com/browse/PROJ-123"}]`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/participants":
			_, _ = w.Write([]byte(`{"values":[{"name":"alice","displayName":"Alice","emailAddress":"alice@example.com","active":true}]}`))

		case r.Method == http.MethodGet && path == "/rest/default-reviewers/latest/projects/PRJ/repos/demo/reviewers":
			_, _ = w.Write([]byte(`[{"id":1,"reviewers":[{"name":"bob","displayName":"Bob"}]}]`))

		case r.Method == http.MethodGet && strings.Contains(path, "/projects/PRJ/repos/demo/pull-requests/42/rebase"):
			_, _ = w.Write([]byte(`{"canRebase":true}`))

		case r.Method == http.MethodPost && strings.Contains(path, "/projects/PRJ/repos/demo/pull-requests/42/rebase"):
			_, _ = w.Write([]byte(`{"id":42,"title":"Test PR","state":"OPEN","open":true}`))

		case r.Method == http.MethodGet && strings.Contains(path, "/build-status/"):
			_, _ = w.Write([]byte(`{"values":[{"key":"ci/build","state":"SUCCESSFUL","url":"https://ci.example.com"}]}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/merge-base":
			_, _ = w.Write([]byte(`{"id":"base123456","displayId":"base123","message":"Base commit"}`))

		case r.Method == http.MethodPost && strings.Contains(path, "/comments/101/apply-suggestion"):
			w.WriteHeader(http.StatusOK)

		case r.Method == http.MethodGet && strings.Contains(path, "/pull-requests/42/review"):
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":101,"text":"Draft comment","author":{"name":"alice"}}]}`))

		case (r.Method == http.MethodPost || r.Method == http.MethodPut) && strings.Contains(path, "/pull-requests/42/review"):
			w.WriteHeader(http.StatusOK)

		case r.Method == http.MethodDelete && strings.Contains(path, "/pull-requests/42/review"):
			w.WriteHeader(http.StatusNoContent)

		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	return server
}

func executePr(t *testing.T, serverURL string, args ...string) (string, error) {
	t.Helper()

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", serverURL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")
	t.Setenv("BITBUCKET_TOKEN", "test-token")

	var jsonFlag bool
	var dryRunFlag bool
	filteredArgs := make([]string, 0, len(args))
	for _, a := range args {
		if a == "--json" {
			jsonFlag = true
		} else if a == "--dry-run" {
			dryRunFlag = true
		} else {
			filteredArgs = append(filteredArgs, a)
		}
	}

	deps := Dependencies{
		JSONEnabled:   func() bool { return jsonFlag },
		DryRunEnabled: func() bool { return dryRunFlag },
		LoadConfig: func() (config.AppConfig, error) {
			return config.LoadFromEnv()
		},
		LoadConfigAndClient: func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
			cfg, err := config.LoadFromEnv()
			if err != nil {
				return config.AppConfig{}, nil, err
			}
			client, err := openapi.NewClientWithResponsesFromConfig(cfg)
			return cfg, client, err
		},
		WriteJSON:     jsonoutput.Write,
		WriteJSONList: jsonoutput.WriteList,
		PermissionChecker: func(client *openapigenerated.ClientWithResponses) PermissionChecker {
			return testChecker{}
		},
	}

	root := New(deps)
	buffer := &bytes.Buffer{}
	root.SetOut(buffer)
	root.SetErr(buffer)

	root.SetArgs(filteredArgs)
	err := root.Execute()
	return buffer.String(), err
}

func TestPRList(t *testing.T) {
	server := newMockPRServer(t)
	out, err := executePr(t, server.URL, "list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "#42") || !strings.Contains(out, "Test PR") {
		t.Fatalf("expected PR #42 in list output, got:\n%s", out)
	}
}

func TestPRGet(t *testing.T) {
	server := newMockPRServer(t)
	out, err := executePr(t, server.URL, "get", "42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Test PR") || !strings.Contains(out, "feature/x") {
		t.Fatalf("expected PR details in get output, got:\n%s", out)
	}
}

func TestPRCreate(t *testing.T) {
	server := newMockPRServer(t)
	out, err := executePr(t, server.URL, "create", "--from-ref", "feature/y", "--to-ref", "main", "--title", "Created PR")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Created pull request #43") {
		t.Fatalf("expected create confirmation, got:\n%s", out)
	}
}

func TestPRUpdate(t *testing.T) {
	server := newMockPRServer(t)
	out, err := executePr(t, server.URL, "update", "42", "--version", "1", "--title", "Updated PR", "--description", "New Desc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Updated pull request #42") {
		t.Fatalf("expected update confirmation, got:\n%s", out)
	}
}

func TestPRMerge(t *testing.T) {
	server := newMockPRServer(t)
	out, err := executePr(t, server.URL, "merge", "42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Merged pull request #42") {
		t.Fatalf("expected merge confirmation, got:\n%s", out)
	}
}

func TestPRDecline(t *testing.T) {
	server := newMockPRServer(t)
	out, err := executePr(t, server.URL, "decline", "42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Declined pull request #42") {
		t.Fatalf("expected decline confirmation, got:\n%s", out)
	}
}

func TestPRReopen(t *testing.T) {
	server := newMockPRServer(t)
	out, err := executePr(t, server.URL, "reopen", "42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Reopened pull request #42") {
		t.Fatalf("expected reopen confirmation, got:\n%s", out)
	}
}

func TestPRReviewApprove(t *testing.T) {
	server := newMockPRServer(t)
	out, err := executePr(t, server.URL, "review", "approve", "42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Approved pull request #42") {
		t.Fatalf("expected approval confirmation, got:\n%s", out)
	}
}

func TestPRReviewUnapprove(t *testing.T) {
	server := newMockPRServer(t)
	out, err := executePr(t, server.URL, "review", "unapprove", "42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Removed approval for pull request #42") {
		t.Fatalf("expected unapprove confirmation, got:\n%s", out)
	}
}

func TestPRReviewerAdd(t *testing.T) {
	server := newMockPRServer(t)
	out, err := executePr(t, server.URL, "review", "reviewer", "add", "42", "--user", "bob")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Added reviewer bob to pull request #42") {
		t.Fatalf("expected reviewer add confirmation, got:\n%s", out)
	}
}

func TestPRReviewerRemove(t *testing.T) {
	server := newMockPRServer(t)
	out, err := executePr(t, server.URL, "review", "reviewer", "remove", "42", "--user", "bob")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Removed reviewer bob from pull request #42") {
		t.Fatalf("expected reviewer remove confirmation, got:\n%s", out)
	}
}

func TestPRCommentCommands(t *testing.T) {
	server := newMockPRServer(t)

	// Add comment
	out, err := executePr(t, server.URL, "comment", "add", "42", "--text", "New comment")
	if err != nil {
		t.Fatalf("unexpected error on comment add: %v", err)
	}
	if !strings.Contains(out, "Created comment") && !strings.Contains(out, "102") {
		t.Fatalf("expected Created comment in add output: %s", out)
	}

	// Add inline comment
	out, err = executePr(t, server.URL, "comment", "add", "42", "--text", "Inline review comment", "--path", "file1.go", "--line", "10")
	if err != nil {
		t.Fatalf("unexpected error on inline comment add: %v", err)
	}
	if !strings.Contains(out, "Created comment") && !strings.Contains(out, "102") {
		t.Fatalf("expected Created comment in inline add output: %s", out)
	}

	// Add inline comment with line-type
	out, err = executePr(t, server.URL, "comment", "add", "42", "--text", "Removed line comment", "--path", "file1.go", "--line", "10", "--line-type", "REMOVED")
	if err != nil {
		t.Fatalf("unexpected error on inline comment with line-type: %v", err)
	}
	if !strings.Contains(out, "Created comment") {
		t.Fatalf("expected Created comment in line-type add output: %s", out)
	}

	// Add inline comment (dry-run)
	out, err = executePr(t, server.URL, "--dry-run", "comment", "add", "42", "--text", "Inline review comment", "--path", "file1.go", "--line", "10")
	if err != nil {
		t.Fatalf("unexpected error on inline comment add dry-run: %v", err)
	}
	assertDryRunPreview(t, out)

	// Add inline comment (json)
	out, err = executePr(t, server.URL, "--json", "comment", "add", "42", "--text", "Inline review comment", "--path", "file1.go", "--line", "10")
	if err != nil {
		t.Fatalf("unexpected error on inline comment add json: %v", err)
	}
	if !strings.Contains(out, "file1.go") {
		t.Fatalf("expected file1.go in json output: %s", out)
	}

	// Add threaded reply comment
	out, err = executePr(t, server.URL, "comment", "add", "42", "--text", "Threaded reply", "--parent-id", "101")
	if err != nil {
		t.Fatalf("unexpected error on threaded reply add: %v", err)
	}
	if !strings.Contains(out, "Created comment") {
		t.Fatalf("expected Created comment in reply add output: %s", out)
	}

	// Add threaded reply (dry-run)
	out, err = executePr(t, server.URL, "--dry-run", "comment", "add", "42", "--text", "Threaded reply", "--parent-id", "101")
	if err != nil {
		t.Fatalf("unexpected error on reply dry-run: %v", err)
	}
	assertDryRunPreview(t, out)

	// Add threaded reply (json)
	out, err = executePr(t, server.URL, "--json", "comment", "add", "42", "--text", "Threaded reply", "--parent-id", "101")
	if err != nil {
		t.Fatalf("unexpected error on reply json: %v", err)
	}
	if !strings.Contains(out, "parentId") {
		t.Fatalf("expected parentId in reply json: %s", out)
	}

	// Validation errors
	// line without path
	_, err = executePr(t, server.URL, "comment", "add", "42", "--text", "bad", "--line", "10")
	if err == nil || !strings.Contains(err.Error(), "line requires path") {
		t.Fatalf("expected error for line without path, got %v", err)
	}

	// path without line
	_, err = executePr(t, server.URL, "comment", "add", "42", "--text", "bad", "--path", "file1.go")
	if err == nil || !strings.Contains(err.Error(), "path requires a positive line") {
		t.Fatalf("expected error for path without line, got %v", err)
	}

	// parent-id combined with path/line
	_, err = executePr(t, server.URL, "comment", "add", "42", "--text", "bad", "--path", "file1.go", "--line", "10", "--parent-id", "101")
	if err == nil || !strings.Contains(err.Error(), "parent-id cannot be combined with path/line") {
		t.Fatalf("expected error for parent-id with path/line, got %v", err)
	}

	// parent-id combined with blocker
	_, err = executePr(t, server.URL, "comment", "add", "42", "--text", "bad", "--parent-id", "101", "--blocker")
	if err == nil || !strings.Contains(err.Error(), "parent-id cannot be combined with blocker") {
		t.Fatalf("expected error for parent-id with blocker, got %v", err)
	}

	// line-type without inline
	_, err = executePr(t, server.URL, "comment", "add", "42", "--text", "bad", "--line-type", "ADDED")
	if err == nil || !strings.Contains(err.Error(), "line-type only applies to inline comments") {
		t.Fatalf("expected error for line-type without inline, got %v", err)
	}

	// List comments
	out, err = executePr(t, server.URL, "comment", "list", "42")
	if err != nil {
		t.Fatalf("unexpected error on comment list: %v", err)
	}
	if !strings.Contains(out, "Comment 1") {
		t.Fatalf("expected Comment 1 in list output: %s", out)
	}

	// Get comment
	out, err = executePr(t, server.URL, "comment", "get", "42", "101")
	if err != nil {
		t.Fatalf("unexpected error on comment get: %v", err)
	}
	if !strings.Contains(out, "Comment 1") {
		t.Fatalf("expected Comment 1 in get output: %s", out)
	}

	// Resolve comment
	out, err = executePr(t, server.URL, "comment", "resolve", "42", "101")
	if err != nil {
		t.Fatalf("unexpected error on resolve: %v", err)
	}
	if !strings.Contains(out, "Resolved comment 101") {
		t.Fatalf("expected resolve confirmation, got:\n%s", out)
	}

	// Reopen comment
	out, err = executePr(t, server.URL, "comment", "reopen", "42", "101")
	if err != nil {
		t.Fatalf("unexpected error on reopen: %v", err)
	}
	if !strings.Contains(out, "Reopened comment 101") {
		t.Fatalf("expected reopen confirmation, got:\n%s", out)
	}

	// React to comment
	out, err = executePr(t, server.URL, "comment", "react", "42", "101", "thumbsup")
	if err != nil {
		t.Fatalf("unexpected error on react: %v", err)
	}
	if !strings.Contains(out, "Added reaction") {
		t.Fatalf("expected reaction confirmation, got:\n%s", out)
	}
}

func TestPRCommitsAndFiles(t *testing.T) {
	server := newMockPRServer(t)

	// Commits
	out, err := executePr(t, server.URL, "commits", "42")
	if err != nil {
		t.Fatalf("unexpected error on commits: %v", err)
	}
	if !strings.Contains(out, "c123") {
		t.Fatalf("expected commit c123 in output: %s", out)
	}

	// Files
	out, err = executePr(t, server.URL, "files", "42")
	if err != nil {
		t.Fatalf("unexpected error on files: %v", err)
	}
	if !strings.Contains(out, "file1.go") {
		t.Fatalf("expected file1.go in output: %s", out)
	}

	// Diff
	out, err = executePr(t, server.URL, "diff", "42")
	if err != nil {
		t.Fatalf("unexpected error on diff: %v", err)
	}
	if !strings.Contains(out, "file1.go") {
		t.Fatalf("expected diff output, got: %s", out)
	}
}

func TestPRAutoMergeGet(t *testing.T) {
	server := newMockPRServer(t)
	out, err := executePr(t, server.URL, "auto-merge", "get", "42")
	if err != nil {
		t.Fatalf("unexpected error on auto-merge get: %v", err)
	}
	if !strings.Contains(out, "Auto-merge: enabled") {
		t.Fatalf("expected auto-merge enabled, got:\n%s", out)
	}
}

func TestPRAutoMergeEnableAndDisable(t *testing.T) {
	server := newMockPRServer(t)

	// Enable
	out, err := executePr(t, server.URL, "auto-merge", "enable", "42", "--strategy", "no-ff")
	if err != nil {
		t.Fatalf("unexpected error on enable: %v", err)
	}
	if !strings.Contains(out, "Enabled auto-merge") && !strings.Contains(out, "Merged pull request") {
		t.Fatalf("expected enable confirmation, got:\n%s", out)
	}

	// Disable
	out, err = executePr(t, server.URL, "auto-merge", "disable", "42")
	if err != nil {
		t.Fatalf("unexpected error on disable: %v", err)
	}
	if !strings.Contains(out, "Disabled auto-merge on pull request #42") {
		t.Fatalf("expected disable confirmation, got:\n%s", out)
	}
}

func TestPRWatchAndUnwatch(t *testing.T) {
	server := newMockPRServer(t)

	// Watch human mode
	out, err := executePr(t, server.URL, "watch", "42")
	if err != nil {
		t.Fatalf("unexpected error on watch: %v", err)
	}
	if !strings.Contains(out, "Watching pull request #42") {
		t.Fatalf("expected Watching confirmation, got: %s", out)
	}

	// Watch JSON mode
	out, err = executePr(t, server.URL, "--json", "watch", "42")
	if err != nil {
		t.Fatalf("unexpected error on watch json: %v", err)
	}
	if !strings.Contains(out, `"watched": true`) && !strings.Contains(out, `"watched":true`) {
		t.Fatalf("expected watched: true in json, got: %s", out)
	}

	// Watch dry-run mode
	out, err = executePr(t, server.URL, "--dry-run", "watch", "42")
	if err != nil {
		t.Fatalf("unexpected error on watch dry-run: %v", err)
	}
	assertDryRunPreview(t, out)

	// Unwatch human mode
	out, err = executePr(t, server.URL, "unwatch", "42")
	if err != nil {
		t.Fatalf("unexpected error on unwatch: %v", err)
	}
	if !strings.Contains(out, "Unwatching pull request #42") {
		t.Fatalf("expected Unwatching confirmation, got: %s", out)
	}

	// Unwatch JSON mode
	out, err = executePr(t, server.URL, "--json", "unwatch", "42")
	if err != nil {
		t.Fatalf("unexpected error on unwatch json: %v", err)
	}
	if !strings.Contains(out, `"watched": false`) && !strings.Contains(out, `"watched":false`) {
		t.Fatalf("expected watched: false in json, got: %s", out)
	}

	// Unwatch dry-run mode
	out, err = executePr(t, server.URL, "--dry-run", "unwatch", "42")
	if err != nil {
		t.Fatalf("unexpected error on unwatch dry-run: %v", err)
	}
	assertDryRunPreview(t, out)
}

func TestPRJiraIssues(t *testing.T) {
	server := newMockPRServer(t)

	// Human mode
	out, err := executePr(t, server.URL, "jira", "42")
	if err != nil {
		t.Fatalf("unexpected error on jira: %v", err)
	}
	if !strings.Contains(out, "PROJ-123") {
		t.Fatalf("expected PROJ-123 in jira output: %s", out)
	}

	// JSON mode
	out, err = executePr(t, server.URL, "--json", "jira", "42")
	if err != nil {
		t.Fatalf("unexpected error on jira json: %v", err)
	}
	if !strings.Contains(out, "PROJ-123") {
		t.Fatalf("expected PROJ-123 in jira json output: %s", out)
	}
}

func TestPRParticipants(t *testing.T) {
	server := newMockPRServer(t)

	// Human mode
	out, err := executePr(t, server.URL, "participants", "--search", "alice")
	if err != nil {
		t.Fatalf("unexpected error on participants: %v", err)
	}
	if !strings.Contains(out, "alice") {
		t.Fatalf("expected alice in participants output: %s", out)
	}

	// JSON mode
	out, err = executePr(t, server.URL, "--json", "participants", "--search", "alice")
	if err != nil {
		t.Fatalf("unexpected error on participants json: %v", err)
	}
	if !strings.Contains(out, "alice") {
		t.Fatalf("expected alice in participants json output: %s", out)
	}
}

func TestPRDefaultReviewers(t *testing.T) {
	server := newMockPRServer(t)

	// Human mode
	out, err := executePr(t, server.URL, "default-reviewers")
	if err != nil {
		t.Fatalf("unexpected error on default-reviewers: %v", err)
	}
	if !strings.Contains(out, "bob") {
		t.Fatalf("expected bob in default-reviewers output: %s", out)
	}

	// JSON mode
	out, err = executePr(t, server.URL, "--json", "default-reviewers")
	if err != nil {
		t.Fatalf("unexpected error on default-reviewers json: %v", err)
	}
	if !strings.Contains(out, "bob") {
		t.Fatalf("expected bob in default-reviewers json output: %s", out)
	}
}

func TestPRRebase(t *testing.T) {
	server := newMockPRServer(t)

	// Dry run
	out, err := executePr(t, server.URL, "--dry-run", "rebase", "42")
	if err != nil {
		t.Fatalf("unexpected error on rebase dry run: %v", err)
	}
	assertDryRunPreview(t, out)

	// Real execution
	out, err = executePr(t, server.URL, "rebase", "42")
	if err != nil {
		t.Fatalf("unexpected error on rebase: %v", err)
	}
	if !strings.Contains(out, "Rebased pull request #42") {
		t.Fatalf("expected Rebased confirmation, got: %s", out)
	}
}

func TestPRActivity(t *testing.T) {
	server := newMockPRServer(t)

	// Human mode
	out, err := executePr(t, server.URL, "activity", "list", "42")
	if err != nil {
		t.Fatalf("unexpected error on activity list: %v", err)
	}
	if !strings.Contains(out, "Comment 1") && !strings.Contains(out, "COMMENTED") {
		t.Fatalf("expected activity in output: %s", out)
	}

	// JSON mode
	out, err = executePr(t, server.URL, "--json", "activity", "list", "42")
	if err != nil {
		t.Fatalf("unexpected error on activity list json: %v", err)
	}
	if !strings.Contains(out, "activities") {
		t.Fatalf("expected activities in json: %s", out)
	}
}

func TestPRBuildStatus(t *testing.T) {
	server := newMockPRServer(t)

	// Human mode
	out, err := executePr(t, server.URL, "build", "status", "42")
	if err != nil {
		t.Fatalf("unexpected error on build status: %v", err)
	}
	if !strings.Contains(out, "SUCCESSFUL") && !strings.Contains(out, "ci/build") {
		t.Fatalf("expected build status in output: %s", out)
	}

	// JSON mode
	out, err = executePr(t, server.URL, "--json", "build", "status", "42")
	if err != nil {
		t.Fatalf("unexpected error on build status json: %v", err)
	}
	if !strings.Contains(out, "statuses") {
		t.Fatalf("expected statuses in json: %s", out)
	}
}

func TestPRMergeBase(t *testing.T) {
	server := newMockPRServer(t)

	// Human mode
	out, err := executePr(t, server.URL, "merge-base", "42")
	if err != nil {
		t.Fatalf("unexpected error on merge-base: %v", err)
	}
	if !strings.Contains(out, "base123") {
		t.Fatalf("expected base123 in merge-base output: %s", out)
	}

	// JSON mode
	out, err = executePr(t, server.URL, "--json", "merge-base", "42")
	if err != nil {
		t.Fatalf("unexpected error on merge-base json: %v", err)
	}
	if !strings.Contains(out, "mergeBase") {
		t.Fatalf("expected mergeBase in json: %s", out)
	}
}

func TestPRFiles(t *testing.T) {
	server := newMockPRServer(t)

	// Human mode
	out, err := executePr(t, server.URL, "files", "42", "--start", "0", "--limit", "10")
	if err != nil {
		t.Fatalf("unexpected error on files: %v", err)
	}
	if !strings.Contains(out, "file1.go") {
		t.Fatalf("expected file1.go in files output: %s", out)
	}

	// JSON mode
	out, err = executePr(t, server.URL, "--json", "files", "42")
	if err != nil {
		t.Fatalf("unexpected error on files json: %v", err)
	}
	if !strings.Contains(out, "file1.go") {
		t.Fatalf("expected file1.go in json: %s", out)
	}
}

func TestPRReviewCompleteAndDiscard(t *testing.T) {
	server := newMockPRServer(t)

	// Complete dry-run
	out, err := executePr(t, server.URL, "--dry-run", "review", "complete", "42", "--status", "APPROVED", "--comment", "Looks good")
	if err != nil {
		t.Fatalf("unexpected error on review complete dry-run: %v", err)
	}
	assertDryRunPreview(t, out)

	// Complete real execution (human & JSON)
	out, err = executePr(t, server.URL, "review", "complete", "42", "--status", "APPROVED", "--comment", "Looks good")
	if err != nil {
		t.Fatalf("unexpected error on review complete: %v", err)
	}
	if !strings.Contains(out, "Completed review for pull request #42") {
		t.Fatalf("expected Completed review in output: %s", out)
	}

	out, err = executePr(t, server.URL, "--json", "review", "complete", "42")
	if err != nil {
		t.Fatalf("unexpected error on review complete json: %v", err)
	}
	if !strings.Contains(out, "completed") {
		t.Fatalf("expected completed in json output: %s", out)
	}

	// Discard dry-run
	out, err = executePr(t, server.URL, "--dry-run", "review", "discard", "42")
	if err != nil {
		t.Fatalf("unexpected error on review discard dry-run: %v", err)
	}
	assertDryRunPreview(t, out)

	// Discard real execution (human & JSON)
	out, err = executePr(t, server.URL, "review", "discard", "42")
	if err != nil {
		t.Fatalf("unexpected error on review discard: %v", err)
	}
	if !strings.Contains(out, "Discarded review for pull request #42") {
		t.Fatalf("expected Discarded review in output: %s", out)
	}

	out, err = executePr(t, server.URL, "--json", "review", "discard", "42")
	if err != nil {
		t.Fatalf("unexpected error on review discard json: %v", err)
	}
	if !strings.Contains(out, "discarded") {
		t.Fatalf("expected discarded in json output: %s", out)
	}
}

func TestPRReviewGet(t *testing.T) {
	server := newMockPRServer(t)

	// Human mode
	out, err := executePr(t, server.URL, "review", "get", "42")
	if err != nil {
		t.Fatalf("unexpected error on review get: %v", err)
	}
	if !strings.Contains(out, "Draft comment") {
		t.Fatalf("expected Draft comment in review get output: %s", out)
	}

	// JSON mode
	out, err = executePr(t, server.URL, "--json", "review", "get", "42")
	if err != nil {
		t.Fatalf("unexpected error on review get json: %v", err)
	}
	if !strings.Contains(out, "Draft comment") {
		t.Fatalf("expected Draft comment in json output: %s", out)
	}
}

func TestPRCommentApplySuggestion(t *testing.T) {
	server := newMockPRServer(t)

	// Dry run
	out, err := executePr(t, server.URL, "--dry-run", "comment", "apply-suggestion", "42", "101", "--commit-message", "Apply fix", "--index", "0")
	if err != nil {
		t.Fatalf("unexpected error on apply-suggestion dry-run: %v", err)
	}
	assertDryRunPreview(t, out)

	// Real execution (human & JSON)
	out, err = executePr(t, server.URL, "comment", "apply-suggestion", "42", "101", "--commit-message", "Apply fix", "--comment-version", "1", "--pr-version", "1")
	if err != nil {
		t.Fatalf("unexpected error on apply-suggestion: %v", err)
	}
	if !strings.Contains(out, "Applied suggestion on comment 101 for pull request 42") {
		t.Fatalf("expected Applied suggestion in output: %s", out)
	}

	out, err = executePr(t, server.URL, "--json", "comment", "apply-suggestion", "42", "101")
	if err != nil {
		t.Fatalf("unexpected error on apply-suggestion json: %v", err)
	}
	if !strings.Contains(out, "applied") && !strings.Contains(out, "ok") {
		t.Fatalf("expected ok/applied in json output: %s", out)
	}
}

func TestPRCommentReact(t *testing.T) {
	server := newMockPRServer(t)

	// React add dry-run
	out, err := executePr(t, server.URL, "--dry-run", "comment", "react", "42", "101", "thumbsup")
	if err != nil {
		t.Fatalf("unexpected error on react dry-run: %v", err)
	}
	assertDryRunPreview(t, out)

	// React add (human & JSON)
	out, err = executePr(t, server.URL, "comment", "react", "42", "101", "thumbsup")
	if err != nil {
		t.Fatalf("unexpected error on react add: %v", err)
	}
	if !strings.Contains(out, "Added reaction :thumbsup: to comment 101") {
		t.Fatalf("expected Added reaction in output: %s", out)
	}

	out, err = executePr(t, server.URL, "--json", "comment", "react", "42", "101", "thumbsup")
	if err != nil {
		t.Fatalf("unexpected error on react json: %v", err)
	}
	if !strings.Contains(out, "thumbsup") {
		t.Fatalf("expected thumbsup in json output: %s", out)
	}

	// React delete (dry-run, human, JSON)
	out, err = executePr(t, server.URL, "--dry-run", "comment", "react", "42", "101", "thumbsup", "--remove")
	if err != nil {
		t.Fatalf("unexpected error on react delete dry-run: %v", err)
	}
	assertDryRunPreview(t, out)

	out, err = executePr(t, server.URL, "comment", "react", "42", "101", "thumbsup", "--remove")
	if err != nil {
		t.Fatalf("unexpected error on react delete: %v", err)
	}
	if !strings.Contains(out, "Removed reaction :thumbsup: from comment 101") {
		t.Fatalf("expected Removed reaction in output: %s", out)
	}

	out, err = executePr(t, server.URL, "--json", "comment", "react", "42", "101", "thumbsup", "--remove")
	if err != nil {
		t.Fatalf("unexpected error on react delete json: %v", err)
	}
	if !strings.Contains(out, "removed") && !strings.Contains(out, "ok") {
		t.Fatalf("expected removed/ok in json output: %s", out)
	}
}

func TestPRDefaultDependencies(t *testing.T) {
	cmd := New(Dependencies{})
	if cmd == nil {
		t.Fatal("expected New to return command with default dependencies")
	}
	checker := nopPermissionChecker{}
	if err := checker.CheckRepoPermission(context.Background(), "PRJ", "demo", openapi.RepoRead); err != nil {
		t.Fatalf("expected nop checker to return nil error: %v", err)
	}
}

func TestPRListModes(t *testing.T) {
	server := newMockPRServer(t)

	// List with review status
	out, err := executePr(t, server.URL, "list", "--with-review-status", "--state", "OPEN", "--source-branch", "feature/x", "--target-branch", "main")
	if err != nil {
		t.Fatalf("unexpected error on list with review status: %v", err)
	}
	if !strings.Contains(out, "#42") {
		t.Fatalf("expected #42 in list output: %s", out)
	}

	// List JSON mode
	out, err = executePr(t, server.URL, "--json", "list", "--limit", "10", "--start", "0")
	if err != nil {
		t.Fatalf("unexpected error on list json: %v", err)
	}
	if !strings.Contains(out, "pullRequests") {
		t.Fatalf("expected pull_requests in json: %s", out)
	}
}

func TestPRGetModes(t *testing.T) {
	server := newMockPRServer(t)

	// Get with --no-review-summary
	out, err := executePr(t, server.URL, "get", "42", "--no-review-summary")
	if err != nil {
		t.Fatalf("unexpected error on get no review summary: %v", err)
	}
	if !strings.Contains(out, "Test PR") {
		t.Fatalf("expected Test PR in get output: %s", out)
	}

	// Get JSON mode
	out, err = executePr(t, server.URL, "--json", "get", "42")
	if err != nil {
		t.Fatalf("unexpected error on get json: %v", err)
	}
	if !strings.Contains(out, "pullRequest") {
		t.Fatalf("expected pullRequest in json: %s", out)
	}
}

func TestPRCreateUpdateMergeDeclineReopenModes(t *testing.T) {
	server := newMockPRServer(t)

	// Create dry-run and JSON
	out, err := executePr(t, server.URL, "--dry-run", "create", "--from-ref", "feature/y", "--to-ref", "main", "--title", "Created PR", "--description", "Desc", "--reviewers", "alice", "--draft")
	if err != nil {
		t.Fatalf("unexpected error on create dry run: %v", err)
	}
	assertDryRunPreview(t, out)

	out, err = executePr(t, server.URL, "--json", "create", "--from-ref", "feature/y", "--to-ref", "main", "--title", "Created PR")
	if err != nil {
		t.Fatalf("unexpected error on create json: %v", err)
	}
	if !strings.Contains(out, "Created PR") {
		t.Fatalf("expected Created PR in json: %s", out)
	}

	// Update dry-run and JSON
	out, err = executePr(t, server.URL, "--dry-run", "update", "42", "--version", "1", "--title", "Updated PR", "--description", "New Desc", "--draft")
	if err != nil {
		t.Fatalf("unexpected error on update dry run: %v", err)
	}
	assertDryRunPreview(t, out)

	out, err = executePr(t, server.URL, "--json", "update", "42", "--version", "1", "--title", "Updated PR")
	if err != nil {
		t.Fatalf("unexpected error on update json: %v", err)
	}
	if !strings.Contains(out, "Updated PR") {
		t.Fatalf("expected Updated PR in json: %s", out)
	}

	// Merge dry-run and JSON
	out, err = executePr(t, server.URL, "--dry-run", "merge", "42", "--version", "1")
	if err != nil {
		t.Fatalf("unexpected error on merge dry run: %v", err)
	}
	assertDryRunPreview(t, out)

	out, err = executePr(t, server.URL, "--json", "merge", "42", "--version", "1")
	if err != nil {
		t.Fatalf("unexpected error on merge json: %v", err)
	}
	if !strings.Contains(out, "MERGED") {
		t.Fatalf("expected MERGED in json: %s", out)
	}

	// Decline dry-run and JSON
	out, err = executePr(t, server.URL, "--dry-run", "decline", "42", "--version", "1")
	if err != nil {
		t.Fatalf("unexpected error on decline dry run: %v", err)
	}
	assertDryRunPreview(t, out)

	out, err = executePr(t, server.URL, "--json", "decline", "42")
	if err != nil {
		t.Fatalf("unexpected error on decline json: %v", err)
	}
	if !strings.Contains(out, "DECLINED") {
		t.Fatalf("expected DECLINED in json: %s", out)
	}

	// Reopen dry-run and JSON
	out, err = executePr(t, server.URL, "--dry-run", "reopen", "42", "--version", "1")
	if err != nil {
		t.Fatalf("unexpected error on reopen dry run: %v", err)
	}
	assertDryRunPreview(t, out)

	out, err = executePr(t, server.URL, "--json", "reopen", "42")
	if err != nil {
		t.Fatalf("unexpected error on reopen json: %v", err)
	}
	if !strings.Contains(out, "OPEN") {
		t.Fatalf("expected OPEN in json: %s", out)
	}
}

func TestPRReviewAndReviewerModes(t *testing.T) {
	server := newMockPRServer(t)

	// Review approve (dry-run & JSON)
	out, err := executePr(t, server.URL, "--dry-run", "review", "approve", "42")
	if err != nil {
		t.Fatalf("unexpected error on approve dry run: %v", err)
	}
	assertDryRunPreview(t, out)

	out, err = executePr(t, server.URL, "--json", "review", "approve", "42")
	if err != nil {
		t.Fatalf("unexpected error on approve json: %v", err)
	}
	if !strings.Contains(out, "APPROVED") {
		t.Fatalf("expected APPROVED in json: %s", out)
	}

	// Review unapprove (dry-run & JSON)
	out, err = executePr(t, server.URL, "--dry-run", "review", "unapprove", "42")
	if err != nil {
		t.Fatalf("unexpected error on unapprove dry run: %v", err)
	}
	assertDryRunPreview(t, out)

	out, err = executePr(t, server.URL, "--json", "review", "unapprove", "42")
	if err != nil {
		t.Fatalf("unexpected error on unapprove json: %v", err)
	}
	if !strings.Contains(out, "UNAPPROVED") {
		t.Fatalf("expected UNAPPROVED in json: %s", out)
	}

	// Reviewer add (dry-run & JSON)
	out, err = executePr(t, server.URL, "--dry-run", "review", "reviewer", "add", "42", "--user", "bob")
	if err != nil {
		t.Fatalf("unexpected error on reviewer add dry run: %v", err)
	}
	assertDryRunPreview(t, out)

	out, err = executePr(t, server.URL, "--json", "review", "reviewer", "add", "42", "--user", "bob")
	if err != nil {
		t.Fatalf("unexpected error on reviewer add json: %v", err)
	}
	if !strings.Contains(out, "bob") {
		t.Fatalf("expected bob in json: %s", out)
	}

	// Reviewer remove (dry-run & JSON)
	out, err = executePr(t, server.URL, "--dry-run", "review", "reviewer", "remove", "42", "--user", "bob")
	if err != nil {
		t.Fatalf("unexpected error on reviewer remove dry run: %v", err)
	}
	assertDryRunPreview(t, out)

	out, err = executePr(t, server.URL, "--json", "review", "reviewer", "remove", "42", "--user", "bob")
	if err != nil {
		t.Fatalf("unexpected error on reviewer remove json: %v", err)
	}
	if !strings.Contains(out, "pullRequest") {
		t.Fatalf("expected pullRequest in json: %s", out)
	}
}

func TestPRCommentModes(t *testing.T) {
	server := newMockPRServer(t)

	// Comment list variations
	out, err := executePr(t, server.URL, "comment", "list", "42", "--unresolved")
	if err != nil {
		t.Fatalf("unexpected error on comment list unresolved: %v", err)
	}
	if !strings.Contains(out, "Comment 1") {
		t.Fatalf("expected Comment 1 in list: %s", out)
	}

	out, err = executePr(t, server.URL, "comment", "list", "42", "--full")
	if err != nil {
		t.Fatalf("unexpected error on comment list full: %v", err)
	}
	if !strings.Contains(out, "Comment 1") {
		t.Fatalf("--full must still list the comments: %s", out)
	}

	out, err = executePr(t, server.URL, "comment", "list", "42", "--blocker")
	if err != nil {
		t.Fatalf("unexpected error on comment list blocker: %v", err)
	}
	// --blocker reads a different endpoint, so the fixture returns different
	// text. Asserting it is what distinguishes the flag being honoured from
	// the flag being accepted and ignored -- the failure #476 describes.
	if !strings.Contains(out, "Blocker comment") {
		t.Fatalf("--blocker must read the blocker comments: %s", out)
	}

	out, err = executePr(t, server.URL, "comment", "list", "42", "--path", "file1.go")
	if err != nil {
		t.Fatalf("unexpected error on comment list path: %v", err)
	}
	if !strings.Contains(out, "Comment 1") {
		t.Fatalf("--path must still list the matching comments: %s", out)
	}

	out, err = executePr(t, server.URL, "--json", "comment", "list", "42")
	if err != nil {
		t.Fatalf("unexpected error on comment list json: %v", err)
	}
	if !strings.Contains(out, "threads") {
		t.Fatalf("expected threads in json: %s", out)
	}

	// Comment get JSON
	out, err = executePr(t, server.URL, "--json", "comment", "get", "42", "101")
	if err != nil {
		t.Fatalf("unexpected error on comment get json: %v", err)
	}
	if !strings.Contains(out, "Comment 1") {
		t.Fatalf("expected Comment 1 in json: %s", out)
	}

	// Comment add dry-run and JSON
	out, err = executePr(t, server.URL, "--dry-run", "comment", "add", "42", "--text", "Blocker note", "--blocker")
	if err != nil {
		t.Fatalf("unexpected error on comment add blocker dry run: %v", err)
	}
	assertDryRunPreview(t, out)

	out, err = executePr(t, server.URL, "--json", "comment", "add", "42", "--text", "Draft note", "--pending")
	if err != nil {
		t.Fatalf("unexpected error on comment add json: %v", err)
	}
	if !strings.Contains(out, "pending") {
		t.Fatalf("expected pending in json: %s", out)
	}

	// Comment resolve dry-run and JSON
	out, err = executePr(t, server.URL, "--dry-run", "comment", "resolve", "42", "101")
	if err != nil {
		t.Fatalf("unexpected error on comment resolve dry run: %v", err)
	}
	assertDryRunPreview(t, out)

	out, err = executePr(t, server.URL, "--json", "comment", "resolve", "42", "101")
	if err != nil {
		t.Fatalf("unexpected error on comment resolve json: %v", err)
	}
	if !strings.Contains(out, "RESOLVED") {
		t.Fatalf("expected RESOLVED in json: %s", out)
	}

	// Comment reopen dry-run and JSON
	out, err = executePr(t, server.URL, "--dry-run", "comment", "reopen", "42", "101")
	if err != nil {
		t.Fatalf("unexpected error on comment reopen dry run: %v", err)
	}
	assertDryRunPreview(t, out)

	out, err = executePr(t, server.URL, "--json", "comment", "reopen", "42", "101")
	if err != nil {
		t.Fatalf("unexpected error on comment reopen json: %v", err)
	}
	if !json.Valid([]byte(out)) {
		t.Fatalf("--json must emit a parseable envelope: %s", out)
	}
}

func TestPRDiffModes(t *testing.T) {
	server := newMockPRServer(t)

	// --patch
	out, err := executePr(t, server.URL, "diff", "42", "--patch")
	if err != nil {
		t.Fatalf("unexpected error on diff patch: %v", err)
	}
	if !strings.Contains(out, "Patch") {
		t.Fatalf("expected Patch in diff patch output: %s", out)
	}

	out, err = executePr(t, server.URL, "--json", "diff", "42", "--patch")
	if err != nil {
		t.Fatalf("unexpected error on diff patch json: %v", err)
	}
	if !strings.Contains(out, "Patch") {
		t.Fatalf("expected Patch in diff patch json output: %s", out)
	}

	// --stat
	out, err = executePr(t, server.URL, "diff", "42", "--stat")
	if err != nil {
		t.Fatalf("unexpected error on diff stat: %v", err)
	}
	if !strings.Contains(out, "totalInsertions") {
		t.Fatalf("expected the stats summary in diff stat output: %s", out)
	}

	out, err = executePr(t, server.URL, "--json", "diff", "42", "--stat")
	if err != nil {
		t.Fatalf("unexpected error on diff stat json: %v", err)
	}
	if !strings.Contains(out, "filesChanged") && !strings.Contains(out, "linesAdded") {
		t.Fatalf("expected stat in diff stat json output: %s", out)
	}

	// --name-only
	out, err = executePr(t, server.URL, "diff", "42", "--name-only")
	if err != nil {
		t.Fatalf("unexpected error on diff name-only: %v", err)
	}
	if !strings.Contains(out, "file1.go") {
		t.Fatalf("expected file1.go in diff name-only output: %s", out)
	}

	out, err = executePr(t, server.URL, "--json", "diff", "42", "--name-only")
	if err != nil {
		t.Fatalf("unexpected error on diff name-only json: %v", err)
	}
	if !strings.Contains(out, "file1.go") {
		t.Fatalf("expected file1.go in diff name-only json output: %s", out)
	}

	// raw diff json
	out, err = executePr(t, server.URL, "--json", "diff", "42")
	if err != nil {
		t.Fatalf("unexpected error on diff raw json: %v", err)
	}
	if !strings.Contains(out, "diff") {
		t.Fatalf("expected diff in raw json output: %s", out)
	}

	// mutual exclusion
	_, err = executePr(t, server.URL, "diff", "42", "--patch", "--stat")
	if err == nil {
		t.Fatalf("expected error on --patch and --stat together")
	}
}

func TestPRValidationErrors(t *testing.T) {
	server := newMockPRServer(t)

	// --unresolved with conflicting --state
	_, err := executePr(t, server.URL, "comment", "list", "42", "--unresolved", "--state", "resolved")
	if err == nil {
		t.Fatalf("expected error on --unresolved with --state resolved")
	}

	// invalid state
	_, err = executePr(t, server.URL, "comment", "list", "42", "--state", "invalid-state")
	if err == nil {
		t.Fatalf("expected error on invalid comment state")
	}
}

func TestPRURLAndBranchResolution(t *testing.T) {
	server := newMockPRServer(t)

	t.Run("resolve via full Bitbucket browser PR URL", func(t *testing.T) {
		prURL := server.URL + "/projects/PRJ/repos/demo/pull-requests/42"
		out, err := executePr(t, server.URL, "get", prURL)
		if err != nil {
			t.Fatalf("unexpected error resolving PR URL: %v", err)
		}
		if !strings.Contains(out, "#42") || !strings.Contains(out, "Test PR") {
			t.Fatalf("unexpected output for PR URL: %s", out)
		}
	})

	t.Run("resolve via numeric hash #42", func(t *testing.T) {
		out, err := executePr(t, server.URL, "get", "#42")
		if err != nil {
			t.Fatalf("unexpected error resolving #42: %v", err)
		}
		if !strings.Contains(out, "#42") {
			t.Fatalf("unexpected output for #42: %s", out)
		}
	})

	t.Run("resolve via source branch name", func(t *testing.T) {
		out, err := executePr(t, server.URL, "get", "feature/x")
		if err != nil {
			t.Fatalf("unexpected error resolving branch name: %v", err)
		}
		if !strings.Contains(out, "#42") {
			t.Fatalf("unexpected output for branch name: %s", out)
		}
	})
}

func TestPRCreateReviewerGroups(t *testing.T) {
	server := newMockPRServer(t)

	t.Run("create with reviewer group flag", func(t *testing.T) {
		out, err := executePr(t, server.URL, "create", "--from-ref", "feature/y", "--to-ref", "main", "--title", "Created PR", "--reviewer-group", "core-team")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, "Created pull request #43") {
			t.Fatalf("unexpected output: %s", out)
		}
	})

	t.Run("create with @group syntax in reviewers flag", func(t *testing.T) {
		out, err := executePr(t, server.URL, "create", "--from-ref", "feature/y", "--to-ref", "main", "--title", "Created PR", "--reviewers", "alice,@arch-team")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, "Created pull request #43") {
			t.Fatalf("unexpected output: %s", out)
		}
	})

	t.Run("create dry-run with group expansion", func(t *testing.T) {
		out, err := executePr(t, server.URL, "--dry-run", "--json", "create", "--from-ref", "feature/y", "--to-ref", "main", "--title", "Created PR", "--reviewer-group", "core-team")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertDryRunPreview(t, out)
		if !strings.Contains(out, "bob") || !strings.Contains(out, "charlie") {
			t.Fatalf("expected expanded reviewers in dry run output: %s", out)
		}
	})

	t.Run("create with nonexistent reviewer group fails", func(t *testing.T) {
		_, err := executePr(t, server.URL, "create", "--from-ref", "feature/y", "--to-ref", "main", "--title", "Created PR", "--reviewer-group", "nonexistent-team")
		if err == nil {
			t.Fatal("expected error for nonexistent group")
		}
		if !strings.Contains(err.Error(), "nonexistent-team") {
			t.Fatalf("expected nonexistent-team in error: %v", err)
		}
	})
}

func TestPRReviewerAddEnhanced(t *testing.T) {
	server := newMockPRServer(t)

	t.Run("validation error when no user or group provided", func(t *testing.T) {
		_, err := executePr(t, server.URL, "review", "reviewer", "add", "42")
		if err == nil {
			t.Fatal("expected validation error")
		}
	})

	t.Run("repeatable user flag", func(t *testing.T) {
		out, err := executePr(t, server.URL, "review", "reviewer", "add", "42", "--user", "bob", "--user", "charlie")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, "bob") || !strings.Contains(out, "charlie") {
			t.Fatalf("expected both users in output: %s", out)
		}
	})

	t.Run("users comma-separated", func(t *testing.T) {
		out, err := executePr(t, server.URL, "review", "reviewer", "add", "42", "--users", "bob,charlie")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, "bob") || !strings.Contains(out, "charlie") {
			t.Fatalf("expected both users in output: %s", out)
		}
	})

	t.Run("reviewer group expansion skipping already present reviewer", func(t *testing.T) {
		// PR 42 already has alice. core-team has bob and charlie.
		out, err := executePr(t, server.URL, "review", "reviewer", "add", "42", "--reviewer-group", "core-team", "--user", "alice")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, "Added reviewers") {
			t.Fatalf("expected Added reviewers in output: %s", out)
		}
		if !strings.Contains(out, "already present: alice") {
			t.Fatalf("expected already present note in output: %s", out)
		}
	})

	t.Run("skip pull request author", func(t *testing.T) {
		// PR 42 author is "authoruser"
		out, err := executePr(t, server.URL, "review", "reviewer", "add", "42", "--user", "authoruser", "--user", "bob")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, "Skipped authoruser (pull request author)") {
			t.Fatalf("expected skipped author message: %s", out)
		}
		if !strings.Contains(out, "Added reviewer bob") {
			t.Fatalf("expected bob added: %s", out)
		}
	})

	t.Run("dry-run preview with mixed states", func(t *testing.T) {
		out, err := executePr(t, server.URL, "--dry-run", "review", "reviewer", "add", "42", "--user", "alice", "--user", "authoruser", "--user", "bob")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertDryRunPreview(t, out)
		if !strings.Contains(out, "reviewer already present") {
			t.Fatalf("expected reviewer already present in dry run: %s", out)
		}
		if !strings.Contains(out, "pull request author cannot be reviewer") {
			t.Fatalf("expected author cannot be reviewer in dry run: %s", out)
		}
		if !strings.Contains(out, "reviewer will be added") {
			t.Fatalf("expected reviewer will be added in dry run: %s", out)
		}
	})

	t.Run("at-group shorthand", func(t *testing.T) {
		out, err := executePr(t, server.URL, "review", "reviewer", "add", "42", "--user", "@arch-team")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, "Added reviewer david") {
			t.Fatalf("expected david added: %s", out)
		}
	})
}

func TestPRDefaultReviewersAndCodeOwners(t *testing.T) {
	server := newMockPRServer(t)

	t.Run("pr create includes default reviewers and codeowners by default", func(t *testing.T) {
		out, err := executePr(t, server.URL, "--json", "create",
			"--from-ref", "feature/x",
			"--to-ref", "main",
			"--title", "Feature PR",
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, `"name": "bob"`) {
			t.Fatalf("expected default reviewer bob to be included: %s", out)
		}
		if !strings.Contains(out, `"name": "gopher"`) {
			t.Fatalf("expected codeowner gopher to be included by default: %s", out)
		}
	})

	t.Run("pr create excludes default reviewers with --no-default-reviewers", func(t *testing.T) {
		out, err := executePr(t, server.URL, "--json", "create",
			"--from-ref", "feature/x",
			"--to-ref", "main",
			"--title", "Feature PR",
			"--no-default-reviewers",
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if strings.Contains(out, `"name": "bob"`) {
			t.Fatalf("expected bob NOT to be included: %s", out)
		}
		if !strings.Contains(out, `"name": "gopher"`) {
			t.Fatalf("expected codeowner gopher to still be included: %s", out)
		}
	})

	t.Run("pr create excludes code owners with --no-codeowners", func(t *testing.T) {
		out, err := executePr(t, server.URL, "--json", "create",
			"--from-ref", "feature/x",
			"--to-ref", "main",
			"--title", "Feature PR",
			"--no-codeowners",
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, `"name": "bob"`) {
			t.Fatalf("expected default reviewer bob to still be included: %s", out)
		}
		if strings.Contains(out, `"name": "gopher"`) {
			t.Fatalf("expected codeowner gopher NOT to be included: %s", out)
		}
	})

	t.Run("pr create excludes both with --no-default-reviewers and --no-codeowners", func(t *testing.T) {
		out, err := executePr(t, server.URL, "--json", "create",
			"--from-ref", "feature/x",
			"--to-ref", "main",
			"--title", "Feature PR",
			"--no-default-reviewers",
			"--no-codeowners",
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if strings.Contains(out, `"name": "bob"`) || strings.Contains(out, `"name": "gopher"`) {
			t.Fatalf("expected no reviewers to be included: %s", out)
		}
	})

	t.Run("reviewer add with --default-reviewers assigns default reviewers", func(t *testing.T) {
		out, err := executePr(t, server.URL, "review", "reviewer", "add", "42", "--default-reviewers")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, "Added reviewer bob to pull request #42") {
			t.Fatalf("expected bob added: %s", out)
		}
	})

	t.Run("reviewer add with --codeowners assigns code owners", func(t *testing.T) {
		out, err := executePr(t, server.URL, "review", "reviewer", "add", "42", "--codeowners")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, "Added reviewer gopher to pull request #42") {
			t.Fatalf("expected gopher added: %s", out)
		}
	})
}

// assertDryRunPreview fails when a --dry-run invocation produced no preview.
//
// The preview is the entire product of a dry run: it is what tells the caller
// what would happen. These tests used to capture the output and check only that
// the command did not error, which cannot tell a real preview from an empty one
// -- and a --dry-run that quietly does nothing is the shape of #481, where a
// command pre-flighted as read-only and then created a commit.
//
// The text comes from the shared writer in internal/cli/dryrunpreview, which
// renders "Dry-run (<mode>, capability=<capability>)" for every command.
func assertDryRunPreview(t *testing.T, out string) {
	t.Helper()

	// Human output carries the rendered banner; --json carries the same
	// preview as an envelope with dryRun true. Either proves a preview was
	// produced, which is the thing being asserted.
	if !strings.Contains(out, "Dry-run") && !strings.Contains(out, `"dryRun": true`) {
		t.Fatalf("expected a dry-run preview, got: %q", out)
	}
}

// TestPRMergeDryRunReadsMergeability is #479.
//
// The prediction was made from the pull request's state alone, so any open pull
// request was reported as "will be merged" at confidence full with an empty
// blockingReasons -- however many vetoes stood against it. Merging is the
// irreversible operation, so this was the weakest prediction in the tool making
// the strongest claim the contract offers.
func TestPRMergeDryRunReadsMergeability(t *testing.T) {
	newServer := func(t *testing.T, mergeBody string) *httptest.Server {
		t.Helper()

		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("Content-Type", "application/json")

			switch {
			case request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/pull-requests/42/merge"):
				if mergeBody == "" {
					writer.WriteHeader(http.StatusNotFound)
					_, _ = writer.Write([]byte(`{"errors":[{"message":"no mergeability"}]}`))

					return
				}
				_, _ = writer.Write([]byte(mergeBody))
			case request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/pull-requests/42"):
				_, _ = writer.Write([]byte(`{"id":42,"version":1,"state":"OPEN","title":"T","open":true,
					"fromRef":{"displayId":"feat","repository":{"slug":"demo","project":{"key":"PRJ"}}},
					"toRef":{"displayId":"main","repository":{"slug":"demo","project":{"key":"PRJ"}}}}`))
			default:
				writer.WriteHeader(http.StatusOK)
				_, _ = writer.Write([]byte(`{}`))
			}
		}))
		t.Cleanup(server.Close)

		return server
	}

	t.Run("a mergeable pull request still predicts a merge", func(t *testing.T) {
		server := newServer(t, `{"canMerge":true,"conflicted":false,"vetoes":[]}`)

		out, err := executePr(t, server.URL, "--json", "--dry-run", "merge", "42")
		if err != nil {
			t.Fatalf("merge dry-run: %v\n%s", err, out)
		}
		if !strings.Contains(out, `"predictedAction": "update"`) {
			t.Errorf("a mergeable pull request was not predicted to merge: %s", out)
		}
	})

	t.Run("vetoes block the prediction and are named", func(t *testing.T) {
		server := newServer(t, `{"canMerge":false,"conflicted":true,"vetoes":[
			{"summaryMessage":"Not enough approvals","detailedMessage":"2 of 3 required"},
			{"summaryMessage":"Build failed"}]}`)

		out, err := executePr(t, server.URL, "--json", "--dry-run", "merge", "42")
		if err != nil {
			t.Fatalf("merge dry-run: %v\n%s", err, out)
		}
		if !strings.Contains(out, `"predictedAction": "blocked"`) {
			t.Errorf("a vetoed merge was predicted to succeed: %s", out)
		}
		for _, want := range []string{"merge conflicts", "Not enough approvals", "2 of 3 required", "Build failed"} {
			if !strings.Contains(out, want) {
				t.Errorf("blockingReasons does not name %q: %s", want, out)
			}
		}
	})

	t.Run("an unanswered mergeability check drops the tier", func(t *testing.T) {
		server := newServer(t, "")

		out, err := executePr(t, server.URL, "--json", "--dry-run", "merge", "42")
		if err != nil {
			t.Fatalf("merge dry-run: %v\n%s", err, out)
		}
		if !strings.Contains(out, `"confidence": "partial"`) {
			t.Errorf("an unchecked prediction still claimed full confidence: %s", out)
		}
	})
}
