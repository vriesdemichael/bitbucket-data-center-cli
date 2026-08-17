//go:build live

package live_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli"
)

func TestLiveCLIDiffOutputModes(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 2)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	from := repo.CommitIDs[len(repo.CommitIDs)-1]
	to := repo.CommitIDs[0]

	nameOnlyOutput, err := executeLiveCLI(t, "diff", "refs", from, to, "--name-only")
	if err != nil {
		t.Fatalf("diff refs --name-only failed: %v\noutput: %s", err, nameOnlyOutput)
	}
	if !strings.Contains(nameOnlyOutput, "seed.txt") {
		t.Fatalf("expected changed file in --name-only output, got: %s", nameOnlyOutput)
	}

	statOutput, err := executeLiveCLI(t, "--json", "diff", "refs", from, to, "--stat")
	if err != nil {
		t.Fatalf("diff refs --stat failed: %v\noutput: %s", err, statOutput)
	}
	statPayload := decodeJSONMap(t, statOutput)
	if _, ok := statPayload["stats"]; !ok {
		t.Fatalf("expected stats field in --stat output, got: %s", statOutput)
	}

	patchOutput, err := executeLiveCLI(t, "diff", "refs", from, to, "--patch")
	if err != nil {
		t.Fatalf("diff refs --patch failed: %v\noutput: %s", err, patchOutput)
	}
	if !strings.Contains(patchOutput, "diff --git") {
		t.Fatalf("expected patch output, got: %s", patchOutput)
	}

	_, err = executeLiveCLI(t, "diff", "refs", from, to, "--patch", "--stat")
	if err == nil {
		t.Fatalf("expected validation error for conflicting diff output modes")
	}
}

func TestLiveCLIDiffPRAndCommitHumanOutput(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 2)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	branch := fmt.Sprintf("lt-diff-cli-%d", time.Now().UnixNano()%100000)
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "diff-feature.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	pullRequestID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
	if err != nil {
		t.Fatalf("create pull request failed: %v", err)
	}

	prDiffOutput, err := executeLiveCLI(t, "diff", "pr", pullRequestID, "--patch")
	if err != nil {
		t.Fatalf("diff pr failed: %v\noutput: %s", err, prDiffOutput)
	}
	if !strings.Contains(prDiffOutput, "diff --git") {
		t.Fatalf("expected patch output for diff pr, got: %s", prDiffOutput)
	}

	// `bb pr diff` is the gh-shaped spelling of the same command. Asserting the
	// two produce identical output is what makes it an alias rather than a
	// second implementation that can drift.
	prDiffAliasOutput, err := executeLiveCLI(t, "pr", "diff", pullRequestID, "--patch")
	if err != nil {
		t.Fatalf("pr diff failed: %v\noutput: %s", err, prDiffAliasOutput)
	}
	if prDiffAliasOutput != prDiffOutput {
		t.Fatalf("pr diff and diff pr disagree:\n pr diff: %s\n diff pr: %s", prDiffAliasOutput, prDiffOutput)
	}

	commitDiffOutput, err := executeLiveCLI(t, "diff", "commit", repo.CommitIDs[0], "--path", "seed.txt")
	if err != nil {
		t.Fatalf("diff commit failed: %v\noutput: %s", err, commitDiffOutput)
	}
	if !strings.Contains(commitDiffOutput, "diff --git") && !strings.Contains(commitDiffOutput, "\"diffs\"") {
		t.Fatalf("expected diff payload for diff commit, got: %s", commitDiffOutput)
	}
}

