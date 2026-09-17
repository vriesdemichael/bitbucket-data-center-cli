//go:build live

package live_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// TestLiveSearchCommands covers bb search repos, commits and prs.
//
// All three are read-only, so the guarantee they add is narrower than a
// mutating command's: not "does this change the right thing" but "does the
// query bb builds actually return what the server has". A wrong parameter name
// yields an empty list rather than an error, which is the failure a stub
// cannot see.
func TestLiveSearchCommands(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 3)
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
	// And nothing the name does not match: a search that lost its query answers
	// with the first page of every repository, which on a shared instance may
	// hold this one too.
	for _, slug := range commandCoverageFieldValues(t, reposOutput, "slug") {
		if !strings.Contains(strings.ToLower(slug), strings.ToLower(repo.Slug)) {
			t.Errorf("searching for %q found %q", repo.Slug, slug)
		}
	}

	commitsOutput, err := executeLiveCLI(t, "--json", "search", "commits", "--repo", repoRef, "--limit", "10")
	if err != nil {
		t.Fatalf("search commits failed: %v\noutput: %s", err, commitsOutput)
	}
	if !strings.Contains(commitsOutput, "commits") {
		t.Fatalf("expected a commits payload, got: %s", commitsOutput)
	}

	// The filters, which nothing had driven.
	//
	// --since and --until are the only way the commit listing's Since and Until
	// options are ever set, and --path the only way its Path is set outside the
	// history command. They are three lines that build query parameters, and a
	// query parameter that is built wrong does not fail -- it comes back with
	// the wrong commits, or with all of them.
	t.Run("the commit filters narrow the answer", func(t *testing.T) {
		// Not a skip on a short history: the repository is seeded with three
		// commits, so fewer is a broken fixture rather than a reason to pass. A
		// test that can skip itself passes whether or not the command works,
		// which is what command-reach refuses to count.
		commits, err := harness.listCommitIDs(ctx, seeded.Key, repo.Slug, 10)
		if err != nil {
			t.Fatalf("list commit ids failed: %v", err)
		}
		if len(commits) != 3 {
			t.Fatalf("the seeded repository has %d commits, want three to bound a range inside", len(commits))
		}
		newest, middle, oldest := commits[0], commits[1], commits[2]

		// since is exclusive and until inclusive, so the range from the oldest
		// to the middle one holds the middle one alone. Neither end is where the
		// listing would stop without it: without --since the oldest is in too,
		// and without --until the newest.
		ranged := mustLiveCLI(t, "search", "commits", "--repo", repoRef,
			"--since", oldest, "--until", middle, "--limit", "50")
		if ids := listedCommitIDs(t, ranged); !slices.Equal(ids, []string{middle}) {
			t.Errorf("--since %s --until %s returned %v, want [%s] (the newest is %s):\n%s",
				oldest, middle, ids, middle, newest, ranged)
		}

		// A path nothing touches, so the filter has something to exclude.
		byPath := mustLiveCLI(t, "search", "commits", "--repo", repoRef,
			"--path", "no/such/path.txt", "--limit", "50")
		if listed, _ := decodeJSONMap(t, byPath)["commits"].([]any); len(listed) != 0 {
			t.Errorf("--path on a file that does not exist returned %d commits:\n%s", len(listed), byPath)
		}
	})

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

// listedCommitIDs reads the ids of a commit listing, in the order it lists them.
func listedCommitIDs(t *testing.T, output string) []string {
	t.Helper()

	commits, ok := decodeJSONMap(t, output)["commits"].([]any)
	if !ok {
		t.Fatalf("no commits in the listing:\n%s", output)
	}
	ids := make([]string, 0, len(commits))
	for _, entry := range commits {
		commit, _ := entry.(map[string]any)
		ids = append(ids, asString(commit["id"]))
	}

	return ids
}

// TestLiveRepoLabelAndWatchLifecycle covers repo label add, list and remove
// plus repo watch and unwatch.
//
// Not parallel. Twice in CI, with the parallel tests running, Bitbucket
// answered the label add with 500 "A database error has occurred". Label adds
// fired all at once, and label adds amid a stream of repository creations, did
// not reproduce it against an otherwise idle instance, so what it collides with
// is something the parallel suite does. Go runs the tests that did not declare
// themselves parallel before it releases any that did, so this one has the
// instance to itself.
func TestLiveRepoLabelAndWatchLifecycle(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	label := testsupport.UniqueName("live-label-")

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

	if output, err := executeLiveCLI(t, "--json", "repo", "label", "remove", label, "--repo", repoRef, "--yes"); err != nil {
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
