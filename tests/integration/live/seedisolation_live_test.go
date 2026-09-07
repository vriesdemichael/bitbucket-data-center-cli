//go:build live

package live_test

import (
	"context"
	"testing"
	"time"
)

// TestLiveSeededRepositoriesGetDistinctCommits guards the isolation every other
// live test assumes.
//
// Seeded commits used to be byte-identical across repositories -- same empty
// parent, same seed.txt, same message, same bb-live-test author -- and a git
// timestamp has one-second resolution, so any two repositories seeded in the
// same second shared a commit sha. Sequentially that was rare; under a parallel
// suite it was the normal case.
//
// The damage is not that the ids look alike. Bitbucket keys build statuses and
// commit-level reports on the hash across the whole instance rather than per
// repository, so one test setting a build status marked every other test's
// identical commit as built. TestLiveQualityEmptyAnswers found four successful
// builds on a commit nobody had built, and it was right to complain. Statuses
// outlive the repository they were set through, so the collision reached across
// runs too.
//
// Two repositories in one project, seeded back to back, is the tightest version
// of that race this suite can arrange deliberately.
func TestLiveSeededRepositoriesGetDistinctCommits(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{Repos: 2, Commits: 2, WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed two repositories failed: %v", err)
	}
	if len(seeded.Repos) != 2 {
		t.Fatalf("got %d repositories, want 2", len(seeded.Repos))
	}

	seenIn := map[string]string{}
	for _, repo := range seeded.Repos {
		if len(repo.CommitIDs) == 0 {
			t.Fatalf("repository %s reported no commit ids", repo.Slug)
		}

		for _, commitID := range repo.CommitIDs {
			if previous, seen := seenIn[commitID]; seen {
				t.Errorf("commit %s is shared by %s and %s; a build status set on one would be read by the other",
					commitID, previous, repo.Slug)

				continue
			}
			seenIn[commitID] = repo.Slug
		}
	}
}
