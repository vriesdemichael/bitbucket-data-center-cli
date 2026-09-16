//go:build live

package live_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// postLiveJSON sends a raw authenticated POST, for fixtures bb has no command
// for -- setting a repository up as a fork is the remaining case.
//
// It is deliberately not used for anchored comments any more: bb anchors them
// itself now, and a fixture that reaches past the CLI is a fixture that cannot
// notice the CLI is broken.
func postLiveJSON(t *testing.T, path string, payload any) map[string]any {
	t.Helper()

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal fixture payload failed: %v", err)
	}

	request, err := http.NewRequest(http.MethodPost, strings.TrimSuffix(os.Getenv("BITBUCKET_URL"), "/")+path, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build fixture request failed: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.SetBasicAuth(os.Getenv("ADMIN_USER"), os.Getenv("ADMIN_PASSWORD"))

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("fixture request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	responseBody, _ := io.ReadAll(response.Body)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		t.Fatalf("fixture POST %s returned %d: %s", path, response.StatusCode, responseBody)
	}

	var decoded map[string]any
	if len(responseBody) > 0 {
		if err := json.Unmarshal(responseBody, &decoded); err != nil {
			t.Fatalf("decode fixture response failed: %v\nbody: %s", err, responseBody)
		}
	}
	return decoded
}

// TestLivePullRequestPendingReview covers bb pr review get/complete/discard
// together with the pending comments they act on.
//
// A pending review only exists as the sum of its draft comments, so the three
// commands cannot be tested apart: discard has nothing to discard and complete
// has nothing to publish unless a pending comment was added first.
//
// The review is a reviewer's, not the author's. Bitbucket completes an author's
// review with 200 and drops the status it carries, since an author holds none
// (OPENAPI-027), so a --status sent as the author could never be read back.
func TestLivePullRequestPendingReview(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	branch := "feature/pending-review"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "pending-review.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}
	pullRequestID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
	if err != nil {
		t.Fatalf("create pull request failed: %v", err)
	}
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

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
			seeded.Key, repo.Slug, pullRequestID),
		map[string]any{"user": map[string]any{"name": reviewer.Username}, "role": "REVIEWER"}); err != nil {
		t.Fatalf("add the reviewer failed: %v", err)
	}
	pullRequest := prReviewPullRequest(t, pullRequestID)
	prReviewAssertOpened(t, pullRequest, prReviewHarnessTitle, branch)
	prReviewAssertSoleReviewer(t, pullRequest, reviewer.Username, "UNAPPROVED")

	configureLiveCLIEnvForUser(t, harness, seeded.Key, repo.Slug, reviewer)

	// A draft comment is what brings a pending review into existence.
	const draftText = "draft comment from the live suite"
	if _, err := executeLiveCLI(t, "--json", "pr", "comment", "add", pullRequestID, "--text", draftText, "--pending"); err != nil {
		t.Fatalf("pr comment add --pending failed: %v", err)
	}

	pendingOutput, err := executeLiveCLI(t, "--json", "pr", "review", "get", pullRequestID)
	if err != nil {
		t.Fatalf("pr review get failed: %v\noutput: %s", err, pendingOutput)
	}
	if !strings.Contains(pendingOutput, draftText) {
		t.Fatalf("expected the draft comment in the pending review, got: %s", pendingOutput)
	}
	prReviewAssertOnlyDraft(t, pullRequestID, draftText)

	// A draft is not visible as a comment until the review is completed, which
	// is the whole point of the pending state.
	listWhilePending, err := executeLiveCLI(t, "--json", "pr", "comment", "list", pullRequestID)
	if err != nil {
		t.Fatalf("pr comment list failed: %v\noutput: %s", err, listWhilePending)
	}
	if strings.Contains(listWhilePending, draftText) {
		t.Fatalf("expected the draft to stay out of the published comments, got: %s", listWhilePending)
	}
	prReviewAssertNotPublished(t, pullRequestID, draftText)

	if _, err := executeLiveCLI(t, "--json", "pr", "review", "discard", pullRequestID); err != nil {
		t.Fatalf("pr review discard failed: %v", err)
	}

	afterDiscard, err := executeLiveCLI(t, "--json", "pr", "review", "get", pullRequestID)
	if err != nil {
		t.Fatalf("pr review get after discard failed: %v\noutput: %s", err, afterDiscard)
	}
	if strings.Contains(afterDiscard, draftText) {
		t.Fatalf("expected the discarded draft to be gone, got: %s", afterDiscard)
	}
	// Gone from the review is also what publishing looks like from there, so
	// the comments have to be read too.
	if drafts := prReviewDrafts(t, pullRequestID); len(drafts) != 0 {
		t.Fatalf("the review still holds drafts after discard: %v", drafts)
	}
	prReviewAssertNotPublished(t, pullRequestID, draftText)

	// Complete publishes drafts rather than dropping them, which is the
	// difference from discard and the reason both need covering.
	const publishedText = "draft comment that gets published"
	if _, err := executeLiveCLI(t, "--json", "pr", "comment", "add", pullRequestID, "--text", publishedText, "--pending"); err != nil {
		t.Fatalf("second pr comment add --pending failed: %v", err)
	}
	prReviewAssertOnlyDraft(t, pullRequestID, publishedText)

	const completionText = "completing the review from the live suite"
	completeOutput, err := executeLiveCLI(t, "--json", "pr", "review", "complete", pullRequestID,
		"--status", "NEEDS_WORK", "--comment", completionText)
	if err != nil {
		t.Fatalf("pr review complete failed: %v\noutput: %s", err, completeOutput)
	}

	listAfterComplete, err := executeLiveCLI(t, "--json", "pr", "comment", "list", pullRequestID)
	if err != nil {
		t.Fatalf("pr comment list after complete failed: %v\noutput: %s", err, listAfterComplete)
	}
	if !strings.Contains(listAfterComplete, publishedText) {
		t.Fatalf("expected completing the review to publish the draft, got: %s", listAfterComplete)
	}

	prReviewAssertSoleReviewer(t, prReviewPullRequest(t, pullRequestID), reviewer.Username, "NEEDS_WORK")
	published := prReviewPublished(t, pullRequestID)
	for _, text := range []string{publishedText, completionText} {
		if matching := prReviewWithText(published, text); len(matching) != 1 || matching[0]["state"] != "OPEN" || matching[0]["pending"] != false {
			t.Errorf("want one published OPEN comment reading %q, no longer pending, got %v", text, matching)
		}
	}
	if discarded := prReviewWithText(published, draftText); len(discarded) != 0 {
		t.Errorf("completing the review published the draft discarded before it: %v", discarded)
	}
	if drafts := prReviewDrafts(t, pullRequestID); len(drafts) != 0 {
		t.Errorf("the review still holds drafts after complete: %v", drafts)
	}
}

