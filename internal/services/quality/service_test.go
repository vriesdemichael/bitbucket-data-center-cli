package quality

import (
	"context"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

func newQualityTestService(t *testing.T, handler http.HandlerFunc) *Service {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := openapigenerated.NewClientWithResponses(server.URL + "/rest")
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	return NewService(client)
}

func TestQualityServiceBuildAndReportsFlow(t *testing.T) {
	service := newQualityTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/rest/build-status/latest/commits/abc":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/rest/build-status/latest/commits/abc":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"key":"ci/main","state":"SUCCESSFUL"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/build-status/latest/commits/stats/abc":
			_, _ = w.Write([]byte(`{"successful":1}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/required-builds/latest/projects/TEST/repos/demo/conditions":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":1}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/required-builds/latest/projects/TEST/repos/demo/condition":
			_, _ = w.Write([]byte(`{"id":2}`))
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/rest/required-builds/latest/projects/TEST/repos/demo/condition/"):
			_, _ = w.Write([]byte(`{"id":2}`))
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/rest/required-builds/latest/projects/TEST/repos/demo/condition/"):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/rest/insights/latest/projects/TEST/repos/demo/commits/abc/reports":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"key":"lint","title":"Lint"}]}`))
		case r.Method == http.MethodPut && r.URL.Path == "/rest/insights/latest/projects/TEST/repos/demo/commits/abc/reports/lint":
			_, _ = w.Write([]byte(`{"key":"lint","title":"Lint"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/insights/latest/projects/TEST/repos/demo/commits/abc/reports/lint":
			_, _ = w.Write([]byte(`{"key":"lint","title":"Lint"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/insights/latest/projects/TEST/repos/demo/commits/abc/reports/lint":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/rest/insights/latest/projects/TEST/repos/demo/commits/abc/reports/lint/annotations":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/rest/insights/latest/projects/TEST/repos/demo/commits/abc/reports/lint/annotations":
			_, _ = w.Write([]byte(`{"annotations":[{"externalId":"a1","message":"ok"}]}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/insights/latest/projects/TEST/repos/demo/commits/abc/reports/lint/annotations":
			if r.URL.Query().Get("externalId") == "" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte("missing externalId"))
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})

	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

	if err := service.SetBuildStatus(context.Background(), "abc", BuildStatusSetInput{Key: "ci/main", State: "SUCCESSFUL", URL: "https://ci.example"}); err != nil {
		t.Fatalf("set build status: %v", err)
	}

	statuses, err := service.GetBuildStatuses(context.Background(), "abc", 25, "NEWEST")
	if err != nil || len(statuses) != 1 {
		t.Fatalf("get build statuses len=%d err=%v", len(statuses), err)
	}

	stats, err := service.GetBuildStatusStats(context.Background(), "abc", true)
	if err != nil || stats.Successful == nil || *stats.Successful != 1 {
		t.Fatalf("get build stats %#v err=%v", stats, err)
	}

	checks, err := service.ListRequiredBuildChecks(context.Background(), repo, 25)
	if err != nil || len(checks) != 1 {
		t.Fatalf("list required checks len=%d err=%v", len(checks), err)
	}

	createdCheck, err := service.CreateRequiredBuildCheck(context.Background(), repo, map[string]any{"buildParentKeys": []string{"ci"}})
	if err != nil || createdCheck["id"] == nil {
		t.Fatalf("create required check %#v err=%v", createdCheck, err)
	}

	updatedCheck, err := service.UpdateRequiredBuildCheck(context.Background(), repo, 2, map[string]any{"buildParentKeys": []string{"ci"}})
	if err != nil || updatedCheck["id"] == nil {
		t.Fatalf("update required check %#v err=%v", updatedCheck, err)
	}

	if err := service.DeleteRequiredBuildCheck(context.Background(), repo, 2); err != nil {
		t.Fatalf("delete required check: %v", err)
	}

	reports, err := service.ListReports(context.Background(), repo, "abc", 25)
	if err != nil || len(reports) != 1 {
		t.Fatalf("list reports len=%d err=%v", len(reports), err)
	}

	result := "PASS"
	report, err := service.SetReport(context.Background(), repo, "abc", "lint", openapigenerated.SetACodeInsightsReportJSONRequestBody{Result: &result, Title: "Lint"})
	if err != nil || report.Key == nil || *report.Key != "lint" {
		t.Fatalf("set report %#v err=%v", report, err)
	}

	gotReport, err := service.GetReport(context.Background(), repo, "abc", "lint")
	if err != nil || gotReport.Key == nil || *gotReport.Key != "lint" {
		t.Fatalf("get report %#v err=%v", gotReport, err)
	}

	if err := service.DeleteReport(context.Background(), repo, "abc", "lint"); err != nil {
		t.Fatalf("delete report: %v", err)
	}

	externalID := "a1"
	path := "seed.txt"
	line := int32(1)
	annotation := openapigenerated.RestSingleAddInsightAnnotationRequest{ExternalId: &externalID, Message: "ok", Severity: "LOW", Path: &path, Line: &line}
	if err := service.AddAnnotations(context.Background(), repo, "abc", "lint", []openapigenerated.RestSingleAddInsightAnnotationRequest{annotation}); err != nil {
		t.Fatalf("add annotations: %v", err)
	}

	annotations, err := service.ListAnnotations(context.Background(), repo, "abc", "lint")
	if err != nil || len(annotations) != 1 {
		t.Fatalf("list annotations len=%d err=%v", len(annotations), err)
	}

	if err := service.DeleteAnnotations(context.Background(), repo, "abc", "lint", "a1"); err != nil {
		t.Fatalf("delete annotations: %v", err)
	}
}

func TestQualityServiceValidationGuards(t *testing.T) {
	service := newQualityTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	err := service.SetBuildStatus(context.Background(), "", BuildStatusSetInput{})
	if err == nil || !strings.Contains(err.Error(), "commit id is required") {
		t.Fatalf("expected commit id validation error, got %v", err)
	}

	_, err = service.UpdateRequiredBuildCheck(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, 0, map[string]any{"x": "y"})
	if err == nil || !strings.Contains(err.Error(), "must be > 0") {
		t.Fatalf("expected id validation error, got %v", err)
	}

	err = service.DeleteAnnotations(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, "abc", "lint", "")
	if err == nil || !strings.Contains(err.Error(), "external annotation id is required") {
		t.Fatalf("expected external id validation error, got %v", err)
	}

	_, err = service.GetBuildStatuses(context.Background(), "", 25, "")
	if err == nil || !strings.Contains(err.Error(), "commit id is required") {
		t.Fatalf("expected get statuses validation error, got %v", err)
	}

	_, err = service.GetBuildStatusStats(context.Background(), "", false)
	if err == nil || !strings.Contains(err.Error(), "commit id is required") {
		t.Fatalf("expected get stats validation error, got %v", err)
	}

	_, err = service.ListRequiredBuildChecks(context.Background(), RepositoryRef{}, 25)
	if err == nil || !strings.Contains(err.Error(), "repository must be specified") {
		t.Fatalf("expected repository validation error, got %v", err)
	}

	_, err = service.ListReports(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, "", 25)
	if err == nil || !strings.Contains(err.Error(), "commit id is required") {
		t.Fatalf("expected report commit validation error, got %v", err)
	}

	_, err = service.SetReport(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, "", "lint", openapigenerated.SetACodeInsightsReportJSONRequestBody{})
	if err == nil || !strings.Contains(err.Error(), "commit id is required") {
		t.Fatalf("expected set report validation error, got %v", err)
	}

	_, err = service.GetReport(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, "abc", "")
	if err == nil || !strings.Contains(err.Error(), "report key is required") {
		t.Fatalf("expected get report key validation error, got %v", err)
	}

	err = service.DeleteReport(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, "abc", "")
	if err == nil || !strings.Contains(err.Error(), "report key is required") {
		t.Fatalf("expected delete report key validation error, got %v", err)
	}

	err = service.AddAnnotations(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, "abc", "lint", nil)
	if err == nil || !strings.Contains(err.Error(), "at least one annotation is required") {
		t.Fatalf("expected annotation validation error, got %v", err)
	}

	_, err = service.CreateRequiredBuildCheck(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, map[string]any{"id": strconv.Itoa(1)})
	if err != nil && !strings.Contains(err.Error(), "invalid required build check payload") {
		t.Fatalf("expected either success or invalid required build check payload error, got %v", err)
	}
}

func TestQualityServicePaginationAndFallbackBranches(t *testing.T) {
	service := newQualityTestService(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/build-status/latest/commits/abc":
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Query().Get("start") == "1" {
				_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"key":"k2","state":"SUCCESSFUL"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"isLastPage":false,"nextPageStart":1,"values":[{"key":"k1","state":"SUCCESSFUL"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/build-status/latest/commits/stats/abc":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/rest/required-builds/latest/projects/TEST/repos/demo/conditions":
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Query().Get("start") == "1" {
				_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":2}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"isLastPage":false,"nextPageStart":1,"values":[{"id":1}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/required-builds/latest/projects/TEST/repos/demo/condition":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/rest/required-builds/latest/projects/TEST/repos/demo/condition/"):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/rest/insights/latest/projects/TEST/repos/demo/commits/abc/reports":
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Query().Get("start") == "1" {
				_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"key":"r2","title":"R2"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"isLastPage":false,"nextPageStart":1,"values":[{"key":"r1","title":"R1"}]}`))
		default:
			http.NotFound(w, r)
		}
	})

	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

	statuses, err := service.GetBuildStatuses(context.Background(), "abc", 2, "NEWEST")
	if err != nil {
		t.Fatalf("expected paginated build statuses success, got: %v", err)
	}
	if len(statuses) != 2 {
		t.Fatalf("expected 2 statuses from pagination, got: %d", len(statuses))
	}

	stats, err := service.GetBuildStatusStats(context.Background(), "abc", true)
	if err != nil {
		t.Fatalf("expected stats fallback success, got: %v", err)
	}
	if stats.Successful != nil {
		t.Fatalf("expected empty stats payload on no-content response, got: %#v", stats)
	}

	checks, err := service.ListRequiredBuildChecks(context.Background(), repo, 2)
	if err != nil {
		t.Fatalf("expected paginated required checks success, got: %v", err)
	}
	if len(checks) != 2 {
		t.Fatalf("expected 2 required checks from pagination, got: %d", len(checks))
	}

	created, err := service.CreateRequiredBuildCheck(context.Background(), repo, map[string]any{"buildParentKeys": []string{"ci"}})
	if err != nil {
		t.Fatalf("expected create required fallback success, got: %v", err)
	}
	if len(created) != 0 {
		t.Fatalf("expected empty map for nil created payload, got: %#v", created)
	}

	updated, err := service.UpdateRequiredBuildCheck(context.Background(), repo, 7, map[string]any{"buildParentKeys": []string{"ci"}})
	if err != nil {
		t.Fatalf("expected update required fallback success, got: %v", err)
	}
	if len(updated) != 0 {
		t.Fatalf("expected empty map for nil updated payload, got: %#v", updated)
	}

	reports, err := service.ListReports(context.Background(), repo, "abc", 2)
	if err != nil {
		t.Fatalf("expected paginated reports success, got: %v", err)
	}
	if len(reports) != 2 {
		t.Fatalf("expected 2 reports from pagination, got: %d", len(reports))
	}
}

func TestQualityMapStatusErrorCoverage(t *testing.T) {
	if err := openapi.MapStatusError(http.StatusOK, nil); err != nil {
		t.Fatalf("expected nil for success status, got: %v", err)
	}

	tests := []struct {
		status   int
		exitCode int
	}{
		{status: http.StatusBadRequest, exitCode: 2},
		{status: http.StatusUnauthorized, exitCode: 3},
		{status: http.StatusForbidden, exitCode: 3},
		{status: http.StatusNotFound, exitCode: 4},
		{status: http.StatusConflict, exitCode: 5},
		{status: http.StatusTooManyRequests, exitCode: 10},
		{status: http.StatusInternalServerError, exitCode: 10},
		{status: http.StatusNotAcceptable, exitCode: 1},
	}

	for _, testCase := range tests {
		err := openapi.MapStatusError(testCase.status, []byte("boom"))
		if err == nil {
			t.Fatalf("expected error for status %d", testCase.status)
		}
		if apperrors.ExitCode(err) != testCase.exitCode {
			t.Fatalf("expected exit code %d for status %d, got %d", testCase.exitCode, testCase.status, apperrors.ExitCode(err))
		}
	}
}

func TestSetBuildStatusValidationAndOptionalFields(t *testing.T) {
	t.Run("validation", func(t *testing.T) {
		service := newQualityTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})

		err := service.SetBuildStatus(context.Background(), "abc", BuildStatusSetInput{State: "SUCCESSFUL", URL: "https://ci.example"})
		if err == nil || !strings.Contains(err.Error(), "build status key is required") {
			t.Fatalf("expected key validation error, got: %v", err)
		}

		err = service.SetBuildStatus(context.Background(), "abc", BuildStatusSetInput{Key: "ci/main", URL: "https://ci.example"})
		if err == nil || !strings.Contains(err.Error(), "build status state is required") {
			t.Fatalf("expected state validation error, got: %v", err)
		}

		err = service.SetBuildStatus(context.Background(), "abc", BuildStatusSetInput{Key: "ci/main", State: "SUCCESSFUL"})
		if err == nil || !strings.Contains(err.Error(), "build status url is required") {
			t.Fatalf("expected url validation error, got: %v", err)
		}
	})

	t.Run("optional fields", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.URL.Path != "/rest/build-status/latest/commits/abc" {
				http.NotFound(w, r)
				return
			}

			body, _ := io.ReadAll(r.Body)
			bodyText := string(body)
			checks := []string{"\"name\":\"Build\"", "\"description\":\"Desc\"", "\"ref\":\"refs/heads/main\"", "\"parent\":\"ci\"", "\"buildNumber\":\"42\"", "\"duration\":123", "\"state\":\"SUCCESSFUL\""}
			for _, expected := range checks {
				if !strings.Contains(bodyText, expected) {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte("missing field: " + expected))
					return
				}
			}
			w.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()

		client, err := openapigenerated.NewClientWithResponses(server.URL + "/rest")
		if err != nil {
			t.Fatalf("create client: %v", err)
		}

		service := NewService(client)
		err = service.SetBuildStatus(context.Background(), "abc", BuildStatusSetInput{
			Key:         "ci/main",
			State:       "successful",
			URL:         "https://ci.example/1",
			Name:        "Build",
			Description: "Desc",
			Ref:         "refs/heads/main",
			Parent:      "ci",
			BuildNumber: "42",
			DurationMS:  123,
		})
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
	})
}

