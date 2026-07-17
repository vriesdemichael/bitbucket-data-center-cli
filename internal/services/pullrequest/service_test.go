package pullrequest

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/transport/httpclient"
)

func TestListPullRequestsWithPaginationAndFilters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/rest/api/latest/projects/TEST/repos/demo/pull-requests" {
			http.NotFound(w, request)
			return
		}

		start := request.URL.Query().Get("start")
		if start == "" || start == "0" {
			_, _ = fmt.Fprint(w, `{"values":[{"id":1,"title":"Open PR","state":"OPEN","open":true,"closed":false,"fromRef":{"displayId":"feature/a"},"toRef":{"displayId":"master"},"author":{"user":{"displayName":"A"}}}],"isLastPage":false,"nextPageStart":1}`)
			return
		}

		_, _ = fmt.Fprint(w, `{"values":[{"id":2,"title":"Merged PR","state":"MERGED","open":false,"closed":true,"fromRef":{"displayId":"feature/b"},"toRef":{"displayId":"master"},"author":{"user":{"name":"b-user"}}}],"isLastPage":true,"nextPageStart":2}`)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")

	cfg, err := config.LoadFromEnv()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	service := NewService(httpclient.NewFromConfig(cfg))

	results, err := service.List(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, ListOptions{State: "all", Limit: 1, SourceBranch: "feature/b"})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 filtered pull request, got %d", len(results))
	}
	if results[0].ID != 2 || results[0].Author != "b-user" {
		t.Fatalf("unexpected mapped pull request: %#v", results[0])
	}
}

func TestListPullRequestsDashboard(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/rest/api/1.0/dashboard/pull-requests" {
			http.NotFound(w, request)
			return
		}

		start := request.URL.Query().Get("start")
		if start == "" || start == "0" {
			_, _ = fmt.Fprint(w, `{"values":[{"id":1,"title":"Dashboard PR","state":"OPEN","open":true,"closed":false,"toRef":{"repository":{"slug":"demo","project":{"key":"TEST"}}}}],"isLastPage":false,"nextPageStart":1}`)
			return
		}

		_, _ = fmt.Fprint(w, `{"values":[{"id":2,"title":"Dashboard PR 2","state":"MERGED","open":false,"closed":true,"toRef":{"repository":{"slug":"demo","project":{"key":"TEST"}}}}],"isLastPage":true,"nextPageStart":2}`)
	}))
	defer server.Close()

	cfg := config.AppConfig{BitbucketURL: server.URL}
	service := NewService(httpclient.NewFromConfig(cfg))

	results, err := service.ListDashboard(context.Background(), DashboardListOptions{State: "all", Role: "author", Limit: 10})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 dashboard pull requests, got %d", len(results))
	}
	if results[0].ID != 1 || results[1].ID != 2 {
		t.Fatalf("unexpected mapped dashboard pull requests: %#v", results)
	}

	// Test state filter specific branch logic
	_, err = service.ListDashboard(context.Background(), DashboardListOptions{State: "open", Limit: 10})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	_, err = service.ListDashboard(context.Background(), DashboardListOptions{State: "closed", Limit: 10})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	_, err = service.ListDashboard(context.Background(), DashboardListOptions{State: "invalid"})
	if err == nil {
		t.Fatalf("expected error for invalid state")
	}

	_, err = service.ListDashboard(context.Background(), DashboardListOptions{Start: -1})
	if err == nil || apperrors.ExitCode(err) != 2 {
		t.Fatalf("expected start validation error exit code 2, got: %v", err)
	}

	_, err = service.ListDashboard(context.Background(), DashboardListOptions{State: "invalid"})
	if err == nil || apperrors.ExitCode(err) != 2 {
		t.Fatalf("expected state validation error exit code 2, got: %v", err)
	}
}

func TestListPullRequestsValidationAndAuthError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, `{"errors":[{"message":"Authentication required"}]}`)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")

	cfg, err := config.LoadFromEnv()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	service := NewService(httpclient.NewFromConfig(cfg))

	_, err = service.List(context.Background(), RepositoryRef{}, ListOptions{})
	if err == nil || apperrors.ExitCode(err) != 2 {
		t.Fatalf("expected validation error exit code 2, got: %v", err)
	}

	_, err = service.List(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, ListOptions{State: "invalid"})
	if err == nil || apperrors.ExitCode(err) != 2 {
		t.Fatalf("expected state validation error exit code 2, got: %v", err)
	}

	_, err = service.List(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, ListOptions{State: "open", Start: -1})
	if err == nil || apperrors.ExitCode(err) != 2 {
		t.Fatalf("expected start validation error exit code 2, got: %v", err)
	}

	_, err = service.List(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, ListOptions{State: "open", Limit: 5})
	if err == nil || apperrors.ExitCode(err) != 3 {
		t.Fatalf("expected auth error exit code 3, got: %v", err)
	}
}

func TestPullRequestHelperBranches(t *testing.T) {
	if normalized, err := normalizeState(""); err != nil || normalized != "open" {
		t.Fatalf("expected empty state to normalize to open, got=%q err=%v", normalized, err)
	}

	closedPR := PullRequest{Open: false, Closed: true, SourceBranch: "feature/a", TargetBranch: "master"}
	if !matchesFilters(closedPR, "closed", "refs/heads/feature/a", "master") {
		t.Fatal("expected closed pull request to match closed-state and normalized branch filters")
	}

	openPR := PullRequest{Open: true, Closed: false, SourceBranch: "feature/a", TargetBranch: "master"}
	if matchesFilters(openPR, "closed", "", "") {
		t.Fatal("expected open pull request to be excluded by closed-state filter")
	}

	if branchDisplayName(nil) != "" {
		t.Fatal("expected empty branch display for nil ref")
	}
	if branchDisplayName(&pullRequestRef{ID: "refs/heads/fallback"}) != "refs/heads/fallback" {
		t.Fatal("expected branch display to fall back to ref id when display id is missing")
	}
}

