//go:build live

package live_test

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/safederef"
	qualityservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/quality"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

func TestLiveBuildStatusSetAndGet(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	service := qualityservice.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{Commits: 2, WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	commitID := repo.CommitIDs[0]
	buildKey := testsupport.UniqueName("live-build-")

	first := qualityservice.BuildStatusSetInput{
		Key:   buildKey,
		State: "SUCCESSFUL",
		URL:   "https://example.invalid/live-build",
		Name:  "Live Build",
	}
	err = service.SetBuildStatus(ctx, commitID, first)
	if err != nil {
		t.Fatalf("set build status failed: %v", err)
	}

	// OLDEST, because newest first is what Bitbucket answers when no order
	// arrives at all, so asking for that could not show the order was sent.
	statuses, err := service.GetBuildStatuses(ctx, commitID, 25, "OLDEST")
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
	assertQualityBuildStatusStored(t, statuses, first)

	// A second status gives the order something to decide, and setting it
	// after the read above puts a round trip between the two timestamps.
	second := qualityservice.BuildStatusSetInput{
		Key:   testsupport.UniqueName("live-build-"),
		State: "FAILED",
		URL:   "https://example.invalid/live-build-2",
		Name:  "Live Build 2",
	}
	if err := service.SetBuildStatus(ctx, commitID, second); err != nil {
		t.Fatalf("set second build status failed: %v", err)
	}

	ordered, err := service.GetBuildStatuses(ctx, commitID, 25, "OLDEST")
	if err != nil {
		t.Fatalf("get build statuses oldest first failed: %v", err)
	}
	assertQualityBuildStatusStored(t, ordered, second)
	if keys := qualityBuildStatusKeys(ordered); !slices.Equal(keys, []string{first.Key, second.Key}) {
		t.Errorf("oldest first listed %v, want [%s %s]", keys, first.Key, second.Key)
	}
}

// assertQualityBuildStatusStored checks that a listing holds the build status
// that was set, field for field, rather than something under the same key.
func assertQualityBuildStatusStored(t *testing.T, statuses []openapigenerated.RestBuildStatus, want qualityservice.BuildStatusSetInput) {
	t.Helper()

	for _, status := range statuses {
		if safederef.String(status.Key) != want.Key {
			continue
		}

		state := ""
		if status.State != nil {
			state = string(*status.State)
		}
		if state != want.State || safederef.String(status.Url) != want.URL || safederef.String(status.Name) != want.Name {
			t.Errorf("build status %s is stored as state=%q url=%q name=%q, want state=%q url=%q name=%q",
				want.Key, state, safederef.String(status.Url), safederef.String(status.Name), want.State, want.URL, want.Name)
		}

		return
	}

	t.Errorf("build status %s is not in the listing", want.Key)
}

func qualityBuildStatusKeys(statuses []openapigenerated.RestBuildStatus) []string {
	keys := make([]string, 0, len(statuses))
	for _, status := range statuses {
		keys = append(keys, safederef.String(status.Key))
	}

	return keys
}

func TestLiveCodeInsightsReportSetAndGet(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	service := qualityservice.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	commitID := repo.CommitIDs[0]
	reportKey := testsupport.UniqueName("live-report-")
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
	if report.Result == nil || string(*report.Result) != result {
		t.Fatalf("expected report result=%s, got %#v", result, report.Result)
	}

	if err := service.DeleteReport(
		ctx,
		qualityservice.RepositoryRef{ProjectKey: seeded.Key, Slug: repo.Slug},
		commitID,
		reportKey,
	); err != nil {
		t.Fatalf("delete report failed: %v", err)
	}

	if _, err := service.GetReport(
		ctx,
		qualityservice.RepositoryRef{ProjectKey: seeded.Key, Slug: repo.Slug},
		commitID,
		reportKey,
	); apperrors.ExitCode(err) != 4 {
		t.Fatalf("expected the deleted report to be not_found, got %v", err)
	}
}

func TestLiveRequiredBuildCheckLifecycle(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	service := qualityservice.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{WithCommitIDs: true})
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

	stored, err := service.ListRequiredBuildChecks(ctx, repo, 25)
	if err != nil {
		t.Fatalf("list required build checks after create failed: %v", err)
	}
	assertQualityRequiredCheckStored(t, stored, checkID, []string{"ci"}, "refs/heads/master", "BRANCH")

	// Other keys and another kind of matcher: an update carrying what the check
	// already holds would read back the same whether or not it arrived.
	updated := map[string]any{
		"buildParentKeys": []string{"ci", "lint"},
		"refMatcher": map[string]any{
			"id": "refs/heads/release/*",
			"type": map[string]any{
				"id": "PATTERN",
			},
		},
	}
	if _, err := service.UpdateRequiredBuildCheck(ctx, repo, checkID, updated); err != nil {
		t.Fatalf("update required build check failed: %v", err)
	}

	checks, err := service.ListRequiredBuildChecks(ctx, repo, 25)
	if err != nil {
		t.Fatalf("list required build checks failed: %v", err)
	}
	if len(checks) == 0 {
		t.Fatalf("expected at least one required build check")
	}
	assertQualityRequiredCheckStored(t, checks, checkID, []string{"ci", "lint"}, "refs/heads/release/*", "PATTERN")
	if len(checks) != 1 {
		t.Errorf("the update left %d required build checks, want the one it replaced", len(checks))
	}

	if err := service.DeleteRequiredBuildCheck(ctx, repo, checkID); err != nil {
		t.Fatalf("delete required build check failed: %v", err)
	}

	remaining, err := service.ListRequiredBuildChecks(ctx, repo, 25)
	if err != nil {
		t.Fatalf("list required build checks after delete failed: %v", err)
	}
	for _, check := range remaining {
		if safederef.Int64(check.Id) == checkID {
			t.Errorf("required build check %d survived its delete", checkID)
		}
	}
}

