package branch

import (
	"context"
	"fmt"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

func newBranchTestService(t *testing.T, handler http.HandlerFunc) *Service {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := openapigenerated.NewClientWithResponses(server.URL + "/rest")
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	return NewService(client)
}

func TestBranchServiceCoreCommands(t *testing.T) {
	service := newBranchTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/branches":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"displayId":"main","id":"refs/heads/main","latestCommit":"abc"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/branch-utils/latest/projects/TEST/repos/demo/branches":
			_, _ = w.Write([]byte(`{"displayId":"feature/demo","id":"refs/heads/feature/demo"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/branch-utils/latest/projects/TEST/repos/demo/branches":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/default-branch":
			_, _ = w.Write([]byte(`{"id":"refs/heads/main","displayId":"main"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/default-branch":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/rest/branch-utils/latest/projects/TEST/repos/demo/branches/info/"):
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":"refs/heads/main","displayId":"main"}]}`))
		default:
			http.NotFound(w, r)
		}
	})

	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

	branches, err := service.List(context.Background(), repo, ListOptions{Limit: 25, OrderBy: "ALPHABETICAL"})
	if err != nil || len(branches) != 1 {
		t.Fatalf("expected branches list success, len=%d err=%v", len(branches), err)
	}

	created, err := service.Create(context.Background(), repo, "feature/demo", "abc")
	if err != nil || created.DisplayId == nil || *created.DisplayId != "feature/demo" {
		t.Fatalf("expected create success, got %#v err=%v", created, err)
	}

	if err := service.Delete(context.Background(), repo, "feature/demo", "", false); err != nil {
		t.Fatalf("expected delete success, got %v", err)
	}

	defaultRef, err := service.GetDefault(context.Background(), repo)
	if err != nil || defaultRef.DisplayId == nil || *defaultRef.DisplayId != "main" {
		t.Fatalf("expected default get success, got %#v err=%v", defaultRef, err)
	}

	if err := service.SetDefault(context.Background(), repo, "main"); err != nil {
		t.Fatalf("expected default set success, got %v", err)
	}

	refs, err := service.FindByCommit(context.Background(), repo, "abc", 25)
	if err != nil || len(refs) != 1 {
		t.Fatalf("expected model inspect success, len=%d err=%v", len(refs), err)
	}
}

