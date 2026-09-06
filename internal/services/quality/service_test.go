package quality

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

func TestQualityServiceValidationGuards(t *testing.T) {
	t.Parallel()

	// Every case here is refused before a request is built, so the handler is
	// an assertion rather than a stand-in: reaching it means a guard let
	// something through (ADR-079).
	service := newQualityTestService(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("validation let a request through: %s %s", r.Method, r.URL.Path)
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

	// CreateRequiredBuildCheck with a valid payload used to sit here, which is
	// not a validation case: it calls out, and its assertion accepted success
	// or one particular error, so it could not fail. The create is covered
	// against a real server by TestLiveRequiredBuildCheckLifecycle.
}

func TestSetBuildStatusValidationAndOptionalFields(t *testing.T) {
	t.Parallel()

	t.Run("validation", func(t *testing.T) {
		service := newQualityTestService(t, testsupport.UnreachedHandler(t))

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

	// The optional fields moved to the live suite.
	//
	// What was here matched substrings in the request body against a mock --
	// "name":"Build", "duration":123 -- which says the client serialised them
	// and nothing about whether Bitbucket kept them. A field that is sent and
	// silently dropped looks identical from this side, and is the failure worth
	// catching: the caller is told the build was recorded with a description it
	// does not have. TestLiveBuildStatusLifecycle sets every one of them and
	// reads them back.
}

// TestQualityReportAndAnnotationFallbackBranches covers what the service does
// with a body Bitbucket does not send.
//
// Probed against a running instance: writing a report answers 200 with the whole
// object and so does reading one, and TestLiveQualityEmptyAnswers pins that. The
// 204-with-nothing these branches handle is therefore defensive, and what they
// do with it -- return a zero value and no error -- is the reading #537 exists to
// settle. Four places in this codebase answer an empty body four different ways;
// ADR-077 already moved the diff stats summary to refusing it outright, on the
// grounds that an unreadable body is not an empty one. These have not moved yet,
// so this records the current answer rather than endorsing it.
//
// mock-inventory: unreachable-state — a 204 with no body, which these endpoints never return (probed; TestLiveQualityEmptyAnswers pins what they do return); the subject is the fallback branch, not what Bitbucket sends.
func TestQualityReportAndAnnotationFallbackBranches(t *testing.T) {
	t.Parallel()

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

// mock-inventory: transport-fault — a conflict and three closed connections are injected, none of which a live instance can be asked for; the subject is that each build-status call classifies them rather than reporting success.
func TestBuildStatusFocusedErrorAndFallbackBranches(t *testing.T) {
	t.Parallel()

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
		baseURL := testsupport.ClosedListenerURL(t)

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

	// Two wire assertions went from here.
	//
	// One watched limit=25 and start=0 leave for a caller passing zero, which is
	// openapi.PageThrough's business and tested where the loop lives. The other
	// watched orderBy=NEWEST leave and then answered 404 to check the mapping,
	// which is one assertion about openapi.MapStatusError wearing a service's
	// clothes -- the mapping has its own table test and a guard stopping it from
	// being re-tested per service, and whether this service is wired to the
	// taxonomy at all is asked against a server that really refuses, in
	// TestLiveEveryServiceMapsItsFailures.
	//
	// TestLiveQualityListingsPageToTheEnd sends orderBy NEWEST to a real
	// Bitbucket and reads the answer back, which is the half neither of them
	// could reach.

	t.Run("get build statuses transport failure", func(t *testing.T) {
		baseURL := testsupport.ClosedListenerURL(t)

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
		baseURL := testsupport.ClosedListenerURL(t)

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

func TestQualityServiceScopedAndDeploymentsErrorPaths(t *testing.T) {
	t.Parallel()

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
		baseURL := testsupport.ClosedListenerURL(t)

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
