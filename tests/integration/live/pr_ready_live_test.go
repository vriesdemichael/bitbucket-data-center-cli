//go:build live

package live_test

import (
	"context"
	"strings"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// TestLivePRReady covers `bb pr ready` in both directions, invoked the way it
// exists to be invoked: without --version.
//
// The pull request carries a reviewer and a description because the change is
// a PUT, and Bitbucket reads a PUT without reviewers as "no reviewers" (#511).
// A ready command that dropped them would pass every assertion about the draft
// flag.
func TestLivePRReady(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]

	// The author cannot review their own pull request, so the reviewer has to
	// be somebody else.
	reviewer, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create reviewer user failed: %v", err)
	}
	if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, reviewer.Username, "REPO_READ"); err != nil {
		t.Fatalf("grant reviewer read access failed: %v", err)
	}

	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	const branch = "feature/ready"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "ready.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	const description = "kept across the draft change"
	prID := createLivePRForRegression(t, branch, "Marked ready by bb pr ready",
		"--draft", "--description", description, "--reviewers", reviewer.Username,
		"--no-default-reviewers", "--no-codeowners")
	if !livePRIsDraft(t, prID) {
		t.Fatal("expected the pull request to be created as a draft")
	}

	// The preview first, so it is judged against a draft and has to leave one.
	preview, err := executeLiveCLI(t, "--dry-run", "--json", "pr", "ready", prID)
	if err != nil {
		t.Fatalf("pr ready --dry-run failed: %v\noutput: %s", err, preview)
	}
	if !strings.Contains(preview, `"predictedAction": "update"`) {
		t.Errorf("marking a draft ready was not predicted an update:\n%s", preview)
	}
	if !livePRIsDraft(t, prID) {
		t.Fatal("pr ready --dry-run took the pull request out of draft")
	}

	output, err := executeLiveCLI(t, "--json", "pr", "ready", prID)
	if err != nil {
		t.Fatalf("pr ready failed: %v\noutput: %s", err, output)
	}
	if changed, _ := decodeJSONMap(t, output)["changed"].(bool); !changed {
		t.Errorf("marking a draft ready did not report a change:\n%s", output)
	}
	if livePRIsDraft(t, prID) {
		t.Fatal("pr ready left the pull request a draft")
	}
	assertLivePRKeptItsReviewerAndDescription(t, prID, reviewer.Username, description)

	// Asked again: gh answers this as a success that says nothing changed, and
	// nothing may be sent -- a write would move the version under anyone else
	// holding it.
	version := currentLivePRVersion(t, prID)
	again, err := executeLiveCLI(t, "--json", "pr", "ready", prID)
	if err != nil {
		t.Fatalf("pr ready on a pull request already ready must succeed: %v\noutput: %s", err, again)
	}
	if changed, _ := decodeJSONMap(t, again)["changed"].(bool); changed {
		t.Errorf("a pull request already ready reported a change:\n%s", again)
	}
	if after := currentLivePRVersion(t, prID); after != version {
		t.Errorf("the version moved from %s to %s although nothing needed changing", version, after)
	}

	human, err := executeLiveCLI(t, "pr", "ready", prID)
	if err != nil {
		t.Fatalf("pr ready on a pull request already ready must succeed: %v\noutput: %s", err, human)
	}
	if !strings.Contains(human, "already ready for review") {
		t.Errorf("expected the human output to say it was already ready, got:\n%s", human)
	}

	noop, err := executeLiveCLI(t, "--dry-run", "--json", "pr", "ready", prID)
	if err != nil {
		t.Fatalf("pr ready --dry-run failed: %v\noutput: %s", err, noop)
	}
	if !strings.Contains(noop, `"predictedAction": "no-op"`) {
		t.Errorf("asking for the state it already holds was not predicted a no-op:\n%s", noop)
	}

	undone, err := executeLiveCLI(t, "--json", "pr", "ready", prID, "--undo")
	if err != nil {
		t.Fatalf("pr ready --undo failed: %v\noutput: %s", err, undone)
	}
	if changed, _ := decodeJSONMap(t, undone)["changed"].(bool); !changed {
		t.Errorf("turning a ready pull request into a draft did not report a change:\n%s", undone)
	}
	if !livePRIsDraft(t, prID) {
		t.Fatal("pr ready --undo did not make the pull request a draft")
	}
	assertLivePRKeptItsReviewerAndDescription(t, prID, reviewer.Username, description)

	undoneAgain, err := executeLiveCLI(t, "--json", "pr", "ready", prID, "--undo")
	if err != nil {
		t.Fatalf("pr ready --undo on a draft must succeed: %v\noutput: %s", err, undoneAgain)
	}
	if changed, _ := decodeJSONMap(t, undoneAgain)["changed"].(bool); changed {
		t.Errorf("a pull request already a draft reported a change:\n%s", undoneAgain)
	}
}