func TestPullRequestLifecycleReviewAndTaskOperations(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/22":
			_, _ = fmt.Fprint(w, `{"id":22,"title":"Feature","state":"OPEN","open":true,"closed":false,"version":2,"participants":[{"role":"REVIEWER","status":"UNAPPROVED","approved":false,"user":{"name":"reviewer1","displayName":"Reviewer One"}}],"fromRef":{"displayId":"feature/a"},"toRef":{"displayId":"master"}}`)
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/22/merge":
			_, _ = fmt.Fprint(w, `{"conflicted":false,"outcome":"CLEAN","vetoes":[]}`)
		case request.Method == http.MethodPost && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests":
			if !strings.Contains(readBody(t, request), "refs/heads/feature/new") {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = fmt.Fprint(w, `{"errors":[{"message":"missing fromRef"}]}`)
				return
			}
			_, _ = fmt.Fprint(w, `{"id":30,"title":"New PR","state":"OPEN","open":true,"closed":false,"fromRef":{"displayId":"feature/new"},"toRef":{"displayId":"master"}}`)
		case request.Method == http.MethodPut && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/30":
			_, _ = fmt.Fprint(w, `{"id":30,"title":"Updated PR","state":"OPEN","open":true,"closed":false,"version":3,"fromRef":{"displayId":"feature/new"},"toRef":{"displayId":"master"}}`)
		case request.Method == http.MethodPost && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/30/merge":
			if request.URL.Query().Get("version") != "3" {
				w.WriteHeader(http.StatusConflict)
				_, _ = fmt.Fprint(w, `{"errors":[{"message":"version required"}]}`)
				return
			}
			_, _ = fmt.Fprint(w, `{"id":30,"title":"Updated PR","state":"MERGED","open":false,"closed":true}`)
		case request.Method == http.MethodPost && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/30/decline":
			_, _ = fmt.Fprint(w, `{"id":30,"title":"Updated PR","state":"DECLINED","open":false,"closed":true}`)
		case request.Method == http.MethodPost && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/30/reopen":
			_, _ = fmt.Fprint(w, `{"id":30,"title":"Updated PR","state":"OPEN","open":true,"closed":false}`)
		case request.Method == http.MethodPost && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/30/approve":
			_, _ = fmt.Fprint(w, `{"id":30,"title":"Updated PR","state":"OPEN","open":true,"closed":false,"participants":[{"role":"REVIEWER","status":"APPROVED","approved":true,"user":{"name":"reviewer1"}}]}`)
		case request.Method == http.MethodDelete && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/30/approve":
			_, _ = fmt.Fprint(w, `{"id":30,"title":"Updated PR","state":"OPEN","open":true,"closed":false,"participants":[{"role":"REVIEWER","status":"UNAPPROVED","approved":false,"user":{"name":"reviewer1"}}]}`)
		case request.Method == http.MethodPost && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/30/participants":
			_, _ = fmt.Fprint(w, `{"id":30,"title":"Updated PR","state":"OPEN","open":true,"closed":false,"participants":[{"role":"REVIEWER","status":"UNAPPROVED","approved":false,"user":{"name":"reviewer2"}}]}`)
		case request.Method == http.MethodDelete && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/30/participants/reviewer2":
			_, _ = fmt.Fprint(w, `{"id":30,"title":"Updated PR","state":"OPEN","open":true,"closed":false,"participants":[]}`)
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/30/tasks":
			_, _ = fmt.Fprint(w, `{"isLastPage":true,"nextPageStart":0,"values":[{"id":500,"text":"Open task","state":"OPEN","resolved":false},{"id":501,"text":"Resolved task","state":"RESOLVED","resolved":true}]}`)
		case request.Method == http.MethodPost && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/30/tasks":
			_, _ = fmt.Fprint(w, `{"id":502,"text":"New task","state":"OPEN","resolved":false}`)
		case request.Method == http.MethodPut && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/30/tasks/501":
			_, _ = fmt.Fprint(w, `{"id":501,"text":"Resolved task updated","state":"RESOLVED","resolved":true}`)
		case request.Method == http.MethodDelete && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/30/tasks/501":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")

	cfg, err := config.LoadFromEnv()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	service := NewService(httpclient.NewFromConfig(cfg))
	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

	fetched, err := service.Get(context.Background(), repo, "22")
	if err != nil {
		t.Fatalf("expected get to succeed, got: %v", err)
	}
	if len(fetched.Reviewers) != 1 || fetched.Reviewers[0].Name != "reviewer1" {
		t.Fatalf("expected reviewer mapping in get response, got: %#v", fetched.Reviewers)
	}
	if fetched.Mergeability == nil || !fetched.Mergeability.Mergeable || fetched.Mergeability.Outcome != "CLEAN" {
		t.Fatalf("expected mergeability mapping in get response, got: %#v", fetched.Mergeability)
	}

	created, err := service.Create(context.Background(), repo, CreateInput{FromRef: "feature/new", ToRef: "master", Title: "New PR"})
	if err != nil || created.ID != 30 {
		t.Fatalf("expected create to succeed with id 30, got id=%d err=%v", created.ID, err)
	}

	updated, err := service.Update(context.Background(), repo, "30", UpdateInput{Title: "Updated PR", Version: 2})
	if err != nil || updated.Version != 3 {
		t.Fatalf("expected update to succeed with version 3, got version=%d err=%v", updated.Version, err)
	}

	merged, err := service.Merge(context.Background(), repo, "30", intPtr(3))
	if err != nil || merged.State != "MERGED" {
		t.Fatalf("expected merge to succeed, got state=%q err=%v", merged.State, err)
	}

	declined, err := service.Decline(context.Background(), repo, "30", nil)
	if err != nil || declined.State != "DECLINED" {
		t.Fatalf("expected decline to succeed, got state=%q err=%v", declined.State, err)
	}

	reopened, err := service.Reopen(context.Background(), repo, "30", nil)
	if err != nil || reopened.State != "OPEN" {
		t.Fatalf("expected reopen to succeed, got state=%q err=%v", reopened.State, err)
	}

	approved, err := service.Approve(context.Background(), repo, "30")
	if err != nil || len(approved.Reviewers) != 1 || !approved.Reviewers[0].Approved {
		t.Fatalf("expected approve to set reviewer approval, got reviewers=%#v err=%v", approved.Reviewers, err)
	}

	unapproved, err := service.Unapprove(context.Background(), repo, "30")
	if err != nil || len(unapproved.Reviewers) != 1 || unapproved.Reviewers[0].Approved {
		t.Fatalf("expected unapprove to clear reviewer approval, got reviewers=%#v err=%v", unapproved.Reviewers, err)
	}

	withReviewer, err := service.AddReviewer(context.Background(), repo, "30", "reviewer2")
	if err != nil || len(withReviewer.Reviewers) != 1 || withReviewer.Reviewers[0].Name != "reviewer2" {
		t.Fatalf("expected add reviewer to succeed, got reviewers=%#v err=%v", withReviewer.Reviewers, err)
	}

	withoutReviewer, err := service.RemoveReviewer(context.Background(), repo, "30", "reviewer2")
	if err != nil || len(withoutReviewer.Reviewers) != 0 {
		t.Fatalf("expected remove reviewer to succeed, got reviewers=%#v err=%v", withoutReviewer.Reviewers, err)
	}

	openTasks, err := service.ListTasks(context.Background(), repo, "30", TaskListOptions{State: "open", Limit: 20})
	if err != nil || len(openTasks) != 1 || openTasks[0].Resolved {
		t.Fatalf("expected open task filter to return one unresolved task, got tasks=%#v err=%v", openTasks, err)
	}

	createdTask, err := service.CreateTask(context.Background(), repo, "30", "New task")
	if err != nil || createdTask.ID != 502 {
		t.Fatalf("expected create task to succeed, got task=%#v err=%v", createdTask, err)
	}

	updatedTask, err := service.UpdateTask(context.Background(), repo, "30", "501", "Resolved task updated", boolPtr(true), nil)
	if err != nil || !updatedTask.Resolved {
		t.Fatalf("expected update task to succeed, got task=%#v err=%v", updatedTask, err)
	}

	if err := service.DeleteTask(context.Background(), repo, "30", "501", intPtr(3)); err != nil {
		t.Fatalf("expected delete task to succeed, got: %v", err)
	}
}