// TestLivePullRequestReviewDryRuns covers the three review previews.
//
// The mocked versions asserted the preview names the right intent, which is a
// question about a string the command wrote. What a preview has to be right
// about is the pull request in front of it, and the half worth checking is that
// nothing happened -- which needs something that would have happened.
func TestLivePullRequestReviewDryRuns(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]

	const branch = "feature/review-dry-runs"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "review-dry-runs.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}
	pullRequestID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
	if err != nil {
		t.Fatalf("create pull request failed: %v", err)
	}
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)
	prReviewAssertOpened(t, prReviewPullRequest(t, pullRequestID), prReviewHarnessTitle, branch)

	const draft = "a draft the dry runs must not touch"
	if _, err := executeLiveCLI(t, "--json", "pr", "comment", "add", pullRequestID, "--text", draft, "--pending"); err != nil {
		t.Fatalf("pr comment add --pending failed: %v", err)
	}
	prReviewAssertOnlyDraft(t, pullRequestID, draft)

	draftIsStillPending := func(t *testing.T) {
		t.Helper()

		output, err := executeLiveCLI(t, "--json", "pr", "review", "get", pullRequestID)
		if err != nil {
			t.Fatalf("pr review get failed: %v\noutput: %s", err, output)
		}
		if !strings.Contains(output, draft) {
			t.Fatalf("the dry run acted on the review: the draft is gone\n%s", output)
		}
		// Still the one draft and still unpublished: completing the review
		// would have moved it out of the review and into the comments.
		prReviewAssertOnlyDraft(t, pullRequestID, draft)
		prReviewAssertNotPublished(t, pullRequestID, draft)
	}

	t.Run("adding a pending comment", func(t *testing.T) {
		output := mustLiveCLI(t, "--dry-run", "pr", "comment", "add", pullRequestID,
			"--text", "predicted only", "--pending")
		if !strings.Contains(output, `"pending": true`) {
			t.Errorf("expected the preview to say the comment would be pending:\n%s", output)
		}

		listing := mustLiveCLI(t, "pr", "review", "get", pullRequestID)
		if strings.Contains(listing, "predicted only") {
			t.Fatalf("the dry run created the comment:\n%s", listing)
		}
		// Nor as a published comment, which the review would not list.
		prReviewAssertOnlyDraft(t, pullRequestID, draft)
		prReviewAssertNotPublished(t, pullRequestID, "predicted only")
	})

	t.Run("completing the review", func(t *testing.T) {
		output := mustLiveCLI(t, "--dry-run", "pr", "review", "complete", pullRequestID, "--status", "NEEDS_WORK")
		if !strings.Contains(output, "pr.review.complete") {
			t.Errorf("expected the preview to name the intent:\n%s", output)
		}

		draftIsStillPending(t)
	})

	t.Run("discarding the review", func(t *testing.T) {
		output := mustLiveCLI(t, "--dry-run", "pr", "review", "discard", pullRequestID)
		if !strings.Contains(output, "pr.review.discard") {
			t.Errorf("expected the preview to name the intent:\n%s", output)
		}

		draftIsStillPending(t)
	})
}