func TestBranchServiceRestrictionsLifecycle(t *testing.T) {
	service := newBranchTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/branch-permissions/latest/projects/TEST/repos/demo/restrictions":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":12,"type":"read-only","matcher":{"id":"refs/heads/main","displayId":"main","type":{"id":"BRANCH"}},"groups":["devs"]}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/branch-permissions/latest/projects/TEST/repos/demo/restrictions/12":
			_, _ = w.Write([]byte(`{"id":12,"type":"read-only","matcher":{"id":"refs/heads/main","displayId":"main","type":{"id":"BRANCH"}}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/branch-permissions/latest/projects/TEST/repos/demo/restrictions":
			_, _ = w.Write([]byte(`[{"id":12,"type":"read-only","matcher":{"id":"refs/heads/main","displayId":"main","type":{"id":"BRANCH"}}}]`))
		case r.Method == http.MethodPut && r.URL.Path == "/rest/branch-permissions/latest/projects/TEST/repos/demo/restrictions/12":
			_, _ = w.Write([]byte(`{"id":12,"type":"read-only","matcher":{"id":"refs/heads/main","displayId":"main","type":{"id":"BRANCH"}}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/branch-permissions/latest/projects/TEST/repos/demo/restrictions/12":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})

	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

	restrictions, err := service.ListRestrictions(context.Background(), repo, RestrictionListOptions{Limit: 25, Type: "read-only", MatcherType: "BRANCH", MatcherID: "refs/heads/main"})
	if err != nil || len(restrictions) != 1 {
		t.Fatalf("expected restriction list success, len=%d err=%v", len(restrictions), err)
	}

	created, err := service.CreateRestriction(context.Background(), repo, RestrictionUpsertInput{
		Type:           "read-only",
		MatcherType:    "BRANCH",
		MatcherID:      "refs/heads/main",
		MatcherDisplay: "main",
		Users:          []string{"alice"},
		Groups:         []string{"devs"},
		AccessKeyIDs:   []int32{7},
	})
	if err != nil || created.Id == nil || *created.Id != 12 {
		t.Fatalf("expected restriction create success, got %#v err=%v", created, err)
	}

	updated, err := service.UpdateRestriction(context.Background(), repo, "12", RestrictionUpsertInput{
		Type:           "read-only",
		MatcherType:    "BRANCH",
		MatcherID:      "refs/heads/main",
		MatcherDisplay: "main",
		Users:          []string{"bob"},
		Groups:         []string{"admins"},
		AccessKeyIDs:   []int32{8},
	})
	if err != nil || updated.Id == nil || *updated.Id != 12 {
		t.Fatalf("expected restriction update success, got %#v err=%v", updated, err)
	}

	restriction, err := service.GetRestriction(context.Background(), repo, "12")
	if err != nil || restriction.Id == nil || *restriction.Id != 12 {
		t.Fatalf("expected restriction get success, got %#v err=%v", restriction, err)
	}

	if err := service.DeleteRestriction(context.Background(), repo, "12"); err != nil {
		t.Fatalf("expected restriction delete success, got %v", err)
	}
}

func TestBranchServiceValidationAndHelpers(t *testing.T) {
	service := newBranchTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden"))
	})

	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

	if _, err := service.Create(context.Background(), repo, "", "abc"); err == nil {
		t.Fatal("expected branch name validation error")
	}
	if err := service.Delete(context.Background(), repo, "", "", false); err == nil {
		t.Fatal("expected branch delete validation error")
	}
	if err := service.SetDefault(context.Background(), repo, " "); err == nil {
		t.Fatal("expected default branch validation error")
	}
	if _, err := service.FindByCommit(context.Background(), repo, "", 10); err == nil {
		t.Fatal("expected commit validation error")
	}

	if _, err := service.List(context.Background(), repo, ListOptions{}); err == nil || !strings.Contains(err.Error(), "authorization") {
		t.Fatalf("expected mapped authorization error, got %v", err)
	}

	if _, err := service.ListRestrictions(context.Background(), repo, RestrictionListOptions{MatcherType: "bad"}); err == nil {
		t.Fatal("expected matcher type validation error")
	}

	if _, err := service.UpdateRestriction(context.Background(), repo, "abc", RestrictionUpsertInput{Type: "read-only", MatcherID: "refs/heads/main"}); err == nil {
		t.Fatal("expected restriction id parse validation error")
	}

	if normalizeBranchRef("main") != "refs/heads/main" {
		t.Fatal("expected normalizeBranchRef to add refs/heads prefix")
	}

	if err := openapi.MapStatusError(http.StatusCreated, nil); err != nil {
		t.Fatalf("expected nil for success status, got %v", err)
	}
	err := openapi.MapStatusError(http.StatusConflict, []byte("conflict"))
	if err == nil || apperrors.ExitCode(err) != 5 {
		t.Fatalf("expected conflict exit code 5, got %v (%d)", err, apperrors.ExitCode(err))
	}
}

func TestBranchServicePaginationAndFilters(t *testing.T) {
	branchCalls := 0
	modelCalls := 0
	restrictionCalls := 0

	service := newBranchTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/branches":
			branchCalls++
			if branchCalls == 1 {
				_, _ = w.Write([]byte(`{"isLastPage":false,"nextPageStart":2,"values":[{"displayId":"main","id":"refs/heads/main"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"displayId":"develop","id":"refs/heads/develop"}]}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/rest/branch-utils/latest/projects/TEST/repos/demo/branches/info/"):
			modelCalls++
			if modelCalls == 1 {
				_, _ = w.Write([]byte(`{"isLastPage":false,"nextPageStart":1,"values":[{"displayId":"main","id":"refs/heads/main"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"displayId":"release","id":"refs/heads/release"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/branch-permissions/latest/projects/TEST/repos/demo/restrictions":
			restrictionCalls++
			if restrictionCalls == 1 {
				_, _ = w.Write([]byte(`{"isLastPage":false,"nextPageStart":3,"values":[{"id":1,"type":"read-only"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":2,"type":"no-deletes"}]}`))
		default:
			http.NotFound(w, r)
		}
	})

	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

	branches, err := service.List(context.Background(), repo, ListOptions{
		Limit:      0,
		OrderBy:    "alphabetical",
		FilterText: "main",
		Base:       "refs/heads/main",
		Details:    nil,
	})
	if err != nil || len(branches) != 2 {
		t.Fatalf("expected paginated branch list, len=%d err=%v", len(branches), err)
	}

	refs, err := service.FindByCommit(context.Background(), repo, "abc", 0)
	if err != nil || len(refs) != 2 {
		t.Fatalf("expected paginated branch model inspect, len=%d err=%v", len(refs), err)
	}

	restrictions, err := service.ListRestrictions(context.Background(), repo, RestrictionListOptions{
		Limit:       0,
		Type:        "read-only",
		MatcherType: "branch",
		MatcherID:   "refs/heads/main",
	})
	if err != nil || len(restrictions) != 2 {
		t.Fatalf("expected paginated restrictions list, len=%d err=%v", len(restrictions), err)
	}
}

func TestBranchServiceCreateFallbackBodyDecode(t *testing.T) {
	service := newBranchTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/rest/branch-utils/latest/projects/TEST/repos/demo/branches" {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"displayId":"feature/fallback","id":"refs/heads/feature/fallback"}`))
			return
		}
		http.NotFound(w, r)
	})

	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}
	created, err := service.Create(context.Background(), repo, "feature/fallback", "abc")
	if err != nil {
		t.Fatalf("expected fallback decode success, got %v", err)
	}
	if created.DisplayId == nil || *created.DisplayId != "feature/fallback" {
		t.Fatalf("expected decoded fallback branch display id, got %#v", created)
	}
}

