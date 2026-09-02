package comment

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

func newCommentTestService(t *testing.T, handler http.HandlerFunc) *Service {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := openapigenerated.NewClientWithResponses(server.URL + "/rest")
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	return NewService(client)
}

func TestTargetContext(t *testing.T) {
	ctx := Target{Repository: RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, CommitID: "abc"}.Context()
	if ctx.Type != "commit" || ctx.CommitID != "abc" {
		t.Fatalf("unexpected commit context: %+v", ctx)
	}

	prCtx := Target{Repository: RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, PullRequestID: "12"}.Context()
	if prCtx.Type != "pull_request" || prCtx.PullRequestID != "12" {
		t.Fatalf("unexpected pull request context: %+v", prCtx)
	}
}

func TestServiceCreateGetUpdateDeleteCommit(t *testing.T) {
	service := newCommentTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/commits/abc/comments":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":10,"text":"created","version":0}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/commits/abc/comments/10":
			_, _ = w.Write([]byte(`{"id":10,"text":"current","version":2}`))
		case r.Method == http.MethodPut && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/commits/abc/comments/10":
			_, _ = w.Write([]byte(`{"id":10,"text":"updated","version":3}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/commits/abc/comments/10":
			if r.URL.Query().Get("version") != "2" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte("missing version"))
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})

	target := Target{Repository: RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, CommitID: "abc"}

	created, err := service.Create(context.Background(), target, "hello")
	if err != nil || created.Id == nil || *created.Id != 10 {
		t.Fatalf("expected created comment, got %#v err=%v", created, err)
	}

	updated, err := service.Update(context.Background(), target, "10", "changed", nil)
	if err != nil || updated.Version == nil || *updated.Version != 3 {
		t.Fatalf("expected updated comment, got %#v err=%v", updated, err)
	}

	resolvedVersion, err := service.Delete(context.Background(), target, "10", nil)
	if err != nil || resolvedVersion == nil || *resolvedVersion != 2 {
		t.Fatalf("expected delete with resolved version, got %v err=%v", resolvedVersion, err)
	}
}

func TestServiceValidationAndStatusMapping(t *testing.T) {
	service := newCommentTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("unauthorized"))
	})

	_, err := service.List(context.Background(), Target{}, "", 25)
	if err == nil || !strings.Contains(err.Error(), "repository must be specified") {
		t.Fatalf("expected repository validation error, got %v", err)
	}

	_, err = service.List(context.Background(), Target{Repository: RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, CommitID: "abc"}, "seed.txt", 25)
	if err == nil || !strings.Contains(err.Error(), "authentication") {
		t.Fatalf("expected mapped auth error, got %v", err)
	}
}

func TestServicePullRequestPaginationAndCRUDFallbacks(t *testing.T) {
	service := newCommentTestService(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/12/comments":
			if r.URL.Query().Get("start") == "1" {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":2,"text":"page2","version":1}]}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"isLastPage":false,"nextPageStart":1,"values":[{"id":1,"text":"page1","version":1}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/12/comments":
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodPut && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/12/comments/22":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/12/comments/22":
			if r.URL.Query().Get("version") != "7" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte("missing version=7"))
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/12/comments/22":
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	})

	target := Target{Repository: RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, PullRequestID: "12"}

	comments, err := service.List(context.Background(), target, "seed.txt", 2)
	if err != nil {
		t.Fatalf("expected paginated pr list to succeed, got: %v", err)
	}
	if len(comments) != 2 {
		t.Fatalf("expected 2 comments from pagination, got: %d", len(comments))
	}

	created, err := service.Create(context.Background(), target, "new pr comment")
	if err != nil {
		t.Fatalf("expected pr create to succeed, got: %v", err)
	}
	if created.Text == nil || *created.Text != "new pr comment" {
		t.Fatalf("expected fallback created text payload, got: %#v", created)
	}

	providedVersion := int32(7)
	updated, err := service.Update(context.Background(), target, "22", "updated text", &providedVersion)
	if err != nil {
		t.Fatalf("expected pr update to succeed, got: %v", err)
	}
	if updated.Text == nil || *updated.Text != "updated text" {
		t.Fatalf("expected fallback updated text payload, got: %#v", updated)
	}

	resolvedVersion, err := service.Delete(context.Background(), target, "22", &providedVersion)
	if err != nil {
		t.Fatalf("expected pr delete to succeed, got: %v", err)
	}
	if resolvedVersion == nil || *resolvedVersion != 7 {
		t.Fatalf("expected resolved version 7, got: %v", resolvedVersion)
	}

	// A read has no fallback. An empty body is not a comment, and answering
	// with a zero-value one made the version lookup behind update, delete and
	// resolve return nothing -- so the write went out unlocked and succeeded.
	if _, err := service.Get(context.Background(), target, "22"); err == nil {
		t.Fatal("an empty successful response was reported as a readable comment")
	}
}

func TestCommentMapStatusErrorCoverage(t *testing.T) {
	if err := openapi.MapStatusError(http.StatusOK, nil); err != nil {
		t.Fatalf("expected nil for success status, got: %v", err)
	}

	tests := []struct {
		status   int
		exitCode int
	}{
		{status: http.StatusBadRequest, exitCode: 2},
		{status: http.StatusUnauthorized, exitCode: 3},
		{status: http.StatusForbidden, exitCode: 3},
		{status: http.StatusNotFound, exitCode: 4},
		{status: http.StatusConflict, exitCode: 5},
		{status: http.StatusTooManyRequests, exitCode: 10},
		{status: http.StatusInternalServerError, exitCode: 10},
		{status: http.StatusNotAcceptable, exitCode: 1},
	}

	for _, testCase := range tests {
		err := openapi.MapStatusError(testCase.status, []byte("boom"))
		if err == nil {
			t.Fatalf("expected error for status %d", testCase.status)
		}
		if apperrors.ExitCode(err) != testCase.exitCode {
			t.Fatalf("expected exit code %d for status %d, got %d", testCase.exitCode, testCase.status, apperrors.ExitCode(err))
		}
	}
}

func TestCommentServiceAdditionalBranches(t *testing.T) {
	t.Run("validation branches", func(t *testing.T) {
		service := newCommentTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		target := Target{Repository: RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, CommitID: "abc"}
		if _, err := service.List(context.Background(), target, "", 10); err == nil {
			t.Fatal("expected path validation error")
		}
		if _, err := service.List(context.Background(), Target{Repository: RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, PullRequestID: "12"}, "", 10); err == nil {
			t.Fatal("expected pull request path validation error")
		}
		if _, err := service.Create(context.Background(), target, " "); err == nil {
			t.Fatal("expected comment text validation error")
		}
		if _, err := service.Update(context.Background(), target, "", "text", nil); err == nil {
			t.Fatal("expected comment id validation error")
		}
		if _, err := service.Update(context.Background(), target, "10", "", nil); err == nil {
			t.Fatal("expected update text validation error")
		}
		if _, err := service.Delete(context.Background(), target, "", nil); err == nil {
			t.Fatal("expected delete comment id validation error")
		}
		if _, err := service.Get(context.Background(), target, ""); err == nil {
			t.Fatal("expected get comment id validation error")
		}
	})

	t.Run("commit pagination and fallback payloads", func(t *testing.T) {
		service := newCommentTestService(t, func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/commits/abc/comments":
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Query().Get("start") == "1" {
					_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":2,"text":"c2","version":1}]}`))
					return
				}
				_, _ = w.Write([]byte(`{"isLastPage":false,"nextPageStart":1,"values":[{"id":1,"text":"c1","version":1}]}`))
			case r.Method == http.MethodPost && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/commits/abc/comments":
				w.WriteHeader(http.StatusCreated)
			case r.Method == http.MethodPut && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/commits/abc/comments/10":
				w.WriteHeader(http.StatusOK)
			case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/commits/abc/comments/10":
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":10,"version":5}`))
			case r.Method == http.MethodDelete && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/commits/abc/comments/10":
				w.WriteHeader(http.StatusNoContent)
			default:
				http.NotFound(w, r)
			}
		})

		target := Target{Repository: RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, CommitID: "abc"}

		comments, err := service.List(context.Background(), target, "seed.txt", 2)
		if err != nil || len(comments) != 2 {
			t.Fatalf("expected paginated commit comments, got len=%d err=%v", len(comments), err)
		}

		created, err := service.Create(context.Background(), target, "new")
		if err != nil {
			t.Fatalf("expected create fallback success, got %v", err)
		}
		if created.Text == nil || *created.Text != "new" {
			t.Fatalf("expected fallback create payload, got %#v", created)
		}

		updated, err := service.Update(context.Background(), target, "10", "updated", nil)
		if err != nil {
			t.Fatalf("expected update fallback success, got %v", err)
		}
		if updated.Text == nil || *updated.Text != "updated" {
			t.Fatalf("expected fallback update payload, got %#v", updated)
		}

		resolved, err := service.Delete(context.Background(), target, "10", nil)
		if err != nil {
			t.Fatalf("expected delete success, got %v", err)
		}
		if resolved == nil || *resolved != 5 {
			t.Fatalf("expected resolved delete version 5, got %v", resolved)
		}
	})

	t.Run("transport and status error branches", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		baseURL := server.URL
		server.Close()

		client, err := openapigenerated.NewClientWithResponses(baseURL + "/rest")
		if err != nil {
			t.Fatalf("create client: %v", err)
		}
		service := NewService(client)

		commitTarget := Target{Repository: RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, CommitID: "abc"}
		prTarget := Target{Repository: RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, PullRequestID: "12"}

		if _, err := service.Create(context.Background(), commitTarget, "x"); err == nil || apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient commit create transport error, got %v", err)
		}
		if _, err := service.Create(context.Background(), prTarget, "x"); err == nil || apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient pr create transport error, got %v", err)
		}

		statusService := newCommentTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte("conflict"))
		})
		if _, err := statusService.Get(context.Background(), prTarget, "1"); err == nil || apperrors.ExitCode(err) != 5 {
			t.Fatalf("expected conflict mapping for get, got %v", err)
		}
	})
}

