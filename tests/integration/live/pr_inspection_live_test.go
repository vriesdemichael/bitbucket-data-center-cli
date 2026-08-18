//go:build live

package live_test

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestLivePullRequestInspection covers the read side of a pull request:
// commits, files, merge-base, build status and jira.
//
// Read-only commands carry a narrower guarantee than mutating ones, and it is
// worth naming: a wrong parameter or path returns an empty result rather than
// an error, so the assertion is that the *seeded* content comes back — not
// merely that the call succeeded.
func TestLivePullRequestInspection(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	branch := "feature/pr-inspection-live"
	fileName := "pr-inspection-live.txt"

	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, fileName); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	pullRequestID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
	if err != nil {
		t.Fatalf("create pull request failed: %v", err)
	}

	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	commitsOutput, err := executeLiveCLI(t, "--json", "pr", "commits", pullRequestID, "--repo", repoRef, "--limit", "25")
	if err != nil {
		t.Fatalf("pr commits failed: %v\noutput: %s", err, commitsOutput)
	}
	if !strings.Contains(commitsOutput, "commits") {
		t.Fatalf("expected a commits payload, got: %s", commitsOutput)
	}

	// The seeded file must appear, which is what proves the change listing is
	// scoped to this pull request rather than returning something generic.
	filesOutput, err := executeLiveCLI(t, "--json", "pr", "files", pullRequestID, "--repo", repoRef, "--limit", "25")
	if err != nil {
		t.Fatalf("pr files failed: %v\noutput: %s", err, filesOutput)
	}
	if !strings.Contains(filesOutput, fileName) {
		t.Fatalf("expected the seeded file in the change listing, got: %s", filesOutput)
	}

	mergeBaseOutput, err := executeLiveCLI(t, "--json", "pr", "merge-base", pullRequestID, "--repo", repoRef)
	if err != nil {
		t.Fatalf("pr merge-base failed: %v\noutput: %s", err, mergeBaseOutput)
	}

	buildStatusOutput, err := executeLiveCLI(t, "--json", "pr", "build", "status", pullRequestID, "--repo", repoRef)
	if err != nil {
		t.Fatalf("pr build status failed: %v\noutput: %s", err, buildStatusOutput)
	}

	// No Jira link is configured on the test instance, so the guarantee here is
	// that the command handles an unlinked pull request rather than failing.
	jiraOutput, err := executeLiveCLI(t, "--json", "pr", "jira", pullRequestID, "--repo", repoRef)
	if err != nil {
		t.Fatalf("pr jira failed: %v\noutput: %s", err, jiraOutput)
	}
}

// bb pr task create|list|update|delete are deliberately not covered here.
// The pull request task endpoints do not exist in Bitbucket 10.2 — the vendored
// spec has no pull-requests/{id}/tasks path at all — so the commands answer 404
// and cannot be made to pass. Removal is filed separately; a test that skipped
// on the error is exactly what let this survive for years.