func TestQualityReportAndAnnotationFallbackBranches(t *testing.T) {
	service := newQualityTestService(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/rest/insights/latest/projects/TEST/repos/demo/commits/abc/reports/lint":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/rest/insights/latest/projects/TEST/repos/demo/commits/abc/reports/lint":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/rest/insights/latest/projects/TEST/repos/demo/commits/abc/reports/lint/annotations":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	})

	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}
	result := "PASS"

	setReport, err := service.SetReport(context.Background(), repo, "abc", "lint", openapigenerated.SetACodeInsightsReportJSONRequestBody{Title: "Lint", Result: &result})
	if err != nil {
		t.Fatalf("expected set report fallback success, got: %v", err)
	}
	if setReport.Key != nil {
		t.Fatalf("expected zero-value report when response payload is empty, got: %#v", setReport)
	}

	gotReport, err := service.GetReport(context.Background(), repo, "abc", "lint")
	if err != nil {
		t.Fatalf("expected get report fallback success, got: %v", err)
	}
	if gotReport.Key != nil {
		t.Fatalf("expected zero-value report when response payload is empty, got: %#v", gotReport)
	}

	annotations, err := service.ListAnnotations(context.Background(), repo, "abc", "lint")
	if err != nil {
		t.Fatalf("expected list annotations fallback success, got: %v", err)
	}
	if len(annotations) != 0 {
		t.Fatalf("expected empty annotations fallback, got: %#v", annotations)
	}
}

