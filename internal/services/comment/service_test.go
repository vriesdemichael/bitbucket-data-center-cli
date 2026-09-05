package comment

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
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

// TestServiceCreateExplainsAnchorRejection covers the 400 Bitbucket returns
// when the anchored line is not in the diff. The raw response says nothing
// about the line, the path, or the rule.
// mock-inventory: transport-fault — the rejection is injected to check the message bb builds around it; TestLiveInlineCommentAnchoring proves the real one.
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

// mock-inventory: unreached-guard — every case is refused before a request is built; the handler is never reached.
func TestCountTasksValidatesTarget(t *testing.T) {
	service := newCommentTestService(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	if _, err := service.CountTasks(context.Background(), RepositoryRef{ProjectKey: "", Slug: "demo"}, "12"); err == nil {
		t.Fatalf("expected a validation error for a missing project key")
	}
}

// mock-inventory: unreached-guard — every case is refused before a request is built; the handler is never reached.
func TestSetStateValidation(t *testing.T) {
	service := newCommentTestService(t, testsupport.UnreachedHandler(t))
	target := Target{Repository: RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, PullRequestID: "12"}

	if _, err := service.SetState(context.Background(), target, "  ", CommentStateResolved, nil); err == nil {
		t.Fatal("expected a validation error for an empty comment id")
	}
	if _, err := service.SetState(context.Background(), target, "7", CommentState("DONE"), nil); err == nil {
		t.Fatal("expected a validation error for an unknown state")
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
// mock-inventory: transport-fault — the version read is made to fail on purpose; the subject is that the write is abandoned, not what Bitbucket answers.
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
// mock-inventory: transport-fault — an empty body is injected; the subject is that the service refuses rather than inventing a comment.
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
// mock-inventory: transport-fault — a page whose start never advances is a guard against a server that misbehaves, not a claim that one does.
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
