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

// TestLiveReviewSummaryNeverOverclaims covers the case that makes a partial
// measurement dangerous: a pull request with an unresolved comment and no tasks
// at all.
//
// Counted through the task tally alone -- which is what --no-review-summary
// does, and what a degraded timeline leaves -- every number that comes back is
// zero and every one of them is true. The summary used to turn that into
// "Open items: none" and actionRequired false, which is a claim about the
// comments it never looked at, on a pull request that had one waiting.
//
// The counts have been pointers from the start so an unmeasured one is absent
// rather than zero. actionRequired was the exception, and it is the field a
// caller branches on.
func TestLiveReviewSummaryNeverOverclaims(t *testing.T) {
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

	const branch = "feature/overclaim"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "overclaim.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}
	prID := createLivePRForRegression(t, branch, "Overclaim", "--no-default-reviewers", "--no-codeowners")

	// One unresolved comment and deliberately no task, so every count the
	// blocker-comment tally can produce is a truthful zero.
	if _, err := harness.liveJSON(ctx, http.MethodPost,
		fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/pull-requests/%s/comments",
			seeded.Key, repo.Slug, prID),
		map[string]any{"text": "an unresolved comment"}); err != nil {
		t.Fatalf("create the comment failed: %v", err)
	}

	t.Run("a partial measurement does not answer actionRequired", func(t *testing.T) {
		summary := liveReviewSummary(t, mustLiveCLI(t, "pr", "get", prID, "--no-review-summary"))

		if summary["countsSource"] != "blocker_comments" {
			t.Fatalf("countsSource = %#v, want blocker_comments", summary["countsSource"])
		}
		if _, present := summary["actionRequired"]; present {
			t.Errorf("actionRequired = %#v; the threads were never counted, so there is no answer to give",
				summary["actionRequired"])
		}
		if _, present := summary["unresolvedThreads"]; present {
			t.Errorf("unresolvedThreads = %#v, want it absent", summary["unresolvedThreads"])
		}
	})

	t.Run("the human line admits what was not checked", func(t *testing.T) {
		output := mustLiveHumanCLI(t, "pr", "get", prID, "--no-review-summary")

		if strings.Contains(output, "Open items: none\n") {
			t.Fatalf("the summary claims nothing is outstanding while a comment waits:\n%s", output)
		}
		if !strings.Contains(output, "not checked") {
			t.Fatalf("expected the unmeasured half to be named, got:\n%s", output)
		}
	})

	t.Run("the full measurement still answers", func(t *testing.T) {
		// The point is not to make the summary timid. With the timeline walked,
		// the comment is found and the answer is definite.
		summary := liveReviewSummary(t, mustLiveCLI(t, "pr", "get", prID))

		if summary["countsSource"] != "activities" {
			t.Fatalf("countsSource = %#v, want activities", summary["countsSource"])
		}
		if summary["actionRequired"] != true {
			t.Fatalf("actionRequired = %#v, want true with an unresolved comment", summary["actionRequired"])
		}
		if summary["unresolvedThreads"] != float64(1) {
			t.Fatalf("unresolvedThreads = %#v, want 1", summary["unresolvedThreads"])
		}
	})
}