func TestBuildStatusFocusedErrorAndFallbackBranches(t *testing.T) {
	t.Run("set build status maps conflict", func(t *testing.T) {
		service := newQualityTestService(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && r.URL.Path == "/rest/build-status/latest/commits/abc" {
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte("conflict"))
				return
			}
			http.NotFound(w, r)
		})

		err := service.SetBuildStatus(context.Background(), "abc", BuildStatusSetInput{Key: "ci/main", State: "SUCCESSFUL", URL: "https://ci.example"})
		if err == nil {
			t.Fatal("expected conflict error")
		}
		if apperrors.ExitCode(err) != 5 {
			t.Fatalf("expected conflict exit code 5, got %d (%v)", apperrors.ExitCode(err), err)
		}
	})

	t.Run("set build status transport failure", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		baseURL := server.URL
		server.Close()

		client, err := openapigenerated.NewClientWithResponses(baseURL + "/rest")
		if err != nil {
			t.Fatalf("create client: %v", err)
		}

		service := NewService(client)
		err = service.SetBuildStatus(context.Background(), "abc", BuildStatusSetInput{Key: "ci/main", State: "SUCCESSFUL", URL: "https://ci.example"})
		if err == nil {
			t.Fatal("expected transient transport error")
		}
		if apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient exit code 10, got %d (%v)", apperrors.ExitCode(err), err)
		}
	})

	t.Run("get build statuses default limit and empty payload", func(t *testing.T) {
		service := newQualityTestService(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && r.URL.Path == "/rest/build-status/latest/commits/abc" {
				if r.URL.Query().Get("limit") != "25" || r.URL.Query().Get("start") != "0" {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte("unexpected paging defaults"))
					return
				}
				if r.URL.Query().Get("orderBy") != "" {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte("orderBy should be omitted when blank"))
					return
				}
				_, _ = w.Write([]byte(`{"isLastPage":false}`))
				return
			}
			http.NotFound(w, r)
		})

		statuses, err := service.GetBuildStatuses(context.Background(), "abc", 0, "   ")
		if err != nil {
			t.Fatalf("expected empty payload fallback success, got: %v", err)
		}
		if len(statuses) != 0 {
			t.Fatalf("expected empty statuses, got: %#v", statuses)
		}
	})

	t.Run("get build statuses orderBy and not-found mapping", func(t *testing.T) {
		service := newQualityTestService(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && r.URL.Path == "/rest/build-status/latest/commits/missing" {
				if r.URL.Query().Get("orderBy") != "NEWEST" {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte("missing orderBy"))
					return
				}
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte("missing commit"))
				return
			}
			http.NotFound(w, r)
		})

		_, err := service.GetBuildStatuses(context.Background(), "missing", 5, "NEWEST")
		if err == nil {
			t.Fatal("expected not found error")
		}
		if apperrors.ExitCode(err) != 4 {
			t.Fatalf("expected not found exit code 4, got %d (%v)", apperrors.ExitCode(err), err)
		}
	})

	t.Run("get build statuses transport failure", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[]}`))
		}))
		baseURL := server.URL
		server.Close()

		client, err := openapigenerated.NewClientWithResponses(baseURL + "/rest")
		if err != nil {
			t.Fatalf("create client: %v", err)
		}

		service := NewService(client)
		_, err = service.GetBuildStatuses(context.Background(), "abc", 10, "")
		if err == nil {
			t.Fatal("expected transient transport error")
		}
		if apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient exit code 10, got %d (%v)", apperrors.ExitCode(err), err)
		}
	})

	t.Run("get build status stats transport failure", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"successful":1}`))
		}))
		baseURL := server.URL
		server.Close()

		client, err := openapigenerated.NewClientWithResponses(baseURL + "/rest")
		if err != nil {
			t.Fatalf("create client: %v", err)
		}

		service := NewService(client)
		_, err = service.GetBuildStatusStats(context.Background(), "abc", true)
		if err == nil {
			t.Fatal("expected transient transport error")
		}
		if apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient exit code 10, got %d (%v)", apperrors.ExitCode(err), err)
		}
	})
}

