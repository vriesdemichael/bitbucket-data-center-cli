//go:build live

package live_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// noDraftReviewException is Bitbucket's answer to finishing a review the
// caller never started.
const noDraftReviewException = "com.atlassian.bitbucket.pull.NoSuchPullRequestReviewException"

// TestLivePullRequestReviewCompleteWithoutDraft covers `bb pr review complete`
// for a caller with no draft review, which is where anyone lands who posted
// their comments directly (#621).
//
// Bitbucket only finishes a review that was started, so a status passed with
// --status is never set. The command still fails. What is under test is that
// the failure, and the preview before it, name the command that does set the
// status, and that the command they name works.
//
// It needs a second licensed account for the same reason the review set test
// does: Bitbucket refuses the author's own review status.
func TestLivePullRequestReviewCompleteWithoutDraft(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	branch := testsupport.UniqueName("lt-review-complete-")
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "review-complete.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}
	prID := createLivePRForRegression(t, branch, "Review complete without a draft", "--no-default-reviewers", "--no-codeowners")

	reviewer, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create reviewer failed: %v", err)
	}
	if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, reviewer.Username, "REPO_WRITE"); err != nil {
		t.Fatalf("grant the reviewer write access failed: %v", err)
	}
	prReviewAssertRepoPermission(t, reviewer.Username, "REPO_WRITE")
	if _, err := harness.liveJSON(ctx, http.MethodPost,
		fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/pull-requests/%s/participants",
			seeded.Key, repo.Slug, prID),
		map[string]any{"user": map[string]any{"name": reviewer.Username}, "role": "REVIEWER"}); err != nil {
		t.Fatalf("add the reviewer failed: %v", err)
	}
	pullRequest := prReviewPullRequest(t, prID)
	prReviewAssertOpened(t, pullRequest, "Review complete without a draft", branch)
	prReviewAssertSoleReviewer(t, pullRequest, reviewer.Username, "UNAPPROVED")

	configureLiveCLIEnvForUser(t, harness, seeded.Key, repo.Slug, reviewer)

	held := func(t *testing.T) string {
		t.Helper()

		data := extractPRData(decodeJSONMap(t, mustLiveCLI(t, "pr", "get", prID, "--repo", repoRef)))
		reviewers, _ := data["reviewers"].([]any)
		for _, value := range reviewers {
			entry, _ := value.(map[string]any)
			if name, _ := entry["name"].(string); name == reviewer.Username {
				status, _ := entry["status"].(string)

				return status
			}
		}

		return ""
	}

	// Posted straight onto the pull request, the way the skill had agents post
	// their findings: no draft review comes into existence.
	mustLiveCLI(t, "pr", "comment", "add", prID, "--text", "the unit tests fail", "--repo", repoRef)
	if published := prReviewPublished(t, prID); len(published) != 1 || published[0]["text"] != "the unit tests fail" || published[0]["state"] != "OPEN" {
		t.Fatalf("published comments = %v, want only the one posted, OPEN", published)
	}
	if drafts := prReviewDrafts(t, prID); len(drafts) != 0 {
		t.Fatalf("posting a comment started a draft review: %v", drafts)
	}

	setCommand := fmt.Sprintf("bb pr review set %s NEEDS_WORK --repo %s", prID, repoRef)

	t.Run("a dry run predicts the failure and names the command that sets the status", func(t *testing.T) {
		output := mustLiveCLI(t, "--dry-run", "pr", "review", "complete", prID, "--status", "NEEDS_WORK", "--repo", repoRef)
		assertLivePreview(t, output, "blocked")
		if !strings.Contains(output, setCommand) {
			t.Errorf("expected the preview to name %q:\n%s", setCommand, output)
		}

		if status := held(t); status != "UNAPPROVED" {
			t.Errorf("the dry run changed the status to %q", status)
		}
		if drafts := prReviewDrafts(t, prID); len(drafts) != 0 {
			t.Errorf("the dry run started a draft review: %v", drafts)
		}
	})

	t.Run("--status fails naming the command that sets the status", func(t *testing.T) {
		output, err := executeLiveCLI(t, "--json", "pr", "review", "complete", prID, "--status", "NEEDS_WORK", "--repo", repoRef)
		if err == nil {
			t.Fatalf("completing a review that was never started succeeded:\n%s", output)
		}
		if !strings.Contains(err.Error(), setCommand) {
			t.Errorf("the failure does not name %q:\n%v", setCommand, err)
		}
		if kind := apperrors.KindOf(err); kind != apperrors.KindNotFound {
			t.Errorf("kind = %v, want not_found", kind)
		}
		if exception := apperrors.DetailsOf(err)["upstreamException"]; exception != noDraftReviewException {
			t.Errorf("upstreamException = %q, want %q", exception, noDraftReviewException)
		}
		status := held(t)
		if status == "NEEDS_WORK" {
			t.Fatalf("the status was set although the command failed")
		}
		if status != "UNAPPROVED" {
			t.Fatalf("status = %q after the refused completion, want UNAPPROVED", status)
		}
	})

	t.Run("--comment fails naming the command that posts it", func(t *testing.T) {
		output, err := executeLiveCLI(t, "--json", "pr", "review", "complete", prID, "--comment", "please add tests", "--repo", repoRef)
		if err == nil {
			t.Fatalf("completing a review that was never started succeeded:\n%s", output)
		}
		want := fmt.Sprintf("bb pr comment add %s --text 'please add tests' --repo %s", prID, repoRef)
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the failure does not name %q:\n%v", want, err)
		}
		if strings.Contains(err.Error(), "bb pr review set") {
			t.Errorf("no status was asked for, yet the failure suggests setting one:\n%v", err)
		}
		prReviewAssertNotPublished(t, prID, "please add tests")
	})

	t.Run("with neither flag the failure says there is no draft review", func(t *testing.T) {
		output, err := executeLiveCLI(t, "--json", "pr", "review", "complete", prID, "--repo", repoRef)
		if err == nil {
			t.Fatalf("completing a review that was never started succeeded:\n%s", output)
		}
		if !strings.Contains(err.Error(), "no draft review") {
			t.Errorf("the failure does not say there is no draft review:\n%v", err)
		}
		if kind := apperrors.KindOf(err); kind != apperrors.KindNotFound {
			t.Errorf("kind = %v, want not_found", kind)
		}
	})

	t.Run("the command the failure names sets the status", func(t *testing.T) {
		mustLiveCLI(t, "pr", "review", "set", prID, "NEEDS_WORK", "--repo", repoRef)

		if status := held(t); status != "NEEDS_WORK" {
			t.Fatalf("status = %q, want NEEDS_WORK", status)
		}
	})

	t.Run("with a draft review the preview predicts the update and complete sets the status", func(t *testing.T) {
		mustLiveCLI(t, "pr", "comment", "add", prID, "--text", "a draft to publish", "--pending", "--repo", repoRef)
		prReviewAssertOnlyDraft(t, prID, "a draft to publish")

		output := mustLiveCLI(t, "--dry-run", "pr", "review", "complete", prID, "--status", "APPROVED", "--repo", repoRef)
		assertLivePreview(t, output, "update")
		if status := held(t); status != "NEEDS_WORK" {
			t.Fatalf("the dry run changed the status to %q", status)
		}
		prReviewAssertOnlyDraft(t, prID, "a draft to publish")
		prReviewAssertNotPublished(t, prID, "a draft to publish")

		mustLiveCLI(t, "pr", "review", "complete", prID, "--status", "APPROVED", "--repo", repoRef)
		if status := held(t); status != "APPROVED" {
			t.Fatalf("status = %q, want APPROVED", status)
		}
		if published := prReviewWithText(prReviewPublished(t, prID), "a draft to publish"); len(published) != 1 || published[0]["state"] != "OPEN" {
			t.Errorf("want the draft published once, OPEN; got %v", published)
		}
		if drafts := prReviewDrafts(t, prID); len(drafts) != 0 {
			t.Errorf("the review still holds drafts after complete: %v", drafts)
		}
	})
}
