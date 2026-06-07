package reviewer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

func ptr[T any](v T) *T {
	return &v
}

func TestReviewerService(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/default-reviewers/latest/projects/PRJ/conditions":
			_, _ = w.Write([]byte(`[{"id":1,"requiredApprovals":1}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/default-reviewers/latest/projects/PRJ/repos/demo/conditions":
			_, _ = w.Write([]byte(`[{"id":2,"requiredApprovals":2}]`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/default-reviewers/latest/projects/PRJ/condition/1":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/default-reviewers/latest/projects/PRJ/repos/demo/condition/2":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/rest/default-reviewers/latest/projects/PRJ/condition":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":3,"requiredApprovals":1}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/default-reviewers/latest/projects/PRJ/repos/demo/condition":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":4,"requiredApprovals":1}`))
		case r.Method == http.MethodPut && r.URL.Path == "/rest/default-reviewers/latest/projects/PRJ/condition/1":
			_, _ = w.Write([]byte(`{"id":1,"requiredApprovals":3}`))
		case r.Method == http.MethodPut && r.URL.Path == "/rest/default-reviewers/latest/projects/PRJ/repos/demo/condition/2":
			_, _ = w.Write([]byte(`{"id":2,"requiredApprovals":4}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, _ := openapigenerated.NewClientWithResponses(server.URL + "/rest")
	service := NewService(client)

	projConditions, err := service.ListProjectConditions(context.Background(), "PRJ")
	if err != nil || len(projConditions) != 1 {
		t.Fatalf("list project conditions failed: %v", err)
	}

	repoConditions, err := service.ListRepositoryConditions(context.Background(), "PRJ", "demo")
	if err != nil || len(repoConditions) != 1 {
		t.Fatalf("list repository conditions failed: %v", err)
	}

	if err := service.DeleteProjectCondition(context.Background(), "PRJ", "1"); err != nil {
		t.Fatalf("delete project condition failed: %v", err)
	}

	if err := service.DeleteRepositoryCondition(context.Background(), "PRJ", "demo", "2"); err != nil {
		t.Fatalf("delete repository condition failed: %v", err)
	}

	if _, err := service.CreateProjectCondition(context.Background(), "PRJ", openapigenerated.RestDefaultReviewersRequest{}); err != nil {
		t.Fatalf("create project condition failed: %v", err)
	}

	if _, err := service.CreateRepositoryCondition(context.Background(), "PRJ", "demo", openapigenerated.RestDefaultReviewersRequest{}); err != nil {
		t.Fatalf("create repository condition failed: %v", err)
	}

	if _, err := service.UpdateProjectCondition(context.Background(), "PRJ", "1", openapigenerated.UpdatePullRequestConditionJSONRequestBody{
		RequiredApprovals: ptr(int32(3)),
	}); err != nil {
		t.Fatalf("update project condition failed: %v", err)
	}

	if _, err := service.UpdateRepositoryCondition(context.Background(), "PRJ", "demo", "2", openapigenerated.UpdatePullRequestCondition1JSONRequestBody{
		RequiredApprovals: ptr(int32(4)),
	}); err != nil {
		t.Fatalf("update repository condition failed: %v", err)
	}
}

func TestReviewerServiceAdditional(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/rest/default-reviewers/latest/projects/PRJ/conditions":
			_, _ = w.Write([]byte(`[]`))
		case r.URL.Path == "/rest/default-reviewers/latest/projects/PRJ/repos/demo/conditions":
			_, _ = w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, _ := openapigenerated.NewClientWithResponses(server.URL + "/rest")
	service := NewService(client)

	// Hit empty branches
	_, _ = service.ListProjectConditions(context.Background(), "PRJ")
	_, _ = service.ListRepositoryConditions(context.Background(), "PRJ", "demo")
}

func TestReviewerServiceErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("error"))
	}))
	defer server.Close()

	client, _ := openapigenerated.NewClientWithResponses(server.URL + "/rest")
	service := NewService(client)

	if _, err := service.ListProjectConditions(context.Background(), "PRJ"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.ListRepositoryConditions(context.Background(), "PRJ", "demo"); err == nil {
		t.Fatal("expected error")
	}
	if err := service.DeleteProjectCondition(context.Background(), "PRJ", "1"); err == nil {
		t.Fatal("expected delete project condition error")
	}
	if err := service.DeleteRepositoryCondition(context.Background(), "PRJ", "demo", "2"); err == nil {
		t.Fatal("expected delete repository condition error")
	}
}

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

func TestReviewerServiceUpdateErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client, _ := openapigenerated.NewClientWithResponses(server.URL)
	service := NewService(client)

	if _, err := service.UpdateProjectCondition(context.Background(), "P", "1", openapigenerated.UpdatePullRequestConditionJSONRequestBody{}); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.UpdateRepositoryCondition(context.Background(), "P", "S", "1", openapigenerated.UpdatePullRequestCondition1JSONRequestBody{}); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.CreateRepositoryCondition(context.Background(), "P", "S", openapigenerated.RestDefaultReviewersRequest{}); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.CreateProjectCondition(context.Background(), "P", openapigenerated.RestDefaultReviewersRequest{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestReviewerServiceCreationBranches(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			// Return 200 instead of 201 to hit JSON200 branch
			_, _ = w.Write([]byte(`{"id":9}`))
			return
		}
		if r.Method == http.MethodPut {
			// Return invalid JSON to hit unmarshal fail branch
			_, _ = w.Write([]byte(`invalid`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client, _ := openapigenerated.NewClientWithResponses(server.URL)
	service := NewService(client)

	_, _ = service.CreateProjectCondition(context.Background(), "P", openapigenerated.RestDefaultReviewersRequest{})
	_, _ = service.UpdateProjectCondition(context.Background(), "P", "1", openapigenerated.UpdatePullRequestConditionJSONRequestBody{})
}

func TestReviewerServiceCreationUnmarshalFail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`invalid`))
	}))
	defer server.Close()

	client, _ := openapigenerated.NewClientWithResponses(server.URL)
	service := NewService(client)

	_, _ = service.CreateProjectCondition(context.Background(), "P", openapigenerated.RestDefaultReviewersRequest{})
}

func TestReviewerServiceUpdateResponseBranches(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPut {
			// Return 200 to hit JSON200 branches instead of 201 or 300
			_, _ = w.Write([]byte(`{"id":42}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client, _ := openapigenerated.NewClientWithResponses(server.URL)
	service := NewService(client)

	_, _ = service.UpdateProjectCondition(context.Background(), "P", "1", openapigenerated.UpdatePullRequestConditionJSONRequestBody{})
	_, _ = service.UpdateRepositoryCondition(context.Background(), "P", "S", "1", openapigenerated.UpdatePullRequestCondition1JSONRequestBody{})
}

func TestReviewerGroupsAndDefaultReviewersService(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		// Repository Reviewer Groups
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/settings/reviewer-groups":
			_, _ = w.Write([]byte(`{"values":[{"id":1,"name":"group1"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/settings/reviewer-groups":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":2,"name":"group2"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/settings/reviewer-groups/1":
			_, _ = w.Write([]byte(`{"id":1,"name":"group1"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/settings/reviewer-groups/1":
			_, _ = w.Write([]byte(`{"id":1,"name":"group1-updated"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/settings/reviewer-groups/1":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/settings/reviewer-groups/1/users":
			_, _ = w.Write([]byte(`[{"id":100,"name":"user1"}]`))

		// Project Reviewer Groups
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/settings/reviewer-groups":
			_, _ = w.Write([]byte(`{"values":[{"id":3,"name":"group3"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/latest/projects/PRJ/settings/reviewer-groups":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":4,"name":"group4"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/settings/reviewer-groups/3":
			_, _ = w.Write([]byte(`{"id":3,"name":"group3"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/rest/api/latest/projects/PRJ/settings/reviewer-groups/3":
			_, _ = w.Write([]byte(`{"id":3,"name":"group3-updated"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/api/latest/projects/PRJ/settings/reviewer-groups/3":
			w.WriteHeader(http.StatusNoContent)

		// Default Reviewers
		case r.Method == http.MethodGet && r.URL.Path == "/rest/default-reviewers/latest/projects/PRJ/repos/demo/reviewers":
			_, _ = w.Write([]byte(`[{"id":10,"requiredApprovals":2}]`))

		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, _ := openapigenerated.NewClientWithResponses(server.URL + "/rest")
	service := NewService(client)

	// Repo-scoped list
	repoGroups, err := service.ListRepositoryReviewerGroups(context.Background(), "PRJ", "demo")
	if err != nil || len(repoGroups) != 1 || *repoGroups[0].Name != "group1" {
		t.Fatalf("ListRepositoryReviewerGroups failed: %v", err)
	}

	// Repo-scoped create
	createdRepoGroup, err := service.CreateRepositoryReviewerGroup(context.Background(), "PRJ", "demo", "group2", "desc2")
	if err != nil || *createdRepoGroup.Id != 2 || *createdRepoGroup.Name != "group2" {
		t.Fatalf("CreateRepositoryReviewerGroup failed: %v", err)
	}

	// Repo-scoped get
	gotRepoGroup, err := service.GetRepositoryReviewerGroup(context.Background(), "PRJ", "demo", "1")
	if err != nil || *gotRepoGroup.Id != 1 || *gotRepoGroup.Name != "group1" {
		t.Fatalf("GetRepositoryReviewerGroup failed: %v", err)
	}

	// Repo-scoped update
	updatedRepoGroup, err := service.UpdateRepositoryReviewerGroup(context.Background(), "PRJ", "demo", "1", "group1-updated", "desc1")
	if err != nil || *updatedRepoGroup.Name != "group1-updated" {
		t.Fatalf("UpdateRepositoryReviewerGroup failed: %v", err)
	}

	// Repo-scoped delete
	if err := service.DeleteRepositoryReviewerGroup(context.Background(), "PRJ", "demo", "1"); err != nil {
		t.Fatalf("DeleteRepositoryReviewerGroup failed: %v", err)
	}

	// Repo-scoped list users
	repoUsers, err := service.ListRepositoryReviewerGroupUsers(context.Background(), "PRJ", "demo", "1")
	if err != nil || len(repoUsers) != 1 || *repoUsers[0].Name != "user1" {
		t.Fatalf("ListRepositoryReviewerGroupUsers failed: %v", err)
	}

	// Project-scoped list
	projGroups, err := service.ListProjectReviewerGroups(context.Background(), "PRJ")
	if err != nil || len(projGroups) != 1 || *projGroups[0].Name != "group3" {
		t.Fatalf("ListProjectReviewerGroups failed: %v", err)
	}

	// Project-scoped create
	createdProjGroup, err := service.CreateProjectReviewerGroup(context.Background(), "PRJ", "group4", "desc4")
	if err != nil || *createdProjGroup.Id != 4 || *createdProjGroup.Name != "group4" {
		t.Fatalf("CreateProjectReviewerGroup failed: %v", err)
	}

	// Project-scoped get
	gotProjGroup, err := service.GetProjectReviewerGroup(context.Background(), "PRJ", "3")
	if err != nil || *gotProjGroup.Id != 3 || *gotProjGroup.Name != "group3" {
		t.Fatalf("GetProjectReviewerGroup failed: %v", err)
	}

	// Project-scoped update
	updatedProjGroup, err := service.UpdateProjectReviewerGroup(context.Background(), "PRJ", "3", "group3-updated", "desc3")
	if err != nil || *updatedProjGroup.Name != "group3-updated" {
		t.Fatalf("UpdateProjectReviewerGroup failed: %v", err)
	}

	// Project-scoped delete
	if err := service.DeleteProjectReviewerGroup(context.Background(), "PRJ", "3"); err != nil {
		t.Fatalf("DeleteProjectReviewerGroup failed: %v", err)
	}

	// Default reviewers
	defReviewers, err := service.GetDefaultReviewers(context.Background(), "PRJ", "demo", nil, nil, nil, nil)
	if err != nil || len(defReviewers) != 1 || *defReviewers[0].Id != 10 {
		t.Fatalf("GetDefaultReviewers failed: %v", err)
	}
}

func TestReviewerGroupsAndDefaultReviewersServiceErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client, _ := openapigenerated.NewClientWithResponses(server.URL)
	service := NewService(client)

	ctx := context.Background()

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


