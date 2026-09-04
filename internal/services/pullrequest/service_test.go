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

func TestPullRequestUpdateValidation(t *testing.T) {
	service := NewService(httpclient.NewFromConfig(config.AppConfig{BitbucketURL: "http://localhost:7990"}))
	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

	_, err := service.Update(context.Background(), repo, "30", UpdateInput{Version: 0})
	if err == nil || apperrors.ExitCode(err) != 2 {
		t.Fatalf("expected update validation error, got: %v", err)
	}
}

func intPtr(value int) *int {
	return &value
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

// The reviewers key is written two ways and they must not be confused. The
// service writes it itself, echoing the current list so an update does not
// clear it (#511) -- that is not the caller naming a field. --reviewers is,
// and without it there would be no way to change the list through update at
// all.
func TestUpdateRequiresAFieldTheCallerNamed(t *testing.T) {
	t.Run("version alone is not a change", func(t *testing.T) {
		_, err := buildUpdatePayload(UpdateInput{Version: 3})
		if err == nil {
			t.Fatal("expected a validation error when nothing was named")
		}
		if !apperrors.IsKind(err, apperrors.KindValidation) {
			t.Errorf("kind = %v, want validation", err)
		}
	})

	t.Run("reviewers alone is a change", func(t *testing.T) {
		payload, err := buildUpdatePayload(UpdateInput{Version: 3, Reviewers: &[]string{"alice"}})
		if err != nil {
			t.Fatalf("--reviewers on its own must be accepted: %v", err)
		}
		if _, ok := payload["reviewers"]; !ok {
			t.Error("the payload carries no reviewers key")
		}
	})

	t.Run("an empty reviewers list is a change, and clears", func(t *testing.T) {
		payload, err := buildUpdatePayload(UpdateInput{Version: 3, Reviewers: &[]string{}})
		if err != nil {
			t.Fatalf(`--reviewers "" must be accepted: %v`, err)
		}
		reviewers, ok := payload["reviewers"].([]map[string]any)
		if !ok || len(reviewers) != 0 {
			t.Errorf("reviewers = %#v, want an empty list", payload["reviewers"])
		}
	})
}
