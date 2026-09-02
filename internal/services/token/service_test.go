package token

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
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
	// A closed loopback port rather than an unresolvable hostname. This test
	// makes fifteen calls, and each one against a bogus host waits out a DNS
	// lookup — 40 seconds for the test, most of the package's runtime. It was
	// also the wrong failure: whether a name fails to resolve depends on the
	// resolver, and a network with wildcard DNS would resolve it and change
	// what this test exercises. Connection refused on loopback is immediate
	// and the same everywhere.
	closed := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	baseURL := closed.URL
	closed.Close()

	client, err := openapigenerated.NewClientWithResponses(baseURL + "/rest")
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

// TestTokenCreateRejectsOutOfRangeExpiry covers the bound on --expiry-days.
//
// The API field is 32 bits and int is 64 on every platform bb ships for, so
// before this check the value silently wrapped: --expiry-days 2147483648
// reached the server as a negative expiry.
func TestTokenCreateRejectsOutOfRangeExpiry(t *testing.T) {
	service := NewService(nil)

	_, err := service.Create(context.Background(), ScopeUser, "alice", "ci", nil, math.MaxInt32+1)
	if err == nil {
		t.Fatal("expected an out-of-range expiry to be rejected")
	}
	if !apperrors.IsKind(err, apperrors.KindValidation) {
		t.Fatalf("expected KindValidation, got %v", err)
	}
	if !strings.Contains(err.Error(), "expiry days") {
		t.Fatalf("message does not name the field: %v", err)
	}
}