func TestServiceBlockerCommentsReactionsAndSuggestions(t *testing.T) {
	service := newCommentTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		// Blocker List
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/12/blocker-comments":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":100,"text":"blocker","version":1}]}`))
		// Blocker Create
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/12/blocker-comments":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":101,"text":"new blocker","version":0}`))
		// Blocker Get
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/12/blocker-comments/101":
			_, _ = w.Write([]byte(`{"id":101,"text":"current blocker","version":2}`))
		// Blocker Update
		case r.Method == http.MethodPut && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/12/blocker-comments/101":
			_, _ = w.Write([]byte(`{"id":101,"text":"updated blocker","version":3}`))
		// Blocker Delete
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/12/blocker-comments/101":
			if r.URL.Query().Get("version") != "2" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusNoContent)

		// Reaction Add
		case r.Method == http.MethodPut && r.URL.Path == "/rest/comment-likes/latest/projects/TEST/repos/demo/pull-requests/12/comments/100/reactions/thumbsup":
			_, _ = w.Write([]byte(`{"emoticon":{"shortcut":"thumbsup","value":"👍"}}`))
		// Reaction Remove
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/comment-likes/latest/projects/TEST/repos/demo/pull-requests/12/comments/100/reactions/thumbsup":
			w.WriteHeader(http.StatusNoContent)

		// Apply Suggestion
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/12/comments/100/apply-suggestion":
			w.WriteHeader(http.StatusOK)

		default:
			http.NotFound(w, r)
		}
	})

	target := Target{Repository: RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, PullRequestID: "12", Blocker: true}

	// Test blocker validation error for commit
	invalidTarget := Target{Repository: RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, CommitID: "abc", Blocker: true}
	_, err := service.List(context.Background(), invalidTarget, "", 25)
	if err == nil {
		t.Fatal("expected blocker commit validation error")
	}

	// 1. List
	list, err := service.List(context.Background(), target, "", 25)
	if err != nil || len(list) != 1 || *list[0].Id != 100 {
		t.Fatalf("expected 1 blocker comment, got %v err=%v", list, err)
	}

	// 2. Create
	created, err := service.Create(context.Background(), target, "new blocker")
	if err != nil || *created.Id != 101 {
		t.Fatalf("expected created blocker comment, got %v err=%v", created, err)
	}

	// 3. Get
	got, err := service.Get(context.Background(), target, "101")
	if err != nil || *got.Version != 2 {
		t.Fatalf("expected blocker comment details, got %v err=%v", got, err)
	}

	// 4. Update
	updated, err := service.Update(context.Background(), target, "101", "updated blocker", nil)
	if err != nil || *updated.Version != 3 {
		t.Fatalf("expected updated blocker comment, got %v err=%v", updated, err)
	}

	// 5. Delete
	resolvedVersion, err := service.Delete(context.Background(), target, "101", nil)
	if err != nil || *resolvedVersion != 2 {
		t.Fatalf("expected resolved version 2, got %v err=%v", resolvedVersion, err)
	}

	// 6. React
	reaction, err := service.React(context.Background(), target.Repository, "12", "100", "thumbsup")
	if err != nil || reaction.Emoticon == nil || *reaction.Emoticon.Shortcut != "thumbsup" {
		t.Fatalf("expected reaction, got %v err=%v", reaction, err)
	}

	// 7. UnReact
	err = service.UnReact(context.Background(), target.Repository, "12", "100", "thumbsup")
	if err != nil {
		t.Fatalf("expected successful unreact, got %v", err)
	}

	// 8. ApplySuggestion
	err = service.ApplySuggestion(context.Background(), target.Repository, "12", "100", openapigenerated.RestApplySuggestionRequest{})
	if err != nil {
		t.Fatalf("expected successful suggestion application, got %v", err)
	}
}

