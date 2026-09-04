//go:build live

package live_test

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestLivePullRequestAutoMergeEnable is the test whose absence let #378 ship.
//
// bb posted to the auto-merge endpoint to arm auto-merge. That endpoint retries
// an existing request rather than creating one, so the call had never once
// succeeded against a real Bitbucket — and nothing noticed, because the unit
// tests stubbed the endpoint bb believed in. A stub can confirm bb called what
// bb thought it should call; only a real server can say that belief was wrong.
func TestLivePullRequestAutoMergeEnable(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	branch := "feature/auto-merge-live"

	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "auto-merge-live.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	pullRequestID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
	if err != nil {
		t.Fatalf("create pull request failed: %v", err)
	}

	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// The server rejects arming with 403 unless the repository permits
	// auto-merge at all.
	if output, err := executeLiveCLI(t, "repo", "settings", "auto-merge", "set", "--enabled", "--repo", repoRef); err != nil {
		t.Fatalf("enable repository auto-merge failed: %v\noutput: %s", err, output)
	}

	// A blocker, so the pull request cannot merge on the spot. Without one it
	// would merge immediately and the pending state — the thing that was broken
	// — would never be exercised.
	if output, err := executeLiveCLI(t, "repo", "settings", "pull-requests", "update-approvers", "--count", "1", "--repo", repoRef); err != nil {
		t.Fatalf("require an approver failed: %v\noutput: %s", err, output)
	}

	output, err := executeLiveCLI(t, "--json", "pr", "auto-merge", "enable", pullRequestID, "--repo", repoRef)
	if err != nil {
		t.Fatalf("pr auto-merge enable failed: %v\noutput: %s", err, output)
	}

	payload := decodeJSONMap(t, output)
	data, ok := payload["data"].(map[string]any)
	if !ok {
		data = payload
	}
	autoMerge, ok := data["autoMerge"].(map[string]any)
	if !ok {
		t.Fatalf("expected autoMerge in the payload, got: %s", output)
	}
	if autoMerge["enabled"] != true {
		t.Fatalf("expected auto-merge to be armed, got: %s", output)
	}

	// The server's own view, not bb's echo of its own request: this is what
	// AutoMergeNotRequestedException was telling us was missing.
	getOutput, err := executeLiveCLI(t, "--json", "pr", "auto-merge", "get", pullRequestID, "--repo", repoRef)
	if err != nil {
		t.Fatalf("pr auto-merge get failed: %v\noutput: %s", err, getOutput)
	}
	if !strings.Contains(getOutput, "\"enabled\": true") {
		t.Fatalf("expected the server to report auto-merge armed, got: %s", getOutput)
	}

	// And it can be cancelled again, which only works if something was armed.
	if disableOutput, err := executeLiveCLI(t, "pr", "auto-merge", "disable", pullRequestID, "--repo", repoRef); err != nil {
		t.Fatalf("pr auto-merge disable failed: %v\noutput: %s", err, disableOutput)
	}
}

// TestLivePullRequestAutoMergeMergesImmediately covers the other outcome.
//
// The test above deliberately blocks the merge so the pending state is
// exercised. With nothing blocking, arming auto-merge merges the pull request
// on the spot, and Bitbucket says so in the same response -- there is no
// pending auto-merge afterwards, because there is nothing left to wait for.
//
// The unit test that covered this built the answer from a fixture, so it could
// only confirm that bb reads the field it was handed. Whether the server
// reports an immediate merge this way, and whether the pull request really is
// merged, are the parts that matter.
func TestLivePullRequestAutoMergeMergesImmediately(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	const branch = "feature/auto-merge-immediate"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "auto-merge-now.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}
	prID := createLivePRForRegression(t, branch, "Merges immediately", "--no-default-reviewers", "--no-codeowners")

	if output, err := executeLiveCLI(t, "repo", "settings", "auto-merge", "set", "--enabled", "--repo", repoRef); err != nil {
		t.Fatalf("enable repository auto-merge failed: %v\noutput: %s", err, output)
	}

	// Nothing blocks this one, so arming it should merge it.
	output := mustLiveCLI(t, "pr", "auto-merge", "enable", prID, "--repo", repoRef)

	autoMerge, ok := decodeJSONMap(t, output)["autoMerge"].(map[string]any)
	if !ok {
		t.Fatalf("expected autoMerge in the payload, got:\n%s", output)
	}
	if autoMerge["mergedImmediately"] != true {
		t.Fatalf("expected mergedImmediately, got: %#v", autoMerge)
	}
	// Reporting an armed auto-merge here would describe a state that will never
	// fire: there is nothing left to merge.
	if autoMerge["enabled"] == true {
		t.Errorf("an immediate merge left auto-merge armed: %#v", autoMerge)
	}

	state, _ := extractPRData(decodeJSONMap(t, mustLiveCLI(t, "pr", "get", prID)))["state"].(string)
	if state != "MERGED" {
		t.Fatalf("state = %q, want MERGED", state)
	}
}
