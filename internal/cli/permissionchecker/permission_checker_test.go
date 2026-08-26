package permissionchecker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

func TestPermissionCheckerRepoPermission(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/rest/api/latest/repos" {
			perm := r.URL.Query().Get("permission")
			start := r.URL.Query().Get("start")
			if perm == "REPO_ADMIN" {
				if start == "" || start == "0" {
					_, _ = w.Write([]byte(`{"values":[{"slug":"other","project":{"key":"PRJ"}}],"isLastPage":false,"nextPageStart":1}`))
					return
				}
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

	// 1. REPO_ADMIN permission is granted on page 2
	if err := checker.CheckRepoPermission(ctx, "PRJ", "repo1", openapi.RepoAdmin); err != nil {
		t.Fatalf("expected REPO_ADMIN to be allowed, got error: %v", err)
	}

	// 2. Cached check should return nil without extra network call
	if err := checker.CheckRepoPermission(ctx, "PRJ", "repo1", openapi.RepoAdmin); err != nil {
		t.Fatalf("expected cached REPO_ADMIN to be allowed, got error: %v", err)
	}

	// 3. REPO_WRITE permission not present in returned repos -> returns authorization error
	err = checker.CheckRepoPermission(ctx, "PRJ", "repo1", openapi.RepoWrite)
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
		case "/rest/api/latest/projects/PRJ_NO_NAME":
			_, _ = w.Write([]byte(`{"key":"PRJ_NO_NAME"}`))
		case "/rest/api/latest/projects/PRJ_MISMATCH":
			_, _ = w.Write([]byte(`{"key":"PRJ_MISMATCH","name":"Mismatch Project"}`))
		case "/rest/api/latest/projects/PRJ_FORBIDDEN_LIST":
			_, _ = w.Write([]byte(`{"key":"PRJ_FORBIDDEN_LIST","name":"Forbidden List"}`))
		case "/rest/api/latest/projects/PRJ_FORBIDDEN":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"errors":[{"message":"Forbidden"}]}`))
		case "/rest/api/latest/projects/PRJ/permissions/users":
			_, _ = w.Write([]byte(`{"values":[{"user":{"name":"admin"}}],"isLastPage":true}`))
		case "/rest/api/latest/projects/PRJ_MISMATCH/permissions/users":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"errors":[{"message":"Forbidden"}]}`))
		case "/rest/api/latest/projects/PRJ_FORBIDDEN/permissions/users":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"errors":[{"message":"Forbidden"}]}`))
		case "/rest/api/latest/projects":
			name := r.URL.Query().Get("name")
			if name == "Forbidden" || name == "Forbidden List" {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"errors":[{"message":"Forbidden"}]}`))
				return
			}
			if name == "Mismatch Project" {
				_, _ = w.Write([]byte(`{"values":[{"key":"OTHER_KEY","name":"Mismatch Project"}],"isLastPage":true}`))
				return
			}
			perm := r.URL.Query().Get("permission")
			if perm == "PROJECT_ADMIN" || perm == "PROJECT_WRITE" || perm == "PROJECT_READ" {
				_, _ = w.Write([]byte(`{"values":[{"key":"PRJ","name":"Project One"}],"isLastPage":true}`))
				return
			}
			_, _ = w.Write([]byte(`{"values":[],"isLastPage":true}`))
		case "/rest/api/latest/repos":
			_, _ = w.Write([]byte(`{"values":[{"slug":"repo1","project":{"key":"PRJ"}}],"isLastPage":true}`))
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
	// Cached
	if err := checker.CheckProjectWrite(ctx, "PRJ"); err != nil {
		t.Fatalf("expected cached CheckProjectWrite to succeed, got: %v", err)
	}

	// CheckProjectWrite on project with no name -> internal error
	if err := checker.CheckProjectWrite(ctx, "PRJ_NO_NAME"); err == nil {
		t.Fatal("expected error on CheckProjectWrite when project has no name")
	}

	// CheckProjectWrite on project where key does not match in list -> auth error
	if err := checker.CheckProjectWrite(ctx, "PRJ_MISMATCH"); err == nil {
		t.Fatal("expected error on CheckProjectWrite for mismatched key")
	}

	// CheckProjectWrite on forbidden project
	if err := checker.CheckProjectWrite(ctx, "PRJ_FORBIDDEN"); err == nil {
		t.Fatal("expected error on CheckProjectWrite for forbidden project")
	}

	// CheckProjectWrite where GetProjects returns 403
	if err := checker.CheckProjectWrite(ctx, "PRJ_FORBIDDEN_LIST"); err == nil {
		t.Fatal("expected error on CheckProjectWrite when project list returns forbidden")
	}

	// CheckProjectAdmin on valid project
	if err := checker.CheckProjectAdmin(ctx, "PRJ"); err != nil {
		t.Fatalf("expected CheckProjectAdmin to succeed, got: %v", err)
	}
	// Cached
	if err := checker.CheckProjectAdmin(ctx, "PRJ"); err != nil {
		t.Fatalf("expected cached CheckProjectAdmin to succeed, got: %v", err)
	}

	// CheckProjectAdmin on forbidden project
	if err := checker.CheckProjectAdmin(ctx, "PRJ_FORBIDDEN"); err == nil {
		t.Fatal("expected error on CheckProjectAdmin for forbidden project")
	}

	// CheckProjectRead
	if err := checker.CheckProjectRead(ctx, "PRJ"); err != nil {
		t.Fatalf("expected CheckProjectRead to succeed, got: %v", err)
	}
	// Cached
	if err := checker.CheckProjectRead(ctx, "PRJ"); err != nil {
		t.Fatalf("expected cached CheckProjectRead to succeed, got: %v", err)
	}

	// CheckProjectRead on project with no name
	if err := checker.CheckProjectRead(ctx, "PRJ_NO_NAME"); err == nil {
		t.Fatal("expected error on CheckProjectRead when project has no name")
	}

	// CheckProjectRead on project where key does not match in list
	if err := checker.CheckProjectRead(ctx, "PRJ_MISMATCH"); err == nil {
		t.Fatal("expected error on CheckProjectRead for mismatched key")
	}

	// CheckProjectRead where GetProjects returns 403
	if err := checker.CheckProjectRead(ctx, "PRJ_FORBIDDEN_LIST"); err == nil {
		t.Fatal("expected error on CheckProjectRead when project list returns forbidden")
	}

	// CheckProjectRead on forbidden project
	if err := checker.CheckProjectRead(ctx, "PRJ_FORBIDDEN"); err == nil {
		t.Fatal("expected error on CheckProjectRead for forbidden project")
	}

	// InspectProjectPermissions on valid project
	projPerms, err := checker.InspectProjectPermissions(ctx, "PRJ")
	if err != nil {
		t.Fatalf("expected InspectProjectPermissions to succeed: %v", err)
	}
	if !projPerms["PROJECT_READ"] || !projPerms["PROJECT_WRITE"] || !projPerms["PROJECT_ADMIN"] {
		t.Fatalf("expected all project permissions granted, got: %#v", projPerms)
	}

	// InspectProjectPermissions on unauthorized project (mismatch)
	mismatchPerms, err := checker.InspectProjectPermissions(ctx, "PRJ_MISMATCH")
	if err != nil {
		t.Fatalf("expected InspectProjectPermissions on mismatch to succeed: %v", err)
	}
	if mismatchPerms["PROJECT_READ"] || mismatchPerms["PROJECT_WRITE"] || mismatchPerms["PROJECT_ADMIN"] {
		t.Fatalf("expected all project permissions false for mismatch, got: %#v", mismatchPerms)
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

func TestPermissionCheckerInspect500Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"errors":[{"message":"Internal Server Error"}]}`))
	}))
	defer server.Close()

	client, err := openapigenerated.NewClientWithResponses(server.URL + "/rest")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	checker := New(client)
	ctx := context.Background()

	// InspectRepoPermissions returns error on 500
	if _, err := checker.InspectRepoPermissions(ctx, "PRJ", "repo1"); err == nil {
		t.Fatal("expected error from InspectRepoPermissions on 500")
	}

	// InspectProjectPermissions returns error on 500
	if _, err := checker.InspectProjectPermissions(ctx, "PRJ"); err == nil {
		t.Fatal("expected error from InspectProjectPermissions on 500")
	}
}

func TestPermissionCheckerCheckProjectCreate(t *testing.T) {
	statusCode := http.StatusBadRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/rest/api/latest/projects" && r.Method == http.MethodPost {
			w.WriteHeader(statusCode)
			if statusCode == http.StatusBadRequest {
				_, _ = w.Write([]byte(`{"errors":[{"message":"Project key is required"}]}`))
			} else if statusCode == http.StatusForbidden {
				_, _ = w.Write([]byte(`{"errors":[{"message":"Forbidden"}]}`))
			} else {
				_, _ = w.Write([]byte(`{"errors":[{"message":"Internal error"}]}`))
			}
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client, err := openapigenerated.NewClientWithResponses(server.URL + "/rest")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()

	// 1. Status 400 Bad Request indicates create endpoint is accessible (permission granted)
	statusCode = http.StatusBadRequest
	checker := New(client)
	if err := checker.CheckProjectCreate(ctx); err != nil {
		t.Fatalf("expected CheckProjectCreate to succeed on 400, got: %v", err)
	}
	// Cached
	if err := checker.CheckProjectCreate(ctx); err != nil {
		t.Fatalf("expected cached CheckProjectCreate to succeed, got: %v", err)
	}

	// 2. Status 403 Forbidden indicates permission denied
	statusCode = http.StatusForbidden
	checker = New(client)
	if err := checker.CheckProjectCreate(ctx); err == nil {
		t.Fatal("expected error on 403 for CheckProjectCreate")
	}

	// 3. Status 500 indicates unexpected error
	statusCode = http.StatusInternalServerError
	checker = New(client)
	if err := checker.CheckProjectCreate(ctx); err == nil {
		t.Fatal("expected error on 500 for CheckProjectCreate")
	}
}