func TestServiceCreateInlineAndThreadedComments(t *testing.T) {
	var receivedBody map[string]any
	service := newCommentTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/12/comments" {
			_ = json.NewDecoder(r.Body).Decode(&receivedBody)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":200,"text":"created","version":1}`))
			return
		}
		http.NotFound(w, r)
	})

	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

	// 1. Validation tests
	// path without line
	_, err := service.Create(context.Background(), Target{Repository: repo, PullRequestID: "12", Path: "foo.go"}, "msg")
	if err == nil || !strings.Contains(err.Error(), "path requires a positive line") {
		t.Fatalf("expected error for path without line, got %v", err)
	}

	// line without path
	_, err = service.Create(context.Background(), Target{Repository: repo, PullRequestID: "12", Line: 10}, "msg")
	if err == nil || !strings.Contains(err.Error(), "line requires path") {
		t.Fatalf("expected error for line without path, got %v", err)
	}

	// parent_id combined with path/line
	_, err = service.Create(context.Background(), Target{Repository: repo, PullRequestID: "12", Path: "foo.go", Line: 10, ParentID: 99}, "msg")
	if err == nil || !strings.Contains(err.Error(), "parent_id cannot be combined with path/line") {
		t.Fatalf("expected error for parent_id with path/line, got %v", err)
	}

	// parent_id combined with blocker
	_, err = service.Create(context.Background(), Target{Repository: repo, PullRequestID: "12", ParentID: 99, Blocker: true}, "msg")
	if err == nil || !strings.Contains(err.Error(), "parent_id cannot be combined with blocker") {
		t.Fatalf("expected error for parent_id with blocker, got %v", err)
	}

	// line_type on non-inline comment
	_, err = service.Create(context.Background(), Target{Repository: repo, PullRequestID: "12", LineType: "ADDED"}, "msg")
	if err == nil || !strings.Contains(err.Error(), "line_type only applies to inline comments") {
		t.Fatalf("expected error for line_type without inline, got %v", err)
	}

	// invalid line_type
	_, err = service.Create(context.Background(), Target{Repository: repo, PullRequestID: "12", Path: "foo.go", Line: 10, LineType: "INVALID"}, "msg")
	if err == nil || !strings.Contains(err.Error(), "line_type must be ADDED, REMOVED, or CONTEXT") {
		t.Fatalf("expected error for invalid line_type, got %v", err)
	}

	// 2. Threaded reply comment
	receivedBody = nil
	cmt, err := service.Create(context.Background(), Target{Repository: repo, PullRequestID: "12", ParentID: 55}, "reply text")
	if err != nil || cmt.Id == nil || *cmt.Id != 200 {
		t.Fatalf("expected successful reply creation, got %v err=%v", cmt, err)
	}
	parentMap, ok := receivedBody["parent"].(map[string]any)
	if !ok || parentMap["id"] != float64(55) {
		t.Fatalf("expected parent id 55 in payload, got %#v", receivedBody)
	}

	// 3. Inline comment with ADDED
	receivedBody = nil
	cmt, err = service.Create(context.Background(), Target{Repository: repo, PullRequestID: "12", Path: "pkg/foo/bar.go", Line: 42, LineType: "ADDED"}, "inline comment")
	if err != nil || cmt.Id == nil || *cmt.Id != 200 {
		t.Fatalf("expected successful inline comment creation, got %v err=%v", cmt, err)
	}
	anchorMap, ok := receivedBody["anchor"].(map[string]any)
	if !ok {
		t.Fatalf("expected anchor in payload, got %#v", receivedBody)
	}
	if anchorMap["line"] != float64(42) || anchorMap["lineType"] != "ADDED" || anchorMap["fileType"] != "TO" || anchorMap["diffType"] != "EFFECTIVE" {
		t.Fatalf("unexpected anchor values: %#v", anchorMap)
	}
	// The create endpoint takes anchor.path as a plain string. The generated
	// model describes the object Bitbucket sends back, and building the request
	// from that shape leaves the comment unanchored.
	if anchorMap["path"] != "pkg/foo/bar.go" {
		t.Fatalf("expected a plain string anchor path, got %#v", anchorMap["path"])
	}

	// 4. Inline comment with REMOVED
	receivedBody = nil
	_, err = service.Create(context.Background(), Target{Repository: repo, PullRequestID: "12", Path: "root.go", Line: 5, LineType: "REMOVED"}, "removed line comment")
	if err != nil {
		t.Fatalf("unexpected error for REMOVED comment: %v", err)
	}
	anchorMap = receivedBody["anchor"].(map[string]any)
	if anchorMap["fileType"] != "FROM" || anchorMap["lineType"] != "REMOVED" {
		t.Fatalf("unexpected anchor for REMOVED: %#v", anchorMap)
	}
	if anchorMap["path"] != "root.go" {
		t.Fatalf("expected a plain string anchor path for a root file, got %#v", anchorMap["path"])
	}

	// 5. A pending inline comment still asks for the PENDING state.
	receivedBody = nil
	_, err = service.Create(context.Background(), Target{Repository: repo, PullRequestID: "12", Path: "root.go", Line: 5, Pending: true}, "draft inline")
	if err != nil {
		t.Fatalf("unexpected error for pending inline comment: %v", err)
	}
	if receivedBody["state"] != "PENDING" {
		t.Fatalf("expected PENDING state alongside the anchor, got %#v", receivedBody)
	}
	if _, ok := receivedBody["anchor"].(map[string]any); !ok {
		t.Fatalf("expected the anchor to survive alongside the pending state, got %#v", receivedBody)
	}
}

// TestServiceCreateExplainsAnchorRejection covers the 400 Bitbucket returns
// when the anchored line is not in the diff. The raw response says nothing
// about the line, the path, or the rule.
func TestServiceCreateExplainsAnchorRejection(t *testing.T) {
	service := newCommentTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json;charset=UTF-8")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errors":[{"message":"The comment anchor is invalid.","exceptionName":null}]}`))
	})

	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

	_, err := service.Create(context.Background(),
		Target{Repository: repo, PullRequestID: "12", Path: "pkg/foo/bar.go", Line: 157, LineType: "ADDED"},
		"this line needs a guard")
	if err == nil {
		t.Fatal("expected an error for a rejected anchor")
	}
	if !apperrors.IsKind(err, apperrors.KindValidation) {
		t.Fatalf("expected a validation error, got %v", err)
	}

	message := err.Error()
	for _, want := range []string{
		"pkg/foo/bar.go:157",
		"line type ADDED",
		"only accepted on a line that appears in the pull request diff",
		// The server's own words are kept, not swallowed.
		"The comment anchor is invalid.",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("expected the error to mention %q, got: %s", want, message)
		}
	}

	// A plain pull-request-level comment must not be dressed up with anchor
	// advice it has nothing to do with.
	_, err = service.Create(context.Background(), Target{Repository: repo, PullRequestID: "12"}, "general comment")
	if err == nil {
		t.Fatal("expected an error for the general comment too")
	}
	if strings.Contains(err.Error(), "pull request diff") {
		t.Fatalf("anchor advice must not appear on a non-inline comment: %s", err.Error())
	}
}

// TestServiceCreateDecodesStringAnchorPathResponse covers the other half of the
// path-shape mismatch: Bitbucket echoes the created comment back with a string
// anchor path, which the generated model cannot decode. The comment exists by
// then, so a decode failure must not be reported as a failed create.
func TestServiceCreateDecodesStringAnchorPathResponse(t *testing.T) {
	service := newCommentTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json;charset=UTF-8")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":321,"version":0,"text":"inline","anchor":{"line":42,"lineType":"ADDED","fileType":"TO","diffType":"EFFECTIVE","path":"pkg/foo/bar.go","srcPath":"pkg/foo/bar.go"}}`))
	})

	created, err := service.Create(context.Background(),
		Target{Repository: RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, PullRequestID: "12", Path: "pkg/foo/bar.go", Line: 42},
		"inline")
	if err != nil {
		t.Fatalf("a string anchor path in the response must not fail the create: %v", err)
	}
	if created.Id == nil || *created.Id != 321 {
		t.Fatalf("expected the server-assigned id 321, got %#v", created.Id)
	}
	if created.Anchor == nil || created.Anchor.Path == nil || created.Anchor.Path.Name == nil || *created.Anchor.Path.Name != "bar.go" {
		t.Fatalf("expected the anchor path to be decoded into the object form, got %#v", created.Anchor)
	}
}

