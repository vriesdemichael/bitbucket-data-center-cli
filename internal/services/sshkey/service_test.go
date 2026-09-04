package sshkey

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

func newSshKeyTestService(t *testing.T, handler http.HandlerFunc) *Service {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := openapigenerated.NewClientWithResponses(server.URL + "/rest")
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	return NewService(client)
}

func TestSshKeyServiceValidation(t *testing.T) {
	service := NewService(nil)
	ctx := context.Background()

	// User key validations
	if _, err := service.AddUserKey(ctx, "label", ""); err == nil {
		t.Fatal("expected user key text validation error")
	}
	if err := service.RemoveUserKey(ctx, ""); err == nil {
		t.Fatal("expected user key ID validation error")
	}

	// Project key validations
	if _, err := service.ListProjectKeys(ctx, "", 10); err == nil {
		t.Fatal("expected project key projectKey validation error")
	}
	if _, err := service.AddProjectKey(ctx, "", "label", "ssh-rsa AAA", "PROJECT_READ"); err == nil {
		t.Fatal("expected project key projectKey validation error")
	}
	if _, err := service.AddProjectKey(ctx, "PRJ", "label", "", "PROJECT_READ"); err == nil {
		t.Fatal("expected project key text validation error")
	}
	if _, err := service.AddProjectKey(ctx, "PRJ", "label", "ssh-rsa AAA", "INVALID"); err == nil {
		t.Fatal("expected project key permission validation error")
	}
	if err := service.RemoveProjectKey(ctx, "", "456"); err == nil {
		t.Fatal("expected project key projectKey validation error")
	}
	if err := service.RemoveProjectKey(ctx, "PRJ", ""); err == nil {
		t.Fatal("expected project key ID validation error")
	}

	// Repo key validations
	if _, err := service.ListRepoKeys(ctx, "", "repo", 10); err == nil {
		t.Fatal("expected repo key projectKey validation error")
	}
	if _, err := service.ListRepoKeys(ctx, "PRJ", "", 10); err == nil {
		t.Fatal("expected repo key repoSlug validation error")
	}
	if _, err := service.AddRepoKey(ctx, "", "repo1", "label", "ssh-rsa AAA", "REPO_READ"); err == nil {
		t.Fatal("expected repo key projectKey validation error")
	}
	if _, err := service.AddRepoKey(ctx, "PRJ", "", "label", "ssh-rsa AAA", "REPO_READ"); err == nil {
		t.Fatal("expected repo key repoSlug validation error")
	}
	if _, err := service.AddRepoKey(ctx, "PRJ", "repo1", "label", "", "REPO_READ"); err == nil {
		t.Fatal("expected repo key text validation error")
	}
	if _, err := service.AddRepoKey(ctx, "PRJ", "repo1", "label", "ssh-rsa AAA", "INVALID"); err == nil {
		t.Fatal("expected repo key permission validation error")
	}
	if err := service.RemoveRepoKey(ctx, "", "repo1", "789"); err == nil {
		t.Fatal("expected repo key projectKey validation error")
	}
	if err := service.RemoveRepoKey(ctx, "PRJ", "", "789"); err == nil {
		t.Fatal("expected repo key repoSlug validation error")
	}
	if err := service.RemoveRepoKey(ctx, "PRJ", "repo1", ""); err == nil {
		t.Fatal("expected repo key ID validation error")
	}
}

// mock-inventory: transport-fault — the subject is this loop's arithmetic -- that start advances and the limit narrows to what is left. Bitbucket's side of the convention is pinned live by branches and tags; seeding thirty keys to re-prove it here would buy nothing.
func TestSshKeyServicePagination(t *testing.T) {
	calls := 0
	service := newSshKeyTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		calls++
		if calls == 1 {
			if r.URL.Query().Get("start") != "1" || r.URL.Query().Get("limit") != "3" {
				t.Errorf("expected start=1 limit=3 on call 1, got start=%s limit=%s", r.URL.Query().Get("start"), r.URL.Query().Get("limit"))
			}
			_, _ = w.Write([]byte(`{"isLastPage":false,"nextPageStart":3,"values":[{"id":123,"label":"Key1"},{"id":124,"label":"Key2"}]}`))
			return
		}
		if r.URL.Query().Get("start") != "3" || r.URL.Query().Get("limit") != "1" {
			t.Errorf("expected start=3 limit=1 on call 2, got start=%s limit=%s", r.URL.Query().Get("start"), r.URL.Query().Get("limit"))
		}
		_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":125,"label":"Key3"}]}`))
	})

	keys, err := service.ListUserKeys(context.Background(), 3, 1)
	if err != nil || len(keys) != 3 {
		t.Fatalf("expected paginated list of 3 elements, len=%d err=%v", len(keys), err)
	}
	if calls != 2 {
		t.Errorf("expected 2 page requests, got %d", calls)
	}
}

