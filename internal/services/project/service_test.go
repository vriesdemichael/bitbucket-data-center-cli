package project

import (
	"context"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
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

func TestProjectServiceCoreCommands(t *testing.T) {
	service := newProjectTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"key":"PRJ","name":"Project"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ":
			_, _ = w.Write([]byte(`{"key":"PRJ","name":"Project"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/latest/projects":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"key":"PRJ2","name":"Project 2"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/rest/api/latest/projects/PRJ":
			_, _ = w.Write([]byte(`{"key":"PRJ","name":"Project Updated"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/api/latest/projects/PRJ":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})

	list, err := service.List(context.Background(), ListOptions{Limit: 25, Name: "Project"})
	if err != nil || len(list) != 1 {
		t.Fatalf("expected list success, len=%d err=%v", len(list), err)
	}

	get, err := service.Get(context.Background(), "PRJ")
	if err != nil || get.Key == nil || *get.Key != "PRJ" {
		t.Fatalf("expected get success, got %#v err=%v", get, err)
	}

	created, err := service.Create(context.Background(), CreateInput{Key: "PRJ2", Name: "Project 2", Description: "desc"})
	if err != nil || created.Key == nil || *created.Key != "PRJ2" {
		t.Fatalf("expected create success, got %#v err=%v", created, err)
	}

	updated, err := service.Update(context.Background(), "PRJ", UpdateInput{Name: "Project Updated", Description: "desc"})
	if err != nil || updated.Name == nil || *updated.Name != "Project Updated" {
		t.Fatalf("expected update success, got %#v err=%v", updated, err)
	}

	if err := service.Delete(context.Background(), "PRJ"); err != nil {
		t.Fatalf("expected delete success, got %v", err)
	}
}

func TestProjectServiceValidation(t *testing.T) {
	service := newProjectTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden"))
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

	if _, err := service.List(context.Background(), ListOptions{}); err == nil || !strings.Contains(err.Error(), "authorization") {
		t.Fatalf("expected mapped authorization error, got %v", err)
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

	projects, err := service.List(context.Background(), ListOptions{Limit: 0})
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

func TestProjectServicePermissions(t *testing.T) {
	service := newProjectTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/permissions/users":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"user":{"name":"alice"},"permission":"PROJECT_ADMIN"}]}`))
		case r.Method == http.MethodPut && r.URL.Path == "/rest/api/latest/projects/PRJ/permissions/users":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/api/latest/projects/PRJ/permissions/users":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/permissions/groups":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"group":{"name":"admins"},"permission":"PROJECT_ADMIN"}]}`))
		case r.Method == http.MethodPut && r.URL.Path == "/rest/api/latest/projects/PRJ/permissions/groups":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/api/latest/projects/PRJ/permissions/groups":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})

	users, err := service.ListProjectPermissionUsers(context.Background(), "PRJ", 100)
	if err != nil || len(users) != 1 || users[0].Name != "alice" {
		t.Fatalf("list users failed: %v", err)
	}

	if err := service.GrantProjectUserPermission(context.Background(), "PRJ", "alice", "PROJECT_WRITE"); err != nil {
		t.Fatalf("grant user failed: %v", err)
	}

	if err := service.RevokeProjectUserPermission(context.Background(), "PRJ", "alice"); err != nil {
		t.Fatalf("revoke user failed: %v", err)
	}

	groups, err := service.ListProjectPermissionGroups(context.Background(), "PRJ", 100)
	if err != nil || len(groups) != 1 || groups[0].Name != "admins" {
		t.Fatalf("list groups failed: %v", err)
	}

	if err := service.GrantProjectGroupPermission(context.Background(), "PRJ", "admins", "PROJECT_WRITE"); err != nil {
		t.Fatalf("grant group failed: %v", err)
	}

	if err := service.RevokeProjectGroupPermission(context.Background(), "PRJ", "admins"); err != nil {
		t.Fatalf("revoke group failed: %v", err)
	}
}

func TestProjectServicePermissionsAdditional(t *testing.T) {
	var userCount atomic.Int32
	var groupCount atomic.Int32
	service := newProjectTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/permissions/users":
			if userCount.Add(1) == 1 {
				_, _ = w.Write([]byte(`{"isLastPage":false,"nextPageStart":1,"values":[{"user":{"name":"alice"},"permission":"PROJECT_ADMIN"}]}`))
			} else {
				_, _ = w.Write([]byte(`{"isLastPage":true,"values":[]}`))
			}
		case r.URL.Path == "/rest/api/latest/projects/PRJ/permissions/groups":
			if groupCount.Add(1) == 1 {
				_, _ = w.Write([]byte(`{"isLastPage":false,"nextPageStart":1,"values":[{"group":{"name":"g1"},"permission":"PROJECT_READ"}]}`))
			} else {
				_, _ = w.Write([]byte(`{"isLastPage":true,"values":[]}`))
			}
		default:
			http.NotFound(w, r)
		}
	})

	// Test pagination branches
	_, _ = service.ListProjectPermissionUsers(context.Background(), "PRJ", 1)
	_, _ = service.ListProjectPermissionGroups(context.Background(), "PRJ", 1)
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

func TestProjectServicePermissionsMapStatusError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client, _ := openapigenerated.NewClientWithResponses(server.URL)
	service := NewService(client)

	if _, err := service.ListProjectPermissionUsers(context.Background(), "PRJ", 1); err == nil {
		t.Fatal("expected error")
	}
	if err := service.GrantProjectUserPermission(context.Background(), "PRJ", "u", "PROJECT_READ"); err == nil {
		t.Fatal("expected error")
	}
	if err := service.RevokeProjectUserPermission(context.Background(), "PRJ", "u"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.ListProjectPermissionGroups(context.Background(), "PRJ", 1); err == nil {
		t.Fatal("expected error")
	}
	if err := service.GrantProjectGroupPermission(context.Background(), "PRJ", "g", "PROJECT_READ"); err == nil {
		t.Fatal("expected error")
	}
	if err := service.RevokeProjectGroupPermission(context.Background(), "PRJ", "g"); err == nil {
		t.Fatal("expected error")
	}
}

func TestProjectServicePaginationLimit(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		calls++
		if calls == 1 {
			if r.URL.Query().Get("start") != "1" || r.URL.Query().Get("limit") != "3" {
				t.Errorf("expected start=1 limit=3 on call 1, got start=%s limit=%s", r.URL.Query().Get("start"), r.URL.Query().Get("limit"))
			}
			_, _ = w.Write([]byte(`{"isLastPage":false,"nextPageStart":3,"values":[{"key":"P1"},{"key":"P2"}]}`))
			return
		}
		if r.URL.Query().Get("start") != "3" || r.URL.Query().Get("limit") != "1" {
			t.Errorf("expected start=3 limit=1 on call 2, got start=%s limit=%s", r.URL.Query().Get("start"), r.URL.Query().Get("limit"))
		}
		_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"key":"P3"}]}`))
	}))
	defer server.Close()

	client, _ := openapigenerated.NewClientWithResponses(server.URL + "/rest")
	service := NewService(client)

	projects, err := service.List(context.Background(), ListOptions{Limit: 3, Start: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(projects) != 3 {
		t.Errorf("expected 3 projects, got %d", len(projects))
	}
	if calls != 2 {
		t.Errorf("expected 2 page requests, got %d", calls)
	}
}

func TestProjectServicePaginationEdgeCases(t *testing.T) {
	service := newProjectTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("start") != "0" {
			t.Errorf("expected start=0, got start=%s", r.URL.Query().Get("start"))
		}
		_, _ = w.Write([]byte(`{"isLastPage":false,"nextPageStart":4,"values":[{"key":"P1"},{"key":"P2"},{"key":"P3"},{"key":"P4"}]}`))
	})

	projects, err := service.List(context.Background(), ListOptions{Start: -1, Limit: 3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(projects) != 3 {
		t.Errorf("expected 3 projects, got %d", len(projects))
	}
}
