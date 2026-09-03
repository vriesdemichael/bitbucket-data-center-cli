package pullrequest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
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

	results, err := service.List(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, ListOptions{State: "all", MaxResults: 1, SourceBranch: "feature/b"})
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

	results, err := service.ListDashboard(context.Background(), DashboardListOptions{State: "all", Role: "author", MaxResults: 10})
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
	_, err = service.ListDashboard(context.Background(), DashboardListOptions{State: "open", MaxResults: 10})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	_, err = service.ListDashboard(context.Background(), DashboardListOptions{State: "closed", MaxResults: 10})
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

	_, err = service.List(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, ListOptions{State: "open", MaxResults: 5})
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

func TestPullRequestLifecycleAndReviewOperations(t *testing.T) {
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
		// pr update reads the pull request before writing it, so it can echo the
		// reviewers back rather than clearing them (#511).
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/30":
			_, _ = fmt.Fprint(w, `{"id":30,"title":"New PR","state":"OPEN","open":true,"closed":false,"version":2,"reviewers":[{"user":{"name":"reviewer2"}}],"fromRef":{"displayId":"feature/new"},"toRef":{"displayId":"master"}}`)
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
}

func TestPullRequestUpdateValidation(t *testing.T) {
	service := NewService(httpclient.NewFromConfig(config.AppConfig{BitbucketURL: "http://localhost:7990"}))
	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

	_, err := service.Update(context.Background(), repo, "30", UpdateInput{Version: 0})
	if err == nil || apperrors.ExitCode(err) != 2 {
		t.Fatalf("expected update validation error, got: %v", err)
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
		// The update reads before it writes, so it can echo the reviewers back
		// rather than clearing them (#511).
		if r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/50" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"id":50,"version":1,"title":"Draft PR","state":"OPEN","open":true,"reviewers":[],"fromRef":{"displayId":"feature/x"},"toRef":{"displayId":"master"}}`)

			return
		}
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

// TestAutoMergeGetAndDisable covers the two operations that genuinely use the
// auto-merge endpoint. Enable does not: it arms through the merge endpoint, and
// is covered by TestEnableAutoMergeUsesTheMergeEndpoint.
//
// This test previously exercised Enable here too, against a stub that served
// only the auto-merge path — which is part of why #378 survived. A stub can
// confirm bb called what bb thought it should call; it cannot tell you that
// belief was wrong.
func TestAutoMergeGetAndDisable(t *testing.T) {
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

// newCommentTestService wires a service against a test server and captures the
// decoded request body of the last write it received.
func newCommentTestService(t *testing.T, handler func(w http.ResponseWriter, request *http.Request)) *Service {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(handler))
	t.Cleanup(server.Close)

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")

	cfg, err := config.LoadFromEnv()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	return NewService(httpclient.NewFromConfig(cfg))
}

func TestNeedsWorkSetsParticipantStatus(t *testing.T) {
	var gotPath string
	var gotMethod string
	var gotPayload map[string]any

	service := newCommentTestService(t, func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/rest/api/latest/users" {
			w.Header().Set("X-AUSERNAME", "alice")
			_, _ = fmt.Fprint(w, `{"values":[]}`)
			return
		}

		gotPath = request.URL.Path
		gotMethod = request.Method
		body, _ := io.ReadAll(request.Body)
		_ = json.Unmarshal(body, &gotPayload)

		_, _ = fmt.Fprint(w, `{"id":42,"title":"Feature","state":"OPEN","open":true,"closed":false,"participants":[{"role":"REVIEWER","status":"NEEDS_WORK","approved":false,"user":{"name":"alice"}}]}`)
	})

	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

	pr, err := service.NeedsWork(context.Background(), repo, "42")
	if err != nil {
		t.Fatalf("expected needs work success, got %v", err)
	}

	if gotMethod != http.MethodPut {
		t.Fatalf("expected PUT, got %s", gotMethod)
	}
	wantPath := "/rest/api/latest/projects/TEST/repos/demo/pull-requests/42/participants/alice"
	if gotPath != wantPath {
		t.Fatalf("expected path %q, got %q", wantPath, gotPath)
	}
	if gotPayload["status"] != "NEEDS_WORK" {
		t.Fatalf("expected NEEDS_WORK status payload, got %#v", gotPayload)
	}
	if len(pr.Reviewers) != 1 || pr.Reviewers[0].Status != "NEEDS_WORK" {
		t.Fatalf("expected mapped NEEDS_WORK reviewer, got %#v", pr.Reviewers)
	}
}

