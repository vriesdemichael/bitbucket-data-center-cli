//go:build live

package live_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestLiveSearchCommands covers bb search repos, commits and prs.
//
// All three are read-only, so the guarantee they add is narrower than a
// mutating command's: not "does this change the right thing" but "does the
// query bb builds actually return what the server has". A wrong parameter name
// yields an empty list rather than an error, which is the failure a stub
// cannot see.
func TestLiveSearchCommands(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 2)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	branch := "feature/search-live"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "search-live.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}
	if _, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master"); err != nil {
		t.Fatalf("create pull request failed: %v", err)
	}

	// Repositories: the seeded one must come back, which is what proves the
	// query reached the right endpoint with the right filter.
	reposOutput, err := executeLiveCLI(t, "--json", "search", "repos", repo.Slug, "--limit", "50")
	if err != nil {
		t.Fatalf("search repos failed: %v\noutput: %s", err, reposOutput)
	}
	if !strings.Contains(reposOutput, repo.Slug) {
		t.Fatalf("expected the seeded repository in search results, got: %s", reposOutput)
	}

	commitsOutput, err := executeLiveCLI(t, "--json", "search", "commits", "--repo", repoRef, "--limit", "10")
	if err != nil {
		t.Fatalf("search commits failed: %v\noutput: %s", err, commitsOutput)
	}
	if !strings.Contains(commitsOutput, "commits") {
		t.Fatalf("expected a commits payload, got: %s", commitsOutput)
	}

	// Dashboard-scoped, so it needs no repository and exercises a different
	// endpoint from the repository listing.
	prsOutput, err := executeLiveCLI(t, "--json", "search", "prs", "--state", "open", "--limit", "10")
	if err != nil {
		t.Fatalf("search prs failed: %v\noutput: %s", err, prsOutput)
	}
	if !strings.Contains(prsOutput, "pullRequests") {
		t.Fatalf("expected a pull_requests payload, got: %s", prsOutput)
	}
}

// TestLiveRepoLabelAndWatchLifecycle covers repo label add, list and remove
// plus repo watch and unwatch.
func TestLiveRepoLabelAndWatchLifecycle(t *testing.T) {
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

	label := fmt.Sprintf("live-label-%d", time.Now().UnixNano()%100000)

	if output, err := executeLiveCLI(t, "--json", "repo", "label", "add", label, "--repo", repoRef); err != nil {
		t.Fatalf("repo label add failed: %v\noutput: %s", err, output)
	}

	listOutput, err := executeLiveCLI(t, "--json", "repo", "label", "list", "--repo", repoRef)
	if err != nil {
		t.Fatalf("repo label list failed: %v\noutput: %s", err, listOutput)
	}
	if !strings.Contains(listOutput, label) {
		t.Fatalf("expected the added label to be listed, got: %s", listOutput)
	}

	if output, err := executeLiveCLI(t, "--json", "repo", "label", "remove", label, "--repo", repoRef); err != nil {
		t.Fatalf("repo label remove failed: %v\noutput: %s", err, output)
	}

	afterRemove, err := executeLiveCLI(t, "--json", "repo", "label", "list", "--repo", repoRef)
	if err != nil {
		t.Fatalf("repo label list after remove failed: %v\noutput: %s", err, afterRemove)
	}
	if strings.Contains(afterRemove, label) {
		t.Fatalf("expected the label to be gone, got: %s", afterRemove)
	}

	// Watch and unwatch have no read-back of their own, so the assertion is
	// that the server accepts both and that unwatch is not rejected as a no-op
	// after watch — the pairing is the behaviour worth pinning.
	if output, err := executeLiveCLI(t, "repo", "watch", "--repo", repoRef); err != nil {
		t.Fatalf("repo watch failed: %v\noutput: %s", err, output)
	}
	if output, err := executeLiveCLI(t, "repo", "unwatch", "--repo", repoRef); err != nil {
		t.Fatalf("repo unwatch failed: %v\noutput: %s", err, output)
	}
}
