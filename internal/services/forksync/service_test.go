package forksync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
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

func TestForkSyncServiceValidation(t *testing.T) {
	service := newForkSyncTestService(t, testsupport.UnreachedHandler(t))
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
	err = service.Synchronize(ctx, " ", "repo", "refs/heads/master", "MERGE")
	if err == nil || apperrors.ExitCode(err) != 2 {
		t.Fatalf("expected validation error syncing with empty project, got %v", err)
	}
}

// mock-inventory: transport-fault — the failures are injected below the API: a broken client, not a server's answer.
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
	if err := badService.Synchronize(ctx, "PRJ", "repo", "refs/heads/master", "MERGE"); err == nil {
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
	if err := errorService.Synchronize(ctx, "PRJ", "repo", "refs/heads/master", "MERGE"); err == nil {
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

// The server answers a missing ref or a missing action with a 500 that names
// nothing, so both are refused here instead, where the error can name the flag.
func TestForkSyncSynchronizeRequiresRefAndAction(t *testing.T) {
	service := newForkSyncTestService(t, testsupport.UnreachedHandler(t))
	ctx := context.Background()

	if err := service.Synchronize(ctx, "PRJ", "repo", "  ", "MERGE"); err == nil || apperrors.ExitCode(err) != 2 {
		t.Fatalf("expected a validation error for a missing ref, got %v", err)
	}
	if err := service.Synchronize(ctx, "PRJ", "repo", "refs/heads/master", "SQUASH"); err == nil || apperrors.ExitCode(err) != 2 {
		t.Fatalf("expected a validation error for an unknown action, got %v", err)
	}
}

// An omitted action means MERGE rather than an empty field, since the server
// requires one and merging is what the endpoint is for.
//
// mock-inventory: unreachable-state — a caller that omits the action, which no command does: `repo sync` defaults the flag to MERGE, so this defence is only reachable from here. TestLiveRepositoryForkSync covers the wire form the CLI actually sends, with and without the flag.
func TestForkSyncSynchronizeDefaultsToMerge(t *testing.T) {
	var captured map[string]any
	service := newForkSyncTestService(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.WriteHeader(http.StatusOK)
	})

	if err := service.Synchronize(context.Background(), "PRJ", "repo", "refs/heads/master", ""); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if captured["action"] != "MERGE" {
		t.Fatalf("action = %v, want MERGE", captured["action"])
	}
	if captured["refId"] != "refs/heads/master" {
		t.Fatalf("refId = %v, want refs/heads/master", captured["refId"])
	}
}