func TestServiceBlockerReactionsAndSuggestionsAdditionalErrors(t *testing.T) {
	// Blocker pagination test
	t.Run("blocker pagination", func(t *testing.T) {
		service := newCommentTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/12/blocker-comments" {
				if r.URL.Query().Get("start") == "1" {
					_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":101,"text":"blocker2","version":1}]}`))
					return
				}
				_, _ = w.Write([]byte(`{"isLastPage":false,"nextPageStart":1,"values":[{"id":100,"text":"blocker1","version":1}]}`))
				return
			}
			http.NotFound(w, r)
		})
		target := Target{Repository: RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, PullRequestID: "12", Blocker: true}
		list, err := service.List(context.Background(), target, "", 2)
		if err != nil || len(list) != 2 {
			t.Fatalf("expected paginated blocker list, got len=%d err=%v", len(list), err)
		}

		// Pagination with nil NextPageStart
		serviceNilNext := newCommentTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"isLastPage":false,"values":[{"id":100,"text":"blocker1","version":1}]}`))
		})
		listNilNext, err := serviceNilNext.List(context.Background(), target, "", 2)
		if err != nil || len(listNilNext) != 1 {
			t.Fatalf("expected 1 element for nil next page start, got len=%d err=%v", len(listNilNext), err)
		}
	})

	// React validation, transport, status errors
	t.Run("reactions and suggestions errors", func(t *testing.T) {
		// Validation
		service := newCommentTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		emptyRepo := RepositoryRef{}
		validRepo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

		if _, err := service.React(context.Background(), emptyRepo, "12", "100", "thumbsup"); err == nil {
			t.Fatal("expected error on empty repo for React")
		}
		if _, err := service.React(context.Background(), validRepo, "", "100", "thumbsup"); err == nil {
			t.Fatal("expected error on empty prID for React")
		}
		if _, err := service.React(context.Background(), validRepo, "12", "", "thumbsup"); err == nil {
			t.Fatal("expected error on empty commentID for React")
		}
		if _, err := service.React(context.Background(), validRepo, "12", "100", ""); err == nil {
			t.Fatal("expected error on empty emoticon for React")
		}

		if err := service.UnReact(context.Background(), emptyRepo, "12", "100", "thumbsup"); err == nil {
			t.Fatal("expected error on empty repo for UnReact")
		}
		if err := service.UnReact(context.Background(), validRepo, "", "100", "thumbsup"); err == nil {
			t.Fatal("expected error on empty prID for UnReact")
		}
		if err := service.UnReact(context.Background(), validRepo, "12", "", "thumbsup"); err == nil {
			t.Fatal("expected error on empty commentID for UnReact")
		}
		if err := service.UnReact(context.Background(), validRepo, "12", "100", ""); err == nil {
			t.Fatal("expected error on empty emoticon for UnReact")
		}

		if err := service.ApplySuggestion(context.Background(), emptyRepo, "12", "100", openapigenerated.RestApplySuggestionRequest{}); err == nil {
			t.Fatal("expected error on empty repo for ApplySuggestion")
		}
		if err := service.ApplySuggestion(context.Background(), validRepo, "", "100", openapigenerated.RestApplySuggestionRequest{}); err == nil {
			t.Fatal("expected error on empty prID for ApplySuggestion")
		}
		if err := service.ApplySuggestion(context.Background(), validRepo, "12", "", openapigenerated.RestApplySuggestionRequest{}); err == nil {
			t.Fatal("expected error on empty commentID for ApplySuggestion")
		}

		// Transport errors (client closed)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		baseURL := server.URL
		server.Close()
		client, _ := openapigenerated.NewClientWithResponses(baseURL + "/rest")
		serviceClosed := NewService(client)

		if _, err := serviceClosed.React(context.Background(), validRepo, "12", "100", "thumbsup"); err == nil {
			t.Fatal("expected transient react transport error")
		}
		if err := serviceClosed.UnReact(context.Background(), validRepo, "12", "100", "thumbsup"); err == nil {
			t.Fatal("expected transient unreact transport error")
		}
		if err := serviceClosed.ApplySuggestion(context.Background(), validRepo, "12", "100", openapigenerated.RestApplySuggestionRequest{}); err == nil {
			t.Fatal("expected transient apply suggestion transport error")
		}

		// Blocker transport errors
		blockerTarget := Target{Repository: validRepo, PullRequestID: "12", Blocker: true}
		if _, err := serviceClosed.List(context.Background(), blockerTarget, "", 25); err == nil {
			t.Fatal("expected blocker list transport error")
		}
		if _, err := serviceClosed.Create(context.Background(), blockerTarget, "hello"); err == nil {
			t.Fatal("expected blocker create transport error")
		}
		if _, err := serviceClosed.Update(context.Background(), blockerTarget, "100", "hello", nil); err == nil {
			t.Fatal("expected blocker update transport error")
		}
		if _, err := serviceClosed.Delete(context.Background(), blockerTarget, "100", nil); err == nil {
			t.Fatal("expected blocker delete transport error")
		}
		if _, err := serviceClosed.Get(context.Background(), blockerTarget, "100"); err == nil {
			t.Fatal("expected blocker get transport error")
		}

		// Blocker API status error (e.g. Forbidden)
		errService := newCommentTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		})
		if _, err := errService.List(context.Background(), blockerTarget, "", 25); err == nil || apperrors.ExitCode(err) != 3 {
			t.Fatalf("expected forbidden error for blocker list, got %v", err)
		}
		if _, err := errService.Create(context.Background(), blockerTarget, "hello"); err == nil || apperrors.ExitCode(err) != 3 {
			t.Fatalf("expected forbidden error for blocker create, got %v", err)
		}
		if _, err := errService.Update(context.Background(), blockerTarget, "100", "hello", nil); err == nil || apperrors.ExitCode(err) != 3 {
			t.Fatalf("expected forbidden error for blocker update, got %v", err)
		}
		if _, err := errService.Delete(context.Background(), blockerTarget, "100", nil); err == nil || apperrors.ExitCode(err) != 3 {
			t.Fatalf("expected forbidden error for blocker delete, got %v", err)
		}
		if _, err := errService.Get(context.Background(), blockerTarget, "100"); err == nil || apperrors.ExitCode(err) != 3 {
			t.Fatalf("expected forbidden error for blocker get, got %v", err)
		}

		// Reaction and Suggestion API status error (Forbidden)
		if _, err := errService.React(context.Background(), validRepo, "12", "100", "thumbsup"); err == nil || apperrors.ExitCode(err) != 3 {
			t.Fatalf("expected forbidden react error, got %v", err)
		}
		if err := errService.UnReact(context.Background(), validRepo, "12", "100", "thumbsup"); err == nil || apperrors.ExitCode(err) != 3 {
			t.Fatalf("expected forbidden unreact error, got %v", err)
		}
		if err := errService.ApplySuggestion(context.Background(), validRepo, "12", "100", openapigenerated.RestApplySuggestionRequest{}); err == nil || apperrors.ExitCode(err) != 3 {
			t.Fatalf("expected forbidden apply suggestion error, got %v", err)
		}
	})
}

func TestServiceFallbacks(t *testing.T) {
	validRepo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}
	blockerTarget := Target{Repository: validRepo, PullRequestID: "12", Blocker: true}

	t.Run("fallbacks for empty successful responses", func(t *testing.T) {
		service := newCommentTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK) // 200 OK with no body
		})

		// 1. List blocker (nil values / isLastPage / nextPageStart)
		list, err := service.List(context.Background(), blockerTarget, "", 25)
		if err != nil || len(list) != 0 {
			t.Fatalf("expected empty list for fallback, got %v err=%v", list, err)
		}

		// 2. Create blocker (uses 201 Created mock)
		createService := newCommentTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusCreated) // 201 Created with no body
		})
		created, err := createService.Create(context.Background(), blockerTarget, "hello")
		if err != nil || *created.Text != "hello" {
			t.Fatalf("expected fallback create body, got %v err=%v", created, err)
		}

		// 3. Update with an explicit version still falls back to the request
		// body: the write happened, and reporting a failure would invite a retry
		// that edits the comment twice.
		version := int32(4)
		updated, err := service.Update(context.Background(), blockerTarget, "100", "hello", &version)
		if err != nil || *updated.Text != "hello" {
			t.Fatalf("expected fallback update body, got %v err=%v", updated, err)
		}

		// 4. A write that has to look the version up first refuses instead.
		// An empty body is not a comment, and treating it as one sent the write
		// with no version at all -- which Bitbucket reads as "no optimistic
		// locking wanted" and applies over whatever a concurrent edit did.
		if _, err := service.Update(context.Background(), blockerTarget, "100", "hello", nil); err == nil {
			t.Fatal("an update proceeded after an unreadable version lookup")
		}
		deleteService := newCommentTestService(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodDelete {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			w.WriteHeader(http.StatusOK) // empty body for Get
		})
		if _, err := deleteService.Delete(context.Background(), blockerTarget, "100", nil); err == nil {
			t.Fatal("a delete proceeded after an unreadable version lookup")
		}

		// 5. And the read that fails says so, rather than answering with a
		// comment that has no fields.
		if _, err := service.Get(context.Background(), blockerTarget, "100"); err == nil {
			t.Fatal("an empty body was reported as a readable comment")
		}

		// 6. React
		reaction, err := service.React(context.Background(), validRepo, "12", "100", "thumbsup")
		if err != nil || reaction.Emoticon != nil {
			t.Fatalf("expected empty reaction fallback, got %v err=%v", reaction, err)
		}
	})
}

func TestServiceCreatePendingComment(t *testing.T) {
	var capturedBody openapigenerated.RestComment
	service := newCommentTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/12/comments" {
			decoder := json.NewDecoder(r.Body)
			if err := decoder.Decode(&capturedBody); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":15,"text":"pending comment","version":0,"pending":true}`))
			return
		}
		http.NotFound(w, r)
	})

	target := Target{
		Repository:    RepositoryRef{ProjectKey: "TEST", Slug: "demo"},
		PullRequestID: "12",
		Pending:       true,
	}

	created, err := service.Create(context.Background(), target, "pending comment")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if created.Pending == nil || !*created.Pending {
		t.Errorf("expected created comment to have pending true, got: %v", created.Pending)
	}

	// The server reads the draft state from state, not from the pending flag it
	// marks readOnly. Asserting the flag is what let a no-op --pending ship.
	if capturedBody.State == nil || *capturedBody.State != "PENDING" {
		t.Errorf("expected API payload to carry state PENDING, got: %v", capturedBody.State)
	}
}