func TestQualityRequiredAndInsightsErrorHandlingBranches(t *testing.T) {
	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

	t.Run("required checks status mapping", func(t *testing.T) {
		service := newQualityTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte("conflict"))
		})

		if _, err := service.ListRequiredBuildChecks(context.Background(), repo, 25); err == nil || apperrors.ExitCode(err) != 5 {
			t.Fatalf("expected conflict mapping for list required checks, got: %v", err)
		}
		if _, err := service.CreateRequiredBuildCheck(context.Background(), repo, map[string]any{"buildParentKeys": []string{"ci"}}); err == nil || apperrors.ExitCode(err) != 5 {
			t.Fatalf("expected conflict mapping for create required check, got: %v", err)
		}
		if _, err := service.UpdateRequiredBuildCheck(context.Background(), repo, 7, map[string]any{"buildParentKeys": []string{"ci"}}); err == nil || apperrors.ExitCode(err) != 5 {
			t.Fatalf("expected conflict mapping for update required check, got: %v", err)
		}
		if err := service.DeleteRequiredBuildCheck(context.Background(), repo, 7); err == nil || apperrors.ExitCode(err) != 5 {
			t.Fatalf("expected conflict mapping for delete required check, got: %v", err)
		}
	})

	t.Run("required checks transport failures", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		baseURL := server.URL
		server.Close()

		client, err := openapigenerated.NewClientWithResponses(baseURL + "/rest")
		if err != nil {
			t.Fatalf("create client: %v", err)
		}
		service := NewService(client)

		if _, err := service.ListRequiredBuildChecks(context.Background(), repo, 25); err == nil || apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient transport error for list required checks, got: %v", err)
		}
		if _, err := service.CreateRequiredBuildCheck(context.Background(), repo, map[string]any{"buildParentKeys": []string{"ci"}}); err == nil || apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient transport error for create required check, got: %v", err)
		}
		if _, err := service.UpdateRequiredBuildCheck(context.Background(), repo, 7, map[string]any{"buildParentKeys": []string{"ci"}}); err == nil || apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient transport error for update required check, got: %v", err)
		}
		if err := service.DeleteRequiredBuildCheck(context.Background(), repo, 7); err == nil || apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient transport error for delete required check, got: %v", err)
		}
	})

	t.Run("insights status mapping", func(t *testing.T) {
		service := newQualityTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("missing"))
		})

		result := "PASS"
		if _, err := service.ListReports(context.Background(), repo, "abc", 25); err == nil || apperrors.ExitCode(err) != 4 {
			t.Fatalf("expected not found mapping for list reports, got: %v", err)
		}
		if _, err := service.SetReport(context.Background(), repo, "abc", "lint", openapigenerated.SetACodeInsightsReportJSONRequestBody{Title: "Lint", Result: &result}); err == nil || apperrors.ExitCode(err) != 4 {
			t.Fatalf("expected not found mapping for set report, got: %v", err)
		}
		if _, err := service.GetReport(context.Background(), repo, "abc", "lint"); err == nil || apperrors.ExitCode(err) != 4 {
			t.Fatalf("expected not found mapping for get report, got: %v", err)
		}
		if err := service.DeleteReport(context.Background(), repo, "abc", "lint"); err == nil || apperrors.ExitCode(err) != 4 {
			t.Fatalf("expected not found mapping for delete report, got: %v", err)
		}

		externalID := "ann1"
		annotation := openapigenerated.RestSingleAddInsightAnnotationRequest{ExternalId: &externalID, Message: "note", Severity: "LOW"}
		if err := service.AddAnnotations(context.Background(), repo, "abc", "lint", []openapigenerated.RestSingleAddInsightAnnotationRequest{annotation}); err == nil || apperrors.ExitCode(err) != 4 {
			t.Fatalf("expected not found mapping for add annotations, got: %v", err)
		}
		if _, err := service.ListAnnotations(context.Background(), repo, "abc", "lint"); err == nil || apperrors.ExitCode(err) != 4 {
			t.Fatalf("expected not found mapping for list annotations, got: %v", err)
		}
		if err := service.DeleteAnnotations(context.Background(), repo, "abc", "lint", externalID); err == nil || apperrors.ExitCode(err) != 4 {
			t.Fatalf("expected not found mapping for delete annotations, got: %v", err)
		}
	})

	t.Run("insights transport failures", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		baseURL := server.URL
		server.Close()

		client, err := openapigenerated.NewClientWithResponses(baseURL + "/rest")
		if err != nil {
			t.Fatalf("create client: %v", err)
		}
		service := NewService(client)

		result := "PASS"
		externalID := "ann1"
		annotation := openapigenerated.RestSingleAddInsightAnnotationRequest{ExternalId: &externalID, Message: "note", Severity: "LOW"}

		if _, err := service.ListReports(context.Background(), repo, "abc", 25); err == nil || apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient transport error for list reports, got: %v", err)
		}
		if _, err := service.SetReport(context.Background(), repo, "abc", "lint", openapigenerated.SetACodeInsightsReportJSONRequestBody{Title: "Lint", Result: &result}); err == nil || apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient transport error for set report, got: %v", err)
		}
		if _, err := service.GetReport(context.Background(), repo, "abc", "lint"); err == nil || apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient transport error for get report, got: %v", err)
		}
		if err := service.DeleteReport(context.Background(), repo, "abc", "lint"); err == nil || apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient transport error for delete report, got: %v", err)
		}
		if err := service.AddAnnotations(context.Background(), repo, "abc", "lint", []openapigenerated.RestSingleAddInsightAnnotationRequest{annotation}); err == nil || apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient transport error for add annotations, got: %v", err)
		}
		if _, err := service.ListAnnotations(context.Background(), repo, "abc", "lint"); err == nil || apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient transport error for list annotations, got: %v", err)
		}
		if err := service.DeleteAnnotations(context.Background(), repo, "abc", "lint", externalID); err == nil || apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient transport error for delete annotations, got: %v", err)
		}
	})
}

