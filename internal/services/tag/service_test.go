package tag

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

func newTagTestService(t *testing.T, handler http.HandlerFunc) *Service {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := openapigenerated.NewClientWithResponses(server.URL + "/rest")
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	return NewService(client)
}

func TestTagServiceListCreateGetDelete(t *testing.T) {
	service := newTagTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/tags":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"displayId":"v1.0.0"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/tags":
			_, _ = w.Write([]byte(`{"displayId":"v1.0.1","latestCommit":"abc"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/tags/v1.0.1":
			_, _ = w.Write([]byte(`{"displayId":"v1.0.1"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/git/latest/projects/TEST/repos/demo/tags/v1.0.1":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})

	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

	tags, err := service.List(context.Background(), repo, ListOptions{Limit: 25, OrderBy: "ALPHABETICAL"})
	if err != nil || len(tags) != 1 {
		t.Fatalf("expected tags list, got len=%d err=%v", len(tags), err)
	}

	created, err := service.Create(context.Background(), repo, "v1.0.1", "abc", "release")
	if err != nil || created.DisplayId == nil || *created.DisplayId != "v1.0.1" {
		t.Fatalf("expected created tag, got %#v err=%v", created, err)
	}

	viewed, err := service.Get(context.Background(), repo, "v1.0.1")
	if err != nil || viewed.DisplayId == nil || *viewed.DisplayId != "v1.0.1" {
		t.Fatalf("expected viewed tag, got %#v err=%v", viewed, err)
	}

	if err := service.Delete(context.Background(), repo, "v1.0.1"); err != nil {
		t.Fatalf("expected delete success, got %v", err)
	}
}

func TestTagServiceValidationAndStatusMapping(t *testing.T) {
	service := newTagTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden"))
	})

	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

	_, err := service.Create(context.Background(), repo, "", "abc", "")
	if err == nil || !strings.Contains(err.Error(), "tag name is required") {
		t.Fatalf("expected tag name validation error, got %v", err)
	}

	_, err = service.List(context.Background(), repo, ListOptions{Limit: 25})
	if err == nil || !strings.Contains(err.Error(), "authorization") {
		t.Fatalf("expected mapped authorization error, got %v", err)
	}
}

func TestTagServicePaginationAndFallbackBranches(t *testing.T) {
	service := newTagTestService(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/tags":
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Query().Get("start") == "1" {
				_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"displayId":"v2"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"isLastPage":false,"nextPageStart":1,"values":[{"displayId":"v1"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/tags":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/tags/v2":
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	})

	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

	tags, err := service.List(context.Background(), repo, ListOptions{Limit: 2, FilterText: "v"})
	if err != nil {
		t.Fatalf("expected paginated list success, got: %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("expected 2 tags from pagination, got: %d", len(tags))
	}

	created, err := service.Create(context.Background(), repo, "v2", "abc", "")
	if err != nil {
		t.Fatalf("expected create fallback success, got: %v", err)
	}
	if created.DisplayId != nil {
		t.Fatalf("expected zero-value created tag for empty response body, got: %#v", created)
	}

	viewed, err := service.Get(context.Background(), repo, "v2")
	if err != nil {
		t.Fatalf("expected get fallback success, got: %v", err)
	}
	if viewed.DisplayId != nil {
		t.Fatalf("expected zero-value viewed tag for empty response body, got: %#v", viewed)
	}
}

func TestTagServiceValidationAndMapStatusHelpers(t *testing.T) {
	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}
	service := newTagTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	_, err := service.Create(context.Background(), repo, "name", "", "msg")
	if err == nil {
		t.Fatal("expected start-point validation error")
	}

	_, err = service.Get(context.Background(), repo, " ")
	if err == nil {
		t.Fatal("expected tag name validation error on get")
	}

	err = service.Delete(context.Background(), repo, " ")
	if err == nil {
		t.Fatal("expected tag name validation error on delete")
	}

	if err := openapi.MapStatusError(http.StatusCreated, nil); err != nil {
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

func TestTagServiceTransportAndValidationBranches(t *testing.T) {
	t.Run("repository validation branches", func(t *testing.T) {
		service := newTagTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		if _, err := service.List(context.Background(), RepositoryRef{}, ListOptions{}); err == nil {
			t.Fatal("expected repository validation error on list")
		}
		if _, err := service.Create(context.Background(), RepositoryRef{}, "v1", "abc", ""); err == nil {
			t.Fatal("expected repository validation error on create")
		}
		if _, err := service.Get(context.Background(), RepositoryRef{}, "v1"); err == nil {
			t.Fatal("expected repository validation error on get")
		}
		if err := service.Delete(context.Background(), RepositoryRef{}, "v1"); err == nil {
			t.Fatal("expected repository validation error on delete")
		}
	})

	t.Run("transport failures", func(t *testing.T) {
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
		repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

		if _, err := service.List(context.Background(), repo, ListOptions{}); err == nil || apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient transport error for list, got %v", err)
		}
		if _, err := service.Create(context.Background(), repo, "v1", "abc", "msg"); err == nil || apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient transport error for create, got %v", err)
		}
		if _, err := service.Get(context.Background(), repo, "v1"); err == nil || apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient transport error for get, got %v", err)
		}
		if err := service.Delete(context.Background(), repo, "v1"); err == nil || apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient transport error for delete, got %v", err)
		}
	})

	t.Run("list uses defaults and trims filters", func(t *testing.T) {
		service := newTagTestService(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("limit") != "25" || r.URL.Query().Get("start") != "0" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte("expected default paging values"))
				return
			}
			if r.URL.Query().Get("orderBy") != "ALPHABETICAL" || r.URL.Query().Get("filterText") != "release" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte("expected trimmed order/filter"))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[]}`))
		})

		repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}
		tags, err := service.List(context.Background(), repo, ListOptions{Limit: 0, OrderBy: " ALPHABETICAL ", FilterText: " release "})
		if err != nil {
			t.Fatalf("expected default/trim branch success, got: %v", err)
		}
		if len(tags) != 0 {
			t.Fatalf("expected empty tags list, got: %#v", tags)
		}
	})
}

func TestTagServicePaginationLimit(t *testing.T) {
	calls := 0
	service := newTagTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		calls++
		if calls == 1 {
			if r.URL.Query().Get("start") != "1" || r.URL.Query().Get("limit") != "3" {
				t.Errorf("expected start=1 limit=3 on call 1, got start=%s limit=%s", r.URL.Query().Get("start"), r.URL.Query().Get("limit"))
			}
			_, _ = w.Write([]byte(`{"isLastPage":false,"nextPageStart":3,"values":[{"displayId":"t1"},{"displayId":"t2"}]}`))
			return
		}
		if r.URL.Query().Get("start") != "3" || r.URL.Query().Get("limit") != "1" {
			t.Errorf("expected start=3 limit=1 on call 2, got start=%s limit=%s", r.URL.Query().Get("start"), r.URL.Query().Get("limit"))
		}
		_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"displayId":"t3"}]}`))
	})

	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}
	tags, err := service.List(context.Background(), repo, ListOptions{Limit: 3, Start: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tags) != 3 {
		t.Errorf("expected 3 tags, got %d", len(tags))
	}
	if calls != 2 {
		t.Errorf("expected 2 page requests, got %d", calls)
	}
}

func TestTagServicePaginationEdgeCases(t *testing.T) {
	service := newTagTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("start") != "0" {
			t.Errorf("expected start=0, got start=%s", r.URL.Query().Get("start"))
		}
		_, _ = w.Write([]byte(`{"isLastPage":false,"nextPageStart":4,"values":[{"displayId":"t1"},{"displayId":"t2"},{"displayId":"t3"},{"displayId":"t4"}]}`))
	})

	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}
	tags, err := service.List(context.Background(), repo, ListOptions{Start: -1, Limit: 3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tags) != 3 {
		t.Errorf("expected 3 tags, got %d", len(tags))
	}
}
