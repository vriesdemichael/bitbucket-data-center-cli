package commit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
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
