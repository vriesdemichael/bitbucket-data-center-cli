//go:build live

package live_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
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
func TestLivePullRequestPendingReview(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
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

	// A draft is not visible as a comment until the review is completed, which
	// is the whole point of the pending state.
	listWhilePending, err := executeLiveCLI(t, "--json", "pr", "comment", "list", pullRequestID)
	if err != nil {
		t.Fatalf("pr comment list failed: %v\noutput: %s", err, listWhilePending)
	}
	if strings.Contains(listWhilePending, draftText) {
		t.Fatalf("expected the draft to stay out of the published comments, got: %s", listWhilePending)
	}

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

	// Complete publishes drafts rather than dropping them, which is the
	// difference from discard and the reason both need covering.
	const publishedText = "draft comment that gets published"
	if _, err := executeLiveCLI(t, "--json", "pr", "comment", "add", pullRequestID, "--text", publishedText, "--pending"); err != nil {
		t.Fatalf("second pr comment add --pending failed: %v", err)
	}

	completeOutput, err := executeLiveCLI(t, "--json", "pr", "review", "complete", pullRequestID,
		"--status", "NEEDS_WORK", "--comment", "completing the review from the live suite")
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
}

// TestLivePullRequestCommentReaction covers bb pr comment react in both
// directions.
func TestLivePullRequestCommentReaction(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
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
}

// TestLivePullRequestApplySuggestion covers bb pr comment apply-suggestion.
//
// The suggestion has to sit on an inline comment anchored to a file and line,
// which bb cannot create, so the comment is posted directly. What is under test
// is the applying, and the proof is the file content on the source branch
// afterwards rather than the response body.
func TestLivePullRequestApplySuggestion(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
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

	applyOutput, err := executeLiveCLI(t, "--json", "pr", "comment", "apply-suggestion", pullRequestID, commentID,
		"--commit-message", "apply suggestion from the live suite")
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
}

// TestLivePullRequestCommentResolveReopen covers bb pr comment resolve and
// reopen, which are what replaced marking a pull request task done.
//
// Bitbucket removed pull request tasks in 8.0 and folded them into comments with
// a blocker severity. bb could already create one and list them but not close
// one, so the workflow the removed commands served had no ending.
func TestLivePullRequestCommentResolveReopen(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
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

	// Resolving twice in a row exercises the version being re-read each time.
	// A cached version would make the second call fail with a 409.
	if _, err := executeLiveCLI(t, "--json", "pr", "comment", "resolve", pullRequestID, commentID); err != nil {
		t.Fatalf("second resolve failed: %v", err)
	}
	if _, err := executeLiveCLI(t, "--json", "pr", "comment", "reopen", pullRequestID, commentID); err != nil {
		t.Fatalf("second reopen failed: %v", err)
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
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
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
	addComment("pull request remark", "--text", remarkText)

	inlineText := "this line needs a guard"
	inlineID := addComment("inline remark",
		"--text", inlineText, "--path", reviewedFile, "--line", "1", "--line-type", "ADDED")

	inlineBlockerText := "this line must change before merge"
	inlineBlockerID := addComment("inline blocker",
		"--text", inlineBlockerText, "--blocker", "--path", reviewedFile, "--line", "1", "--line-type", "ADDED")

	prBlockerText := "add a regression test before merging"
	prBlockerID := addComment("pull request blocker", "--text", prBlockerText, "--blocker")

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
	assertTaskCounts("one blocker resolved", 1, 1)

	// 6. Reopening puts it back, so a reviewer who resolved too eagerly is not
	// stuck.
	if output, reopenErr := executeLiveCLI(t, "--json", "pr", "comment", "reopen", pullRequestID, inlineBlockerID); reopenErr != nil {
		t.Fatalf("pr comment reopen failed: %v\noutput: %s", reopenErr, output)
	}
	assertTaskCounts("blocker reopened", 2, 0)

	// 7. With every blocker resolved the task count clears, even though the two
	// ordinary remarks are still open -- a remark is feedback, a blocker is a
	// condition, and conflating them is what makes a gate useless.
	for _, id := range []string{inlineBlockerID, prBlockerID} {
		if output, resolveErr := executeLiveCLI(t, "--json", "pr", "comment", "resolve", pullRequestID, id); resolveErr != nil {
			t.Fatalf("pr comment resolve %s failed: %v\noutput: %s", id, resolveErr, output)
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
}
