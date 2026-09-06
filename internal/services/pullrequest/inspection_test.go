package pullrequest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
)

func newInspectionService(t *testing.T, handler http.HandlerFunc) *Service {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")

	cfg, err := config.LoadFromEnv()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	return NewService(httpclient.NewFromConfig(cfg))
}

var inspectionRepo = RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

func TestInspectionValidatesInput(t *testing.T) {
	t.Parallel()

	service := NewService(nil)

	if _, err := service.ListCommits(context.Background(), RepositoryRef{}, "7", PageOptions{}); err == nil {
		t.Fatal("expected error for missing repository")
	}
	if _, err := service.ListChanges(context.Background(), inspectionRepo, "not-a-number", PageOptions{}); err == nil {
		t.Fatal("expected error for invalid pull request id")
	}
	if _, err := service.ListCommits(context.Background(), inspectionRepo, "7", PageOptions{Start: -1}); err == nil {
		t.Fatal("expected error for negative start")
	}
	if _, err := service.GetMergeBase(context.Background(), inspectionRepo, ""); err == nil {
		t.Fatal("expected error for empty pull request id")
	}
}

// TestInspectionPropagatesTransportErrors covers the error branches that run
// after validation succeeds, when the server returns a non-2xx response.
// mock-inventory: transport-fault — the transport is broken on purpose; the subject is that the error is not swallowed.
func TestInspectionPropagatesTransportErrors(t *testing.T) {
	service := newInspectionService(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"errors":[{"message":"boom"}]}`, http.StatusInternalServerError)
	})

	if _, err := service.ListCommits(context.Background(), inspectionRepo, "7", PageOptions{}); err == nil {
		t.Fatal("expected transport error from ListCommits")
	}
	if _, err := service.ListChanges(context.Background(), inspectionRepo, "7", PageOptions{}); err == nil {
		t.Fatal("expected transport error from ListChanges")
	}
	if _, err := service.GetMergeBase(context.Background(), inspectionRepo, "7"); err == nil {
		t.Fatal("expected transport error from GetMergeBase")
	}
}
