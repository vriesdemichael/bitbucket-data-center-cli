//go:build live

package live_test

import (
	"context"
	"strings"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// TestLiveBuildStatusLifecycle covers bb build set/get/delete against a real
// commit.
//
// Build statuses hang off a commit id rather than a repository path, and the
// endpoint moved between API versions, so the thing worth proving here is that
// what bb writes is what bb reads back.
func TestLiveBuildStatusLifecycle(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	commits, err := harness.listCommitIDs(ctx, seeded.Key, repo.Slug, 1)
	if err != nil || len(commits) == 0 {
		t.Fatalf("list commit ids failed: %v (%d commits)", err, len(commits))
	}
	commit := commits[0]

	const buildKey = "live-suite-build"
	setOutput, err := executeLiveCLI(t, "--json", "build", "set", commit,
		"--key", buildKey, "--state", "SUCCESSFUL", "--name", "Live Suite Build",
		"--url", "http://localhost:7990/builds/1", "--description", "set by the live suite",
		"--build-number", "1", "--duration-ms", "1234", "--repo", repoRef)
	if err != nil {
		t.Fatalf("build set failed: %v\noutput: %s", err, setOutput)
	}

	// A second build on the same commit. get and delete find a build by --key
	// alone, and Bitbucket answers a delete whose key matches nothing with 204,
	// so only a neighbour under another key shows which key they honoured.
	const neighbourKey = "live-suite-neighbour"
	mustLiveCLI(t, "build", "set", commit, "--key", neighbourKey, "--state", "FAILED",
		"--url", "http://localhost:7990/builds/2", "--repo", repoRef)
	assertNeighbour := func(when string) {
		t.Helper()

		neighbour := decodeJSONMap(t, mustLiveCLI(t, "build", "get", commit, "--key", neighbourKey, "--repo", repoRef))
		if neighbour["key"] != neighbourKey || neighbour["state"] != "FAILED" || neighbour["url"] != "http://localhost:7990/builds/2" {
			t.Errorf("%s, the build under %s came back as %v", when, neighbourKey, neighbour)
		}
	}

	getOutput, err := executeLiveCLI(t, "--json", "build", "get", commit, "--key", buildKey, "--repo", repoRef)
	if err != nil {
		t.Fatalf("build get failed: %v\noutput: %s", err, getOutput)
	}
	if !strings.Contains(getOutput, "SUCCESSFUL") || !strings.Contains(getOutput, buildKey) {
		t.Fatalf("expected the build status just set, got: %s", getOutput)
	}

	// Every optional field, read back rather than watched on the wire.
	//
	// A unit test asserted these by matching substrings in the request body
	// against a mock, which says the client serialised them and nothing about
	// whether Bitbucket kept them. A field that is sent and dropped looks
	// identical from the client side and is the failure worth catching: the
	// caller was told the build was recorded with a description it does not
	// have.
	for field, want := range map[string]string{
		"key":         buildKey,
		"state":       "SUCCESSFUL",
		"name":        "Live Suite Build",
		"description": "set by the live suite",
		"url":         "http://localhost:7990/builds/1",
		"buildNumber": "1",
	} {
		if got, _ := decodeJSONMap(t, getOutput)[field].(string); got != want {
			t.Errorf("%s came back as %q, want %q:\n%s", field, got, want, getOutput)
		}
	}
	if duration, _ := decodeJSONMap(t, getOutput)["duration"].(float64); int64(duration) != 1234 {
		t.Errorf("duration came back as %v, want 1234:\n%s", duration, getOutput)
	}
	// --repo is the path the build was set through, and Bitbucket names that
	// repository on the status it stored.
	if repository, _ := decodeJSONMap(t, getOutput)["repository"].(map[string]any); repository["projectKey"] != seeded.Key || repository["slug"] != repo.Slug {
		t.Errorf("repository came back as %v, want %s:\n%s", repository, repoRef, getOutput)
	}
	assertNeighbour("before the delete")

	deleteOutput, err := executeLiveCLI(t, "--json", "build", "delete", commit, "--key", buildKey, "--repo", repoRef, "--yes")
	if err != nil {
		t.Fatalf("build delete failed: %v\noutput: %s", err, deleteOutput)
	}

	// Deleting has to actually remove it, which a 204 on its own does not say.
	afterDelete, afterErr := executeLiveCLI(t, "--json", "build", "get", commit, "--key", buildKey, "--repo", repoRef)
	if afterErr == nil && strings.Contains(afterDelete, "SUCCESSFUL") {
		t.Fatalf("expected the build status to be gone after delete, got: %s", afterDelete)
	}
	if afterErr == nil || apperrors.ExitCode(afterErr) != 4 {
		t.Errorf("reading the deleted build did not answer not found (exit 4): %v\n%s", afterErr, afterDelete)
	}
	assertNeighbour("after deleting " + buildKey)
}

// TestLiveInsightsAnnotationSet covers bb insights annotation set, which needs a
// code-insights report to attach to.
func TestLiveInsightsAnnotationSet(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	commits, err := harness.listCommitIDs(ctx, seeded.Key, repo.Slug, 1)
	if err != nil || len(commits) == 0 {
		t.Fatalf("list commit ids failed: %v (%d commits)", err, len(commits))
	}
	commit := commits[0]

	const reportKey = "live-suite-report"
	reportOutput, err := executeLiveCLI(t, "--json", "insights", "report", "set", commit, reportKey,
		"--body", `{"title":"Live Suite Report","result":"PASS"}`, "--repo", repoRef)
	if err != nil {
		t.Fatalf("insights report set failed: %v\noutput: %s", err, reportOutput)
	}

	// The body reaches Bitbucket through bb's typed request, so a field that
	// type does not carry would be dropped before it is sent. result is absent
	// on a report that was not given one.
	report := decodeJSONMap(t, mustLiveCLI(t, "insights", "report", "get", commit, reportKey, "--repo", repoRef))
	for field, want := range map[string]string{"key": reportKey, "title": "Live Suite Report", "result": "PASS"} {
		if got, _ := report[field].(string); got != want {
			t.Errorf("report %s came back as %q, want %q: %v", field, got, want, report)
		}
	}

	setOutput, err := executeLiveCLI(t, "--json", "insights", "annotation", "set", commit, reportKey, "live-annotation-1",
		"--message", "annotation from the live suite", "--severity", "MEDIUM", "--type", "CODE_SMELL",
		"--path", "file-1.txt", "--line", "1", "--link", "http://localhost:7990/annotation/1", "--repo", repoRef)
	if err != nil {
		t.Fatalf("insights annotation set failed: %v\noutput: %s", err, setOutput)
	}

	listOutput, err := executeLiveCLI(t, "--json", "insights", "annotation", "list", commit, reportKey, "--repo", repoRef)
	if err != nil {
		t.Fatalf("insights annotation list failed: %v\noutput: %s", err, listOutput)
	}
	if !strings.Contains(listOutput, "annotation from the live suite") {
		t.Fatalf("expected the annotation just set in the listing, got: %s", listOutput)
	}

	// Every field the set sent. Bitbucket answers the set with 204 and no body,
	// so the listing is the only account of what it kept; path, line, link and
	// type are optional and absent unless sent.
	first := map[string]any{
		"reportKey": reportKey, "message": "annotation from the live suite", "severity": "MEDIUM", "type": "CODE_SMELL",
		"path": "file-1.txt", "line": float64(1), "link": "http://localhost:7990/annotation/1",
	}
	assertListedInsightAnnotation(t, listOutput, "live-annotation-1", first)

	// Neither annotation endpoint pages, so --limit did nothing: a listing
	// asked for one of two printed both and still reported reaching the
	// limit (#573).
	secondOutput, err := executeLiveCLI(t, "--json", "insights", "annotation", "set", commit, reportKey, "live-annotation-2",
		"--message", "a second annotation from the live suite", "--severity", "LOW", "--type", "CODE_SMELL",
		"--path", "file-1.txt", "--line", "2", "--link", "http://localhost:7990/annotation/2", "--repo", repoRef)
	if err != nil {
		t.Fatalf("second insights annotation set failed: %v\noutput: %s", err, secondOutput)
	}

	// A set is addressed by its external id, so the second is kept beside the
	// first rather than in its place.
	bothOutput := mustLiveCLI(t, "insights", "annotation", "list", commit, reportKey, "--repo", repoRef)
	assertListedInsightAnnotation(t, bothOutput, "live-annotation-1", first)
	assertListedInsightAnnotation(t, bothOutput, "live-annotation-2", map[string]any{
		"reportKey": reportKey, "message": "a second annotation from the live suite", "severity": "LOW", "type": "CODE_SMELL",
		"path": "file-1.txt", "line": float64(2), "link": "http://localhost:7990/annotation/2",
	})

	cutOutput, err := executeLiveCLI(t, "--json", "insights", "annotation", "list", commit, reportKey, "--repo", repoRef, "--limit", "1")
	if err != nil {
		t.Fatalf("insights annotation list --limit 1 failed: %v\noutput: %s", err, cutOutput)
	}
	if got := strings.Count(cutOutput, `"externalId"`); got != 1 || !strings.Contains(cutOutput, `"limitReached": true`) {
		t.Fatalf("a listing of two annotations under --limit 1 returned %d and should say it stopped:\n%s", got, cutOutput)
	}
}

// TestLiveBranchModelInspect covers bb branch model inspect, which classifies a
// commit against the repository's branching model.
func TestLiveBranchModelInspect(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	commits, err := harness.listCommitIDs(ctx, seeded.Key, repo.Slug, 1)
	if err != nil || len(commits) == 0 {
		t.Fatalf("list commit ids failed: %v (%d commits)", err, len(commits))
	}

	output, err := executeLiveCLI(t, "--json", "branch", "model", "inspect", commits[0], "--repo", repoRef)
	if err != nil {
		t.Fatalf("branch model inspect failed: %v\noutput: %s", err, output)
	}
	// The commit is on master and nowhere else, so master is what has to come
	// back. Until recently the commit id reached the server wrapped in literal
	// double quotes and every call answered 500, whatever the id.
	if !strings.Contains(output, "refs/heads/master") {
		t.Fatalf("expected the containing branch in the output, got: %s", output)
	}
}

// assertListedInsightAnnotation finds one annotation in an `insights annotation
// list` output by its external id and compares the fields given.
func assertListedInsightAnnotation(t *testing.T, listing, externalID string, want map[string]any) {
	t.Helper()

	var annotations []map[string]any
	decodeJSONData(t, listing, &annotations)

	for _, annotation := range annotations {
		if annotation["externalId"] != externalID {
			continue
		}
		for field, value := range want {
			if annotation[field] != value {
				t.Errorf("annotation %s: %s came back as %v, want %v", externalID, field, annotation[field], value)
			}
		}
		return
	}

	t.Errorf("annotation %s is not in the listing:\n%s", externalID, listing)
}