// TestLivePullRequestCommentReaction covers bb pr comment react in both
// directions.
func TestLivePullRequestCommentReaction(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	branch := "feature/comment-reaction"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "comment-reaction.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}
	pullRequestID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
	if err != nil {
		t.Fatalf("create pull request failed: %v", err)
	}
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)
	prReviewAssertOpened(t, prReviewPullRequest(t, pullRequestID), prReviewHarnessTitle, branch)

	addOutput, err := executeLiveCLI(t, "--json", "pr", "comment", "add", pullRequestID,
		"--text", "comment that gets a reaction", "--blocker")
	if err != nil {
		t.Fatalf("pr comment add --blocker failed: %v\noutput: %s", err, addOutput)
	}
	addData := decodeJSONMap(t, addOutput)
	commentObject, ok := addData["comment"].(map[string]any)
	if !ok {
		commentObject = addData
	}
	commentID, ok := numericOrStringID(commentObject["id"])
	if !ok {
		t.Fatalf("expected a comment id in the add output: %s", addOutput)
	}
	if stored := prReviewComment(t, pullRequestID, commentID); stored["text"] != "comment that gets a reaction" || stored["severity"] != "BLOCKER" {
		t.Fatalf("stored comment = text %v, severity %v; want the text as sent and BLOCKER", stored["text"], stored["severity"])
	}

	reactOutput, err := executeLiveCLI(t, "--json", "pr", "comment", "react", pullRequestID, commentID, "thumbsup")
	if err != nil {
		t.Fatalf("pr comment react failed: %v\noutput: %s", err, reactOutput)
	}

	getOutput, err := executeLiveCLI(t, "--json", "pr", "comment", "get", pullRequestID, commentID)
	if err != nil {
		t.Fatalf("pr comment get failed: %v\noutput: %s", err, getOutput)
	}
	if !strings.Contains(getOutput, "thumbsup") {
		t.Fatalf("expected the reaction on the comment, got: %s", getOutput)
	}
	if reactors := prReviewReactors(prReviewCommentIn(t, getOutput), "thumbsup"); !slices.Equal(reactors, []string{harness.username()}) {
		t.Fatalf("thumbsup is held by %v, want [%s]", reactors, harness.username())
	}

	if _, err := executeLiveCLI(t, "--json", "pr", "comment", "react", pullRequestID, commentID, "thumbsup", "--remove"); err != nil {
		t.Fatalf("pr comment react --remove failed: %v", err)
	}

	afterRemove, err := executeLiveCLI(t, "--json", "pr", "comment", "get", pullRequestID, commentID)
	if err != nil {
		t.Fatalf("pr comment get after removing the reaction failed: %v\noutput: %s", err, afterRemove)
	}
	if strings.Contains(afterRemove, "thumbsup") {
		t.Fatalf("expected the reaction to be gone, got: %s", afterRemove)
	}
	if reactors := prReviewReactors(prReviewCommentIn(t, afterRemove), "thumbsup"); len(reactors) != 0 {
		t.Fatalf("thumbsup is still held by %v after --remove", reactors)
	}

	// A reply, and then the listing that has to show it. Bitbucket nests a
	// reply under its root, and the flat model reduced that to a count -- so
	// the reply body reached no bb command at all. Only a real server nests
	// anything, so only a live run proves the flattening reads it back.
	replyText := "a reply that has to survive the flattening"
	replyOutput, err := executeLiveCLI(t, "--json", "pr", "comment", "add", pullRequestID,
		"--text", replyText, "--parent-id", commentID)
	if err != nil {
		t.Fatalf("pr comment add --parent-id failed: %v\noutput: %s", err, replyOutput)
	}

	listOutput, err := executeLiveCLI(t, "--json", "pr", "comment", "list", pullRequestID, "--full", "--state", "all")
	if err != nil {
		t.Fatalf("pr comment list --full failed: %v\noutput: %s", err, listOutput)
	}
	if !strings.Contains(listOutput, replyText) {
		t.Fatalf("the reply body did not reach the ungrouped listing: %s", listOutput)
	}
	if !strings.Contains(listOutput, `"reply": true`) || !strings.Contains(listOutput, `"parentId"`) {
		t.Fatalf("the reply did not say what it answers: %s", listOutput)
	}
	prReviewAssertReplyTo(t, prReviewEntries(t, decodeJSONMap(t, listOutput), "comments"), replyText, commentID)

	humanList, err := executeLiveCLI(t, "pr", "comment", "list", pullRequestID, "--full", "--state", "all")
	if err != nil {
		t.Fatalf("pr comment list --full (human) failed: %v\noutput: %s", err, humanList)
	}
	if !strings.Contains(humanList, replyText) {
		t.Fatalf("the human listing dropped the reply the payload carries: %s", humanList)
	}

	// A second reply, because --with-replies is only distinguishable from the
	// default once a thread has more than one: the default reports a count and
	// the most recent, so with a single reply both forms carry the same text.
	secondReplyText := "a second reply, which only --with-replies should show"
	if _, err := executeLiveCLI(t, "--json", "pr", "comment", "add", pullRequestID,
		"--text", secondReplyText, "--parent-id", commentID); err != nil {
		t.Fatalf("second pr comment add --parent-id failed: %v", err)
	}
	bothReplies := prReviewPublished(t, pullRequestID)
	prReviewAssertReplyTo(t, bothReplies, replyText, commentID)
	prReviewAssertReplyTo(t, bothReplies, secondReplyText, commentID)

	// Asserted on the key rather than on which reply texts appear. The
	// activity timeline emits an activity per comment action, so whether a
	// reply also reaches the thread view as a root of its own is the server's
	// choice, not bb's -- an assertion about text presence would be encoding a
	// guess about that. Whether replies is populated at all is bb's choice,
	// and that is what the flag controls.
	collapsed, err := executeLiveCLI(t, "--json", "pr", "comment", "list", pullRequestID, "--state", "all")
	if err != nil {
		t.Fatalf("pr comment list failed: %v\noutput: %s", err, collapsed)
	}
	if strings.Contains(collapsed, `"replies"`) {
		t.Fatalf("the default listing populated replies without --with-replies: %s", collapsed)
	}
	if !strings.Contains(collapsed, `"lastReply"`) {
		t.Fatalf("a thread with replies reported no most-recent reply: %s", collapsed)
	}

	// One thread, not three. The pull request holds a single root with two
	// replies, and a reply is not a thread -- if the timeline reports each
	// reply as its own activity and bb maps every activity to a thread, then
	// summary.unresolved counts work that does not exist, and an agent reading
	// it sees two outstanding threads that were already answered. Only a real
	// timeline can say whether that happens.
	collapsedSummary, ok := decodeJSONMap(t, collapsed)["summary"].(map[string]any)
	if !ok {
		t.Fatalf("no summary in the thread listing: %s", collapsed)
	}
	if total, _ := collapsedSummary["totalThreads"].(float64); total != 1 {
		t.Fatalf("totalThreads = %v, want 1: a reply was counted as a thread of its own\n%s", collapsedSummary["totalThreads"], collapsed)
	}
	collapsedThreads, _ := prReviewThreadList(t, collapsed)
	if ids := prReviewIDs(collapsedThreads); !slices.Equal(ids, []string{commentID}) {
		t.Fatalf("threads = %v, want only the comment the replies answer, %s", ids, commentID)
	}
	lastReply, _ := collapsedThreads[0]["lastReply"].(map[string]any)
	if collapsedThreads[0]["replyCount"] != float64(2) || lastReply["text"] != secondReplyText {
		t.Errorf("replyCount = %v, lastReply = %v; want 2 and the second reply", collapsedThreads[0]["replyCount"], lastReply)
	}

	withReplies, err := executeLiveCLI(t, "--json", "pr", "comment", "list", pullRequestID, "--state", "all", "--with-replies")
	if err != nil {
		t.Fatalf("pr comment list --with-replies failed: %v\noutput: %s", err, withReplies)
	}
	if !strings.Contains(withReplies, `"replies"`) {
		t.Fatalf("--with-replies did not populate replies: %s", withReplies)
	}
	if !strings.Contains(withReplies, replyText) || !strings.Contains(withReplies, secondReplyText) {
		t.Fatalf("--with-replies did not carry every reply: %s", withReplies)
	}
	expandedThreads, _ := prReviewThreadList(t, withReplies)
	if ids := prReviewIDs(expandedThreads); !slices.Equal(ids, []string{commentID}) {
		t.Fatalf("threads = %v with --with-replies, want only %s", ids, commentID)
	}
	if texts := prReviewTexts(t, expandedThreads[0], "replies"); !slices.Equal(texts, []string{replyText, secondReplyText}) {
		t.Errorf("replies = %q, want both, oldest first", texts)
	}

	// --blocker reads a different endpoint from every other listing above:
	// blocker-comments rather than the activity timeline, which is why the
	// payload names its source. Nothing else exercised that path.
	blockerList, err := executeLiveCLI(t, "--json", "pr", "comment", "list", pullRequestID, "--blocker", "--state", "all")
	if err != nil {
		t.Fatalf("pr comment list --blocker failed: %v\noutput: %s", err, blockerList)
	}
	if !strings.Contains(blockerList, `"source": "blocker_comments"`) {
		t.Fatalf("--blocker did not report which endpoint answered: %s", blockerList)
	}
	if !strings.Contains(blockerList, "comment that gets a reaction") {
		t.Fatalf("the blocker listing dropped the task it was asked for: %s", blockerList)
	}
	blockerThreads, _ := prReviewThreadList(t, blockerList)
	if ids := prReviewIDs(blockerThreads); !slices.Equal(ids, []string{commentID}) || blockerThreads[0]["kind"] != "task" {
		t.Errorf("blocker threads = %v, want only %s as a task", blockerThreads, commentID)
	}
}

