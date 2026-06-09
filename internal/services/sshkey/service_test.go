package sshkey

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
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

func TestSshKeyServiceUserKeys(t *testing.T) {
	service := newSshKeyTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/ssh/latest/keys":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":123,"label":"MyKey","text":"ssh-rsa AAA"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/ssh/latest/keys":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":123,"label":"MyKey","text":"ssh-rsa AAA"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/ssh/latest/keys/123":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})

	ctx := context.Background()

	// List
	list, err := service.ListUserKeys(ctx, 10)
	if err != nil || len(list) != 1 || *list[0].Id != 123 {
		t.Fatalf("expected user key list success, got len=%d err=%v", len(list), err)
	}

	// Add
	added, err := service.AddUserKey(ctx, "MyKey", "ssh-rsa AAA")
	if err != nil || *added.Id != 123 {
		t.Fatalf("expected user key add success, got %#v err=%v", added, err)
	}

	// Remove
	if err := service.RemoveUserKey(ctx, "123"); err != nil {
		t.Fatalf("expected user key remove success, got %v", err)
	}
}

func TestSshKeyServiceProjectKeys(t *testing.T) {
	service := newSshKeyTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/keys/latest/projects/PRJ/ssh":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"permission":"PROJECT_READ","key":{"id":456,"label":"ProjKey"}}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/keys/latest/projects/PRJ/ssh":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"permission":"PROJECT_READ","key":{"id":456,"label":"ProjKey"}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/keys/latest/projects/PRJ/ssh/456":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})

	ctx := context.Background()

	// List
	list, err := service.ListProjectKeys(ctx, "PRJ", 10)
	if err != nil || len(list) != 1 || *list[0].Key.Id != 456 {
		t.Fatalf("expected project key list success, got len=%d err=%v", len(list), err)
	}

	// Add
	added, err := service.AddProjectKey(ctx, "PRJ", "ProjKey", "ssh-rsa AAA", "PROJECT_READ")
	if err != nil || *added.Key.Id != 456 {
		t.Fatalf("expected project key add success, got %#v err=%v", added, err)
	}

	// Remove
	if err := service.RemoveProjectKey(ctx, "PRJ", "456"); err != nil {
		t.Fatalf("expected project key remove success, got %v", err)
	}
}

func TestSshKeyServiceRepoKeys(t *testing.T) {
	service := newSshKeyTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/keys/latest/projects/PRJ/repos/repo1/ssh":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"permission":"REPO_WRITE","key":{"id":789,"label":"RepoKey"}}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/keys/latest/projects/PRJ/repos/repo1/ssh":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"permission":"REPO_WRITE","key":{"id":789,"label":"RepoKey"}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/keys/latest/projects/PRJ/repos/repo1/ssh/789":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})

	ctx := context.Background()

	// List
	list, err := service.ListRepoKeys(ctx, "PRJ", "repo1", 10)
	if err != nil || len(list) != 1 || *list[0].Key.Id != 789 {
		t.Fatalf("expected repo key list success, got len=%d err=%v", len(list), err)
	}

	// Add
	added, err := service.AddRepoKey(ctx, "PRJ", "repo1", "RepoKey", "ssh-rsa AAA", "REPO_WRITE")
	if err != nil || *added.Key.Id != 789 {
		t.Fatalf("expected repo key add success, got %#v err=%v", added, err)
	}

	// Remove
	if err := service.RemoveRepoKey(ctx, "PRJ", "repo1", "789"); err != nil {
		t.Fatalf("expected repo key remove success, got %v", err)
	}
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

func TestSshKeyServicePagination(t *testing.T) {
	calls := 0
	service := newSshKeyTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		calls++
		if calls == 1 {
			_, _ = w.Write([]byte(`{"isLastPage":false,"nextPageStart":1,"values":[{"id":123,"label":"Key1"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":456,"label":"Key2"}]}`))
	})

	keys, err := service.ListUserKeys(context.Background(), 10)
	if err != nil || len(keys) != 2 {
		t.Fatalf("expected paginated list, len=%d err=%v", len(keys), err)
	}
}

func TestSshKeyServiceTransientErrors(t *testing.T) {
	service := newSshKeyTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errors":[{"message":"Unauthorized"}]}`))
	})

	ctx := context.Background()

	if _, err := service.ListUserKeys(ctx, 10); err == nil || apperrors.ExitCode(err) != 3 {
		t.Fatalf("expected unauthorized error, got %v", err)
	}
	if _, err := service.AddUserKey(ctx, "label", "ssh-rsa AAA"); err == nil || apperrors.ExitCode(err) != 3 {
		t.Fatalf("expected unauthorized error, got %v", err)
	}
	if err := service.RemoveUserKey(ctx, "123"); err == nil || apperrors.ExitCode(err) != 3 {
		t.Fatalf("expected unauthorized error, got %v", err)
	}
}
