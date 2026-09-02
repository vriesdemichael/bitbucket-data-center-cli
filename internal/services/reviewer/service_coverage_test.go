package reviewer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

func TestReviewerServiceCoverageAdditional(t *testing.T) {
	service := NewService(nil)

	if _, err := service.ListProjectConditions(context.Background(), ""); err == nil {
		t.Error("expected error for empty project key")
	}
	if _, err := service.ListRepositoryConditions(context.Background(), "", ""); err == nil {
		t.Error("expected error for empty repo")
	}
	if err := service.DeleteProjectCondition(context.Background(), "", ""); err == nil {
		t.Error("expected error for empty project/id")
	}
	if err := service.DeleteRepositoryCondition(context.Background(), "", "", ""); err == nil {
		t.Error("expected error for empty repo/id")
	}
	if _, err := service.CreateProjectCondition(context.Background(), "", openapigenerated.RestDefaultReviewersRequest{}); err == nil {
		t.Error("expected error for empty project")
	}
	if _, err := service.CreateRepositoryCondition(context.Background(), "", "", openapigenerated.RestDefaultReviewersRequest{}); err == nil {
		t.Error("expected error for empty repo")
	}
}

func TestCreateRepositoryConditionUnmarshalError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{invalid json`))
	}))
	defer server.Close()

	client, _ := openapigenerated.NewClientWithResponses(server.URL)
	service := NewService(client)

	_, err := service.CreateRepositoryCondition(context.Background(), "P", "R", openapigenerated.RestDefaultReviewersRequest{})
	if err == nil {
		t.Error("expected unmarshal error for invalid JSON on 201")
	}
}

func TestCreateProjectConditionUnmarshalError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{invalid json`))
	}))
	defer server.Close()

	client, _ := openapigenerated.NewClientWithResponses(server.URL)
	service := NewService(client)

	_, err := service.CreateProjectCondition(context.Background(), "P", openapigenerated.RestDefaultReviewersRequest{})
	if err == nil {
		t.Error("expected unmarshal error for invalid JSON on 201")
	}
}
