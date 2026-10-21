//go:build live

package live_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// The pr commands that only read a pull request take it from the checked-out
// branch when no number is given, as gh pr view and gh pr checks do (#766):
// the open pull request whose source is that branch, named on stderr. A branch
// with none, or with more than one, is refused, naming what was found, and so
// is a repository named with --repo, which the branch underfoot says nothing
// about.
//
// Not parallel: the commands read the working directory, which is one per
// process.
func TestLivePullRequestReadCommandsTakeTheCheckedOutBranch(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnvVars(t, "WRONG", "wrong")

	for _, branch := range []string{"has-one", "has-two", "has-none", "release"} {
		if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, branch+".txt"); err != nil {
			t.Fatalf("push %s failed: %v", branch, err)
		}
	}
	only, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, "has-one", "master")
	if err != nil {
		t.Fatalf("open the pull request from has-one failed: %v", err)
	}
	intoMaster, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, "has-two", "master")
	if err != nil {
		t.Fatalf("open the first pull request from has-two failed: %v", err)
	}
	intoRelease, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, "has-two", "release")
	if err != nil {
		t.Fatalf("open the second pull request from has-two failed: %v", err)
	}

	pushURL, err := repositoryPushURL(harness.config, seeded.Key, repo.Slug)
	if err != nil {
		t.Fatalf("build push url: %v", err)
	}
	checkout := t.TempDir()
	for _, args := range [][]string{{"init"}, {"remote", "add", "origin", pushURL}, {"fetch", "origin"}} {
		if err := runGit(checkout, args...); err != nil {
			t.Fatalf("git %s failed: %v", strings.Join(args, " "), err)
		}
	}
	checkoutBranch := func(t *testing.T, branch string) {
		t.Helper()
		if err := runGit(checkout, "checkout", "-B", branch, "origin/"+branch); err != nil {
			t.Fatalf("git checkout %s failed: %v", branch, err)
		}
	}

	originalDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	if err := os.Chdir(checkout); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalDirectory) })

	t.Run("one open pull request is read, and named", func(t *testing.T) {
		checkoutBranch(t, "has-one")

		var viewed struct {
			PullRequest map[string]any `json:"pullRequest"`
		}
		decodeJSONData(t, mustLiveCLI(t, "pr", "view", "--no-review-summary"), &viewed)
		if id, _ := numericOrStringID(viewed.PullRequest["id"]); id != only || viewed.PullRequest["sourceBranch"] != "has-one" {
			t.Fatalf("pr view read pull request %v from %v, want %s from has-one", viewed.PullRequest["id"], viewed.PullRequest["sourceBranch"], only)
		}

		// Human output names what it inferred, on stderr.
		output, err := executeLiveCLI(t, "pr", "checks")
		if err != nil {
			t.Fatalf("pr checks failed: %v\n%s", err, output)
		}
		if !strings.Contains(output, "Using pull request #"+only+" (the open pull request from the checked-out branch, has-one)") {
			t.Errorf("pr checks does not say which pull request it read:\n%s", output)
		}

		var comments struct {
			Comments []any `json:"comments"`
		}
		decodeJSONData(t, mustLiveCLI(t, "pr", "comment", "list"), &comments)
	})

	t.Run("two open pull requests are refused, both named", func(t *testing.T) {
		checkoutBranch(t, "has-two")

		output, err := executeLiveCLI(t, "pr", "view")
		if !apperrors.IsKind(err, apperrors.KindValidation) {
			t.Fatalf("pr view on a branch with two open pull requests: want validation, got %v\n%s", err, output)
		}
		for _, named := range []string{"#" + intoMaster + " into master", "#" + intoRelease + " into release"} {
			if !strings.Contains(err.Error(), named) {
				t.Errorf("the refusal does not name %s: %v", named, err)
			}
		}
	})

	t.Run("no open pull request is not found", func(t *testing.T) {
		checkoutBranch(t, "has-none")

		output, err := executeLiveCLI(t, "pr", "view")
		if !apperrors.IsKind(err, apperrors.KindNotFound) || !strings.Contains(err.Error(), "has-none") {
			t.Fatalf("pr view on a branch with no open pull request: want not_found naming the branch, got %v\n%s", err, output)
		}
	})

	t.Run("a repository named with --repo needs the number", func(t *testing.T) {
		checkoutBranch(t, "has-one")

		output, err := executeLiveCLI(t, "pr", "view", "--repo", seeded.Key+"/"+repo.Slug)
		if !apperrors.IsKind(err, apperrors.KindValidation) || !strings.Contains(err.Error(), "not taken from a checkout") {
			t.Fatalf("pr view --repo without a number: want validation, got %v\n%s", err, output)
		}
	})

	t.Run("the arity message names the command as typed", func(t *testing.T) {
		output, err := executeLiveCLI(t, "pr", "view", only, only)
		if err == nil || !strings.Contains(err.Error(), "pr view takes [pr-id]") {
			t.Fatalf("pr view with two numbers: want the message to name pr view, got %v\n%s", err, output)
		}
	})
}