func TestNeedsWorkValidation(t *testing.T) {
	service := newCommentTestService(t, func(w http.ResponseWriter, request *http.Request) {
		t.Errorf("server must not be reached, got %s", request.URL.Path)
	})

	if _, err := service.NeedsWork(context.Background(), RepositoryRef{}, "42"); err == nil {
		t.Fatal("expected repository validation error")
	}
	if _, err := service.NeedsWork(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, "not-a-number"); err == nil {
		t.Fatal("expected pull request id validation error")
	}
}

func TestNeedsWorkPropagatesUserLookupFailure(t *testing.T) {
	service := newCommentTestService(t, func(w http.ResponseWriter, request *http.Request) {
		// No X-AUSERNAME header: the slug cannot be resolved.
		_, _ = fmt.Fprint(w, `{"values":[]}`)
	})

	_, err := service.NeedsWork(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, "42")
	if err == nil || !strings.Contains(err.Error(), "X-AUSERNAME") {
		t.Fatalf("expected the user lookup failure to propagate, got %v", err)
	}
}

func TestAddCommentPlainAndReply(t *testing.T) {
	var gotPath string
	var gotPayload map[string]any

	service := newCommentTestService(t, func(w http.ResponseWriter, request *http.Request) {
		gotPath = request.URL.Path
		body, _ := io.ReadAll(request.Body)
		gotPayload = map[string]any{}
		_ = json.Unmarshal(body, &gotPayload)
		_, _ = fmt.Fprint(w, `{"id":900,"version":0,"text":"looks good","author":{"name":"alice","displayName":"Alice","slug":"alice"}}`)
	})

	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

	comment, err := service.AddComment(context.Background(), repo, "42", "  looks good  ", 0)
	if err != nil {
		t.Fatalf("expected comment success, got %v", err)
	}
	if gotPath != "/rest/api/latest/projects/TEST/repos/demo/pull-requests/42/comments" {
		t.Fatalf("unexpected path %q", gotPath)
	}
	if gotPayload["text"] != "looks good" {
		t.Fatalf("expected trimmed text, got %#v", gotPayload["text"])
	}
	if _, hasParent := gotPayload["parent"]; hasParent {
		t.Fatalf("expected no parent for a top level comment, got %#v", gotPayload)
	}
	if comment.ID != 900 || comment.Author.Slug != "alice" {
		t.Fatalf("expected decoded comment, got %#v", comment)
	}

	if _, err := service.AddComment(context.Background(), repo, "42", "replying", 900); err != nil {
		t.Fatalf("expected reply success, got %v", err)
	}
	parent, ok := gotPayload["parent"].(map[string]any)
	if !ok {
		t.Fatalf("expected parent object on a reply, got %#v", gotPayload)
	}
	if id, _ := parent["id"].(float64); int64(id) != 900 {
		t.Fatalf("expected parent id 900, got %#v", parent["id"])
	}
}

func TestAddCommentValidation(t *testing.T) {
	service := newCommentTestService(t, func(w http.ResponseWriter, request *http.Request) {
		t.Errorf("server must not be reached, got %s", request.URL.Path)
	})

	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

	if _, err := service.AddComment(context.Background(), RepositoryRef{}, "42", "text", 0); err == nil {
		t.Fatal("expected repository validation error")
	}
	if _, err := service.AddComment(context.Background(), repo, "nope", "text", 0); err == nil {
		t.Fatal("expected pull request id validation error")
	}
	if _, err := service.AddComment(context.Background(), repo, "42", "   ", 0); err == nil {
		t.Fatal("expected empty text validation error")
	}
}