// TestLivePullRequestApplySuggestion covers bb pr comment apply-suggestion.
//
// The suggestion has to sit on an inline comment anchored to a file and line,
// which bb cannot create, so the comment is posted directly. What is under test
// is the applying, and the proof is the file content on the source branch
// afterwards rather than the response body.
func TestLivePullRequestApplySuggestion(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	branch := "feature/apply-suggestion"
	const fileName = "apply-suggestion.txt"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, fileName); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}
	pullRequestID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
	if err != nil {
		t.Fatalf("create pull request failed: %v", err)
	}
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)
	prReviewAssertOpened(t, prReviewPullRequest(t, pullRequestID), prReviewHarnessTitle, branch)

	const suggested = "branch=rewritten-by-suggestion"
	suggestionText := "please change this\n\n" + "```suggestion\n" + suggested + "\n```"
	// Through bb rather than a raw POST: a suggestion only exists on an inline
	// comment, and bb can anchor one, so posting it any other way would leave
	// the command that has to produce it untested.
	suggestionOutput, err := executeLiveCLI(t, "--json", "pr", "comment", "add", pullRequestID,
		"--text", suggestionText, "--path", fileName, "--line", "1", "--line-type", "ADDED")
	if err != nil {
		t.Fatalf("pr comment add (suggestion) failed: %v\noutput: %s", err, suggestionOutput)
	}
	suggestionPayload := decodeJSONMap(t, suggestionOutput)
	comment, ok := suggestionPayload["comment"].(map[string]any)
	if !ok {
		comment = suggestionPayload
	}

	commentID, ok := numericOrStringID(comment["id"])
	if !ok {
		t.Fatalf("expected an id on the fixture comment: %v", comment)
	}
	stored := prReviewComment(t, pullRequestID, commentID)
	if stored["text"] != suggestionText {
		t.Fatalf("stored text = %q, want the suggestion as sent", stored["text"])
	}
	prReviewAssertAnchor(t, stored, fileName, 1, "ADDED")

	const commitMessage = "apply suggestion from the live suite"
	applyOutput, err := executeLiveCLI(t, "--json", "pr", "comment", "apply-suggestion", pullRequestID, commentID,
		"--commit-message", commitMessage)
	if err != nil {
		t.Fatalf("pr comment apply-suggestion failed: %v\noutput: %s", err, applyOutput)
	}

	// Applying is a commit on the source branch, so the file has to have changed.
	catOutput, err := executeLiveCLI(t, "repo", "cat", fileName, "--at", branch)
	if err != nil {
		t.Fatalf("repo cat after applying the suggestion failed: %v\noutput: %s", err, catOutput)
	}
	if !strings.Contains(catOutput, suggested) {
		t.Fatalf("expected the suggestion to be applied to %s, got: %s", fileName, catOutput)
	}
	// The whole file, not a line of it: the suggestion replaces line 1, and the
	// file had no other.
	content, _ := decodeJSONMap(t, mustLiveCLI(t, "repo", "cat", fileName, "--at", branch))["content"].(string)
	if lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n"); !slices.Equal(lines, []string{suggested}) {
		t.Errorf("%s now reads %q, want only the suggested line", fileName, lines)
	}

	// The changed file says nothing about the message: a server writing its own
	// would have changed it just the same. The specification names the
	// property the message goes in wrongly (OPENAPI-015), which is why it is
	// read back from the commit.
	commits := prReviewEntries(t, decodeJSONMap(t, mustLiveCLI(t, "pr", "commits", pullRequestID)), "commits")
	if len(commits) == 0 || commits[0]["message"] != commitMessage {
		t.Errorf("the newest commit on the pull request is %v, want one with the message %q", commits, commitMessage)
	}
}

