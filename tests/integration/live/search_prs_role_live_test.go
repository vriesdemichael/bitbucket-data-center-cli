//go:build live

package live_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestLiveSearchPullRequestsRole covers #540: --role was accepted with --repo
// and quietly did nothing.
//
// The repository pull-requests endpoint has no role parameter. The published
// spec lists withAttributes, at, withProperties, draft, filterText, state,
// order and direction, and Bitbucket ignores anything else, so a role sent
// there changes nothing about the answer. Probed against 10.4.2: role=AUTHOR,
// role=AUTHOR with an explicit username, and role=REVIEWER all return every
// open pull request in the repository.
//
// bb knew this -- the flag help said "only applied when --repo is not used" and
// the repository branch dropped the value on purpose -- but a caller who passed
// both got a list that meant something other than what they asked for, with
// nothing to say so. The dashboard is where role works, and it is what the
// refusal points at.
func TestLiveSearchPullRequestsRole(t *testing.T) {
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

	// One pull request the caller wrote, and one they did not, so a filter that
	// works has something to keep and something to drop.
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, "feature/mine", "mine.txt"); err != nil {
		t.Fatalf("push failed: %v", err)
	}
	mine, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, "feature/mine", "master")
	if err != nil {
		t.Fatalf("create the caller's pull request failed: %v", err)
	}

	author, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create the other author failed: %v", err)
	}
	if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, author.Username, "REPO_WRITE"); err != nil {
		t.Fatalf("grant write failed: %v", err)
	}
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, "feature/theirs", "theirs.txt"); err != nil {
		t.Fatalf("push failed: %v", err)
	}
	authored, err := harness.liveJSONAs(ctx, author, http.MethodPost,
		fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/pull-requests", seeded.Key, repo.Slug),
		map[string]any{
			"title":   "Written by somebody else",
			"fromRef": map[string]any{"id": "refs/heads/feature/theirs"},
			"toRef":   map[string]any{"id": "refs/heads/master"},
		})
	if err != nil {
		t.Fatalf("create the other author's pull request failed: %v", err)
	}
	theirs := fmt.Sprintf("%d", int64(authored["id"].(float64)))

	listedIDs := func(t *testing.T, output string) []string {
		t.Helper()
		ids := make([]string, 0, 2)
		for _, entry := range collectionFromCLI(t, output, "pullRequests") {
			if pullRequest, ok := entry.(map[string]any); ok {
				ids = append(ids, asString(pullRequest["id"]))
			}
		}
		return ids
	}

	t.Run("a repository search refuses a role it cannot apply", func(t *testing.T) {
		output, err := executeLiveCLI(t, "--json", "search", "prs", "--repo", repoRef, "--role", "author")
		if err == nil {
			t.Fatalf("--role was accepted with --repo, and the answer means something else:\n%s", output)
		}
		if !strings.Contains(err.Error(), "validation") {
			t.Errorf("kind should be validation, got: %v", err)
		}
		if !strings.Contains(err.Error(), "--repo") || !strings.Contains(err.Error(), "--role") {
			t.Errorf("the refusal should name both flags: %v", err)
		}
	})

	t.Run("a repository search without a role still works", func(t *testing.T) {
		// The refusal is about the combination, not about --repo.
		got := listedIDs(t, mustLiveCLI(t, "search", "prs", "--repo", repoRef, "--limit", "25"))
		if !containsFold(got, mine) || !containsFold(got, theirs) {
			t.Errorf("the repository search returned %v, want both %s and %s", got, mine, theirs)
		}
	})

	t.Run("the dashboard is where role applies", func(t *testing.T) {
		// Which is what the refusal points the caller at, so it has to be true.
		got := listedIDs(t, mustLiveCLI(t, "search", "prs", "--role", "author", "--limit", "25"))
		if !containsFold(got, mine) {
			t.Errorf("role=author dropped %s, which the caller wrote: %v", mine, got)
		}
		if containsFold(got, theirs) {
			t.Errorf("role=author kept %s, which somebody else wrote: %v", theirs, got)
		}
	})
}
