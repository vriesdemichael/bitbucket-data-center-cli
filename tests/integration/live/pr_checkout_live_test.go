//go:build live

package live_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
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
	if secondData["fastForwarded"] != true {
		t.Fatalf("expected the second run to fast-forward the existing branch, got: %s", secondOutput)
	}

	// A dirty tree is refused rather than silently discarded.
	//
	// Modifying a tracked file in place, without switching branches first: an
	// earlier version wrote the file and then ran `git checkout master`, which
	// git refuses for exactly the reason under test here. The refusal happens
	// before anything is touched, so where HEAD currently sits does not matter.
	if err := os.WriteFile(filepath.Join(cloneDir, "checkout-live.txt"), []byte("local edit\n"), 0o600); err != nil {
		t.Fatalf("write local edit failed: %v", err)
	}

	dirtyOutput, dirtyErr := executeLiveCLI(t, "pr", "checkout", pullRequestID)
	if dirtyErr == nil {
		t.Fatalf("expected a dirty working tree to be refused, got: %s", dirtyOutput)
	}
	if !strings.Contains(dirtyOutput, "uncommitted changes") && !strings.Contains(dirtyErr.Error(), "uncommitted changes") {
		t.Fatalf("expected the refusal to name uncommitted changes, got: %v\noutput: %s", dirtyErr, dirtyOutput)
	}

	// --force gets past it, which is the escape hatch the refusal advertises.
	forcedOutput, err := executeLiveCLI(t, "pr", "checkout", pullRequestID, "--force")
	if err != nil {
		t.Fatalf("pr checkout --force over a dirty tree failed: %v\noutput: %s", err, forcedOutput)
	}
}

// TestLivePullRequestCheckoutFromAFork covers the case the same-repository
// test cannot reach: a pull request whose source is a different repository.
//
// Unit tests asserted the branch prefix and the added remote against a pull
// request payload they wrote, with a stub backend recording what it was asked
// to do. Two things there could be wrong and neither was under test: whether
// Bitbucket's payload distinguishes a fork the way bb reads it, and whether
// the remote bb adds is one git can fetch from.
func TestLivePullRequestCheckoutFromAFork(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	upstream := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, upstream.Slug)

	forkName := fmt.Sprintf("lt-fork-checkout-%d", time.Now().UnixNano()%100000)
	forkOutput, err := executeLiveCLI(t, "--json", "repo", "admin", "fork",
		"--repo", seeded.Key+"/"+upstream.Slug, "--name", forkName, "--project", seeded.Key)
	if err != nil {
		t.Fatalf("repo admin fork failed: %v\noutput: %s", err, forkOutput)
	}
	forkSlug := asString(decodeJSONMap(t, forkOutput)["slug"])
	if forkSlug == "" {
		if inner, ok := decodeJSONMap(t, forkOutput)["repository"].(map[string]any); ok {
			forkSlug = asString(inner["slug"])
		}
	}
	if forkSlug == "" {
		t.Fatalf("the fork has no slug:\n%s", forkOutput)
	}

	const sourceBranch = "feature/from-the-fork"
	if err := harness.pushCommitOnBranch(seeded.Key, forkSlug, sourceBranch, "from-the-fork.txt"); err != nil {
		t.Fatalf("push a commit on the fork failed: %v", err)
	}

	// Cross-repository, which the harness helper cannot express: the source ref
	// carries its own repository.
	created, err := harness.liveJSON(ctx, http.MethodPost,
		fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/pull-requests", seeded.Key, upstream.Slug),
		map[string]any{
			"title": "From the fork",
			"fromRef": map[string]any{
				"id":         "refs/heads/" + sourceBranch,
				"repository": map[string]any{"slug": forkSlug, "project": map[string]any{"key": seeded.Key}},
			},
			"toRef": map[string]any{"id": "refs/heads/master"},
		})
	if err != nil {
		t.Fatalf("create the cross-repository pull request failed: %v", err)
	}
	rawID, _ := created["id"].(float64)
	pullRequestID := strconv.Itoa(int(rawID))
	if pullRequestID == "0" {
		t.Fatalf("the created pull request has no id: %#v", created)
	}

	cloneDir := filepath.Join(t.TempDir(), "upstream-clone")
	if output, err := executeLiveCLI(t, "repo", "clone", seeded.Key+"/"+upstream.Slug, cloneDir); err != nil {
		t.Fatalf("repo clone failed: %v\noutput: %s", err, output)
	}

	originalDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	if err := os.Chdir(cloneDir); err != nil {
		t.Fatalf("chdir into the clone failed: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalDirectory) })

	output, err := executeLiveCLI(t, "--json", "pr", "checkout", pullRequestID)
	if err != nil {
		t.Fatalf("pr checkout of a fork pull request failed: %v\noutput: %s", err, output)
	}

	data := decodeJSONMap(t, output)
	if data["fork"] != true {
		t.Fatalf("a pull request from a fork was not reported as one: %s", output)
	}

	// The branch is prefixed so it cannot collide with a local branch of the
	// same name, and HEAD is actually on it.
	branch := asString(data["branch"])
	if branch == sourceBranch || !strings.Contains(branch, sourceBranch) {
		t.Fatalf("expected the fork's branch to be prefixed, got %q: %s", branch, output)
	}
	// symbolic-ref rather than rev-parse --abbrev-ref: the prefix bb gives the
	// branch is the name it gives the remote, so refs/heads/<owner>/<branch>
	// and refs/remotes/<owner>/<branch> both exist and --abbrev-ref answers
	// "heads/<owner>/<branch>" to disambiguate.
	head, err := runGitCapture(cloneDir, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		t.Fatalf("symbolic-ref HEAD failed: %v", err)
	}
	if strings.TrimSpace(head) != branch {
		t.Fatalf("expected HEAD on %q, got %q", branch, strings.TrimSpace(head))
	}

	// And the remote it fetched from is a real one pointing at the fork -- the
	// half a recording stub cannot establish.
	remotes, err := runGitCapture(cloneDir, "remote", "-v")
	if err != nil {
		t.Fatalf("git remote -v failed: %v", err)
	}
	if !strings.Contains(remotes, "/"+forkSlug+".git") {
		t.Fatalf("no remote points at the fork:\n%s", remotes)
	}
}
