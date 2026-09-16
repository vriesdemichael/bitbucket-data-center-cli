//go:build live

package live_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

func TestLiveCLIRepoListAndComments(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 2)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	// A second project, for the project filter to leave out. A listing that only
	// has to include this test's repository passes just as well when the filter
	// is dropped and the whole instance comes back.
	other, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed the project the filter must exclude failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// Unscoped repo list is instance-wide, and an instance has as many
	// repositories as it has. Asking whether this test's own repository is in
	// the answer was a question about how big the instance is: at 50 rows the
	// rest of the parallel suite filled it, and at 1000 the fixtures a leaking
	// cleanup had left behind did. What the unscoped form owes is that it
	// answers with repositories at all; that this repository is among them is
	// the project-scoped question below.
	repoListOutput, err := executeLiveCLI(t, "--json", "repo", "list", "--limit", "50")
	if err != nil {
		t.Fatalf("repo list failed: %v\noutput: %s", err, repoListOutput)
	}
	if !jsonArrayHasEntries(t, repoListOutput) {
		t.Fatalf("the instance-wide repo list came back empty: %s", repoListOutput)
	}
	// The cap holds whatever the instance holds, which is the part of --limit
	// the unscoped answer can be held to.
	if listed := repoCLIArray(t, repoListOutput); len(listed) > 50 {
		t.Fatalf("repo list --limit 50 answered with %d repositories", len(listed))
	}

	// A cap below what the instance holds shows --limit reaches the listing at
	// all: the two projects above are two repositories, and the default of 25
	// would answer with both.
	if limited := repoCLIArray(t, mustLiveCLI(t, "repo", "list", "--limit", "1")); len(limited) != 1 {
		t.Fatalf("repo list --limit 1 answered with %d repositories: %v", len(limited), limited)
	}

	projectRepoListOutput, err := executeLiveCLI(t, "--json", "repo", "list", "--project", seeded.Key, "--limit", "50")
	if err != nil {
		t.Fatalf("repo list with project filter failed: %v\noutput: %s", err, projectRepoListOutput)
	}
	if !jsonArrayContainsSlug(t, projectRepoListOutput, repo.Slug) {
		t.Fatalf("expected repo slug %s in project-filtered repo list output: %s", repo.Slug, projectRepoListOutput)
	}
	// The project's one repository and nothing else. The limit is above what
	// the project holds, so the filter is what this listing proves.
	projectRepos := repoCLIArray(t, projectRepoListOutput)
	if len(projectRepos) != 1 || projectRepos[0]["projectKey"] != seeded.Key || projectRepos[0]["slug"] != repo.Slug {
		t.Fatalf("want exactly %s/%s from the project filter, got %v", seeded.Key, repo.Slug, projectRepos)
	}
	if jsonArrayContainsSlug(t, projectRepoListOutput, other.Repos[0].Slug) {
		t.Fatalf("the project filter let %s/%s through: %s", other.Key, other.Repos[0].Slug, projectRepoListOutput)
	}

	commitID := repo.CommitIDs[0]
	// Anchored to seed.txt, because Bitbucket refuses to retrieve comments
	// without a path -- a commit comment anchored to nothing can be created and
	// then never read back by any listing.
	createCommitOutput, err := executeLiveCLI(t, "--json", "repo", "comment", "create", "--commit", commitID,
		"--text", "live cli commit comment", "--path", "seed.txt", "--line", "1", "--line-type", "CONTEXT")
	if err != nil {
		t.Fatalf("repo comment create (commit) failed: %v\noutput: %s", err, createCommitOutput)
	}
	commitCommentID, ok := commentIDFromCreateOutput(createCommitOutput)
	if !ok {
		t.Fatalf("expected comment id in commit create output: %s", createCommitOutput)
	}
	commitCommentVersion, ok := commentVersionFromCreateOutput(createCommitOutput)
	if !ok {
		t.Fatalf("expected version in commit create output: %s", createCommitOutput)
	}

	// The round trip bb could not make. A commit has no thread view, so a reply
	// to a commit comment reached no bb command at all until the listing was
	// flattened. A reply inherits the anchor of what it answers rather than
	// carrying one, which is why it takes no --path here and is still listable.
	commitReplyText := "a reply on a commit, which has no thread view to reach it"
	replyOnCommitOutput, err := executeLiveCLI(t, "--json", "repo", "comment", "create",
		"--commit", commitID, "--text", commitReplyText, "--parent", commitCommentID)
	if err != nil {
		t.Fatalf("repo comment create --parent failed: %v\noutput: %s", err, replyOnCommitOutput)
	}
	replyCommentID, ok := commentIDFromCreateOutput(replyOnCommitOutput)
	if !ok {
		t.Fatalf("expected comment id in reply create output: %s", replyOnCommitOutput)
	}

	listCommitOutput, err := executeLiveCLI(t, "--json", "repo", "comment", "list", "--commit", commitID, "--path", "seed.txt", "--limit", "25")
	if err != nil {
		t.Fatalf("repo comment list (commit) failed: %v\noutput: %s", err, listCommitOutput)
	}
	if !jsonObjectHasCommentsArray(t, listCommitOutput) {
		t.Fatalf("expected comments array in commit list output: %s", listCommitOutput)
	}
	if !strings.Contains(listCommitOutput, "live cli commit comment") {
		t.Fatalf("the listing did not carry the anchored comment bb created: %s", listCommitOutput)
	}
	if !strings.Contains(listCommitOutput, commitReplyText) {
		t.Fatalf("the reply body did not reach the commit listing: %s", listCommitOutput)
	}
	if !strings.Contains(listCommitOutput, `"reply": true`) || !strings.Contains(listCommitOutput, `"parentId"`) {
		t.Fatalf("the reply did not say what it answers: %s", listCommitOutput)
	}

	// The substrings above say the text reached the listing somewhere. The line
	// is what --line and --line-type sent, and the parent is what --parent sent:
	// a comment stored without them is a comment on no line, or a new thread.
	listedCommitComments := repoCLIComments(t, listCommitOutput)
	commitComment := repoCLIEntryWithID(t, listedCommitComments, commitCommentID)
	if commitComment["text"] != "live cli commit comment" {
		t.Errorf("commit comment text = %v, want %q", commitComment["text"], "live cli commit comment")
	}
	repoCLIAssertAnchor(t, commitComment, "seed.txt", 1, "CONTEXT")
	commitReply := repoCLIEntryWithID(t, listedCommitComments, replyCommentID)
	if commitReply["text"] != commitReplyText || commitReply["reply"] != true {
		t.Errorf("reply %s = text %v, reply %v; want %q, true", replyCommentID, commitReply["text"], commitReply["reply"], commitReplyText)
	}
	if parent, _ := numericOrStringID(commitReply["parentId"]); parent != commitCommentID {
		t.Errorf("reply %s answers %q, want %s", replyCommentID, parent, commitCommentID)
	}

	humanCommitOutput, err := executeLiveCLI(t, "repo", "comment", "list", "--commit", commitID, "--path", "seed.txt", "--limit", "25")
	if err != nil {
		t.Fatalf("repo comment list (commit, human) failed: %v\noutput: %s", err, humanCommitOutput)
	}
	if !strings.Contains(humanCommitOutput, commitReplyText) {
		t.Fatalf("the human commit listing dropped the reply the payload carries: %s", humanCommitOutput)
	}

	humanListCommitOutput, err := executeLiveCLI(t, "repo", "comment", "list", "--commit", commitID, "--path", "seed.txt", "--limit", "25")
	if err != nil {
		t.Fatalf("repo comment list (commit human) failed: %v\noutput: %s", err, humanListCommitOutput)
	}
	if !strings.Contains(humanListCommitOutput, "No comments found") && !strings.Contains(humanListCommitOutput, "[") {
		t.Fatalf("expected human comment list output, got: %s", humanListCommitOutput)
	}
	// The empty-listing notice satisfied the check above too, on a file that
	// holds a comment.
	if strings.Contains(humanListCommitOutput, "No comments found") || !strings.Contains(humanListCommitOutput, "live cli commit comment") {
		t.Fatalf("the human listing did not show the comment on seed.txt: %s", humanListCommitOutput)
	}

	// A version the comment is not at has to be refused as out of date. bb
	// resolves a missing version itself, so an update whose --version never
	// reached Bitbucket succeeds whatever number it named.
	staleUpdateOutput, err := executeLiveCLI(t, "--json", "repo", "comment", "update", "--commit", commitID, "--id", commitCommentID,
		"--text", "an update at a version the comment is not at", "--version", repoCLIVersionAfter(t, commitCommentVersion))
	repoCLIAssertOutOfDate(t, staleUpdateOutput, err)

	updateCommitArgs := []string{"--json", "repo", "comment", "update", "--commit", commitID, "--id", commitCommentID, "--text", "live cli commit comment updated"}
	if commitCommentVersion != "" {
		updateCommitArgs = append(updateCommitArgs, "--version", commitCommentVersion)
	}
	updateCommitOutput, err := executeLiveCLI(t, updateCommitArgs...)
	if err != nil {
		t.Fatalf("repo comment update (commit) failed: %v\noutput: %s", err, updateCommitOutput)
	}
	updatedCommitVersion, ok := commentVersionFromCreateOutput(updateCommitOutput)
	if !ok {
		t.Fatalf("expected version in commit update output: %s", updateCommitOutput)
	}

	updatedCommitComment := repoCLIEntryWithID(t, repoCLICommitComments(t, commitID, "seed.txt"), commitCommentID)
	if updatedCommitComment["text"] != "live cli commit comment updated" {
		t.Errorf("commit comment text after the update = %v, want %q", updatedCommitComment["text"], "live cli commit comment updated")
	}
	if stored, _ := numericOrStringID(updatedCommitComment["version"]); stored != updatedCommitVersion || stored == commitCommentVersion {
		t.Errorf("commit comment is stored at version %s; the update reported %s, from %s", stored, updatedCommitVersion, commitCommentVersion)
	}

	// The reply goes first. Bitbucket refuses to delete a comment that has
	// replies -- "This comment has replies which must be deleted first" -- so a
	// thread is torn down leaf upwards, and asserting it here keeps the order
	// from being rediscovered by whoever adds the next reply to this fixture.
	if deleteReplyOutput, err := executeLiveCLI(t, "repo", "comment", "delete", "--commit", commitID, "--id", replyCommentID, "--yes"); err != nil {
		t.Fatalf("repo comment delete (reply) failed: %v\noutput: %s", err, deleteReplyOutput)
	}
	afterReplyDelete := repoCLICommitComments(t, commitID, "seed.txt")
	if _, found := repoCLIEntryByID(afterReplyDelete, replyCommentID); found {
		t.Fatalf("reply %s is still listed after its delete: %v", replyCommentID, afterReplyDelete)
	}
	if _, found := repoCLIEntryByID(afterReplyDelete, commitCommentID); !found {
		t.Fatalf("deleting reply %s removed comment %s as well: %v", replyCommentID, commitCommentID, afterReplyDelete)
	}

	// The version from before the update, which the comment is no longer at.
	staleDeleteOutput, err := executeLiveCLI(t, "--json", "repo", "comment", "delete", "--commit", commitID, "--id", commitCommentID,
		"--version", commitCommentVersion, "--yes")
	repoCLIAssertOutOfDate(t, staleDeleteOutput, err)
	if _, found := repoCLIEntryByID(repoCLICommitComments(t, commitID, "seed.txt"), commitCommentID); !found {
		t.Fatalf("a delete refused for its version removed comment %s anyway", commitCommentID)
	}

	deleteCommitArgs := []string{"repo", "comment", "delete", "--commit", commitID, "--id", commitCommentID, "--yes"}
	if updatedCommitVersion != "" {
		deleteCommitArgs = append(deleteCommitArgs, "--version", updatedCommitVersion)
	}
	deleteCommitOutput, err := executeLiveCLI(t, deleteCommitArgs...)
	if err != nil {
		t.Fatalf("repo comment delete (commit) failed: %v\noutput: %s", err, deleteCommitOutput)
	}
	if !strings.Contains(deleteCommitOutput, "Deleted comment") {
		t.Fatalf("expected human delete output, got: %s", deleteCommitOutput)
	}
	if remaining := repoCLICommitComments(t, commitID, "seed.txt"); len(remaining) != 0 {
		t.Fatalf("comments still listed on seed.txt after the last one was deleted: %v", remaining)
	}

	branch := testsupport.UniqueName("lt-repo-cli-")
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "repo-cli-feature.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	pullRequestID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
	if err != nil {
		t.Fatalf("create pull request failed: %v", err)
	}

	createPROutput, err := executeLiveCLI(t, "--json", "repo", "comment", "create", "--pr", pullRequestID, "--text", "live cli pr comment")
	if err != nil {
		t.Fatalf("repo comment create (pr) failed: %v\noutput: %s", err, createPROutput)
	}
	prCommentID, ok := commentIDFromCreateOutput(createPROutput)
	if !ok {
		t.Fatalf("expected comment id in pr create output: %s", createPROutput)
	}
	prCommentVersion, ok := commentVersionFromCreateOutput(createPROutput)
	if !ok {
		t.Fatalf("expected version in pr create output: %s", createPROutput)
	}

	// Anchored to the file the branch adds. The comment above is anchored to
	// nothing, so the listings scoped to that file had nothing they could show:
	// this is what they must find, and the other is what they must leave out.
	anchoredPRText := "live cli anchored pr comment"
	createAnchoredPROutput := mustLiveCLI(t, "repo", "comment", "create", "--pr", pullRequestID,
		"--text", anchoredPRText, "--path", "repo-cli-feature.txt", "--line", "1", "--line-type", "ADDED")
	anchoredPRCommentID, ok := commentIDFromCreateOutput(createAnchoredPROutput)
	if !ok {
		t.Fatalf("expected comment id in anchored pr create output: %s", createAnchoredPROutput)
	}

	listPROutput, err := executeLiveCLI(t, "--json", "repo", "comment", "list", "--pr", pullRequestID, "--path", "repo-cli-feature.txt", "--limit", "25")
	if err != nil {
		t.Fatalf("repo comment list (pr) failed: %v\noutput: %s", err, listPROutput)
	}
	if !jsonObjectHasCommentsArray(t, listPROutput) {
		t.Fatalf("expected comments array in pr list output: %s", listPROutput)
	}
	repoCLIAssertOnlyAnchoredComment(t, repoCLIComments(t, listPROutput), anchoredPRCommentID, anchoredPRText, prCommentID)

	prCommentListOutput, err := executeLiveCLI(t, "--json", "pr", "comment", "list", pullRequestID, "--path", "repo-cli-feature.txt", "--limit", "25")
	if err != nil {
		t.Fatalf("pr comment list failed: %v\noutput: %s", err, prCommentListOutput)
	}
	if !jsonObjectHasThreadsArray(t, prCommentListOutput) {
		t.Fatalf("expected threads array in pr comment list output: %s", prCommentListOutput)
	}
	pathThreads, _ := decodeJSONMap(t, prCommentListOutput)["threads"].([]any)
	repoCLIAssertOnlyAnchoredComment(t, pathThreads, anchoredPRCommentID, anchoredPRText, prCommentID)

	prCommentListFullOutput, err := executeLiveCLI(t, "--json", "pr", "comment", "list", pullRequestID, "--path", "repo-cli-feature.txt", "--limit", "25", "--full")
	if err != nil {
		t.Fatalf("pr comment list --full failed: %v\noutput: %s", err, prCommentListFullOutput)
	}
	if !jsonObjectHasCommentsArray(t, prCommentListFullOutput) {
		t.Fatalf("expected --full to restore the raw comments array: %s", prCommentListFullOutput)
	}
	repoCLIAssertOnlyAnchoredComment(t, repoCLIComments(t, prCommentListFullOutput), anchoredPRCommentID, anchoredPRText, prCommentID)

	aggregatePRCommentListOutput, err := executeLiveCLI(t, "--json", "pr", "comment", "list", pullRequestID, "--limit", "25")
	if err != nil {
		t.Fatalf("aggregate pr comment list failed: %v\noutput: %s", err, aggregatePRCommentListOutput)
	}
	if !jsonObjectHasThreadsArray(t, aggregatePRCommentListOutput) {
		t.Fatalf("expected threads array in aggregate pr comment list output: %s", aggregatePRCommentListOutput)
	}
	if !strings.Contains(aggregatePRCommentListOutput, `"source": "activities"`) {
		t.Fatalf("expected activities source in aggregate pr comment list output: %s", aggregatePRCommentListOutput)
	}
	// The whole pull request, so both: the one on no file and the one on a line.
	aggregateThreads, _ := decodeJSONMap(t, aggregatePRCommentListOutput)["threads"].([]any)
	if thread := repoCLIEntryWithID(t, aggregateThreads, prCommentID); thread["text"] != "live cli pr comment" || thread["anchor"] != nil {
		t.Errorf("thread %s = text %v, anchor %v; want %q on no file", prCommentID, thread["text"], thread["anchor"], "live cli pr comment")
	}
	repoCLIAssertAnchor(t, repoCLIEntryWithID(t, aggregateThreads, anchoredPRCommentID), "repo-cli-feature.txt", 1, "ADDED")

	prCommentGetOutput, err := executeLiveCLI(t, "--json", "pr", "comment", "get", pullRequestID, prCommentID)
	if err != nil {
		t.Fatalf("pr comment get failed: %v\noutput: %s", err, prCommentGetOutput)
	}
	if !strings.Contains(prCommentGetOutput, `"comment"`) {
		t.Fatalf("expected comment payload in pr comment get output: %s", prCommentGetOutput)
	}
	gotPRComment := nestedJSONMap(t, prCommentGetOutput, "comment")
	if id, _ := numericOrStringID(gotPRComment["id"]); id != prCommentID || gotPRComment["text"] != "live cli pr comment" || gotPRComment["anchor"] != nil {
		t.Errorf("pr comment get = id %s, text %v, anchor %v; want %s, %q, no anchor", id, gotPRComment["text"], gotPRComment["anchor"], prCommentID, "live cli pr comment")
	}

	prActivityListOutput, err := executeLiveCLI(t, "--json", "pr", "activity", "list", pullRequestID, "--limit", "25")
	if err != nil {
		t.Fatalf("pr activity list failed: %v\noutput: %s", err, prActivityListOutput)
	}
	if !strings.Contains(prActivityListOutput, `"activities"`) {
		t.Fatalf("expected activities payload in pr activity list output: %s", prActivityListOutput)
	}
	if !repoCLIActivityComments(t, prActivityListOutput, prCommentID, "live cli pr comment") {
		t.Errorf("no COMMENTED activity carries comment %s with its text: %s", prCommentID, prActivityListOutput)
	}

	humanPRCommentListOutput, err := executeLiveCLI(t, "pr", "comment", "list", pullRequestID, "--path", "repo-cli-feature.txt", "--limit", "25")
	if err != nil {
		t.Fatalf("pr comment list human failed: %v\noutput: %s", err, humanPRCommentListOutput)
	}
	if !strings.Contains(humanPRCommentListOutput, "[") && !strings.Contains(humanPRCommentListOutput, "No comments found") {
		t.Fatalf("expected human pr comment list output, got: %s", humanPRCommentListOutput)
	}
	if !strings.Contains(humanPRCommentListOutput, anchoredPRText) || strings.Contains(humanPRCommentListOutput, "live cli pr comment") {
		t.Fatalf("the human listing of repo-cli-feature.txt should show the comment on it and only that one: %s", humanPRCommentListOutput)
	}

	// Refused as out of date, or --version never reached Bitbucket.
	stalePRUpdateOutput, err := executeLiveCLI(t, "--json", "repo", "comment", "update", "--pr", pullRequestID, "--id", prCommentID,
		"--text", "an update at a version the comment is not at", "--version", repoCLIVersionAfter(t, prCommentVersion))
	repoCLIAssertOutOfDate(t, stalePRUpdateOutput, err)

	updatePRArgs := []string{"--json", "repo", "comment", "update", "--pr", pullRequestID, "--id", prCommentID, "--text", "live cli pr comment updated"}
	if prCommentVersion != "" {
		updatePRArgs = append(updatePRArgs, "--version", prCommentVersion)
	}
	updatePROutput, err := executeLiveCLI(t, updatePRArgs...)
	if err != nil {
		t.Fatalf("repo comment update (pr) failed: %v\noutput: %s", err, updatePROutput)
	}
	updatedPRVersion, ok := commentVersionFromCreateOutput(updatePROutput)
	if !ok {
		t.Fatalf("expected version in pr update output: %s", updatePROutput)
	}

	updatedPRComment := repoCLIPRComment(t, pullRequestID, prCommentID)
	if updatedPRComment["text"] != "live cli pr comment updated" {
		t.Errorf("pr comment text after the update = %v, want %q", updatedPRComment["text"], "live cli pr comment updated")
	}
	if stored, _ := numericOrStringID(updatedPRComment["version"]); stored != updatedPRVersion || stored == prCommentVersion {
		t.Errorf("pr comment is stored at version %s; the update reported %s, from %s", stored, updatedPRVersion, prCommentVersion)
	}

	stalePRDeleteOutput, err := executeLiveCLI(t, "--json", "repo", "comment", "delete", "--pr", pullRequestID, "--id", prCommentID,
		"--version", prCommentVersion, "--yes")
	repoCLIAssertOutOfDate(t, stalePRDeleteOutput, err)
	if kept := repoCLIPRComment(t, pullRequestID, prCommentID); kept["text"] != "live cli pr comment updated" {
		t.Fatalf("a delete refused for its version changed comment %s: %v", prCommentID, kept)
	}

	deletePRArgs := []string{"repo", "comment", "delete", "--pr", pullRequestID, "--id", prCommentID, "--yes"}
	if updatedPRVersion != "" {
		deletePRArgs = append(deletePRArgs, "--version", updatedPRVersion)
	}
	deletePROutput, err := executeLiveCLI(t, deletePRArgs...)
	if err != nil {
		t.Fatalf("repo comment delete (pr) failed: %v\noutput: %s", err, deletePROutput)
	}
	if !strings.Contains(deletePROutput, "Deleted comment") {
		t.Fatalf("expected human delete output, got: %s", deletePROutput)
	}
	repoCLIAssertPRCommentGone(t, pullRequestID, prCommentID)
}