func TestAddInlineCommentBuildsAnchor(t *testing.T) {
	var gotPayload map[string]any

	service := newCommentTestService(t, func(w http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		gotPayload = map[string]any{}
		_ = json.Unmarshal(body, &gotPayload)
		_, _ = fmt.Fprint(w, `{"id":901,"version":0,"text":"nit"}`)
	})

	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

	if _, err := service.AddInlineComment(context.Background(), repo, "42", "nit", InlineCommentAnchor{
		Line: 12,
		Path: " src/main.go ",
	}); err != nil {
		t.Fatalf("expected inline comment success, got %v", err)
	}

	anchor, ok := gotPayload["anchor"].(map[string]any)
	if !ok {
		t.Fatalf("expected an anchor object, got %#v", gotPayload)
	}
	if line, _ := anchor["line"].(float64); int(line) != 12 {
		t.Fatalf("expected line 12, got %#v", anchor["line"])
	}
	if anchor["path"] != "src/main.go" {
		t.Fatalf("expected trimmed path, got %#v", anchor["path"])
	}
	if anchor["lineType"] != "ADDED" {
		t.Fatalf("expected ADDED to be the default lineType, got %#v", anchor["lineType"])
	}
	if anchor["fileType"] != "TO" {
		t.Fatalf("expected fileType TO for an added line, got %#v", anchor["fileType"])
	}
	if anchor["diffType"] != "EFFECTIVE" {
		t.Fatalf("expected diffType EFFECTIVE, got %#v", anchor["diffType"])
	}
}

func TestAddInlineCommentRemovedLineAnchorsToOriginalFile(t *testing.T) {
	var gotPayload map[string]any

	service := newCommentTestService(t, func(w http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		gotPayload = map[string]any{}
		_ = json.Unmarshal(body, &gotPayload)
		_, _ = fmt.Fprint(w, `{"id":902}`)
	})

	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

	if _, err := service.AddInlineComment(context.Background(), repo, "42", "why drop this?", InlineCommentAnchor{
		Line:     3,
		Path:     "src/main.go",
		LineType: "removed",
	}); err != nil {
		t.Fatalf("expected inline comment success, got %v", err)
	}

	anchor, _ := gotPayload["anchor"].(map[string]any)
	if anchor["lineType"] != "REMOVED" {
		t.Fatalf("expected lineType to be upper-cased to REMOVED, got %#v", anchor["lineType"])
	}
	if anchor["fileType"] != "FROM" {
		t.Fatalf("expected fileType FROM for a removed line, got %#v", anchor["fileType"])
	}
}

func TestAddInlineCommentValidation(t *testing.T) {
	service := newCommentTestService(t, func(w http.ResponseWriter, request *http.Request) {
		t.Errorf("server must not be reached, got %s", request.URL.Path)
	})

	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}
	valid := InlineCommentAnchor{Line: 1, Path: "a.go"}

	cases := []struct {
		name   string
		repo   RepositoryRef
		prID   string
		text   string
		anchor InlineCommentAnchor
	}{
		{name: "bad repository", repo: RepositoryRef{}, prID: "42", text: "t", anchor: valid},
		{name: "bad pull request id", repo: repo, prID: "nope", text: "t", anchor: valid},
		{name: "empty text", repo: repo, prID: "42", text: "  ", anchor: valid},
		{name: "missing path", repo: repo, prID: "42", text: "t", anchor: InlineCommentAnchor{Line: 1, Path: "  "}},
		{name: "zero line", repo: repo, prID: "42", text: "t", anchor: InlineCommentAnchor{Line: 0, Path: "a.go"}},
		{name: "negative line", repo: repo, prID: "42", text: "t", anchor: InlineCommentAnchor{Line: -3, Path: "a.go"}},
		{name: "unknown line type", repo: repo, prID: "42", text: "t", anchor: InlineCommentAnchor{Line: 1, Path: "a.go", LineType: "SIDEWAYS"}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := service.AddInlineComment(context.Background(), testCase.repo, testCase.prID, testCase.text, testCase.anchor)
			if err == nil {
				t.Fatal("expected a validation error")
			}
			if apperrors.ExitCode(err) != 2 {
				t.Fatalf("expected validation exit code 2, got %d: %v", apperrors.ExitCode(err), err)
			}
		})
	}
}

