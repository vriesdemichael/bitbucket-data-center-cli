package commit

import (
	"context"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

func newCommitTestService(t *testing.T, handler http.HandlerFunc) *Service {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := openapigenerated.NewClientWithResponses(server.URL + "/rest")
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	return NewService(client)
}

func TestCommitServiceCoreCommands(t *testing.T) {
	service := newCommitTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/commits":
			if r.URL.Query().Get("since") == "v1.0" && r.URL.Query().Get("until") == "main" && r.URL.Query().Get("merges") == "exclude" {
				_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":"abc","displayId":"abc","message":"init"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":"abc","displayId":"abc","message":"init"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/commits/abc":
			_, _ = w.Write([]byte(`{"id":"abc","displayId":"abc","message":"init"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/compare/commits":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":"def","displayId":"def","message":"feature"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/branches":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":"refs/heads/main","displayId":"main","type":"BRANCH"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/tags":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":"refs/tags/v1.0","displayId":"v1.0","type":"TAG"}]}`))
		default:
			http.NotFound(w, r)
		}
	})

	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

	commits, err := service.List(context.Background(), repo, ListOptions{Limit: 25})
	if err != nil || len(commits) != 1 {
		t.Fatalf("expected commit list success, len=%d err=%v", len(commits), err)
	}

	commitsWithOptions, err := service.List(context.Background(), repo, ListOptions{
		Limit:  25,
		Since:  "v1.0",
		Until:  "main",
		Merges: "exclude",
	})
	if err != nil || len(commitsWithOptions) != 1 {
		t.Fatalf("expected commit list with options success, len=%d err=%v", len(commitsWithOptions), err)
	}

	commit, err := service.Get(context.Background(), repo, "abc")
	if err != nil || commit.Id == nil || *commit.Id != "abc" {
		t.Fatalf("expected commit get success, got %#v err=%v", commit, err)
	}

	compared, err := service.Compare(context.Background(), repo, CompareOptions{From: "abc", To: "def", Limit: 25})
	if err != nil || len(compared) != 1 {
		t.Fatalf("expected commit compare success, len=%d err=%v", len(compared), err)
	}

	refs, err := service.ListTagsAndBranches(context.Background(), repo, "")
	if err != nil || len(refs) != 2 {
		t.Fatalf("expected ref list success, len=%d err=%v", len(refs), err)
	}
}

func TestCommitServiceValidationAndHelpers(t *testing.T) {
	service := newCommitTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden"))
	})

	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

	if _, err := service.Get(context.Background(), repo, ""); err == nil {
		t.Fatal("expected commit get validation error")
	}

	if _, err := service.Compare(context.Background(), repo, CompareOptions{From: "", To: "def"}); err == nil {
		t.Fatal("expected compare from validation error")
	}
	if _, err := service.Compare(context.Background(), repo, CompareOptions{From: "abc", To: ""}); err == nil {
		t.Fatal("expected compare to validation error")
	}

	if _, err := service.List(context.Background(), repo, ListOptions{}); err == nil || !strings.Contains(err.Error(), "authorization") {
		t.Fatalf("expected mapped authorization error, got %v", err)
	}

	invalidRepo := RepositoryRef{ProjectKey: "", Slug: ""}
	if _, err := service.List(context.Background(), invalidRepo, ListOptions{}); err == nil {
		t.Error("expected error for invalid repository")
	}
}

func TestCommitServicePagination(t *testing.T) {
	listCalls := 0
	compareCalls := 0

	service := newCommitTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/commits":
			listCalls++
			if listCalls == 1 {
				_, _ = w.Write([]byte(`{"isLastPage":false,"nextPageStart":1,"values":[{"id":"abc"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":"def"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/compare/commits":
			compareCalls++
			if compareCalls == 1 {
				_, _ = w.Write([]byte(`{"isLastPage":false,"nextPageStart":1,"values":[{"id":"123"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":"456"}]}`))
		default:
			http.NotFound(w, r)
		}
	})

	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

	commits, err := service.List(context.Background(), repo, ListOptions{Limit: 0, Path: "src/main.go"})
	if err != nil || len(commits) != 2 {
		t.Fatalf("expected paginated list, len=%d err=%v", len(commits), err)
	}

	compared, err := service.Compare(context.Background(), repo, CompareOptions{From: "abc", To: "def"})
	if err != nil || len(compared) != 2 {
		t.Fatalf("expected paginated compare, len=%d err=%v", len(compared), err)
	}
}

func TestCommitServiceTransientAndMapping(t *testing.T) {
	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

	transientService := newCommitTestService(t, func(w http.ResponseWriter, r *http.Request) {
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "hijacking not supported", http.StatusInternalServerError)
			return
		}
		connection, _, hijackErr := hijacker.Hijack()
		if hijackErr == nil {
			_ = connection.Close()
		}
	})

	if _, err := transientService.List(context.Background(), repo, ListOptions{}); err == nil || apperrors.ExitCode(err) != 10 {
		t.Fatalf("expected list transient error, got %v", err)
	}
	if _, err := transientService.Get(context.Background(), repo, "abc"); err == nil || apperrors.ExitCode(err) != 10 {
		t.Fatalf("expected get transient error, got %v", err)
	}
	if _, err := transientService.Compare(context.Background(), repo, CompareOptions{From: "abc", To: "def"}); err == nil || apperrors.ExitCode(err) != 10 {
		t.Fatalf("expected compare transient error, got %v", err)
	}
	if _, err := transientService.ListTagsAndBranches(context.Background(), repo, "abc"); err == nil || apperrors.ExitCode(err) != 10 {
		t.Fatalf("expected ref transient error, got %v", err)
	}

	service := newCommitTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/commits":
			_, _ = w.Write([]byte(`{"isLastPage":true}`))
		case r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/commits/abc":
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`not found`))
		case r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/compare/commits":
			_, _ = w.Write([]byte(`{"isLastPage":true}`))
		case strings.HasSuffix(r.URL.Path, "/branches"):
			_, _ = w.Write([]byte(`{"isLastPage":true}`))
		case strings.HasSuffix(r.URL.Path, "/tags"):
			_, _ = w.Write([]byte(`{"isLastPage":true}`))
		default:
			http.NotFound(w, r)
		}
	})

	commits, err := service.List(context.Background(), repo, ListOptions{})
	if err != nil || len(commits) != 0 {
		t.Fatalf("expected empty list success, got %v", err)
	}

	if _, err := service.Get(context.Background(), repo, "abc"); err == nil || apperrors.ExitCode(err) != 4 {
		t.Fatalf("expected not found get error, got %v", err)
	}

	compared, err := service.Compare(context.Background(), repo, CompareOptions{From: "abc", To: "def"})
	if err != nil || len(compared) != 0 {
		t.Fatalf("expected empty compare success, got %v", err)
	}

	refs, err := service.ListTagsAndBranches(context.Background(), repo, "")
	if err != nil || len(refs) != 0 {
		t.Fatalf("expected empty ref success, got %v", err)
	}
}

