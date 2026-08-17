//go:build live

package live_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLivePullRequestCheckout runs bb pr checkout against a real Bitbucket and
// a real clone.
//
// The unit tests assert which git commands the command chooses; only this one
// establishes that those commands actually produce a checked-out branch with a
// working upstream. The refspec and the branch config are the two things most
// likely to be subtly wrong in a way no stub would notice.
func TestLivePullRequestCheckout(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	sourceBranch := "feature/checkout-live"
	// The harness seeds repositories with master, as the other live tests assume.
	const defaultBranchName = "master"

	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, sourceBranch, "checkout-live.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	pullRequestID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, sourceBranch, defaultBranchName)
	if err != nil {
		t.Fatalf("create pull request failed: %v", err)
	}

	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	cloneDir := filepath.Join(t.TempDir(), "clone")
	cloneOutput, err := executeLiveCLI(t, "repo", "clone", seeded.Key+"/"+repo.Slug, cloneDir)
	if err != nil {
		t.Fatalf("repo clone failed: %v\noutput: %s", err, cloneOutput)
	}

	originalDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	if err := os.Chdir(cloneDir); err != nil {
		t.Fatalf("chdir into the clone failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalDirectory)
	})

	output, err := executeLiveCLI(t, "--json", "pr", "checkout", pullRequestID)
	if err != nil {
		t.Fatalf("pr checkout failed: %v\noutput: %s", err, output)
	}

	result := decodeJSONMap(t, output)
	data, ok := result["data"].(map[string]any)
	if !ok {
		data = result
	}
	if asString(data["branch"]) != sourceBranch {
		t.Fatalf("expected branch %q, got: %s", sourceBranch, output)
	}
	if data["fork"] != false {
		t.Fatalf("expected a same-repository checkout, got: %s", output)
	}

	// The branch is actually checked out, not merely fetched.
	headOutput, err := runGitCapture(cloneDir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse HEAD failed: %v", err)
	}
	if strings.TrimSpace(headOutput) != sourceBranch {
		t.Fatalf("expected HEAD on %q, got %q", sourceBranch, strings.TrimSpace(headOutput))
	}

	// The upstream is what makes a later bare `git push` reach the right place,
	// and it is the part a stub cannot prove.
	upstreamOutput, err := runGitCapture(cloneDir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	if err != nil {
		t.Fatalf("resolve upstream failed: %v", err)
	}
	if strings.TrimSpace(upstreamOutput) != "origin/"+sourceBranch {
		t.Fatalf("expected upstream origin/%s, got %q", sourceBranch, strings.TrimSpace(upstreamOutput))
	}

	// Running it again must be a no-op fast-forward rather than a failure: the
	// branch now exists locally, which is the second-run case.
	secondOutput, err := executeLiveCLI(t, "--json", "pr", "checkout", pullRequestID)
	if err != nil {
		t.Fatalf("second pr checkout failed: %v\noutput: %s", err, secondOutput)
	}
	secondResult := decodeJSONMap(t, secondOutput)
	secondData, ok := secondResult["data"].(map[string]any)
	if !ok {
		secondData = secondResult
	}
	if secondData["fast_forwarded"] != true {
		t.Fatalf("expected the second run to fast-forward the existing branch, got: %s", secondOutput)
	}

	// A dirty tree is refused rather than silently discarded.
	if err := os.WriteFile(filepath.Join(cloneDir, "checkout-live.txt"), []byte("local edit\n"), 0o600); err != nil {
		t.Fatalf("write local edit failed: %v", err)
	}
	if _, err := runGitCapture(cloneDir, "checkout", defaultBranchName); err != nil {
		t.Fatalf("switch to the default branch failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cloneDir, "checkout-live-dirty.txt"), []byte("tracked edit\n"), 0o600); err != nil {
		t.Fatalf("write tracked file failed: %v", err)
	}
	if _, err := runGitCapture(cloneDir, "add", "checkout-live-dirty.txt"); err != nil {
		t.Fatalf("git add failed: %v", err)
	}

	dirtyOutput, dirtyErr := executeLiveCLI(t, "pr", "checkout", pullRequestID)
	if dirtyErr == nil {
		t.Fatalf("expected a dirty working tree to be refused, got: %s", dirtyOutput)
	}
	if !strings.Contains(dirtyOutput, "uncommitted changes") && !strings.Contains(dirtyErr.Error(), "uncommitted changes") {
		t.Fatalf("expected the refusal to name uncommitted changes, got: %v\noutput: %s", dirtyErr, dirtyOutput)
	}
}