func TestLiveCLIRepoSettingsSurface(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	permissionListOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "security", "permissions", "users", "list", "--limit", "100")
	if err != nil {
		t.Fatalf("repo settings permissions users list failed: %v\noutput: %s", err, permissionListOutput)
	}
	permissionListPayload := decodeJSONMap(t, permissionListOutput)
	if permissionListPayload["subject"] != "user" {
		t.Fatalf("expected a user listing in permissions list output: %s", permissionListOutput)
	}
	if _, ok := permissionListPayload["entries"].([]any); !ok {
		t.Fatalf("expected an entries list in permissions list output: %s", permissionListOutput)
	}

	// Whoever the harness authenticates as. The username used to come from the
	// environment alone, and a run on a token has none there, so the grant and
	// its check were skipped without a word.
	username := harness.username()
	if held := repoCLIPermissionEntry(t, permissionListOutput, username); held != nil {
		t.Fatalf("%s already holds %v on the fresh repository, so a grant would prove nothing: %s", username, held["permission"], permissionListOutput)
	}
	grantOutput, grantErr := executeLiveCLI(t, "--json", "repo", "settings", "security", "permissions", "users", "grant", username, "repo_write")
	if grantErr != nil {
		t.Fatalf("repo settings permissions users grant failed: %v\noutput: %s", grantErr, grantOutput)
	}
	if asString(decodeJSONMap(t, grantOutput)["status"]) != "ok" {
		t.Fatalf("expected grant status ok, got: %s", grantOutput)
	}
	grantedListOutput := mustLiveCLI(t, "repo", "settings", "security", "permissions", "users", "list", "--limit", "100")
	granted := repoCLIPermissionEntry(t, grantedListOutput, username)
	if granted == nil || granted["permission"] != "REPO_WRITE" {
		t.Fatalf("%s does not hold REPO_WRITE after a grant of repo_write: %s", username, grantedListOutput)
	}

	webhooksListOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "workflow", "webhooks", "list")
	if err != nil {
		t.Fatalf("repo settings workflow webhooks list failed: %v\noutput: %s", err, webhooksListOutput)
	}
	webhooksListPayload := decodeJSONMap(t, webhooksListOutput)
	if _, ok := webhooksListPayload["webhooks"]; !ok {
		t.Fatalf("expected webhooks field in webhooks list output: %s", webhooksListOutput)
	}
	if existing := repoCLIWebhooksIn(t, webhooksListOutput); len(existing) != 0 {
		t.Fatalf("the fresh repository already has webhooks: %v", existing)
	}

	// Neither --event nor --active is what bb sends when the flag is absent: the
	// event defaults to repo:refs_changed alone, and active to true. A flag that
	// never reached the request reads back as the default, not as this.
	webhookName := testsupport.UniqueName("lt-cli-webhook-")
	createWebhookOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "workflow", "webhooks", "create", webhookName, "http://localhost:65535/hook",
		"--event", "repo:refs_changed", "--event", "pr:merged", "--active=false")
	if err != nil {
		t.Fatalf("repo settings workflow webhooks create failed: %v\noutput: %s", err, createWebhookOutput)
	}
	// Failing rather than skipping the delete: without an id nothing below it
	// ran, and the test still passed.
	webhookID, ok := webhookIDFromCreateOutput(createWebhookOutput)
	if !ok {
		t.Fatalf("expected webhook id in create output, got: %s", createWebhookOutput)
	}
	storedWebhook := repoCLIEntryWithID(t, repoCLIWebhooksIn(t, mustLiveCLI(t, "repo", "settings", "workflow", "webhooks", "list")), webhookID)
	repoCLIAssertWebhook(t, storedWebhook, webhookName, "http://localhost:65535/hook", false, "pr:merged", "repo:refs_changed")

	deleteWebhookOutput, deleteErr := executeLiveCLI(t, "--json", "repo", "settings", "workflow", "webhooks", "delete", webhookID, "--yes")
	if deleteErr != nil {
		t.Fatalf("repo settings workflow webhooks delete failed: %v\noutput: %s", deleteErr, deleteWebhookOutput)
	}
	if asString(decodeJSONMap(t, deleteWebhookOutput)["status"]) != "ok" {
		t.Fatalf("expected webhook delete status ok, got: %s", deleteWebhookOutput)
	}
	if remaining := repoCLIWebhooksIn(t, mustLiveCLI(t, "repo", "settings", "workflow", "webhooks", "list")); len(remaining) != 0 {
		t.Fatalf("webhooks still listed after the delete: %v", remaining)
	}

	pullRequestsGetOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "pull-requests", "get")
	if err != nil {
		t.Fatalf("repo settings pull-requests get failed: %v\noutput: %s", err, pullRequestsGetOutput)
	}
	getPayload := decodeJSONMap(t, pullRequestsGetOutput)
	if _, ok := getPayload["requiredApprovers"]; !ok {
		t.Fatalf("expected the pull request settings in get output: %s", pullRequestsGetOutput)
	}
	// What a fresh repository holds. Every update below sends something else,
	// so one that was dropped reads back as this.
	if getPayload["requiredAllTasksComplete"] != false || getPayload["requiredApprovers"] != float64(0) {
		t.Fatalf("a fresh repository should require neither tasks nor approvals: %s", pullRequestsGetOutput)
	}

	pullRequestsUpdateOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "pull-requests", "update", "--required-all-tasks-complete=true")
	if err != nil {
		t.Fatalf("repo settings pull-requests update failed: %v\noutput: %s", err, pullRequestsUpdateOutput)
	}
	// The settings themselves are the confirmation: an update returns the
	// object it changed rather than a status beside it, like every other
	// command that reports what it just wrote.
	if decodeJSONMap(t, pullRequestsUpdateOutput)["requiredAllTasksComplete"] != true {
		t.Fatalf("expected the update to be reflected in the settings, got: %s", pullRequestsUpdateOutput)
	}
	if stored := repoCLIPullRequestSettings(t)["requiredAllTasksComplete"]; stored != true {
		t.Fatalf("requiredAllTasksComplete reads back as %v after setting it to true", stored)
	}

	pullRequestsApproversOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "pull-requests", "update-approvers", "--count", "2")
	if err != nil {
		t.Fatalf("repo settings pull-requests update-approvers failed: %v\noutput: %s", err, pullRequestsApproversOutput)
	}
	if _, ok := decodeJSONMap(t, pullRequestsApproversOutput)["requiredApprovers"]; !ok {
		t.Fatalf("expected the approver count in the update-approvers output, got: %s", pullRequestsApproversOutput)
	}
	if stored := repoCLIPullRequestSettings(t)["requiredApprovers"]; stored != float64(2) {
		t.Fatalf("requiredApprovers reads back as %v after setting it to 2", stored)
	}

	humanPermissionListOutput, err := executeLiveCLI(t, "repo", "settings", "security", "permissions", "users", "list", "--limit", "10")
	if err != nil {
		t.Fatalf("repo settings permissions users list (human) failed: %v\noutput: %s", err, humanPermissionListOutput)
	}
	if strings.TrimSpace(humanPermissionListOutput) == "" {
		t.Fatalf("expected non-empty human permissions output")
	}
	// The empty-listing notice is not empty either. A person reads the display
	// name, which is what the row carries.
	display := asString(granted["displayName"])
	if line := repoCLIHumanLine(humanPermissionListOutput, display); display == "" || !strings.HasSuffix(line, "REPO_WRITE") {
		t.Fatalf("expected the human listing to show %q with REPO_WRITE, got: %s", display, humanPermissionListOutput)
	}

	// The seeded repository has no webhooks, so the listing says so rather
	// than counting to zero. It used to answer "Webhooks configured: 0" and
	// nothing else, which is why the id `webhooks delete` takes could not be
	// obtained from the command that lists them (#522).
	humanWebhooksListOutput, err := executeLiveCLI(t, "repo", "settings", "workflow", "webhooks", "list")
	if err != nil {
		t.Fatalf("repo settings webhooks list (human) failed: %v\noutput: %s", err, humanWebhooksListOutput)
	}
	if !strings.Contains(humanWebhooksListOutput, "No webhooks found") {
		t.Fatalf("expected human webhooks output, got: %s", humanWebhooksListOutput)
	}

	humanPullRequestsGetOutput, err := executeLiveCLI(t, "repo", "settings", "pull-requests", "get")
	if err != nil {
		t.Fatalf("repo settings pull-requests get (human) failed: %v\noutput: %s", err, humanPullRequestsGetOutput)
	}
	if !strings.Contains(humanPullRequestsGetOutput, "Required tasks complete:") {
		t.Fatalf("expected human pull-request settings output, got: %s", humanPullRequestsGetOutput)
	}
	if !strings.HasSuffix(repoCLIHumanLine(humanPullRequestsGetOutput, "Required tasks complete:"), " true") ||
		!strings.HasSuffix(repoCLIHumanLine(humanPullRequestsGetOutput, "Required approvers:"), " 2") {
		t.Fatalf("expected the human settings to show the values stored above, got: %s", humanPullRequestsGetOutput)
	}

	// Back to false and to one: each differs from what the updates above
	// stored, so a dropped update reads back as true and two.
	humanPullRequestsUpdateOutput, err := executeLiveCLI(t, "repo", "settings", "pull-requests", "update", "--required-all-tasks-complete=false")
	if err != nil {
		t.Fatalf("repo settings pull-requests update (human) failed: %v\noutput: %s", err, humanPullRequestsUpdateOutput)
	}
	if !strings.Contains(humanPullRequestsUpdateOutput, "Updated pull-request settings") {
		t.Fatalf("expected human pull-requests update output, got: %s", humanPullRequestsUpdateOutput)
	}
	if stored := repoCLIPullRequestSettings(t)["requiredAllTasksComplete"]; stored != false {
		t.Fatalf("requiredAllTasksComplete reads back as %v after setting it to false", stored)
	}

	humanPullRequestsApproversOutput, err := executeLiveCLI(t, "repo", "settings", "pull-requests", "update-approvers", "--count", "1")
	if err != nil {
		t.Fatalf("repo settings pull-requests update-approvers (human) failed: %v\noutput: %s", err, humanPullRequestsApproversOutput)
	}
	if !strings.Contains(humanPullRequestsApproversOutput, "Updated pull-request settings") {
		t.Fatalf("expected human pull-requests update-approvers output, got: %s", humanPullRequestsApproversOutput)
	}
	if stored := repoCLIPullRequestSettings(t)["requiredApprovers"]; stored != float64(1) {
		t.Fatalf("requiredApprovers reads back as %v after setting it to 1", stored)
	}
}

