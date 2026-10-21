//go:build live

package live_test

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"
)

// TestLiveListOffsetsStillApply holds --start where it moved (#722). It was
// declared on bb branch and bb commit, so every command beneath took it and
// only list read it; now list alone takes it. Each listing from an offset is
// read against the whole listing, asked for separately: it has to be the same
// entries, from that offset on.
func TestLiveListOffsetsStillApply(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{Commits: 4})
	if err != nil {
		t.Fatalf("seed repository failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)
	for index := range 3 {
		branch := fmt.Sprintf("offset-%d", index)
		if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, branch+".txt"); err != nil {
			t.Fatalf("push %s: %v", branch, err)
		}
	}

	t.Run("bb branch list --start", func(t *testing.T) {
		names := func(args ...string) []string {
			t.Helper()
			listed := []string{}
			for _, branch := range branchesInListing(t, mustLiveCLI(t, append([]string{"branch", "list", "--all"}, args...)...)) {
				listed = append(listed, branch.DisplayID)
			}
			return listed
		}

		whole := names()
		if len(whole) < 4 {
			t.Fatalf("the repository lists %d branches, want master and the three pushed: %v", len(whole), whole)
		}
		if fromTwo := names("--start", "2"); !slices.Equal(fromTwo, whole[2:]) {
			t.Errorf("branch list --start 2 lists %v, want %v", fromTwo, whole[2:])
		}
	})

	t.Run("bb commit list --start", func(t *testing.T) {
		ids := func(args ...string) []string {
			t.Helper()
			var listing struct {
				Commits []struct {
					ID string `json:"id"`
				} `json:"commits"`
			}
			output := mustLiveCLI(t, append([]string{"commit", "list", "--all"}, args...)...)
			if err := decodeJSONEnvelopeData(output, &listing); err != nil {
				t.Fatalf("commit list returned invalid JSON: %v\n%s", err, output)
			}
			listed := []string{}
			for _, commit := range listing.Commits {
				listed = append(listed, commit.ID)
			}
			return listed
		}

		whole := ids()
		if len(whole) < 4 {
			encoded, _ := json.Marshal(whole)
			t.Fatalf("master lists %d commits, want the four seeded: %s", len(whole), encoded)
		}
		if fromTwo := ids("--start", "2"); !slices.Equal(fromTwo, whole[2:]) {
			t.Errorf("commit list --start 2 lists %v, want %v", fromTwo, whole[2:])
		}
	})
}