// TestLivePullRequestCommentResolveReopen covers bb pr comment resolve and
// reopen, which are what replaced marking a pull request task done.
//
// Bitbucket removed pull request tasks in 8.0 and folded them into comments with
// a blocker severity. bb could already create one and list them but not close
// one, so the workflow the removed commands served had no ending.
func TestLivePullRequestCommentResolveReopen(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	branch := "feature/resolve-reopen"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "resolve-reopen.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}
	pullRequestID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
	if err != nil {
		t.Fatalf("create pull request failed: %v", err)
	}
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)
	prReviewAssertOpened(t, prReviewPullRequest(t, pullRequestID), prReviewHarnessTitle, branch)

	// A blocker comment is what Bitbucket now calls a task.
	addOutput, err := executeLiveCLI(t, "--json", "pr", "comment", "add", pullRequestID,
		"--text", "blocker to resolve", "--blocker")
	if err != nil {
		t.Fatalf("pr comment add --blocker failed: %v\noutput: %s", err, addOutput)
	}
	addData := decodeJSONMap(t, addOutput)
	commentObject, ok := addData["comment"].(map[string]any)
	if !ok {
		commentObject = addData
	}
	commentID, ok := numericOrStringID(commentObject["id"])
	if !ok {
		t.Fatalf("expected a comment id in the add output: %s", addOutput)
	}
	if stored := prReviewComment(t, pullRequestID, commentID); stored["text"] != "blocker to resolve" || stored["severity"] != "BLOCKER" || stored["state"] != "OPEN" {
		t.Fatalf("stored comment = text %v, severity %v, state %v; want the text as sent, BLOCKER and OPEN",
			stored["text"], stored["severity"], stored["state"])
	}

	// An open blocker counts against the pull request, which is what makes
	// resolving it mean something.
	beforeResolve, err := executeLiveCLI(t, "--json", "pr", "comment", "list", pullRequestID, "--tasks-only")
	if err != nil {
		t.Fatalf("pr comment list --tasks-only failed: %v\noutput: %s", err, beforeResolve)
	}
	if !strings.Contains(beforeResolve, "blocker to resolve") {
		t.Fatalf("expected the blocker in the task listing, got: %s", beforeResolve)
	}

	resolveOutput, err := executeLiveCLI(t, "--json", "pr", "comment", "resolve", pullRequestID, commentID)
	if err != nil {
		t.Fatalf("pr comment resolve failed: %v\noutput: %s", err, resolveOutput)
	}

	// Read back rather than trusting the response: the version handling is the
	// part most likely to be subtly wrong, and a stale version is refused with a
	// 409 rather than silently ignored.
	afterResolve, err := executeLiveCLI(t, "--json", "pr", "comment", "get", pullRequestID, commentID)
	if err != nil {
		t.Fatalf("pr comment get after resolve failed: %v\noutput: %s", err, afterResolve)
	}
	if !strings.Contains(afterResolve, "RESOLVED") {
		t.Fatalf("expected the comment to read as RESOLVED, got: %s", afterResolve)
	}
	if state := prReviewCommentIn(t, afterResolve)["state"]; state != "RESOLVED" {
		t.Fatalf("state = %v after resolve, want RESOLVED", state)
	}

	reopenOutput, err := executeLiveCLI(t, "--json", "pr", "comment", "reopen", pullRequestID, commentID)
	if err != nil {
		t.Fatalf("pr comment reopen failed: %v\noutput: %s", err, reopenOutput)
	}

	afterReopen, err := executeLiveCLI(t, "--json", "pr", "comment", "get", pullRequestID, commentID)
	if err != nil {
		t.Fatalf("pr comment get after reopen failed: %v\noutput: %s", err, afterReopen)
	}
	if !strings.Contains(afterReopen, `"state": "OPEN"`) {
		t.Fatalf("expected the comment to read as OPEN again, got: %s", afterReopen)
	}
	if state := prReviewCommentIn(t, afterReopen)["state"]; state != "OPEN" {
		t.Fatalf("state = %v after reopen, want OPEN", state)
	}

	// Resolving twice in a row exercises the version being re-read each time.
	// A cached version would make the second call fail with a 409.
	if _, err := executeLiveCLI(t, "--json", "pr", "comment", "resolve", pullRequestID, commentID); err != nil {
		t.Fatalf("second resolve failed: %v", err)
	}
	if state := prReviewComment(t, pullRequestID, commentID)["state"]; state != "RESOLVED" {
		t.Fatalf("state = %v after the second resolve, want RESOLVED", state)
	}
	if _, err := executeLiveCLI(t, "--json", "pr", "comment", "reopen", pullRequestID, commentID); err != nil {
		t.Fatalf("second reopen failed: %v", err)
	}
	if state := prReviewComment(t, pullRequestID, commentID)["state"]; state != "OPEN" {
		t.Fatalf("state = %v after the second reopen, want OPEN", state)
	}
}