func TestNormalizeLineType(t *testing.T) {
	cases := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{input: "", want: "ADDED"},
		{input: "   ", want: "ADDED"},
		{input: "added", want: "ADDED"},
		{input: " Removed ", want: "REMOVED"},
		{input: "CONTEXT", want: "CONTEXT"},
		{input: "bogus", wantErr: true},
	}

	for _, testCase := range cases {
		got, err := normalizeLineType(testCase.input)
		if testCase.wantErr {
			if err == nil {
				t.Fatalf("expected error for %q", testCase.input)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", testCase.input, err)
		}
		if got != testCase.want {
			t.Fatalf("normalizeLineType(%q) = %q, want %q", testCase.input, got, testCase.want)
		}
	}
}

// TestMapReviewersPrefersParticipants pins the fallback behaviour: Bitbucket
// Data Center 9.4.16 returns an empty "participants" array and populates
// "reviewers" instead, but servers that do populate "participants" must keep
// working from that field.
func TestMapReviewersPrefersParticipants(t *testing.T) {
	participant := pullRequestParticipant{
		Role:     "REVIEWER",
		Status:   "APPROVED",
		Approved: true,
		User:     &pullRequestUserIdentity{Name: "from-participants"},
	}
	reviewer := pullRequestParticipant{
		Role:     "REVIEWER",
		Status:   "UNAPPROVED",
		Approved: false,
		User:     &pullRequestUserIdentity{Name: "from-reviewers"},
	}

	t.Run("participants win when present", func(t *testing.T) {
		got := mapReviewers([]pullRequestParticipant{participant}, []pullRequestParticipant{reviewer})
		if len(got) != 1 || got[0].Name != "from-participants" {
			t.Fatalf("expected participants to take precedence, got %#v", got)
		}
	})

	t.Run("reviewers used when participants empty", func(t *testing.T) {
		got := mapReviewers(nil, []pullRequestParticipant{reviewer})
		if len(got) != 1 || got[0].Name != "from-reviewers" {
			t.Fatalf("expected the reviewers fallback, got %#v", got)
		}
	})

	t.Run("both empty", func(t *testing.T) {
		if got := mapReviewers(nil, nil); got != nil {
			t.Fatalf("expected nil, got %#v", got)
		}
	})

	t.Run("authors filtered out", func(t *testing.T) {
		author := pullRequestParticipant{
			Role: "AUTHOR",
			User: &pullRequestUserIdentity{Name: "author"},
		}
		if got := mapReviewers(nil, []pullRequestParticipant{author}); got != nil {
			t.Fatalf("expected authors to be filtered out, got %#v", got)
		}
	})

	t.Run("entries without a user skipped", func(t *testing.T) {
		if got := mapReviewers([]pullRequestParticipant{{Role: "REVIEWER"}}, nil); got != nil {
			t.Fatalf("expected entries without a user to be skipped, got %#v", got)
		}
	})
}

func TestListPullRequestsAppliesRoleFilter(t *testing.T) {
	var gotRole string

	service := newCommentTestService(t, func(w http.ResponseWriter, request *http.Request) {
		gotRole = request.URL.Query().Get("role")
		_, _ = fmt.Fprint(w, `{"values":[],"isLastPage":true}`)
	})

	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

	if _, err := service.List(context.Background(), repo, ListOptions{Role: "reviewer", MaxResults: 5}); err != nil {
		t.Fatalf("expected list success, got %v", err)
	}
	if gotRole != "REVIEWER" {
		t.Fatalf("expected role to be upper-cased to REVIEWER, got %q", gotRole)
	}
}