func TestLiveCLIRepoPermissionsUserGrantDryRunNoSideEffect(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	username := harness.username()

	listBeforeOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "security", "permissions", "users", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("permissions users list before failed: %v\noutput: %s", err, listBeforeOutput)
	}
	// Not held yet, so a grant that was really sent would be in the listing.
	if held := repoCLIPermissionEntry(t, listBeforeOutput, username); held != nil {
		t.Fatalf("%s already holds %v, so a sent grant could change nothing: %s", username, held["permission"], listBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "repo", "settings", "security", "permissions", "users", "grant", username, "REPO_WRITE")
	if err != nil {
		t.Fatalf("permissions users grant dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"planningMode": "stateful"`) {
		t.Fatalf("expected stateful planning mode, got: %s", dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "repo.permission.user.grant"`) {
		t.Fatalf("expected intent in dry-run output, got: %s", dryRunOutput)
	}
	if predicted := repoCLIPredictedAction(t, dryRunOutput); predicted != "create" {
		t.Fatalf("expected the grant to be predicted as a create, got %q: %s", predicted, dryRunOutput)
	}

	listAfterOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "security", "permissions", "users", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("permissions users list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no permission side-effect from dry-run\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}
	if held := repoCLIPermissionEntry(t, listAfterOutput, username); held != nil {
		t.Fatalf("the dry run left %s holding %v: %s", username, held["permission"], listAfterOutput)
	}
}

func TestLiveCLIRepoPermissionsGroupGrantDryRunNoSideEffect(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	group := "stash-users"
	listBeforeOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "security", "permissions", "groups", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("permissions groups list before failed: %v\noutput: %s", err, listBeforeOutput)
	}
	// Not held yet, so a grant that was really sent would be in the listing.
	if held := repoCLIPermissionEntry(t, listBeforeOutput, group); held != nil {
		t.Fatalf("%s already holds %v, so a sent grant could change nothing: %s", group, held["permission"], listBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "repo", "settings", "security", "permissions", "groups", "grant", group, "REPO_READ")
	if err != nil {
		t.Fatalf("permissions groups grant dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "repo.permission.group.grant"`) {
		t.Fatalf("expected repo.permission.group.grant intent, got: %s", dryRunOutput)
	}
	if predicted := repoCLIPredictedAction(t, dryRunOutput); predicted != "create" {
		t.Fatalf("expected the grant to be predicted as a create, got %q: %s", predicted, dryRunOutput)
	}

	listAfterOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "security", "permissions", "groups", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("permissions groups list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no group permission side-effect from dry-run\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}
	if held := repoCLIPermissionEntry(t, listAfterOutput, group); held != nil {
		t.Fatalf("the dry run left %s holding %v: %s", group, held["permission"], listAfterOutput)
	}
}

func TestLiveCLIRepoPermissionsUserRevokeDryRunNoSideEffect(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// A grant for the dry run to preview removing. The subject used to be a user
	// who does not exist, whose revoke leaves the listing as it was whether it
	// is sent or not.
	username := harness.username()
	mustLiveCLI(t, "repo", "settings", "security", "permissions", "users", "grant", username, "REPO_WRITE")

	listBeforeOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "security", "permissions", "users", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("permissions users list before failed: %v\noutput: %s", err, listBeforeOutput)
	}
	if held := repoCLIPermissionEntry(t, listBeforeOutput, username); held == nil || held["permission"] != "REPO_WRITE" {
		t.Fatalf("%s does not hold REPO_WRITE after a grant of it: %s", username, listBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "repo", "settings", "security", "permissions", "users", "revoke", username, "--yes")
	if err != nil {
		t.Fatalf("permissions users revoke dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "repo.permission.user.revoke"`) {
		t.Fatalf("expected repo.permission.user.revoke intent, got: %s", dryRunOutput)
	}
	if predicted := repoCLIPredictedAction(t, dryRunOutput); predicted != "delete" {
		t.Fatalf("expected the revoke to be predicted as a delete, got %q: %s", predicted, dryRunOutput)
	}

	listAfterOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "security", "permissions", "users", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("permissions users list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no user permission side-effect from revoke dry-run\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}
	if held := repoCLIPermissionEntry(t, listAfterOutput, username); held == nil || held["permission"] != "REPO_WRITE" {
		t.Fatalf("the revoke dry run changed what %s holds: %s", username, listAfterOutput)
	}
}

func TestLiveCLIRepoPermissionsGroupRevokeDryRunNoSideEffect(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// A grant for the dry run to preview removing, for the same reason as the
	// user revoke: a group holding nothing is left as it was by a revoke that
	// was really sent.
	const group = "stash-users"
	mustLiveCLI(t, "repo", "settings", "security", "permissions", "groups", "grant", group, "REPO_READ")

	listBeforeOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "security", "permissions", "groups", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("permissions groups list before failed: %v\noutput: %s", err, listBeforeOutput)
	}
	if held := repoCLIPermissionEntry(t, listBeforeOutput, group); held == nil || held["permission"] != "REPO_READ" {
		t.Fatalf("%s does not hold REPO_READ after a grant of it: %s", group, listBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "repo", "settings", "security", "permissions", "groups", "revoke", group, "--yes")
	if err != nil {
		t.Fatalf("permissions groups revoke dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "repo.permission.group.revoke"`) {
		t.Fatalf("expected repo.permission.group.revoke intent, got: %s", dryRunOutput)
	}
	if predicted := repoCLIPredictedAction(t, dryRunOutput); predicted != "delete" {
		t.Fatalf("expected the revoke to be predicted as a delete, got %q: %s", predicted, dryRunOutput)
	}

	listAfterOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "security", "permissions", "groups", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("permissions groups list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no group permission side-effect from revoke dry-run\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}
	if held := repoCLIPermissionEntry(t, listAfterOutput, group); held == nil || held["permission"] != "REPO_READ" {
		t.Fatalf("the revoke dry run changed what %s holds: %s", group, listAfterOutput)
	}
}

func TestLiveCLIRepoWebhookCreateDryRunNoSideEffect(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	listBeforeOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "workflow", "webhooks", "list")
	if err != nil {
		t.Fatalf("webhooks list before failed: %v\noutput: %s", err, listBeforeOutput)
	}
	if existing := repoCLIWebhooksIn(t, listBeforeOutput); len(existing) != 0 {
		t.Fatalf("the fresh repository already has webhooks: %v", existing)
	}

	name := testsupport.UniqueName("lt-dryrun-webhook-")
	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "repo", "settings", "workflow", "webhooks", "create", name, "http://localhost:65535/hook", "--event", "repo:refs_changed")
	if err != nil {
		t.Fatalf("webhook create dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"planningMode": "stateful"`) {
		t.Fatalf("expected stateful planning mode, got: %s", dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "repo.webhook.create"`) {
		t.Fatalf("expected repo.webhook.create intent, got: %s", dryRunOutput)
	}
	if predicted := repoCLIPredictedAction(t, dryRunOutput); predicted != "create" {
		t.Fatalf("expected the webhook to be predicted as a create, got %q: %s", predicted, dryRunOutput)
	}

	listAfterOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "workflow", "webhooks", "list")
	if err != nil {
		t.Fatalf("webhooks list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no webhook side-effect from dry-run create\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}
	if created := repoCLIWebhooksIn(t, listAfterOutput); len(created) != 0 {
		t.Fatalf("the dry run left a webhook behind: %v", created)
	}
}

func TestLiveCLIRepoPullRequestSettingsUpdateDryRunNoSideEffect(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	settingsBeforeOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "pull-requests", "get")
	if err != nil {
		t.Fatalf("pull-request settings get before failed: %v\noutput: %s", err, settingsBeforeOutput)
	}
	// False to begin with, so an update to true that was really sent shows.
	if before := decodeJSONMap(t, settingsBeforeOutput)["requiredAllTasksComplete"]; before != false {
		t.Fatalf("requiredAllTasksComplete is %v on a fresh repository, want false: %s", before, settingsBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "repo", "settings", "pull-requests", "update", "--required-all-tasks-complete=true")
	if err != nil {
		t.Fatalf("pull-request settings update dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"planningMode": "stateful"`) {
		t.Fatalf("expected stateful planning mode, got: %s", dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "repo.pull-request-settings.update"`) {
		t.Fatalf("expected repo.pull-request-settings.update intent, got: %s", dryRunOutput)
	}
	if predicted := repoCLIPredictedAction(t, dryRunOutput); predicted != "update" {
		t.Fatalf("expected the settings change to be predicted as an update, got %q: %s", predicted, dryRunOutput)
	}

	settingsAfterOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "pull-requests", "get")
	if err != nil {
		t.Fatalf("pull-request settings get after failed: %v\noutput: %s", err, settingsAfterOutput)
	}

	if settingsBeforeOutput != settingsAfterOutput {
		t.Fatalf("expected no pull-request settings side-effect from dry-run update\nbefore: %s\nafter: %s", settingsBeforeOutput, settingsAfterOutput)
	}
	if after := decodeJSONMap(t, settingsAfterOutput)["requiredAllTasksComplete"]; after != false {
		t.Fatalf("requiredAllTasksComplete is %v after the dry run, want false", after)
	}
}

func TestLiveCLIRepoPullRequestSettingsUpdateApproversDryRunNoSideEffect(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	settingsBeforeOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "pull-requests", "get")
	if err != nil {
		t.Fatalf("pull-request settings get before failed: %v\noutput: %s", err, settingsBeforeOutput)
	}
	// None required to begin with, so a count of two that was really sent shows.
	if before := decodeJSONMap(t, settingsBeforeOutput)["requiredApprovers"]; before != float64(0) {
		t.Fatalf("requiredApprovers is %v on a fresh repository, want 0: %s", before, settingsBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "repo", "settings", "pull-requests", "update-approvers", "--count", "2")
	if err != nil {
		t.Fatalf("pull-request settings update-approvers dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"planningMode": "stateful"`) {
		t.Fatalf("expected stateful planning mode, got: %s", dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "repo.pull-request-settings.update-approvers"`) {
		t.Fatalf("expected update-approvers intent, got: %s", dryRunOutput)
	}
	if predicted := repoCLIPredictedAction(t, dryRunOutput); predicted != "update" {
		t.Fatalf("expected the approver count to be predicted as an update, got %q: %s", predicted, dryRunOutput)
	}

	settingsAfterOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "pull-requests", "get")
	if err != nil {
		t.Fatalf("pull-request settings get after failed: %v\noutput: %s", err, settingsAfterOutput)
	}

	if settingsBeforeOutput != settingsAfterOutput {
		t.Fatalf("expected no pull-request settings side-effect from update-approvers dry-run\nbefore: %s\nafter: %s", settingsBeforeOutput, settingsAfterOutput)
	}
	if after := decodeJSONMap(t, settingsAfterOutput)["requiredApprovers"]; after != float64(0) {
		t.Fatalf("requiredApprovers is %v after the dry run, want 0", after)
	}
}

func TestLiveCLIRepoPullRequestSettingsSetStrategyDryRunNoSideEffect(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	settingsBeforeOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "pull-requests", "get")
	if err != nil {
		t.Fatalf("pull-request settings get before failed: %v\noutput: %s", err, settingsBeforeOutput)
	}
	// Not squash to begin with, so a default of squash that was really sent
	// shows.
	strategyBefore, _ := decodeJSONMap(t, settingsBeforeOutput)["defaultMergeStrategy"].(string)
	if strategyBefore == "" || strategyBefore == "squash" {
		t.Fatalf("defaultMergeStrategy is %q on a fresh repository, want a strategy other than squash: %s", strategyBefore, settingsBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "repo", "settings", "pull-requests", "set-strategy", "squash")
	if err != nil {
		t.Fatalf("pull-request settings set-strategy dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "repo.pull-request-settings.set-strategy"`) {
		t.Fatalf("expected set-strategy intent, got: %s", dryRunOutput)
	}
	if predicted := repoCLIPredictedAction(t, dryRunOutput); predicted != "update" {
		t.Fatalf("expected the strategy change to be predicted as an update, got %q: %s", predicted, dryRunOutput)
	}

	settingsAfterOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "pull-requests", "get")
	if err != nil {
		t.Fatalf("pull-request settings get after failed: %v\noutput: %s", err, settingsAfterOutput)
	}

	if settingsBeforeOutput != settingsAfterOutput {
		t.Fatalf("expected no pull-request settings side-effect from set-strategy dry-run\nbefore: %s\nafter: %s", settingsBeforeOutput, settingsAfterOutput)
	}
	if after := decodeJSONMap(t, settingsAfterOutput)["defaultMergeStrategy"]; after != strategyBefore {
		t.Fatalf("defaultMergeStrategy is %v after the dry run, want %s", after, strategyBefore)
	}
}

func TestLiveCLIRepoWebhookDeleteDryRunNoSideEffect(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// An event other than the one bb subscribes to when --event is absent, so a
	// fixture stored without it reads back differently.
	createName := testsupport.UniqueName("lt-dryrun-webhook-del-")
	createOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "workflow", "webhooks", "create", createName, "http://localhost:65535/hook", "--event", "pr:opened")
	if err != nil {
		t.Fatalf("webhook create fixture failed: %v\noutput: %s", err, createOutput)
	}
	webhookID, ok := webhookIDFromCreateOutput(createOutput)
	if !ok {
		t.Fatalf("expected webhook id in create output, got: %s", createOutput)
	}

	listBeforeOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "workflow", "webhooks", "list")
	if err != nil {
		t.Fatalf("webhooks list before failed: %v\noutput: %s", err, listBeforeOutput)
	}
	// The fixture has to be there for a delete that was really sent to remove.
	repoCLIAssertWebhook(t, repoCLIEntryWithID(t, repoCLIWebhooksIn(t, listBeforeOutput), webhookID), createName, "http://localhost:65535/hook", true, "pr:opened")

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "repo", "settings", "workflow", "webhooks", "delete", webhookID, "--yes")
	if err != nil {
		t.Fatalf("webhook delete dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "repo.webhook.delete"`) {
		t.Fatalf("expected repo.webhook.delete intent, got: %s", dryRunOutput)
	}
	if predicted := repoCLIPredictedAction(t, dryRunOutput); predicted != "delete" {
		t.Fatalf("expected the webhook to be predicted as a delete, got %q: %s", predicted, dryRunOutput)
	}

	listAfterOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "workflow", "webhooks", "list")
	if err != nil {
		t.Fatalf("webhooks list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no webhook side-effect from delete dry-run\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}
	repoCLIAssertWebhook(t, repoCLIEntryWithID(t, repoCLIWebhooksIn(t, listAfterOutput), webhookID), createName, "http://localhost:65535/hook", true, "pr:opened")

	// The real delete, checked like any other: it used to discard its error.
	mustLiveCLI(t, "repo", "settings", "workflow", "webhooks", "delete", webhookID, "--yes")
	if remaining := repoCLIWebhooksIn(t, mustLiveCLI(t, "repo", "settings", "workflow", "webhooks", "list")); len(remaining) != 0 {
		t.Fatalf("webhooks still listed after the delete: %v", remaining)
	}
}

func TestLiveCLIPRCreateDryRunNoSideEffect(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 2)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	branch := testsupport.UniqueName("feature/live-pr-dryrun-create-")
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "dryrun-pr-create.txt"); err != nil {
		t.Fatalf("create branch failed: %v", err)
	}

	listBeforeOutput, err := executeLiveCLI(t, "--json", "pr", "list", "--state", "all", "--source-branch", branch, "--target-branch", "master")
	if err != nil {
		t.Fatalf("pr list before failed: %v\noutput: %s", err, listBeforeOutput)
	}
	if listed := repoCLIPullRequestsIn(t, listBeforeOutput); len(listed) != 0 {
		t.Fatalf("the branch already has pull requests, so one opened by the dry run would not stand out: %v", listed)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "pr", "create", "--from-ref", branch, "--to-ref", "master", "--title", "Dry run PR")
	if err != nil {
		t.Fatalf("pr create dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"planningMode": "stateful"`) {
		t.Fatalf("expected stateful planning mode, got: %s", dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "pr.create"`) {
		t.Fatalf("expected pr.create intent, got: %s", dryRunOutput)
	}
	if predicted := repoCLIPredictedAction(t, dryRunOutput); predicted != "create" {
		t.Fatalf("expected the pull request to be predicted as a create, got %q: %s", predicted, dryRunOutput)
	}

	listAfterOutput, err := executeLiveCLI(t, "--json", "pr", "list", "--state", "all", "--source-branch", branch, "--target-branch", "master")
	if err != nil {
		t.Fatalf("pr list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no pull request side-effect from create dry-run\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}
	// Across the whole repository as well, which is fresh: a branch filter that
	// matched nothing would hide a pull request just as well as there being none.
	if listed := repoCLIPullRequestsIn(t, mustLiveCLI(t, "pr", "list", "--state", "all")); len(listed) != 0 {
		t.Fatalf("the repository has pull requests after a dry run: %v", listed)
	}
}

func TestLiveCLIPRUpdateDryRunNoSideEffect(t *testing.T) {
	t.Parallel()

	harness, seeded, repo, pullRequestID := prepareOpenPRDryRunFixture(t)
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	beforeOutput, err := executeLiveCLI(t, "--json", "pr", "get", pullRequestID)
	if err != nil {
		t.Fatalf("pr get before failed: %v\noutput: %s", err, beforeOutput)
	}
	beforeTitle := prFieldAsString(t, beforeOutput, "title")
	beforeVersion := prFieldAsString(t, beforeOutput, "version")

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "pr", "update", pullRequestID, "--title", beforeTitle+" dry-run", "--version", "0")
	if err != nil {
		t.Fatalf("pr update dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "pr.update"`) {
		t.Fatalf("expected pr.update intent, got: %s", dryRunOutput)
	}
	if predicted := repoCLIPredictedAction(t, dryRunOutput); predicted != "update" {
		t.Fatalf("expected the title change to be predicted as an update, got %q: %s", predicted, dryRunOutput)
	}

	afterOutput, err := executeLiveCLI(t, "--json", "pr", "get", pullRequestID)
	if err != nil {
		t.Fatalf("pr get after failed: %v\noutput: %s", err, afterOutput)
	}
	afterTitle := prFieldAsString(t, afterOutput, "title")
	if beforeTitle != afterTitle {
		t.Fatalf("expected no title side-effect from update dry-run\nbefore: %s\nafter: %s", beforeTitle, afterTitle)
	}
	// Any update moves the version, including one to a field nobody reads here.
	if afterVersion := prFieldAsString(t, afterOutput, "version"); afterVersion != beforeVersion {
		t.Fatalf("the pull request moved from version %s to %s during an update dry run", beforeVersion, afterVersion)
	}
}

func TestLiveCLIPRGetIncludesMergeability(t *testing.T) {
	t.Parallel()

	harness, seeded, repo, pullRequestID := prepareOpenPRDryRunFixture(t)
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	output, err := executeLiveCLI(t, "--json", "pr", "get", pullRequestID)
	if err != nil {
		t.Fatalf("pr get failed: %v\noutput: %s", err, output)
	}

	payload := decodeJSONMap(t, output)
	pullRequest, ok := payload["pullRequest"].(map[string]any)
	if !ok {
		t.Fatalf("pull_request field missing from output: %s", output)
	}
	mergeability, ok := pullRequest["mergeability"].(map[string]any)
	if !ok {
		t.Fatalf("mergeability field missing from pr get output: %s", output)
	}
	if _, ok := mergeability["mergeable"].(bool); !ok {
		t.Fatalf("mergeability.mergeable missing from pr get output: %s", output)
	}

	if id, _ := numericOrStringID(pullRequest["id"]); id != pullRequestID {
		t.Fatalf("pr get %s answered with pull request %s", pullRequestID, id)
	}
	// A branch adding one file to an untouched master merges cleanly, so the
	// answer is known in advance. Checking only that mergeable is a boolean
	// passed for true and false alike.
	if mergeability["mergeable"] != true || mergeability["conflicted"] != false || mergeability["outcome"] != "CLEAN" {
		t.Fatalf("want a clean, mergeable pull request, got %v", mergeability)
	}
}

func TestLiveCLIPRMergeDryRunNoSideEffect(t *testing.T) {
	t.Parallel()

	harness, seeded, repo, pullRequestID := prepareOpenPRDryRunFixture(t)
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	beforeOutput, err := executeLiveCLI(t, "--json", "pr", "get", pullRequestID)
	if err != nil {
		t.Fatalf("pr get before failed: %v\noutput: %s", err, beforeOutput)
	}
	beforeState := prFieldAsString(t, beforeOutput, "state")
	// Open, so a merge that was really sent would leave it MERGED.
	if beforeState != "OPEN" {
		t.Fatalf("the fixture pull request is %s, want OPEN", beforeState)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "pr", "merge", pullRequestID, "--version", "0")
	if err != nil {
		t.Fatalf("pr merge dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "pr.merge"`) {
		t.Fatalf("expected pr.merge intent, got: %s", dryRunOutput)
	}
	if predicted := repoCLIPredictedAction(t, dryRunOutput); predicted != "update" {
		t.Fatalf("expected the merge to be predicted as an update, got %q: %s", predicted, dryRunOutput)
	}

	afterOutput, err := executeLiveCLI(t, "--json", "pr", "get", pullRequestID)
	if err != nil {
		t.Fatalf("pr get after failed: %v\noutput: %s", err, afterOutput)
	}
	afterState := prFieldAsString(t, afterOutput, "state")
	if beforeState != afterState {
		t.Fatalf("expected no state side-effect from merge dry-run\nbefore: %s\nafter: %s", beforeState, afterState)
	}
}

func TestLiveCLIPRReviewerAddDryRunNoSideEffect(t *testing.T) {
	t.Parallel()

	harness, seeded, repo, pullRequestID := prepareOpenPRDryRunFixture(t)
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// Somebody Bitbucket would take as a reviewer. This named the harness
	// account, which opened the pull request: Bitbucket refuses an author as a
	// reviewer and bb's preview skips one, so an add that was really sent could
	// not have changed the reviewers either.
	username := repoCLIRepositoryReader(t, harness, seeded.Key, repo.Slug).Username

	beforeOutput, err := executeLiveCLI(t, "--json", "pr", "get", pullRequestID)
	if err != nil {
		t.Fatalf("pr get before failed: %v\noutput: %s", err, beforeOutput)
	}
	beforeReviewers := prReviewersSnapshot(t, beforeOutput)
	if _, found := repoCLIReviewerIn(t, beforeOutput, username); found {
		t.Fatalf("%s is already on the pull request: %s", username, beforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "pr", "review", "reviewer", "add", pullRequestID, "--user", username)
	if err != nil {
		t.Fatalf("pr reviewer add dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "pr.review.reviewer.add"`) {
		t.Fatalf("expected pr.review.reviewer.add intent, got: %s", dryRunOutput)
	}
	if predicted := repoCLIPredictedAction(t, dryRunOutput); predicted != "update" {
		t.Fatalf("expected adding %s to be predicted as an update, got %q: %s", username, predicted, dryRunOutput)
	}

	afterOutput, err := executeLiveCLI(t, "--json", "pr", "get", pullRequestID)
	if err != nil {
		t.Fatalf("pr get after failed: %v\noutput: %s", err, afterOutput)
	}
	afterReviewers := prReviewersSnapshot(t, afterOutput)
	if beforeReviewers != afterReviewers {
		t.Fatalf("expected no reviewer side-effect from add dry-run\nbefore: %s\nafter: %s", beforeReviewers, afterReviewers)
	}
	if _, found := repoCLIReviewerIn(t, afterOutput, username); found {
		t.Fatalf("the add dry run put %s on the pull request: %s", username, afterOutput)
	}
}

func TestLiveCLIPRReviewerRemoveDryRunNoSideEffect(t *testing.T) {
	t.Parallel()

	harness, seeded, repo, pullRequestID := prepareOpenPRDryRunFixture(t)
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// A reviewer for the dry run to preview removing. This named the author,
	// who was never a reviewer, so a remove that was really sent changed nothing.
	username := repoCLIRepositoryReader(t, harness, seeded.Key, repo.Slug).Username
	mustLiveCLI(t, "pr", "review", "reviewer", "add", pullRequestID, "--user", username)

	beforeOutput, err := executeLiveCLI(t, "--json", "pr", "get", pullRequestID)
	if err != nil {
		t.Fatalf("pr get before failed: %v\noutput: %s", err, beforeOutput)
	}
	beforeReviewers := prReviewersSnapshot(t, beforeOutput)
	if entry, found := repoCLIReviewerIn(t, beforeOutput, username); !found || entry["role"] != "REVIEWER" {
		t.Fatalf("%s is not a reviewer after being added as one: %s", username, beforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "pr", "review", "reviewer", "remove", pullRequestID, "--user", username, "--yes")
	if err != nil {
		t.Fatalf("pr reviewer remove dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "pr.review.reviewer.remove"`) {
		t.Fatalf("expected pr.review.reviewer.remove intent, got: %s", dryRunOutput)
	}
	if predicted := repoCLIPredictedAction(t, dryRunOutput); predicted != "delete" {
		t.Fatalf("expected removing %s to be predicted as a delete, got %q: %s", username, predicted, dryRunOutput)
	}

	afterOutput, err := executeLiveCLI(t, "--json", "pr", "get", pullRequestID)
	if err != nil {
		t.Fatalf("pr get after failed: %v\noutput: %s", err, afterOutput)
	}
	afterReviewers := prReviewersSnapshot(t, afterOutput)
	if beforeReviewers != afterReviewers {
		t.Fatalf("expected no reviewer side-effect from remove dry-run\nbefore: %s\nafter: %s", beforeReviewers, afterReviewers)
	}
	if entry, found := repoCLIReviewerIn(t, afterOutput, username); !found || entry["role"] != "REVIEWER" {
		t.Fatalf("the remove dry run took %s off the pull request: %s", username, afterOutput)
	}
}

func TestLiveCLIPRDeclineDryRunNoSideEffect(t *testing.T) {
	t.Parallel()

	harness, seeded, repo, pullRequestID := prepareOpenPRDryRunFixture(t)
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	beforeOutput, err := executeLiveCLI(t, "--json", "pr", "get", pullRequestID)
	if err != nil {
		t.Fatalf("pr get before failed: %v\noutput: %s", err, beforeOutput)
	}
	beforeState := prFieldAsString(t, beforeOutput, "state")
	// Open, so a decline that was really sent would leave it DECLINED.
	if beforeState != "OPEN" {
		t.Fatalf("the fixture pull request is %s, want OPEN", beforeState)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "pr", "decline", pullRequestID, "--version", "0")
	if err != nil {
		t.Fatalf("pr decline dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "pr.decline"`) {
		t.Fatalf("expected pr.decline intent, got: %s", dryRunOutput)
	}
	if predicted := repoCLIPredictedAction(t, dryRunOutput); predicted != "update" {
		t.Fatalf("expected the decline to be predicted as an update, got %q: %s", predicted, dryRunOutput)
	}

	afterOutput, err := executeLiveCLI(t, "--json", "pr", "get", pullRequestID)
	if err != nil {
		t.Fatalf("pr get after failed: %v\noutput: %s", err, afterOutput)
	}
	afterState := prFieldAsString(t, afterOutput, "state")
	if beforeState != afterState {
		t.Fatalf("expected no state side-effect from decline dry-run\nbefore: %s\nafter: %s", beforeState, afterState)
	}
}

func TestLiveCLIPRReopenDryRunNoSideEffect(t *testing.T) {
	t.Parallel()

	harness, seeded, repo, pullRequestID := prepareOpenPRDryRunFixture(t)
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// bb reads the version itself when none is given, so a decline whose
	// --version was dropped succeeds at any number. One the pull request is not
	// at has to be refused.
	staleDeclineOutput, err := executeLiveCLI(t, "--json", "pr", "decline", pullRequestID, "--version", "1")
	repoCLIAssertOutOfDate(t, staleDeclineOutput, err)

	declineOutput, err := executeLiveCLI(t, "--json", "pr", "decline", pullRequestID, "--version", "0")
	if err != nil {
		t.Fatalf("pr decline fixture failed: %v\noutput: %s", err, declineOutput)
	}

	beforeOutput, err := executeLiveCLI(t, "--json", "pr", "get", pullRequestID)
	if err != nil {
		t.Fatalf("pr get before failed: %v\noutput: %s", err, beforeOutput)
	}
	beforeState := prFieldAsString(t, beforeOutput, "state")
	// Declined by the fixture, so a reopen that was really sent would leave it
	// OPEN. Were the decline not stored, the state would already be OPEN and the
	// comparison below could not fail.
	if beforeState != "DECLINED" {
		t.Fatalf("the pull request is %s after the decline, want DECLINED", beforeState)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "pr", "reopen", pullRequestID, "--version", "1")
	if err != nil {
		t.Fatalf("pr reopen dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "pr.reopen"`) {
		t.Fatalf("expected pr.reopen intent, got: %s", dryRunOutput)
	}
	if predicted := repoCLIPredictedAction(t, dryRunOutput); predicted != "update" {
		t.Fatalf("expected the reopen to be predicted as an update, got %q: %s", predicted, dryRunOutput)
	}

	afterOutput, err := executeLiveCLI(t, "--json", "pr", "get", pullRequestID)
	if err != nil {
		t.Fatalf("pr get after failed: %v\noutput: %s", err, afterOutput)
	}
	afterState := prFieldAsString(t, afterOutput, "state")
	if beforeState != afterState {
		t.Fatalf("expected no state side-effect from reopen dry-run\nbefore: %s\nafter: %s", beforeState, afterState)
	}
}

func TestLiveCLIPRApproveDryRunNoSideEffect(t *testing.T) {
	t.Parallel()

	harness, seeded, repo, pullRequestID := prepareOpenPRDryRunFixture(t)
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// A reviewer who has not approved, previewing their own approval. This ran
	// as the author, whose approval Bitbucket refuses, so an approve that was
	// really sent could not have changed the reviewers either.
	reviewer := repoCLIRepositoryReader(t, harness, seeded.Key, repo.Slug)
	mustLiveCLI(t, "pr", "review", "reviewer", "add", pullRequestID, "--user", reviewer.Username)
	configureLiveCLIEnvForUser(t, harness, seeded.Key, repo.Slug, reviewer)

	beforeOutput, err := executeLiveCLI(t, "--json", "pr", "get", pullRequestID)
	if err != nil {
		t.Fatalf("pr get before failed: %v\noutput: %s", err, beforeOutput)
	}
	beforeReviewers := prReviewersSnapshot(t, beforeOutput)
	if entry, found := repoCLIReviewerIn(t, beforeOutput, reviewer.Username); !found || entry["role"] != "REVIEWER" || entry["approved"] != false {
		t.Fatalf("want %s as a reviewer who has not approved: %s", reviewer.Username, beforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "pr", "review", "approve", pullRequestID)
	if err != nil {
		t.Fatalf("pr approve dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "pr.review.approve"`) {
		t.Fatalf("expected pr.review.approve intent, got: %s", dryRunOutput)
	}
	if predicted := repoCLIPredictedAction(t, dryRunOutput); predicted != "update" {
		t.Fatalf("expected the approval to be predicted as an update, got %q: %s", predicted, dryRunOutput)
	}

	afterOutput, err := executeLiveCLI(t, "--json", "pr", "get", pullRequestID)
	if err != nil {
		t.Fatalf("pr get after failed: %v\noutput: %s", err, afterOutput)
	}
	afterReviewers := prReviewersSnapshot(t, afterOutput)
	if beforeReviewers != afterReviewers {
		t.Fatalf("expected no reviewer side-effect from approve dry-run\nbefore: %s\nafter: %s", beforeReviewers, afterReviewers)
	}
	assertLiveReviewerApproval(t, pullRequestID, reviewer.Username, false)
}

func TestLiveCLIPRUnapproveDryRunNoSideEffect(t *testing.T) {
	t.Parallel()

	harness, seeded, repo, pullRequestID := prepareOpenPRDryRunFixture(t)
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// An approval for the dry run to preview clearing. Nobody held a review
	// status on the fixture, so an unapprove that was really sent changed
	// nothing. The approval is the reviewer's own, so from here the CLI runs as
	// them.
	reviewer := repoCLIRepositoryReader(t, harness, seeded.Key, repo.Slug)
	configureLiveCLIEnvForUser(t, harness, seeded.Key, repo.Slug, reviewer)
	mustLiveCLI(t, "pr", "review", "approve", pullRequestID)
	assertLiveReviewerApproval(t, pullRequestID, reviewer.Username, true)

	beforeOutput, err := executeLiveCLI(t, "--json", "pr", "get", pullRequestID)
	if err != nil {
		t.Fatalf("pr get before failed: %v\noutput: %s", err, beforeOutput)
	}
	beforeReviewers := prReviewersSnapshot(t, beforeOutput)

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "pr", "review", "unapprove", pullRequestID)
	if err != nil {
		t.Fatalf("pr unapprove dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "pr.review.unapprove"`) {
		t.Fatalf("expected pr.review.unapprove intent, got: %s", dryRunOutput)
	}
	if predicted := repoCLIPredictedAction(t, dryRunOutput); predicted != "update" {
		t.Fatalf("expected clearing the approval to be predicted as an update, got %q: %s", predicted, dryRunOutput)
	}

	afterOutput, err := executeLiveCLI(t, "--json", "pr", "get", pullRequestID)
	if err != nil {
		t.Fatalf("pr get after failed: %v\noutput: %s", err, afterOutput)
	}
	afterReviewers := prReviewersSnapshot(t, afterOutput)
	if beforeReviewers != afterReviewers {
		t.Fatalf("expected no reviewer side-effect from unapprove dry-run\nbefore: %s\nafter: %s", beforeReviewers, afterReviewers)
	}
	assertLiveReviewerApproval(t, pullRequestID, reviewer.Username, true)
}

func TestLiveCLIPRWatchUnwatchRebase(t *testing.T) {
	t.Parallel()

	harness, seeded, repo, pullRequestID := prepareOpenPRDryRunFixture(t)
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// Watch dry-run
	watchDryRun, err := executeLiveCLI(t, "--json", "--dry-run", "pr", "watch", pullRequestID)
	if err != nil {
		t.Fatalf("pr watch dry-run failed: %v\noutput: %s", err, watchDryRun)
	}
	if !strings.Contains(watchDryRun, `"intent": "pr.watch"`) {
		t.Fatalf("expected pr.watch intent, got: %s", watchDryRun)
	}

	// Watch live
	watchLive, err := executeLiveCLI(t, "--json", "pr", "watch", pullRequestID)
	if err != nil {
		t.Fatalf("pr watch live failed: %v\noutput: %s", err, watchLive)
	}

	// Unwatch dry-run
	unwatchDryRun, err := executeLiveCLI(t, "--json", "--dry-run", "pr", "unwatch", pullRequestID)
	if err != nil {
		t.Fatalf("pr unwatch dry-run failed: %v\noutput: %s", err, unwatchDryRun)
	}
	if !strings.Contains(unwatchDryRun, `"intent": "pr.unwatch"`) {
		t.Fatalf("expected pr.unwatch intent, got: %s", unwatchDryRun)
	}

	// Unwatch live
	unwatchLive, err := executeLiveCLI(t, "--json", "pr", "unwatch", pullRequestID)
	if err != nil {
		t.Fatalf("pr unwatch live failed: %v\noutput: %s", err, unwatchLive)
	}

	// Rebase dry-run (checking rebaseability)
	rebaseDryRun, err := executeLiveCLI(t, "--json", "--dry-run", "pr", "rebase", pullRequestID)
	if err != nil {
		t.Fatalf("pr rebase dry-run failed: %v\noutput: %s", err, rebaseDryRun)
	}
	if !strings.Contains(rebaseDryRun, `"intent": "pr.rebase"`) {
		t.Fatalf("expected pr.rebase intent, got: %s", rebaseDryRun)
	}
}

func TestLiveCommitPRsAndParticipants(t *testing.T) {
	t.Parallel()

	harness, seeded, repo, pullRequestID := prepareOpenPRDryRunFixture(t)
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// Get the PR to find its source commit
	prGetJSON, err := executeLiveCLI(t, "--json", "pr", "get", pullRequestID)
	if err != nil {
		t.Fatalf("pr get failed: %v\noutput: %s", err, prGetJSON)
	}

	var envelope map[string]any
	if err := json.Unmarshal([]byte(prGetJSON), &envelope); err != nil {
		t.Fatalf("failed to parse pr get JSON: %v", err)
	}
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatalf("data field not found or not a map in envelope: %s", prGetJSON)
	}
	prData, ok := data["pullRequest"].(map[string]any)
	if !ok {
		t.Fatalf("pull_request field not found or not a map in data: %s", prGetJSON)
	}
	sourceCommit, ok := prData["sourceCommit"].(string)
	if !ok || sourceCommit == "" {
		t.Fatalf("source_commit not found or empty in get output: %s", prGetJSON)
	}

	// 1. Test List pull requests containing commit
	commitPRsJSON, err := executeLiveCLI(t, "--json", "commit", "prs", sourceCommit)
	if err != nil {
		t.Fatalf("commit prs failed: %v\noutput: %s", err, commitPRsJSON)
	}
	if !strings.Contains(commitPRsJSON, fmt.Sprintf(`"id": %s`, pullRequestID)) {
		t.Fatalf("expected PR ID %s in commit prs output, got: %s", pullRequestID, commitPRsJSON)
	}

	// 2. Test Search participants
	participantsJSON, err := executeLiveCLI(t, "--json", "pr", "participants", "--search", "admin")
	if err != nil {
		t.Fatalf("pr participants failed: %v\noutput: %s", err, participantsJSON)
	}
	if !strings.Contains(participantsJSON, `"name": "admin"`) {
		t.Fatalf("expected participant admin in output, got: %s", participantsJSON)
	}
}

func TestLiveCLIRepoCommentCreateDryRunNoSideEffect(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 2)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	branch := testsupport.UniqueName("feature/live-comment-dryrun-")
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "dryrun-comment-fixture.txt"); err != nil {
		t.Fatalf("create branch failed: %v", err)
	}

	pullRequestID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
	if err != nil {
		t.Fatalf("create pull request fixture failed: %v", err)
	}

	listBeforeOutput, err := executeLiveCLI(t, "--json", "repo", "comment", "list", "--pr", pullRequestID, "--path", "dryrun-comment-fixture.txt", "--limit", "200")
	if err != nil {
		t.Fatalf("repo comment list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "repo", "comment", "create", "--pr", pullRequestID, "--text", "dry-run comment")
	if err != nil {
		t.Fatalf("repo comment create dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "repo.comment.create"`) {
		t.Fatalf("expected repo.comment.create intent, got: %s", dryRunOutput)
	}

	listAfterOutput, err := executeLiveCLI(t, "--json", "repo", "comment", "list", "--pr", pullRequestID, "--path", "dryrun-comment-fixture.txt", "--limit", "200")
	if err != nil {
		t.Fatalf("repo comment list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no comment side-effect from create dry-run\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}
}

func TestLiveCLIRepoCommentUpdateDryRunNoSideEffect(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 2)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	branch := testsupport.UniqueName("feature/live-comment-update-dryrun-")
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "dryrun-comment-update-fixture.txt"); err != nil {
		t.Fatalf("create branch failed: %v", err)
	}

	pullRequestID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
	if err != nil {
		t.Fatalf("create pull request fixture failed: %v", err)
	}

	createOutput, err := executeLiveCLI(t, "--json", "repo", "comment", "create", "--pr", pullRequestID, "--text", "fixture comment")
	if err != nil {
		t.Fatalf("create fixture comment failed: %v\noutput: %s", err, createOutput)
	}
	commentID, ok := commentIDFromCreateOutput(createOutput)
	if !ok {
		t.Fatalf("expected comment id in fixture output: %s", createOutput)
	}

	beforeOutput, err := executeLiveCLI(t, "--json", "repo", "comment", "list", "--pr", pullRequestID, "--path", "dryrun-comment-update-fixture.txt", "--limit", "200")
	if err != nil {
		t.Fatalf("comment list before failed: %v\noutput: %s", err, beforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "repo", "comment", "update", "--pr", pullRequestID, "--id", commentID, "--text", "dry-run updated comment")
	if err != nil {
		t.Fatalf("repo comment update dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "repo.comment.update"`) {
		t.Fatalf("expected repo.comment.update intent, got: %s", dryRunOutput)
	}

	afterOutput, err := executeLiveCLI(t, "--json", "repo", "comment", "list", "--pr", pullRequestID, "--path", "dryrun-comment-update-fixture.txt", "--limit", "200")
	if err != nil {
		t.Fatalf("comment list after failed: %v\noutput: %s", err, afterOutput)
	}

	if beforeOutput != afterOutput {
		t.Fatalf("expected no comment side-effect from update dry-run\nbefore: %s\nafter: %s", beforeOutput, afterOutput)
	}

	_, _ = executeLiveCLI(t, "repo", "comment", "delete", "--pr", pullRequestID, "--id", commentID, "--yes")
}

func TestLiveCLIRepoCommentDeleteDryRunNoSideEffect(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 2)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	branch := testsupport.UniqueName("feature/live-comment-delete-dryrun-")
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "dryrun-comment-delete-fixture.txt"); err != nil {
		t.Fatalf("create branch failed: %v", err)
	}

	pullRequestID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
	if err != nil {
		t.Fatalf("create pull request fixture failed: %v", err)
	}

	createOutput, err := executeLiveCLI(t, "--json", "repo", "comment", "create", "--pr", pullRequestID, "--text", "fixture comment delete")
	if err != nil {
		t.Fatalf("create fixture comment failed: %v\noutput: %s", err, createOutput)
	}
	commentID, ok := commentIDFromCreateOutput(createOutput)
	if !ok {
		t.Fatalf("expected comment id in fixture output: %s", createOutput)
	}

	beforeOutput, err := executeLiveCLI(t, "--json", "repo", "comment", "list", "--pr", pullRequestID, "--path", "dryrun-comment-delete-fixture.txt", "--limit", "200")
	if err != nil {
		t.Fatalf("comment list before failed: %v\noutput: %s", err, beforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "repo", "comment", "delete", "--pr", pullRequestID, "--id", commentID, "--yes")
	if err != nil {
		t.Fatalf("repo comment delete dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "repo.comment.delete"`) {
		t.Fatalf("expected repo.comment.delete intent, got: %s", dryRunOutput)
	}

	afterOutput, err := executeLiveCLI(t, "--json", "repo", "comment", "list", "--pr", pullRequestID, "--path", "dryrun-comment-delete-fixture.txt", "--limit", "200")
	if err != nil {
		t.Fatalf("comment list after failed: %v\noutput: %s", err, afterOutput)
	}

	if beforeOutput != afterOutput {
		t.Fatalf("expected no comment side-effect from delete dry-run\nbefore: %s\nafter: %s", beforeOutput, afterOutput)
	}

	_, _ = executeLiveCLI(t, "repo", "comment", "delete", "--pr", pullRequestID, "--id", commentID, "--yes")
}

func prepareOpenPRDryRunFixture(t *testing.T) (*liveHarness, seededProject, seededRepository, string) {
	t.Helper()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 2)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	branch := testsupport.UniqueName("feature/live-pr-dryrun-")
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "dryrun-pr-fixture.txt"); err != nil {
		t.Fatalf("create branch failed: %v", err)
	}

	pullRequestID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
	if err != nil {
		t.Fatalf("create pull request fixture failed: %v", err)
	}

	return harness, seeded, repo, pullRequestID
}

func prFieldAsString(t *testing.T, output, field string) string {
	t.Helper()
	payload := decodeJSONMap(t, output)
	pullRequest, ok := payload["pullRequest"].(map[string]any)
	if !ok {
		t.Fatalf("pull_request field missing from output: %s", output)
	}
	return asString(pullRequest[field])
}

func prReviewersSnapshot(t *testing.T, output string) string {
	t.Helper()
	payload := decodeJSONMap(t, output)
	pullRequest, ok := payload["pullRequest"].(map[string]any)
	if !ok {
		t.Fatalf("pull_request field missing from output: %s", output)
	}
	reviewers := pullRequest["reviewers"]
	raw, err := json.Marshal(reviewers)
	if err != nil {
		t.Fatalf("failed to marshal reviewers snapshot: %v", err)
	}
	return string(raw)
}

func jsonArrayContainsSlug(t *testing.T, output string, slug string) bool {
	t.Helper()

	items := make([]map[string]any, 0)
	if err := unmarshalJSONArray(output, &items); err != nil {
		t.Fatalf("expected json array output, got parse error %v for: %s", err, output)
	}

	for _, item := range items {
		if asString(item["slug"]) == slug {
			return true
		}
	}

	return false
}

// jsonArrayHasEntries reports whether a listing came back with anything in it.
//
// For the instance-wide listings, where naming a row would be a claim about
// how many repositories the whole Bitbucket holds rather than about the
// command.
func jsonArrayHasEntries(t *testing.T, output string) bool {
	t.Helper()

	items := make([]map[string]any, 0)
	if err := unmarshalJSONArray(output, &items); err != nil {
		t.Fatalf("expected json array output, got parse error %v for: %s", err, output)
	}

	return len(items) > 0
}

func jsonObjectHasCommentsArray(t *testing.T, output string) bool {
	t.Helper()

	payload := decodeJSONMap(t, output)
	_, ok := payload["comments"].([]any)
	return ok
}

// jsonObjectHasThreadsArray checks the summarised thread view that
// `bb pr comment list` emits. `bb repo comment list` still emits the raw
// comments array, so the two helpers are not interchangeable.
func jsonObjectHasThreadsArray(t *testing.T, output string) bool {
	t.Helper()

	payload := decodeJSONMap(t, output)
	if _, ok := payload["summary"].(map[string]any); !ok {
		return false
	}
	_, ok := payload["threads"].([]any)
	return ok
}

func commentIDFromCreateOutput(output string) (string, bool) {
	payload := map[string]any{}
	if err := unmarshalJSONObject(output, &payload); err != nil {
		return "", false
	}

	comment, ok := payload["comment"].(map[string]any)
	if !ok {
		return "", false
	}

	return numericOrStringID(comment["id"])
}

func commentVersionFromCreateOutput(output string) (string, bool) {
	payload := map[string]any{}
	if err := unmarshalJSONObject(output, &payload); err != nil {
		return "", false
	}

	comment, ok := payload["comment"].(map[string]any)
	if !ok {
		return "", false
	}

	return numericOrStringID(comment["version"])
}

func webhookIDFromCreateOutput(output string) (string, bool) {
	payload := map[string]any{}
	if err := unmarshalJSONObject(output, &payload); err != nil {
		return "", false
	}

	webhook, ok := payload["webhook"].(map[string]any)
	if !ok {
		return "", false
	}

	return numericOrStringID(webhook["id"])
}

func numericOrStringID(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return "", false
		}
		return trimmed, true
	case float64:
		return strconv.FormatInt(int64(typed), 10), true
	case int64:
		return strconv.FormatInt(typed, 10), true
	case int:
		return strconv.Itoa(typed), true
	default:
		return "", false
	}
}

func unmarshalJSONObject(value string, target *map[string]any) error {
	envelope := map[string]any{}
	if err := json.Unmarshal([]byte(value), &envelope); err != nil {
		return err
	}

	rawData, ok := envelope["data"]
	if !ok {
		return fmt.Errorf("missing data field")
	}

	encodedData, err := json.Marshal(rawData)
	if err != nil {
		return err
	}

	return json.Unmarshal(encodedData, target)
}

// repoCLIArray decodes a listing whose data is a list, failing the test when it
// is not one.
func repoCLIArray(t *testing.T, output string) []map[string]any {
	t.Helper()

	items := make([]map[string]any, 0)
	if err := unmarshalJSONArray(output, &items); err != nil {
		t.Fatalf("expected json array output, got parse error %v for: %s", err, output)
	}

	return items
}

// repoCLIComments reads the comments array out of a comment listing.
func repoCLIComments(t *testing.T, output string) []any {
	t.Helper()

	comments, ok := decodeJSONMap(t, output)["comments"].([]any)
	if !ok {
		t.Fatalf("expected a comments array in: %s", output)
	}

	return comments
}

// repoCLICommitComments lists what bb reads back for one file of a commit,
// replies included.
func repoCLICommitComments(t *testing.T, commitID, path string) []any {
	t.Helper()

	return repoCLIComments(t, mustLiveCLI(t, "repo", "comment", "list", "--commit", commitID, "--path", path, "--limit", "25"))
}

// repoCLIEntryByID finds the entry of a listing whose own id is the one given.
//
// Only the entries themselves and not what they nest: a comment's author has
// an id too, and a walk that matched it would read a user as the comment.
func repoCLIEntryByID(entries []any, id string) (map[string]any, bool) {
	for _, entry := range entries {
		fields, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if candidate, ok := numericOrStringID(fields["id"]); ok && candidate == id {
			return fields, true
		}
	}

	return nil, false
}

// repoCLIEntryWithID is repoCLIEntryByID for an entry the listing must hold.
func repoCLIEntryWithID(t *testing.T, entries []any, id string) map[string]any {
	t.Helper()

	entry, ok := repoCLIEntryByID(entries, id)
	if !ok {
		t.Fatalf("no entry with id %s in the listing: %v", id, entries)
	}

	return entry
}

// repoCLIAssertAnchor checks where a comment, or the thread it opens, sits in
// the diff. Both carry the anchor under the same keys.
func repoCLIAssertAnchor(t *testing.T, comment map[string]any, path string, line int, lineType string) {
	t.Helper()

	anchor, ok := comment["anchor"].(map[string]any)
	if !ok {
		t.Errorf("comment %v has no anchor, want %s line %d %s", comment["id"], path, line, lineType)
		return
	}
	if anchor["path"] != path || anchor["line"] != float64(line) || anchor["lineType"] != lineType {
		t.Errorf("comment %v is anchored at %v line %v %v, want %s line %d %s",
			comment["id"], anchor["path"], anchor["line"], anchor["lineType"], path, line, lineType)
	}
}

// repoCLIAssertOnlyAnchoredComment checks a listing scoped to the file of the
// anchored comment: that one is in it where it was put, and the comment
// anchored to no file is not.
func repoCLIAssertOnlyAnchoredComment(t *testing.T, entries []any, anchoredID, anchoredText, unanchoredID string) {
	t.Helper()

	anchored := repoCLIEntryWithID(t, entries, anchoredID)
	if anchored["text"] != anchoredText {
		t.Errorf("comment %s text = %v, want %q", anchoredID, anchored["text"], anchoredText)
	}
	repoCLIAssertAnchor(t, anchored, "repo-cli-feature.txt", 1, "ADDED")

	if _, found := repoCLIEntryByID(entries, unanchoredID); found {
		t.Errorf("a listing scoped to repo-cli-feature.txt holds comment %s, which is on no file: %v", unanchoredID, entries)
	}
}

// repoCLIActivityComments reports whether a pull request's timeline records
// the comment, with its text, as a COMMENTED entry.
func repoCLIActivityComments(t *testing.T, output, commentID, text string) bool {
	t.Helper()

	activities, _ := decodeJSONMap(t, output)["activities"].([]any)
	for _, entry := range activities {
		activity, _ := entry.(map[string]any)
		comment, _ := activity["comment"].(map[string]any)
		if activity["action"] != "COMMENTED" || comment == nil {
			continue
		}
		if id, _ := numericOrStringID(comment["id"]); id == commentID && comment["text"] == text {
			return true
		}
	}

	return false
}

// repoCLIPRComment reads one pull request comment back by id.
func repoCLIPRComment(t *testing.T, prID, commentID string) map[string]any {
	t.Helper()

	return nestedJSONMap(t, mustLiveCLI(t, "pr", "comment", "get", prID, commentID), "comment")
}

// repoCLIAssertPRCommentGone checks a deleted pull request comment cannot be
// read by id, for the reason that it is not there. Any failure of the read would
// otherwise pass for a delete.
func repoCLIAssertPRCommentGone(t *testing.T, prID, commentID string) {
	t.Helper()

	output, err := executeLiveCLI(t, "--json", "pr", "comment", "get", prID, commentID)
	if !apperrors.IsKind(err, apperrors.KindNotFound) {
		t.Fatalf("comment %s on pull request %s: want not found after the delete, got %v\n%s", commentID, prID, err, output)
	}
}

// repoCLIPermissionEntry finds the entry a repository permission listing holds
// for one user or group, or nil when it names them nowhere.
func repoCLIPermissionEntry(t *testing.T, output, name string) map[string]any {
	t.Helper()

	entries, ok := decodeJSONMap(t, output)["entries"].([]any)
	if !ok {
		t.Fatalf("expected an entries list in: %s", output)
	}
	for _, entry := range entries {
		fields, _ := entry.(map[string]any)
		if subject, _ := fields["name"].(string); strings.EqualFold(subject, name) {
			return fields
		}
	}

	return nil
}

// repoCLIWebhooksIn reads the webhooks out of a webhook listing.
func repoCLIWebhooksIn(t *testing.T, output string) []any {
	t.Helper()

	webhooks, ok := decodeJSONMap(t, output)["webhooks"].([]any)
	if !ok {
		t.Fatalf("expected a webhooks array in: %s", output)
	}

	return webhooks
}

// repoCLIAssertWebhook checks a webhook read back holds what it was created
// with. Events are compared as a set: the order they are stored in is not
// something a caller asked for.
func repoCLIAssertWebhook(t *testing.T, webhook map[string]any, name, url string, active bool, events ...string) {
	t.Helper()

	if webhook["name"] != name || webhook["url"] != url || webhook["active"] != active {
		t.Errorf("webhook %v = name %v, url %v, active %v; want %s, %s, %t", webhook["id"], webhook["name"], webhook["url"], webhook["active"], name, url, active)
	}

	stored, _ := webhook["events"].([]any)
	got := make([]string, 0, len(stored))
	for _, event := range stored {
		got = append(got, asString(event))
	}
	want := append([]string{}, events...)
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("webhook %v subscribes to %v, want %v", webhook["id"], got, want)
	}
}

// repoCLIPullRequestSettings reads the repository's pull request settings back.
func repoCLIPullRequestSettings(t *testing.T) map[string]any {
	t.Helper()

	return decodeJSONMap(t, mustLiveCLI(t, "repo", "settings", "pull-requests", "get"))
}

// repoCLIHumanLine returns the first line of human output that mentions text,
// trimmed, or "" when none does.
func repoCLIHumanLine(output, text string) string {
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, text) {
			return strings.TrimSpace(line)
		}
	}

	return ""
}

// repoCLIPredictedAction reads what a dry run says the command would do, from
// the preview's first item.
//
// A dry run is read back by showing nothing changed, which only means
// something where a real run would have changed it. The prediction is the
// preview agreeing that it would have.
func repoCLIPredictedAction(t *testing.T, output string) string {
	t.Helper()

	items, _ := decodeJSONMap(t, output)["items"].([]any)
	if len(items) == 0 {
		t.Fatalf("no items in the preview: %s", output)
	}
	item, _ := items[0].(map[string]any)
	predicted, _ := item["predictedAction"].(string)

	return predicted
}

// repoCLIAssertOutOfDate checks a write was refused as a conflict, the refusal
// that only the version it named explains. Any failure at all would otherwise
// pass for the version having been checked.
func repoCLIAssertOutOfDate(t *testing.T, output string, err error) {
	t.Helper()

	if !apperrors.IsKind(err, apperrors.KindConflict) {
		t.Fatalf("want a conflict for a version the target is not at, got %v\n%s", err, output)
	}
}

// repoCLIPullRequestsIn reads the pull requests out of a pr list payload.
func repoCLIPullRequestsIn(t *testing.T, output string) []any {
	t.Helper()

	pullRequests, ok := decodeJSONMap(t, output)["pullRequests"].([]any)
	if !ok {
		t.Fatalf("expected a pullRequests array in: %s", output)
	}

	return pullRequests
}

// repoCLIReviewerIn finds one participant in a pr get payload.
func repoCLIReviewerIn(t *testing.T, output, username string) (map[string]any, bool) {
	t.Helper()

	reviewers, _ := extractPRData(decodeJSONMap(t, output))["reviewers"].([]any)
	for _, entry := range reviewers {
		reviewer, _ := entry.(map[string]any)
		if name, _ := reviewer["name"].(string); strings.EqualFold(name, username) {
			return reviewer, true
		}
	}

	return nil, false
}

// repoCLIRepositoryReader creates a licensed user who can read the repository,
// which is what Bitbucket asks of a reviewer and of anyone who approves.
//
// The grant is read back rather than trusted: a dry run previewing a review
// action proves nothing about side effects when the real action would have
// been refused for want of it.
func repoCLIRepositoryReader(t *testing.T, harness *liveHarness, projectKey, repositorySlug string) restrictedUser {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	user, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create a licensed user failed: %v", err)
	}
	if err := harness.grantRepoPermission(ctx, projectKey, repositorySlug, user.Username, "REPO_READ"); err != nil {
		t.Fatalf("grant %s read access failed: %v", user.Username, err)
	}

	listing := mustLiveCLI(t, "repo", "settings", "security", "permissions", "users", "list", "--limit", "200")
	if held := repoCLIPermissionEntry(t, listing, user.Username); held == nil || held["permission"] != "REPO_READ" {
		t.Fatalf("%s does not hold REPO_READ after the grant: %s", user.Username, listing)
	}

	return user
}

// repoCLIVersionAfter is a version one past the one given, which a comment at
// the given version is not at.
func repoCLIVersionAfter(t *testing.T, version string) string {
	t.Helper()

	number, err := strconv.Atoi(version)
	if err != nil {
		t.Fatalf("comment version %q is not a number: %v", version, err)
	}

	return strconv.Itoa(number + 1)
}

func unmarshalJSONArray(value string, target *[]map[string]any) error {
	envelope := map[string]any{}
	if err := json.Unmarshal([]byte(value), &envelope); err != nil {
		return err
	}

	rawData, ok := envelope["data"]
	if !ok {
		return fmt.Errorf("missing data field")
	}

	encodedData, err := json.Marshal(rawData)
	if err != nil {
		return err
	}

	return json.Unmarshal(encodedData, target)
}