func TestLiveCLIInsightsLifecycle(t *testing.T) {
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
	reportKey := fmt.Sprintf("live-cli-report-%d", time.Now().UnixNano()%100000)
	externalID := fmt.Sprintf("live-cli-ann-%d", time.Now().UnixNano()%100000)

	reportBody := `{"title":"Live CLI Insights","result":"PASS","details":"cli lifecycle"}`
	setOutput, err := executeLiveCLI(t, "--json", "insights", "report", "set", commitID, reportKey, "--body", reportBody)
	if err != nil {
		t.Fatalf("insights report set failed: %v\noutput: %s", err, setOutput)
	}
	setPayload := decodeJSONMap(t, setOutput)
	if asString(setPayload["key"]) != reportKey {
		t.Fatalf("expected report key=%s, got output: %s", reportKey, setOutput)
	}

	listOutput, err := executeLiveCLI(t, "--json", "insights", "report", "list", commitID)
	if err != nil {
		t.Fatalf("insights report list failed: %v\noutput: %s", err, listOutput)
	}
	if !jsonArrayContainsKey(t, listOutput, reportKey) {
		t.Fatalf("expected report key %s in list output: %s", reportKey, listOutput)
	}

	annotationBody := fmt.Sprintf(`[{"externalId":"%s","message":"integration annotation","severity":"LOW","path":"seed.txt","line":1}]`, externalID)
	addAnnotationOutput, err := executeLiveCLI(t, "--json", "insights", "annotation", "add", commitID, reportKey, "--body", annotationBody)
	if err != nil {
		t.Fatalf("insights annotation add failed: %v\noutput: %s", err, addAnnotationOutput)
	}
	addPayload := decodeJSONMap(t, addAnnotationOutput)
	if count, ok := addPayload["count"].(float64); !ok || count < 1 {
		t.Fatalf("expected count >= 1 in add annotation output, got: %s", addAnnotationOutput)
	}

	listAnnotationOutput, err := executeLiveCLI(t, "--json", "insights", "annotation", "list", commitID, reportKey)
	if err != nil {
		t.Fatalf("insights annotation list failed: %v\noutput: %s", err, listAnnotationOutput)
	}
	if !jsonArrayContainsExternalID(t, listAnnotationOutput, externalID) {
		t.Fatalf("expected annotation external id %s in output: %s", externalID, listAnnotationOutput)
	}

	deleteAnnotationOutput, err := executeLiveCLI(t, "--json", "insights", "annotation", "delete", commitID, reportKey, "--external-id", externalID)
	if err != nil {
		t.Fatalf("insights annotation delete failed: %v\noutput: %s", err, deleteAnnotationOutput)
	}

	deleteReportOutput, err := executeLiveCLI(t, "--json", "insights", "report", "delete", commitID, reportKey)
	if err != nil {
		t.Fatalf("insights report delete failed: %v\noutput: %s", err, deleteReportOutput)
	}
	deletePayload := decodeJSONMap(t, deleteReportOutput)
	if asString(deletePayload["status"]) != "ok" {
		t.Fatalf("expected delete status ok, got: %s", deleteReportOutput)
	}
}

func TestLiveCLIBuildAndTagLifecycle(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 2)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	commitID := repo.CommitIDs[0]
	buildKey := fmt.Sprintf("live-cli-build-%d", time.Now().UnixNano()%100000)

	setBuildOutput, err := executeLiveCLI(
		t,
		"--json", "build", "status", "set", commitID,
		"--key", buildKey,
		"--state", "SUCCESSFUL",
		"--url", "https://example.invalid/live-cli-build",
		"--name", "Live CLI Build",
		"--description", "build status for coverage",
		"--ref", "refs/heads/master",
		"--parent", "ci",
		"--build-number", "42",
		"--duration-ms", "123",
	)
	if err != nil {
		t.Fatalf("build status set failed: %v\noutput: %s", err, setBuildOutput)
	}
	if asString(decodeJSONMap(t, setBuildOutput)["status"]) != "ok" {
		t.Fatalf("expected build set status ok, got: %s", setBuildOutput)
	}

	getBuildOutput, err := executeLiveCLI(t, "--json", "build", "status", "get", commitID)
	if err != nil {
		t.Fatalf("build status get failed: %v\noutput: %s", err, getBuildOutput)
	}
	if !jsonArrayContainsKey(t, getBuildOutput, buildKey) {
		t.Fatalf("expected build key %s in output: %s", buildKey, getBuildOutput)
	}

	statsBuildOutput, err := executeLiveCLI(t, "--json", "build", "status", "stats", commitID)
	if err != nil {
		t.Fatalf("build status stats failed: %v\noutput: %s", err, statsBuildOutput)
	}
	statsPayload := decodeJSONMap(t, statsBuildOutput)
	if _, ok := statsPayload["successful"]; !ok {
		t.Fatalf("expected successful field in build stats output, got: %s", statsBuildOutput)
	}

	tagName := fmt.Sprintf("v-live-cli-%d", time.Now().UnixNano()%100000)
	createTagOutput, err := executeLiveCLI(t, "--json", "tag", "create", tagName, "--start-point", commitID, "--message", "live cli tag")
	if err != nil {
		t.Fatalf("tag create failed: %v\noutput: %s", err, createTagOutput)
	}
	createTagPayload := decodeJSONMap(t, createTagOutput)
	if asString(createTagPayload["displayId"]) != tagName {
		t.Fatalf("expected created tag %s, got: %s", tagName, createTagOutput)
	}

	viewTagOutput, err := executeLiveCLI(t, "--json", "tag", "view", tagName)
	if err != nil {
		t.Fatalf("tag view failed: %v\noutput: %s", err, viewTagOutput)
	}
	viewTagPayload := decodeJSONMap(t, viewTagOutput)
	if asString(viewTagPayload["displayId"]) != tagName {
		t.Fatalf("expected viewed tag %s, got: %s", tagName, viewTagOutput)
	}

	listTagOutput, err := executeLiveCLI(t, "tag", "list", "--limit", "50", "--order-by", "ALPHABETICAL", "--filter", "v-live-cli")
	if err != nil {
		t.Fatalf("tag list (human) failed: %v\noutput: %s", err, listTagOutput)
	}
	if !strings.Contains(listTagOutput, tagName) {
		t.Fatalf("expected tag name in human tag list output, got: %s", listTagOutput)
	}

	deleteTagOutput, err := executeLiveCLI(t, "--json", "tag", "delete", tagName)
	if err != nil {
		t.Fatalf("tag delete failed: %v\noutput: %s", err, deleteTagOutput)
	}
	if asString(decodeJSONMap(t, deleteTagOutput)["status"]) != "ok" {
		t.Fatalf("expected tag delete status ok, got: %s", deleteTagOutput)
	}
}

