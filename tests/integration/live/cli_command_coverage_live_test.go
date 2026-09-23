//go:build live

package live_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

func TestLiveCLIDiffOutputModes(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{Commits: 2, WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// A range that every misreading answers differently: from is the older of
	// two master commits and to the middle of three commits stacked on a branch,
	// so neither is a branch tip, and dropping, swapping or defaulting either one
	// changes what lies between them. Each branch commit adds a file of its own,
	// so the name-only form sees the range as well as the patch does.
	branch := testsupport.UniqueName("lt-diff-range-")
	if err := harness.pushCommitsOnBranch(seeded.Key, repo.Slug, branch, 3); err != nil {
		t.Fatalf("push commits on branch failed: %v", err)
	}
	onBranch := commandCoverageCommitsOn(t, ctx, harness, seeded.Key, repo.Slug, branch, len(repo.CommitIDs)+3)

	from := repo.CommitIDs[len(repo.CommitIDs)-1]
	to := onBranch[1]

	// What lies between them: the second master commit and the first two on the
	// branch.
	wantNames := []string{"seed.txt", branch + "-0.txt", branch + "-1.txt"}
	wantAdded := []string{"commit-2", "file 0", "file 1"}

	nameOnlyOutput, err := executeLiveCLI(t, "diff", "refs", from, to, "--name-only")
	if err != nil {
		t.Fatalf("diff refs --name-only failed: %v\noutput: %s", err, nameOnlyOutput)
	}
	if !strings.Contains(nameOnlyOutput, "seed.txt") {
		t.Fatalf("expected changed file in --name-only output, got: %s", nameOnlyOutput)
	}
	if names := strings.Fields(nameOnlyOutput); !slices.Equal(names, wantNames) {
		t.Errorf("--name-only named %v, want %v", names, wantNames)
	}

	statOutput, err := executeLiveCLI(t, "--json", "diff", "refs", from, to, "--stat")
	if err != nil {
		t.Fatalf("diff refs --stat failed: %v\noutput: %s", err, statOutput)
	}
	statPayload := decodeJSONMap(t, statOutput)
	if statPayload["output"] != "stat" {
		t.Fatalf("expected the payload to report which form it produced, got: %s", statOutput)
	}
	// The same change the patch shows: three files, one line added to each.
	stats, _ := statPayload["stats"].(map[string]any)
	for field, want := range map[string]float64{"filesChanged": 3, "totalInsertions": 3, "totalDeletions": 0} {
		if got, ok := stats[field].(float64); !ok || got != want {
			t.Errorf("stats.%s = %v, want %v:\n%s", field, stats[field], want, statOutput)
		}
	}

	patchOutput, err := executeLiveCLI(t, "diff", "refs", from, to, "--patch")
	if err != nil {
		t.Fatalf("diff refs --patch failed: %v\noutput: %s", err, patchOutput)
	}
	if !strings.Contains(patchOutput, "diff --git") {
		t.Fatalf("expected patch output, got: %s", patchOutput)
	}
	if added := commandCoverageAddedLines(patchOutput); !slices.Equal(added, wantAdded) {
		t.Errorf("--patch added %q, want %q:\n%s", added, wantAdded, patchOutput)
	}

	_, err = executeLiveCLI(t, "diff", "refs", from, to, "--patch", "--stat")
	if err == nil {
		t.Fatalf("expected validation error for conflicting diff output modes")
	}
}

func TestLiveCLIDiffPRAndCommitHumanOutput(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	branch := testsupport.UniqueName("lt-diff-cli-")
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "diff-feature.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	pullRequestID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
	if err != nil {
		t.Fatalf("create pull request failed: %v", err)
	}

	// --repo is spelled out rather than left to the environment: a pull request
	// is found in a repository, and passing one to a command that takes its id
	// from elsewhere is a combination that has been rejected before.
	prDiffOutput, err := executeLiveCLI(t, "diff", "pr", pullRequestID, "--patch", "--repo", seeded.Key+"/"+repo.Slug)
	if err != nil {
		t.Fatalf("diff pr failed: %v\noutput: %s", err, prDiffOutput)
	}
	if !strings.Contains(prDiffOutput, "diff --git") {
		t.Fatalf("expected patch output for diff pr, got: %s", prDiffOutput)
	}
	// Exactly the change the pull request carries, which is what shows the id
	// chose it.
	if files := commandCoverageDiffFiles(prDiffOutput); !slices.Equal(files, []string{"diff-feature.txt"}) {
		t.Errorf("diff pr --patch changes %v, want [diff-feature.txt]:\n%s", files, prDiffOutput)
	}
	if added := commandCoverageAddedLines(prDiffOutput); !slices.Equal(added, []string{"branch=" + branch}) {
		t.Errorf("diff pr --patch added %q, want [branch=%s]:\n%s", added, branch, prDiffOutput)
	}

	// The name-only form reads another endpoint, the raw diff.
	prNames := decodeJSONMap(t, mustLiveCLI(t, "diff", "pr", pullRequestID, "--name-only", "--repo", seeded.Key+"/"+repo.Slug))
	if names, _ := prNames["names"].([]any); len(names) != 1 || names[0] != "diff-feature.txt" {
		t.Errorf("diff pr --name-only named %v, want [diff-feature.txt]", prNames["names"])
	}

	// `bb pr diff` is the gh-shaped spelling of the same command. Asserting the
	// two produce identical output is what makes it an alias rather than a
	// second implementation that can drift.
	prDiffAliasOutput, err := executeLiveCLI(t, "pr", "diff", pullRequestID, "--patch", "--repo", seeded.Key+"/"+repo.Slug)
	if err != nil {
		t.Fatalf("pr diff failed: %v\noutput: %s", err, prDiffAliasOutput)
	}
	if prDiffAliasOutput != prDiffOutput {
		t.Fatalf("pr diff and diff pr disagree:\n pr diff: %s\n diff pr: %s", prDiffAliasOutput, prDiffOutput)
	}

	// A commit that changes seed.txt and one other file, off the default branch:
	// the path has something to leave out, and the commit is not what a request
	// resolved against the default branch would find.
	changedLine := "changed by " + branch
	twoFileCommit := commandCoverageTwoFileCommit(t, harness, seeded.Key, repo.Slug, branch+"-two-files", changedLine)

	// Without the path the commit shows both files, or leaving one out would
	// prove nothing.
	wholeCommit := asString(decodeJSONMap(t, mustLiveCLI(t, "diff", "commit", twoFileCommit))["patch"])
	if files := commandCoverageDiffFiles(wholeCommit); !slices.Equal(slices.Sorted(slices.Values(files)), []string{"second-file.txt", "seed.txt"}) {
		t.Fatalf("diff commit %s changes %v, want second-file.txt and seed.txt:\n%s", twoFileCommit, files, wholeCommit)
	}

	commitDiffOutput, err := executeLiveCLI(t, "diff", "commit", twoFileCommit, "--path", "seed.txt")
	if err != nil {
		t.Fatalf("diff commit failed: %v\noutput: %s", err, commitDiffOutput)
	}
	if !strings.Contains(commitDiffOutput, "diff --git") && !strings.Contains(commitDiffOutput, "\"diffs\"") {
		t.Fatalf("expected diff payload for diff commit, got: %s", commitDiffOutput)
	}
	if files := commandCoverageDiffFiles(commitDiffOutput); !slices.Equal(files, []string{"seed.txt"}) {
		t.Errorf("diff commit --path seed.txt changes %v, want [seed.txt]:\n%s", files, commitDiffOutput)
	}
	if added := commandCoverageAddedLines(commitDiffOutput); !slices.Equal(added, []string{changedLine}) {
		t.Errorf("diff commit --path seed.txt added %q, want [%s]:\n%s", added, changedLine, commitDiffOutput)
	}
}

func TestLiveCLIInsightsLifecycle(t *testing.T) {
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
	reportKey := testsupport.UniqueName("live-cli-report-")
	externalID := testsupport.UniqueName("live-cli-ann-")

	reportBody := `{"title":"Live CLI Insights","result":"PASS","details":"cli lifecycle"}`
	setOutput, err := executeLiveCLI(t, "--json", "insights", "report", "set", commitID, reportKey, "--body", reportBody)
	if err != nil {
		t.Fatalf("insights report set failed: %v\noutput: %s", err, setOutput)
	}
	setPayload := decodeJSONMap(t, setOutput)
	if asString(setPayload["key"]) != reportKey {
		t.Fatalf("expected report key=%s, got output: %s", reportKey, setOutput)
	}

	commandCoverageAssertFields(t, "the stored report", decodeJSONMap(t, mustLiveCLI(t, "insights", "report", "get", commitID, reportKey)),
		map[string]any{"title": "Live CLI Insights", "result": "PASS", "details": "cli lifecycle"})

	listOutput, err := executeLiveCLI(t, "--json", "insights", "report", "list", commitID)
	if err != nil {
		t.Fatalf("insights report list failed: %v\noutput: %s", err, listOutput)
	}
	if !jsonArrayContainsKey(t, listOutput, reportKey) {
		t.Fatalf("expected report key %s in list output: %s", reportKey, listOutput)
	}

	// A second annotation stays behind when the first is deleted by its
	// external id. A delete that lost the id removes every annotation on the
	// report, which with one annotation looks the same as removing the one named.
	keptID := testsupport.UniqueName("live-cli-ann-kept-")
	annotationBody := fmt.Sprintf(`[{"externalId":"%s","message":"integration annotation","severity":"LOW","path":"seed.txt","line":1},`+
		`{"externalId":"%s","message":"kept annotation","severity":"HIGH","path":"seed.txt","line":1}]`, externalID, keptID)
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
	stored := commandCoverageAnnotations(t, listAnnotationOutput)
	commandCoverageAssertFields(t, "annotation "+externalID, stored[externalID],
		map[string]any{"reportKey": reportKey, "message": "integration annotation", "severity": "LOW", "path": "seed.txt", "line": float64(1)})
	commandCoverageAssertFields(t, "annotation "+keptID, stored[keptID],
		map[string]any{"reportKey": reportKey, "message": "kept annotation", "severity": "HIGH", "path": "seed.txt", "line": float64(1)})

	deleteAnnotationOutput, err := executeLiveCLI(t, "--json", "insights", "annotation", "delete", commitID, reportKey, "--external-id", externalID, "--yes")
	if err != nil {
		t.Fatalf("insights annotation delete failed: %v\noutput: %s", err, deleteAnnotationOutput)
	}
	if remaining := commandCoverageAnnotations(t, mustLiveCLI(t, "insights", "annotation", "list", commitID, reportKey)); len(remaining) != 1 || remaining[keptID] == nil {
		t.Errorf("after deleting %s the report holds %v, want only %s", externalID, remaining, keptID)
	}

	deleteReportOutput, err := executeLiveCLI(t, "--json", "insights", "report", "delete", commitID, reportKey, "--yes")
	if err != nil {
		t.Fatalf("insights report delete failed: %v\noutput: %s", err, deleteReportOutput)
	}
	deletePayload := decodeJSONMap(t, deleteReportOutput)
	if asString(deletePayload["status"]) != "ok" {
		t.Fatalf("expected delete status ok, got: %s", deleteReportOutput)
	}
	if output, err := executeLiveCLI(t, "--json", "insights", "report", "get", commitID, reportKey); !apperrors.IsKind(err, apperrors.KindNotFound) {
		t.Errorf("the deleted report is still readable, err=%v:\n%s", err, output)
	}
}

func TestLiveCLIBuildAndTagLifecycle(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{Commits: 2, WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	commitID := repo.CommitIDs[0]
	buildKey := testsupport.UniqueName("live-cli-build-")

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
	storedBuild := map[string]any{
		"key": buildKey, "state": "SUCCESSFUL", "url": "https://example.invalid/live-cli-build",
		"name": "Live CLI Build", "description": "build status for coverage",
	}
	commandCoverageAssertFields(t, "the build status", commandCoverageEntry(t, getBuildOutput, "key", buildKey), storedBuild)

	// build status set writes to the commit's build status resource, which keeps
	// five of the nine fields sent: it answers 204 to ref, parent, buildNumber and
	// duration and stores none of them, however they are spelled (10.4.3, raw
	// HTTP). The repository builds resource `bb build set` writes to keeps all
	// four, and reading the status by key through it carries every field, so it
	// shows which were kept.
	scopedBuild := decodeJSONMap(t, mustLiveCLI(t, "build", "get", commitID, "--key", buildKey))
	commandCoverageAssertFields(t, "the build status read by key", scopedBuild, storedBuild)
	for _, dropped := range []string{"ref", "parent", "buildNumber", "duration"} {
		if value, kept := scopedBuild[dropped]; kept {
			t.Errorf("%s came back as %v: the commit build status resource keeps it now, so assert the value sent", dropped, value)
		}
	}

	statsBuildOutput, err := executeLiveCLI(t, "--json", "build", "status", "stats", commitID)
	if err != nil {
		t.Fatalf("build status stats failed: %v\noutput: %s", err, statsBuildOutput)
	}
	statsPayload := firstOfJSONArray(t, statsBuildOutput)
	if _, ok := statsPayload["successful"]; !ok {
		t.Fatalf("expected successful field in build stats output, got: %s", statsBuildOutput)
	}
	if statsPayload["commit"] != commitID {
		t.Fatalf("expected the row to name the commit it counts, got: %s", statsBuildOutput)
	}
	// The seeded commit is this repository's alone, so the one status set above
	// is all there is to count.
	commandCoverageAssertFields(t, "the build stats", statsPayload,
		map[string]any{"successful": float64(1), "failed": float64(0), "inProgress": float64(0), "unknown": float64(0), "cancelled": float64(0)})

	// Two tags besides the one under test give the listing's flags something to
	// decide, and tag dates have one-second resolution, hence the pauses. This
	// one matches the filter, is older and sorts first, so it heads a filtered
	// listing in its default order, which is alphabetical. The other is created
	// last and is outside the filter, so it heads an unfiltered one.
	tagsPath := fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/tags", seeded.Key, repo.Slug)
	olderTag := testsupport.UniqueName("v-live-cli-0-")
	if _, err := harness.liveJSON(ctx, http.MethodPost, tagsPath, map[string]any{"name": olderTag, "startPoint": commitID}); err != nil {
		t.Fatalf("seed an older tag inside the filter: %v", err)
	}
	time.Sleep(1100 * time.Millisecond)

	// The older of the two commits, which a start point defaulted to the branch
	// tip would miss.
	tagCommit := repo.CommitIDs[1]
	tagName := testsupport.UniqueName("v-live-cli-")
	createTagOutput, err := executeLiveCLI(t, "--json", "tag", "create", tagName, "--start-point", tagCommit, "--message", "live cli tag")
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
	if viewTagPayload["latestCommit"] != tagCommit {
		t.Errorf("tag %s points at %v, want %s", tagName, viewTagPayload["latestCommit"], tagCommit)
	}
	// No REST response carries a tag's message; the tag object itself does.
	if message := commandCoverageTagMessage(t, harness, seeded.Key, repo.Slug, tagName); message != "live cli tag" {
		t.Errorf("tag %s carries the message %q, want %q", tagName, message, "live cli tag")
	}

	time.Sleep(1100 * time.Millisecond)
	outsideTag := testsupport.UniqueName("a-live-cli-outside-")
	if _, err := harness.liveJSON(ctx, http.MethodPost, tagsPath, map[string]any{"name": outsideTag, "startPoint": commitID}); err != nil {
		t.Fatalf("seed a newer tag outside the filter: %v", err)
	}
	// All three are there, so the flags below have something to leave out.
	if listed := commandCoverageFieldValues(t, mustLiveCLI(t, "tag", "list"), "displayId"); !slices.Equal(listed, slices.Sorted(slices.Values([]string{olderTag, tagName, outsideTag}))) {
		t.Fatalf("the repository holds the tags %v, want %s, %s and %s", listed, olderTag, tagName, outsideTag)
	}

	// By modification rather than alphabetically: a filtered listing is already
	// alphabetical when no order is given, so that order would prove nothing.
	listTagOutput, err := executeLiveCLI(t, "tag", "list", "--limit", "1", "--order-by", "MODIFICATION", "--filter", "v-live-cli")
	if err != nil {
		t.Fatalf("tag list (human) failed: %v\noutput: %s", err, listTagOutput)
	}
	if !strings.Contains(listTagOutput, tagName) {
		t.Fatalf("expected tag name in human tag list output, got: %s", listTagOutput)
	}
	if listed := commandCoverageFirstColumn(listTagOutput); !slices.Equal(listed, []string{tagName}) {
		t.Errorf("tag list --limit 1 --order-by MODIFICATION --filter v-live-cli listed %v, want [%s]:\n%s", listed, tagName, listTagOutput)
	}

	deleteTagOutput, err := executeLiveCLI(t, "--json", "tag", "delete", tagName, "--yes")
	if err != nil {
		t.Fatalf("tag delete failed: %v\noutput: %s", err, deleteTagOutput)
	}
	if asString(decodeJSONMap(t, deleteTagOutput)["status"]) != "ok" {
		t.Fatalf("expected tag delete status ok, got: %s", deleteTagOutput)
	}
	if output, err := executeLiveCLI(t, "--json", "tag", "view", tagName); !apperrors.IsKind(err, apperrors.KindNotFound) {
		t.Errorf("the deleted tag %s is still readable, err=%v:\n%s", tagName, err, output)
	}
}

func TestLiveCLIBuildRequiredAndInsightsHumanOutput(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{Commits: 2, WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	commitID := repo.CommitIDs[0]
	buildKey := testsupport.UniqueName("live-cli-build-human-")

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
	commandCoverageAssertFields(t, "the build status", commandCoverageEntry(t, mustLiveCLI(t, "build", "status", "get", commitID), "key", buildKey),
		map[string]any{"state": "SUCCESSFUL", "url": "https://example.invalid/live-cli-build-human"})

	statsBuildOutput, err := executeLiveCLI(t, "build", "status", "stats", commitID)
	if err != nil {
		t.Fatalf("build status stats (human) failed: %v\noutput: %s", err, statsBuildOutput)
	}
	if !strings.Contains(statsBuildOutput, "Successful:") {
		t.Fatalf("expected human stats output, got: %s", statsBuildOutput)
	}
	commandCoverageAssertFields(t, "the build stats", firstOfJSONArray(t, mustLiveCLI(t, "build", "status", "stats", commitID)),
		map[string]any{"successful": float64(1), "failed": float64(0), "inProgress": float64(0), "unknown": float64(0), "cancelled": float64(0)})

	requiredBody := `{"buildParentKeys":["ci"],"refMatcher":{"id":"refs/heads/master","type":{"id":"BRANCH"}}}`
	requiredID := createRequiredBuildCheckWithRetry(t, requiredBody)
	commandCoverageAssertRequiredCheck(t, mustLiveCLI(t, "build", "required", "list"), requiredID, "ci", "refs/heads/master", "BRANCH")

	requiredListOutput, err := executeLiveCLI(t, "build", "required", "list")
	if err != nil {
		t.Fatalf("build required list (human) failed: %v\noutput: %s", err, requiredListOutput)
	}
	if !strings.Contains(requiredListOutput, "id=") || !strings.Contains(requiredListOutput, "buildParentKeys=") {
		t.Fatalf("expected human required list output, got: %s", requiredListOutput)
	}

	// Other values than the create's, or an update that changed nothing would
	// read back the same.
	updatedKey := testsupport.UniqueName("ci-updated-")
	updatedBody := fmt.Sprintf(`{"buildParentKeys":[%q],"refMatcher":{"id":"refs/heads/release/*","type":{"id":"PATTERN"}}}`, updatedKey)
	updateRequiredOutput, err := executeLiveCLI(t, "--json", "build", "required", "update", requiredID, "--body", updatedBody)
	if err != nil {
		t.Fatalf("build required update failed: %v\noutput: %s", err, updateRequiredOutput)
	}
	afterUpdate := mustLiveCLI(t, "build", "required", "list")
	var checks []any
	if err := decodeJSONEnvelopeData(afterUpdate, &checks); err != nil || len(checks) != 1 {
		t.Errorf("want the one check after an update, got %d (%v):\n%s", len(checks), err, afterUpdate)
	}
	commandCoverageAssertRequiredCheck(t, afterUpdate, requiredID, updatedKey, "refs/heads/release/*", "PATTERN")

	deleteRequiredOutput, err := executeLiveCLI(t, "build", "required", "delete", requiredID, "--yes")
	if err != nil {
		t.Fatalf("build required delete (human) failed: %v\noutput: %s", err, deleteRequiredOutput)
	}
	if !strings.Contains(deleteRequiredOutput, "Deleted required build merge check") {
		t.Fatalf("expected human required delete output, got: %s", deleteRequiredOutput)
	}
	var remainingChecks any
	if err := decodeJSONEnvelopeData(mustLiveCLI(t, "build", "required", "list"), &remainingChecks); err != nil {
		t.Fatalf("build required list returned invalid JSON: %v", err)
	}
	if check, found := findByID(remainingChecks, requiredID); found {
		t.Errorf("the deleted check %s is still listed: %v", requiredID, check)
	}

	reportKey := testsupport.UniqueName("live-cli-insights-human-")
	externalID := testsupport.UniqueName("live-cli-insights-ann-")
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
	commandCoverageAssertFields(t, "the stored report", decodeJSONMap(t, getReportOutput),
		map[string]any{"title": "Live CLI Insights Human", "result": "PASS", "details": "human output coverage"})

	// A second report, so a limit of one has something to cut. It follows
	// reportKey by key, by title and by creation, whichever the listing sorts by.
	laterReport := fmt.Sprintf("/rest/insights/latest/projects/%s/repos/%s/commits/%s/reports/%s-later", seeded.Key, repo.Slug, commitID, reportKey)
	if _, err := harness.liveJSON(ctx, http.MethodPut, laterReport, map[string]any{"title": "zz later report", "result": "FAIL"}); err != nil {
		t.Fatalf("seed a second report: %v", err)
	}
	if keys := commandCoverageFieldValues(t, mustLiveCLI(t, "insights", "report", "list", commitID), "key"); !slices.Equal(keys, []string{reportKey, reportKey + "-later"}) {
		t.Fatalf("the commit holds the reports %v, want %s and %s-later", keys, reportKey, reportKey)
	}

	listReportOutput, err := executeLiveCLI(t, "insights", "report", "list", commitID, "--limit", "1")
	if err != nil {
		t.Fatalf("insights report list (human) failed: %v\noutput: %s", err, listReportOutput)
	}
	if !strings.Contains(listReportOutput, reportKey) || !strings.Contains(listReportOutput, "PASS") {
		t.Fatalf("expected report key/result in human list output, got: %s", listReportOutput)
	}
	if listed := commandCoverageFirstColumn(listReportOutput); !slices.Equal(listed, []string{reportKey}) {
		t.Errorf("insights report list --limit 1 listed %v, want [%s]:\n%s", listed, reportKey, listReportOutput)
	}

	keptID := testsupport.UniqueName("live-cli-insights-kept-")
	annotationBody := fmt.Sprintf(`[{"externalId":"%s","message":"human annotation","severity":"LOW","path":"seed.txt","line":1},`+
		`{"externalId":"%s","message":"kept annotation","severity":"MEDIUM","path":"seed.txt","line":1}]`, externalID, keptID)
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
	stored := commandCoverageAnnotations(t, mustLiveCLI(t, "insights", "annotation", "list", commitID, reportKey))
	commandCoverageAssertFields(t, "annotation "+externalID, stored[externalID],
		map[string]any{"message": "human annotation", "severity": "LOW", "path": "seed.txt", "line": float64(1)})
	commandCoverageAssertFields(t, "annotation "+keptID, stored[keptID],
		map[string]any{"message": "kept annotation", "severity": "MEDIUM", "path": "seed.txt", "line": float64(1)})

	deleteAnnotationOutput, err := executeLiveCLI(t, "insights", "annotation", "delete", commitID, reportKey, "--external-id", externalID, "--yes")
	if err != nil {
		t.Fatalf("insights annotation delete (human) failed: %v\noutput: %s", err, deleteAnnotationOutput)
	}
	if !strings.Contains(deleteAnnotationOutput, "Deleted annotations") {
		t.Fatalf("expected human annotation delete output, got: %s", deleteAnnotationOutput)
	}
	if remaining := commandCoverageAnnotations(t, mustLiveCLI(t, "insights", "annotation", "list", commitID, reportKey)); len(remaining) != 1 || remaining[keptID] == nil {
		t.Errorf("after deleting %s the report holds %v, want only %s", externalID, remaining, keptID)
	}

	deleteReportOutput, err := executeLiveCLI(t, "insights", "report", "delete", commitID, reportKey, "--yes")
	if err != nil {
		t.Fatalf("insights report delete (human) failed: %v\noutput: %s", err, deleteReportOutput)
	}
	if !strings.Contains(deleteReportOutput, "Deleted report") {
		t.Fatalf("expected human report delete output, got: %s", deleteReportOutput)
	}
	if output, err := executeLiveCLI(t, "--json", "insights", "report", "get", commitID, reportKey); !apperrors.IsKind(err, apperrors.KindNotFound) {
		t.Errorf("the deleted report is still readable, err=%v:\n%s", err, output)
	}
}

func TestLiveCLIAuthStoredConfigFlow(t *testing.T) {
	// Read before the environment is cleared below.
	host := liveInstanceURL()

	configPath := filepath.Join(t.TempDir(), "bb-config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "0")
	t.Setenv("BITBUCKET_URL", "")
	t.Setenv("BITBUCKET_TOKEN", "")
	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("BITBUCKET_PASSWORD", "")
	t.Setenv("ADMIN_USER", "")
	t.Setenv("ADMIN_PASSWORD", "")

	loginOutput, err := executeLiveCLIWithStdin(t, "admin", "auth", "login", host, "--username", "admin", "--password-stdin", "--set-default")
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
	if asString(statusPayload["authSource"]) != "stored" {
		t.Fatalf("expected auth_source=stored, got: %s", statusOutput)
	}
	// Where the credential came from says nothing about whether Bitbucket takes
	// it, and the JSON status exits zero either way: the verdict is in the
	// payload.
	commandCoverageAssertFields(t, "auth status", statusPayload, map[string]any{"ok": true, "authMode": "basic"})
	if check := commandCoverageAuthCheck(statusPayload); check == nil || check["ok"] != true {
		t.Errorf("the stored credential did not authenticate: %v", check)
	}
	identity := decodeJSONMap(t, mustLiveCLI(t, "auth", "identity", "--host", host))
	if user, _ := identity["user"].(map[string]any); user["name"] != "admin" {
		t.Errorf("the stored credential authenticates as %v, want admin", identity["user"])
	}

	logoutOutput, err := executeLiveCLI(t, "auth", "logout", "--host", host)
	if err != nil {
		t.Fatalf("auth logout failed: %v\noutput: %s", err, logoutOutput)
	}
	if !strings.Contains(logoutOutput, "Stored credentials removed") {
		t.Fatalf("expected auth logout output, got: %s", logoutOutput)
	}
	// Logout sends nothing to Bitbucket; what it changes is which credential the
	// next request carries.
	afterLogout := decodeJSONMap(t, mustLiveCLI(t, "auth", "status", "--host", host))
	if afterLogout["authSource"] == "stored" || afterLogout["authMode"] != "none" {
		t.Errorf("after logout bb still holds a credential: authSource=%v authMode=%v", afterLogout["authSource"], afterLogout["authMode"])
	}
}

func TestLiveCLIPRListAndIssueCommandUnavailable(t *testing.T) {
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

	branch := testsupport.UniqueName("lt-pr-list-")
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "pr-list-feature.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	// Two pull requests every flag must keep -- one declined, so --state all has
	// something the default leaves out -- and one for each branch filter to
	// exclude: from another branch, and into another target.
	declinedID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
	if err != nil {
		t.Fatalf("create pull request failed: %v", err)
	}
	mustLiveCLI(t, "pr", "decline", declinedID)

	openID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
	if err != nil {
		t.Fatalf("create pull request failed: %v", err)
	}

	otherSource := testsupport.UniqueName("lt-pr-list-other-")
	otherTarget := testsupport.UniqueName("lt-pr-list-target-")
	for branchName, fileName := range map[string]string{otherSource: "pr-list-other.txt", otherTarget: "pr-list-target.txt"} {
		if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branchName, fileName); err != nil {
			t.Fatalf("push commit on branch %s failed: %v", branchName, err)
		}
	}
	otherSourceID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, otherSource, "master")
	if err != nil {
		t.Fatalf("create pull request from %s failed: %v", otherSource, err)
	}
	otherTargetID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, otherTarget)
	if err != nil {
		t.Fatalf("create pull request into %s failed: %v", otherTarget, err)
	}

	// All four are there as seeded, so each flag below has something to decide.
	everything := commandCoveragePullRequests(t, mustLiveCLI(t, "pr", "list", "--repo", seeded.Key+"/"+repo.Slug, "--state", "all"))
	for id, want := range map[string]map[string]any{
		declinedID:    {"state": "DECLINED", "sourceBranch": branch, "targetBranch": "master"},
		openID:        {"state": "OPEN", "sourceBranch": branch, "targetBranch": "master"},
		otherSourceID: {"state": "OPEN", "sourceBranch": otherSource, "targetBranch": "master"},
		otherTargetID: {"state": "OPEN", "sourceBranch": branch, "targetBranch": otherTarget},
	} {
		commandCoverageAssertFields(t, "seeded pull request "+id, everything[id], want)
	}
	if len(everything) != 4 {
		t.Fatalf("the repository holds %d pull requests, want the 4 seeded: %v", len(everything), everything)
	}

	prOutput, prErr := executeLiveCLI(
		t,
		"--json", "pr", "list",
		"--repo", seeded.Key+"/"+repo.Slug,
		"--state", "all",
		"--source-branch", branch,
		"--target-branch", "master",
		"--limit", "1",
	)
	if prErr != nil {
		t.Fatalf("pr list failed: %v\noutput: %s", prErr, prOutput)
	}

	prPayload := decodeJSONMap(t, prOutput)
	pullRequests, ok := prPayload["pullRequests"].([]any)
	if !ok || len(pullRequests) == 0 {
		t.Fatalf("expected non-empty pull_requests array, got: %s", prOutput)
	}
	// Two match, so a limit of one has to cut one.
	limited := commandCoveragePullRequests(t, prOutput)
	if len(limited) != 1 {
		t.Errorf("--limit 1 returned %d pull requests: %v", len(limited), limited)
	}
	for id, pullRequest := range limited {
		if id != declinedID && id != openID {
			t.Errorf("--limit 1 returned pull request %s, which the filters exclude: %v", id, pullRequest)
		}
	}

	// And without the limit, exactly the two.
	filtered := commandCoveragePullRequests(t, mustLiveCLI(t, "pr", "list", "--repo", seeded.Key+"/"+repo.Slug,
		"--state", "all", "--source-branch", branch, "--target-branch", "master"))
	for id, wantState := range map[string]string{declinedID: "DECLINED", openID: "OPEN"} {
		commandCoverageAssertFields(t, "pull request "+id, filtered[id],
			map[string]any{"state": wantState, "sourceBranch": branch, "targetBranch": "master"})
	}
	if len(filtered) != 2 {
		t.Errorf("--state all --source-branch %s --target-branch master returned %d pull requests, want 2: %v", branch, len(filtered), filtered)
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
	// healthy is true for a refused credential too; auth=ok is the credential.
	if !strings.Contains(humanOutput, "status=200, auth=ok") {
		t.Errorf("expected the credential to be accepted in human output, got: %s", humanOutput)
	}

	jsonOutput, jsonErr := executeLiveCLI(t, "--json", "admin", "health")
	if jsonErr != nil {
		t.Fatalf("admin health (json) failed: %v\noutput: %s", jsonErr, jsonOutput)
	}
	jsonPayload := decodeJSONMap(t, jsonOutput)
	if healthy, ok := jsonPayload["healthy"].(bool); !ok || !healthy {
		t.Fatalf("expected healthy=true in json output, got: %s", jsonOutput)
	}
	commandCoverageAssertFields(t, "admin health", jsonPayload, map[string]any{"authenticated": true, "statusCode": float64(200)})
	// And the credential Bitbucket accepted is the configured user's.
	identity := decodeJSONMap(t, mustLiveCLI(t, "auth", "identity"))
	if user, _ := identity["user"].(map[string]any); user["name"] != harness.username() {
		t.Errorf("the configured credential authenticates as %v, want %s", identity["user"], harness.username())
	}

	// The same probe carrying no credential, so the answer above is known to be
	// the credential's doing rather than an instance that lets anyone read.
	for _, variable := range []string{"BITBUCKET_USERNAME", "BITBUCKET_USER", "BITBUCKET_PASSWORD", "BITBUCKET_TOKEN", "ADMIN_USER", "ADMIN_PASSWORD"} {
		t.Setenv(variable, "")
	}
	anonymous := decodeJSONMap(t, mustLiveCLI(t, "admin", "health"))
	commandCoverageAssertFields(t, "admin health without a credential", anonymous, map[string]any{"authenticated": false, "statusCode": float64(401)})
}

func TestLiveCLITagCreateDryRunNoSideEffect(t *testing.T) {
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

	listBeforeOutput, err := executeLiveCLI(t, "--json", "tag", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("tag list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	tagName := testsupport.UniqueName("v-live-dryrun-")
	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "tag", "create", tagName, "--start-point", repo.CommitIDs[0])
	if err != nil {
		t.Fatalf("tag create dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"planningMode": "stateful"`) {
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
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{Commits: 2, WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// The older commit, which a start point defaulted to the branch tip would miss.
	tagCommit := repo.CommitIDs[1]
	tagName := testsupport.UniqueName("v-live-dryrun-del-")
	createOutput, err := executeLiveCLI(t, "--json", "tag", "create", tagName, "--start-point", tagCommit)
	if err != nil {
		t.Fatalf("tag create fixture failed: %v\noutput: %s", err, createOutput)
	}

	listBeforeOutput, err := executeLiveCLI(t, "--json", "tag", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("tag list before failed: %v\noutput: %s", err, listBeforeOutput)
	}
	// Before and after agreeing proves nothing if the tag was never there.
	commandCoverageAssertFields(t, "the tag before the dry run", commandCoverageEntry(t, listBeforeOutput, "displayId", tagName),
		map[string]any{"latestCommit": tagCommit})

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "tag", "delete", tagName, "--yes")
	if err != nil {
		t.Fatalf("tag delete dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"planningMode": "stateful"`) {
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
	commandCoverageAssertFields(t, "the tag after the dry run", decodeJSONMap(t, mustLiveCLI(t, "tag", "view", tagName)),
		map[string]any{"displayId": tagName, "latestCommit": tagCommit})

	// The delete the dry run described, for real, and read back.
	mustLiveCLI(t, "tag", "delete", tagName, "--yes")
	if output, err := executeLiveCLI(t, "--json", "tag", "view", tagName); !apperrors.IsKind(err, apperrors.KindNotFound) {
		t.Errorf("the deleted tag %s is still readable, err=%v:\n%s", tagName, err, output)
	}
}

// createRequiredBuildCheckWithRetry creates the check the required-build
// lifecycle assertions run against.
//
// Bitbucket answers 500 here for a short window after a repository is seeded,
// so the create is retried. What it no longer does is give up quietly: this
// used to return a boolean, and the caller skipped list, update and delete
// whenever the window outlasted three attempts -- three commands untested, in a
// run that stayed green and said so in a log line nobody reads. A dependency
// that cannot be reached fails the test.
func createRequiredBuildCheckWithRetry(t *testing.T, body string) string {
	t.Helper()

	const attempts = 5

	var lastOutput string
	var lastErr error

	for attempt := range attempts {
		createOutput, createErr := executeLiveCLI(t, "--json", "build", "required", "create", "--body", body)
		if createErr != nil {
			lastOutput, lastErr = createOutput, createErr

			lower := strings.ToLower(createErr.Error() + " " + createOutput)
			if strings.Contains(lower, "returned 500") {
				t.Logf("required-build create attempt %d returned 500; retrying", attempt+1)
				time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
				continue
			}
			t.Fatalf("build required create failed: %v\noutput: %s", createErr, createOutput)
		}

		createPayload := decodeJSONMap(t, createOutput)
		if requiredID, ok := numericOrStringID(createPayload["id"]); ok {
			return requiredID
		}

		lastOutput, lastErr = createOutput, fmt.Errorf("the create answered without an id")
	}

	t.Fatalf("build required create did not succeed in %d attempts, so the required-build lifecycle was never exercised: %v\noutput: %s",
		attempts, lastErr, lastOutput)

	return ""
}

// configureLiveCLIEnv points a test's CLI calls at the repository it seeded.
//
// It used to set six environment variables. Four of them -- the URL and the
// three credentials -- are identical for every test in the run and are set once
// in TestMain, where being process-wide is harmless because nothing changes
// them. The two that vary are carried per test instead, and travel to the
// command as --repo.
//
// That is the whole reason no live test could be parallel: t.Setenv refuses to
// run in a parallel test, and even if it did not, a second test setting
// BITBUCKET_PROJECT_KEY would be visible to the first.
func configureLiveCLIEnv(t *testing.T, harness *liveHarness, projectKey, repositorySlug string) {
	t.Helper()

	_ = harness
	setLiveRepoContext(t, projectKey, repositorySlug)
}

func executeLiveCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()

	command := cli.NewRootCommandWithOverrides(liveCLIOverrides(t))
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs(withLiveRepoContext(t, command, args))

	err := command.Execute()
	return output.String(), err
}

// executeLiveCLIUnscoped runs a command with no repository context injected.
//
// For the calls whose subject is being unscoped. `bb search prs --role author`
// reports what Bitbucket answers on the dashboard and refuses a --repo,
// because a role has no meaning inside one repository; `bb auth token create
// --user admin` is scoped by the user it names. Both would fail with an
// injected --repo, and both are asking for exactly what the injection exists
// to supply by default -- so they say so here rather than the helper carrying
// a list of commands it must not help.
func executeLiveCLIUnscoped(t *testing.T, args ...string) (string, error) {
	t.Helper()

	command := cli.NewRootCommandWithOverrides(liveCLIOverrides(t))
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
		Data map[string]any `json:"data"`
		Meta struct {
			BBVersion string `json:"bbVersion"`
		} `json:"meta"`
	}

	if err := json.Unmarshal([]byte(value), &envelope); err != nil {
		t.Fatalf("expected json object output, got parse error %v for: %s", err, value)
	}

	if strings.TrimSpace(envelope.Meta.BBVersion) == "" {
		t.Fatalf("expected a bb envelope carrying meta.bbVersion: %s", value)
	}

	if envelope.Data == nil {
		t.Fatalf("expected json envelope data object in output: %s", value)
	}

	return envelope.Data
}

func decodeJSONData(t *testing.T, value string, target any) {
	t.Helper()

	var envelope struct {
		Data any `json:"data"`
		Meta struct {
			BBVersion string `json:"bbVersion"`
		} `json:"meta"`
	}

	if err := json.Unmarshal([]byte(value), &envelope); err != nil {
		t.Fatalf("expected json envelope output, got parse error %v for: %s", err, value)
	}

	if strings.TrimSpace(envelope.Meta.BBVersion) == "" {
		t.Fatalf("expected a bb envelope carrying meta.bbVersion: %s", value)
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

// decodeJSONEnvelopeData is decodeJSONData for a caller that wants the error
// back rather than a failed test.
func decodeJSONEnvelopeData(value string, target any) error {
	envelope := map[string]any{}
	if err := json.Unmarshal([]byte(value), &envelope); err != nil {
		return err
	}

	rawData, ok := envelope["data"]
	if !ok {
		return os.ErrInvalid
	}

	encodedData, err := json.Marshal(rawData)
	if err != nil {
		return err
	}

	return json.Unmarshal(encodedData, target)
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

// firstOfJSONArray decodes an envelope whose data is a list and returns its
// first entry.
//
// A companion to decodeJSONMap, for the commands that answer with a list rather
// than an object -- a listing, or a command whose result is one row per thing
// the caller named.
func firstOfJSONArray(t *testing.T, value string) map[string]any {
	t.Helper()

	var envelope struct {
		Data []map[string]any `json:"data"`
		Meta struct {
			BBVersion string `json:"bbVersion"`
		} `json:"meta"`
	}

	if err := json.Unmarshal([]byte(value), &envelope); err != nil {
		t.Fatalf("expected json array output, got parse error %v for: %s", err, value)
	}
	if strings.TrimSpace(envelope.Meta.BBVersion) == "" {
		t.Fatalf("expected a bb envelope carrying meta.bbVersion: %s", value)
	}
	if len(envelope.Data) == 0 {
		t.Fatalf("expected at least one entry in the output: %s", value)
	}

	return envelope.Data[0]
}

// nestedJSONMap reads one object out of an envelope's data.
//
// The payloads that report a thing they created name it -- {"repository": ...,
// "task": ...} -- rather than returning the object bare, so the caller can see
// what it was created on without a second call.
func nestedJSONMap(t *testing.T, output string, key string) map[string]any {
	t.Helper()

	nested, ok := decodeJSONMap(t, output)[key].(map[string]any)
	if !ok {
		t.Fatalf("expected a %q object in the output: %s", key, output)
	}

	return nested
}

// mustLiveCLIUnscoped is mustLiveCLI without an injected repository context.
func mustLiveCLIUnscoped(t *testing.T, args ...string) string {
	t.Helper()

	output, err := executeLiveCLIUnscoped(t, append([]string{"--json"}, args...)...)
	if err != nil {
		t.Fatalf("%s failed: %v\noutput: %s", strings.Join(args, " "), err, output)
	}

	return output
}

// commandCoverageAssertFields compares the named fields of a decoded object with
// the values that were sent.
func commandCoverageAssertFields(t *testing.T, subject string, got map[string]any, want map[string]any) {
	t.Helper()

	for field, value := range want {
		if got[field] != value {
			t.Errorf("%s: %s = %v, want %v (all: %v)", subject, field, got[field], value, got)
		}
	}
}

// commandCoverageEntry finds the entry of a listing whose field holds value.
func commandCoverageEntry(t *testing.T, output, field, value string) map[string]any {
	t.Helper()

	var entries []map[string]any
	decodeJSONData(t, output, &entries)
	for _, entry := range entries {
		if entry[field] == value {
			return entry
		}
	}

	t.Fatalf("no entry with %s=%s in the listing: %s", field, value, output)

	return nil
}

// commandCoverageFieldValues reads one field off every entry of a listing,
// sorted, so what a listing holds can be compared apart from its order.
func commandCoverageFieldValues(t *testing.T, output, field string) []string {
	t.Helper()

	var entries []map[string]any
	decodeJSONData(t, output, &entries)

	values := make([]string, 0, len(entries))
	for _, entry := range entries {
		values = append(values, asString(entry[field]))
	}
	slices.Sort(values)

	return values
}

// commandCoverageAnnotations keys an annotation listing by external id.
func commandCoverageAnnotations(t *testing.T, output string) map[string]map[string]any {
	t.Helper()

	var entries []map[string]any
	decodeJSONData(t, output, &entries)

	byExternalID := make(map[string]map[string]any, len(entries))
	for _, entry := range entries {
		byExternalID[asString(entry["externalId"])] = entry
	}

	return byExternalID
}

// commandCoveragePullRequests keys a pull request listing by id.
func commandCoveragePullRequests(t *testing.T, output string) map[string]map[string]any {
	t.Helper()

	entries, _ := decodeJSONMap(t, output)["pullRequests"].([]any)
	byID := make(map[string]map[string]any, len(entries))
	for _, entry := range entries {
		pullRequest, _ := entry.(map[string]any)
		id, _ := numericOrStringID(pullRequest["id"])
		byID[id] = pullRequest
	}

	return byID
}

// commandCoverageAssertRequiredCheck reads one required build check out of a
// listing and compares the build key and branch matcher it was given.
func commandCoverageAssertRequiredCheck(t *testing.T, output, id, buildKey, matcherID, matcherType string) {
	t.Helper()

	var listed any
	if err := decodeJSONEnvelopeData(output, &listed); err != nil {
		t.Fatalf("build required list returned invalid JSON: %v\n%s", err, output)
	}
	check, found := findByID(listed, id)
	if !found {
		t.Fatalf("required build check %s is not listed: %s", id, output)
	}
	if keys, _ := check["buildParentKeys"].([]any); len(keys) != 1 || keys[0] != buildKey {
		t.Errorf("check %s: buildParentKeys = %v, want [%s]", id, check["buildParentKeys"], buildKey)
	}
	matcher, _ := check["refMatcher"].(map[string]any)
	commandCoverageAssertFields(t, "the refMatcher of check "+id, matcher, map[string]any{"id": matcherID, "type": matcherType})
}

// commandCoverageAuthCheck is the authentication check of an auth status
// payload, the one that asks Bitbucket who the credential belongs to.
func commandCoverageAuthCheck(status map[string]any) map[string]any {
	checks, _ := status["checks"].([]any)
	for _, raw := range checks {
		if check, ok := raw.(map[string]any); ok && check["name"] == "authentication" {
			return check
		}
	}

	return nil
}

// commandCoverageFirstColumn reads the first column of each row a human listing
// prints, one tab-separated row per entry. The paging hint has no tab.
func commandCoverageFirstColumn(output string) []string {
	rows := []string{}
	for _, line := range strings.Split(output, "\n") {
		if first, _, tabbed := strings.Cut(strings.TrimSuffix(line, "\r"), "\t"); tabbed {
			rows = append(rows, first)
		}
	}

	return rows
}

// commandCoverageAddedLines is what a unified diff adds: every line starting
// with + that is not a +++ header, without the marker.
func commandCoverageAddedLines(patch string) []string {
	added := []string{}
	for _, line := range strings.Split(patch, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			added = append(added, strings.TrimPrefix(line, "+"))
		}
	}

	return added
}

// commandCoverageDiffFiles names the files a unified diff touches, in order, from
// its --- and +++ lines. Bitbucket marks the two sides a/ and b/ in a patch and
// src:// and dst:// in a raw diff.
func commandCoverageDiffFiles(patch string) []string {
	files := []string{}
	for _, line := range strings.Split(patch, "\n") {
		side, isHeader := strings.CutPrefix(strings.TrimSuffix(line, "\r"), "--- ")
		if !isHeader {
			side, isHeader = strings.CutPrefix(strings.TrimSuffix(line, "\r"), "+++ ")
		}
		if !isHeader {
			continue
		}

		side, _, _ = strings.Cut(side, "\t")
		for _, prefix := range []string{"a/", "b/", "src://", "dst://"} {
			if trimmed, found := strings.CutPrefix(side, prefix); found {
				side = trimmed

				break
			}
		}
		if side != "/dev/null" && !slices.Contains(files, side) {
			files = append(files, side)
		}
	}

	return files
}

// commandCoverageCommitsOn lists the commits reachable from a branch, newest
// first, polling until the listing holds as many as were pushed.
func commandCoverageCommitsOn(t *testing.T, ctx context.Context, harness *liveHarness, projectKey, slug, branch string, want int) []string {
	t.Helper()

	until := "refs/heads/" + branch
	limit := float32(want)

	var ids []string
	for range 8 {
		response, err := harness.client.GetCommitsWithResponse(ctx, projectKey, slug, &openapigenerated.GetCommitsParams{Until: &until, Limit: &limit})
		if err != nil {
			t.Fatalf("list the commits on %s: %v", branch, err)
		}

		ids = nil
		if page := response.ApplicationjsonCharsetUTF8200; page != nil && page.Values != nil {
			for _, commit := range *page.Values {
				if commit.Id != nil {
					ids = append(ids, *commit.Id)
				}
			}
		}
		if len(ids) >= want {
			return ids
		}

		time.Sleep(500 * time.Millisecond)
	}

	t.Fatalf("want %d commits on %s, found %d: %v", want, branch, len(ids), ids)

	return nil
}

// commandCoverageTwoFileCommit pushes one commit, on a branch of its own off
// master, that appends line to seed.txt and adds a second file with it, and
// returns the commit's id.
func commandCoverageTwoFileCommit(t *testing.T, harness *liveHarness, projectKey, slug, branch, line string) string {
	t.Helper()

	dir := t.TempDir()
	pushURL, err := repositoryPushURL(harness.config, projectKey, slug)
	if err != nil {
		t.Fatalf("build the push url: %v", err)
	}

	for _, args := range [][]string{
		{"init"},
		{"config", "user.name", "bb-live-test"},
		{"config", "user.email", "bb-live-test@example.local"},
		{"remote", "add", "origin", pushURL},
		{"fetch", "origin", "master"},
		{"checkout", "-b", branch, "FETCH_HEAD"},
	} {
		if err := runGit(dir, args...); err != nil {
			t.Fatalf("prepare the two-file commit: %v", err)
		}
	}

	seed, err := os.OpenFile(filepath.Join(dir, "seed.txt"), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open seed.txt: %v", err)
	}
	_, writeErr := seed.WriteString(line + "\n")
	if closeErr := seed.Close(); writeErr != nil || closeErr != nil {
		t.Fatalf("append to seed.txt: %v, %v", writeErr, closeErr)
	}
	if err := os.WriteFile(filepath.Join(dir, "second-file.txt"), []byte(line+"\n"), 0o600); err != nil {
		t.Fatalf("write the second file: %v", err)
	}

	for _, args := range [][]string{
		{"add", "seed.txt", "second-file.txt"},
		{"commit", "-m", "change two files"},
		{"push", "-u", "origin", branch},
	} {
		if err := runGit(dir, args...); err != nil {
			t.Fatalf("push the two-file commit: %v", err)
		}
	}

	head, err := runGitCapture(dir, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("read the two-file commit id: %v", err)
	}

	return strings.TrimSpace(head)
}

// commandCoverageTagMessage fetches a tag over git and returns the first line
// of its message.
func commandCoverageTagMessage(t *testing.T, harness *liveHarness, projectKey, slug, tag string) string {
	t.Helper()

	dir := t.TempDir()
	fetchURL, err := repositoryPushURL(harness.config, projectKey, slug)
	if err != nil {
		t.Fatalf("build the fetch url: %v", err)
	}

	ref := "refs/tags/" + tag
	if err := runGit(dir, "init"); err != nil {
		t.Fatalf("git init: %v", err)
	}
	if err := runGit(dir, "fetch", fetchURL, ref+":"+ref); err != nil {
		t.Fatalf("fetch %s: %v", ref, err)
	}

	subject, err := runGitCapture(dir, "for-each-ref", "--format=%(contents:subject)", ref)
	if err != nil {
		t.Fatalf("read the message of %s: %v", ref, err)
	}

	return strings.TrimSpace(subject)
}
