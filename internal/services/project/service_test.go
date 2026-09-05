package project

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

func newProjectTestService(t *testing.T, handler http.HandlerFunc) *Service {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := openapigenerated.NewClientWithResponses(server.URL + "/rest")
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	return NewService(client)
}

func TestProjectServiceValidation(t *testing.T) {
	service := newProjectTestService(t, func(w http.ResponseWriter, r *http.Request) {
		// Every case here is refused before a request is built, so the handler
		// is an assertion rather than a stand-in: reaching it means a guard
		// let something through (ADR-079).
		t.Errorf("validation let a request through: %s %s", r.Method, r.URL.Path)
	})

	if _, err := service.Get(context.Background(), ""); err == nil {
		t.Fatal("expected get key validation error")
	}

	if _, err := service.Create(context.Background(), CreateInput{Key: "", Name: "abc"}); err == nil {
		t.Fatal("expected create key validation error")
	}
	if _, err := service.Create(context.Background(), CreateInput{Key: "abc", Name: ""}); err == nil {
		t.Fatal("expected create name validation error")
	}

	if _, err := service.Update(context.Background(), "", UpdateInput{Name: "abc"}); err == nil {
		t.Fatal("expected update key validation error")
	}

	if err := service.Delete(context.Background(), ""); err == nil {
		t.Fatal("expected delete key validation error")
	}

}

func TestProjectServicePagination(t *testing.T) {
	calls := 0
	service := newProjectTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		calls++
		if calls == 1 {
			_, _ = w.Write([]byte(`{"isLastPage":false,"nextPageStart":1,"values":[{"key":"PRJ1"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"key":"PRJ2"}]}`))
	})

	projects, err := service.List(context.Background(), ListOptions{MaxResults: 0})
	if err != nil || len(projects) != 2 {
		t.Fatalf("expected paginated list, len=%d err=%v", len(projects), err)
	}
}

func TestProjectServiceTransientAndMapping(t *testing.T) {
	transientService := newProjectTestService(t, func(w http.ResponseWriter, r *http.Request) {
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

	if _, err := transientService.List(context.Background(), ListOptions{}); err == nil || apperrors.ExitCode(err) != 10 {
		t.Fatalf("expected list transient error, got %v", err)
	}
	if _, err := transientService.Get(context.Background(), "PRJ"); err == nil || apperrors.ExitCode(err) != 10 {
		t.Fatalf("expected get transient error, got %v", err)
	}
	if _, err := transientService.Create(context.Background(), CreateInput{Key: "P", Name: "N"}); err == nil || apperrors.ExitCode(err) != 10 {
		t.Fatalf("expected create transient error, got %v", err)
	}
	if _, err := transientService.Update(context.Background(), "PRJ", UpdateInput{}); err == nil || apperrors.ExitCode(err) != 10 {
		t.Fatalf("expected update transient error, got %v", err)
	}
	if err := transientService.Delete(context.Background(), "PRJ"); err == nil || apperrors.ExitCode(err) != 10 {
		t.Fatalf("expected delete transient error, got %v", err)
	}

	service := newProjectTestService(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"isLastPage":true}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ":
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("not found"))
		case r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPut:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	})

	list, err := service.List(context.Background(), ListOptions{})
	if err != nil || len(list) != 0 {
		t.Fatalf("expected empty list success, got %v", err)
	}

	if _, err := service.Get(context.Background(), "PRJ"); err == nil || apperrors.ExitCode(err) != 4 {
		t.Fatalf("expected not found get error, got %v", err)
	}

	if created, err := service.Create(context.Background(), CreateInput{Key: "P", Name: "N"}); err != nil || created.Key != nil {
		t.Fatalf("expected empty create success, got %v", err)
	}

	if updated, err := service.Update(context.Background(), "P", UpdateInput{}); err != nil || updated.Key != nil {
		t.Fatalf("expected empty update success, got %v", err)
	}

}

func TestProjectServicePermissionsValidation(t *testing.T) {
	service := NewService(nil)
	if err := service.GrantProjectUserPermission(context.Background(), "", "u", "p"); err == nil {
		t.Fatal("expected error")
	}
	if err := service.GrantProjectUserPermission(context.Background(), "P", "", "p"); err == nil {
		t.Fatal("expected error")
	}
	if err := service.RevokeProjectUserPermission(context.Background(), "", "u"); err == nil {
		t.Fatal("expected error")
	}
	if err := service.RevokeProjectUserPermission(context.Background(), "P", ""); err == nil {
		t.Fatal("expected error")
	}
	if err := service.GrantProjectGroupPermission(context.Background(), "", "g", "p"); err == nil {
		t.Fatal("expected error")
	}
	if err := service.GrantProjectGroupPermission(context.Background(), "P", "", "p"); err == nil {
		t.Fatal("expected error")
	}
	if err := service.RevokeProjectGroupPermission(context.Background(), "", "g"); err == nil {
		t.Fatal("expected error")
	}
	if err := service.RevokeProjectGroupPermission(context.Background(), "P", ""); err == nil {
		t.Fatal("expected error")
	}
	if _, err := normalizeProjectPermission("INVALID"); err == nil {
		t.Fatal("expected error")
	}
}

// The pagination edge case that was here is openapi.PageThrough's, for the same
// reason it is gone from the branch service: an oversized page trimmed to the
// cap is one of the loop's rules, tested where the loop lives.