func TestBranchServiceStatusMappingAndNormalizers(t *testing.T) {
	statusCases := []struct {
		status   int
		body     string
		exitCode int
	}{
		{status: http.StatusBadRequest, body: "bad request", exitCode: 2},
		{status: http.StatusUnauthorized, body: "unauthorized", exitCode: 3},
		{status: http.StatusForbidden, body: "forbidden", exitCode: 3},
		{status: http.StatusNotFound, body: "missing", exitCode: 4},
		{status: http.StatusConflict, body: "conflict", exitCode: 5},
		{status: http.StatusTooManyRequests, body: "rate", exitCode: 10},
		{status: http.StatusInternalServerError, body: "server", exitCode: 10},
		{status: http.StatusTeapot, body: "teapot", exitCode: 1},
	}

	for _, testCase := range statusCases {
		t.Run(fmt.Sprintf("status_%d", testCase.status), func(t *testing.T) {
			err := openapi.MapStatusError(testCase.status, []byte(testCase.body))
			if err == nil {
				t.Fatalf("expected mapped error for status %d", testCase.status)
			}
			if apperrors.ExitCode(err) != testCase.exitCode {
				t.Fatalf("expected exit code %d, got %d (%v)", testCase.exitCode, apperrors.ExitCode(err), err)
			}
		})
	}

	if _, err := normalizeBranchOrderBy("MODIFICATION"); err != nil {
		t.Fatalf("expected MODIFICATION order-by to validate, got %v", err)
	}
	if _, err := normalizeBranchOrderBy("invalid"); err == nil {
		t.Fatal("expected invalid order-by validation error")
	}

	restrictionTypes := []string{"read-only", "no-deletes", "fast-forward-only", "pull-request-only", "no-creates"}
	for _, restrictionType := range restrictionTypes {
		if _, err := normalizeRestrictionType(restrictionType); err != nil {
			t.Fatalf("expected restriction type %s to validate, got %v", restrictionType, err)
		}
	}
	if _, err := normalizeRestrictionType("bad"); err == nil {
		t.Fatal("expected invalid restriction type error")
	}

	matcherTypes := []string{"BRANCH", "MODEL_BRANCH", "MODEL_CATEGORY", "PATTERN"}
	for _, matcherType := range matcherTypes {
		if _, err := normalizeRestrictionMatcherType(strings.ToLower(matcherType)); err != nil {
			t.Fatalf("expected matcher type %s to validate, got %v", matcherType, err)
		}
	}
	if _, err := normalizeRestrictionMatcherType("bad"); err == nil {
		t.Fatal("expected invalid restriction matcher type error")
	}

	if normalized, err := normalizeRestrictionRequestMatcherType(""); err != nil || string(normalized) != "BRANCH" {
		t.Fatalf("expected empty request matcher type to default to BRANCH, got %q err=%v", string(normalized), err)
	}
	if _, err := normalizeRestrictionRequestMatcherType("bad"); err == nil {
		t.Fatal("expected invalid request matcher type error")
	}

	if parsed, err := parseRestrictionID("10"); err != nil || parsed != 10 {
		t.Fatalf("expected parsed restriction id 10, got %d err=%v", parsed, err)
	}
	if _, err := parseRestrictionID("0"); err == nil {
		t.Fatal("expected parse restriction id > 0 error")
	}

	cleaned := cleanedStrings([]string{" alice ", "", "  ", "bob"})
	if len(cleaned) != 2 || cleaned[0] != "alice" || cleaned[1] != "bob" {
		t.Fatalf("expected cleaned strings [alice bob], got %#v", cleaned)
	}

	if normalizeBranchRef("refs/heads/main") != "refs/heads/main" {
		t.Fatal("expected normalizeBranchRef to preserve existing refs/heads prefix")
	}
}

