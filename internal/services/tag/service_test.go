package tag

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
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

	_, err = service.List(context.Background(), repo, ListOptions{MaxResults: 25})
	if err == nil || !strings.Contains(err.Error(), "authorization") {
		t.Fatalf("expected mapped authorization error, got %v", err)
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
		tags, err := service.List(context.Background(), repo, ListOptions{MaxResults: 0, OrderBy: " ALPHABETICAL ", FilterText: " release "})
		if err != nil {
			t.Fatalf("expected default/trim branch success, got: %v", err)
		}
		if len(tags) != 0 {
			t.Fatalf("expected empty tags list, got: %#v", tags)
		}
	})
}
