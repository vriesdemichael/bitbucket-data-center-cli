package reviewer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

func TestReviewerServiceValidation(t *testing.T) {
	service := NewService(nil)
	if _, err := service.ListProjectConditions(context.Background(), ""); err == nil {
		t.Fatal("expected validation error")
	}
	if _, err := service.ListRepositoryConditions(context.Background(), "", ""); err == nil {
		t.Fatal("expected validation error")
	}
	if err := service.DeleteProjectCondition(context.Background(), "", ""); err == nil {
		t.Fatal("expected validation error")
	}
	if err := service.DeleteRepositoryCondition(context.Background(), "", "", ""); err == nil {
		t.Fatal("expected validation error")
	}
	if err := service.DeleteRepositoryCondition(context.Background(), "PRJ", "demo", "abc"); err == nil {
		t.Fatal("expected validation error for non-int id")
	}
}

func TestReviewerServiceUpdateValidation(t *testing.T) {
	service := NewService(nil)
	if _, err := service.UpdateProjectCondition(context.Background(), "", "1", openapigenerated.UpdatePullRequestConditionJSONRequestBody{}); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.UpdateProjectCondition(context.Background(), "P", "", openapigenerated.UpdatePullRequestConditionJSONRequestBody{}); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.UpdateRepositoryCondition(context.Background(), "", "S", "1", openapigenerated.UpdatePullRequestCondition1JSONRequestBody{}); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.UpdateRepositoryCondition(context.Background(), "P", "", "1", openapigenerated.UpdatePullRequestCondition1JSONRequestBody{}); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.UpdateRepositoryCondition(context.Background(), "P", "S", "", openapigenerated.UpdatePullRequestCondition1JSONRequestBody{}); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.CreateRepositoryCondition(context.Background(), "", "S", openapigenerated.RestDefaultReviewersRequest{}); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.CreateRepositoryCondition(context.Background(), "P", "", openapigenerated.RestDefaultReviewersRequest{}); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.CreateProjectCondition(context.Background(), "", openapigenerated.RestDefaultReviewersRequest{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestReviewerGroupsAndDefaultReviewersServiceValidation(t *testing.T) {
	service := NewService(nil)
	ctx := context.Background()

	if _, err := service.ListRepositoryReviewerGroups(ctx, "", "S"); err == nil {
		t.Fatal("expected validation error")
	}
	if _, err := service.CreateRepositoryReviewerGroup(ctx, "P", "", "n", ""); err == nil {
		t.Fatal("expected validation error")
	}
	if _, err := service.GetRepositoryReviewerGroup(ctx, "P", "S", ""); err == nil {
		t.Fatal("expected validation error")
	}
	if _, err := service.UpdateRepositoryReviewerGroup(ctx, "", "S", "1", "n", ""); err == nil {
		t.Fatal("expected validation error")
	}
	if err := service.DeleteRepositoryReviewerGroup(ctx, "P", "", "1"); err == nil {
		t.Fatal("expected validation error")
	}
	if _, err := service.ListRepositoryReviewerGroupUsers(ctx, "P", "S", ""); err == nil {
		t.Fatal("expected validation error")
	}

	if _, err := service.ListProjectReviewerGroups(ctx, ""); err == nil {
		t.Fatal("expected validation error")
	}
	if _, err := service.CreateProjectReviewerGroup(ctx, "", "n", ""); err == nil {
		t.Fatal("expected validation error")
	}
	if _, err := service.GetProjectReviewerGroup(ctx, "P", ""); err == nil {
		t.Fatal("expected validation error")
	}
	if _, err := service.UpdateProjectReviewerGroup(ctx, "", "1", "n", ""); err == nil {
		t.Fatal("expected validation error")
	}
	if err := service.DeleteProjectReviewerGroup(ctx, "P", ""); err == nil {
		t.Fatal("expected validation error")
	}
	if _, err := service.GetDefaultReviewers(ctx, "", "S", nil, nil, nil, nil); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestReviewerGroupsAndDefaultReviewersServiceContextCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, _ := openapigenerated.NewClientWithResponses(server.URL)
	service := NewService(client)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := service.ListRepositoryReviewerGroups(ctx, "P", "S"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.CreateRepositoryReviewerGroup(ctx, "P", "S", "n", ""); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.GetRepositoryReviewerGroup(ctx, "P", "S", "1"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.UpdateRepositoryReviewerGroup(ctx, "P", "S", "1", "n", ""); err == nil {
		t.Fatal("expected error")
	}
	if err := service.DeleteRepositoryReviewerGroup(ctx, "P", "S", "1"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.ListRepositoryReviewerGroupUsers(ctx, "P", "S", "1"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.ListProjectReviewerGroups(ctx, "P"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.CreateProjectReviewerGroup(ctx, "P", "n", ""); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.GetProjectReviewerGroup(ctx, "P", "1"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.UpdateProjectReviewerGroup(ctx, "P", "1", "n", ""); err == nil {
		t.Fatal("expected error")
	}
	if err := service.DeleteProjectReviewerGroup(ctx, "P", "1"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.GetDefaultReviewers(ctx, "P", "S", nil, nil, nil, nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestReviewerGroupsAndDefaultReviewersServiceResponseFallbacks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if r.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/settings/reviewer-groups/1/users" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client, _ := openapigenerated.NewClientWithResponses(server.URL + "/rest")
	service := NewService(client)

	ctx := context.Background()

	groups, err := service.ListRepositoryReviewerGroups(ctx, "PRJ", "demo")
	if err != nil || len(groups) != 0 {
		t.Fatalf("expected empty groups, got %v: %v", groups, err)
	}

	_, _ = service.CreateRepositoryReviewerGroup(ctx, "PRJ", "demo", "group", "")

	users, err := service.ListRepositoryReviewerGroupUsers(ctx, "PRJ", "demo", "1")
	if err != nil || len(users) != 0 {
		t.Fatalf("expected empty users, got %v: %v", users, err)
	}

	projGroups, err := service.ListProjectReviewerGroups(ctx, "PRJ")
	if err != nil || len(projGroups) != 0 {
		t.Fatalf("expected empty projGroups, got %v: %v", projGroups, err)
	}

	_, _ = service.CreateProjectReviewerGroup(ctx, "PRJ", "group", "")
}