func TestBranchServiceEmptyResponsesAndTransientErrors(t *testing.T) {
	service := newBranchTestService(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/default-branch":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Path == "/rest/branch-permissions/latest/projects/TEST/repos/demo/restrictions/12":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/branch-permissions/latest/projects/TEST/repos/demo/restrictions/12":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/branch-utils/latest/projects/TEST/repos/demo/branches":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/rest/branch-utils/latest/projects/TEST/repos/demo/branches":
			w.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(w, r)
		}
	})

	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

	defaultRef, err := service.GetDefault(context.Background(), repo)
	if err != nil {
		t.Fatalf("expected empty default response success, got %v", err)
	}
	if defaultRef.Id != nil || defaultRef.DisplayId != nil {
		t.Fatalf("expected zero-value default ref, got %#v", defaultRef)
	}

	restriction, err := service.GetRestriction(context.Background(), repo, "12")
	if err != nil {
		t.Fatalf("expected empty restriction response success, got %v", err)
	}
	if restriction.Id != nil || restriction.Type != nil {
		t.Fatalf("expected zero-value restriction, got %#v", restriction)
	}

	if err := service.DeleteRestriction(context.Background(), repo, "12"); err != nil {
		t.Fatalf("expected delete restriction success, got %v", err)
	}

	if err := service.Delete(context.Background(), repo, "feature/demo", "abc", true); err != nil {
		t.Fatalf("expected delete with endpoint+dryrun success, got %v", err)
	}

	created, err := service.Create(context.Background(), repo, "feature/no-payload", "abc")
	if err != nil {
		t.Fatalf("expected create empty payload success, got %v", err)
	}
	if created.Id != nil || created.DisplayId != nil {
		t.Fatalf("expected zero-value branch on empty create payload, got %#v", created)
	}

	transientService := newBranchTestService(t, func(w http.ResponseWriter, r *http.Request) {
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "hijacking not supported", http.StatusInternalServerError)
			return
		}
		connection, _, hijackErr := hijacker.Hijack()
		if hijackErr == nil {
			_ = connection.Close()
		}
	})

	if _, err := transientService.GetDefault(context.Background(), repo); err == nil || apperrors.ExitCode(err) != 10 {
		t.Fatalf("expected transient get default error, got %v (%d)", err, apperrors.ExitCode(err))
	}
	if _, err := transientService.GetRestriction(context.Background(), repo, "12"); err == nil || apperrors.ExitCode(err) != 10 {
		t.Fatalf("expected transient get restriction error, got %v (%d)", err, apperrors.ExitCode(err))
	}
	if err := transientService.DeleteRestriction(context.Background(), repo, "12"); err == nil || apperrors.ExitCode(err) != 10 {
		t.Fatalf("expected transient delete restriction error, got %v (%d)", err, apperrors.ExitCode(err))
	}
	if err := transientService.Delete(context.Background(), repo, "feature/demo", "", false); err == nil || apperrors.ExitCode(err) != 10 {
		t.Fatalf("expected transient delete branch error, got %v (%d)", err, apperrors.ExitCode(err))
	}
}

