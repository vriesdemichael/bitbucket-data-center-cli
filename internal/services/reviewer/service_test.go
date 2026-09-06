package reviewer

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

func TestReviewerServiceValidation(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

	service := NewService(nil)
	ctx := context.Background()

	if _, err := service.ListRepositoryReviewerGroups(ctx, "", "S"); err == nil {
		t.Fatal("expected validation error")
	}
	if _, err := service.CreateRepositoryReviewerGroup(ctx, "P", "", "n", "", nil); err == nil {
		t.Fatal("expected validation error")
	}
	if _, err := service.GetRepositoryReviewerGroup(ctx, "P", "S", ""); err == nil {
		t.Fatal("expected validation error")
	}
	if _, err := service.UpdateRepositoryReviewerGroup(ctx, "", "S", "1", "n", "", nil); err == nil {
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
	if _, err := service.CreateProjectReviewerGroup(ctx, "", "n", "", nil); err == nil {
		t.Fatal("expected validation error")
	}
	if _, err := service.GetProjectReviewerGroup(ctx, "P", ""); err == nil {
		t.Fatal("expected validation error")
	}
	if _, err := service.UpdateProjectReviewerGroup(ctx, "", "1", "n", "", nil); err == nil {
		t.Fatal("expected validation error")
	}
	if err := service.DeleteProjectReviewerGroup(ctx, "P", ""); err == nil {
		t.Fatal("expected validation error")
	}
	if _, err := service.GetDefaultReviewers(ctx, "", "S", nil, nil, nil, nil); err == nil {
		t.Fatal("expected validation error")
	}
}

// mock-inventory: transport-fault — a cancelled context is the caller's doing; no server produces it.
func TestReviewerGroupsAndDefaultReviewersServiceContextCanceled(t *testing.T) {
	t.Parallel()

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
	if _, err := service.CreateRepositoryReviewerGroup(ctx, "P", "S", "n", "", nil); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.GetRepositoryReviewerGroup(ctx, "P", "S", "1"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.UpdateRepositoryReviewerGroup(ctx, "P", "S", "1", "n", "", nil); err == nil {
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
	if _, err := service.CreateProjectReviewerGroup(ctx, "P", "n", "", nil); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.GetProjectReviewerGroup(ctx, "P", "1"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.UpdateProjectReviewerGroup(ctx, "P", "1", "n", "", nil); err == nil {
		t.Fatal("expected error")
	}
	if err := service.DeleteProjectReviewerGroup(ctx, "P", "1"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.GetDefaultReviewers(ctx, "P", "S", nil, nil, nil, nil); err == nil {
		t.Fatal("expected error")
	}
}

// mock-inventory: unreachable-state — an object with no values key where a page belongs, which Bitbucket does not send; the subject is that a listing missing its values reads as empty rather than as a nil dereference.
func TestReviewerGroupsAndDefaultReviewersServiceResponseFallbacks(t *testing.T) {
	t.Parallel()

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

	_, _ = service.CreateRepositoryReviewerGroup(ctx, "PRJ", "demo", "group", "", nil)

	users, err := service.ListRepositoryReviewerGroupUsers(ctx, "PRJ", "demo", "1")
	if err != nil || len(users) != 0 {
		t.Fatalf("expected empty users, got %v: %v", users, err)
	}

	projGroups, err := service.ListProjectReviewerGroups(ctx, "PRJ")
	if err != nil || len(projGroups) != 0 {
		t.Fatalf("expected empty projGroups, got %v: %v", projGroups, err)
	}

	_, _ = service.CreateProjectReviewerGroup(ctx, "PRJ", "group", "", nil)
}

// TestResolveGroupMembersReportsATransportFailure covers the branch a server
// cannot produce on request.
//
// Resolving a reviewer group's members is an extra round trip the caller never
// asked for, made on their behalf to turn a username into the numeric id
// Bitbucket wants. When that lookup cannot be made at all -- the connection
// refused, the far end gone -- the failure has to reach the caller as
// transient, naming the user it was looking up. Swallowing it would create the
// group without that member, which is the outcome the id lookup exists to
// prevent.
//
// A live Bitbucket cannot be asked to drop a connection on cue, so the listener
// is closed before the request is made.
func TestResolveGroupMembersReportsATransportFailure(t *testing.T) {
	t.Parallel()

	client, err := openapigenerated.NewClientWithResponses(testsupport.ClosedListenerURL(t))
	if err != nil {
		t.Fatalf("build client: %v", err)
	}
	service := NewService(nil)
	service.client = client

	members, err := service.resolveGroupMembers(context.Background(), []string{"alice"})
	if err == nil {
		t.Fatalf("a refused connection resolved to %v", members)
	}
	if kind := apperrors.KindOf(err); kind != apperrors.KindTransient {
		t.Errorf("kind = %v, want transient: %v", kind, err)
	}
	if !strings.Contains(err.Error(), "alice") {
		t.Errorf("the failure does not name the user it was looking up: %v", err)
	}
}

// TestResolveGroupMembersSkipsBlanks covers the padding a comma-separated flag
// leaves behind: "alice,,bob" and "alice, bob" both reach here with entries
// that are not names. They are dropped rather than looked up, because a lookup
// of "" is a request that cannot succeed and an error the caller cannot act on.
func TestResolveGroupMembersSkipsBlanks(t *testing.T) {
	t.Parallel()

	service := NewService(nil)

	members, err := service.resolveGroupMembers(context.Background(), []string{"", "   ", "\t"})
	if err != nil {
		t.Fatalf("blank entries should be skipped, got: %v", err)
	}
	if len(members) != 0 {
		t.Errorf("resolved %d members from blanks, want none", len(members))
	}
}
