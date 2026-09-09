//go:build live

package live_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestLivePullRequestBuildStatuses covers reading build statuses through a pull
// request, and what --limit does to the result.
//
// The unit tests these replace drove a mock through pages of a listing the
// author had shaped, asserting the page size sent and the number of items kept.
// Both are claims about how Bitbucket paginates. Posting several statuses to a
// real commit and reading them back through the pull request settles the same
// questions without either claim.
func TestLivePullRequestBuildStatuses(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	const branch = "feature/build-statuses"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "built.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	prID := createLivePRForRegression(t, branch, "Has build statuses", "--no-default-reviewers", "--no-codeowners")

	// The statuses hang off the source commit, so it has to be the pull
	// request's own head rather than any commit in the repository.
	commit := currentLivePRSourceCommit(t, prID)

	const statusCount = 3
	for index := range statusCount {
		key := fmt.Sprintf("build-%d", index)
		mustLiveCLI(t, "build", "status", "set", commit,
			"--key", key,
			"--state", "SUCCESSFUL",
			"--url", "http://example.invalid/"+key,
			"--name", key)
	}

	t.Run("the statuses are readable through the pull request", func(t *testing.T) {
		output := mustLiveCLI(t, "pr", "build", "status", prID, "--all")
		for index := range statusCount {
			key := fmt.Sprintf("build-%d", index)
			if !strings.Contains(output, key) {
				t.Errorf("expected %s in the pull request build statuses:\n%s", key, output)
			}
		}
	})

	t.Run("bb pr checks is the same command under the gh spelling", func(t *testing.T) {
		// ADR-050 asks an alias to prove it produces what the canonical path
		// produces. Asserted here rather than in a unit test because the two
		// are separate registrations built from one constructor: they could
		// diverge in what they send, and only a real call sees that.
		canonical := mustLiveCLI(t, "pr", "build", "status", prID, "--all")
		alias := mustLiveCLI(t, "pr", "checks", prID, "--all")

		if alias != canonical {
			t.Errorf("pr checks and pr build status disagree.\nchecks:\n%s\nbuild status:\n%s", alias, canonical)
		}
	})

	t.Run("--limit truncates the result", func(t *testing.T) {
		// The flag has to mean "give me at most this many", not "fetch this
		// many per page and return everything".
		output := mustLiveCLI(t, "pr", "build", "status", prID, "--limit", "1")

		found := 0
		for index := range statusCount {
			if strings.Contains(output, fmt.Sprintf("build-%d", index)) {
				found++
			}
		}
		if found != 1 {
			t.Errorf("--limit 1 returned %d of %d statuses:\n%s", found, statusCount, output)
		}
	})
}