func TestLiveCLIBuildRequiredAndInsightsHumanOutput(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 2)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	commitID := repo.CommitIDs[0]
	buildKey := fmt.Sprintf("live-cli-build-human-%d", time.Now().UnixNano()%100000)

	setBuildOutput, err := executeLiveCLI(
		t,
		"build", "status", "set", commitID,
		"--key", buildKey,
		"--state", "SUCCESSFUL",
		"--url", "https://example.invalid/live-cli-build-human",
	)
	if err != nil {
		t.Fatalf("build status set (human) failed: %v\noutput: %s", err, setBuildOutput)
	}
	if !strings.Contains(setBuildOutput, "Build status") {
		t.Fatalf("expected human build set output, got: %s", setBuildOutput)
	}

	getBuildOutput, err := executeLiveCLI(t, "build", "status", "get", commitID)
	if err != nil {
		t.Fatalf("build status get (human) failed: %v\noutput: %s", err, getBuildOutput)
	}
	if !strings.Contains(getBuildOutput, buildKey) || !strings.Contains(getBuildOutput, "SUCCESSFUL") {
		t.Fatalf("expected key/state in human get output, got: %s", getBuildOutput)
	}

	statsBuildOutput, err := executeLiveCLI(t, "build", "status", "stats", commitID)
	if err != nil {
		t.Fatalf("build status stats (human) failed: %v\noutput: %s", err, statsBuildOutput)
	}
	if !strings.Contains(statsBuildOutput, "Successful:") {
		t.Fatalf("expected human stats output, got: %s", statsBuildOutput)
	}

	requiredBody := `{"buildParentKeys":["ci"],"refMatcher":{"id":"refs/heads/master"}}`
	requiredID, requiredAvailable := createRequiredBuildCheckWithRetry(t, requiredBody)
	if requiredAvailable {
		requiredListOutput, err := executeLiveCLI(t, "build", "required", "list")
		if err != nil {
			t.Fatalf("build required list (human) failed: %v\noutput: %s", err, requiredListOutput)
		}
		if !strings.Contains(requiredListOutput, "id=") || !strings.Contains(requiredListOutput, "buildParentKeys=") {
			t.Fatalf("expected human required list output, got: %s", requiredListOutput)
		}

		updateRequiredOutput, err := executeLiveCLI(t, "--json", "build", "required", "update", requiredID, "--body", requiredBody)
		if err != nil {
			t.Fatalf("build required update failed: %v\noutput: %s", err, updateRequiredOutput)
		}

		deleteRequiredOutput, err := executeLiveCLI(t, "build", "required", "delete", requiredID)
		if err != nil {
			t.Fatalf("build required delete (human) failed: %v\noutput: %s", err, deleteRequiredOutput)
		}
		if !strings.Contains(deleteRequiredOutput, "Deleted required build merge check") {
			t.Fatalf("expected human required delete output, got: %s", deleteRequiredOutput)
		}
	}

	reportKey := fmt.Sprintf("live-cli-insights-human-%d", time.Now().UnixNano()%100000)
	externalID := fmt.Sprintf("live-cli-insights-ann-%d", time.Now().UnixNano()%100000)
	reportBody := `{"title":"Live CLI Insights Human","result":"PASS","details":"human output coverage"}`

	setReportOutput, err := executeLiveCLI(t, "--json", "insights", "report", "set", commitID, reportKey, "--body", reportBody)
	if err != nil {
		t.Fatalf("insights report set failed: %v\noutput: %s", err, setReportOutput)
	}

	getReportOutput, err := executeLiveCLI(t, "--json", "insights", "report", "get", commitID, reportKey)
	if err != nil {
		t.Fatalf("insights report get failed: %v\noutput: %s", err, getReportOutput)
	}
	if asString(decodeJSONMap(t, getReportOutput)["key"]) != reportKey {
		t.Fatalf("expected report key=%s, got: %s", reportKey, getReportOutput)
	}

	listReportOutput, err := executeLiveCLI(t, "insights", "report", "list", commitID, "--limit", "25")
	if err != nil {
		t.Fatalf("insights report list (human) failed: %v\noutput: %s", err, listReportOutput)
	}
	if !strings.Contains(listReportOutput, reportKey) || !strings.Contains(listReportOutput, "PASS") {
		t.Fatalf("expected report key/result in human list output, got: %s", listReportOutput)
	}

	annotationBody := fmt.Sprintf(`[{"externalId":"%s","message":"human annotation","severity":"LOW","path":"seed.txt","line":1}]`, externalID)
	addAnnotationOutput, err := executeLiveCLI(t, "--json", "insights", "annotation", "add", commitID, reportKey, "--body", annotationBody)
	if err != nil {
		t.Fatalf("insights annotation add failed: %v\noutput: %s", err, addAnnotationOutput)
	}

	listAnnotationOutput, err := executeLiveCLI(t, "insights", "annotation", "list", commitID, reportKey)
	if err != nil {
		t.Fatalf("insights annotation list (human) failed: %v\noutput: %s", err, listAnnotationOutput)
	}
	if !strings.Contains(listAnnotationOutput, externalID) || !strings.Contains(listAnnotationOutput, "human annotation") {
		t.Fatalf("expected annotation details in human list output, got: %s", listAnnotationOutput)
	}

	deleteAnnotationOutput, err := executeLiveCLI(t, "insights", "annotation", "delete", commitID, reportKey, "--external-id", externalID)
	if err != nil {
		t.Fatalf("insights annotation delete (human) failed: %v\noutput: %s", err, deleteAnnotationOutput)
	}
	if !strings.Contains(deleteAnnotationOutput, "Deleted annotations") {
		t.Fatalf("expected human annotation delete output, got: %s", deleteAnnotationOutput)
	}

	deleteReportOutput, err := executeLiveCLI(t, "insights", "report", "delete", commitID, reportKey)
	if err != nil {
		t.Fatalf("insights report delete (human) failed: %v\noutput: %s", err, deleteReportOutput)
	}
	if !strings.Contains(deleteReportOutput, "Deleted report") {
		t.Fatalf("expected human report delete output, got: %s", deleteReportOutput)
	}
}

