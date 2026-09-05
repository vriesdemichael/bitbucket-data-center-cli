//go:build live

package live_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	qualityservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/quality"
)

func TestLiveBuildStatusSetAndGet(t *testing.T) {
	harness := newLiveHarness(t)
	service := qualityservice.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 2)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	commitID := repo.CommitIDs[0]
	buildKey := fmt.Sprintf("live-build-%d", time.Now().UnixNano()%100000)

	err = service.SetBuildStatus(ctx, commitID, qualityservice.BuildStatusSetInput{
		Key:   buildKey,
		State: "SUCCESSFUL",
		URL:   "https://example.invalid/live-build",
		Name:  "Live Build",
	})
	if err != nil {
		t.Fatalf("set build status failed: %v", err)
	}

	statuses, err := service.GetBuildStatuses(ctx, commitID, 25, "NEWEST")
	if err != nil {
		t.Fatalf("get build statuses failed: %v", err)
	}

	found := false
	for _, status := range statuses {
		if status.Key != nil && *status.Key == buildKey {
			found = true
			break
		}
	}

	if !found {
		t.Fatalf("expected build status key=%s in response", buildKey)
	}
}

func TestLiveCodeInsightsReportSetAndGet(t *testing.T) {
	harness := newLiveHarness(t)
	service := qualityservice.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	commitID := repo.CommitIDs[0]
	reportKey := fmt.Sprintf("live-report-%d", time.Now().UnixNano()%100000)
	title := "Live Insights"
	result := "PASS"
	reportRequest := openapigenerated.SetACodeInsightsReportJSONRequestBody{
		Title:  title,
		Result: &result,
	}

	_, err = service.SetReport(
		ctx,
		qualityservice.RepositoryRef{ProjectKey: seeded.Key, Slug: repo.Slug},
		commitID,
		reportKey,
		reportRequest,
	)
	if err != nil {
		t.Fatalf("set report failed: %v", err)
	}

	report, err := service.GetReport(
		ctx,
		qualityservice.RepositoryRef{ProjectKey: seeded.Key, Slug: repo.Slug},
		commitID,
		reportKey,
	)
	if err != nil {
		t.Fatalf("get report failed: %v", err)
	}

	if report.Key == nil || *report.Key != reportKey {
		t.Fatalf("expected report key=%s, got %#v", reportKey, report.Key)
	}
	if report.Title == nil || *report.Title != title {
		t.Fatalf("expected report title=%s, got %#v", title, report.Title)
	}

	if err := service.DeleteReport(
		ctx,
		qualityservice.RepositoryRef{ProjectKey: seeded.Key, Slug: repo.Slug},
		commitID,
		reportKey,
	); err != nil {
		t.Fatalf("delete report failed: %v", err)
	}
}

func TestLiveRequiredBuildCheckLifecycle(t *testing.T) {
	harness := newLiveHarness(t)
	service := qualityservice.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := qualityservice.RepositoryRef{ProjectKey: seeded.Key, Slug: seeded.Repos[0].Slug}
	payload := map[string]any{
		"buildParentKeys": []string{"ci"},
		"refMatcher": map[string]any{
			"id": "refs/heads/master",
			"type": map[string]any{
				"id": "BRANCH",
			},
		},
	}

	created, err := service.CreateRequiredBuildCheck(ctx, repo, payload)
	if err != nil {
		t.Fatalf("create required build check failed: %v", err)
	}

	checkID, ok := requiredBuildCheckID(created)
	if !ok || checkID <= 0 {
		t.Fatalf("expected created check id, got %#v", created)
	}

	if _, err := service.UpdateRequiredBuildCheck(ctx, repo, checkID, payload); err != nil {
		t.Fatalf("update required build check failed: %v", err)
	}

	checks, err := service.ListRequiredBuildChecks(ctx, repo, 25)
	if err != nil {
		t.Fatalf("list required build checks failed: %v", err)
	}
	if len(checks) == 0 {
		t.Fatalf("expected at least one required build check")
	}

	if err := service.DeleteRequiredBuildCheck(ctx, repo, checkID); err != nil {
		t.Fatalf("delete required build check failed: %v", err)
	}
}