func TestPullRequestTaskAndUpdateValidation(t *testing.T) {
	service := NewService(httpclient.NewFromConfig(config.AppConfig{BitbucketURL: "http://localhost:7990"}))
	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

	_, err := service.Update(context.Background(), repo, "30", UpdateInput{Version: 0})
	if err == nil || apperrors.ExitCode(err) != 2 {
		t.Fatalf("expected update validation error, got: %v", err)
	}

	_, err = service.UpdateTask(context.Background(), repo, "30", "501", "", nil, nil)
	if err == nil || apperrors.ExitCode(err) != 2 {
		t.Fatalf("expected update task validation error, got: %v", err)
	}

	_, err = service.ListTasks(context.Background(), repo, "30", TaskListOptions{State: "bad"})
	if err == nil || apperrors.ExitCode(err) != 2 {
		t.Fatalf("expected task state validation error, got: %v", err)
	}
}

func TestCreatePullRequestWithReviewers(t *testing.T) {
	var receivedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests" {
			receivedBody = readBody(t, r)
			_, _ = fmt.Fprint(w, `{"id":42,"title":"Feature","state":"OPEN","open":true,"closed":false,"fromRef":{"displayId":"feature/a"},"toRef":{"displayId":"main"},"reviewers":[{"role":"REVIEWER","status":"UNAPPROVED","approved":false,"user":{"name":"alice"}},{"role":"REVIEWER","status":"UNAPPROVED","approved":false,"user":{"name":"bob"}}]}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")

	cfg, err := config.LoadFromEnv()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	service := NewService(httpclient.NewFromConfig(cfg))
	created, err := service.Create(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, CreateInput{
		FromRef:   "feature/a",
		ToRef:     "main",
		Title:     "Feature",
		Reviewers: []string{"alice", "bob"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created.ID != 42 {
		t.Fatalf("expected PR ID 42, got %d", created.ID)
	}

	// Verify the request body included reviewers
	if !strings.Contains(receivedBody, `"reviewers"`) {
		t.Fatal("expected request body to contain 'reviewers'")
	}
	if !strings.Contains(receivedBody, `"alice"`) || !strings.Contains(receivedBody, `"bob"`) {
		t.Fatal("expected request body to contain reviewer names")
	}
}

func intPtr(value int) *int {
	return &value
}

func boolPtr(value bool) *bool {
	return &value
}

func readBody(t *testing.T, request *http.Request) string {
	t.Helper()
	bodyBytes, _ := io.ReadAll(request.Body)
	_ = request.Body.Close()
	return string(bodyBytes)
}

func TestGetPRBuildStatuses(t *testing.T) {
	const prPath = "/rest/api/latest/projects/TEST/repos/demo/pull-requests/42"
	const buildStatusPath = "/rest/build-status/latest/commits/abc123"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == prPath:
			_, _ = fmt.Fprint(w, `{"id":42,"title":"My PR","state":"OPEN","open":true,"closed":false,
				"fromRef":{"displayId":"feature/x","latestCommit":"abc123"},
				"toRef":{"displayId":"main"}}`)
		case r.URL.Path == buildStatusPath:
			_, _ = fmt.Fprint(w, `{"values":[{"key":"ci/main","state":"SUCCESSFUL","url":"https://ci.example/1","name":"CI"},{"key":"ci/lint","state":"FAILED","url":"https://ci.example/2","name":"Lint"}],"isLastPage":true}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")

	cfg, err := config.LoadFromEnv()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	service := NewService(httpclient.NewFromConfig(cfg))
	statuses, err := service.GetBuildStatuses(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, "42", 25)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(statuses) != 2 {
		t.Fatalf("expected 2 build statuses, got %d", len(statuses))
	}
	if statuses[0].Key != "ci/main" || statuses[0].State != "SUCCESSFUL" {
		t.Fatalf("unexpected first status: %+v", statuses[0])
	}
	if statuses[1].Key != "ci/lint" || statuses[1].State != "FAILED" {
		t.Fatalf("unexpected second status: %+v", statuses[1])
	}
}

func TestGetPRBuildStatusesValidation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")

	cfg, _ := config.LoadFromEnv()
	service := NewService(httpclient.NewFromConfig(cfg))

	// Missing repository ref
	_, err := service.GetBuildStatuses(context.Background(), RepositoryRef{}, "1", 25)
	if err == nil || apperrors.ExitCode(err) != 2 {
		t.Fatalf("expected validation error for missing repo ref, got: %v", err)
	}

	// Invalid PR ID
	_, err = service.GetBuildStatuses(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, "bad", 25)
	if err == nil || apperrors.ExitCode(err) != 2 {
		t.Fatalf("expected validation error for non-numeric PR ID, got: %v", err)
	}
}

