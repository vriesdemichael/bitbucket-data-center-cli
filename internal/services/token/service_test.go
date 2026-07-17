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
		case r.Method == http.MethodPut && r.URL.Path == "/rest/access-tokens/latest/projects/PRJ":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"tok-2","name":"ProjToken","token":"secret-456"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/rest/access-tokens/latest/projects/PRJ/repos/repo1":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"tok-3","name":"RepoToken","token":"secret-789"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/access-tokens/latest/users/alice/tok-1":
			_, _ = w.Write([]byte(`{"id":"tok-1","name":"UserTokenUpdated"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/access-tokens/latest/projects/PRJ/tok-2":
			_, _ = w.Write([]byte(`{"id":"tok-2","name":"ProjTokenUpdated"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/access-tokens/latest/projects/PRJ/repos/repo1/tok-3":
			_, _ = w.Write([]byte(`{"id":"tok-3","name":"RepoTokenUpdated"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/access-tokens/latest/users/alice/tok-1":
			_, _ = w.Write([]byte(`{"id":"tok-1","name":"UserToken"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/access-tokens/latest/projects/PRJ/tok-2":
			_, _ = w.Write([]byte(`{"id":"tok-2","name":"ProjToken"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/access-tokens/latest/projects/PRJ/repos/repo1/tok-3":
			_, _ = w.Write([]byte(`{"id":"tok-3","name":"RepoToken"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/access-tokens/latest/users/alice/tok-1":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/access-tokens/latest/projects/PRJ/tok-2":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/access-tokens/latest/projects/PRJ/repos/repo1/tok-3":
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
	createdUser, err := service.Create(ctx, ScopeUser, "alice", "UserToken", []string{"PROJECT_READ"}, 30)
	if err != nil || *createdUser.Token != "secret-123" {
		t.Fatalf("expected create user token success, got %#v err=%v", createdUser, err)
	}

	createdProj, err := service.Create(ctx, ScopeProject, "PRJ", "ProjToken", []string{"PROJECT_READ"}, 30)
	if err != nil || *createdProj.Token != "secret-456" {
		t.Fatalf("expected create proj token success, got %#v err=%v", createdProj, err)
	}

	createdRepo, err := service.Create(ctx, ScopeRepo, "PRJ/repo1", "RepoToken", []string{"REPO_READ"}, 30)
	if err != nil || *createdRepo.Token != "secret-789" {
		t.Fatalf("expected create repo token success, got %#v err=%v", createdRepo, err)
	}

	// Update test
	updatedUser, err := service.Update(ctx, ScopeUser, "alice", "tok-1", "UserTokenUpdated", []string{"PROJECT_READ"})
	if err != nil || *updatedUser.Name != "UserTokenUpdated" {
		t.Fatalf("expected update user token success, got %#v err=%v", updatedUser, err)
	}

	updatedProj, err := service.Update(ctx, ScopeProject, "PRJ", "tok-2", "ProjTokenUpdated", []string{"PROJECT_READ"})
	if err != nil || *updatedProj.Name != "ProjTokenUpdated" {
		t.Fatalf("expected update proj token success, got %#v err=%v", updatedProj, err)
	}

	updatedRepo, err := service.Update(ctx, ScopeRepo, "PRJ/repo1", "tok-3", "RepoTokenUpdated", []string{"REPO_READ"})
	if err != nil || *updatedRepo.Name != "RepoTokenUpdated" {
		t.Fatalf("expected update repo token success, got %#v err=%v", updatedRepo, err)
	}

	// Get test
	getUser, err := service.Get(ctx, ScopeUser, "alice", "tok-1")
	if err != nil || *getUser.Name != "UserToken" {
		t.Fatalf("expected get user token success, got %#v err=%v", getUser, err)
	}

	getProj, err := service.Get(ctx, ScopeProject, "PRJ", "tok-2")
	if err != nil || *getProj.Name != "ProjToken" {
		t.Fatalf("expected get proj token success, got %#v err=%v", getProj, err)
	}

	getRepo, err := service.Get(ctx, ScopeRepo, "PRJ/repo1", "tok-3")
	if err != nil || *getRepo.Name != "RepoToken" {
		t.Fatalf("expected get repo token success, got %#v err=%v", getRepo, err)
	}

	// Revoke test
	if err := service.Revoke(ctx, ScopeUser, "alice", "tok-1"); err != nil {
		t.Fatalf("expected revoke user token success, got %v", err)
	}
	if err := service.Revoke(ctx, ScopeProject, "PRJ", "tok-2"); err != nil {
		t.Fatalf("expected revoke proj token success, got %v", err)
	}
	if err := service.Revoke(ctx, ScopeRepo, "PRJ/repo1", "tok-3"); err != nil {
		t.Fatalf("expected revoke repo token success, got %v", err)
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
	if _, err := service.Get(ctx, ScopeProject, "", "tok-1"); err == nil {
		t.Fatal("expected target validation error")
	}
	if _, err := service.Get(ctx, ScopeRepo, "PRJ", "tok-1"); err == nil {
		t.Fatal("expected target validation error")
	}
	if _, err := service.Get(ctx, ScopeType("invalid"), "abc", "tok-1"); err == nil {
		t.Fatal("expected invalid scope validation error")
	}

	// Create validations
	if _, err := service.Create(ctx, ScopeUser, "alice", "", nil, 0); err == nil {
		t.Fatal("expected token name validation error")
	}
	if _, err := service.Create(ctx, ScopeUser, "", "name", nil, 0); err == nil {
		t.Fatal("expected user validation error")
	}
	if _, err := service.Create(ctx, ScopeProject, "", "name", nil, 0); err == nil {
		t.Fatal("expected project validation error")
	}
	if _, err := service.Create(ctx, ScopeRepo, "PRJ", "name", nil, 0); err == nil {
		t.Fatal("expected repo validation error")
	}
	if _, err := service.Create(ctx, ScopeType("invalid"), "abc", "name", nil, 0); err == nil {
		t.Fatal("expected invalid scope validation error")
	}

	// Update validations
	if _, err := service.Update(ctx, ScopeUser, "alice", "", "name", nil); err == nil {
		t.Fatal("expected token ID validation error")
	}
	if _, err := service.Update(ctx, ScopeUser, "", "tok-1", "name", nil); err == nil {
		t.Fatal("expected user validation error")
	}
	if _, err := service.Update(ctx, ScopeProject, "", "tok-1", "name", nil); err == nil {
		t.Fatal("expected project validation error")
	}
	if _, err := service.Update(ctx, ScopeRepo, "PRJ", "tok-1", "name", nil); err == nil {
		t.Fatal("expected repo validation error")
	}
	if _, err := service.Update(ctx, ScopeType("invalid"), "abc", "tok-1", "name", nil); err == nil {
		t.Fatal("expected invalid scope validation error")
	}

	// Revoke validations
	if err := service.Revoke(ctx, ScopeUser, "alice", ""); err == nil {
		t.Fatal("expected token ID validation error")
	}
	if err := service.Revoke(ctx, ScopeUser, "", "tok-1"); err == nil {
		t.Fatal("expected user validation error")
	}
	if err := service.Revoke(ctx, ScopeProject, "", "tok-1"); err == nil {
		t.Fatal("expected project validation error")
	}
	if err := service.Revoke(ctx, ScopeRepo, "PRJ", "tok-1"); err == nil {
		t.Fatal("expected repo validation error")
	}
	if err := service.Revoke(ctx, ScopeType("invalid"), "abc", "tok-1"); err == nil {
		t.Fatal("expected invalid scope validation error")
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

	// User scope errors
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

	// Project scope errors
	if _, err := service.List(ctx, ScopeProject, "PRJ", 10); err == nil || apperrors.ExitCode(err) != 3 {
		t.Fatalf("expected forbidden/unauthorized error, got %v", err)
	}
	if _, err := service.Get(ctx, ScopeProject, "PRJ", "tok-1"); err == nil || apperrors.ExitCode(err) != 3 {
		t.Fatalf("expected forbidden/unauthorized error, got %v", err)
	}
	if _, err := service.Create(ctx, ScopeProject, "PRJ", "name", nil, 0); err == nil || apperrors.ExitCode(err) != 3 {
		t.Fatalf("expected forbidden/unauthorized error, got %v", err)
	}
	if _, err := service.Update(ctx, ScopeProject, "PRJ", "tok-1", "name", nil); err == nil || apperrors.ExitCode(err) != 3 {
		t.Fatalf("expected forbidden/unauthorized error, got %v", err)
	}
	if err := service.Revoke(ctx, ScopeProject, "PRJ", "tok-1"); err == nil || apperrors.ExitCode(err) != 3 {
		t.Fatalf("expected forbidden/unauthorized error, got %v", err)
	}

	// Repo scope errors
	if _, err := service.List(ctx, ScopeRepo, "PRJ/repo1", 10); err == nil || apperrors.ExitCode(err) != 3 {
		t.Fatalf("expected forbidden/unauthorized error, got %v", err)
	}
	if _, err := service.Get(ctx, ScopeRepo, "PRJ/repo1", "tok-1"); err == nil || apperrors.ExitCode(err) != 3 {
		t.Fatalf("expected forbidden/unauthorized error, got %v", err)
	}
	if _, err := service.Create(ctx, ScopeRepo, "PRJ/repo1", "name", nil, 0); err == nil || apperrors.ExitCode(err) != 3 {
		t.Fatalf("expected forbidden/unauthorized error, got %v", err)
	}
	if _, err := service.Update(ctx, ScopeRepo, "PRJ/repo1", "tok-1", "name", nil); err == nil || apperrors.ExitCode(err) != 3 {
		t.Fatalf("expected forbidden/unauthorized error, got %v", err)
	}
	if err := service.Revoke(ctx, ScopeRepo, "PRJ/repo1", "tok-1"); err == nil || apperrors.ExitCode(err) != 3 {
		t.Fatalf("expected forbidden/unauthorized error, got %v", err)
	}
}

func TestTokenServiceNetworkErrors(t *testing.T) {
	client, err := openapigenerated.NewClientWithResponses("http://invalid.local/rest")
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	service := NewService(client)
	ctx := context.Background()

	// List User, Project, Repo
	if _, err := service.List(ctx, ScopeUser, "alice", 10); err == nil {
		t.Fatal("expected network error")
	}
	if _, err := service.List(ctx, ScopeProject, "PRJ", 10); err == nil {
		t.Fatal("expected network error")
	}
	if _, err := service.List(ctx, ScopeRepo, "PRJ/repo1", 10); err == nil {
		t.Fatal("expected network error")
	}

	// Get User, Project, Repo
	if _, err := service.Get(ctx, ScopeUser, "alice", "tok-1"); err == nil {
		t.Fatal("expected network error")
	}
	if _, err := service.Get(ctx, ScopeProject, "PRJ", "tok-1"); err == nil {
		t.Fatal("expected network error")
	}
	if _, err := service.Get(ctx, ScopeRepo, "PRJ/repo1", "tok-1"); err == nil {
		t.Fatal("expected network error")
	}

	// Create User, Project, Repo
	if _, err := service.Create(ctx, ScopeUser, "alice", "name", nil, 0); err == nil {
		t.Fatal("expected network error")
	}
	if _, err := service.Create(ctx, ScopeProject, "PRJ", "name", nil, 0); err == nil {
		t.Fatal("expected network error")
	}
	if _, err := service.Create(ctx, ScopeRepo, "PRJ/repo1", "name", nil, 0); err == nil {
		t.Fatal("expected network error")
	}

	// Update User, Project, Repo
	if _, err := service.Update(ctx, ScopeUser, "alice", "tok-1", "name", nil); err == nil {
		t.Fatal("expected network error")
	}
	if _, err := service.Update(ctx, ScopeProject, "PRJ", "tok-1", "name", nil); err == nil {
		t.Fatal("expected network error")
	}
	if _, err := service.Update(ctx, ScopeRepo, "PRJ/repo1", "tok-1", "name", nil); err == nil {
		t.Fatal("expected network error")
	}

	// Revoke User, Project, Repo
	if err := service.Revoke(ctx, ScopeUser, "alice", "tok-1"); err == nil {
		t.Fatal("expected network error")
	}
	if err := service.Revoke(ctx, ScopeProject, "PRJ", "tok-1"); err == nil {
		t.Fatal("expected network error")
	}
	if err := service.Revoke(ctx, ScopeRepo, "PRJ/repo1", "tok-1"); err == nil {
		t.Fatal("expected network error")
	}
}