func TestLiveCodeInsightsAnnotationsLifecycle(t *testing.T) {
	harness := newLiveHarness(t)
	service := qualityservice.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := qualityservice.RepositoryRef{ProjectKey: seeded.Key, Slug: seeded.Repos[0].Slug}
	commitID := seeded.Repos[0].CommitIDs[0]
	reportKey := fmt.Sprintf("live-report-annotations-%d", time.Now().UnixNano()%100000)

	result := "PASS"
	title := "Live Annotations"
	_, err = service.SetReport(ctx, repo, commitID, reportKey, openapigenerated.SetACodeInsightsReportJSONRequestBody{Title: title, Result: &result})
	if err != nil {
		t.Fatalf("set report for annotations failed: %v", err)
	}

	externalID := fmt.Sprintf("ann-%d", time.Now().UnixNano()%100000)
	path := "seed.txt"
	line := int32(1)
	annotations := []openapigenerated.RestSingleAddInsightAnnotationRequest{{
		ExternalId: &externalID,
		Message:    "integration annotation",
		Severity:   "LOW",
		Path:       &path,
		Line:       &line,
	}}

	if err := service.AddAnnotations(ctx, repo, commitID, reportKey, annotations); err != nil {
		t.Fatalf("add annotations failed: %v", err)
	}

	listed, err := service.ListAnnotations(ctx, repo, commitID, reportKey)
	if err != nil {
		t.Fatalf("list annotations failed: %v", err)
	}
	if len(listed) == 0 {
		t.Fatalf("expected at least one annotation")
	}

	if err := service.DeleteAnnotations(ctx, repo, commitID, reportKey, externalID); err != nil {
		t.Fatalf("delete annotations failed: %v", err)
	}

	if err := service.DeleteReport(ctx, repo, commitID, reportKey); err != nil {
		t.Fatalf("delete report failed: %v", err)
	}
}

