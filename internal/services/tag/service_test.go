package tag

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

func newTagTestService(t *testing.T, handler http.HandlerFunc) *Service {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := openapigenerated.NewClientWithResponses(server.URL + "/rest")
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	return NewService(client)
}

// TestTagServiceRefusesAnEmptyName covers the guard that runs before a request
// is built.
//
// The 403-maps-to-authorization half that stood beside it is gone: that is one
// assertion about openapi.MapStatusError, which has its own table test and a
// guard stopping it from being re-tested per service. Whether the tag service is
// wired to the taxonomy at all is asked against a server that really refuses, in
// TestLiveEveryServiceMapsItsFailures.
func TestTagServiceRefusesAnEmptyName(t *testing.T) {
	// Refused before a request is built, so reaching the handler is the failure.
	service := newTagTestService(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("validation let a request through: %s %s", r.Method, r.URL.Path)
	})

	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

	_, err := service.Create(context.Background(), repo, "", "abc", "")
	if err == nil || !strings.Contains(err.Error(), "tag name is required") {
		t.Fatalf("expected tag name validation error, got %v", err)
	}
}

func TestTagServiceValidationAndMapStatusHelpers(t *testing.T) {
	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}
	service := newTagTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	_, err := service.Create(context.Background(), repo, "name", "", "msg")
	if err == nil {
		t.Fatal("expected start-point validation error")
	}

	_, err = service.Get(context.Background(), repo, " ")
	if err == nil {
		t.Fatal("expected tag name validation error on get")
	}

	err = service.Delete(context.Background(), repo, " ")
	if err == nil {
		t.Fatal("expected tag name validation error on delete")
	}
}

// mock-inventory: transport-fault — the server is closed before the call, which no live instance can be asked to do; the subject is that every tag operation classifies a dead connection as transient.
func TestTagServiceTransportAndValidationBranches(t *testing.T) {
	t.Run("repository validation branches", func(t *testing.T) {
		service := newTagTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		if _, err := service.List(context.Background(), RepositoryRef{}, ListOptions{}); err == nil {
			t.Fatal("expected repository validation error on list")
		}
		if _, err := service.Create(context.Background(), RepositoryRef{}, "v1", "abc", ""); err == nil {
			t.Fatal("expected repository validation error on create")
		}
		if _, err := service.Get(context.Background(), RepositoryRef{}, "v1"); err == nil {
			t.Fatal("expected repository validation error on get")
		}
		if err := service.Delete(context.Background(), RepositoryRef{}, "v1"); err == nil {
			t.Fatal("expected repository validation error on delete")
		}
	})

	// A server that is not there, which is the one failure a live instance
	// cannot be asked for and the one every paged call has to classify: a walk
	// that loses the connection halfway must report it rather than return the
	// pages it managed.
	t.Run("transport failures", func(t *testing.T) {
		baseURL := testsupport.ClosedListenerURL(t)

		client, err := openapigenerated.NewClientWithResponses(baseURL + "/rest")
		if err != nil {
			t.Fatalf("create client: %v", err)
		}
		service := NewService(client)
		repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

		if _, err := service.List(context.Background(), repo, ListOptions{}); err == nil || apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient transport error for list, got %v", err)
		}
		if _, err := service.Create(context.Background(), repo, "v1", "abc", "msg"); err == nil || apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient transport error for create, got %v", err)
		}
		if _, err := service.Get(context.Background(), repo, "v1"); err == nil || apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient transport error for get, got %v", err)
		}
		if err := service.Delete(context.Background(), repo, "v1"); err == nil || apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient transport error for delete, got %v", err)
		}
	})

	// "list uses defaults and trims filters" is gone.
	//
	// It matched limit=25 and start=0 on the wire, which is openapi.PageThrough's
	// business now and tested where the loop lives, and orderBy/filterText, which
	// TestLiveCLICommandCoverage sends to a real Bitbucket and reads the answer
	// back from. Watching a query string leave says the parameter was built;
	// only the server says it was understood.
}