// assertQualityRequiredCheckStored checks the keys and matcher a required build
// check holds. The keys are compared as a set, because Bitbucket does not keep
// the order they were sent in.
func assertQualityRequiredCheckStored(t *testing.T, checks []openapigenerated.RestRequiredBuildCondition, id int64, keys []string, matcherID, matcherType string) {
	t.Helper()

	for _, check := range checks {
		if safederef.Int64(check.Id) != id {
			continue
		}

		storedKeys := slices.Sorted(slices.Values(safederef.StringSlice(check.BuildParentKeys)))
		if !slices.Equal(storedKeys, slices.Sorted(slices.Values(keys))) {
			t.Errorf("required build check %d holds keys %v, want %v", id, storedKeys, keys)
		}

		storedID, storedType := "", ""
		if check.RefMatcher != nil {
			storedID = safederef.String(check.RefMatcher.Id)
			if check.RefMatcher.Type != nil {
				storedType = string(check.RefMatcher.Type.Id)
			}
		}
		if storedID != matcherID || storedType != matcherType {
			t.Errorf("required build check %d matches %s %q, want %s %q", id, storedType, storedID, matcherType, matcherID)
		}

		return
	}

	t.Errorf("required build check %d is not in the listing", id)
}

func TestLiveCodeInsightsAnnotationsLifecycle(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	service := qualityservice.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := qualityservice.RepositoryRef{ProjectKey: seeded.Key, Slug: seeded.Repos[0].Slug}
	commitID := seeded.Repos[0].CommitIDs[0]
	reportKey := testsupport.UniqueName("live-report-annotations-")

	result := "PASS"
	title := "Live Annotations"
	_, err = service.SetReport(ctx, repo, commitID, reportKey, openapigenerated.SetACodeInsightsReportJSONRequestBody{Title: title, Result: &result})
	if err != nil {
		t.Fatalf("set report for annotations failed: %v", err)
	}

	report, err := service.GetReport(ctx, repo, commitID, reportKey)
	if err != nil {
		t.Fatalf("get report for annotations failed: %v", err)
	}
	if safederef.String(report.Title) != title || report.Result == nil || string(*report.Result) != result {
		t.Fatalf("report stored as title=%q result=%v, want %q and %s", safederef.String(report.Title), report.Result, title, result)
	}

	externalID := testsupport.UniqueName("ann-")
	survivorID := testsupport.UniqueName("ann-kept-")
	path := "seed.txt"
	line := int32(1)
	annotations := []openapigenerated.RestSingleAddInsightAnnotationRequest{{
		ExternalId: &externalID,
		Message:    "integration annotation",
		Severity:   "LOW",
		Path:       &path,
		Line:       &line,
	}, {
		// The delete below names the other annotation, and this one has to
		// survive it: without an external id the endpoint deletes them all.
		ExternalId: &survivorID,
		Message:    "annotation the delete must keep",
		Severity:   "HIGH",
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
	for _, sent := range annotations {
		assertQualityAnnotationStored(t, listed, sent)
	}

	if err := service.DeleteAnnotations(ctx, repo, commitID, reportKey, externalID); err != nil {
		t.Fatalf("delete annotations failed: %v", err)
	}

	remaining, err := service.ListAnnotations(ctx, repo, commitID, reportKey)
	if err != nil {
		t.Fatalf("list annotations after delete failed: %v", err)
	}
	if kept := qualityAnnotationIDs(remaining); !slices.Equal(kept, []string{survivorID}) {
		t.Errorf("after deleting %s the report holds %v, want only %s", externalID, kept, survivorID)
	}

	if err := service.DeleteReport(ctx, repo, commitID, reportKey); err != nil {
		t.Fatalf("delete report failed: %v", err)
	}

	if _, err := service.GetReport(ctx, repo, commitID, reportKey); apperrors.ExitCode(err) != 4 {
		t.Fatalf("expected the deleted report to be not_found, got %v", err)
	}
}

// assertQualityAnnotationStored checks that a listing holds the annotation that
// was sent, field for field.
func assertQualityAnnotationStored(t *testing.T, listed []openapigenerated.RestInsightAnnotation, want openapigenerated.RestSingleAddInsightAnnotationRequest) {
	t.Helper()

	wantID := safederef.String(want.ExternalId)
	for _, annotation := range listed {
		if safederef.String(annotation.ExternalId) != wantID {
			continue
		}

		got := fmt.Sprintf("message=%q severity=%q path=%q line=%d", safederef.String(annotation.Message),
			safederef.String(annotation.Severity), safederef.String(annotation.Path), safederef.Int32(annotation.Line))
		sent := fmt.Sprintf("message=%q severity=%q path=%q line=%d", want.Message,
			want.Severity, safederef.String(want.Path), safederef.Int32(want.Line))
		if got != sent {
			t.Errorf("annotation %s is stored as %s, want %s", wantID, got, sent)
		}

		return
	}

	t.Errorf("annotation %s is not on the report", wantID)
}

func qualityAnnotationIDs(annotations []openapigenerated.RestInsightAnnotation) []string {
	ids := make([]string, 0, len(annotations))
	for _, annotation := range annotations {
		ids = append(ids, safederef.String(annotation.ExternalId))
	}

	return ids
}

func TestLiveCLIInsightsReportSetDryRunNoSideEffect(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	commitID := repo.CommitIDs[0]
	reportKey := testsupport.UniqueName("live-dryrun-report-")

	listBeforeOutput, err := executeLiveCLI(t, "--json", "insights", "report", "list", commitID, "--limit", "200")
	if err != nil {
		t.Fatalf("insights report list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	body := fmt.Sprintf(`{"title":"Dry Run Report %s","result":"PASS"}`, reportKey)
	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "insights", "report", "set", commitID, reportKey, "--body", body)
	if err != nil {
		t.Fatalf("insights report set dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	assertLivePreviewOf(t, dryRunOutput, "insights report set", jsonoutput.OutcomeWouldApply)

	listAfterOutput, err := executeLiveCLI(t, "--json", "insights", "report", "list", commitID, "--limit", "200")
	if err != nil {
		t.Fatalf("insights report list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no report side-effect from set dry-run\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}
}

func TestLiveCLIBuildStatusSetDryRunNoSideEffect(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{WithCommitIDs: true})
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

	statusKey := testsupport.UniqueName("live-dryrun-status-")
	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "build", "status", "set", commitID, "--key", statusKey, "--state", "SUCCESSFUL", "--url", "https://example.invalid/dryrun")
	if err != nil {
		t.Fatalf("build status set dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	assertLivePreviewOf(t, dryRunOutput, "build status set", jsonoutput.OutcomeWouldApply)

	statsAfterOutput, err := executeLiveCLI(t, "--json", "build", "status", "stats", commitID)
	if err != nil {
		t.Fatalf("build status stats after failed: %v\noutput: %s", err, statsAfterOutput)
	}

	if statsBeforeOutput != statsAfterOutput {
		t.Fatalf("expected no build-status side-effect from set dry-run\nbefore: %s\nafter: %s", statsBeforeOutput, statsAfterOutput)
	}
}

func TestLiveCLIInsightsReportDeleteDryRunNoSideEffect(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	commitID := repo.CommitIDs[0]
	reportKey := testsupport.UniqueName("live-dryrun-report-del-")
	body := fmt.Sprintf(`{"title":"Dry Run Report Delete %s","result":"PASS"}`, reportKey)

	setOutput, err := executeLiveCLI(t, "--json", "insights", "report", "set", commitID, reportKey, "--body", body)
	if err != nil {
		t.Fatalf("insights report set fixture failed: %v\noutput: %s", err, setOutput)
	}
	assertQualityCLIReportStored(t, commitID, reportKey, "Dry Run Report Delete "+reportKey, "PASS")

	listBeforeOutput, err := executeLiveCLI(t, "--json", "insights", "report", "list", commitID, "--limit", "200")
	if err != nil {
		t.Fatalf("insights report list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "insights", "report", "delete", commitID, reportKey, "--yes")
	if err != nil {
		t.Fatalf("insights report delete dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	assertLivePreviewOf(t, dryRunOutput, "insights report delete", jsonoutput.OutcomeWouldApply)

	listAfterOutput, err := executeLiveCLI(t, "--json", "insights", "report", "list", commitID, "--limit", "200")
	if err != nil {
		t.Fatalf("insights report list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no report side-effect from delete dry-run\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}

	mustLiveCLI(t, "insights", "report", "delete", commitID, reportKey, "--yes")
	assertQualityCLIReportGone(t, commitID, reportKey)
}

// assertQualityCLIReportStored reads a report back through bb and checks the
// title and result it holds.
func assertQualityCLIReportStored(t *testing.T, commitID, key, title, result string) {
	t.Helper()

	stored := decodeJSONMap(t, mustLiveCLI(t, "insights", "report", "get", commitID, key))
	if stored["key"] != key || stored["title"] != title || stored["result"] != result {
		t.Fatalf("report %s is stored as key=%v title=%v result=%v, want %q, %q and %s",
			key, stored["key"], stored["title"], stored["result"], key, title, result)
	}
}

// assertQualityCLIReportGone checks that reading a report back finds nothing.
func assertQualityCLIReportGone(t *testing.T, commitID, key string) {
	t.Helper()

	output, err := executeLiveCLI(t, "--json", "insights", "report", "get", commitID, key)
	if code := apperrors.ExitCode(err); code != 4 {
		t.Fatalf("report %s after its delete: exit %d, want 4 (not_found): %v\noutput: %s", key, code, err, output)
	}
}

func TestLiveCLIInsightsAnnotationAddDeleteDryRunNoSideEffect(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	commitID := repo.CommitIDs[0]
	reportKey := testsupport.UniqueName("live-dryrun-ann-report-")
	body := fmt.Sprintf(`{"title":"Dry Run Annotation Report %s","result":"PASS"}`, reportKey)

	setOutput, err := executeLiveCLI(t, "--json", "insights", "report", "set", commitID, reportKey, "--body", body)
	if err != nil {
		t.Fatalf("insights report set fixture failed: %v\noutput: %s", err, setOutput)
	}
	assertQualityCLIReportStored(t, commitID, reportKey, "Dry Run Annotation Report "+reportKey, "PASS")

	listBeforeOutput, err := executeLiveCLI(t, "--json", "insights", "annotation", "list", commitID, reportKey)
	if err != nil {
		t.Fatalf("insights annotation list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	externalID := testsupport.UniqueName("live-dryrun-ann-")
	annotationBody := fmt.Sprintf(`[{"externalId":"%s","message":"dry-run annotation","severity":"LOW","path":"seed.txt","line":1}]`, externalID)

	addDryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "insights", "annotation", "add", commitID, reportKey, "--body", annotationBody)
	if err != nil {
		t.Fatalf("insights annotation add dry-run failed: %v\noutput: %s", err, addDryRunOutput)
	}
	assertLivePreviewOf(t, addDryRunOutput, "insights annotation add", jsonoutput.OutcomeWouldApply)

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

	var stored []map[string]any
	decodeJSONData(t, listBeforeDeleteOutput, &stored)
	want := map[string]any{"externalId": externalID, "reportKey": reportKey, "message": "dry-run annotation", "severity": "LOW", "path": "seed.txt", "line": float64(1)}
	if len(stored) != 1 {
		t.Fatalf("want the one annotation added, got %d: %s", len(stored), listBeforeDeleteOutput)
	}
	for field, value := range want {
		if stored[0][field] != value {
			t.Errorf("annotation %s = %v, want %v", field, stored[0][field], value)
		}
	}

	deleteDryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "insights", "annotation", "delete", commitID, reportKey, "--external-id", externalID, "--yes")
	if err != nil {
		t.Fatalf("insights annotation delete dry-run failed: %v\noutput: %s", err, deleteDryRunOutput)
	}
	assertLivePreviewOf(t, deleteDryRunOutput, "insights annotation delete", jsonoutput.OutcomeWouldApply)

	listAfterDeleteOutput, err := executeLiveCLI(t, "--json", "insights", "annotation", "list", commitID, reportKey)
	if err != nil {
		t.Fatalf("insights annotation list after delete dry-run failed: %v\noutput: %s", err, listAfterDeleteOutput)
	}
	if listBeforeDeleteOutput != listAfterDeleteOutput {
		t.Fatalf("expected no annotation side-effect from delete dry-run\nbefore: %s\nafter: %s", listBeforeDeleteOutput, listAfterDeleteOutput)
	}

	mustLiveCLI(t, "insights", "annotation", "delete", commitID, reportKey, "--external-id", externalID, "--yes")
	var remaining []map[string]any
	decodeJSONData(t, mustLiveCLI(t, "insights", "annotation", "list", commitID, reportKey), &remaining)
	if len(remaining) != 0 {
		t.Errorf("the annotation survived its delete: %v", remaining)
	}

	mustLiveCLI(t, "insights", "report", "delete", commitID, reportKey, "--yes")
	assertQualityCLIReportGone(t, commitID, reportKey)
}

func TestLiveCLIBuildRequiredCreateUpdateDeleteDryRunNoSideEffect(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{WithCommitIDs: true})
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
	assertLivePreviewOf(t, createDryRunOutput, "build required create", jsonoutput.OutcomeWouldApply)

	listAfterCreateOutput, err := executeLiveCLI(t, "--json", "build", "required", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("build required list after create dry-run failed: %v\noutput: %s", err, listAfterCreateOutput)
	}
	if listBeforeCreateOutput != listAfterCreateOutput {
		t.Fatalf("expected no required-build side-effect from create dry-run\nbefore: %s\nafter: %s", listBeforeCreateOutput, listAfterCreateOutput)
	}

	requiredID := createRequiredBuildCheckWithRetry(t, body)

	listBeforeUpdateOutput, err := executeLiveCLI(t, "--json", "build", "required", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("build required list before update dry-run failed: %v\noutput: %s", err, listBeforeUpdateOutput)
	}
	assertQualityCLIRequiredCheckStored(t, listBeforeUpdateOutput, requiredID, []string{"ci"}, "refs/heads/master", "BRANCH")

	// Not the body the check was created with. Previewing an update to what is
	// already stored, a preview that leaked into a real update would leave the
	// listing exactly as it was, and the comparison below would pass.
	updateBody := `{"buildParentKeys":["ci","dry-run"],"refMatcher":{"id":"refs/heads/release/*","type":{"id":"PATTERN"}}}`
	updateDryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "build", "required", "update", requiredID, "--body", updateBody)
	if err != nil {
		t.Fatalf("build required update dry-run failed: %v\noutput: %s", err, updateDryRunOutput)
	}
	assertLivePreviewOf(t, updateDryRunOutput, "build required update", jsonoutput.OutcomeWouldApply)

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

	deleteDryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "build", "required", "delete", requiredID, "--yes")
	if err != nil {
		t.Fatalf("build required delete dry-run failed: %v\noutput: %s", err, deleteDryRunOutput)
	}
	assertLivePreviewOf(t, deleteDryRunOutput, "build required delete", jsonoutput.OutcomeWouldApply)

	listAfterDeleteOutput, err := executeLiveCLI(t, "--json", "build", "required", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("build required list after delete dry-run failed: %v\noutput: %s", err, listAfterDeleteOutput)
	}
	if listBeforeDeleteOutput != listAfterDeleteOutput {
		t.Fatalf("expected no required-build side-effect from delete dry-run\nbefore: %s\nafter: %s", listBeforeDeleteOutput, listAfterDeleteOutput)
	}

	mustLiveCLI(t, "build", "required", "delete", requiredID, "--yes")
	var remaining []any
	decodeJSONData(t, mustLiveCLI(t, "build", "required", "list", "--limit", "200"), &remaining)
	if _, found := findByID(remaining, requiredID); found {
		t.Errorf("required build check %s survived its delete: %v", requiredID, remaining)
	}
}

// assertQualityCLIRequiredCheckStored checks the keys and matcher a required
// build check holds, read from bb's own listing. The keys are a set to
// Bitbucket, which does not keep the order they were sent in.
func assertQualityCLIRequiredCheckStored(t *testing.T, listing, id string, keys []string, matcherID, matcherType string) {
	t.Helper()

	var checks []any
	decodeJSONData(t, listing, &checks)
	check, found := findByID(checks, id)
	if !found {
		t.Fatalf("required build check %s is not in the listing: %s", id, listing)
	}

	var stored struct {
		BuildParentKeys []string `json:"buildParentKeys"`
		RefMatcher      struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"refMatcher"`
	}
	encoded, err := json.Marshal(check)
	if err != nil {
		t.Fatalf("re-encode required build check %s: %v", id, err)
	}
	if err := json.Unmarshal(encoded, &stored); err != nil {
		t.Fatalf("decode required build check %s: %v", id, err)
	}

	if got, want := slices.Sorted(slices.Values(stored.BuildParentKeys)), slices.Sorted(slices.Values(keys)); !slices.Equal(got, want) {
		t.Errorf("required build check %s holds keys %v, want %v", id, got, want)
	}
	if stored.RefMatcher.ID != matcherID || stored.RefMatcher.Type != matcherType {
		t.Errorf("required build check %s matches %s %q, want %s %q", id, stored.RefMatcher.Type, stored.RefMatcher.ID, matcherType, matcherID)
	}
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
	t.Parallel()

	harness := newLiveHarness(t)
	service := qualityservice.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := qualityservice.RepositoryRef{ProjectKey: seeded.Key, Slug: repo.Slug}
	commitID := repo.CommitIDs[0]

	const total = 30

	t.Run("build statuses", func(t *testing.T) {
		sent := make([]qualityservice.BuildStatusSetInput, 0, total)
		for index := range total {
			key := fmt.Sprintf("paged-build-%s-%d", testsupport.UniqueSuffix(), index)
			input := qualityservice.BuildStatusSetInput{
				Key:   key,
				State: "SUCCESSFUL",
				URL:   "https://ci.example.invalid/" + key,
				Name:  key,
			}
			if err := service.SetBuildStatus(ctx, commitID, input); err != nil {
				t.Fatalf("set build status %d failed: %v", index, err)
			}
			sent = append(sent, input)
		}

		// More than one page, so the loop has to come back for the rest.
		//
		// OLDEST rather than NEWEST, which is what Bitbucket answers when no
		// order arrives: only an order that differs from that can show it was
		// sent.
		statuses, err := service.GetBuildStatuses(ctx, commitID, total+10, "OLDEST")
		if err != nil {
			t.Fatalf("get build statuses failed: %v", err)
		}
		if len(statuses) < total {
			t.Fatalf("paging stopped early: got %d build statuses, want at least %d", len(statuses), total)
		}
		// Every status once, with what was sent: pages that overlap or skip can
		// still add up to the right count.
		for _, input := range sent {
			assertQualityBuildStatusStored(t, statuses, input)
		}
		assertQualityNoRepeats(t, qualityBuildStatusKeys(statuses))

		if capped, err := service.GetBuildStatuses(ctx, commitID, 5, "OLDEST"); err != nil {
			t.Fatalf("get capped build statuses failed: %v", err)
		} else if len(capped) != 5 {
			t.Fatalf("a cap of 5 returned %d build statuses", len(capped))
		} else {
			// The first status set and the last, which lie far enough apart that
			// no shared timestamp can swap them across the cap.
			keys := qualityBuildStatusKeys(capped)
			if !slices.Contains(keys, sent[0].Key) || slices.Contains(keys, sent[total-1].Key) {
				t.Errorf("the five oldest are %v: want %s among them and %s not", keys, sent[0].Key, sent[total-1].Key)
			}
		}
	})

	t.Run("insight reports", func(t *testing.T) {
		passed := "PASS"
		keys := make([]string, 0, total)
		for index := range total {
			key := fmt.Sprintf("paged-report-%s-%d", testsupport.UniqueSuffix(), index)
			if _, err := service.SetReport(ctx, repoRef, commitID, key,
				openapigenerated.SetACodeInsightsReportJSONRequestBody{Title: key, Result: &passed}); err != nil {
				t.Fatalf("set report %d failed: %v", index, err)
			}
			keys = append(keys, key)
		}

		reports, err := service.ListReports(ctx, repoRef, commitID, total+10)
		if err != nil {
			t.Fatalf("list reports failed: %v", err)
		}
		if len(reports) < total {
			t.Fatalf("paging stopped early: got %d reports, want at least %d", len(reports), total)
		}

		stored := map[string]openapigenerated.RestInsightReport{}
		listedKeys := make([]string, 0, len(reports))
		for _, report := range reports {
			stored[safederef.String(report.Key)] = report
			listedKeys = append(listedKeys, safederef.String(report.Key))
		}
		assertQualityNoRepeats(t, listedKeys)
		for _, key := range keys {
			report, found := stored[key]
			if !found {
				t.Errorf("report %s is not in the listing", key)

				continue
			}
			if safederef.String(report.Title) != key || report.Result == nil || string(*report.Result) != passed {
				t.Errorf("report %s is stored as title=%q result=%v, want %q and %s", key, safederef.String(report.Title), report.Result, key, passed)
			}
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

		// Found by matcher, which is what each create made distinct.
		byMatcher := map[string]openapigenerated.RestRequiredBuildCondition{}
		matchers := make([]string, 0, len(checks))
		for _, check := range checks {
			if check.RefMatcher != nil {
				byMatcher[safederef.String(check.RefMatcher.Id)] = check
				matchers = append(matchers, safederef.String(check.RefMatcher.Id))
			}
		}
		assertQualityNoRepeats(t, matchers)
		for index := range total {
			check, found := byMatcher[fmt.Sprintf("refs/heads/paged-%d", index)]
			if !found {
				t.Errorf("no required build check on refs/heads/paged-%d in the listing", index)

				continue
			}
			assertQualityRequiredCheckStored(t, checks, safederef.Int64(check.Id),
				[]string{fmt.Sprintf("ci-%d", index)}, fmt.Sprintf("refs/heads/paged-%d", index), "BRANCH")
		}

		if capped, err := service.ListRequiredBuildChecks(ctx, repoRef, 5); err != nil {
			t.Fatalf("list capped required build checks failed: %v", err)
		} else if len(capped) != 5 {
			t.Fatalf("a cap of 5 returned %d checks", len(capped))
		}
	})
}

// assertQualityNoRepeats fails when a listing names one entry twice, which is
// what pages that overlap produce.
func assertQualityNoRepeats(t *testing.T, names []string) {
	t.Helper()

	seen := map[string]bool{}
	for _, name := range names {
		if seen[name] {
			t.Errorf("%s is listed more than once: %v", name, names)
		}
		seen[name] = true
	}
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
	t.Parallel()

	harness := newLiveHarness(t)
	service := qualityservice.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := qualityservice.RepositoryRef{ProjectKey: seeded.Key, Slug: repo.Slug}
	commitID := repo.CommitIDs[0]

	t.Run("build status stats for a commit nobody built", func(t *testing.T) {
		// includeUnique is not read back: it adds a result only on a built commit, and RestBuildStats has no field to carry one.
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
		key := testsupport.UniqueName("empty-report-")
		created, err := service.SetReport(ctx, repoRef, commitID, key,
			openapigenerated.SetACodeInsightsReportJSONRequestBody{Title: key, Result: &passed})
		if err != nil {
			t.Fatalf("set report failed: %v", err)
		}

		// Writing a report answers with the report, not with 204 and nothing.
		//
		// A unit test asserted the other shape -- an empty body yielding a
		// zero-value report -- and the service still carries the branch that
		// produces it. Bitbucket answers 200 with the whole object here and on
		// the read, so that branch is defensive rather than something the
		// server exercises, and pinning what it does send is what says so.
		if created.Key == nil || *created.Key != key {
			t.Errorf("set report answered with %#v, want the report it just wrote", created)
		}

		fetched, err := service.GetReport(ctx, repoRef, commitID, key)
		if err != nil {
			t.Fatalf("get report failed: %v", err)
		}
		if fetched.Key == nil || *fetched.Key != key {
			t.Errorf("get report answered with %#v, want the report that is there", fetched)
		}
		if safederef.String(fetched.Title) != key || fetched.Result == nil || string(*fetched.Result) != passed {
			t.Errorf("report %s is stored as title=%q result=%v, want %q and %s", key, safederef.String(fetched.Title), fetched.Result, key, passed)
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