func TestLiveCLIAuthStoredConfigFlow(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "bb-config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "0")
	t.Setenv("BITBUCKET_URL", "")
	t.Setenv("BITBUCKET_TOKEN", "")
	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("BITBUCKET_PASSWORD", "")
	t.Setenv("ADMIN_USER", "")
	t.Setenv("ADMIN_PASSWORD", "")

	host := "http://localhost:7990"
	loginOutput, err := executeLiveCLI(t, "auth", "login", host, "--username", "admin", "--password", "admin", "--set-default")
	if err != nil {
		t.Fatalf("auth login failed: %v\noutput: %s", err, loginOutput)
	}
	if !strings.Contains(loginOutput, "Stored credentials") {
		t.Fatalf("expected auth login output, got: %s", loginOutput)
	}

	statusOutput, err := executeLiveCLI(t, "--json", "auth", "status", "--host", host)
	if err != nil {
		t.Fatalf("auth status failed: %v\noutput: %s", err, statusOutput)
	}
	statusPayload := decodeJSONMap(t, statusOutput)
	if asString(statusPayload["auth_source"]) != "stored" {
		t.Fatalf("expected auth_source=stored, got: %s", statusOutput)
	}

	logoutOutput, err := executeLiveCLI(t, "auth", "logout", "--host", host)
	if err != nil {
		t.Fatalf("auth logout failed: %v\noutput: %s", err, logoutOutput)
	}
	if !strings.Contains(logoutOutput, "Stored credentials removed") {
		t.Fatalf("expected auth logout output, got: %s", logoutOutput)
	}
}