// TestLiveTheCheckedOutBranchsPullRequestIsFromThisRepository holds the
// inference to the checkout's own repository. It listed the pull requests into
// the repository and matched the branch by name, so a fork's pull request from
// a branch of the same name was read as the checkout's, and a fork's checkout,
// whose pull request goes into its upstream, found none.
func TestLiveTheCheckedOutBranchsPullRequestIsFromThisRepository(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	upstream := seeded.Repos[0]
	configureLiveCLIEnvVars(t, "WRONG", "wrong")

	forkOutput, err := executeLiveCLI(t, "--json", "repo", "admin", "fork",
		"--repo", seeded.Key+"/"+upstream.Slug, "--name", testsupport.UniqueName("lt-fork-branch-"), "--project", seeded.Key)
	if err != nil {
		t.Fatalf("repo admin fork failed: %v\noutput: %s", err, forkOutput)
	}
	forkSlug := asString(nestedJSONMap(t, forkOutput, "repository")["slug"])
	if forkSlug == "" {
		t.Fatalf("the fork has no slug:\n%s", forkOutput)
	}

	const branch = "feature/shared-name"
	for _, slug := range []string{upstream.Slug, forkSlug} {
		if err := harness.pushCommitOnBranch(seeded.Key, slug, branch, "shared-name.txt"); err != nil {
			t.Fatalf("push %s to %s failed: %v", branch, slug, err)
		}
	}
	// From the fork's branch into the upstream: the upstream's own branch of
	// the same name has none.
	created, err := harness.liveJSON(ctx, http.MethodPost,
		fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/pull-requests", seeded.Key, upstream.Slug),
		map[string]any{
			"title": "From the fork's branch",
			"fromRef": map[string]any{
				"id":         "refs/heads/" + branch,
				"repository": map[string]any{"slug": forkSlug, "project": map[string]any{"key": seeded.Key}},
			},
			"toRef": map[string]any{"id": "refs/heads/master"},
		})
	if err != nil {
		t.Fatalf("open the pull request from the fork failed: %v", err)
	}
	fromFork, _ := numericOrStringID(created["id"])

	checkout := func(t *testing.T, slug string) {
		t.Helper()
		pushURL, err := repositoryPushURL(harness.config, seeded.Key, slug)
		if err != nil {
			t.Fatalf("build push url: %v", err)
		}
		directory := t.TempDir()
		for _, args := range [][]string{{"init"}, {"remote", "add", "origin", pushURL}, {"fetch", "origin"}, {"checkout", "-B", branch, "origin/" + branch}} {
			if err := runGit(directory, args...); err != nil {
				t.Fatalf("git %s failed: %v", strings.Join(args, " "), err)
			}
		}
		t.Chdir(directory)
	}

	t.Run("the upstream's branch does not take the fork's pull request", func(t *testing.T) {
		checkout(t, upstream.Slug)

		output, err := executeLiveCLI(t, "pr", "view")
		if !apperrors.IsKind(err, apperrors.KindNotFound) {
			t.Fatalf("pr view in the upstream on %s: want not_found, got %v\n%s", branch, err, output)
		}
	})

	t.Run("the fork's branch finds its pull request into the upstream", func(t *testing.T) {
		checkout(t, forkSlug)

		var viewed struct {
			PullRequest map[string]any `json:"pullRequest"`
		}
		decodeJSONData(t, mustLiveCLI(t, "pr", "view", "--no-review-summary"), &viewed)
		repository, _ := viewed.PullRequest["repository"].(map[string]any)
		if id, _ := numericOrStringID(viewed.PullRequest["id"]); id != fromFork || repository["slug"] != upstream.Slug {
			t.Fatalf("pr view in the fork read pull request %v of %v, want %s of %s", viewed.PullRequest["id"], repository, fromFork, upstream.Slug)
		}
	})
}
