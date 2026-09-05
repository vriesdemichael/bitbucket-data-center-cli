package repository

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

func newAdminTestService(t *testing.T, handler http.HandlerFunc) *AdminService {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := openapigenerated.NewClientWithResponses(server.URL + "/rest")
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	return NewAdminService(client)
}

func TestAdminServiceValidation(t *testing.T) {
	service := newAdminTestService(t, testsupport.UnreachedHandler(t))

	if _, err := service.Create(context.Background(), "", CreateInput{Name: "repo"}); err == nil {
		t.Fatal("expected create validation error")
	}
	if _, err := service.Create(context.Background(), "PRJ", CreateInput{Name: ""}); err == nil {
		t.Fatal("expected create validation error")
	}

	if _, err := service.Fork(context.Background(), RepositoryRef{}, ForkInput{}); err == nil {
		t.Fatal("expected fork validation error")
	}

	if _, err := service.Update(context.Background(), RepositoryRef{}, UpdateInput{}); err == nil {
		t.Fatal("expected update validation error")
	}

	if err := service.Delete(context.Background(), RepositoryRef{}); err == nil {
		t.Fatal("expected delete validation error")
	}

	// A 403 mapping to an authorization error used to be asserted here. It is
	// TestMapStatusError's, once for every caller, and it is live besides:
	// TestLivePermissionRepoCreateDeniedWithProjectReadOnly makes a real
	// instance refuse a create the caller may not make.
}

func TestAdminServiceTransientAndMapping(t *testing.T) {
	transientService := newAdminTestService(t, func(w http.ResponseWriter, r *http.Request) {
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "hijacking not supported", http.StatusInternalServerError)
			return
		}
		connection, _, hijackErr := hijacker.Hijack()
		if hijackErr == nil {
			_ = connection.Close()
		}
	})

	repoRef := RepositoryRef{ProjectKey: "PRJ", Slug: "repo"}

	if _, err := transientService.Create(context.Background(), "PRJ", CreateInput{Name: "repo"}); err == nil || apperrors.ExitCode(err) != 10 {
		t.Fatalf("expected transient create error, got %v", err)
	}
	if _, err := transientService.Fork(context.Background(), repoRef, ForkInput{Name: "fork"}); err == nil || apperrors.ExitCode(err) != 10 {
		t.Fatalf("expected transient fork error, got %v", err)
	}
	if _, err := transientService.Update(context.Background(), repoRef, UpdateInput{Name: "update"}); err == nil || apperrors.ExitCode(err) != 10 {
		t.Fatalf("expected transient update error, got %v", err)
	}
	if err := transientService.Delete(context.Background(), repoRef); err == nil || apperrors.ExitCode(err) != 10 {
		t.Fatalf("expected transient delete error, got %v", err)
	}

	service := newAdminTestService(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost || r.Method == http.MethodPut:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	})

	if created, err := service.Create(context.Background(), "PRJ", CreateInput{Name: "repo"}); err != nil || created.Name != nil {
		t.Fatalf("expected empty create success, got %v", err)
	}

	if forked, err := service.Fork(context.Background(), repoRef, ForkInput{}); err != nil || forked.Name != nil {
		t.Fatalf("expected empty fork success, got %v", err)
	}

	if updated, err := service.Update(context.Background(), repoRef, UpdateInput{}); err != nil || updated.Name != nil {
		t.Fatalf("expected empty update success, got %v", err)
	}

}

// TestAdminServiceCoreCommands is gone rather than moved. Create, fork, update
// and delete each answered from a handler that had been written to answer
// them, so what it checked was that a slug we wrote comes back. All four are
// live: TestLiveMutatingCoverage creates and forks against a real instance,
// and the dry-run suites predict against repositories that are really there.