func TestLiveCLIPRListAndIssueCommandUnavailable(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	branch := fmt.Sprintf("lt-pr-list-%d", time.Now().UnixNano()%100000)
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "pr-list-feature.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	if _, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master"); err != nil {
		t.Fatalf("create pull request failed: %v", err)
	}

	prOutput, prErr := executeLiveCLI(
		t,
		"--json", "pr", "list",
		"--repo", seeded.Key+"/"+repo.Slug,
		"--state", "all",
		"--source-branch", branch,
		"--target-branch", "master",
		"--limit", "25",
	)
	if prErr != nil {
		t.Fatalf("pr list failed: %v\noutput: %s", prErr, prOutput)
	}

	prPayload := decodeJSONMap(t, prOutput)
	pullRequests, ok := prPayload["pull_requests"].([]any)
	if !ok || len(pullRequests) == 0 {
		t.Fatalf("expected non-empty pull_requests array, got: %s", prOutput)
	}

	_, validationErr := executeLiveCLI(t, "pr", "list", "--state", "invalid")
	if validationErr == nil {
		t.Fatalf("expected validation error for invalid --state")
	}

	issueOutput, issueErr := executeLiveCLI(t, "issue", "list")
	if issueErr == nil {
		t.Fatalf("expected issue command to be unavailable")
	}
	if !strings.Contains(issueOutput, "unknown command") && !strings.Contains(issueErr.Error(), "unknown command") {
		t.Fatalf("expected unknown command message for issue command, output=%s err=%v", issueOutput, issueErr)
	}
}

func TestLiveCLIAdminHealthOutputs(t *testing.T) {
	harness := newLiveHarness(t)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", harness.config.BitbucketURL)
	t.Setenv("BITBUCKET_USERNAME", harness.config.BitbucketUsername)
	t.Setenv("BITBUCKET_PASSWORD", harness.config.BitbucketPassword)
	t.Setenv("BITBUCKET_TOKEN", harness.config.BitbucketToken)

	humanOutput, humanErr := executeLiveCLI(t, "admin", "health")
	if humanErr != nil {
		t.Fatalf("admin health (human) failed: %v\noutput: %s", humanErr, humanOutput)
	}
	if !strings.Contains(humanOutput, "Bitbucket health: OK") {
		t.Fatalf("expected health line in human output, got: %s", humanOutput)
	}

	jsonOutput, jsonErr := executeLiveCLI(t, "--json", "admin", "health")
	if jsonErr != nil {
		t.Fatalf("admin health (json) failed: %v\noutput: %s", jsonErr, jsonOutput)
	}
	jsonPayload := decodeJSONMap(t, jsonOutput)
	if healthy, ok := jsonPayload["healthy"].(bool); !ok || !healthy {
		t.Fatalf("expected healthy=true in json output, got: %s", jsonOutput)
	}
}