func TestCommitServiceListTagsAndBranchesErrors(t *testing.T) {
	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

	serviceTagErr := newCommitTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/branches") {
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[]}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/tags") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		http.NotFound(w, r)
	})

	if _, err := serviceTagErr.ListTagsAndBranches(context.Background(), repo, "abc"); err == nil || apperrors.ExitCode(err) != 2 {
		t.Fatalf("expected tag failure to return error, got %v", err)
	}

	serviceBranchErr := newCommitTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/branches") {
			w.WriteHeader(http.StatusTeapot)
			return
		}
		http.NotFound(w, r)
	})

	if _, err := serviceBranchErr.ListTagsAndBranches(context.Background(), repo, "abc"); err == nil || apperrors.ExitCode(err) != 1 {
		t.Fatalf("expected branch failure to return error, got %v", err)
	}

	testMapStatusErrors(t)
}

func testMapStatusErrors(t *testing.T) {
	if err := openapi.MapStatusError(http.StatusBadRequest, nil); err == nil || apperrors.ExitCode(err) != 2 {
		t.Fatalf("expected validation error")
	}
	if err := openapi.MapStatusError(http.StatusUnauthorized, nil); err == nil || apperrors.ExitCode(err) != 3 {
		t.Fatalf("expected auth error")
	}
	if err := openapi.MapStatusError(http.StatusNotFound, nil); err == nil || apperrors.ExitCode(err) != 4 {
		t.Fatalf("expected not found error")
	}
	if err := openapi.MapStatusError(http.StatusConflict, nil); err == nil || apperrors.ExitCode(err) != 5 {
		t.Fatalf("expected conflict error")
	}
	if err := openapi.MapStatusError(http.StatusTooManyRequests, []byte("rate")); err == nil || apperrors.ExitCode(err) != 10 {
		t.Fatalf("expected transient rate error")
	}
	if err := openapi.MapStatusError(http.StatusTeapot, nil); err == nil || apperrors.ExitCode(err) != 1 {
		t.Fatalf("expected permanent error")
	}
}

func TestCommitServicePaginationLimit(t *testing.T) {
	calls := 0
	service := newCommitTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		calls++
		if calls == 1 {
			if r.URL.Query().Get("start") != "1" || r.URL.Query().Get("limit") != "3" {
				t.Errorf("expected start=1 limit=3 on call 1, got start=%s limit=%s", r.URL.Query().Get("start"), r.URL.Query().Get("limit"))
			}
			_, _ = w.Write([]byte(`{"isLastPage":false,"nextPageStart":3,"values":[{"id":"c1"},{"id":"c2"}]}`))
			return
		}
		if r.URL.Query().Get("start") != "3" || r.URL.Query().Get("limit") != "1" {
			t.Errorf("expected start=3 limit=1 on call 2, got start=%s limit=%s", r.URL.Query().Get("start"), r.URL.Query().Get("limit"))
		}
		_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":"c3"}]}`))
	})

	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}
	commits, err := service.List(context.Background(), repo, ListOptions{Start: 1, Limit: 3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(commits) != 3 {
		t.Errorf("expected 3 commits, got %d", len(commits))
	}
	if calls != 2 {
		t.Errorf("expected 2 page requests, got %d", calls)
	}
}

func TestCommitServicePaginationEdgeCases(t *testing.T) {
	service := newCommitTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("start") != "0" {
			t.Errorf("expected start=0, got start=%s", r.URL.Query().Get("start"))
		}
		_, _ = w.Write([]byte(`{"isLastPage":false,"nextPageStart":4,"values":[{"id":"c1"},{"id":"c2"},{"id":"c3"},{"id":"c4"}]}`))
	})

	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}
	commits, err := service.List(context.Background(), repo, ListOptions{Start: -1, Limit: 3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(commits) != 3 {
		t.Errorf("expected 3 commits, got %d", len(commits))
	}
}
