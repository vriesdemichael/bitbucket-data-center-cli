package permissionchecker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

func TestPermissionCheckerRepoPermission(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/rest/api/latest/repos" {
			perm := r.URL.Query().Get("permission")
			if perm == "REPO_ADMIN" {
				_, _ = w.Write([]byte(`{"values":[{"slug":"repo1","project":{"key":"PRJ"}}],"isLastPage":true}`))
				return
			}
			_, _ = w.Write([]byte(`{"values":[],"isLastPage":true}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client, err := openapigenerated.NewClientWithResponses(server.URL + "/rest")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	checker := New(client)
	if checker.Client() != client {
		t.Fatalf("expected Client() to return client instance")
	}

	ctx := context.Background()

	// 1. REPO_ADMIN permission is granted
	if err := checker.CheckRepoPermission(ctx, "PRJ", "repo1", openapigenerated.REPOADMIN); err != nil {
		t.Fatalf("expected REPO_ADMIN to be allowed, got error: %v", err)
	}

	// 2. Cached check should return nil without extra network call
	if err := checker.CheckRepoPermission(ctx, "PRJ", "repo1", openapigenerated.REPOADMIN); err != nil {
		t.Fatalf("expected cached REPO_ADMIN to be allowed, got error: %v", err)
	}

	// 3. REPO_WRITE permission not present in returned repos -> returns authorization error
	err = checker.CheckRepoPermission(ctx, "PRJ", "repo1", openapigenerated.REPOWRITE)
	if err == nil {
		t.Fatalf("expected error for ungranted REPO_WRITE permission")
	}
	if !apperrors.IsKind(err, apperrors.KindAuthorization) {
		t.Fatalf("expected authorization error kind, got: %v", apperrors.KindOf(err))
	}
}

func TestPermissionCheckerProjectPermissions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/rest/api/latest/projects/PRJ":
			_, _ = w.Write([]byte(`{"key":"PRJ","name":"Project One"}`))
		case "/rest/api/latest/projects/PRJ_FORBIDDEN":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"errors":[{"message":"Forbidden"}]}`))
		case "/rest/api/latest/projects/PRJ/permissions/users":
			_, _ = w.Write([]byte(`{"values":[{"user":{"name":"admin"}}],"isLastPage":true}`))
		case "/rest/api/latest/projects":
			perm := r.URL.Query().Get("permission")
			if perm == "PROJECT_ADMIN" || perm == "PROJECT_WRITE" || perm == "PROJECT_READ" {
				_, _ = w.Write([]byte(`{"values":[{"key":"PRJ","name":"Project One"}],"isLastPage":true}`))
				return
			}
			_, _ = w.Write([]byte(`{"values":[],"isLastPage":true}`))
		case "/rest/api/latest/repos":
			_, _ = w.Write([]byte(`{"values":[{"slug":"repo1","project":{"key":"PRJ"}}],"isLastPage":true}`))
		case "/rest/api/latest/admin/users":
			_, _ = w.Write([]byte(`{"values":[{"name":"admin"}],"isLastPage":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := openapigenerated.NewClientWithResponses(server.URL + "/rest")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	checker := New(client)
	ctx := context.Background()

	// CheckProjectWrite on valid project
	if err := checker.CheckProjectWrite(ctx, "PRJ"); err != nil {
		t.Fatalf("expected CheckProjectWrite to succeed, got: %v", err)
	}

	// CheckProjectAdmin on valid project
	if err := checker.CheckProjectAdmin(ctx, "PRJ"); err != nil {
		t.Fatalf("expected CheckProjectAdmin to succeed, got: %v", err)
	}

	// CheckProjectRead
	if err := checker.CheckProjectRead(ctx, "PRJ"); err != nil {
		t.Fatalf("expected CheckProjectRead to succeed, got: %v", err)
	}

	// InspectProjectPermissions
	projPerms, err := checker.InspectProjectPermissions(ctx, "PRJ")
	if err != nil {
		t.Fatalf("expected InspectProjectPermissions to succeed: %v", err)
	}
	if !projPerms["PROJECT_READ"] || !projPerms["PROJECT_WRITE"] || !projPerms["PROJECT_ADMIN"] {
		t.Fatalf("expected all project permissions granted, got: %#v", projPerms)
	}

	// InspectRepoPermissions
	repoPerms, err := checker.InspectRepoPermissions(ctx, "PRJ", "repo1")
	if err != nil {
		t.Fatalf("expected InspectRepoPermissions to succeed: %v", err)
	}
	if !repoPerms["REPO_ADMIN"] {
		t.Fatalf("expected REPO_ADMIN granted in inspect, got: %#v", repoPerms)
	}
}