func TestGetPRBuildStatusesMissingCommit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/pull-requests/99") {
			// PR response without a latestCommit
			_, _ = fmt.Fprint(w, `{"id":99,"title":"No Commit PR","state":"OPEN","open":true,"fromRef":{"displayId":"branch"},"toRef":{"displayId":"main"}}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")

	cfg, _ := config.LoadFromEnv()
	service := NewService(httpclient.NewFromConfig(cfg))

	_, err := service.GetBuildStatuses(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, "99", 25)
	if err == nil {
		t.Fatal("expected error when PR has no source commit")
	}
}

func TestGetBuildStatusesPagination(t *testing.T) {
	const prPath = "/rest/api/latest/projects/TEST/repos/demo/pull-requests/7"
	const buildStatusPath = "/rest/build-status/latest/commits/deadbeef"

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == prPath:
			_, _ = fmt.Fprint(w, `{"id":7,"title":"Paginated","state":"OPEN","open":true,"fromRef":{"displayId":"f","latestCommit":"deadbeef"},"toRef":{"displayId":"main"}}`)
		case r.URL.Path == buildStatusPath:
			callCount++
			if callCount == 1 {
				// First page: not last, next starts at 1
				_, _ = fmt.Fprint(w, `{"values":[{"key":"ci/a","state":"SUCCESSFUL","url":"u1"}],"isLastPage":false,"nextPageStart":1}`)
			} else {
				// Second page: last
				_, _ = fmt.Fprint(w, `{"values":[{"key":"ci/b","state":"FAILED","url":"u2"}],"isLastPage":true,"nextPageStart":2}`)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")

	cfg, err := config.LoadFromEnv()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	service := NewService(httpclient.NewFromConfig(cfg))
	statuses, err := service.GetBuildStatuses(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, "7", 25)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(statuses) != 2 {
		t.Fatalf("expected 2 statuses from paginated response, got %d", len(statuses))
	}
	if statuses[0].Key != "ci/a" || statuses[1].Key != "ci/b" {
		t.Fatalf("unexpected statuses: %+v", statuses)
	}
}

func TestGetBuildStatusesDefaultLimit(t *testing.T) {
	const prPath = "/rest/api/latest/projects/TEST/repos/demo/pull-requests/5"
	const buildStatusPath = "/rest/build-status/latest/commits/abc999"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == prPath:
			_, _ = fmt.Fprint(w, `{"id":5,"state":"OPEN","open":true,"fromRef":{"displayId":"f","latestCommit":"abc999"},"toRef":{"displayId":"main"}}`)
		case r.URL.Path == buildStatusPath:
			_, _ = fmt.Fprint(w, `{"values":[],"isLastPage":true}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")

	cfg, _ := config.LoadFromEnv()
	service := NewService(httpclient.NewFromConfig(cfg))

	// limit <= 0 → defaults to 25 internally
	statuses, err := service.GetBuildStatuses(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, "5", 0)
	if err != nil {
		t.Fatalf("unexpected error with limit=0: %v", err)
	}
	if len(statuses) != 0 {
		t.Fatalf("expected empty statuses, got %d", len(statuses))
	}
}

