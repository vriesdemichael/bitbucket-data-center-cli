package forksync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

func newForkSyncTestService(t *testing.T, handler http.HandlerFunc) *Service {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := openapigenerated.NewClientWithResponses(server.URL + "/rest")
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	return NewService(client)
}

func TestForkSyncServiceCRUD(t *testing.T) {
	service := newForkSyncTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/sync/latest/projects/PRJ/repos/repo":
			_, _ = w.Write([]byte(`{"enabled":true,"available":true}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/sync/latest/projects/PRJ/repos/repo":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"enabled":false,"available":true}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/sync/latest/projects/PRJ/repos/repo/synchronize":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})

	ctx := context.Background()

	// Get Status
	status, err := service.GetSyncStatus(ctx, "PRJ", "repo")
	if err != nil || status.Enabled == nil || !*status.Enabled {
		t.Fatalf("expected status success, got %#v err=%v", status, err)
	}

	// Set Enabled
	updated, err := service.SetEnabled(ctx, "PRJ", "repo", false)
	if err != nil || updated.Enabled == nil || *updated.Enabled {
		t.Fatalf("expected update success, got %#v err=%v", updated, err)
	}

	// Trigger Manual Sync
	if err := service.Synchronize(ctx, "PRJ", "repo"); err != nil {
		t.Fatalf("expected trigger sync success, got %v", err)
	}
}

func TestForkSyncServiceValidation(t *testing.T) {
	service := newForkSyncTestService(t, func(w http.ResponseWriter, r *http.Request) {})
	ctx := context.Background()

	// Status validation error
	_, err := service.GetSyncStatus(ctx, "", "repo")
	if err == nil || apperrors.ExitCode(err) != 2 {
		t.Fatalf("expected validation error getting status with empty project, got %v", err)
	}

	// SetEnabled validation error
	_, err = service.SetEnabled(ctx, "PRJ", "", true)
	if err == nil || apperrors.ExitCode(err) != 2 {
		t.Fatalf("expected validation error enabling with empty slug, got %v", err)
	}

	// Synchronize validation error
	err = service.Synchronize(ctx, " ", "repo")
	if err == nil || apperrors.ExitCode(err) != 2 {
		t.Fatalf("expected validation error syncing with empty project, got %v", err)
	}
}

func TestForkSyncServiceErrors(t *testing.T) {
	// 1. Client transport errors
	badClient, _ := openapigenerated.NewClientWithResponses("http://127.0.0.1:0/rest")
	badService := NewService(badClient)
	ctx := context.Background()

	if _, err := badService.GetSyncStatus(ctx, "PRJ", "repo"); err == nil {
		t.Fatal("expected error getting status with bad client")
	}
	if _, err := badService.SetEnabled(ctx, "PRJ", "repo", true); err == nil {
		t.Fatal("expected error setting enabled with bad client")
	}
	if err := badService.Synchronize(ctx, "PRJ", "repo"); err == nil {
		t.Fatal("expected error syncing with bad client")
	}

	// 2. HTTP status errors (500)
	errorService := newForkSyncTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	if _, err := errorService.GetSyncStatus(ctx, "PRJ", "repo"); err == nil {
		t.Fatal("expected error getting status on 500")
	}
	if _, err := errorService.SetEnabled(ctx, "PRJ", "repo", true); err == nil {
		t.Fatal("expected error setting enabled on 500")
	}
	if err := errorService.Synchronize(ctx, "PRJ", "repo"); err == nil {
		t.Fatal("expected error syncing on 500")
	}

	// 3. Nil/Empty response body cases (Status 200 but nil body/empty response)
	nilBodyService := newForkSyncTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`invalid`))
	})

	if _, err := nilBodyService.GetSyncStatus(ctx, "PRJ", "repo"); err == nil {
		t.Fatal("expected error getting status on empty response body")
	}
	if _, err := nilBodyService.SetEnabled(ctx, "PRJ", "repo", true); err == nil {
		t.Fatal("expected error setting enabled on empty response body")
	}

	// 4. Non-JSON 200 OK empty response body cases
	emptyResponseService := newForkSyncTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	if _, err := emptyResponseService.GetSyncStatus(ctx, "PRJ", "repo"); err == nil {
		t.Fatal("expected error getting status on empty response body (non-json 200)")
	}
	if _, err := emptyResponseService.SetEnabled(ctx, "PRJ", "repo", true); err == nil {
		t.Fatal("expected error setting enabled on empty response body (non-json 200)")
	}
}