// TestLivePullRequestBlockerReviewLoop walks the review flow bb exists to
// support, through the CLI rather than the services underneath it.
//
// A reviewer -- a person or an agent -- leaves feedback in three shapes: a
// remark on the pull request, a remark on a line, and a blocker on a line that
// must be dealt with before the merge. Then something reads back whether any of
// it is still outstanding, and resolves what has been addressed.
//
// Every one of those steps had live coverage of its parts and none of the
// whole. The inline comment and the task in the visibility test are created
// through the service, so `bb pr comment add --path --line` and
// `--blocker --path --line` had never run against a server at all -- and an
// inline blocker is the single most useful thing an automated reviewer emits.
func TestLivePullRequestBlockerReviewLoop(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	branch := "feature/blocker-review-loop"
	reviewedFile := "blocker-review-loop.txt"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, reviewedFile); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}
	pullRequestID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
	if err != nil {
		t.Fatalf("create pull request failed: %v", err)
	}
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)
	prReviewAssertOpened(t, prReviewPullRequest(t, pullRequestID), prReviewHarnessTitle, branch)

	addComment := func(what string, args ...string) string {
		t.Helper()
		output, addErr := executeLiveCLI(t, append([]string{"--json", "pr", "comment", "add", pullRequestID}, args...)...)
		if addErr != nil {
			t.Fatalf("pr comment add (%s) failed: %v\noutput: %s", what, addErr, output)
		}
		payload := decodeJSONMap(t, output)
		comment, ok := payload["comment"].(map[string]any)
		if !ok {
			comment = payload
		}
		id, ok := numericOrStringID(comment["id"])
		if !ok {
			t.Fatalf("no comment id in the %s output: %s", what, output)
		}
		return id
	}

	// 1. The three shapes of feedback, all through the CLI.
	remarkText := "a remark on the pull request as a whole"
	remarkID := addComment("pull request remark", "--text", remarkText)

	inlineText := "this line needs a guard"
	inlineID := addComment("inline remark",
		"--text", inlineText, "--path", reviewedFile, "--line", "1", "--line-type", "ADDED")

	inlineBlockerText := "this line must change before merge"
	inlineBlockerID := addComment("inline blocker",
		"--text", inlineBlockerText, "--blocker", "--path", reviewedFile, "--line", "1", "--line-type", "ADDED")

	prBlockerText := "add a regression test before merging"
	prBlockerID := addComment("pull request blocker", "--text", prBlockerText, "--blocker")

	// Each one read back as stored: its text, whether it blocks, and whether
	// and where it is anchored.
	for _, sent := range []struct {
		id, text, severity string
		inline             bool
	}{
		{remarkID, remarkText, "NORMAL", false},
		{inlineID, inlineText, "NORMAL", true},
		{inlineBlockerID, inlineBlockerText, "BLOCKER", true},
		{prBlockerID, prBlockerText, "BLOCKER", false},
	} {
		stored := prReviewComment(t, pullRequestID, sent.id)
		if stored["text"] != sent.text || stored["severity"] != sent.severity {
			t.Errorf("comment %s = text %v, severity %v; want %q, %s", sent.id, stored["text"], stored["severity"], sent.text, sent.severity)
		}
		if sent.inline {
			prReviewAssertAnchor(t, stored, reviewedFile, 1, "ADDED")
		} else if stored["anchor"] != nil {
			t.Errorf("comment %s was posted without an anchor and stored one: %v", sent.id, stored["anchor"])
		}
	}

	// 2. An inline blocker has to keep both facts: that it blocks, and where it
	// points. Losing the anchor makes it unactionable; losing the kind makes it
	// invisible to the gate.
	tasksOnly, err := executeLiveCLI(t, "--json", "pr", "comment", "list", pullRequestID, "--tasks-only", "--state", "all")
	if err != nil {
		t.Fatalf("pr comment list --tasks-only failed: %v\noutput: %s", err, tasksOnly)
	}
	for _, want := range []string{inlineBlockerText, prBlockerText} {
		if !strings.Contains(tasksOnly, want) {
			t.Fatalf("the task listing dropped %q: %s", want, tasksOnly)
		}
	}
	if strings.Contains(tasksOnly, remarkText) || strings.Contains(tasksOnly, inlineText) {
		t.Fatalf("--tasks-only returned an ordinary comment: %s", tasksOnly)
	}
	if !strings.Contains(tasksOnly, `"kind": "task"`) {
		t.Fatalf("a blocker did not report itself as a task: %s", tasksOnly)
	}
	if !strings.Contains(tasksOnly, reviewedFile) {
		t.Fatalf("the inline blocker lost its anchor: %s", tasksOnly)
	}
	// Tied to the thread each fact belongs to: the pull request blocker alone
	// reads as a task, and the inline one alone carries the file name.
	taskThreads, _ := prReviewThreadList(t, tasksOnly)
	if ids := prReviewIDs(taskThreads); !slices.Equal(ids, prReviewSorted(inlineBlockerID, prBlockerID)) {
		t.Fatalf("task threads = %v, want exactly the two blockers %s and %s", ids, inlineBlockerID, prBlockerID)
	}
	for _, thread := range taskThreads {
		id, _ := numericOrStringID(thread["id"])
		anchor, anchored := thread["anchor"].(map[string]any)
		switch {
		case thread["kind"] != "task":
			t.Errorf("blocker %s is listed as %v, want task", id, thread["kind"])
		case id == inlineBlockerID && (!anchored || anchor["path"] != reviewedFile || anchor["line"] != float64(1)):
			t.Errorf("the inline blocker is anchored at %v, want %s line 1", thread["anchor"], reviewedFile)
		case id == prBlockerID && anchored:
			t.Errorf("the pull request blocker is anchored at %v, want no anchor", anchor)
		}
	}

	// 3. The blocker-comments endpoint is a different source from the timeline
	// and must agree with it about what blocks.
	blockerList, err := executeLiveCLI(t, "--json", "pr", "comment", "list", pullRequestID, "--blocker", "--state", "all")
	if err != nil {
		t.Fatalf("pr comment list --blocker failed: %v\noutput: %s", err, blockerList)
	}
	if !strings.Contains(blockerList, `"source": "blocker_comments"`) {
		t.Fatalf("--blocker did not report which endpoint answered: %s", blockerList)
	}
	for _, want := range []string{inlineBlockerText, prBlockerText} {
		if !strings.Contains(blockerList, want) {
			t.Fatalf("the blocker endpoint dropped %q: %s", want, blockerList)
		}
	}
	if blockerThreads, _ := prReviewThreadList(t, blockerList); !slices.Equal(prReviewIDs(blockerThreads), prReviewSorted(inlineBlockerID, prBlockerID)) {
		t.Fatalf("blocker threads = %v, want exactly %s and %s", prReviewIDs(blockerThreads), inlineBlockerID, prBlockerID)
	}

	// 4. The gate. This is what an agent reads to decide whether the pull
	// request is done, so the counts have to move when the work does.
	assertTaskCounts := func(stage string, wantOpen, wantResolved float64) {
		t.Helper()
		output, getErr := executeLiveCLI(t, "--json", "pr", "get", pullRequestID)
		if getErr != nil {
			t.Fatalf("pr get (%s) failed: %v\noutput: %s", stage, getErr, output)
		}
		summary, ok := decodeJSONMap(t, output)["reviewSummary"].(map[string]any)
		if !ok {
			t.Fatalf("no reviewSummary at %s: %s", stage, output)
		}
		if summary["openTasks"] != wantOpen {
			t.Errorf("%s: openTasks = %v, want %v\n%#v", stage, summary["openTasks"], wantOpen, summary)
		}
		if summary["resolvedTasks"] != wantResolved {
			t.Errorf("%s: resolvedTasks = %v, want %v\n%#v", stage, summary["resolvedTasks"], wantResolved, summary)
		}
		// actionRequired stays true at every stage here, the last one included:
		// the two ordinary remarks are still open. That is the distinction
		// worth pinning -- clearing every blocker does not mean there is
		// nothing left to read, and a gate that said otherwise would wave
		// through a pull request with unanswered review feedback on it.
		if summary["actionRequired"] != true {
			t.Errorf("%s: actionRequired = %v, want true while any feedback is open\n%#v", stage, summary["actionRequired"], summary)
		}
	}
	assertTaskCounts("two blockers open", 2, 0)

	// 5. Resolving one moves the gate by exactly one, and the resolved blocker
	// keeps its anchor -- a reviewer coming back needs to see what was fixed
	// and where.
	if output, resolveErr := executeLiveCLI(t, "--json", "pr", "comment", "resolve", pullRequestID, inlineBlockerID); resolveErr != nil {
		t.Fatalf("pr comment resolve failed: %v\noutput: %s", resolveErr, output)
	}
	resolved, err := executeLiveCLI(t, "--json", "pr", "comment", "get", pullRequestID, inlineBlockerID)
	if err != nil {
		t.Fatalf("pr comment get after resolve failed: %v\noutput: %s", err, resolved)
	}
	if !strings.Contains(resolved, "RESOLVED") {
		t.Fatalf("the blocker did not read as resolved: %s", resolved)
	}
	if !strings.Contains(resolved, reviewedFile) {
		t.Fatalf("resolving the blocker lost its anchor: %s", resolved)
	}
	resolvedComment := prReviewCommentIn(t, resolved)
	if resolvedComment["state"] != "RESOLVED" {
		t.Fatalf("state = %v after resolve, want RESOLVED", resolvedComment["state"])
	}
	prReviewAssertAnchor(t, resolvedComment, reviewedFile, 1, "ADDED")
	assertTaskCounts("one blocker resolved", 1, 1)

	// 6. Reopening puts it back, so a reviewer who resolved too eagerly is not
	// stuck.
	if output, reopenErr := executeLiveCLI(t, "--json", "pr", "comment", "reopen", pullRequestID, inlineBlockerID); reopenErr != nil {
		t.Fatalf("pr comment reopen failed: %v\noutput: %s", reopenErr, output)
	}
	if state := prReviewComment(t, pullRequestID, inlineBlockerID)["state"]; state != "OPEN" {
		t.Errorf("state = %v after reopen, want OPEN", state)
	}
	assertTaskCounts("blocker reopened", 2, 0)

	// 7. With every blocker resolved the task count clears, even though the two
	// ordinary remarks are still open -- a remark is feedback, a blocker is a
	// condition, and conflating them is what makes a gate useless.
	for _, id := range []string{inlineBlockerID, prBlockerID} {
		if output, resolveErr := executeLiveCLI(t, "--json", "pr", "comment", "resolve", pullRequestID, id); resolveErr != nil {
			t.Fatalf("pr comment resolve %s failed: %v\noutput: %s", id, resolveErr, output)
		}
		if state := prReviewComment(t, pullRequestID, id)["state"]; state != "RESOLVED" {
			t.Errorf("blocker %s state = %v after resolve, want RESOLVED", id, state)
		}
	}
	assertTaskCounts("every blocker resolved", 0, 2)

	// 8. And the ordinary comments are untouched by any of it.
	remaining, err := executeLiveCLI(t, "--json", "pr", "comment", "list", pullRequestID, "--unresolved")
	if err != nil {
		t.Fatalf("pr comment list --unresolved failed: %v\noutput: %s", err, remaining)
	}
	if !strings.Contains(remaining, inlineText) || !strings.Contains(remaining, remarkText) {
		t.Fatalf("resolving the blockers disturbed the ordinary comments: %s", remaining)
	}
	if strings.Contains(remaining, inlineBlockerText) || strings.Contains(remaining, prBlockerText) {
		t.Fatalf("a resolved blocker is still listed as unresolved: %s", remaining)
	}
	unresolvedThreads, _ := prReviewThreadList(t, remaining)
	if ids := prReviewIDs(unresolvedThreads); !slices.Equal(ids, prReviewSorted(remarkID, inlineID)) {
		t.Fatalf("unresolved threads = %v, want exactly the remarks %s and %s", ids, remarkID, inlineID)
	}
	for _, thread := range unresolvedThreads {
		if id, _ := numericOrStringID(thread["id"]); thread["kind"] != "comment" ||
			(id == remarkID && thread["text"] != remarkText) || (id == inlineID && thread["text"] != inlineText) {
			t.Errorf("unresolved thread %s = kind %v, text %v; want the remark as posted", id, thread["kind"], thread["text"])
		}
	}

	// 9. A blocker cannot be a reply, and bb says so itself rather than letting
	// the server answer with something less specific.
	replyBlocker, err := executeLiveCLI(t, "--json", "pr", "comment", "add", pullRequestID,
		"--text", "a blocker that replies", "--blocker", "--parent-id", inlineID)
	if err == nil {
		t.Fatalf("a blocker reply was accepted: %s", replyBlocker)
	}
	// bb refuses this itself, so the message names the flags rather than
	// whatever the server would have said about a payload it never received.
	refusal := err.Error()
	if !strings.Contains(refusal, "parent-id") || !strings.Contains(refusal, "blocker") {
		t.Fatalf("the refusal did not name the two flags that conflict: %v\noutput: %s", err, replyBlocker)
	}
	if kind := apperrors.KindOf(err); kind != apperrors.KindValidation {
		t.Errorf("refusal kind = %v, want validation so a caller can branch on it", kind)
	}
	prReviewAssertNotPublished(t, pullRequestID, "a blocker that replies")
}