func TestGetBuildStatusesPaginationStuck(t *testing.T) {
	const prPath = "/rest/api/latest/projects/TEST/repos/demo/pull-requests/6"
	const buildStatusPath = "/rest/build-status/latest/commits/fff111"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == prPath:
			_, _ = fmt.Fprint(w, `{"id":6,"state":"OPEN","open":true,"fromRef":{"displayId":"f","latestCommit":"fff111"},"toRef":{"displayId":"main"}}`)
		case r.URL.Path == buildStatusPath:
			// isLastPage=false but nextPageStart=0 (same as current start) → break guard
			_, _ = fmt.Fprint(w, `{"values":[{"key":"ci/x","state":"RUNNING","url":"u"}],"isLastPage":false,"nextPageStart":0}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")

	cfg, _ := config.LoadFromEnv()
	service := NewService(httpclient.NewFromConfig(cfg))
	statuses, err := service.GetBuildStatuses(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, "6", 25)
	if err != nil {
		t.Fatalf("unexpected error with stuck pagination: %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status before stuck pagination break, got %d", len(statuses))
	}
}

func TestGetBuildStatusesPRFetchError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return a non-JSON response for any PR request to cause GetJSON to fail
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, "internal server error")
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")

	cfg, _ := config.LoadFromEnv()
	service := NewService(httpclient.NewFromConfig(cfg))
	_, err := service.GetBuildStatuses(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, "10", 25)
	if err == nil {
		t.Fatal("expected error when PR fetch returns 500")
	}
}

func TestGetPullRequestIgnoresMissingMergeabilityEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/rest/api/latest/projects/TEST/repos/demo/pull-requests/22":
			_, _ = fmt.Fprint(w, `{"id":22,"title":"Feature","state":"OPEN","open":true,"closed":false,"fromRef":{"displayId":"feature/a"},"toRef":{"displayId":"master"}}`)
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")

	cfg, err := config.LoadFromEnv()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	service := NewService(httpclient.NewFromConfig(cfg))
	fetched, err := service.Get(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, "22")
	if err != nil {
		t.Fatalf("expected get to succeed when mergeability endpoint is missing, got: %v", err)
	}
	if fetched.Mergeability != nil {
		t.Fatalf("expected mergeability to be omitted when endpoint is unavailable, got: %#v", fetched.Mergeability)
	}
}

func TestGetPullRequestIgnoresConflictMergeabilityEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/rest/api/latest/projects/TEST/repos/demo/pull-requests/22":
			_, _ = fmt.Fprint(w, `{"id":22,"title":"Feature","state":"OPEN","open":true,"closed":false,"fromRef":{"displayId":"feature/a"},"toRef":{"displayId":"master"}}`)
		case "/rest/api/latest/projects/TEST/repos/demo/pull-requests/22/merge":
			w.WriteHeader(http.StatusConflict)
			_, _ = fmt.Fprint(w, `{"errors":[{"message":"mergeability unavailable"}]}`)
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")

	cfg, err := config.LoadFromEnv()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	service := NewService(httpclient.NewFromConfig(cfg))
	fetched, err := service.Get(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, "22")
	if err != nil {
		t.Fatalf("expected get to succeed when mergeability endpoint conflicts, got: %v", err)
	}
	if fetched.Mergeability != nil {
		t.Fatalf("expected mergeability to be omitted on mergeability conflict, got: %#v", fetched.Mergeability)
	}
}

func TestGetPullRequestReturnsMergeabilityError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/rest/api/latest/projects/TEST/repos/demo/pull-requests/22":
			_, _ = fmt.Fprint(w, `{"id":22,"title":"Feature","state":"OPEN","open":true,"closed":false,"fromRef":{"displayId":"feature/a"},"toRef":{"displayId":"master"}}`)
		case "/rest/api/latest/projects/TEST/repos/demo/pull-requests/22/merge":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = fmt.Fprint(w, `{"errors":[{"message":"boom"}]}`)
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")

	cfg, err := config.LoadFromEnv()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	service := NewService(httpclient.NewFromConfig(cfg))
	_, err = service.Get(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, "22")
	if err == nil {
		t.Fatal("expected mergeability error to be returned")
	}
}

func TestGetPullRequestClosedSkipsMergeability(t *testing.T) {
	mergeCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/rest/api/latest/projects/TEST/repos/demo/pull-requests/22":
			_, _ = fmt.Fprint(w, `{"id":22,"title":"Feature","state":"MERGED","open":false,"closed":true,"fromRef":{"displayId":"feature/a"},"toRef":{"displayId":"master"}}`)
		case "/rest/api/latest/projects/TEST/repos/demo/pull-requests/22/merge":
			mergeCalls++
			_, _ = fmt.Fprint(w, `{"conflicted":false,"outcome":"CLEAN","vetoes":[]}`)
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")

	cfg, err := config.LoadFromEnv()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	service := NewService(httpclient.NewFromConfig(cfg))
	fetched, err := service.Get(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, "22")
	if err != nil {
		t.Fatalf("expected closed get to succeed, got: %v", err)
	}
	if fetched.Mergeability != nil {
		t.Fatalf("expected closed pull request to omit mergeability, got: %#v", fetched.Mergeability)
	}
	if mergeCalls != 0 {
		t.Fatalf("expected closed pull request to skip mergeability lookup, got %d calls", mergeCalls)
	}
}

func TestMapMergeabilityBranches(t *testing.T) {
	mapped := mapMergeability(mergeabilityValue{
		Outcome:    "UNKNOWN",
		Conflicted: false,
		Vetoes: []mergeVetoValue{
			{SummaryMessage: "", DetailedMessage: "detail only blocker"},
			{SummaryMessage: " ", DetailedMessage: " "},
		},
	})

	if mapped.Mergeable {
		t.Fatalf("expected unknown outcome with blocker to be non-mergeable, got %#v", mapped)
	}
	if mapped.Outcome != "UNKNOWN" {
		t.Fatalf("expected outcome UNKNOWN, got %#v", mapped)
	}
	if len(mapped.Blockers) != 1 {
		t.Fatalf("expected blank vetoes to be skipped, got %#v", mapped.Blockers)
	}
	if mapped.Blockers[0].Summary != "" || mapped.Blockers[0].Detail != "detail only blocker" {
		t.Fatalf("expected detail-only blocker to be preserved, got %#v", mapped.Blockers[0])
	}
}

func TestBuildCreatePayloadWithReviewers(t *testing.T) {
	payload, err := buildCreatePayload(CreateInput{
		FromRef:   "feature/my-work",
		ToRef:     "main",
		Title:     "My PR",
		Reviewers: []string{"alice", "bob"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if payload["title"] != "My PR" {
		t.Fatalf("expected title 'My PR', got %v", payload["title"])
	}

	reviewers, ok := payload["reviewers"].([]map[string]any)
	if !ok {
		t.Fatal("expected reviewers to be []map[string]any")
	}
	if len(reviewers) != 2 {
		t.Fatalf("expected 2 reviewers, got %d", len(reviewers))
	}

	firstUser := reviewers[0]["user"].(map[string]any)
	if firstUser["name"] != "alice" {
		t.Fatalf("expected first reviewer 'alice', got %v", firstUser["name"])
	}
	if reviewers[0]["role"] != "REVIEWER" {
		t.Fatalf("expected role 'REVIEWER', got %v", reviewers[0]["role"])
	}

	secondUser := reviewers[1]["user"].(map[string]any)
	if secondUser["name"] != "bob" {
		t.Fatalf("expected second reviewer 'bob', got %v", secondUser["name"])
	}
}

func TestBuildCreatePayloadWithoutReviewers(t *testing.T) {
	payload, err := buildCreatePayload(CreateInput{
		FromRef: "feature/my-work",
		ToRef:   "main",
		Title:   "My PR",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, exists := payload["reviewers"]; exists {
		t.Fatal("expected no reviewers key when none provided")
	}
}

func TestBuildCreatePayloadWithBlankReviewers(t *testing.T) {
	payload, err := buildCreatePayload(CreateInput{
		FromRef:   "feature/my-work",
		ToRef:     "main",
		Title:     "My PR",
		Reviewers: []string{"alice", "", "  ", "bob"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reviewers := payload["reviewers"].([]map[string]any)
	if len(reviewers) != 2 {
		t.Fatalf("expected 2 reviewers (blank entries skipped), got %d", len(reviewers))
	}
}

func TestBuildCreatePayloadWithDraft(t *testing.T) {
	payload, err := buildCreatePayload(CreateInput{
		FromRef: "feature/my-work",
		ToRef:   "main",
		Title:   "My PR",
		Draft:   true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	draft, ok := payload["draft"].(bool)
	if !ok || !draft {
		t.Fatalf("expected draft=true in payload, got %v", payload["draft"])
	}
}

func TestBuildCreatePayloadNoDraftByDefault(t *testing.T) {
	payload, err := buildCreatePayload(CreateInput{
		FromRef: "feature/my-work",
		ToRef:   "main",
		Title:   "My PR",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, exists := payload["draft"]; exists {
		t.Fatalf("expected no draft key when Draft=false, got %v", payload["draft"])
	}
}

func TestBuildUpdatePayloadWithDraft(t *testing.T) {
	trueVal := true
	payload, err := buildUpdatePayload(UpdateInput{
		Title:   "Updated title",
		Version: 1,
		Draft:   &trueVal,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if payload["draft"] != true {
		t.Fatalf("expected draft=true in update payload, got %v", payload["draft"])
	}
}

func TestBuildUpdatePayloadDraftOnlyRequiresVersion(t *testing.T) {
	falseVal := false
	payload, err := buildUpdatePayload(UpdateInput{
		Version: 2,
		Draft:   &falseVal,
	})
	if err != nil {
		t.Fatalf("expected draft-only update to succeed, got: %v", err)
	}

	if payload["draft"] != false {
		t.Fatalf("expected draft=false in update payload, got %v", payload["draft"])
	}
	if payload["version"] != 2 {
		t.Fatalf("expected version=2 in update payload, got %v", payload["version"])
	}
}

func TestBuildUpdatePayloadValidationRequiresField(t *testing.T) {
	_, err := buildUpdatePayload(UpdateInput{Version: 1})
	if err == nil || apperrors.ExitCode(err) != 2 {
		t.Fatalf("expected validation error exit code 2 when no fields set, got: %v", err)
	}
}

func TestCreateDraftPullRequest(t *testing.T) {
	var receivedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests" {
			receivedBody = readBody(t, r)
			_, _ = fmt.Fprint(w, `{"id":50,"title":"Draft PR","state":"OPEN","open":true,"closed":false,"draft":true,"fromRef":{"displayId":"feature/draft"},"toRef":{"displayId":"main"}}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")

	cfg, err := config.LoadFromEnv()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	service := NewService(httpclient.NewFromConfig(cfg))
	created, err := service.Create(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, CreateInput{
		FromRef: "feature/draft",
		ToRef:   "main",
		Title:   "Draft PR",
		Draft:   true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !created.Draft {
		t.Fatal("expected created PR to have Draft=true")
	}
	if !strings.Contains(receivedBody, `"draft":true`) {
		t.Fatalf("expected request body to contain draft:true, got: %s", receivedBody)
	}
}

func TestUpdateDraftPullRequest(t *testing.T) {
	var receivedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/50" {
			receivedBody = readBody(t, r)
			_, _ = fmt.Fprint(w, `{"id":50,"title":"Draft PR","state":"OPEN","open":true,"closed":false,"draft":false,"version":2,"fromRef":{"displayId":"feature/draft"},"toRef":{"displayId":"main"}}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")

	cfg, err := config.LoadFromEnv()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	falseVal := false
	service := NewService(httpclient.NewFromConfig(cfg))
	updated, err := service.Update(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, "50", UpdateInput{
		Title:   "Draft PR",
		Version: 1,
		Draft:   &falseVal,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Draft {
		t.Fatal("expected updated PR to have Draft=false")
	}
	if !strings.Contains(receivedBody, `"draft":false`) {
		t.Fatalf("expected request body to contain draft:false, got: %s", receivedBody)
	}
}

func TestAutoMergeEnableDisableGet(t *testing.T) {
	const autoMergePath = "/rest/api/latest/projects/TEST/repos/demo/pull-requests/42/auto-merge"
	autoMergeEnabled := true

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != autoMergePath {
			http.NotFound(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet:
			if !autoMergeEnabled {
				http.NotFound(w, r)
				return
			}
			_, _ = fmt.Fprint(w, `{"strategyId":"no-ff"}`)
		case http.MethodPost:
			autoMergeEnabled = true
			_, _ = fmt.Fprint(w, `{"strategyId":"rebase-ff-only"}`)
		case http.MethodDelete:
			autoMergeEnabled = false
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")

	cfg, err := config.LoadFromEnv()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	service := NewService(httpclient.NewFromConfig(cfg))
	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

	// GET when enabled
	am, err := service.GetAutoMerge(context.Background(), repo, "42")
	if err != nil {
		t.Fatalf("GetAutoMerge: unexpected error: %v", err)
	}
	if !am.Enabled || am.StrategyID != "no-ff" {
		t.Fatalf("GetAutoMerge: expected enabled=true strategy=no-ff, got %+v", am)
	}

	// Enable with a specific strategy
	enabled, err := service.EnableAutoMerge(context.Background(), repo, "42", "rebase-ff-only")
	if err != nil {
		t.Fatalf("EnableAutoMerge: unexpected error: %v", err)
	}
	if !enabled.Enabled || enabled.StrategyID != "rebase-ff-only" {
		t.Fatalf("EnableAutoMerge: expected enabled=true strategy=rebase-ff-only, got %+v", enabled)
	}

	// Disable
	if err := service.DisableAutoMerge(context.Background(), repo, "42"); err != nil {
		t.Fatalf("DisableAutoMerge: unexpected error: %v", err)
	}

	// GET after disable returns not-found → Enabled=false, no error
	am, err = service.GetAutoMerge(context.Background(), repo, "42")
	if err != nil {
		t.Fatalf("GetAutoMerge after disable: unexpected error: %v", err)
	}
	if am.Enabled {
		t.Fatalf("GetAutoMerge after disable: expected enabled=false, got %+v", am)
	}
}

func TestAutoMergeValidation(t *testing.T) {
	service := NewService(httpclient.NewFromConfig(config.AppConfig{BitbucketURL: "http://localhost:7990"}))

	_, err := service.GetAutoMerge(context.Background(), RepositoryRef{}, "1")
	if err == nil || apperrors.ExitCode(err) != 2 {
		t.Fatalf("expected validation error for missing repo ref, got: %v", err)
	}

	_, err = service.EnableAutoMerge(context.Background(), RepositoryRef{}, "1", "no-ff")
	if err == nil || apperrors.ExitCode(err) != 2 {
		t.Fatalf("expected validation error for missing repo ref in enable, got: %v", err)
	}

	err = service.DisableAutoMerge(context.Background(), RepositoryRef{}, "1")
	if err == nil || apperrors.ExitCode(err) != 2 {
		t.Fatalf("expected validation error for missing repo ref in disable, got: %v", err)
	}
}

func TestEnableAutoMergeDefaultStrategy(t *testing.T) {
	var receivedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			receivedBody = readBody(t, r)
			_, _ = fmt.Fprint(w, `{"strategyId":"no-ff"}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")

	cfg, _ := config.LoadFromEnv()
	service := NewService(httpclient.NewFromConfig(cfg))

	// Empty strategy should default to "no-ff"
	am, err := service.EnableAutoMerge(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, "1", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if am.StrategyID != "no-ff" {
		t.Fatalf("expected strategy no-ff, got %s", am.StrategyID)
	}
	if !strings.Contains(receivedBody, `"strategyId":"no-ff"`) {
		t.Fatalf("expected request body to contain no-ff, got: %s", receivedBody)
	}
}

func TestWatchUnwatchRebase(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/42/watch":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/42/watch":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/rest/git/latest/projects/TEST/repos/demo/pull-requests/42/rebase":
			_, _ = fmt.Fprint(w, `{"vetoes":[{"summaryMessage":"blocked","detailedMessage":"conflict"}]}`)
		case r.Method == http.MethodPost && r.URL.Path == "/rest/git/latest/projects/TEST/repos/demo/pull-requests/42/rebase":
			_, _ = fmt.Fprint(w, `{"refChange":{"toHash":"newhash"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := openapigenerated.NewClientWithResponses(server.URL + "/rest")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	service := NewService(nil).WithAPIClient(client)
	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

	// Test Watch
	err = service.Watch(context.Background(), repo, "42")
	if err != nil {
		t.Fatalf("unexpected error on Watch: %v", err)
	}

	// Test Unwatch
	err = service.Unwatch(context.Background(), repo, "42")
	if err != nil {
		t.Fatalf("unexpected error on Unwatch: %v", err)
	}

	// Test CanRebase
	rebaseability, err := service.CanRebase(context.Background(), repo, "42")
	if err != nil {
		t.Fatalf("unexpected error on CanRebase: %v", err)
	}
	if rebaseability == nil || rebaseability.Vetoes == nil || len(*rebaseability.Vetoes) != 1 || *(*rebaseability.Vetoes)[0].SummaryMessage != "blocked" {
		t.Fatalf("unexpected rebaseability: %+v", rebaseability)
	}

	// Test Rebase
	version := 3
	result, err := service.Rebase(context.Background(), repo, "42", &version)
	if err != nil {
		t.Fatalf("unexpected error on Rebase: %v", err)
	}
	if result == nil || result.RefChange == nil || *result.RefChange.ToHash != "newhash" {
		t.Fatalf("unexpected rebase result: %+v", result)
	}
}

func TestWatchUnwatchRebaseValidation(t *testing.T) {
	service := NewService(nil)
	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

	// Nil client error
	err := service.Watch(context.Background(), repo, "42")
	if err == nil || !strings.Contains(err.Error(), "openapi client is not configured") {
		t.Fatalf("expected nil client error, got: %v", err)
	}

	err = service.Unwatch(context.Background(), repo, "42")
	if err == nil || !strings.Contains(err.Error(), "openapi client is not configured") {
		t.Fatalf("expected nil client error, got: %v", err)
	}

	_, err = service.CanRebase(context.Background(), repo, "42")
	if err == nil || !strings.Contains(err.Error(), "openapi client is not configured") {
		t.Fatalf("expected nil client error, got: %v", err)
	}

	_, err = service.Rebase(context.Background(), repo, "42", nil)
	if err == nil || !strings.Contains(err.Error(), "openapi client is not configured") {
		t.Fatalf("expected nil client error, got: %v", err)
	}

	// Validation errors (missing repository)
	client := &openapigenerated.ClientWithResponses{}
	service.WithAPIClient(client)

	for _, op := range []string{"watch", "unwatch", "canrebase", "rebase"} {
		var err error
		if op == "watch" {
			err = service.Watch(context.Background(), RepositoryRef{}, "42")
		} else if op == "unwatch" {
			err = service.Unwatch(context.Background(), RepositoryRef{}, "42")
		} else if op == "canrebase" {
			_, err = service.CanRebase(context.Background(), RepositoryRef{}, "42")
		} else {
			_, err = service.Rebase(context.Background(), RepositoryRef{}, "42", nil)
		}
		if err == nil || apperrors.ExitCode(err) != 2 {
			t.Fatalf("expected validation error (repo) on %s, got: %v", op, err)
		}
	}

	// Validation errors (invalid PR ID)
	for _, op := range []string{"watch", "unwatch", "canrebase", "rebase"} {
		var err error
		if op == "watch" {
			err = service.Watch(context.Background(), repo, "invalid")
		} else if op == "unwatch" {
			err = service.Unwatch(context.Background(), repo, "invalid")
		} else if op == "canrebase" {
			_, err = service.CanRebase(context.Background(), repo, "invalid")
		} else {
			_, err = service.Rebase(context.Background(), repo, "invalid", nil)
		}
		if err == nil || apperrors.ExitCode(err) != 2 {
			t.Fatalf("expected validation error (pr id) on %s, got: %v", op, err)
		}
	}
}

func TestWatchUnwatchRebaseAPIErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, `{"errors":[{"message":"internal error"}]}`)
	}))
	defer server.Close()

	client, err := openapigenerated.NewClientWithResponses(server.URL + "/rest")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	service := NewService(nil).WithAPIClient(client)
	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

	// Test Watch error
	err = service.Watch(context.Background(), repo, "42")
	if err == nil {
		t.Fatalf("expected error on Watch")
	}

	// Test Unwatch error
	err = service.Unwatch(context.Background(), repo, "42")
	if err == nil {
		t.Fatalf("expected error on Unwatch")
	}

	// Test CanRebase error
	_, err = service.CanRebase(context.Background(), repo, "42")
	if err == nil {
		t.Fatalf("expected error on CanRebase")
	}

	// Test Rebase error
	_, err = service.Rebase(context.Background(), repo, "42", nil)
	if err == nil {
		t.Fatalf("expected error on Rebase")
	}
}

func TestListPullRequestsContainingCommitAndSearchParticipants(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/commits/sha123/pull-requests":
			_, _ = fmt.Fprint(w, `{"values":[{"id":42,"title":"PR Title","state":"OPEN","open":true,"closed":false,"draft":false,"version":1}]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/participants":
			if r.URL.Query().Get("filter") == "user1" {
				_, _ = fmt.Fprint(w, `{"values":[{"active":true,"displayName":"User One","emailAddress":"user1@example.com","id":1,"name":"user1","slug":"user1"}]}`)
			} else {
				_, _ = fmt.Fprint(w, `{"values":[]}`)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := openapigenerated.NewClientWithResponses(server.URL + "/rest")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	service := NewService(nil).WithAPIClient(client)
	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

	// Test ListPullRequestsContainingCommit
	prs, err := service.ListPullRequestsContainingCommit(context.Background(), repo, "sha123")
	if err != nil {
		t.Fatalf("unexpected error on ListPullRequestsContainingCommit: %v", err)
	}
	if len(prs) != 1 || prs[0].ID != 42 || prs[0].Title != "PR Title" {
		t.Fatalf("unexpected PRs: %+v", prs)
	}

	// Test SearchParticipants
	users, err := service.SearchParticipants(context.Background(), repo, "user1")
	if err != nil {
		t.Fatalf("unexpected error on SearchParticipants: %v", err)
	}
	if len(users) != 1 || users[0].Name != "user1" || users[0].DisplayName != "User One" || users[0].EmailAddress != "user1@example.com" || !users[0].Active {
		t.Fatalf("unexpected participants: %+v", users)
	}
}

func TestListPullRequestsContainingCommitAndSearchParticipantsValidation(t *testing.T) {
	service := NewService(nil)
	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

	// Nil client error
	_, err := service.ListPullRequestsContainingCommit(context.Background(), repo, "sha123")
	if err == nil || !strings.Contains(err.Error(), "openapi client is not configured") {
		t.Fatalf("expected nil client error, got: %v", err)
	}

	_, err = service.SearchParticipants(context.Background(), repo, "filter")
	if err == nil || !strings.Contains(err.Error(), "openapi client is not configured") {
		t.Fatalf("expected nil client error, got: %v", err)
	}

	// Validation errors (missing repository)
	client := &openapigenerated.ClientWithResponses{}
	service.WithAPIClient(client)

	for _, op := range []string{"commit_prs", "participants"} {
		var err error
		if op == "commit_prs" {
			_, err = service.ListPullRequestsContainingCommit(context.Background(), RepositoryRef{}, "sha123")
		} else {
			_, err = service.SearchParticipants(context.Background(), RepositoryRef{}, "filter")
		}
		if err == nil || apperrors.ExitCode(err) != 2 {
			t.Fatalf("expected validation error (repo) on %s, got: %v", op, err)
		}
	}

	// Validation errors (missing inputs)
	_, err = service.ListPullRequestsContainingCommit(context.Background(), repo, "")
	if err == nil || apperrors.ExitCode(err) != 2 {
		t.Fatalf("expected validation error (commit ID) on commit_prs, got: %v", err)
	}

	_, err = service.SearchParticipants(context.Background(), repo, "")
	if err == nil || apperrors.ExitCode(err) != 2 {
		t.Fatalf("expected validation error (filter) on participants, got: %v", err)
	}
}

func TestListPullRequestsContainingCommitAndSearchParticipantsAPIErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, `{"errors":[{"message":"internal error"}]}`)
	}))
	defer server.Close()

	client, err := openapigenerated.NewClientWithResponses(server.URL + "/rest")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	service := NewService(nil).WithAPIClient(client)
	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

	// Test commit prs error
	_, err = service.ListPullRequestsContainingCommit(context.Background(), repo, "sha123")
	if err == nil {
		t.Fatalf("expected error on ListPullRequestsContainingCommit")
	}

	// Test participants error
	_, err = service.SearchParticipants(context.Background(), repo, "filter")
	if err == nil {
		t.Fatalf("expected error on SearchParticipants")
	}
}

