package reposettings

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

func TestListRepositoryPermissionUsers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/latest/projects/PRJ/repos/demo/permissions/users" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
		_, _ = writer.Write([]byte(`{"values":[{"permission":"REPO_ADMIN","user":{"name":"admin","displayName":"Admin User"}}],"isLastPage":true}`))
	}))
	defer server.Close()

	client, err := openapigenerated.NewClientWithResponses(server.URL)
	if err != nil {
		t.Fatalf("create generated client: %v", err)
	}

	service := NewService(client)
	users, err := service.ListRepositoryPermissionUsers(context.Background(), RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}, 10)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(users) != 1 || users[0].Name != "admin" || users[0].Permission != "REPO_ADMIN" {
		t.Fatalf("unexpected users payload: %#v", users)
	}
}

func TestListRepositoryWebhooks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/latest/projects/PRJ/repos/demo/webhooks" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
		_, _ = writer.Write([]byte(`{"values":[{"name":"ci-hook"}],"size":1}`))
	}))
	defer server.Close()

	client, err := openapigenerated.NewClientWithResponses(server.URL)
	if err != nil {
		t.Fatalf("create generated client: %v", err)
	}

	service := NewService(client)
	webhooks, err := service.ListRepositoryWebhooks(context.Background(), RepositoryRef{ProjectKey: "PRJ", Slug: "demo"})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if webhooks.Count != 1 {
		t.Fatalf("expected webhook count 1, got %d", webhooks.Count)
	}
}

func TestGetRepositoryPullRequestSettings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/latest/projects/PRJ/repos/demo/settings/pull-requests" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
		_, _ = writer.Write([]byte(`{"requiredAllTasksComplete":true}`))
	}))
	defer server.Close()

	client, err := openapigenerated.NewClientWithResponses(server.URL)
	if err != nil {
		t.Fatalf("create generated client: %v", err)
	}

	service := NewService(client)
	settings, err := service.GetRepositoryPullRequestSettings(context.Background(), RepositoryRef{ProjectKey: "PRJ", Slug: "demo"})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if requiredTasks, ok := settings["requiredAllTasksComplete"].(bool); !ok || !requiredTasks {
		t.Fatalf("expected requiredAllTasksComplete=true, got %#v", settings["requiredAllTasksComplete"])
	}
}

func TestPermissionsNotFoundMapsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusNotFound)
		_, _ = writer.Write([]byte("missing"))
	}))
	defer server.Close()

	client, err := openapigenerated.NewClientWithResponses(server.URL)
	if err != nil {
		t.Fatalf("create generated client: %v", err)
	}

	service := NewService(client)
	_, err = service.ListRepositoryPermissionUsers(context.Background(), RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}, 10)
	if err == nil {
		t.Fatal("expected error")
	}
	if apperrors.ExitCode(err) != 4 {
		t.Fatalf("expected not found exit code 4, got %d (%v)", apperrors.ExitCode(err), err)
	}
}

func TestGrantRepositoryUserPermission(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPut || request.URL.Path != "/api/latest/projects/PRJ/repos/demo/permissions/users" {
			http.NotFound(writer, request)
			return
		}
		if request.URL.Query().Get("permission") != "REPO_WRITE" || request.URL.Query().Get("name") != "alice" {
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte("invalid query"))
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client, err := openapigenerated.NewClientWithResponses(server.URL)
	if err != nil {
		t.Fatalf("create generated client: %v", err)
	}

	service := NewService(client)
	if err := service.GrantRepositoryUserPermission(context.Background(), RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}, "alice", "repo_write"); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestCreateRepositoryWebhook(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/api/latest/projects/PRJ/repos/demo/webhooks" {
			http.NotFound(writer, request)
			return
		}
		body, _ := io.ReadAll(request.Body)
		if !strings.Contains(string(body), "\"name\":\"ci\"") || !strings.Contains(string(body), "\"url\":\"http://example.local/hook\"") {
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte("invalid body"))
			return
		}
		writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
		_, _ = writer.Write([]byte(`{"name":"ci","url":"http://example.local/hook"}`))
	}))
	defer server.Close()

	client, err := openapigenerated.NewClientWithResponses(server.URL)
	if err != nil {
		t.Fatalf("create generated client: %v", err)
	}

	service := NewService(client)
	payload, err := service.CreateRepositoryWebhook(context.Background(), RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}, WebhookCreateInput{
		Name:   "ci",
		URL:    "http://example.local/hook",
		Events: []string{"repo:refs_changed"},
		Active: true,
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if payload == nil {
		t.Fatal("expected created webhook payload")
	}
}