// The helpers below read what a review wrote back through bb's own read
// commands, so each assertion compares a stored field with the value that was
// sent rather than searching an output for it. They rely on the repository
// context and the credentials the calling test registered.

// prReviewPullRequest reads a pull request back through `pr get`.
func prReviewPullRequest(t *testing.T, prID string) map[string]any {
	t.Helper()

	return extractPRData(decodeJSONMap(t, mustLiveCLI(t, "pr", "get", prID)))
}

// prReviewHarnessTitle is the title harness.createPullRequest gives every pull
// request it opens.
const prReviewHarnessTitle = "Live test PR"

// prReviewAssertOpened fails unless a pull request was stored with the title and
// source branch it was opened with, into master.
func prReviewAssertOpened(t *testing.T, pullRequest map[string]any, title, sourceBranch string) {
	t.Helper()

	if pullRequest["title"] != title || pullRequest["sourceBranch"] != sourceBranch || pullRequest["targetBranch"] != "master" {
		t.Fatalf("pull request = title %v, %v -> %v; want %q, %s -> master",
			pullRequest["title"], pullRequest["sourceBranch"], pullRequest["targetBranch"], title, sourceBranch)
	}
}

// prReviewAssertSoleReviewer fails unless the pull request names exactly one
// reviewer, holding the REVIEWER role and the status given. bb lists a
// PARTICIPANT among the reviewers too, and setting a status adds one, so the
// role is what shows the reviewer was stored as asked.
func prReviewAssertSoleReviewer(t *testing.T, pullRequest map[string]any, username, status string) {
	t.Helper()

	reviewers, _ := pullRequest["reviewers"].([]any)
	if len(reviewers) != 1 {
		t.Fatalf("reviewers = %v, want only %s", pullRequest["reviewers"], username)
	}
	reviewer, _ := reviewers[0].(map[string]any)
	if reviewer["name"] != username || reviewer["role"] != "REVIEWER" || reviewer["status"] != status {
		t.Fatalf("reviewer = %v, want %s as REVIEWER holding %s", reviewer, username, status)
	}
}

// prReviewAssertRepoPermission fails unless the repository grants the user
// exactly the permission given.
func prReviewAssertRepoPermission(t *testing.T, username, permission string) {
	t.Helper()

	entries := prReviewEntries(t, decodeJSONMap(t, mustLiveCLI(t, "repo", "permissions", "list", "--all")), "entries")
	for _, entry := range entries {
		if entry["name"] != username {
			continue
		}
		if entry["permission"] != permission {
			t.Fatalf("%s holds %v on the repository, want %s", username, entry["permission"], permission)
		}

		return
	}
	t.Fatalf("%s holds no permission on the repository: %v", username, entries)
}