func TestQualityServiceScopedAndDeploymentsFlow(t *testing.T) {
	service := newQualityTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/commits/abc/builds":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/commits/abc/builds":
			if r.URL.Query().Get("key") != "ci/main" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"key":"ci/main","state":"SUCCESSFUL"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/commits/abc/builds":
			if r.URL.Query().Get("key") != "ci/main" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/rest/build-status/latest/commits/stats":
			_, _ = w.Write([]byte(`{"abc":{"successful":1}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/commits/abc/deployments":
			_, _ = w.Write([]byte(`{"key":"dep1","state":"SUCCESSFUL"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/commits/abc/deployments":
			if r.URL.Query().Get("key") != "dep1" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"key":"dep1","state":"SUCCESSFUL"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/commits/abc/deployments":
			if r.URL.Query().Get("key") != "dep1" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPut && r.URL.Path == "/rest/insights/latest/projects/TEST/repos/demo/commits/abc/reports/lint/annotations/a1":
			_, _ = w.Write([]byte(`{"externalId":"a1","message":"fixed"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/insights/latest/projects/TEST/repos/demo/commits/abc/annotations":
			_, _ = w.Write([]byte(`{"annotations":[{"externalId":"a1","message":"fixed"}]}`))
		default:
			http.NotFound(w, r)
		}
	})

	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}

	// 1. Scoped Build Status
	err := service.AddScopedBuildStatus(context.Background(), repo, "abc", BuildStatusSetInput{
		Key:         "ci/main",
		State:       "SUCCESSFUL",
		URL:         "https://ci.example",
		Name:        "Build Name",
		Description: "Build Desc",
		Ref:         "refs/heads/main",
		Parent:      "ci",
		BuildNumber: "100",
		DurationMS:  2000,
	})
	if err != nil {
		t.Fatalf("AddScopedBuildStatus failed: %v", err)
	}

	build, err := service.GetScopedBuildStatus(context.Background(), repo, "abc", "ci/main")
	if err != nil || build.Key == nil || *build.Key != "ci/main" {
		t.Fatalf("GetScopedBuildStatus failed: err=%v build=%v", err, build)
	}

	err = service.DeleteScopedBuildStatus(context.Background(), repo, "abc", "ci/main")
	if err != nil {
		t.Fatalf("DeleteScopedBuildStatus failed: %v", err)
	}

	// 2. Multi-commit stats
	statsMap, err := service.GetMultipleBuildStatusStats(context.Background(), []string{"abc"})
	if err != nil || len(statsMap) != 1 || statsMap["abc"].Successful == nil || *statsMap["abc"].Successful != 1 {
		t.Fatalf("GetMultipleBuildStatusStats failed: err=%v statsMap=%v", err, statsMap)
	}

	// 3. Deployments
	envKey := "prod"
	envName := "Production"
	envType := "PRODUCTION"
	dep, err := service.CreateOrUpdateDeployment(context.Background(), repo, "abc", openapigenerated.RestDeploymentSetRequest{
		DeploymentSequenceNumber: 1,
		Description:              "Prod Deploy",
		DisplayName:              "Prod Deploy",
		Key:                      "dep1",
		State:                    "SUCCESSFUL",
		Url:                      "https://deploy.example",
		Environment: openapigenerated.RestDeploymentEnvironment{
			Key:         &envKey,
			DisplayName: &envName,
			Type:        &envType,
		},
	})
	if err != nil || dep.Key == nil || *dep.Key != "dep1" {
		t.Fatalf("CreateOrUpdateDeployment failed: err=%v dep=%v", err, dep)
	}

	depKey := "dep1"
	gotDep, err := service.GetDeployment(context.Background(), repo, "abc", openapigenerated.Get1Params{
		Key: &depKey,
	})
	if err != nil || gotDep.Key == nil || *gotDep.Key != "dep1" {
		t.Fatalf("GetDeployment failed: err=%v gotDep=%v", err, gotDep)
	}

	err = service.DeleteDeployment(context.Background(), repo, "abc", openapigenerated.Delete1Params{
		Key: &depKey,
	})
	if err != nil {
		t.Fatalf("DeleteDeployment failed: %v", err)
	}

	// 4. Code Insights single annotation Set
	ann, err := service.SetAnnotation(context.Background(), repo, "abc", "lint", "a1", openapigenerated.RestSingleAddInsightAnnotationRequest{
		Message:  "fixed",
		Severity: "LOW",
	})
	if err != nil || ann.ExternalId == nil || *ann.ExternalId != "a1" {
		t.Fatalf("SetAnnotation failed: err=%v ann=%v", err, ann)
	}

	// 5. Code Insights list commit annotations
	anns, err := service.ListCommitAnnotations(context.Background(), repo, "abc", openapigenerated.GetAnnotations1Params{})
	if err != nil || len(anns) != 1 || anns[0].ExternalId == nil || *anns[0].ExternalId != "a1" {
		t.Fatalf("ListCommitAnnotations failed: err=%v anns=%v", err, anns)
	}
}

func TestQualityServiceScopedAndDeploymentsErrorPaths(t *testing.T) {
	repo := RepositoryRef{ProjectKey: "TEST", Slug: "demo"}
	emptyRepo := RepositoryRef{}

	// 1. Validation guards
	t.Run("validation guards", func(t *testing.T) {
		service := newQualityTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		// AddScopedBuildStatus
		if err := service.AddScopedBuildStatus(context.Background(), emptyRepo, "abc", BuildStatusSetInput{}); err == nil {
			t.Fatal("expected empty repo error")
		}
		if err := service.AddScopedBuildStatus(context.Background(), repo, "", BuildStatusSetInput{}); err == nil {
			t.Fatal("expected empty commit error")
		}
		if err := service.AddScopedBuildStatus(context.Background(), repo, "abc", BuildStatusSetInput{}); err == nil {
			t.Fatal("expected empty key error")
		}
		if err := service.AddScopedBuildStatus(context.Background(), repo, "abc", BuildStatusSetInput{Key: "k"}); err == nil {
			t.Fatal("expected empty state error")
		}
		if err := service.AddScopedBuildStatus(context.Background(), repo, "abc", BuildStatusSetInput{Key: "k", State: "s"}); err == nil {
			t.Fatal("expected empty url error")
		}

		// GetScopedBuildStatus
		if _, err := service.GetScopedBuildStatus(context.Background(), emptyRepo, "abc", "k"); err == nil {
			t.Fatal("expected empty repo error")
		}
		if _, err := service.GetScopedBuildStatus(context.Background(), repo, "", "k"); err == nil {
			t.Fatal("expected empty commit error")
		}
		if _, err := service.GetScopedBuildStatus(context.Background(), repo, "abc", ""); err == nil {
			t.Fatal("expected empty key error")
		}

		// DeleteScopedBuildStatus
		if err := service.DeleteScopedBuildStatus(context.Background(), emptyRepo, "abc", "k"); err == nil {
			t.Fatal("expected empty repo error")
		}
		if err := service.DeleteScopedBuildStatus(context.Background(), repo, "", "k"); err == nil {
			t.Fatal("expected empty commit error")
		}
		if err := service.DeleteScopedBuildStatus(context.Background(), repo, "abc", ""); err == nil {
			t.Fatal("expected empty key error")
		}

		// GetMultipleBuildStatusStats
		if _, err := service.GetMultipleBuildStatusStats(context.Background(), nil); err == nil {
			t.Fatal("expected nil commits error")
		}
		if _, err := service.GetMultipleBuildStatusStats(context.Background(), []string{" "}); err == nil {
			t.Fatal("expected empty commits error")
		}

		// CreateOrUpdateDeployment
		if _, err := service.CreateOrUpdateDeployment(context.Background(), emptyRepo, "abc", openapigenerated.RestDeploymentSetRequest{}); err == nil {
			t.Fatal("expected empty repo error")
		}
		if _, err := service.CreateOrUpdateDeployment(context.Background(), repo, "", openapigenerated.RestDeploymentSetRequest{}); err == nil {
			t.Fatal("expected empty commit error")
		}

		// GetDeployment
		if _, err := service.GetDeployment(context.Background(), emptyRepo, "abc", openapigenerated.Get1Params{}); err == nil {
			t.Fatal("expected empty repo error")
		}
		if _, err := service.GetDeployment(context.Background(), repo, "", openapigenerated.Get1Params{}); err == nil {
			t.Fatal("expected empty commit error")
		}

		// DeleteDeployment
		if err := service.DeleteDeployment(context.Background(), emptyRepo, "abc", openapigenerated.Delete1Params{}); err == nil {
			t.Fatal("expected empty repo error")
		}
		if err := service.DeleteDeployment(context.Background(), repo, "", openapigenerated.Delete1Params{}); err == nil {
			t.Fatal("expected empty commit error")
		}

		// SetAnnotation
		if _, err := service.SetAnnotation(context.Background(), emptyRepo, "abc", "r", "a", openapigenerated.RestSingleAddInsightAnnotationRequest{}); err == nil {
			t.Fatal("expected empty repo error")
		}
		if _, err := service.SetAnnotation(context.Background(), repo, "", "r", "a", openapigenerated.RestSingleAddInsightAnnotationRequest{}); err == nil {
			t.Fatal("expected empty commit error")
		}
		if _, err := service.SetAnnotation(context.Background(), repo, "abc", "", "a", openapigenerated.RestSingleAddInsightAnnotationRequest{}); err == nil {
			t.Fatal("expected empty reportKey error")
		}
		if _, err := service.SetAnnotation(context.Background(), repo, "abc", "r", "", openapigenerated.RestSingleAddInsightAnnotationRequest{}); err == nil {
			t.Fatal("expected empty externalID error")
		}

		// ListCommitAnnotations
		if _, err := service.ListCommitAnnotations(context.Background(), emptyRepo, "abc", openapigenerated.GetAnnotations1Params{}); err == nil {
			t.Fatal("expected empty repo error")
		}
		if _, err := service.ListCommitAnnotations(context.Background(), repo, "", openapigenerated.GetAnnotations1Params{}); err == nil {
			t.Fatal("expected empty commit error")
		}
	})

	// 2. Transport / Status mapping failures
	t.Run("transport and status error mappings", func(t *testing.T) {
		service := newQualityTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("not found"))
		})

		// AddScopedBuildStatus mapping
		err := service.AddScopedBuildStatus(context.Background(), repo, "abc", BuildStatusSetInput{Key: "k", State: "s", URL: "u"})
		if err == nil || apperrors.ExitCode(err) != 4 {
			t.Fatalf("expected 404 exit code 4, got: %v", err)
		}

		// GetScopedBuildStatus mapping
		_, err = service.GetScopedBuildStatus(context.Background(), repo, "abc", "k")
		if err == nil || apperrors.ExitCode(err) != 4 {
			t.Fatalf("expected 404 exit code 4, got: %v", err)
		}

		// DeleteScopedBuildStatus mapping
		err = service.DeleteScopedBuildStatus(context.Background(), repo, "abc", "k")
		if err == nil || apperrors.ExitCode(err) != 4 {
			t.Fatalf("expected 404 exit code 4, got: %v", err)
		}

		// GetMultipleBuildStatusStats mapping
		_, err = service.GetMultipleBuildStatusStats(context.Background(), []string{"abc"})
		if err == nil || apperrors.ExitCode(err) != 4 {
			t.Fatalf("expected 404 exit code 4, got: %v", err)
		}

		// CreateOrUpdateDeployment mapping
		_, err = service.CreateOrUpdateDeployment(context.Background(), repo, "abc", openapigenerated.RestDeploymentSetRequest{Key: "k"})
		if err == nil || apperrors.ExitCode(err) != 4 {
			t.Fatalf("expected 404 exit code 4, got: %v", err)
		}

		// GetDeployment mapping
		depKey := "k"
		_, err = service.GetDeployment(context.Background(), repo, "abc", openapigenerated.Get1Params{Key: &depKey})
		if err == nil || apperrors.ExitCode(err) != 4 {
			t.Fatalf("expected 404 exit code 4, got: %v", err)
		}

		// DeleteDeployment mapping
		err = service.DeleteDeployment(context.Background(), repo, "abc", openapigenerated.Delete1Params{Key: &depKey})
		if err == nil || apperrors.ExitCode(err) != 4 {
			t.Fatalf("expected 404 exit code 4, got: %v", err)
		}

		// SetAnnotation mapping
		_, err = service.SetAnnotation(context.Background(), repo, "abc", "r", "a", openapigenerated.RestSingleAddInsightAnnotationRequest{})
		if err == nil || apperrors.ExitCode(err) != 4 {
			t.Fatalf("expected 404 exit code 4, got: %v", err)
		}

		// ListCommitAnnotations mapping
		_, err = service.ListCommitAnnotations(context.Background(), repo, "abc", openapigenerated.GetAnnotations1Params{})
		if err == nil || apperrors.ExitCode(err) != 4 {
			t.Fatalf("expected 404 exit code 4, got: %v", err)
		}
	})

	// 3. Transient transport failures
	t.Run("transient transport failures", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		baseURL := server.URL
		server.Close()

		client, err := openapigenerated.NewClientWithResponses(baseURL + "/rest")
		if err != nil {
			t.Fatalf("create client: %v", err)
		}
		service := NewService(client)

		depKey := "k"

		if err := service.AddScopedBuildStatus(context.Background(), repo, "abc", BuildStatusSetInput{Key: "k", State: "s", URL: "u"}); err == nil || apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient error, got: %v", err)
		}
		if _, err := service.GetScopedBuildStatus(context.Background(), repo, "abc", "k"); err == nil || apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient error, got: %v", err)
		}
		if err := service.DeleteScopedBuildStatus(context.Background(), repo, "abc", "k"); err == nil || apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient error, got: %v", err)
		}
		if _, err := service.GetMultipleBuildStatusStats(context.Background(), []string{"abc"}); err == nil || apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient error, got: %v", err)
		}
		if _, err := service.CreateOrUpdateDeployment(context.Background(), repo, "abc", openapigenerated.RestDeploymentSetRequest{Key: "k"}); err == nil || apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient error, got: %v", err)
		}
		if _, err := service.GetDeployment(context.Background(), repo, "abc", openapigenerated.Get1Params{Key: &depKey}); err == nil || apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient error, got: %v", err)
		}
		if err := service.DeleteDeployment(context.Background(), repo, "abc", openapigenerated.Delete1Params{Key: &depKey}); err == nil || apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient error, got: %v", err)
		}
		if _, err := service.SetAnnotation(context.Background(), repo, "abc", "r", "a", openapigenerated.RestSingleAddInsightAnnotationRequest{}); err == nil || apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient error, got: %v", err)
		}
		if _, err := service.ListCommitAnnotations(context.Background(), repo, "abc", openapigenerated.GetAnnotations1Params{}); err == nil || apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient error, got: %v", err)
		}
	})

	// 4. JSON parsing failures
	t.Run("json unmarshal failures", func(t *testing.T) {
		service := newQualityTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`42`))
		})

		_, err := service.GetMultipleBuildStatusStats(context.Background(), []string{"abc"})
		if err == nil || !strings.Contains(err.Error(), "failed to unmarshal") {
			t.Fatalf("expected unmarshal error, got: %v", err)
		}

		serviceSetAnn := newQualityTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{invalid}`))
		})

		_, err = serviceSetAnn.SetAnnotation(context.Background(), repo, "abc", "r", "a", openapigenerated.RestSingleAddInsightAnnotationRequest{})
		if err == nil || !strings.Contains(err.Error(), "failed to decode code insights annotation") {
			t.Fatalf("expected decode error, got: %v", err)
		}
	})

	// 5. Nil payload fallback checks
	t.Run("nil payload fallbacks", func(t *testing.T) {
		service := newQualityTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		})

		anns, err := service.ListCommitAnnotations(context.Background(), repo, "abc", openapigenerated.GetAnnotations1Params{})
		if err != nil || len(anns) != 0 {
			t.Fatalf("expected empty annotations, got: err=%v anns=%v", err, anns)
		}

		build, err := service.GetScopedBuildStatus(context.Background(), repo, "abc", "k")
		if err != nil || build.Key != nil {
			t.Fatalf("expected empty build, got: err=%v build=%v", err, build)
		}

		stats, err := service.GetMultipleBuildStatusStats(context.Background(), []string{"abc"})
		if err != nil || len(stats) != 0 {
			t.Fatalf("expected empty stats, got: err=%v stats=%v", err, stats)
		}

		dep, err := service.CreateOrUpdateDeployment(context.Background(), repo, "abc", openapigenerated.RestDeploymentSetRequest{})
		if err != nil || dep.Key != nil {
			t.Fatalf("expected empty deployment, got: err=%v dep=%v", err, dep)
		}

		depKey := "k"
		depGet, err := service.GetDeployment(context.Background(), repo, "abc", openapigenerated.Get1Params{Key: &depKey})
		if err != nil || depGet.Key != nil {
			t.Fatalf("expected empty deployment, got: err=%v depGet=%v", err, depGet)
		}
	})

	t.Run("nil response bodies when content type is not json", func(t *testing.T) {
		service := newQualityTestService(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`ok`))
		})

		build, err := service.GetScopedBuildStatus(context.Background(), repo, "abc", "k")
		if err != nil || build.Key != nil {
			t.Fatalf("expected empty build, got: err=%v build=%v", err, build)
		}

		dep, err := service.CreateOrUpdateDeployment(context.Background(), repo, "abc", openapigenerated.RestDeploymentSetRequest{})
		if err != nil || dep.Key != nil {
			t.Fatalf("expected empty deployment, got: err=%v dep=%v", err, dep)
		}

		depKey := "k"
		depGet, err := service.GetDeployment(context.Background(), repo, "abc", openapigenerated.Get1Params{Key: &depKey})
		if err != nil || depGet.Key != nil {
			t.Fatalf("expected empty deployment, got: err=%v depGet=%v", err, depGet)
		}

		stats, err := service.GetMultipleBuildStatusStats(context.Background(), []string{"abc"})
		if err != nil || len(stats) != 0 {
			t.Fatalf("expected empty stats, got: err=%v stats=%v", err, stats)
		}
	})
}