func TestLiveCLITagCreateDryRunNoSideEffect(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	listBeforeOutput, err := executeLiveCLI(t, "--json", "tag", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("tag list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	tagName := fmt.Sprintf("v-live-dryrun-%d", time.Now().UnixNano()%100000)
	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "tag", "create", tagName, "--start-point", repo.CommitIDs[0])
	if err != nil {
		t.Fatalf("tag create dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"planning_mode": "stateful"`) {
		t.Fatalf("expected stateful planning mode, got: %s", dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "tag.create"`) {
		t.Fatalf("expected tag.create intent, got: %s", dryRunOutput)
	}

	listAfterOutput, err := executeLiveCLI(t, "--json", "tag", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("tag list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no tag side-effect from dry-run create\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}
}

func TestLiveCLITagDeleteDryRunNoSideEffect(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	tagName := fmt.Sprintf("v-live-dryrun-del-%d", time.Now().UnixNano()%100000)
	createOutput, err := executeLiveCLI(t, "--json", "tag", "create", tagName, "--start-point", repo.CommitIDs[0])
	if err != nil {
		t.Fatalf("tag create fixture failed: %v\noutput: %s", err, createOutput)
	}

	listBeforeOutput, err := executeLiveCLI(t, "--json", "tag", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("tag list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "tag", "delete", tagName)
	if err != nil {
		t.Fatalf("tag delete dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"planning_mode": "stateful"`) {
		t.Fatalf("expected stateful planning mode, got: %s", dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "tag.delete"`) {
		t.Fatalf("expected tag.delete intent, got: %s", dryRunOutput)
	}

	listAfterOutput, err := executeLiveCLI(t, "--json", "tag", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("tag list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no tag side-effect from dry-run delete\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}

	_, _ = executeLiveCLI(t, "--json", "tag", "delete", tagName)
}

func createRequiredBuildCheckWithRetry(t *testing.T, body string) (string, bool) {
	t.Helper()

	for attempt := 0; attempt < 3; attempt++ {
		createOutput, createErr := executeLiveCLI(t, "--json", "build", "required", "create", "--body", body)
		if createErr != nil {
			lower := strings.ToLower(createErr.Error() + " " + createOutput)
			if strings.Contains(lower, "returned 500") {
				t.Logf("required-build create attempt %d returned 500; retrying", attempt+1)
				time.Sleep(500 * time.Millisecond)
				continue
			}
			t.Fatalf("build required create failed: %v\noutput: %s", createErr, createOutput)
		}

		createPayload := decodeJSONMap(t, createOutput)
		if requiredID, ok := numericOrStringID(createPayload["id"]); ok {
			return requiredID, true
		}
	}

	t.Log("required-build create remained unavailable after retries; skipping required lifecycle assertions")
	return "", false
}

func configureLiveCLIEnv(t *testing.T, harness *liveHarness, projectKey, repositorySlug string) {
	t.Helper()

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", harness.config.BitbucketURL)
	t.Setenv("BITBUCKET_PROJECT_KEY", projectKey)
	t.Setenv("BITBUCKET_REPO_SLUG", repositorySlug)
	t.Setenv("BITBUCKET_USERNAME", harness.config.BitbucketUsername)
	t.Setenv("BITBUCKET_PASSWORD", harness.config.BitbucketPassword)
	t.Setenv("BITBUCKET_TOKEN", harness.config.BitbucketToken)
}

func executeLiveCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()

	command := cli.NewRootCommand()
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs(args)

	err := command.Execute()
	return output.String(), err
}

func decodeJSONMap(t *testing.T, value string) map[string]any {
	t.Helper()

	var envelope struct {
		Version string         `json:"version"`
		Data    map[string]any `json:"data"`
	}

	if err := json.Unmarshal([]byte(value), &envelope); err != nil {
		t.Fatalf("expected json object output, got parse error %v for: %s", err, value)
	}

	if strings.TrimSpace(envelope.Version) == "" {
		t.Fatalf("expected json envelope version in output: %s", value)
	}

	if envelope.Data == nil {
		t.Fatalf("expected json envelope data object in output: %s", value)
	}

	return envelope.Data
}

func decodeJSONData(t *testing.T, value string, target any) {
	t.Helper()

	var envelope struct {
		Version string `json:"version"`
		Data    any    `json:"data"`
	}

	if err := json.Unmarshal([]byte(value), &envelope); err != nil {
		t.Fatalf("expected json envelope output, got parse error %v for: %s", err, value)
	}

	if strings.TrimSpace(envelope.Version) == "" {
		t.Fatalf("expected json envelope version in output: %s", value)
	}

	if envelope.Data == nil {
		t.Fatalf("expected data field in envelope output: %s", value)
	}

	encodedData, err := json.Marshal(envelope.Data)
	if err != nil {
		t.Fatalf("failed to re-encode envelope data: %v", err)
	}

	if err := json.Unmarshal(encodedData, target); err != nil {
		t.Fatalf("failed to decode envelope data payload: %v for output: %s", err, value)
	}
}

func jsonArrayContainsKey(t *testing.T, output string, key string) bool {
	t.Helper()

	items := make([]map[string]any, 0)
	decodeJSONData(t, output, &items)

	for _, item := range items {
		if asString(item["key"]) == key {
			return true
		}
	}

	return false
}

func jsonArrayContainsExternalID(t *testing.T, output string, externalID string) bool {
	t.Helper()

	items := make([]map[string]any, 0)
	decodeJSONData(t, output, &items)

	for _, item := range items {
		if asString(item["externalId"]) == externalID {
			return true
		}
	}

	return false
}

func asString(value any) string {
	if typed, ok := value.(string); ok {
		return typed
	}
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%v", value)
}