// prReviewDrafts reads the caller's draft review through `pr review get`, the
// one read that lists a comment nobody else can see yet.
func prReviewDrafts(t *testing.T, prID string) []map[string]any {
	t.Helper()

	return prReviewEntries(t, decodeJSONMap(t, mustLiveCLI(t, "pr", "review", "get", prID)), "comments")
}

// prReviewPublished reads every published comment on a pull request, replies
// included, through the ungrouped listing.
func prReviewPublished(t *testing.T, prID string) []map[string]any {
	t.Helper()

	return prReviewEntries(t, decodeJSONMap(t, mustLiveCLI(t, "pr", "comment", "list", prID, "--full", "--state", "all")), "comments")
}

// prReviewAssertOnlyDraft fails unless the caller's review holds exactly one
// draft, reading text, still in the PENDING state and reported as pending.
func prReviewAssertOnlyDraft(t *testing.T, prID, text string) {
	t.Helper()

	drafts := prReviewDrafts(t, prID)
	if len(drafts) != 1 || drafts[0]["text"] != text || drafts[0]["state"] != "PENDING" || drafts[0]["pending"] != true {
		t.Fatalf("drafts = %v, want only %q, pending and in the PENDING state", drafts, text)
	}
}

// prReviewAssertNotPublished fails when any published comment reads text.
func prReviewAssertNotPublished(t *testing.T, prID, text string) {
	t.Helper()

	if published := prReviewWithText(prReviewPublished(t, prID), text); len(published) != 0 {
		t.Fatalf("a comment reading %q was published: %v", text, published)
	}
}

// prReviewComment reads one comment back through `pr comment get`.
func prReviewComment(t *testing.T, prID, commentID string) map[string]any {
	t.Helper()

	return prReviewCommentIn(t, mustLiveCLI(t, "pr", "comment", "get", prID, commentID))
}

// prReviewCommentIn takes the comment out of a `pr comment get` output.
func prReviewCommentIn(t *testing.T, output string) map[string]any {
	t.Helper()

	comment, ok := decodeJSONMap(t, output)["comment"].(map[string]any)
	if !ok {
		t.Fatalf("no comment object in the output: %s", output)
	}

	return comment
}

// prReviewAssertAnchor fails unless a stored comment is anchored to the file,
// line and side of the diff given, and is reported as anchored.
func prReviewAssertAnchor(t *testing.T, comment map[string]any, path string, line int, lineType string) {
	t.Helper()

	anchor, _ := comment["anchor"].(map[string]any)
	if anchor["path"] != path || anchor["line"] != float64(line) || anchor["lineType"] != lineType || comment["anchored"] != true {
		t.Errorf("comment %v is anchored at %v, anchored = %v; want %s line %d (%s), anchored",
			comment["id"], comment["anchor"], comment["anchored"], path, line, lineType)
	}
}

// prReviewAssertReplyTo fails unless exactly one of the comments reads text,
// and it is stored as a reply to parentID.
func prReviewAssertReplyTo(t *testing.T, comments []map[string]any, text, parentID string) {
	t.Helper()

	replies := prReviewWithText(comments, text)
	if len(replies) != 1 {
		t.Fatalf("want one comment reading %q, got %v", text, replies)
	}
	if parent, _ := numericOrStringID(replies[0]["parentId"]); replies[0]["reply"] != true || parent != parentID {
		t.Errorf("%q = reply %v, parentId %v; want a reply to %s", text, replies[0]["reply"], replies[0]["parentId"], parentID)
	}
}

// prReviewThreadList decodes a `pr comment list` output into its threads and
// its summary.
func prReviewThreadList(t *testing.T, output string) ([]map[string]any, map[string]any) {
	t.Helper()

	data := decodeJSONMap(t, output)
	summary, ok := data["summary"].(map[string]any)
	if !ok {
		t.Fatalf("no summary in the comment listing: %s", output)
	}

	return prReviewEntries(t, data, "threads"), summary
}

// prReviewEntries reads the list of objects under key.
func prReviewEntries(t *testing.T, data map[string]any, key string) []map[string]any {
	t.Helper()

	values, ok := data[key].([]any)
	if !ok {
		t.Fatalf("no %s list in %v", key, data)
	}
	entries := make([]map[string]any, 0, len(values))
	for _, value := range values {
		entry, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("%s holds %v, which is not an object", key, value)
		}
		entries = append(entries, entry)
	}

	return entries
}

// prReviewTexts lists the text of each object under key, in order.
func prReviewTexts(t *testing.T, data map[string]any, key string) []string {
	t.Helper()

	entries := prReviewEntries(t, data, key)
	texts := make([]string, 0, len(entries))
	for _, entry := range entries {
		text, _ := entry["text"].(string)
		texts = append(texts, text)
	}

	return texts
}

// prReviewWithText keeps the entries whose text is exactly text.
func prReviewWithText(entries []map[string]any, text string) []map[string]any {
	var matching []map[string]any
	for _, entry := range entries {
		if entry["text"] == text {
			matching = append(matching, entry)
		}
	}

	return matching
}

// prReviewIDs lists the ids of comments or threads, sorted, so a listing can be
// compared with exactly what it should hold.
func prReviewIDs(entries []map[string]any) []string {
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		id, _ := numericOrStringID(entry["id"])
		ids = append(ids, id)
	}
	slices.Sort(ids)

	return ids
}

// prReviewSorted is the ids given, sorted the way prReviewIDs sorts them.
func prReviewSorted(ids ...string) []string {
	sorted := slices.Clone(ids)
	slices.Sort(sorted)

	return sorted
}

// prReviewReactors names the users holding a reaction on a comment. Bitbucket
// keeps reactions under the comment's undocumented properties, one entry per
// emoticon with the users who chose it.
func prReviewReactors(comment map[string]any, shortcut string) []string {
	properties, _ := comment["properties"].(map[string]any)
	reactions, _ := properties["reactions"].([]any)

	var names []string
	for _, value := range reactions {
		reaction, _ := value.(map[string]any)
		if emoticon, _ := reaction["emoticon"].(map[string]any); emoticon["shortcut"] != shortcut {
			continue
		}
		users, _ := reaction["users"].([]any)
		for _, entry := range users {
			user, _ := entry.(map[string]any)
			name, _ := user["name"].(string)
			names = append(names, name)
		}
	}

	return names
}