func TestCountTasks(t *testing.T) {
	service := newCommentTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet || r.URL.Path != "/rest/api/latest/projects/TEST/repos/demo/pull-requests/12/blocker-comments" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("count") != "true" {
			t.Errorf("expected count=true, got query %q", r.URL.RawQuery)
		}
		// With count=true Bitbucket returns a state->count map instead of a page.
		_, _ = w.Write([]byte(`{"OPEN":3,"RESOLVED":7}`))
	})

	counts, err := service.CountTasks(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, "12")
	if err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if counts.Open != 3 || counts.Resolved != 7 {
		t.Fatalf("expected 3 open / 7 resolved, got %#v", counts)
	}
}

// A pull request with no tasks returns an empty map, which must read as zero
// rather than as an error.
func TestCountTasksWithNoTasks(t *testing.T) {
	service := newCommentTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	})

	counts, err := service.CountTasks(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, "12")
	if err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if counts.Open != 0 || counts.Resolved != 0 {
		t.Fatalf("expected zero counts, got %#v", counts)
	}
}

func TestCountTasksValidatesTarget(t *testing.T) {
	service := newCommentTestService(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	if _, err := service.CountTasks(context.Background(), RepositoryRef{ProjectKey: "", Slug: "demo"}, "12"); err == nil {
		t.Fatalf("expected a validation error for a missing project key")
	}
}

// TestSetStateResolvesAndReopens covers the state transitions that replaced
// marking a pull request task done.
func TestSetStateResolvesAndReopens(t *testing.T) {
	var sentBody map[string]any
	currentVersion := int32(4)

	service := newCommentTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			// The version the update needs; the caller did not supply one.
			_, _ = w.Write([]byte(`{"id":7,"version":4,"state":"OPEN","severity":"BLOCKER"}`))
		case http.MethodPut:
			_ = json.NewDecoder(r.Body).Decode(&sentBody)
			_, _ = w.Write([]byte(`{"id":7,"version":5,"state":"RESOLVED","severity":"BLOCKER"}`))
		default:
			http.NotFound(w, r)
		}
	})

	target := Target{Repository: RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, PullRequestID: "12"}

	updated, err := service.SetState(context.Background(), target, "7", CommentStateResolved, nil)
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if updated.State == nil || *updated.State != "RESOLVED" {
		t.Fatalf("state = %v, want RESOLVED", updated.State)
	}
	if sentBody["state"] != "RESOLVED" {
		t.Fatalf("sent state = %v, want RESOLVED", sentBody["state"])
	}
	// The endpoint rejects a request without the version -- 409, reporting
	// expectedVersion -1 -- so reading it first is not an optimisation.
	if sentBody["version"] != float64(currentVersion) {
		t.Fatalf("sent version = %v, want the version read from the server", sentBody["version"])
	}
	// Only the state moves. Sending the text back would overwrite an edit made
	// between the read and the write.
	if _, present := sentBody["text"]; present {
		t.Fatalf("text should not be sent when only changing state, got: %v", sentBody)
	}

	if _, err := service.SetState(context.Background(), target, "7", CommentStateOpen, nil); err != nil {
		t.Fatalf("reopen failed: %v", err)
	}
	if sentBody["state"] != "OPEN" {
		t.Fatalf("sent state = %v, want OPEN", sentBody["state"])
	}
}

func TestSetStateValidation(t *testing.T) {
	service := newCommentTestService(t, func(w http.ResponseWriter, r *http.Request) {})
	target := Target{Repository: RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, PullRequestID: "12"}

	if _, err := service.SetState(context.Background(), target, "  ", CommentStateResolved, nil); err == nil {
		t.Fatal("expected a validation error for an empty comment id")
	}
	if _, err := service.SetState(context.Background(), target, "7", CommentState("DONE"), nil); err == nil {
		t.Fatal("expected a validation error for an unknown state")
	}
}