func TestBranchServiceAdditionalValidationAndFallbackPaths(t *testing.T) {
	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

	service := newBranchTestService(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/branches":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"isLastPage":true}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/branch-utils/latest/projects/TEST/repos/demo/branches":
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/rest/branch-utils/latest/projects/TEST/repos/demo/branches/info/"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"isLastPage":true}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/branch-permissions/latest/projects/TEST/repos/demo/restrictions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"isLastPage":true}`))
		default:
			http.NotFound(w, r)
		}
	})

	branches, err := service.List(context.Background(), repo, ListOptions{Details: func() *bool { value := true; return &value }()})
	if err != nil || len(branches) != 0 {
		t.Fatalf("expected empty list response success, len=%d err=%v", len(branches), err)
	}

	created, err := service.Create(context.Background(), repo, "feature/unmarshal-fail", "abc")
	if err != nil {
		t.Fatalf("expected create with non-object fallback body success, got %v", err)
	}
	if created.Id != nil || created.DisplayId != nil {
		t.Fatalf("expected zero-value create result for fallback unmarshal failure, got %#v", created)
	}

	if _, err := service.Create(context.Background(), repo, "feature/no-start", " "); err == nil {
		t.Fatal("expected create start-point validation error")
	}

	refs, err := service.FindByCommit(context.Background(), repo, "abc", 10)
	if err != nil || len(refs) != 0 {
		t.Fatalf("expected empty find-by-commit response success, len=%d err=%v", len(refs), err)
	}

	restrictions, err := service.ListRestrictions(context.Background(), repo, RestrictionListOptions{})
	if err != nil || len(restrictions) != 0 {
		t.Fatalf("expected empty restrictions response success, len=%d err=%v", len(restrictions), err)
	}

	if _, err := service.GetRestriction(context.Background(), repo, " "); err == nil {
		t.Fatal("expected restriction id validation error")
	}
	if err := service.DeleteRestriction(context.Background(), repo, " "); err == nil {
		t.Fatal("expected delete restriction id validation error")
	}

	if err := validateRepositoryRef(RepositoryRef{}); err == nil {
		t.Fatal("expected repository ref validation error")
	}
	if err := validateRepositoryRef(repo); err != nil {
		t.Fatalf("expected valid repository ref, got %v", err)
	}

	transientService := newBranchTestService(t, func(w http.ResponseWriter, r *http.Request) {
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "hijacking not supported", http.StatusInternalServerError)
			return
		}
		connection, _, hijackErr := hijacker.Hijack()
		if hijackErr == nil {
			_ = connection.Close()
		}
	})

	if err := transientService.SetDefault(context.Background(), repo, "main"); err == nil || apperrors.ExitCode(err) != 10 {
		t.Fatalf("expected transient set default error, got %v (%d)", err, apperrors.ExitCode(err))
	}
	if _, err := transientService.FindByCommit(context.Background(), repo, "abc", 25); err == nil || apperrors.ExitCode(err) != 10 {
		t.Fatalf("expected transient find-by-commit error, got %v (%d)", err, apperrors.ExitCode(err))
	}
	if _, err := transientService.ListRestrictions(context.Background(), repo, RestrictionListOptions{}); err == nil || apperrors.ExitCode(err) != 10 {
		t.Fatalf("expected transient list restrictions error, got %v (%d)", err, apperrors.ExitCode(err))
	}
}

func TestBranchServiceNotFoundAcrossOperations(t *testing.T) {
	service := newBranchTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("missing"))
	})
	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

	if _, err := service.List(context.Background(), repo, ListOptions{OrderBy: "MODIFICATION"}); err == nil || apperrors.ExitCode(err) != 4 {
		t.Fatalf("expected list not found error, got %v (%d)", err, apperrors.ExitCode(err))
	}
	if _, err := service.Create(context.Background(), repo, "feature/not-found", "abc"); err == nil || apperrors.ExitCode(err) != 4 {
		t.Fatalf("expected create not found error, got %v (%d)", err, apperrors.ExitCode(err))
	}
	if err := service.SetDefault(context.Background(), repo, "main"); err == nil || apperrors.ExitCode(err) != 4 {
		t.Fatalf("expected set default not found error, got %v (%d)", err, apperrors.ExitCode(err))
	}
	if _, err := service.FindByCommit(context.Background(), repo, "abc", 10); err == nil || apperrors.ExitCode(err) != 4 {
		t.Fatalf("expected find-by-commit not found error, got %v (%d)", err, apperrors.ExitCode(err))
	}
	if _, err := service.ListRestrictions(context.Background(), repo, RestrictionListOptions{}); err == nil || apperrors.ExitCode(err) != 4 {
		t.Fatalf("expected list restrictions not found error, got %v (%d)", err, apperrors.ExitCode(err))
	}
	if _, err := service.GetRestriction(context.Background(), repo, "12"); err == nil || apperrors.ExitCode(err) != 4 {
		t.Fatalf("expected get restriction not found error, got %v (%d)", err, apperrors.ExitCode(err))
	}
	if err := service.DeleteRestriction(context.Background(), repo, "12"); err == nil || apperrors.ExitCode(err) != 4 {
		t.Fatalf("expected delete restriction not found error, got %v (%d)", err, apperrors.ExitCode(err))
	}
}