// mock-inventory: transport-fault — the subject is this loop's arithmetic -- that start advances and the limit narrows to what is left. Bitbucket's side of the convention is pinned live by branches and tags; seeding thirty keys to re-prove it here would buy nothing.
func TestSshKeyServicePaginationEdgeCases(t *testing.T) {
	service := newSshKeyTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("start") != "0" {
			t.Errorf("expected start=0, got start=%s", r.URL.Query().Get("start"))
		}
		_, _ = w.Write([]byte(`{"isLastPage":false,"nextPageStart":4,"values":[{"id":123,"label":"Key1"},{"id":124,"label":"Key2"},{"id":125,"label":"Key3"},{"id":126,"label":"Key4"}]}`))
	})

	keys, err := service.ListUserKeys(context.Background(), 3, -1)
	if err != nil || len(keys) != 3 {
		t.Fatalf("expected paginated list of 3 elements, len=%d err=%v", len(keys), err)
	}
}

// mock-inventory: transport-fault — the failure is injected below the API; no live server refuses on request.
func TestSshKeyServiceTransientErrors(t *testing.T) {
	service := newSshKeyTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errors":[{"message":"Unauthorized"}]}`))
	})

	ctx := context.Background()

	if _, err := service.ListUserKeys(ctx, 10, 0); err == nil || apperrors.ExitCode(err) != 3 {
		t.Fatalf("expected unauthorized error, got %v", err)
	}
	if _, err := service.AddUserKey(ctx, "label", "ssh-rsa AAA"); err == nil || apperrors.ExitCode(err) != 3 {
		t.Fatalf("expected unauthorized error, got %v", err)
	}
	if err := service.RemoveUserKey(ctx, "123"); err == nil || apperrors.ExitCode(err) != 3 {
		t.Fatalf("expected unauthorized error, got %v", err)
	}

	if _, err := service.ListProjectKeys(ctx, "PRJ", 10); err == nil || apperrors.ExitCode(err) != 3 {
		t.Fatalf("expected unauthorized error, got %v", err)
	}
	if _, err := service.AddProjectKey(ctx, "PRJ", "label", "ssh-rsa AAA", "PROJECT_READ"); err == nil || apperrors.ExitCode(err) != 3 {
		t.Fatalf("expected unauthorized error, got %v", err)
	}
	if err := service.RemoveProjectKey(ctx, "PRJ", "123"); err == nil || apperrors.ExitCode(err) != 3 {
		t.Fatalf("expected unauthorized error, got %v", err)
	}

	if _, err := service.ListRepoKeys(ctx, "PRJ", "repo", 10); err == nil || apperrors.ExitCode(err) != 3 {
		t.Fatalf("expected unauthorized error, got %v", err)
	}
	if _, err := service.AddRepoKey(ctx, "PRJ", "repo", "label", "ssh-rsa AAA", "REPO_READ"); err == nil || apperrors.ExitCode(err) != 3 {
		t.Fatalf("expected unauthorized error, got %v", err)
	}
	if err := service.RemoveRepoKey(ctx, "PRJ", "repo", "123"); err == nil || apperrors.ExitCode(err) != 3 {
		t.Fatalf("expected unauthorized error, got %v", err)
	}
}

// mock-inventory: transport-fault — the failure is injected below the API; no live server refuses on request.
func TestSshKeyServiceNetworkErrors(t *testing.T) {
	// Closed loopback port, not an unresolvable hostname: see the note on
	// TestTokenServiceNetworkErrors. A DNS lookup per call made this 25
	// seconds, and left the failure mode at the mercy of the resolver.
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
	if _, err := service.ListUserKeys(ctx, 10, 0); err == nil {
		t.Fatal("expected network error")
	}
	if _, err := service.ListProjectKeys(ctx, "PRJ", 10); err == nil {
		t.Fatal("expected network error")
	}
	if _, err := service.ListRepoKeys(ctx, "PRJ", "repo", 10); err == nil {
		t.Fatal("expected network error")
	}

	// Add User, Project, Repo
	if _, err := service.AddUserKey(ctx, "label", "ssh-rsa AAA"); err == nil {
		t.Fatal("expected network error")
	}
	if _, err := service.AddProjectKey(ctx, "PRJ", "label", "ssh-rsa AAA", "PROJECT_READ"); err == nil {
		t.Fatal("expected network error")
	}
	if _, err := service.AddRepoKey(ctx, "PRJ", "repo", "label", "ssh-rsa AAA", "REPO_READ"); err == nil {
		t.Fatal("expected network error")
	}

	// Remove User, Project, Repo
	if err := service.RemoveUserKey(ctx, "123"); err == nil {
		t.Fatal("expected network error")
	}
	if err := service.RemoveProjectKey(ctx, "PRJ", "123"); err == nil {
		t.Fatal("expected network error")
	}
	if err := service.RemoveRepoKey(ctx, "PRJ", "repo", "123"); err == nil {
		t.Fatal("expected network error")
	}
}