func TestLiveCLIInsightsReportSetDryRunNoSideEffect(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	commitID := repo.CommitIDs[0]
	reportKey := fmt.Sprintf("live-dryrun-report-%d", time.Now().UnixNano()%100000)

	listBeforeOutput, err := executeLiveCLI(t, "--json", "insights", "report", "list", commitID, "--limit", "200")
	if err != nil {
		t.Fatalf("insights report list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	body := fmt.Sprintf(`{"title":"Dry Run Report %s","result":"PASS"}`, reportKey)
	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "insights", "report", "set", commitID, reportKey, "--body", body)
	if err != nil {
		t.Fatalf("insights report set dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "insights.report.set"`) {
		t.Fatalf("expected insights.report.set intent, got: %s", dryRunOutput)
	}

	listAfterOutput, err := executeLiveCLI(t, "--json", "insights", "report", "list", commitID, "--limit", "200")
	if err != nil {
		t.Fatalf("insights report list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no report side-effect from set dry-run\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}
}

func TestLiveCLIBuildStatusSetDryRunNoSideEffect(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	commitID := repo.CommitIDs[0]

	statsBeforeOutput, err := executeLiveCLI(t, "--json", "build", "status", "stats", commitID)
	if err != nil {
		t.Fatalf("build status stats before failed: %v\noutput: %s", err, statsBeforeOutput)
	}

	statusKey := fmt.Sprintf("live-dryrun-status-%d", time.Now().UnixNano()%100000)
	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "build", "status", "set", commitID, "--key", statusKey, "--state", "SUCCESSFUL", "--url", "https://example.invalid/dryrun")
	if err != nil {
		t.Fatalf("build status set dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "build.status.set"`) {
		t.Fatalf("expected build.status.set intent, got: %s", dryRunOutput)
	}

	statsAfterOutput, err := executeLiveCLI(t, "--json", "build", "status", "stats", commitID)
	if err != nil {
		t.Fatalf("build status stats after failed: %v\noutput: %s", err, statsAfterOutput)
	}

	if statsBeforeOutput != statsAfterOutput {
		t.Fatalf("expected no build-status side-effect from set dry-run\nbefore: %s\nafter: %s", statsBeforeOutput, statsAfterOutput)
	}
}

func TestLiveCLIInsightsReportDeleteDryRunNoSideEffect(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	commitID := repo.CommitIDs[0]
	reportKey := fmt.Sprintf("live-dryrun-report-del-%d", time.Now().UnixNano()%100000)
	body := fmt.Sprintf(`{"title":"Dry Run Report Delete %s","result":"PASS"}`, reportKey)

	setOutput, err := executeLiveCLI(t, "--json", "insights", "report", "set", commitID, reportKey, "--body", body)
	if err != nil {
		t.Fatalf("insights report set fixture failed: %v\noutput: %s", err, setOutput)
	}

	listBeforeOutput, err := executeLiveCLI(t, "--json", "insights", "report", "list", commitID, "--limit", "200")
	if err != nil {
		t.Fatalf("insights report list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "insights", "report", "delete", commitID, reportKey)
	if err != nil {
		t.Fatalf("insights report delete dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "insights.report.delete"`) {
		t.Fatalf("expected insights.report.delete intent, got: %s", dryRunOutput)
	}

	listAfterOutput, err := executeLiveCLI(t, "--json", "insights", "report", "list", commitID, "--limit", "200")
	if err != nil {
		t.Fatalf("insights report list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no report side-effect from delete dry-run\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}

	_, _ = executeLiveCLI(t, "--json", "insights", "report", "delete", commitID, reportKey)
}

func TestLiveCLIInsightsAnnotationAddDeleteDryRunNoSideEffect(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	commitID := repo.CommitIDs[0]
	reportKey := fmt.Sprintf("live-dryrun-ann-report-%d", time.Now().UnixNano()%100000)
	body := fmt.Sprintf(`{"title":"Dry Run Annotation Report %s","result":"PASS"}`, reportKey)

	setOutput, err := executeLiveCLI(t, "--json", "insights", "report", "set", commitID, reportKey, "--body", body)
	if err != nil {
		t.Fatalf("insights report set fixture failed: %v\noutput: %s", err, setOutput)
	}

	listBeforeOutput, err := executeLiveCLI(t, "--json", "insights", "annotation", "list", commitID, reportKey)
	if err != nil {
		t.Fatalf("insights annotation list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	externalID := fmt.Sprintf("live-dryrun-ann-%d", time.Now().UnixNano()%100000)
	annotationBody := fmt.Sprintf(`[{"externalId":"%s","message":"dry-run annotation","severity":"LOW","path":"seed.txt","line":1}]`, externalID)

	addDryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "insights", "annotation", "add", commitID, reportKey, "--body", annotationBody)
	if err != nil {
		t.Fatalf("insights annotation add dry-run failed: %v\noutput: %s", err, addDryRunOutput)
	}
	if !strings.Contains(addDryRunOutput, `"intent": "insights.annotation.add"`) {
		t.Fatalf("expected insights.annotation.add intent, got: %s", addDryRunOutput)
	}

	listAfterAddOutput, err := executeLiveCLI(t, "--json", "insights", "annotation", "list", commitID, reportKey)
	if err != nil {
		t.Fatalf("insights annotation list after add dry-run failed: %v\noutput: %s", err, listAfterAddOutput)
	}
	if listBeforeOutput != listAfterAddOutput {
		t.Fatalf("expected no annotation side-effect from add dry-run\nbefore: %s\nafter: %s", listBeforeOutput, listAfterAddOutput)
	}

	createAnnotationOutput, err := executeLiveCLI(t, "--json", "insights", "annotation", "add", commitID, reportKey, "--body", annotationBody)
	if err != nil {
		t.Fatalf("insights annotation add fixture failed: %v\noutput: %s", err, createAnnotationOutput)
	}

	listBeforeDeleteOutput, err := executeLiveCLI(t, "--json", "insights", "annotation", "list", commitID, reportKey)
	if err != nil {
		t.Fatalf("insights annotation list before delete dry-run failed: %v\noutput: %s", err, listBeforeDeleteOutput)
	}

	deleteDryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "insights", "annotation", "delete", commitID, reportKey, "--external-id", externalID)
	if err != nil {
		t.Fatalf("insights annotation delete dry-run failed: %v\noutput: %s", err, deleteDryRunOutput)
	}
	if !strings.Contains(deleteDryRunOutput, `"intent": "insights.annotation.delete"`) {
		t.Fatalf("expected insights.annotation.delete intent, got: %s", deleteDryRunOutput)
	}

	listAfterDeleteOutput, err := executeLiveCLI(t, "--json", "insights", "annotation", "list", commitID, reportKey)
	if err != nil {
		t.Fatalf("insights annotation list after delete dry-run failed: %v\noutput: %s", err, listAfterDeleteOutput)
	}
	if listBeforeDeleteOutput != listAfterDeleteOutput {
		t.Fatalf("expected no annotation side-effect from delete dry-run\nbefore: %s\nafter: %s", listBeforeDeleteOutput, listAfterDeleteOutput)
	}

	_, _ = executeLiveCLI(t, "--json", "insights", "annotation", "delete", commitID, reportKey, "--external-id", externalID)
	_, _ = executeLiveCLI(t, "--json", "insights", "report", "delete", commitID, reportKey)
}

func TestLiveCLIBuildRequiredCreateUpdateDeleteDryRunNoSideEffect(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	listBeforeCreateOutput, err := executeLiveCLI(t, "--json", "build", "required", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("build required list before create failed: %v\noutput: %s", err, listBeforeCreateOutput)
	}

	body := `{"buildParentKeys":["ci"],"refMatcher":{"id":"refs/heads/master","type":{"id":"BRANCH"}}}`
	createDryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "build", "required", "create", "--body", body)
	if err != nil {
		t.Fatalf("build required create dry-run failed: %v\noutput: %s", err, createDryRunOutput)
	}
	if !strings.Contains(createDryRunOutput, `"intent": "build.required.create"`) {
		t.Fatalf("expected build.required.create intent, got: %s", createDryRunOutput)
	}

	listAfterCreateOutput, err := executeLiveCLI(t, "--json", "build", "required", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("build required list after create dry-run failed: %v\noutput: %s", err, listAfterCreateOutput)
	}
	if listBeforeCreateOutput != listAfterCreateOutput {
		t.Fatalf("expected no required-build side-effect from create dry-run\nbefore: %s\nafter: %s", listBeforeCreateOutput, listAfterCreateOutput)
	}

	requiredID, requiredAvailable := createRequiredBuildCheckWithRetry(t, body)
	if !requiredAvailable {
		t.Fatalf("required-build endpoint unavailable for update/delete dry-run assertions")
	}

	listBeforeUpdateOutput, err := executeLiveCLI(t, "--json", "build", "required", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("build required list before update dry-run failed: %v\noutput: %s", err, listBeforeUpdateOutput)
	}

	updateDryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "build", "required", "update", requiredID, "--body", body)
	if err != nil {
		t.Fatalf("build required update dry-run failed: %v\noutput: %s", err, updateDryRunOutput)
	}
	if !strings.Contains(updateDryRunOutput, `"intent": "build.required.update"`) {
		t.Fatalf("expected build.required.update intent, got: %s", updateDryRunOutput)
	}

	listAfterUpdateOutput, err := executeLiveCLI(t, "--json", "build", "required", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("build required list after update dry-run failed: %v\noutput: %s", err, listAfterUpdateOutput)
	}
	if listBeforeUpdateOutput != listAfterUpdateOutput {
		t.Fatalf("expected no required-build side-effect from update dry-run\nbefore: %s\nafter: %s", listBeforeUpdateOutput, listAfterUpdateOutput)
	}

	listBeforeDeleteOutput, err := executeLiveCLI(t, "--json", "build", "required", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("build required list before delete dry-run failed: %v\noutput: %s", err, listBeforeDeleteOutput)
	}

	deleteDryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "build", "required", "delete", requiredID)
	if err != nil {
		t.Fatalf("build required delete dry-run failed: %v\noutput: %s", err, deleteDryRunOutput)
	}
	if !strings.Contains(deleteDryRunOutput, `"intent": "build.required.delete"`) {
		t.Fatalf("expected build.required.delete intent, got: %s", deleteDryRunOutput)
	}

	listAfterDeleteOutput, err := executeLiveCLI(t, "--json", "build", "required", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("build required list after delete dry-run failed: %v\noutput: %s", err, listAfterDeleteOutput)
	}
	if listBeforeDeleteOutput != listAfterDeleteOutput {
		t.Fatalf("expected no required-build side-effect from delete dry-run\nbefore: %s\nafter: %s", listBeforeDeleteOutput, listAfterDeleteOutput)
	}

	_, _ = executeLiveCLI(t, "build", "required", "delete", requiredID)
}

func requiredBuildCheckID(payload map[string]any) (int64, bool) {
	value, ok := payload["id"]
	if !ok {
		return 0, false
	}

	switch typed := value.(type) {
	case float64:
		return int64(typed), true
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	default:
		return 0, false
	}
}

// TestLiveQualityListingsPageToTheEnd drives the two quality listings past a
// page boundary.
//
// Bitbucket's paging convention -- isLastPage, nextPageStart -- is the server's,
// and the mocks these replace implemented it by hand and then checked that the
// service followed the implementation. Each of these listings runs its own
// paging loop, so proving the convention once elsewhere says nothing about
// them: a loop that stops after the first page returns a short answer that
// looks perfectly well formed.
//
// The lever is the seed count. openapi.PageThrough asks for a page at a time,
// so seeding more than one page of anything forces the boundary -- and asking
// for fewer than exist is the other half, because a cap that is not honoured
// comes back long.
func TestLiveQualityListingsPageToTheEnd(t *testing.T) {
	harness := newLiveHarness(t)
	service := qualityservice.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := qualityservice.RepositoryRef{ProjectKey: seeded.Key, Slug: repo.Slug}
	commitID := repo.CommitIDs[0]

	const total = 30

	t.Run("build statuses", func(t *testing.T) {
		for index := range total {
			key := fmt.Sprintf("paged-build-%d-%d", time.Now().UnixNano()%100000, index)
			if err := service.SetBuildStatus(ctx, commitID, qualityservice.BuildStatusSetInput{
				Key:   key,
				State: "SUCCESSFUL",
				URL:   "https://ci.example.invalid/" + key,
				Name:  key,
			}); err != nil {
				t.Fatalf("set build status %d failed: %v", index, err)
			}
		}

		// More than one page, so the loop has to come back for the rest.
		statuses, err := service.GetBuildStatuses(ctx, commitID, total+10, "NEWEST")
		if err != nil {
			t.Fatalf("get build statuses failed: %v", err)
		}
		if len(statuses) < total {
			t.Fatalf("paging stopped early: got %d build statuses, want at least %d", len(statuses), total)
		}

		if capped, err := service.GetBuildStatuses(ctx, commitID, 5, "NEWEST"); err != nil {
			t.Fatalf("get capped build statuses failed: %v", err)
		} else if len(capped) != 5 {
			t.Fatalf("a cap of 5 returned %d build statuses", len(capped))
		}
	})

	t.Run("insight reports", func(t *testing.T) {
		passed := "PASS"
		for index := range total {
			key := fmt.Sprintf("paged-report-%d-%d", time.Now().UnixNano()%100000, index)
			if _, err := service.SetReport(ctx, repoRef, commitID, key,
				openapigenerated.SetACodeInsightsReportJSONRequestBody{Title: key, Result: &passed}); err != nil {
				t.Fatalf("set report %d failed: %v", index, err)
			}
		}

		reports, err := service.ListReports(ctx, repoRef, commitID, total+10)
		if err != nil {
			t.Fatalf("list reports failed: %v", err)
		}
		if len(reports) < total {
			t.Fatalf("paging stopped early: got %d reports, want at least %d", len(reports), total)
		}

		if capped, err := service.ListReports(ctx, repoRef, commitID, 5); err != nil {
			t.Fatalf("list capped reports failed: %v", err)
		} else if len(capped) != 5 {
			t.Fatalf("a cap of 5 returned %d reports", len(capped))
		}
	})

	t.Run("required build checks", func(t *testing.T) {
		// Each condition needs its own ref matcher, or the server treats the
		// second as a duplicate of the first.
		for index := range total {
			payload := map[string]any{
				"buildParentKeys": []string{fmt.Sprintf("ci-%d", index)},
				"refMatcher": map[string]any{
					"id":   fmt.Sprintf("refs/heads/paged-%d", index),
					"type": map[string]any{"id": "BRANCH"},
				},
			}
			if _, err := service.CreateRequiredBuildCheck(ctx, repoRef, payload); err != nil {
				t.Fatalf("create required build check %d failed: %v", index, err)
			}
		}

		checks, err := service.ListRequiredBuildChecks(ctx, repoRef, total+10)
		if err != nil {
			t.Fatalf("list required build checks failed: %v", err)
		}
		if len(checks) < total {
			t.Fatalf("paging stopped early: got %d checks, want at least %d", len(checks), total)
		}

		if capped, err := service.ListRequiredBuildChecks(ctx, repoRef, 5); err != nil {
			t.Fatalf("list capped required build checks failed: %v", err)
		} else if len(capped) != 5 {
			t.Fatalf("a cap of 5 returned %d checks", len(capped))
		}
	})
}

// TestLiveQualityEmptyAnswers covers what the quality endpoints send when there
// is nothing to report.
//
// The mocks these replace chose 204 with an empty body and asserted the service
// produced a zero value from it. That is the same shape as the rebase defect
// (OPENAPI-028): an empty body is easy to mistake for a broken response, and
// which endpoints send one is the server's decision, not ours. A commit nobody
// has reported on answers the question without anyone deciding what it says.
func TestLiveQualityEmptyAnswers(t *testing.T) {
	harness := newLiveHarness(t)
	service := qualityservice.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := qualityservice.RepositoryRef{ProjectKey: seeded.Key, Slug: repo.Slug}
	commitID := repo.CommitIDs[0]

	t.Run("build status stats for a commit nobody built", func(t *testing.T) {
		stats, err := service.GetBuildStatusStats(ctx, commitID, true)
		if err != nil {
			t.Fatalf("stats for an unbuilt commit must not fail: %v", err)
		}
		// Zero or absent are both honest; a failure is not.
		if stats.Successful != nil && *stats.Successful != 0 {
			t.Errorf("expected no successful builds, got %d", *stats.Successful)
		}
	})

	t.Run("reports on a commit nobody reported on", func(t *testing.T) {
		reports, err := service.ListReports(ctx, repoRef, commitID, 25)
		if err != nil {
			t.Fatalf("listing reports on an unreported commit must not fail: %v", err)
		}
		if len(reports) != 0 {
			t.Errorf("expected no reports, got %d", len(reports))
		}
	})

	t.Run("annotations on a report that has none", func(t *testing.T) {
		passed := "PASS"
		key := fmt.Sprintf("empty-report-%d", time.Now().UnixNano()%100000)
		if _, err := service.SetReport(ctx, repoRef, commitID, key,
			openapigenerated.SetACodeInsightsReportJSONRequestBody{Title: key, Result: &passed}); err != nil {
			t.Fatalf("set report failed: %v", err)
		}

		annotations, err := service.ListAnnotations(ctx, repoRef, commitID, key)
		if err != nil {
			t.Fatalf("listing annotations on a report with none must not fail: %v", err)
		}
		if len(annotations) != 0 {
			t.Errorf("expected no annotations, got %d", len(annotations))
		}
	})
}
