//go:build live

package live_test

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestLiveBuildStatusLifecycle covers bb build set/get/delete against a real
// commit.
//
// Build statuses hang off a commit id rather than a repository path, and the
// endpoint moved between API versions, so the thing worth proving here is that
// what bb writes is what bb reads back.
func TestLiveBuildStatusLifecycle(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
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

	deleteOutput, err := executeLiveCLI(t, "--json", "build", "delete", commit, "--key", buildKey, "--repo", repoRef)
	if err != nil {
		t.Fatalf("build delete failed: %v\noutput: %s", err, deleteOutput)
	}

	// Deleting has to actually remove it, which a 204 on its own does not say.
	afterDelete, afterErr := executeLiveCLI(t, "--json", "build", "get", commit, "--key", buildKey, "--repo", repoRef)
	if afterErr == nil && strings.Contains(afterDelete, "SUCCESSFUL") {
		t.Fatalf("expected the build status to be gone after delete, got: %s", afterDelete)
	}
}

// TestLiveInsightsAnnotationSet covers bb insights annotation set, which needs a
// code-insights report to attach to.
func TestLiveInsightsAnnotationSet(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
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
}

// TestLiveBranchModelInspect covers bb branch model inspect, which classifies a
// commit against the repository's branching model.
func TestLiveBranchModelInspect(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
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