// A supplied version is used as given, so a caller holding one does not pay for
// an extra read.
func TestSetStateUsesSuppliedVersion(t *testing.T) {
	var sentBody map[string]any
	reads := 0

	service := newCommentTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			reads++
			_, _ = w.Write([]byte(`{"id":7,"version":99}`))
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&sentBody)
		_, _ = w.Write([]byte(`{"id":7,"version":3,"state":"RESOLVED"}`))
	})

	target := Target{Repository: RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, PullRequestID: "12"}
	supplied := int32(2)
	if _, err := service.SetState(context.Background(), target, "7", CommentStateResolved, &supplied); err != nil {
		t.Fatalf("resolve with a supplied version failed: %v", err)
	}
	if reads != 0 {
		t.Fatalf("expected no read when a version is supplied, got %d", reads)
	}
	if sentBody["version"] != float64(2) {
		t.Fatalf("sent version = %v, want the supplied 2", sentBody["version"])
	}
}

// The error paths SetState can take, each of which returns a different kind and
// so a different exit code.
func TestSetStateErrorPaths(t *testing.T) {
	validTarget := Target{Repository: RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, PullRequestID: "12"}

	t.Run("invalid target", func(t *testing.T) {
		service := newCommentTestService(t, func(w http.ResponseWriter, r *http.Request) {
			t.Error("expected no request for an invalid target")
		})
		_, err := service.SetState(context.Background(), Target{}, "7", CommentStateResolved, nil)
		if err == nil || apperrors.ExitCode(err) != 2 {
			t.Fatalf("expected a validation error, got: %v", err)
		}
	})

	t.Run("read fails", func(t *testing.T) {
		service := newCommentTestService(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPut {
				t.Error("expected no update when the comment cannot be read")
			}
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":[{"message":"Comment 7 does not exist."}]}`))
		})
		_, err := service.SetState(context.Background(), validTarget, "7", CommentStateResolved, nil)
		if err == nil || apperrors.ExitCode(err) != 4 {
			t.Fatalf("expected a not-found error from the read, got: %v", err)
		}
	})

	t.Run("update fails", func(t *testing.T) {
		service := newCommentTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(`{"id":7,"version":1}`))
				return
			}
			// A stale version is the realistic failure here, and the one the
			// version read exists to avoid.
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"errors":[{"message":"You are attempting to modify a comment based on out-of-date information."}]}`))
		})
		_, err := service.SetState(context.Background(), validTarget, "7", CommentStateResolved, nil)
		if err == nil || apperrors.ExitCode(err) != 5 {
			t.Fatalf("expected a conflict error from the update, got: %v", err)
		}
	})

	t.Run("update answers without a parsed body", func(t *testing.T) {
		supplied := int32(1)
		service := newCommentTestService(t, func(w http.ResponseWriter, r *http.Request) {
			// 200 with a content type the generated client does not decode, so
			// the request body is echoed back instead of a parsed comment.
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
		})
		updated, err := service.SetState(context.Background(), validTarget, "7", CommentStateResolved, &supplied)
		if err != nil {
			t.Fatalf("expected the sent body to be returned, got: %v", err)
		}
		if updated.State == nil || *updated.State != "RESOLVED" {
			t.Fatalf("state = %v, want the state that was sent", updated.State)
		}
	})
}

// TestCreateAnchorsACommitCommentToAFile is what makes a commit comment
// listable.
//
// Bitbucket refuses to retrieve comments without a path -- 400 "The path query
// parameter is required when retrieving comments" -- so a comment anchored to
// nothing can be created and then never read back by any listing. The vendored
// spec types that path as optional, which is wrong, and believing it cost a
// live run.
func TestCreateAnchorsACommitCommentToAFile(t *testing.T) {
	var body []byte
	service := newCommentTestService(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":7,"version":0,"text":"anchored"}`))
	})

	target := Target{
		Repository: RepositoryRef{ProjectKey: "TEST", Slug: "demo"},
		CommitID:   "abc",
		Path:       "internal/cli/root.go",
		Line:       12,
		LineType:   "ADDED",
	}
	if _, err := service.Create(context.Background(), target, "anchored"); err != nil {
		t.Fatalf("create anchored commit comment: %v", err)
	}

	var sent map[string]any
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("decode request body: %v\n%s", err, body)
	}
	anchor, ok := sent["anchor"].(map[string]any)
	if !ok {
		t.Fatalf("no anchor in the request: %s", body)
	}
	// A plain string, not the object Bitbucket returns: the create endpoint
	// takes one shape and answers with another.
	if anchor["path"] != "internal/cli/root.go" {
		t.Errorf("anchor path = %v", anchor["path"])
	}
	if anchor["lineType"] != "ADDED" || anchor["fileType"] != "TO" {
		t.Errorf("anchor = %+v, want an ADDED line on the updated side", anchor)
	}
}

// TestCreateSendsTheParentOfAReply covers the other half: a reply carries the
// id of what it answers and inherits that comment's anchor.
func TestCreateSendsTheParentOfAReply(t *testing.T) {
	var body []byte
	service := newCommentTestService(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":8,"version":0,"text":"a reply"}`))
	})

	target := Target{
		Repository: RepositoryRef{ProjectKey: "TEST", Slug: "demo"},
		CommitID:   "abc",
		ParentID:   7,
	}
	if _, err := service.Create(context.Background(), target, "a reply"); err != nil {
		t.Fatalf("create reply: %v", err)
	}

	var sent map[string]any
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("decode request body: %v\n%s", err, body)
	}
	parent, ok := sent["parent"].(map[string]any)
	if !ok {
		t.Fatalf("no parent in the request: %s", body)
	}
	if parent["id"] != float64(7) {
		t.Errorf("parent = %+v, want the comment being replied to", parent)
	}
	if _, anchored := sent["anchor"]; anchored {
		t.Errorf("a reply carried its own anchor: %s", body)
	}
}