// TestListCapsResultsAtLimit is the defect from #323.
//
// List used to treat Limit purely as a page size: it walked every page and
// returned all of them, so --limit 10 against a repository with more pull
// requests than that returned every one. Every other list service in the
// project caps, and the help text described this one as capping too.
func TestListCapsResultsAtLimit(t *testing.T) {
	pageSize := 10
	total := 47
	pagesServed := 0

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		pagesServed++

		start, _ := strconv.Atoi(request.URL.Query().Get("start"))
		values := make([]map[string]any, 0, pageSize)
		for index := start; index < start+pageSize && index < total; index++ {
			values = append(values, map[string]any{
				"id":    index + 1,
				"title": "pr",
				"state": "OPEN",
				"open":  true,
			})
		}

		nextStart := start + pageSize
		isLast := nextStart >= total

		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"values":        values,
			"isLastPage":    isLast,
			"nextPageStart": nextStart,
		})
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")

	cfg, err := config.LoadFromEnv()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	service := NewService(httpclient.NewFromConfig(cfg))

	results, err := service.List(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "repo"}, ListOptions{
		State:      "open",
		MaxResults: pageSize,
	})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(results) != pageSize {
		t.Fatalf("expected %d results, got %d — Limit is not capping", pageSize, len(results))
	}

	// It must also stop fetching once satisfied, rather than walking to the
	// last page and discarding the rest.
	if pagesServed != 1 {
		t.Fatalf("expected 1 page request, got %d", pagesServed)
	}
}

func TestListReturnsEverythingBelowTheLimit(t *testing.T) {
	total := 3

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		values := make([]map[string]any, 0, total)
		for index := 0; index < total; index++ {
			values = append(values, map[string]any{"id": index + 1, "title": "pr", "state": "OPEN", "open": true})
		}

		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{"values": values, "isLastPage": true, "nextPageStart": total})
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")

	cfg, err := config.LoadFromEnv()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	service := NewService(httpclient.NewFromConfig(cfg))

	results, err := service.List(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "repo"}, ListOptions{State: "open", MaxResults: 25})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(results) != total {
		t.Fatalf("expected %d results, got %d", total, len(results))
	}
}

// TestListTrimsAnOvershootingPage covers the case where a page carries more
// results than --limit asked for.
//
// The loop can only stop between pages, so a page larger than the remaining
// allowance overshoots and the surplus has to be trimmed. Without the trim,
// `bb pr list --limit 7` against a server paging in tens returns 10.
func TestListTrimsAnOvershootingPage(t *testing.T) {
	const (
		serverPageSize = 10
		requested      = 7
	)

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		values := make([]map[string]any, 0, serverPageSize)
		for index := 0; index < serverPageSize; index++ {
			values = append(values, map[string]any{"id": index + 1, "title": "pr", "state": "OPEN", "open": true})
		}

		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"values":        values,
			"isLastPage":    false,
			"nextPageStart": serverPageSize,
		})
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")

	cfg, err := config.LoadFromEnv()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	service := NewService(httpclient.NewFromConfig(cfg))

	results, err := service.List(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "repo"}, ListOptions{
		State:      "open",
		MaxResults: requested,
	})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(results) != requested {
		t.Fatalf("expected %d results after trimming an oversized page, got %d", requested, len(results))
	}
}

