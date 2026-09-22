//go:build live

package live_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// TestLiveCompletionMatcherFollowsTheMatcherType covers the one slot whose
// meaning is decided by another flag on the same line.
//
// A branch restriction matches a ref in four ways and --matcher-id means
// something different in each. Offering branches for all of them would offer
// values Bitbucket refuses for three of the four, which is the failure this
// source exists to avoid -- and the one that looks like it works, because a
// branch name is a plausible-looking thing to have typed.
func TestLiveCompletionMatcherFollowsTheMatcherType(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	branch := testsupport.UniqueName("lt-matcher-")
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "matcher.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	selector := seeded.Key + "/" + repo.Slug

	complete := func(t *testing.T, matcherType string) map[string]string {
		t.Helper()

		candidates, _ := completeLive(t,
			"branch", "restriction", "create",
			"--repo", selector,
			"--matcher-type", matcherType,
			"--matcher-id", "")

		return candidates
	}

	t.Run("a model branch is offered with the branch it points at", func(t *testing.T) {
		candidates := complete(t, "MODEL_BRANCH")

		description, offered := candidates["development"]
		if !offered {
			t.Fatalf("expected the model's development branch to be offered, got %v", candidates)
		}
		// The repository's model reports its default branch here, which is
		// what makes the description worth reading: "development" alone says
		// nothing about which branch the restriction will cover.
		if strings.TrimSpace(description) == "" || strings.EqualFold(description, "not configured") {
			t.Errorf("expected the branch development points at, got %q", description)
		}
		if _, offered := candidates["master"]; offered {
			t.Error("a branch name was offered for a matcher type that takes a model branch")
		}
	})

	t.Run("a model category is offered with the prefix it matches", func(t *testing.T) {
		candidates := complete(t, "MODEL_CATEGORY")

		description, offered := candidates["FEATURE"]
		if !offered {
			t.Fatalf("expected the FEATURE category to be offered, got %v", candidates)
		}
		if !strings.Contains(description, "/") {
			t.Errorf("expected the prefix the category matches, got %q", description)
		}
	})

	t.Run("a pattern is written rather than chosen", func(t *testing.T) {
		if candidates := complete(t, "PATTERN"); len(candidates) != 0 {
			t.Errorf("expected nothing offered for a glob the caller writes, got %v", candidates)
		}
	})

	t.Run("a branch matcher offers branches", func(t *testing.T) {
		candidates := complete(t, "BRANCH")

		if _, offered := candidates[branch]; !offered {
			t.Errorf("expected the seeded branch %s to be offered, got %v", branch, candidates)
		}
		if _, offered := candidates["FEATURE"]; offered {
			t.Error("a model category was offered for a matcher type that takes a branch")
		}
	})
}