func TestUpdateRepositoryPullRequestRequiredAllTasks(t *testing.T) {
	hitUpdate := false
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPost && request.URL.Path == "/api/latest/projects/PRJ/repos/demo/settings/pull-requests" {
			hitUpdate = true
			body, _ := io.ReadAll(request.Body)
			if !strings.Contains(string(body), `"requiredAllTasksComplete":true`) {
				writer.WriteHeader(http.StatusBadRequest)
				_, _ = writer.Write([]byte("missing requiredAllTasksComplete=true"))
				return
			}
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"requiredAllTasksComplete":true}`))
			return
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()

	client, err := openapigenerated.NewClientWithResponses(server.URL)
	if err != nil {
		t.Fatalf("create generated client: %v", err)
	}

	service := NewService(client)
	settings, err := service.UpdateRepositoryPullRequestRequiredAllTasks(context.Background(), RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}, true)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !hitUpdate {
		t.Fatal("expected update call to be issued")
	}
	if value, ok := settings["requiredAllTasksComplete"].(bool); !ok || !value {
		t.Fatalf("expected requiredAllTasksComplete=true, got %#v", settings["requiredAllTasksComplete"])
	}
}

func TestDeleteRepositoryWebhook(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodDelete || request.URL.Path != "/api/latest/projects/PRJ/repos/demo/webhooks/42" {
			http.NotFound(writer, request)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client, err := openapigenerated.NewClientWithResponses(server.URL)
	if err != nil {
		t.Fatalf("create generated client: %v", err)
	}

	service := NewService(client)
	if err := service.DeleteRepositoryWebhook(context.Background(), RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}, "42"); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestUpdateRepositoryPullRequestRequiredApproversCount(t *testing.T) {
	objectPayloadCalls := 0
	integerPayloadCalls := 0

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/api/latest/projects/PRJ/repos/demo/settings/pull-requests" {
			http.NotFound(writer, request)
			return
		}
		body, _ := io.ReadAll(request.Body)
		if strings.Contains(string(body), `"requiredApprovers":{"count":2,"enabled":true}`) {
			objectPayloadCalls++
			// Simulate older Bitbucket that rejects object with 400
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte(`{"errors":[{"message":"invalid payload"}]}`))
			return
		}
		if strings.Contains(string(body), `"requiredApprovers":2`) {
			integerPayloadCalls++
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			_, _ = writer.Write([]byte(`{"requiredApprovers":2}`))
			return
		}
		writer.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client, err := openapigenerated.NewClientWithResponses(server.URL)
	if err != nil {
		t.Fatalf("create generated client: %v", err)
	}

	service := NewService(client)
	settings, err := service.UpdateRepositoryPullRequestRequiredApproversCount(context.Background(), RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}, 2)
	if err != nil {
		t.Fatalf("expected no error with fallback, got: %v", err)
	}

	if objectPayloadCalls != 1 {
		t.Errorf("expected 1 call with object payload, got %d", objectPayloadCalls)
	}
	if integerPayloadCalls != 1 {
		t.Errorf("expected 1 call with integer payload (fallback), got %d", integerPayloadCalls)
	}

	if value, ok := settings["requiredApprovers"].(float64); !ok || int(value) != 2 {
		t.Fatalf("expected required approvers count 2, got %#v", settings["requiredApprovers"])
	}
}

func TestRepositorySettingsHelperCoverage(t *testing.T) {
	permission, err := normalizeRepositoryPermission(" repo_read ")
	if err != nil || string(permission) != "REPO_READ" {
		t.Fatalf("expected REPO_READ normalization, got permission=%q err=%v", permission, err)
	}

	_, err = normalizeRepositoryPermission("invalid")
	if err == nil {
		t.Fatal("expected validation error for invalid permission")
	}

	if err := openapi.MapStatusError(http.StatusCreated, nil); err != nil {
		t.Fatalf("expected nil for success status, got: %v", err)
	}

	tests := []struct {
		status   int
		exitCode int
	}{
		{status: http.StatusBadRequest, exitCode: 2},
		{status: http.StatusUnauthorized, exitCode: 3},
		{status: http.StatusForbidden, exitCode: 3},
		{status: http.StatusNotFound, exitCode: 4},
		{status: http.StatusConflict, exitCode: 5},
		{status: http.StatusTooManyRequests, exitCode: 10},
		{status: http.StatusInternalServerError, exitCode: 10},
		{status: http.StatusNotAcceptable, exitCode: 1},
	}

	for _, testCase := range tests {
		err := openapi.MapStatusError(testCase.status, []byte("err"))
		if err == nil {
			t.Fatalf("expected error for status %d", testCase.status)
		}
		if apperrors.ExitCode(err) != testCase.exitCode {
			t.Fatalf("expected exit code %d for status %d, got %d", testCase.exitCode, testCase.status, apperrors.ExitCode(err))
		}
	}
}

func TestRepositorySettingsJSONFallbackAndValidationBranches(t *testing.T) {
	service := newServiceWithBaseURL(t, func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/api/latest/projects/PRJ/repos/demo/webhooks":
			_, _ = writer.Write([]byte(`[1,2]`))
		case request.Method == http.MethodPost && request.URL.Path == "/api/latest/projects/PRJ/repos/demo/webhooks":
			writer.WriteHeader(http.StatusCreated)
			_, _ = writer.Write([]byte("created"))
		case request.Method == http.MethodPost && request.URL.Path == "/api/latest/projects/PRJ/repos/demo/settings/pull-requests":
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte("updated"))
		default:
			http.NotFound(writer, request)
		}
	})

	repo := RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}

	webhooks, err := service.ListRepositoryWebhooks(context.Background(), repo)
	if err != nil {
		t.Fatalf("expected no error listing array webhooks payload, got: %v", err)
	}
	if webhooks.Count != 2 {
		t.Fatalf("expected webhook count=2 from array payload, got: %d", webhooks.Count)
	}

	created, err := service.CreateRepositoryWebhook(context.Background(), repo, WebhookCreateInput{Name: "ci", URL: "http://example.local/hook"})
	if err != nil {
		t.Fatalf("expected no error creating webhook with non-json response, got: %v", err)
	}
	if created != nil {
		t.Fatalf("expected nil payload for non-json create response, got: %#v", created)
	}

	allTasksSettings, err := service.UpdateRepositoryPullRequestRequiredAllTasks(context.Background(), repo, true)
	if err != nil {
		t.Fatalf("expected no error updating all tasks with fallback response, got: %v", err)
	}
	if value, ok := allTasksSettings["requiredAllTasksComplete"].(bool); !ok || !value {
		t.Fatalf("expected fallback requiredAllTasksComplete=true, got: %#v", allTasksSettings)
	}

	approverSettings, err := service.UpdateRepositoryPullRequestRequiredApproversCount(context.Background(), repo, 3)
	if err != nil {
		t.Fatalf("expected no error updating approvers with fallback response, got: %v", err)
	}
	// With the new logic, the returned map will have the object structure if the first attempt (object) succeeded in the mock
	approvers, ok := approverSettings["requiredApprovers"].(map[string]any)
	if !ok {
		t.Fatalf("expected requiredApprovers object, got: %#v", approverSettings["requiredApprovers"])
	}
	countValue := float64(0)
	switch v := approvers["count"].(type) {
	case float64:
		countValue = v
	case int:
		countValue = float64(v)
	default:
		t.Fatalf("unexpected type for count: %T", approvers["count"])
	}
	if countValue != 3 {
		t.Fatalf("expected fallback requiredApprovers object with count 3, got: %v", countValue)
	}

	_, err = service.UpdateRepositoryPullRequestRequiredApproversCount(context.Background(), repo, -1)
	if err == nil {
		t.Fatal("expected validation error for negative approvers count")
	}

	if err := service.DeleteRepositoryWebhook(context.Background(), repo, " "); err == nil {
		t.Fatal("expected validation error for empty webhook id")
	}
}

func newServiceWithBaseURL(t *testing.T, handler http.HandlerFunc) *Service {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := openapigenerated.NewClientWithResponses(server.URL)
	if err != nil {
		t.Fatalf("create generated client: %v", err)
	}

	return NewService(client)
}

func TestRepositorySettingsAdditionalBranches(t *testing.T) {
	t.Run("permission users pagination and defaults", func(t *testing.T) {
		service := newServiceWithBaseURL(t, func(writer http.ResponseWriter, request *http.Request) {
			if request.Method != http.MethodGet || request.URL.Path != "/api/latest/projects/PRJ/repos/demo/permissions/users" {
				http.NotFound(writer, request)
				return
			}
			writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
			if request.URL.Query().Get("limit") != "100" {
				writer.WriteHeader(http.StatusBadRequest)
				_, _ = writer.Write([]byte("expected default limit=100"))
				return
			}
			if request.URL.Query().Get("start") == "1" {
				_, _ = writer.Write([]byte(`{"isLastPage":true,"values":[{"permission":"REPO_WRITE","user":{"name":"bob"}}]}`))
				return
			}
			_, _ = writer.Write([]byte(`{"isLastPage":false,"nextPageStart":1,"values":[{"permission":"REPO_READ","user":{"name":"alice","displayName":"Alice"}}]}`))
		})

		users, err := service.ListRepositoryPermissionUsers(context.Background(), RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}, 0)
		if err != nil {
			t.Fatalf("expected paginated permission users success, got: %v", err)
		}
		if len(users) != 2 {
			t.Fatalf("expected 2 users from pagination, got: %d", len(users))
		}
	})

	t.Run("webhooks invalid json and transport", func(t *testing.T) {
		invalidService := newServiceWithBaseURL(t, func(writer http.ResponseWriter, request *http.Request) {
			_, _ = writer.Write([]byte("not-json"))
		})
		if _, err := invalidService.ListRepositoryWebhooks(context.Background(), RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}); err == nil {
			t.Fatal("expected invalid json payload error")
		}

		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.WriteHeader(http.StatusOK)
		}))
		baseURL := server.URL
		server.Close()

		client, err := openapigenerated.NewClientWithResponses(baseURL)
		if err != nil {
			t.Fatalf("create generated client: %v", err)
		}
		transportService := NewService(client)
		if _, err := transportService.ListRepositoryWebhooks(context.Background(), RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}); err == nil || apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient transport error, got: %v", err)
		}
	})

	t.Run("permission and webhook validations", func(t *testing.T) {
		service := newServiceWithBaseURL(t, func(writer http.ResponseWriter, request *http.Request) {
			writer.WriteHeader(http.StatusNoContent)
		})

		if err := service.GrantRepositoryUserPermission(context.Background(), RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}, " ", "REPO_READ"); err == nil {
			t.Fatal("expected username validation error")
		}
		if _, err := service.CreateRepositoryWebhook(context.Background(), RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}, WebhookCreateInput{Name: "", URL: "http://example.local"}); err == nil {
			t.Fatal("expected webhook name validation error")
		}
		if _, err := service.CreateRepositoryWebhook(context.Background(), RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}, WebhookCreateInput{Name: "ci", URL: ""}); err == nil {
			t.Fatal("expected webhook url validation error")
		}
	})

	t.Run("pull request settings decode and status branches", func(t *testing.T) {
		decodeService := newServiceWithBaseURL(t, func(writer http.ResponseWriter, request *http.Request) {
			switch {
			case request.Method == http.MethodGet && request.URL.Path == "/api/latest/projects/PRJ/repos/demo/settings/pull-requests":
				_, _ = writer.Write([]byte(`[]`))
			case request.Method == http.MethodPost && request.URL.Path == "/api/latest/projects/PRJ/repos/demo/settings/pull-requests":
				_, _ = writer.Write([]byte(`[]`))
			default:
				http.NotFound(writer, request)
			}
		})

		if _, err := decodeService.GetRepositoryPullRequestSettings(context.Background(), RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}); err == nil {
			t.Fatal("expected decode error for pull request settings map")
		}
		if _, err := decodeService.UpdateRepositoryPullRequestRequiredAllTasks(context.Background(), RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}, true); err == nil {
			t.Fatal("expected decode error for all tasks update map")
		}
		if _, err := decodeService.UpdateRepositoryPullRequestRequiredApproversCount(context.Background(), RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}, 2); err == nil {
			t.Fatal("expected error for approvers update map")
		}

		statusService := newServiceWithBaseURL(t, func(writer http.ResponseWriter, request *http.Request) {
			writer.WriteHeader(http.StatusUnauthorized)
			_, _ = writer.Write([]byte("unauthorized"))
		})
		if _, err := statusService.GetRepositoryPullRequestSettings(context.Background(), RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}); err == nil || apperrors.ExitCode(err) != 3 {
			t.Fatalf("expected auth mapping, got: %v", err)
		}
	})

	t.Run("validate repository ref branch", func(t *testing.T) {
		service := newServiceWithBaseURL(t, func(writer http.ResponseWriter, request *http.Request) {
			writer.WriteHeader(http.StatusOK)
		})
		if _, err := service.ListRepositoryPermissionUsers(context.Background(), RepositoryRef{}, 10); err == nil {
			t.Fatal("expected repository validation error")
		}
	})
}

func TestRepositoryServicePermissionsAndChecks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json;charset=UTF-8")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/latest/projects/PRJ/repos/demo/permissions/groups":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"group":{"name":"admins"},"permission":"REPO_ADMIN"}]}`))
		case r.Method == http.MethodPut && r.URL.Path == "/api/latest/projects/PRJ/repos/demo/permissions/groups":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/latest/projects/PRJ/repos/demo/permissions/groups":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/latest/projects/PRJ/repos/demo/permissions/users":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/required-builds/latest/projects/PRJ/repos/demo/conditions":
			_, _ = w.Write([]byte(`{"values":[{"id":1,"buildParentKeys":["plan1"]}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, _ := openapigenerated.NewClientWithResponses(server.URL)
	service := NewService(client)
	repo := RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}

	groups, err := service.ListRepositoryPermissionGroups(context.Background(), repo, 100)
	if err != nil || len(groups) != 1 || groups[0].Name != "admins" {
		t.Fatalf("list groups failed: %v", err)
	}

	if err := service.GrantRepositoryGroupPermission(context.Background(), repo, "admins", "REPO_WRITE"); err != nil {
		t.Fatalf("grant group failed: %v", err)
	}

	if err := service.RevokeRepositoryGroupPermission(context.Background(), repo, "admins"); err != nil {
		t.Fatalf("revoke group failed: %v", err)
	}

	if err := service.RevokeRepositoryUserPermission(context.Background(), repo, "alice"); err != nil {
		t.Fatalf("revoke user failed: %v", err)
	}

	checks, err := service.ListRequiredBuildsMergeChecks(context.Background(), repo)
	if err != nil {
		t.Fatalf("list checks failed: %v", err)
	}
	if m, ok := checks.(map[string]any); !ok || len(m["values"].([]any)) != 1 {
		t.Fatalf("unexpected checks payload: %#v", checks)
	}
}

func TestRepositoryServicePermissionsValidationAdditional(t *testing.T) {
	service := NewService(nil)
	repo := RepositoryRef{ProjectKey: "P", Slug: "S"}
	if err := service.GrantRepositoryGroupPermission(context.Background(), RepositoryRef{}, "g", "p"); err == nil {
		t.Fatal("expected error")
	}
	if err := service.GrantRepositoryGroupPermission(context.Background(), repo, "", "p"); err == nil {
		t.Fatal("expected error")
	}
	if err := service.RevokeRepositoryGroupPermission(context.Background(), RepositoryRef{}, "g"); err == nil {
		t.Fatal("expected error")
	}
	if err := service.RevokeRepositoryGroupPermission(context.Background(), repo, ""); err == nil {
		t.Fatal("expected error")
	}
	if err := service.RevokeRepositoryUserPermission(context.Background(), RepositoryRef{}, "u"); err == nil {
		t.Fatal("expected error")
	}
	if err := service.RevokeRepositoryUserPermission(context.Background(), repo, ""); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.ListRequiredBuildsMergeChecks(context.Background(), RepositoryRef{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestNewRepositoryServiceValidationErrors(t *testing.T) {
	service := NewService(nil)
	repo := RepositoryRef{ProjectKey: "P", Slug: "S"}
	emptyRepo := RepositoryRef{}
	ctx := context.Background()

	// RepositoryRef validation check on all new methods
	if _, err := service.GetRepositoryAutoMergeSettings(ctx, emptyRepo); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.UpdateRepositoryAutoMergeSettings(ctx, emptyRepo, true); err == nil {
		t.Fatal("expected error")
	}
	if err := service.DeleteRepositoryAutoMergeSettings(ctx, emptyRepo); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.GetRepositoryAutoDeclineSettings(ctx, emptyRepo); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.UpdateRepositoryAutoDeclineSettings(ctx, emptyRepo, true, 4); err == nil {
		t.Fatal("expected error")
	}
	if err := service.DeleteRepositoryAutoDeclineSettings(ctx, emptyRepo); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.ListRepositoryLabels(ctx, emptyRepo); err == nil {
		t.Fatal("expected error")
	}
	if err := service.AddRepositoryLabel(ctx, emptyRepo, "label"); err == nil {
		t.Fatal("expected error")
	}
	if err := service.RemoveRepositoryLabel(ctx, emptyRepo, "label"); err == nil {
		t.Fatal("expected error")
	}
	if err := service.WatchRepository(ctx, emptyRepo); err == nil {
		t.Fatal("expected error")
	}
	if err := service.UnwatchRepository(ctx, emptyRepo); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.ListDefaultTasks(ctx, emptyRepo); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.AddDefaultTask(ctx, emptyRepo, "desc", nil, nil); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.UpdateDefaultTask(ctx, emptyRepo, "123", "desc", nil, nil); err == nil {
		t.Fatal("expected error")
	}
	if err := service.DeleteDefaultTask(ctx, emptyRepo, "123"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.GetWebhook(ctx, emptyRepo, "1"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.UpdateWebhook(ctx, emptyRepo, "1", "name", "url", nil, nil); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.SearchWebhooks(ctx, emptyRepo, nil); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.TestWebhook(ctx, emptyRepo, "1"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.GetWebhookLatestInvocation(ctx, emptyRepo, "1", nil, nil); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.GetWebhookStatistics(ctx, emptyRepo, "1"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.GetWebhookStatisticsSummary(ctx, emptyRepo, "1"); err == nil {
		t.Fatal("expected error")
	}

	// Parameter validations
	if err := service.AddRepositoryLabel(ctx, repo, ""); err == nil {
		t.Fatal("expected error")
	}
	if err := service.RemoveRepositoryLabel(ctx, repo, " "); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.AddDefaultTask(ctx, repo, "", nil, nil); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.UpdateDefaultTask(ctx, repo, "", "desc", nil, nil); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.UpdateDefaultTask(ctx, repo, "123", "", nil, nil); err == nil {
		t.Fatal("expected error")
	}
	if err := service.DeleteDefaultTask(ctx, repo, " "); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.GetWebhook(ctx, repo, ""); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.UpdateWebhook(ctx, repo, "", "name", "url", nil, nil); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.TestWebhook(ctx, repo, ""); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.TestWebhook(ctx, repo, "not-an-int"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.GetWebhookLatestInvocation(ctx, repo, "", nil, nil); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.GetWebhookStatistics(ctx, repo, ""); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.GetWebhookStatisticsSummary(ctx, repo, ""); err == nil {
		t.Fatal("expected error")
	}
}

func TestRepositoryServiceAdditionalCoverage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Return 200 with empty body to hit !json.Valid branches
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, _ := openapigenerated.NewClientWithResponses(server.URL)
	service := NewService(client)
	repo := RepositoryRef{ProjectKey: "P", Slug: "S"}

	_, _ = service.ListRepositoryWebhooks(context.Background(), repo)
	_, _ = service.CreateRepositoryWebhook(context.Background(), repo, WebhookCreateInput{Name: "n", URL: "u"})
}

func TestRepositoryServiceUpdateNilBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// No body
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, _ := openapigenerated.NewClientWithResponses(server.URL)
	service := NewService(client)
	repo := RepositoryRef{ProjectKey: "P", Slug: "S"}

	_, _ = service.UpdateRepositoryPullRequestSettings(context.Background(), repo, map[string]any{"a": 1})
}

func TestRepositoryServiceErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client, _ := openapigenerated.NewClientWithResponses(server.URL)
	service := NewService(client)
	repo := RepositoryRef{ProjectKey: "P", Slug: "S"}
	ctx := context.Background()

	if _, err := service.ListRepositoryPermissionUsers(ctx, repo, 100); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.ListRepositoryPermissionGroups(ctx, repo, 100); err == nil {
		t.Fatal("expected error")
	}
	if err := service.GrantRepositoryUserPermission(ctx, repo, "u", "REPO_READ"); err == nil {
		t.Fatal("expected error")
	}
	if err := service.GrantRepositoryGroupPermission(ctx, repo, "g", "REPO_READ"); err == nil {
		t.Fatal("expected error")
	}
	if err := service.RevokeRepositoryUserPermission(ctx, repo, "u"); err == nil {
		t.Fatal("expected error")
	}
	if err := service.RevokeRepositoryGroupPermission(ctx, repo, "g"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.ListRepositoryWebhooks(ctx, repo); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.CreateRepositoryWebhook(ctx, repo, WebhookCreateInput{Name: "n", URL: "u"}); err == nil {
		t.Fatal("expected error")
	}
	if err := service.DeleteRepositoryWebhook(ctx, repo, "1"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.GetRepositoryPullRequestSettings(ctx, repo); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.UpdateRepositoryPullRequestSettings(ctx, repo, map[string]any{}); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.ListRequiredBuildsMergeChecks(ctx, repo); err == nil {
		t.Fatal("expected error")
	}

	// Auto-merge
	if _, err := service.GetRepositoryAutoMergeSettings(ctx, repo); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.UpdateRepositoryAutoMergeSettings(ctx, repo, true); err == nil {
		t.Fatal("expected error")
	}
	if err := service.DeleteRepositoryAutoMergeSettings(ctx, repo); err == nil {
		t.Fatal("expected error")
	}

	// Auto-decline
	if _, err := service.GetRepositoryAutoDeclineSettings(ctx, repo); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.UpdateRepositoryAutoDeclineSettings(ctx, repo, true, 4); err == nil {
		t.Fatal("expected error")
	}
	if err := service.DeleteRepositoryAutoDeclineSettings(ctx, repo); err == nil {
		t.Fatal("expected error")
	}

	// Labels
	if _, err := service.ListRepositoryLabels(ctx, repo); err == nil {
		t.Fatal("expected error")
	}
	if err := service.AddRepositoryLabel(ctx, repo, "label"); err == nil {
		t.Fatal("expected error")
	}
	if err := service.RemoveRepositoryLabel(ctx, repo, "label"); err == nil {
		t.Fatal("expected error")
	}

	// Watch
	if err := service.WatchRepository(ctx, repo); err == nil {
		t.Fatal("expected error")
	}
	if err := service.UnwatchRepository(ctx, repo); err == nil {
		t.Fatal("expected error")
	}

	// Default tasks
	if _, err := service.ListDefaultTasks(ctx, repo); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.AddDefaultTask(ctx, repo, "desc", nil, nil); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.UpdateDefaultTask(ctx, repo, "123", "desc", nil, nil); err == nil {
		t.Fatal("expected error")
	}
	if err := service.DeleteDefaultTask(ctx, repo, "123"); err == nil {
		t.Fatal("expected error")
	}

	// Webhook
	if _, err := service.GetWebhook(ctx, repo, "1"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.UpdateWebhook(ctx, repo, "1", "name", "url", nil, nil); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.SearchWebhooks(ctx, repo, nil); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.TestWebhook(ctx, repo, "1"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.GetWebhookLatestInvocation(ctx, repo, "1", nil, nil); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.GetWebhookStatistics(ctx, repo, "1"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.GetWebhookStatisticsSummary(ctx, repo, "1"); err == nil {
		t.Fatal("expected error")
	}
}

func TestNewRepositorySettingsMethods(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json;charset=UTF-8")
		
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/api/latest/projects/PRJ/repos/demo/settings/auto-merge":
			_, _ = writer.Write([]byte(`{"enabled":true}`))
		case request.Method == http.MethodPut && request.URL.Path == "/api/latest/projects/PRJ/repos/demo/settings/auto-merge":
			_, _ = writer.Write([]byte(`{"enabled":true}`))
		case request.Method == http.MethodDelete && request.URL.Path == "/api/latest/projects/PRJ/repos/demo/settings/auto-merge":
			writer.WriteHeader(http.StatusNoContent)
		case request.Method == http.MethodGet && request.URL.Path == "/api/latest/projects/PRJ/repos/demo/settings/auto-decline":
			_, _ = writer.Write([]byte(`{"enabled":true,"inactivityWeeks":4}`))
		case request.Method == http.MethodPut && request.URL.Path == "/api/latest/projects/PRJ/repos/demo/settings/auto-decline":
			_, _ = writer.Write([]byte(`{"enabled":true,"inactivityWeeks":4}`))
		case request.Method == http.MethodDelete && request.URL.Path == "/api/latest/projects/PRJ/repos/demo/settings/auto-decline":
			writer.WriteHeader(http.StatusNoContent)
		case request.Method == http.MethodGet && request.URL.Path == "/api/latest/projects/PRJ/repos/demo/labels":
			_, _ = writer.Write([]byte(`{"values":[{"name":"label1"},{"name":"label2"}]}`))
		case request.Method == http.MethodPost && request.URL.Path == "/api/latest/projects/PRJ/repos/demo/labels":
			writer.WriteHeader(http.StatusNoContent)
		case request.Method == http.MethodDelete && request.URL.Path == "/api/latest/projects/PRJ/repos/demo/labels/label1":
			writer.WriteHeader(http.StatusNoContent)
		case request.Method == http.MethodPost && request.URL.Path == "/api/latest/projects/PRJ/repos/demo/watch":
			writer.WriteHeader(http.StatusNoContent)
		case request.Method == http.MethodDelete && request.URL.Path == "/api/latest/projects/PRJ/repos/demo/watch":
			writer.WriteHeader(http.StatusNoContent)
		case request.Method == http.MethodGet && request.URL.Path == "/default-tasks/latest/projects/PRJ/repos/demo/tasks":
			_, _ = writer.Write([]byte(`{"values":[{"id":123,"description":"task1"}]}`))
		case request.Method == http.MethodPost && request.URL.Path == "/default-tasks/latest/projects/PRJ/repos/demo/tasks":
			_, _ = writer.Write([]byte(`{"id":123,"description":"task1"}`))
		case request.Method == http.MethodPut && request.URL.Path == "/default-tasks/latest/projects/PRJ/repos/demo/tasks/123":
			_, _ = writer.Write([]byte(`{"id":123,"description":"task1-updated"}`))
		case request.Method == http.MethodDelete && request.URL.Path == "/default-tasks/latest/projects/PRJ/repos/demo/tasks/123":
			writer.WriteHeader(http.StatusNoContent)
		case request.Method == http.MethodGet && request.URL.Path == "/api/latest/projects/PRJ/repos/demo/webhooks/1":
			_, _ = writer.Write([]byte(`{"id":1,"name":"hook1"}`))
		case request.Method == http.MethodPut && request.URL.Path == "/api/latest/projects/PRJ/repos/demo/webhooks/1":
			_, _ = writer.Write([]byte(`{"id":1,"name":"hook1-updated"}`))
		case request.Method == http.MethodGet && request.URL.Path == "/api/latest/projects/PRJ/repos/demo/webhooks/search":
			_, _ = writer.Write([]byte(`{"values":[{"id":1}]}`))
		case request.Method == http.MethodPost && request.URL.Path == "/api/latest/projects/PRJ/repos/demo/webhooks/test":
			_, _ = writer.Write([]byte(`{"status":"success"}`))
		case request.Method == http.MethodGet && request.URL.Path == "/api/latest/projects/PRJ/repos/demo/webhooks/1/latest":
			_, _ = writer.Write([]byte(`{"outcome":"success"}`))
		case request.Method == http.MethodGet && request.URL.Path == "/api/latest/projects/PRJ/repos/demo/webhooks/1/statistics":
			_, _ = writer.Write([]byte(`{"invocations":10}`))
		case request.Method == http.MethodGet && request.URL.Path == "/api/latest/projects/PRJ/repos/demo/webhooks/1/statistics/summary":
			_, _ = writer.Write([]byte(`{"summary":"ok"}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client, err := openapigenerated.NewClientWithResponses(server.URL)
	if err != nil {
		t.Fatalf("create generated client: %v", err)
	}

	service := NewService(client)
	repo := RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}
	ctx := context.Background()

	// Auto-merge settings tests
	amSettings, err := service.GetRepositoryAutoMergeSettings(ctx, repo)
	if err != nil || amSettings == nil || amSettings.Enabled == nil || !*amSettings.Enabled {
		t.Errorf("GetRepositoryAutoMergeSettings failed: %v", err)
	}
	amSettings, err = service.UpdateRepositoryAutoMergeSettings(ctx, repo, true)
	if err != nil || amSettings == nil || amSettings.Enabled == nil || !*amSettings.Enabled {
		t.Errorf("UpdateRepositoryAutoMergeSettings failed: %v", err)
	}
	err = service.DeleteRepositoryAutoMergeSettings(ctx, repo)
	if err != nil {
		t.Errorf("DeleteRepositoryAutoMergeSettings failed: %v", err)
	}

	// Auto-decline settings tests
	adSettings, err := service.GetRepositoryAutoDeclineSettings(ctx, repo)
	if err != nil || adSettings == nil || adSettings.Enabled == nil || !*adSettings.Enabled || adSettings.InactivityWeeks == nil || *adSettings.InactivityWeeks != 4 {
		t.Errorf("GetRepositoryAutoDeclineSettings failed: %v", err)
	}
	adSettings, err = service.UpdateRepositoryAutoDeclineSettings(ctx, repo, true, 4)
	if err != nil || adSettings == nil || adSettings.Enabled == nil || !*adSettings.Enabled || adSettings.InactivityWeeks == nil || *adSettings.InactivityWeeks != 4 {
		t.Errorf("UpdateRepositoryAutoDeclineSettings failed: %v", err)
	}
	err = service.DeleteRepositoryAutoDeclineSettings(ctx, repo)
	if err != nil {
		t.Errorf("DeleteRepositoryAutoDeclineSettings failed: %v", err)
	}

	// Labels tests
	labels, err := service.ListRepositoryLabels(ctx, repo)
	if err != nil || len(labels) != 2 || labels[0] != "label1" {
		t.Errorf("ListRepositoryLabels failed: %v", err)
	}
	err = service.AddRepositoryLabel(ctx, repo, "label3")
	if err != nil {
		t.Errorf("AddRepositoryLabel failed: %v", err)
	}
	err = service.RemoveRepositoryLabel(ctx, repo, "label1")
	if err != nil {
		t.Errorf("RemoveRepositoryLabel failed: %v", err)
	}

	// Watch tests
	err = service.WatchRepository(ctx, repo)
	if err != nil {
		t.Errorf("WatchRepository failed: %v", err)
	}
	err = service.UnwatchRepository(ctx, repo)
	if err != nil {
		t.Errorf("UnwatchRepository failed: %v", err)
	}

	// Default tasks tests
	tasks, err := service.ListDefaultTasks(ctx, repo)
	if err != nil || len(tasks) != 1 || tasks[0].Id == nil || *tasks[0].Id != 123 {
		t.Errorf("ListDefaultTasks failed: %v", err)
	}
	sourceRef := "refs/heads/feature"
	targetRef := "refs/heads/master"
	task, err := service.AddDefaultTask(ctx, repo, "task1", &sourceRef, &targetRef)
	if err != nil || task == nil || task.Id == nil || *task.Id != 123 {
		t.Errorf("AddDefaultTask failed: %v", err)
	}
	task, err = service.UpdateDefaultTask(ctx, repo, "123", "task1-updated", &sourceRef, &targetRef)
	if err != nil || task == nil || task.Id == nil || *task.Id != 123 || task.Description == nil || *task.Description != "task1-updated" {
		t.Errorf("UpdateDefaultTask failed: %v", err)
	}
	err = service.DeleteDefaultTask(ctx, repo, "123")
	if err != nil {
		t.Errorf("DeleteDefaultTask failed: %v", err)
	}

	// Webhook lifecycle tests
	webhook, err := service.GetWebhook(ctx, repo, "1")
	if err != nil {
		t.Errorf("GetWebhook failed: %v", err)
	}
	webhook, err = service.UpdateWebhook(ctx, repo, "1", "hook1-updated", "http://url", []string{"repo:refs_changed"}, nil)
	if err != nil {
		t.Errorf("UpdateWebhook failed: %v", err)
	}
	searchRes, err := service.SearchWebhooks(ctx, repo, &sourceRef)
	if err != nil {
		t.Errorf("SearchWebhooks failed: %v", err)
	}
	testRes, err := service.TestWebhook(ctx, repo, "1")
	if err != nil {
		t.Errorf("TestWebhook failed: %v", err)
	}
	latestRes, err := service.GetWebhookLatestInvocation(ctx, repo, "1", nil, nil)
	if err != nil {
		t.Errorf("GetWebhookLatestInvocation failed: %v", err)
	}
	statsRes, err := service.GetWebhookStatistics(ctx, repo, "1")
	if err != nil {
		t.Errorf("GetWebhookStatistics failed: %v", err)
	}
	summaryRes, err := service.GetWebhookStatisticsSummary(ctx, repo, "1")
	if err != nil {
		t.Errorf("GetWebhookStatisticsSummary failed: %v", err)
	}

	_ = webhook
	_ = searchRes
	_ = testRes
	_ = latestRes
	_ = statsRes
	_ = summaryRes
}