// TestLivePRReadyOnADeclinedPullRequestIsRefused covers the case the draft flag
// cannot decide.
//
// A declined pull request that is not a draft reads, from its flag alone, as
// already ready for review, and Bitbucket does not settle it either: sent the
// draft flag it already holds, a declined pull request answers 200 and bumps
// its version. Either way bb would report a pull request nobody can review as
// ready for review. It is refused before anything is sent, as gh refuses a
// closed pull request.
func TestLivePRReadyOnADeclinedPullRequestIsRefused(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	const branch = "feature/declined-before-ready"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "declined.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	prID := createLivePRForRegression(t, branch, "Declined before it was ready", "--no-default-reviewers", "--no-codeowners")
	if output, err := executeLiveCLI(t, "--json", "pr", "decline", prID); err != nil {
		t.Fatalf("pr decline failed: %v\noutput: %s", err, output)
	}

	preview, err := executeLiveCLI(t, "--dry-run", "--json", "pr", "ready", prID)
	if err != nil {
		t.Fatalf("pr ready --dry-run failed: %v\noutput: %s", err, preview)
	}
	if !strings.Contains(preview, `"predictedAction": "blocked"`) {
		t.Errorf("a draft change on a declined pull request was not predicted blocked:\n%s", preview)
	}

	version := currentLivePRVersion(t, prID)

	output, err := executeLiveCLI(t, "--json", "pr", "ready", prID)
	if err == nil {
		t.Fatalf("pr ready on a declined pull request reported success:\n%s", output)
	}
	if !apperrors.IsKind(err, apperrors.KindConflict) {
		t.Errorf("expected a conflict for a declined pull request, got %v", err)
	}

	undoOutput, err := executeLiveCLI(t, "--json", "pr", "ready", prID, "--undo")
	if err == nil {
		t.Fatalf("pr ready --undo on a declined pull request reported success:\n%s", undoOutput)
	}
	if !apperrors.IsKind(err, apperrors.KindConflict) {
		t.Errorf("expected a conflict for a declined pull request, got %v", err)
	}

	// Nothing was sent. Bitbucket accepts the draft flag a declined pull request
	// already holds and bumps the version for it, so a refusal that only came
	// after a write would show here.
	if after := currentLivePRVersion(t, prID); after != version {
		t.Errorf("the version moved from %s to %s, so a draft change reached the declined pull request", version, after)
	}
}

// TestLivePRDraftChangeWithAStaleVersionNamesTheException pins the answer the
// retry in `bb pr ready` keys on.
//
// The retry itself needs Bitbucket to move the version between bb's read and
// bb's write inside one invocation, a race the live suite can lose or win but
// not arrange. What can be pinned is what it recognises: a draft change sent
// through the same transport with a version the server no longer holds fails
// with PullRequestOutOfDateException. A bare 409 would not do, because a
// declined pull request answers 409 too, and retrying that is pointless.
func TestLivePRDraftChangeWithAStaleVersionNamesTheException(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	const branch = "feature/stale-draft-change"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "stale.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	prID := createLivePRForRegression(t, branch, "Changed under a stale version", "--draft", "--no-default-reviewers", "--no-codeowners")

	stale := currentLivePRVersion(t, prID)
	mustLiveCLI(t, "pr", "ready", prID)

	output, err := executeLiveCLI(t, "--json", "pr", "update", prID, "--version", stale, "--draft")
	if err == nil {
		t.Fatalf("a draft change carrying a stale version succeeded:\n%s", output)
	}

	details := apperrors.DetailsOf(err)
	if details["upstreamStatus"] != "409" || details["upstreamException"] != "com.atlassian.bitbucket.pull.PullRequestOutOfDateException" {
		t.Fatalf("expected a 409 naming PullRequestOutOfDateException, got %v: %v", details, err)
	}
}

func assertLivePRKeptItsReviewerAndDescription(t *testing.T, prID, reviewer, description string) {
	t.Helper()

	if reviewers := currentLivePRReviewers(t, prID); len(reviewers) != 1 || !strings.EqualFold(reviewers[0], reviewer) {
		t.Errorf("the draft change dropped the reviewers: want [%s], got %v", reviewer, reviewers)
	}

	pr := extractPRData(decodeJSONMap(t, mustLiveCLI(t, "--json", "pr", "get", prID)))
	if got, _ := pr["description"].(string); got != description {
		t.Errorf("the draft change lost the description: want %q, got %q", description, got)
	}
}
