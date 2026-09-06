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
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
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

	// Ids are collected only for this repository, and only after the answer has
	// been checked for having room to hold it.
	//
	// A pull request id is unique inside a repository and nowhere else, so on
	// the dashboard -- which spans the instance -- a bare "2" matches whatever
	// other repository happens to have a second pull request. Against a
	// Bitbucket the whole live suite is writing to, that is most of them.
	listedIDs := func(t *testing.T, output string) []string {
		t.Helper()

		entries := collectionFromCLI(t, output, "pullRequests")
		if len(entries) >= dashboardPage {
			t.Fatalf("the answer filled the %d-row page, so a missing pull request means the page ran out rather than the filter excluded it", dashboardPage)
		}

		ids := make([]string, 0, 2)
		for _, entry := range entries {
			pullRequest, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			repository, _ := pullRequest["repository"].(map[string]any)
			if repository == nil {
				continue
			}
			if asString(repository["projectKey"])+"/"+asString(repository["slug"]) != repoRef {
				continue
			}
			ids = append(ids, asString(pullRequest["id"]))
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
		got := listedIDs(t, mustLiveCLI(t, "search", "prs", "--repo", repoRef, "--limit", dashboardPageArg))
		if !containsFold(got, mine) || !containsFold(got, theirs) {
			t.Errorf("the repository search returned %v, want both %s and %s", got, mine, theirs)
		}
	})

	t.Run("the dashboard is where role applies", func(t *testing.T) {
		// Which is what the refusal points the caller at, so it has to be true.
		// Run without the repository context the harness would otherwise
		// supply: a --repo is what this command refuses alongside --role, and
		// being unscoped is the subject.
		got := listedIDs(t, mustLiveCLIUnscoped(t, "search", "prs", "--role", "author", "--limit", dashboardPageArg))
		if !containsFold(got, mine) {
			t.Errorf("role=author dropped %s, which the caller wrote: %v", mine, got)
		}
		if containsFold(got, theirs) {
			t.Errorf("role=author kept %s, which somebody else wrote: %v", theirs, got)
		}
	})
}