func TestBranchServiceTransientAcrossOperations(t *testing.T) {
	service := newBranchTestService(t, func(w http.ResponseWriter, r *http.Request) {
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "hijacking not supported", http.StatusInternalServerError)
			return
		}
		connection, _, hijackErr := hijacker.Hijack()
		if hijackErr == nil {
			_ = connection.Close()
		}
	})
	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

	if _, err := service.List(context.Background(), repo, ListOptions{}); err == nil || apperrors.ExitCode(err) != 10 {
		t.Fatalf("expected list transient error, got %v (%d)", err, apperrors.ExitCode(err))
	}
	if _, err := service.Create(context.Background(), repo, "feature/transient", "abc"); err == nil || apperrors.ExitCode(err) != 10 {
		t.Fatalf("expected create transient error, got %v (%d)", err, apperrors.ExitCode(err))
	}
	if err := service.SetDefault(context.Background(), repo, "main"); err == nil || apperrors.ExitCode(err) != 10 {
		t.Fatalf("expected set default transient error, got %v (%d)", err, apperrors.ExitCode(err))
	}
	if _, err := service.FindByCommit(context.Background(), repo, "abc", 10); err == nil || apperrors.ExitCode(err) != 10 {
		t.Fatalf("expected find-by-commit transient error, got %v (%d)", err, apperrors.ExitCode(err))
	}
	if _, err := service.ListRestrictions(context.Background(), repo, RestrictionListOptions{}); err == nil || apperrors.ExitCode(err) != 10 {
		t.Fatalf("expected list restrictions transient error, got %v (%d)", err, apperrors.ExitCode(err))
	}
	if _, err := service.GetRestriction(context.Background(), repo, "12"); err == nil || apperrors.ExitCode(err) != 10 {
		t.Fatalf("expected get restriction transient error, got %v (%d)", err, apperrors.ExitCode(err))
	}
	if err := service.DeleteRestriction(context.Background(), repo, "12"); err == nil || apperrors.ExitCode(err) != 10 {
		t.Fatalf("expected delete restriction transient error, got %v (%d)", err, apperrors.ExitCode(err))
	}
}

func TestBranchServicePaginationLimit(t *testing.T) {
	calls := 0
	service := newBranchTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		calls++
		if calls == 1 {
			if r.URL.Query().Get("start") != "1" || r.URL.Query().Get("limit") != "3" {
				t.Errorf("expected start=1 limit=3 on call 1, got start=%s limit=%s", r.URL.Query().Get("start"), r.URL.Query().Get("limit"))
			}
			_, _ = w.Write([]byte(`{"isLastPage":false,"nextPageStart":3,"values":[{"displayId":"b1"},{"displayId":"b2"}]}`))
			return
		}
		if r.URL.Query().Get("start") != "3" || r.URL.Query().Get("limit") != "1" {
			t.Errorf("expected start=3 limit=1 on call 2, got start=%s limit=%s", r.URL.Query().Get("start"), r.URL.Query().Get("limit"))
		}
		_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"displayId":"b3"}]}`))
	})

	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}
	branches, err := service.List(context.Background(), repo, ListOptions{Limit: 3, Start: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(branches) != 3 {
		t.Errorf("expected 3 branches, got %d", len(branches))
	}
	if calls != 2 {
		t.Errorf("expected 2 page requests, got %d", calls)
	}
}

func TestBranchServicePaginationEdgeCases(t *testing.T) {
	service := newBranchTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("start") != "0" {
			t.Errorf("expected start=0, got start=%s", r.URL.Query().Get("start"))
		}
		_, _ = w.Write([]byte(`{"isLastPage":false,"nextPageStart":4,"values":[{"displayId":"b1"},{"displayId":"b2"},{"displayId":"b3"},{"displayId":"b4"}]}`))
	})

	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}
	branches, err := service.List(context.Background(), repo, ListOptions{Start: -1, Limit: 3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(branches) != 3 {
		t.Errorf("expected 3 branches, got %d", len(branches))
	}
}