// TestListRepairsAnchorPathsAndPages covers the listing end to end: the shape
// Bitbucket really sends, across more than one page.
//
// This is the regression the live run found. Every comment written through the
// web interface on a diff is anchored, and an anchored path arrives as a string
// while the generated model expects an object -- so one of them made the whole
// listing fail. Paging is exercised in the same test because the repair has to
// happen on every page, not only the first.
func TestListRepairsAnchorPathsAndPages(t *testing.T) {
	pages := []string{
		`{"isLastPage":false,"nextPageStart":1,"values":[
			{"id":1,"text":"first","anchor":{"line":3,"path":"internal/cli/root.go"},
			 "comments":[{"id":2,"text":"a reply","anchor":{"line":3,"path":"internal/cli/root.go"}}]}
		]}`,
		`{"isLastPage":true,"values":[{"id":3,"text":"second","anchor":{"line":9,"path":"cmd/bb/main.go"}}]}`,
	}
	var starts []string
	served := 0
	service := newCommentTestService(t, func(w http.ResponseWriter, r *http.Request) {
		starts = append(starts, r.URL.Query().Get("start"))
		w.Header().Set("Content-Type", "application/json")
		if served < len(pages) {
			_, _ = w.Write([]byte(pages[served]))
			served++
			return
		}
		_, _ = w.Write([]byte(`{"isLastPage":true,"values":[]}`))
	})

	target := Target{Repository: RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, CommitID: "abc"}
	listed, err := service.List(context.Background(), target, "internal/cli/root.go", 25)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("expected both pages' roots, got %d", len(listed))
	}
	if len(starts) != 2 || starts[0] != "0" || starts[1] != "1" {
		t.Errorf("start values = %v, want the second page requested from nextPageStart", starts)
	}

	// The repair reached the anchor, its nested reply, and the second page.
	if listed[0].Anchor == nil || listed[0].Anchor.Path == nil || listed[0].Anchor.Path.Components == nil {
		t.Fatalf("first comment's anchor path was not repaired: %+v", listed[0].Anchor)
	}
	if listed[0].Comments == nil || len(*listed[0].Comments) != 1 {
		t.Fatalf("the nested reply was lost: %+v", listed[0].Comments)
	}
	if reply := (*listed[0].Comments)[0]; reply.Anchor == nil || reply.Anchor.Path == nil {
		t.Errorf("the reply's anchor path was not repaired: %+v", reply.Anchor)
	}
	if listed[1].Anchor == nil || listed[1].Anchor.Path == nil {
		t.Errorf("the second page was not repaired: %+v", listed[1].Anchor)
	}
}

// TestListSurfacesAMalformedPage keeps a broken response from reading as an
// empty one.
//
// An empty body means "no comments" and must not error; a body that is present
// and unparseable means something is wrong, and reporting it as an empty
// listing would tell a caller the file has no review feedback on it.
func TestListSurfacesAMalformedPage(t *testing.T) {
	service := newCommentTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"values":[ this is not json`))
	})

	target := Target{Repository: RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, CommitID: "abc"}
	if _, err := service.List(context.Background(), target, "seed.txt", 25); err == nil {
		t.Fatal("a malformed page was reported as a successful listing")
	}

	empty := newCommentTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	listed, err := empty.List(context.Background(), target, "seed.txt", 25)
	if err != nil {
		t.Fatalf("an empty body was reported as a failure: %v", err)
	}
	if len(listed) != 0 {
		t.Errorf("listed = %+v, want an empty listing", listed)
	}
}

// TestGetAndUpdateRepairAnchorPaths covers the two single reads that failed the
// same way the listing did.
//
// Get matters twice over: Update and Delete call it to resolve a version when
// the caller supplies none, so an anchored comment could not be edited or
// removed either.
func TestGetAndUpdateRepairAnchorPaths(t *testing.T) {
	anchored := `{"id":11,"version":4,"text":"inline","anchor":{"line":7,"path":"internal/cli/root.go"}}`
	service := newCommentTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(anchored))
	})

	target := Target{Repository: RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, CommitID: "abc"}

	got, err := service.Get(context.Background(), target, "11")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Anchor == nil || got.Anchor.Path == nil || got.Anchor.Path.Components == nil {
		t.Fatalf("get did not repair the anchor path: %+v", got.Anchor)
	}
	if got.Version == nil || *got.Version != 4 {
		t.Fatalf("version = %+v, which is what update and delete depend on", got.Version)
	}

	// No version passed, so Update reads one through Get first.
	updated, err := service.Update(context.Background(), target, "11", "edited", nil)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Anchor == nil || updated.Anchor.Path == nil {
		t.Errorf("update did not repair the anchor path: %+v", updated.Anchor)
	}
}

// TestSetStateRepairsAnAnchoredBlocker is the guard for the defect that
// mattered most.
//
// Resolving an inline blocker is how a reviewer -- or an agent -- closes out
// feedback on a specific line, and it is the action a merge gate reads. The
// generated wrapper decoded straight into RestComment, so it failed on every
// comment that had a line to point at, which is every comment worth blocking
// on. A live run is what found it; this is what keeps it found.
func TestSetStateRepairsAnAnchoredBlocker(t *testing.T) {
	var states []string
	service := newCommentTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			body, _ := io.ReadAll(r.Body)
			var sent map[string]any
			_ = json.Unmarshal(body, &sent)
			state, _ := sent["state"].(string)
			states = append(states, state)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":21,"version":2,"text":"fix this line","severity":"BLOCKER",
			"state":"RESOLVED","anchor":{"line":12,"path":"internal/cli/root.go"}}`))
	})

	target := Target{Repository: RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, PullRequestID: "7"}
	resolved, err := service.SetState(context.Background(), target, "21", CommentStateResolved, nil)
	if err != nil {
		t.Fatalf("resolve an anchored blocker: %v", err)
	}
	if resolved.Anchor == nil || resolved.Anchor.Path == nil || resolved.Anchor.Path.Components == nil {
		t.Fatalf("resolving lost the anchor: %+v", resolved.Anchor)
	}
	if resolved.State == nil || *resolved.State != "RESOLVED" {
		t.Fatalf("state = %+v", resolved.State)
	}
	if len(states) != 1 || states[0] != "RESOLVED" {
		t.Fatalf("sent states = %v, want one RESOLVED", states)
	}

	// Reopening is the same call with the other state, and has to survive the
	// same anchor.
	if _, err := service.SetState(context.Background(), target, "21", CommentStateOpen, nil); err != nil {
		t.Fatalf("reopen an anchored blocker: %v", err)
	}
	if len(states) != 2 || states[1] != "OPEN" {
		t.Fatalf("sent states = %v, want RESOLVED then OPEN", states)
	}

	// Anything else is refused before a request is made: Bitbucket would answer
	// with a generic rejection that names neither the field nor the values.
	if _, err := service.SetState(context.Background(), target, "21", CommentState("PENDING"), nil); err == nil {
		t.Error("an unsupported state was sent to the server")
	}
}

// TestCreateRefusesABlockerOnACommit keeps the two comment worlds apart.
//
// Blocker comments are a pull request concept -- they are posted to a separate
// endpoint and they are what a merge gate counts. A commit has no such thing,
// and saying so here is more use than a 404 from the server.
func TestCreateRefusesABlockerOnACommit(t *testing.T) {
	service := newCommentTestService(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("a request was made for a target that should have been refused: %s", r.URL)
	})

	target := Target{
		Repository: RepositoryRef{ProjectKey: "TEST", Slug: "demo"},
		CommitID:   "abc",
		Blocker:    true,
	}
	_, err := service.Create(context.Background(), target, "a blocker on a commit")
	if err == nil {
		t.Fatal("a blocker on a commit was accepted")
	}
	if !strings.Contains(err.Error(), "only supported for pull requests") {
		t.Errorf("error = %v, want it to say why", err)
	}
}

