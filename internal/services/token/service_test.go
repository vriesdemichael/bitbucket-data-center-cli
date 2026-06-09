package token

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

func newTokenTestService(t *testing.T, handler http.HandlerFunc) *Service {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := openapigenerated.NewClientWithResponses(server.URL + "/rest")
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	return NewService(client)
}

func TestTokenServiceCoreCommands(t *testing.T) {
	service := newTokenTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/access-tokens/latest/users/alice":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":"tok-1","name":"UserToken"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/access-tokens/latest/projects/PRJ":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":"tok-2","name":"ProjToken"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/access-tokens/latest/projects/PRJ/repos/repo1":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":"tok-3","name":"RepoToken"}]}`))
		case r.Method == http.MethodPut && r.URL.Path == "/rest/access-tokens/latest/users/alice":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"tok-1","name":"UserToken","token":"secret-123"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/access-tokens/latest/users/alice/tok-1":
			_, _ = w.Write([]byte(`{"id":"tok-1","name":"UserTokenUpdated"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/access-tokens/latest/users/alice/tok-1":
			_, _ = w.Write([]byte(`{"id":"tok-1","name":"UserToken"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/access-tokens/latest/users/alice/tok-1":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})

	ctx := context.Background()

	// List test
	listUser, err := service.List(ctx, ScopeUser, "alice", 10)
	if err != nil || len(listUser) != 1 || *listUser[0].Id != "tok-1" {
		t.Fatalf("expected list user success, got len=%d err=%v", len(listUser), err)
	}

	listProj, err := service.List(ctx, ScopeProject, "PRJ", 10)
	if err != nil || len(listProj) != 1 || *listProj[0].Id != "tok-2" {
		t.Fatalf("expected list proj success, got len=%d err=%v", len(listProj), err)
	}

	listRepo, err := service.List(ctx, ScopeRepo, "PRJ/repo1", 10)
	if err != nil || len(listRepo) != 1 || *listRepo[0].Id != "tok-3" {
		t.Fatalf("expected list repo success, got len=%d err=%v", len(listRepo), err)
	}

	// Create test
	created, err := service.Create(ctx, ScopeUser, "alice", "UserToken", []string{"PROJECT_READ"}, 30)
	if err != nil || *created.Token != "secret-123" {
		t.Fatalf("expected create token success, got %#v err=%v", created, err)
	}

	// Update test
	updated, err := service.Update(ctx, ScopeUser, "alice", "tok-1", "UserTokenUpdated", []string{"PROJECT_READ"})
	if err != nil || *updated.Name != "UserTokenUpdated" {
		t.Fatalf("expected update token success, got %#v err=%v", updated, err)
	}

	// Get test
	get, err := service.Get(ctx, ScopeUser, "alice", "tok-1")
	if err != nil || *get.Name != "UserToken" {
		t.Fatalf("expected get token success, got %#v err=%v", get, err)
	}

	// Revoke test
	if err := service.Revoke(ctx, ScopeUser, "alice", "tok-1"); err != nil {
		t.Fatalf("expected revoke token success, got %v", err)
	}
}

func TestTokenServiceValidation(t *testing.T) {
	service := NewService(nil)
	ctx := context.Background()

	// List validations
	if _, err := service.List(ctx, ScopeUser, "", 10); err == nil {
		t.Fatal("expected user scope validation error")
	}
	if _, err := service.List(ctx, ScopeProject, "", 10); err == nil {
		t.Fatal("expected project scope validation error")
	}
	if _, err := service.List(ctx, ScopeRepo, "PRJ", 10); err == nil || !strings.Contains(err.Error(), "projectKey/repositorySlug") {
		t.Fatal("expected repo scope validation error")
	}
	if _, err := service.List(ctx, ScopeType("invalid"), "abc", 10); err == nil {
		t.Fatal("expected invalid scope validation error")
	}

	// Get validations
	if _, err := service.Get(ctx, ScopeUser, "alice", ""); err == nil {
		t.Fatal("expected token ID validation error")
	}
	if _, err := service.Get(ctx, ScopeUser, "", "tok-1"); err == nil {
		t.Fatal("expected target validation error")
	}

	// Create validations
	if _, err := service.Create(ctx, ScopeUser, "alice", "", nil, 0); err == nil {
		t.Fatal("expected token name validation error")
	}

	// Update validations
	if _, err := service.Update(ctx, ScopeUser, "alice", "", "name", nil); err == nil {
		t.Fatal("expected token ID validation error")
	}

	// Revoke validations
	if err := service.Revoke(ctx, ScopeUser, "alice", ""); err == nil {
		t.Fatal("expected token ID validation error")
	}
}

func TestTokenServicePagination(t *testing.T) {
	calls := 0
	service := newTokenTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		calls++
		if calls == 1 {
			_, _ = w.Write([]byte(`{"isLastPage":false,"nextPageStart":1,"values":[{"id":"tok-1","name":"Token1"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":"tok-2","name":"Token2"}]}`))
	})

	tokens, err := service.List(context.Background(), ScopeProject, "PRJ", 10)
	if err != nil || len(tokens) != 2 {
		t.Fatalf("expected paginated list, len=%d err=%v", len(tokens), err)
	}
}

func TestTokenServiceTransientErrors(t *testing.T) {
	service := newTokenTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"errors":[{"message":"Forbidden"}]}`))
	})

	ctx := context.Background()

	if _, err := service.List(ctx, ScopeUser, "alice", 10); err == nil || apperrors.ExitCode(err) != 3 {
		t.Fatalf("expected forbidden/unauthorized error, got %v", err)
	}
	if _, err := service.Get(ctx, ScopeUser, "alice", "tok-1"); err == nil || apperrors.ExitCode(err) != 3 {
		t.Fatalf("expected forbidden/unauthorized error, got %v", err)
	}
	if _, err := service.Create(ctx, ScopeUser, "alice", "name", nil, 0); err == nil || apperrors.ExitCode(err) != 3 {
		t.Fatalf("expected forbidden/unauthorized error, got %v", err)
	}
	if _, err := service.Update(ctx, ScopeUser, "alice", "tok-1", "name", nil); err == nil || apperrors.ExitCode(err) != 3 {
		t.Fatalf("expected forbidden/unauthorized error, got %v", err)
	}
	if err := service.Revoke(ctx, ScopeUser, "alice", "tok-1"); err == nil || apperrors.ExitCode(err) != 3 {
		t.Fatalf("expected forbidden/unauthorized error, got %v", err)
	}
}