// TestListDashboardParticipantStatus pins the query parameter that makes
// "requesting your review" mean it.
//
// role=REVIEWER alone returns every pull request you are a reviewer on,
// including the ones you already answered. participantStatus is what narrows
// it, and it has to reach the wire uppercased and trimmed — the API matches on
// the literal enum names.
func TestListDashboardParticipantStatus(t *testing.T) {
	var received []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/rest/api/1.0/dashboard/pull-requests" {
			http.NotFound(w, request)
			return
		}

		received = append(received, request.URL.Query().Get("participantStatus"))
		_, _ = fmt.Fprint(w, `{"values":[],"isLastPage":true}`)
	}))
	defer server.Close()

	cfg := config.AppConfig{BitbucketURL: server.URL}
	service := NewService(httpclient.NewFromConfig(cfg))

	cases := []struct {
		option   string
		expected string
	}{
		{option: "UNAPPROVED", expected: "UNAPPROVED"},
		{option: "  unapproved,needs_work  ", expected: "UNAPPROVED,NEEDS_WORK"},
		{option: "", expected: ""},
		{option: "   ", expected: ""},
	}

	for _, testCase := range cases {
		received = nil
		if _, err := service.ListDashboard(context.Background(), DashboardListOptions{
			State:             "open",
			Role:              "reviewer",
			ParticipantStatus: testCase.option,
			MaxResults:        10,
		}); err != nil {
			t.Fatalf("list dashboard with participant status %q failed: %v", testCase.option, err)
		}
		if len(received) != 1 || received[0] != testCase.expected {
			t.Fatalf("participant status %q: expected %q on the wire, got %v", testCase.option, testCase.expected, received)
		}
	}
}

