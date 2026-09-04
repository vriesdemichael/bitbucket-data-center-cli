package pullrequest

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
)

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

// mock-inventory: transport-fault — a page whose start never advances is a defensive guard against a server that misbehaves, not a claim that one does.
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
