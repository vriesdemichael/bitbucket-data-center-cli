//go:build live

package live_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// TestLiveDryRunPredictionsReadRealState covers the previews whose answer is not
// a property of the command but of what the server currently holds.
//
// A preview that always says "create" needs no server. These are the other
// kind: merging a merged pull request is a no-op, merging a declined one is
// blocked, approving one you have already approved changes nothing, and opening
// a pull request for a branch that already has one conflicts. The prediction is
// read from Bitbucket, and the mocked versions read it from a fixture the same
// author wrote -- so they agreed about a state no Bitbucket had ever been in.
//
// One of them was wrong for exactly that reason: the unapprove preview predicted
// no-op from a fabricated participant list, and against a real pull request
// whose reviewer is on NEEDS_WORK the answer is not no-op. See
// TestLivePullRequestReviewSetCommand.
func TestLiveDryRunPredictionsReadRealState(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// Three pull requests in three states, each reached by doing it rather than
	// by declaring it.
	openBranch := "feature/prediction-open"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, openBranch, "open.txt"); err != nil {
		t.Fatalf("push %s failed: %v", openBranch, err)
	}
	openPR, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, openBranch, "master")
	if err != nil {
		t.Fatalf("create the open pull request failed: %v", err)
	}

	declinedBranch := "feature/prediction-declined"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, declinedBranch, "declined.txt"); err != nil {
		t.Fatalf("push %s failed: %v", declinedBranch, err)
	}
	declinedPR, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, declinedBranch, "master")
	if err != nil {
		t.Fatalf("create the pull request to decline failed: %v", err)
	}
	if _, err := executeLiveCLI(t, "--json", "pr", "decline", declinedPR); err != nil {
		t.Fatalf("decline failed: %v", err)
	}

	mergedBranch := "feature/prediction-merged"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, mergedBranch, "merged.txt"); err != nil {
		t.Fatalf("push %s failed: %v", mergedBranch, err)
	}
	mergedPR, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, mergedBranch, "master")
	if err != nil {
		t.Fatalf("create the pull request to merge failed: %v", err)
	}
	if _, err := executeLiveCLI(t, "--json", "pr", "merge", mergedPR); err != nil {
		t.Fatalf("merge failed: %v", err)
	}

	predicts := func(t *testing.T, want string, args ...string) {
		t.Helper()

		output := mustLiveCLI(t, append([]string{"--dry-run"}, args...)...)
		if !strings.Contains(output, fmt.Sprintf(`"predictedAction": %q`, want)) {
			t.Fatalf("expected %s to predict %q:\n%s", strings.Join(args, " "), want, output)
		}
	}

	t.Run("opening a pull request the branch already has", func(t *testing.T) {
		predicts(t, "conflict", "pr", "create", "--from-ref", openBranch, "--to-ref", "master", "--title", "Second")
	})

	t.Run("merging one that is already merged", func(t *testing.T) {
		predicts(t, "no-op", "pr", "merge", mergedPR)
	})

	t.Run("merging one that was declined", func(t *testing.T) {
		predicts(t, "blocked", "pr", "merge", declinedPR)
	})

	t.Run("declining one that is already declined", func(t *testing.T) {
		predicts(t, "no-op", "pr", "decline", declinedPR)
	})

	t.Run("reopening one that is open", func(t *testing.T) {
		predicts(t, "no-op", "pr", "reopen", openPR)
	})

	reviewer, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, reviewer.Username,
		openapigenerated.SetPermissionForUserParamsPermissionREPOWRITE); err != nil {
		t.Fatalf("grant repository permission failed: %v", err)
	}

	t.Run("adding a reviewer who is already one", func(t *testing.T) {
		if _, err := executeLiveCLI(t, "--json", "pr", "review", "reviewer", "add", openPR, "--user", reviewer.Username); err != nil {
			t.Fatalf("adding the reviewer failed: %v", err)
		}

		predicts(t, "no-op", "pr", "review", "reviewer", "add", openPR, "--user", reviewer.Username)
	})

	t.Run("removing a reviewer who is not one", func(t *testing.T) {
		predicts(t, "no-op", "pr", "review", "reviewer", "remove", openPR, "--user", "no-such-reviewer")
	})

	// Approval is the author's blind spot, and the mock did not have one.
	//
	// Its fixture listed alice as an APPROVED reviewer and ran the preview as
	// alice, who was also the author. Bitbucket refuses that outright --
	// "Authors may not update their status" -- so the state the no-op was
	// predicted from is one no pull request can be in. The reviewer has to be
	// somebody else, and the preview has to run as them.
	t.Run("code insights read the report and its annotations", func(t *testing.T) {
		commit := repo.CommitIDs[0]
		const reportKey = "prediction-report"
		const externalID = "prediction-annotation"

		// Nothing there yet, so both are creates and both deletes are no-ops.
		predicts(t, "create", "insights", "report", "set", commit, reportKey,
			"--body", `{"title":"Predicted","result":"PASS"}`)
		predicts(t, "no-op", "insights", "report", "delete", commit, reportKey)

		mustLiveCLI(t, "insights", "report", "set", commit, reportKey,
			"--body", `{"title":"Predicted","result":"PASS"}`)
		mustLiveCLI(t, "insights", "annotation", "add", commit, reportKey,
			"--body", fmt.Sprintf(`[{"externalId":%q,"message":"note","severity":"LOW"}]`, externalID))

		// And now the same four commands answer differently, which is the whole
		// point: the prediction is about the server, not about the arguments.
		predicts(t, "update", "insights", "report", "set", commit, reportKey,
			"--body", `{"title":"Predicted again","result":"PASS"}`)
		predicts(t, "delete", "insights", "annotation", "delete", commit, reportKey, "--external-id", externalID)
		predicts(t, "no-op", "insights", "annotation", "delete", commit, reportKey, "--external-id", "never-added")
		predicts(t, "delete", "insights", "report", "delete", commit, reportKey)
	})

	t.Run("approving one already approved", func(t *testing.T) {
		configureLiveCLIEnvForUser(t, harness, seeded.Key, repo.Slug, reviewer)

		if _, err := executeLiveCLI(t, "--json", "pr", "review", "approve", openPR); err != nil {
			t.Fatalf("approve failed: %v", err)
		}

		predicts(t, "no-op", "pr", "review", "approve", openPR)
	})
}
