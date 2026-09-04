package comment

import (
	"context"
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

func TestCountTasksValidatesTarget(t *testing.T) {
	service := newCommentTestService(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	if _, err := service.CountTasks(context.Background(), RepositoryRef{ProjectKey: "", Slug: "demo"}, "12"); err == nil {
		t.Fatalf("expected a validation error for a missing project key")
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
