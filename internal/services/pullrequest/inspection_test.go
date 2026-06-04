package pullrequest

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/transport/httpclient"
)

func newInspectionService(t *testing.T, handler http.HandlerFunc) *Service {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")

	cfg, err := config.LoadFromEnv()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	return NewService(httpclient.NewFromConfig(cfg))
}

var inspectionRepo = RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

func TestListCommitsPaginates(t *testing.T) {
	const base = "/rest/api/latest/projects/TEST/repos/demo/pull-requests/7/commits"
	service := newInspectionService(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != base {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("start") == "0" {
			_, _ = fmt.Fprint(w, `{"values":[{"id":"abc1234567890","displayId":"abc1234","message":"first line\nbody","author":{"displayName":"Ada","emailAddress":"ada@example.com"},"authorTimestamp":111}],"isLastPage":false,"nextPageStart":1}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"values":[{"id":"def","message":"second","author":{"name":"bob"}}],"isLastPage":true,"nextPageStart":1}`)
	})

	commits, err := service.ListCommits(context.Background(), inspectionRepo, "7", PageOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(commits) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(commits))
	}
	if commits[0].DisplayID != "abc1234" || commits[0].Author != "Ada" || commits[0].AuthorEmail != "ada@example.com" {
		t.Fatalf("unexpected first commit: %+v", commits[0])
	}
	if commits[1].Author != "bob" {
		t.Fatalf("expected fallback to author name, got %q", commits[1].Author)
	}
}

func TestListChangesMapsFields(t *testing.T) {
	const base = "/rest/api/latest/projects/TEST/repos/demo/pull-requests/7/changes"
	service := newInspectionService(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != base {
			http.NotFound(w, r)
			return
		}
		_, _ = fmt.Fprint(w, `{"values":[{"path":{"toString":"src/new.go"},"type":"ADD","nodeType":"FILE","executable":true},{"path":{"toString":"dst.go"},"srcPath":{"toString":"src.go"},"type":"MOVE"}],"isLastPage":true,"nextPageStart":0}`)
	})

	changes, err := service.ListChanges(context.Background(), inspectionRepo, "7", PageOptions{Limit: 50})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(changes) != 2 {
		t.Fatalf("expected 2 changes, got %d", len(changes))
	}
	if changes[0].Path != "src/new.go" || changes[0].Type != "ADD" || !changes[0].Executable {
		t.Fatalf("unexpected first change: %+v", changes[0])
	}
	if changes[1].SrcPath != "src.go" || changes[1].Path != "dst.go" {
		t.Fatalf("unexpected move change: %+v", changes[1])
	}
}

func TestGetMergeBase(t *testing.T) {
	const base = "/rest/api/latest/projects/TEST/repos/demo/pull-requests/7/merge-base"
	service := newInspectionService(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != base {
			http.NotFound(w, r)
			return
		}
		_, _ = fmt.Fprint(w, `{"id":"base123","displayId":"base12","message":"ancestor"}`)
	})

	commit, err := service.GetMergeBase(context.Background(), inspectionRepo, "7")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if commit.DisplayID != "base12" || commit.Message != "ancestor" {
		t.Fatalf("unexpected merge base: %+v", commit)
	}
}

func TestInspectionValidatesInput(t *testing.T) {
	service := NewService(nil)

	if _, err := service.ListCommits(context.Background(), RepositoryRef{}, "7", PageOptions{}); err == nil {
		t.Fatal("expected error for missing repository")
	}
	if _, err := service.ListChanges(context.Background(), inspectionRepo, "not-a-number", PageOptions{}); err == nil {
		t.Fatal("expected error for invalid pull request id")
	}
	if _, err := service.ListCommits(context.Background(), inspectionRepo, "7", PageOptions{Start: -1}); err == nil {
		t.Fatal("expected error for negative start")
	}
	if _, err := service.GetMergeBase(context.Background(), inspectionRepo, ""); err == nil {
		t.Fatal("expected error for empty pull request id")
	}
}

// TestInspectionPropagatesTransportErrors covers the error branches that run
// after validation succeeds, when the server returns a non-2xx response.
func TestInspectionPropagatesTransportErrors(t *testing.T) {
	service := newInspectionService(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"errors":[{"message":"boom"}]}`, http.StatusInternalServerError)
	})

	if _, err := service.ListCommits(context.Background(), inspectionRepo, "7", PageOptions{}); err == nil {
		t.Fatal("expected transport error from ListCommits")
	}
	if _, err := service.ListChanges(context.Background(), inspectionRepo, "7", PageOptions{}); err == nil {
		t.Fatal("expected transport error from ListChanges")
	}
	if _, err := service.GetMergeBase(context.Background(), inspectionRepo, "7"); err == nil {
		t.Fatal("expected transport error from GetMergeBase")
	}
}