// TestAnUnreadableVersionLookupStopsTheWrite is the hazard a read with no error
// created.
//
// Get answered an undecodable body with an empty comment and no error, so the
// version lookup behind update, delete and resolve returned nothing, and the
// write went out with no version at all. Bitbucket reads a missing version as
// "no optimistic locking wanted": it applies the change over whatever a
// concurrent edit did and answers success. Two reviewers resolving the same
// thread, and one of them silently loses their text.
//
// Resolving a blocker always takes this path, because resolve passes no
// version of its own.
func TestAnUnreadableVersionLookupStopsTheWrite(t *testing.T) {
	writes := 0
	service := newCommentTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":1,"version":9}`))
			return
		}
		// A truncated body: valid-looking, and not a comment.
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":1,"version":`))
	})

	target := Target{Repository: RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, PullRequestID: "7"}

	if _, err := service.Get(context.Background(), target, "1"); err == nil {
		t.Fatal("a truncated body was reported as a readable comment")
	}
	for _, attempt := range []struct {
		what string
		run  func() error
	}{
		{"resolve", func() error {
			_, err := service.SetState(context.Background(), target, "1", CommentStateResolved, nil)
			return err
		}},
		{"update", func() error {
			_, err := service.Update(context.Background(), target, "1", "edited", nil)
			return err
		}},
		{"delete", func() error {
			_, err := service.Delete(context.Background(), target, "1", nil)
			return err
		}},
	} {
		if err := attempt.run(); err == nil {
			t.Errorf("%s proceeded after an unreadable version lookup", attempt.what)
		}
	}
	if writes != 0 {
		t.Errorf("%d write requests were sent after the lookup failed; want none", writes)
	}
}

// TestAnExplicitVersionSkipsTheLookup pins the other half of resolveVersion.
//
// The lookup exists only for callers that did not supply a version. A refactor
// that made it run anyway would add a request per write and, worse, would
// overwrite the caller's version with the server's current one -- turning an
// optimistic-locking check the caller asked for into a blind write that always
// wins.
func TestAnExplicitVersionSkipsTheLookup(t *testing.T) {
	for _, operation := range []struct {
		what string
		run  func(service *Service, target Target, version int32) error
	}{
		{"update", func(service *Service, target Target, version int32) error {
			_, err := service.Update(context.Background(), target, "1", "edited", &version)
			return err
		}},
		{"resolve", func(service *Service, target Target, version int32) error {
			_, err := service.SetState(context.Background(), target, "1", CommentStateResolved, &version)
			return err
		}},
		{"delete", func(service *Service, target Target, version int32) error {
			_, err := service.Delete(context.Background(), target, "1", &version)
			return err
		}},
	} {
		t.Run(operation.what, func(t *testing.T) {
			reads := 0
			var sentVersions []string
			service := newCommentTestService(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodGet:
					reads++
				case http.MethodDelete:
					sentVersions = append(sentVersions, r.URL.Query().Get("version"))
					w.WriteHeader(http.StatusNoContent)
					return
				default:
					body, _ := io.ReadAll(r.Body)
					var sent map[string]any
					_ = json.Unmarshal(body, &sent)
					sentVersions = append(sentVersions, fmt.Sprintf("%v", sent["version"]))
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":1,"version":4}`))
			})

			target := Target{Repository: RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, PullRequestID: "7"}
			if err := operation.run(service, target, 3); err != nil {
				t.Fatalf("%s with an explicit version: %v", operation.what, err)
			}
			if reads != 0 {
				t.Errorf("%s issued %d read(s) despite being given a version", operation.what, reads)
			}
			// The caller's version, not the server's current one.
			if len(sentVersions) != 1 || sentVersions[0] != "3" {
				t.Errorf("%s sent versions %v, want the caller's 3", operation.what, sentVersions)
			}
		})
	}
}

// TestEveryEmptyReadIsRefused enumerates the bodies a read can come back with,
// rather than the one that happened to be tried.
//
// Refusing malformed JSON was not enough. A body of null or {} unmarshals into
// a struct of nils without complaint, so two of the five shapes still produced
// a silently empty comment -- and an empty comment is what turned a failed read
// into a write with no version. Testing one shape and reasoning about the rest
// is what let that stand.
func TestEveryEmptyReadIsRefused(t *testing.T) {
	for _, shape := range []struct {
		what string
		body string
		want bool
	}{
		{"a comment", `{"id":1,"version":2}`, true},
		{"no body at all", ``, false},
		{"a JSON null", `null`, false},
		{"an object with no fields", `{}`, false},
		{"an array", `[]`, false},
		{"truncated JSON", `{"id":1,"version":`, false},
		{"a comment with no id", `{"version":2,"text":"orphan"}`, false},
	} {
		t.Run(shape.what, func(t *testing.T) {
			service := newCommentTestService(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(shape.body))
			})

			target := Target{Repository: RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, PullRequestID: "7"}
			got, err := service.Get(context.Background(), target, "1")
			if shape.want {
				if err != nil {
					t.Fatalf("a real comment was refused: %v", err)
				}
				if got.Id == nil {
					t.Fatalf("the comment came back without its id: %+v", got)
				}
				return
			}
			if err == nil {
				t.Fatalf("%s was accepted as a comment: %+v", shape.what, got)
			}
		})
	}
}

// TestAStationaryPageEndsTheListing stops a hang.
//
// The paging loop follows nextPageStart, and an instance that answers with one
// that does not advance makes it walk forever, collecting the same page each
// time. A wrong answer is recoverable; a command that never returns is not.
// The browse service already guards this the same way.
func TestAStationaryPageEndsTheListing(t *testing.T) {
	requests := 0
	service := newCommentTestService(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"isLastPage":false,"nextPageStart":0,"values":[{"id":1,"text":"stuck"}]}`))
	})

	target := Target{Repository: RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, PullRequestID: "7"}
	listed, err := service.List(context.Background(), target, "a.go", 25)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if requests != 1 {
		t.Errorf("the listing made %d requests for a page that never advanced; want 1", requests)
	}
	if len(listed) != 1 {
		t.Errorf("listed = %d comments, want the one page that was returned", len(listed))
	}
}

// TestEveryEmptyWriteFallsBackToWhatWasSent is the write half of the same
// enumeration.
//
// A write that cannot read its response must answer with what it sent: the
// change happened, and reporting a failure invites a retry that posts a second
// comment. Refusing only malformed JSON left null and {} decoding cleanly into
// a struct of nils, which slipped past the fallback and published a comment
// with neither an id nor the text just written -- strictly less than the echo
// it skipped.
//
// Reads refuse and writes fall back. That difference is the point: nothing
// happened on a failed read, and something did on a failed write.
func TestEveryEmptyWriteFallsBackToWhatWasSent(t *testing.T) {
	for _, shape := range []struct {
		what string
		body string
	}{
		{"no body at all", ``},
		{"a JSON null", `null`},
		{"an object with no fields", `{}`},
		{"truncated JSON", `{"id":9,"version":`},
	} {
		t.Run(shape.what, func(t *testing.T) {
			service := newCommentTestService(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(shape.body))
			})

			target := Target{Repository: RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, PullRequestID: "7"}
			created, err := service.Create(context.Background(), target, "the text that was written")
			if err != nil {
				t.Fatalf("a write that succeeded was reported as a failure: %v", err)
			}
			if created.Text == nil || *created.Text != "the text that was written" {
				t.Fatalf("%s lost the text that was written: %+v", shape.what, created.Text)
			}
		})
	}

	// A real response still wins over the echo.
	service := newCommentTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":9,"version":3,"text":"as the server stored it"}`))
	})
	target := Target{Repository: RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, PullRequestID: "7"}
	created, err := service.Create(context.Background(), target, "what was sent")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Id == nil || *created.Id != 9 || created.Text == nil || *created.Text != "as the server stored it" {
		t.Fatalf("the echo won over a real response: %+v", created)
	}
}