// TestEnableAutoMergeUsesTheMergeEndpoint is the regression guard for #378.
//
// bb posted to .../auto-merge, which the 10.2 spec documents as "requests the
// system to try merging the pull request if auto-merge was requested on it" —
// a retry of an existing request. Arming one that way always failed with
// AutoMergeNotRequestedException, so there was no way to arm auto-merge from
// the CLI at all.
//
// Arming is RestPullRequestMergeRequest.autoMerge on POST .../merge, in the
// body rather than the query string. This asserts the path and the body,
// because those are precisely what was wrong.
func TestEnableAutoMergeUsesTheMergeEndpoint(t *testing.T) {
	var mergePath string
	var mergeBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/pull-requests/42"):
			// The version the merge endpoint needs for optimistic locking.
			_, _ = fmt.Fprint(w, `{"id":42,"title":"t","state":"OPEN","open":true,"version":7}`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/pull-requests/42/merge"):
			mergePath = r.URL.Path
			_ = json.NewDecoder(r.Body).Decode(&mergeBody)
			_, _ = fmt.Fprint(w, `{"id":42,"title":"t","state":"OPEN","open":true,"version":8}`)
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/auto-merge"):
			// The endpoint the bug used. Reaching it is the failure.
			t.Errorf("enable must not post to the auto-merge endpoint: %s", r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	service := NewService(httpclient.NewFromConfig(config.AppConfig{BitbucketURL: server.URL}))

	result, err := service.EnableAutoMerge(context.Background(), RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}, "42", "no-ff")
	if err != nil {
		t.Fatalf("enable auto-merge failed: %v", err)
	}

	if !strings.HasSuffix(mergePath, "/pull-requests/42/merge") {
		t.Fatalf("expected the merge endpoint, got %q", mergePath)
	}
	if mergeBody["autoMerge"] != true {
		t.Fatalf("expected autoMerge=true in the body, got %#v", mergeBody)
	}
	if mergeBody["strategyId"] != "no-ff" {
		t.Fatalf("expected the strategy in the body, got %#v", mergeBody)
	}
	// Without the version the server rejects the merge on optimistic locking,
	// and the caller never supplied one — it is resolved here.
	if mergeBody["version"] != float64(7) {
		t.Fatalf("expected the current version in the body, got %#v", mergeBody["version"])
	}

	if !result.Enabled || result.MergedImmediately {
		t.Fatalf("expected auto-merge to be armed and pending, got %#v", result)
	}
}

// TestEnableAutoMergeReportsAnImmediateMerge covers the other outcome: a pull
// request whose checks already pass merges on the spot. Reporting it as
// "enabled" would describe a pending state that will never fire.
func TestEnableAutoMergeReportsAnImmediateMerge(t *testing.T) {
	var mergeBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/pull-requests/42"):
			_, _ = fmt.Fprint(w, `{"id":42,"title":"t","state":"OPEN","open":true,"version":1}`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/merge"):
			_ = json.NewDecoder(r.Body).Decode(&mergeBody)
			_, _ = fmt.Fprint(w, `{"id":42,"title":"t","state":"MERGED","open":false,"closed":true,"version":2}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	service := NewService(httpclient.NewFromConfig(config.AppConfig{BitbucketURL: server.URL}))

	result, err := service.EnableAutoMerge(context.Background(), RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}, "42", "")
	if err != nil {
		t.Fatalf("enable auto-merge failed: %v", err)
	}

	if !result.MergedImmediately {
		t.Fatalf("expected an immediate merge to be reported, got %#v", result)
	}
	if result.Enabled {
		t.Fatalf("a merged pull request has no pending auto-merge, got %#v", result)
	}
	// The default strategy is reported and, more importantly, actually sent.
	// The test this replaced asserted the body but accepted any POST path, so it
	// passed while bb was calling the wrong endpoint entirely.
	if result.StrategyID != "no-ff" {
		t.Fatalf("expected the default strategy, got %#v", result)
	}
	if mergeBody["strategyId"] != "no-ff" {
		t.Fatalf("expected the defaulted strategy in the request body, got %#v", mergeBody)
	}
}

// TestEnableAutoMergeSurfacesFailures covers the two ways arming can fail
// before it reports success. Both matter: the version lookup is a round trip
// the caller never asked for, so a failure there has to say so rather than
// look like the merge itself was refused.
func TestEnableAutoMergeSurfacesFailures(t *testing.T) {
	t.Run("version lookup fails", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprint(w, `{"errors":[{"message":"no such pull request"}]}`)
		}))
		defer server.Close()

		service := NewService(httpclient.NewFromConfig(config.AppConfig{BitbucketURL: server.URL}))
		if _, err := service.EnableAutoMerge(context.Background(), RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}, "42", ""); err == nil {
			t.Fatal("expected the pull request lookup failure to surface")
		}
	})

	t.Run("merge is refused", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.Method == http.MethodGet {
				_, _ = fmt.Fprint(w, `{"id":42,"title":"t","state":"OPEN","open":true,"version":3}`)
				return
			}
			// What the server says when the repository does not permit
			// auto-merge at all.
			w.WriteHeader(http.StatusForbidden)
			_, _ = fmt.Fprint(w, `{"errors":[{"message":"auto-merge is not enabled for this repository"}]}`)
		}))
		defer server.Close()

		service := NewService(httpclient.NewFromConfig(config.AppConfig{BitbucketURL: server.URL}))
		_, err := service.EnableAutoMerge(context.Background(), RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}, "42", "")
		if err == nil {
			t.Fatal("expected the refused merge to surface")
		}
		if !strings.Contains(err.Error(), "auto-merge is not enabled") {
			t.Fatalf("expected the server's reason to be preserved, got: %v", err)
		}
	})
}

// TestRebaseRejectsOutOfRangeVersion covers the bound on the pull request
// version sent with a rebase.
//
// The API field is 32 bits. A version outside its range used to wrap, which
// would have rebased against a different version than the caller named — the
// one case where silently using the wrong number is worse than refusing.
func TestRebaseRejectsOutOfRangeVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client, err := openapigenerated.NewClientWithResponses(server.URL + "/rest")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	service := NewService(nil).WithAPIClient(client)
	repo := RepositoryRef{ProjectKey: "PRJ", Slug: "repo"}

	for _, version := range []int{-1, math.MaxInt32 + 1} {
		if _, err := service.Rebase(context.Background(), repo, "42", &version); err == nil {
			t.Fatalf("expected version %d to be rejected", version)
		} else if !apperrors.IsKind(err, apperrors.KindValidation) {
			t.Fatalf("expected KindValidation for %d, got %v", version, err)
		}
	}
}

// TestUpdatePreservesReviewers is #511.
//
// A PUT replaces the pull request, and Bitbucket reads an absent reviewers key
// as "no reviewers" rather than "unchanged". Confirmed against a running Data
// Center: a body of version plus description emptied a reviewer set, and the
// same body with the set echoed back left it intact. `bb pr update <id>
// --description ...` therefore removed every reviewer, with nothing said.
func TestUpdatePreservesReviewers(t *testing.T) {
	var sent map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")

		if request.Method == http.MethodGet {
			_, _ = writer.Write([]byte(`{"id":42,"version":1,"title":"T","state":"OPEN",
				"reviewers":[{"user":{"name":"alice"}},{"user":{"name":"bob"}}],
				"fromRef":{"displayId":"feat"},"toRef":{"displayId":"main"}}`))

			return
		}

		_ = json.NewDecoder(request.Body).Decode(&sent)
		_, _ = writer.Write([]byte(`{"id":42,"version":2,"title":"T","state":"OPEN"}`))
	}))
	defer server.Close()

	service := NewService(httpclient.NewFromConfig(config.AppConfig{BitbucketURL: server.URL}))

	t.Run("an update that says nothing about reviewers keeps them", func(t *testing.T) {
		sent = nil

		if _, err := service.Update(context.Background(), RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}, "42",
			UpdateInput{Version: 1, Description: "new text"}); err != nil {
			t.Fatalf("update: %v", err)
		}

		names := reviewerNamesFromPayload(t, sent)
		if len(names) != 2 || names[0] != "alice" || names[1] != "bob" {
			t.Errorf("reviewers sent = %v, want the existing set echoed back", names)
		}
	})

	t.Run("a caller may still set the reviewers deliberately", func(t *testing.T) {
		sent = nil
		replacement := []string{"carol"}

		if _, err := service.Update(context.Background(), RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}, "42",
			UpdateInput{Version: 1, Description: "new text", Reviewers: &replacement}); err != nil {
			t.Fatalf("update: %v", err)
		}

		names := reviewerNamesFromPayload(t, sent)
		if len(names) != 1 || names[0] != "carol" {
			t.Errorf("reviewers sent = %v, want the caller's set", names)
		}
	})

	t.Run("clearing them deliberately is possible", func(t *testing.T) {
		sent = nil
		none := []string{}

		if _, err := service.Update(context.Background(), RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}, "42",
			UpdateInput{Version: 1, Description: "new text", Reviewers: &none}); err != nil {
			t.Fatalf("update: %v", err)
		}

		if names := reviewerNamesFromPayload(t, sent); len(names) != 0 {
			t.Errorf("reviewers sent = %v, want none", names)
		}
	})

	t.Run("an update with nothing to change is still refused", func(t *testing.T) {
		// The echoed reviewers must not count as a requested change, or the
		// guard that requires a title, description or draft stops working.
		_, err := service.Update(context.Background(), RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}, "42",
			UpdateInput{Version: 1})
		if err == nil {
			t.Fatal("an update naming no field was accepted")
		}
	})
}

func reviewerNamesFromPayload(t *testing.T, payload map[string]any) []string {
	t.Helper()

	raw, ok := payload["reviewers"]
	if !ok {
		t.Fatalf("the payload carried no reviewers key at all: %v", payload)
	}

	entries, ok := raw.([]any)
	if !ok {
		t.Fatalf("reviewers is %T, want a list", raw)
	}

	var names []string
	for _, entry := range entries {
		reviewer, _ := entry.(map[string]any)
		user, _ := reviewer["user"].(map[string]any)
		if name, ok := user["name"].(string); ok {
			names = append(names, name)
		}
	}

	return names
}
