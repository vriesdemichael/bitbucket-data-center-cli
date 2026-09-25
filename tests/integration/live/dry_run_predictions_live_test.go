//go:build live

package live_test

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// TestLiveDryRunPredictionsReadRealState covers the previews whose answer is not
// a property of the command but of what the server currently holds.
//
// A preview that always says "create" needs no server. These are the other
// kind: merging a merged pull request or a declined one fails as the real
// merge does, approving one you have already approved changes nothing, and
// opening a pull request for a branch that already has one conflicts. The prediction is
// read from Bitbucket, and the mocked versions read it from a fixture the same
// author wrote -- so they agreed about a state no Bitbucket had ever been in.
//
// One of them was wrong for exactly that reason: the unapprove preview predicted
// no-op from a fabricated participant list, and against a real pull request
// whose reviewer is on NEEDS_WORK the answer is not no-op. See
// TestLivePullRequestReviewSetCommand.
//
// Every state a prediction is read from is read back after it is made, since a
// write Bitbucket answered 2xx may still have dropped what it was sent. Where a
// dry run predicts a change, what it would change is read back afterwards too.
func TestLiveDryRunPredictionsReadRealState(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
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
	assertLifecyclePRHarnessStored(t, openPR, openBranch, "master")

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
	assertLifecyclePRStored(t, readLifecyclePR(t, declinedPR), map[string]any{"state": "DECLINED", "sourceBranch": declinedBranch})

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
	assertLifecyclePRStored(t, readLifecyclePR(t, mergedPR), map[string]any{"state": "MERGED", "sourceBranch": mergedBranch})

	t.Run("opening a pull request the branch already has", func(t *testing.T) {
		liveRefuses(t, apperrors.KindConflict, "pr", "create", "--from-ref", openBranch, "--to-ref", "master", "--title", "Second")
	})

	// Bitbucket refuses a merge, decline or reopen of a pull request already
	// in that state rather than accepting it as nothing to do, so the verdict
	// is that it would fail, as the real run does.
	t.Run("merging one that is already merged", func(t *testing.T) {
		liveVerdictHolds(t, apperrors.KindConflict, "pr", "merge", mergedPR)
	})

	t.Run("merging one that was declined", func(t *testing.T) {
		liveRefuses(t, apperrors.KindConflict, "pr", "merge", declinedPR)
	})

	// The other side of the same prediction: nothing stands against this one,
	// so it must not be reported as blocked. A preview that says blocked for
	// everything is as useless as one that says merge for everything, and only
	// having both cases against a real server tells them apart.
	t.Run("merging one nothing stands against", func(t *testing.T) {
		livePredicts(t, jsonoutput.OutcomeWouldApply, "pr", "merge", openPR)

		// And it is still open: this merge would succeed, so a preview that
		// performed it would leave it merged.
		assertLifecyclePRStored(t, readLifecyclePR(t, openPR), map[string]any{"state": "OPEN"})
	})

	t.Run("declining one that is already declined", func(t *testing.T) {
		liveVerdictHolds(t, apperrors.KindConflict, "pr", "decline", declinedPR)
	})

	t.Run("reopening one that is open", func(t *testing.T) {
		liveVerdictHolds(t, apperrors.KindConflict, "pr", "reopen", openPR)
		assertLifecyclePRStored(t, readLifecyclePR(t, openPR), map[string]any{"state": "OPEN"})
	})

	reviewer, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, reviewer.Username,
		openapigenerated.SetPermissionForUserParamsPermissionREPOWRITE); err != nil {
		t.Fatalf("grant repository permission failed: %v", err)
	}

	// Removing a user who is not a reviewer goes through and changes nothing.
	t.Run("removing a user who is not a reviewer", func(t *testing.T) {
		liveGoesThroughAsPredicted(t, jsonoutput.OutcomeNoOp, "reviewer is not present",
			"pr", "review", "reviewer", "remove", openPR, "--user", reviewer.Username, "--yes")
	})

	t.Run("adding a reviewer who is already one", func(t *testing.T) {
		if _, err := executeLiveCLI(t, "--json", "pr", "review", "reviewer", "add", openPR, "--user", reviewer.Username); err != nil {
			t.Fatalf("adding the reviewer failed: %v", err)
		}
		if added := mcpLiveReviewer(t, readLifecyclePR(t, openPR), reviewer.Username); added["role"] != "REVIEWER" {
			t.Fatalf("%s is on pull request %s as %v, want REVIEWER", reviewer.Username, openPR, added["role"])
		}

		livePredicts(t, jsonoutput.OutcomeNoOp, "pr", "review", "reviewer", "add", openPR, "--user", reviewer.Username)
	})

	// Two removals Bitbucket refuses: the author, with a 409, and a user who
	// does not exist, with a 404. admin opened the pull request.
	t.Run("removing the author, or somebody who does not exist", func(t *testing.T) {
		liveVerdictHolds(t, apperrors.KindConflict, "pr", "review", "reviewer", "remove", openPR, "--user", "admin", "--yes")
		liveVerdictHolds(t, apperrors.KindNotFound, "pr", "review", "reviewer", "remove", openPR, "--user", "no-such-reviewer", "--yes")
	})

	t.Run("code insights read the report and its annotations", func(t *testing.T) {
		commit := repo.CommitIDs[0]
		const reportKey = "prediction-report"
		const externalID = "prediction-annotation"

		// Nothing there yet, so both are creates and both deletes are no-ops.
		livePredictsSaying(t, "will be created", "insights", "report", "set", commit, reportKey,
			"--body", `{"title":"Predicted","result":"PASS"}`)
		assertQualityCLIReportGone(t, commit, reportKey)
		livePredicts(t, jsonoutput.OutcomeNoOp, "insights", "report", "delete", commit, reportKey)

		mustLiveCLI(t, "insights", "report", "set", commit, reportKey,
			"--body", `{"title":"Predicted","result":"PASS"}`)
		assertQualityCLIReportStored(t, commit, reportKey, "Predicted", "PASS")
		mustLiveCLI(t, "insights", "annotation", "add", commit, reportKey,
			"--body", fmt.Sprintf(`[{"externalId":%q,"message":"note","severity":"LOW"}]`, externalID))
		assertListedInsightAnnotation(t, mustLiveCLI(t, "insights", "annotation", "list", commit, reportKey), externalID,
			map[string]any{"message": "note", "severity": "LOW"})

		// And now the same four commands answer differently, which is the whole
		// point: the prediction is about the server, not about the arguments.
		livePredictsSaying(t, "will be updated", "insights", "report", "set", commit, reportKey,
			"--body", `{"title":"Predicted again","result":"PASS"}`)
		livePredicts(t, jsonoutput.OutcomeWouldApply, "insights", "annotation", "delete", commit, reportKey, "--external-id", externalID)
		livePredicts(t, jsonoutput.OutcomeNoOp, "insights", "annotation", "delete", commit, reportKey, "--external-id", "never-added")
		livePredicts(t, jsonoutput.OutcomeWouldApply, "insights", "report", "delete", commit, reportKey)

		// None of the three changes they predicted was made.
		assertQualityCLIReportStored(t, commit, reportKey, "Predicted", "PASS")
		assertListedInsightAnnotation(t, mustLiveCLI(t, "insights", "annotation", "list", commit, reportKey), externalID,
			map[string]any{"message": "note", "severity": "LOW"})
	})

	// Approval is the author's blind spot, and the mock did not have one.
	//
	// Its fixture listed alice as an APPROVED reviewer and ran the preview as
	// alice, who was also the author. Bitbucket refuses that outright --
	// "Authors may not update their status" -- so the state the no-op was
	// predicted from is one no pull request can be in. The reviewer has to be
	// somebody else, and the preview has to run as them, which is why this runs
	// last: it changes who the CLI is.
	t.Run("approving one already approved", func(t *testing.T) {
		configureLiveCLIEnvForUser(t, harness, seeded.Key, repo.Slug, reviewer)

		if _, err := executeLiveCLI(t, "--json", "pr", "review", "approve", openPR); err != nil {
			t.Fatalf("approve failed: %v", err)
		}
		if approved := mcpLiveReviewer(t, readLifecyclePR(t, openPR), reviewer.Username); approved["status"] != "APPROVED" {
			t.Fatalf("%s's review of pull request %s reads back as %v after the approve", reviewer.Username, openPR, approved["status"])
		}

		livePredicts(t, jsonoutput.OutcomeNoOp, "pr", "review", "approve", openPR)
	})
}

// TestLiveGovernanceDryRunPredictionsReadRealState is the same question for the
// settings commands: reviewer conditions, repository permissions, workflow
// webhooks, pull-request settings and commit comments.
//
// Every prediction here is a comparison against what the repository currently
// holds -- this webhook already exists, this group already has that permission,
// this setting is already the value being set. The mocked version supplied both
// sides of each comparison, so it could only ever agree with itself.
func TestLiveGovernanceDryRunPredictionsReadRealState(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	t.Run("reviewer conditions", func(t *testing.T) {
		user, err := harness.createLicensedUser(ctx)
		if err != nil {
			t.Fatalf("create user failed: %v", err)
		}

		// A default reviewer has to be able to see the project, or the create is
		// a 404 naming the user rather than anything about the condition.
		if err := harness.grantProjectPermission(ctx, seeded.Key, user.Username, "PROJECT_READ"); err != nil {
			t.Fatalf("grant project permission failed: %v", err)
		}

		// By numeric id: Bitbucket accepts {"name": ...} here and then rejects
		// the request with "User with ID -1 does not exist", which is the same
		// trap the reviewer groups had (#533).
		userID, err := harness.userID(ctx, user.Username)
		if err != nil {
			t.Fatalf("look up the user id failed: %v", err)
		}

		condition := fmt.Sprintf(
			`{"sourceMatcher":{"id":"ANY_REF","type":{"id":"ANY_REF"}},`+
				`"targetMatcher":{"id":"ANY_REF","type":{"id":"ANY_REF"}},`+
				`"reviewers":[{"id":%d}],"requiredApprovals":1}`, userID)
		stored := maskedCondition{approvals: 1, reviewerID: userID, sourceType: "ANY_REF", targetType: "ANY_REF"}

		// Nothing there yet. The update sends a whole condition: Bitbucket
		// checks the body before it looks for the condition, so a partial one
		// would be refused as invalid and say nothing about a missing one.
		liveVerdictHolds(t, apperrors.KindNotFound, "reviewer", "condition", "delete", "999999", "--project", seeded.Key, "--yes")
		liveRefuses(t, apperrors.KindNotFound, "reviewer", "condition", "update", "999999", condition, "--project", seeded.Key)

		projectCondition := conditionIDFrom(t, mustLiveCLI(t, "reviewer", "condition", "create", condition, "--project", seeded.Key))
		projectListing := mustLiveCLI(t, "reviewer", "condition", "list", "--project", seeded.Key)
		if listed, found := maskedConditionFrom(t, projectListing, projectCondition); !found {
			t.Fatalf("the project condition just created (id %s) is not in the listing:\n%s", projectCondition, projectListing)
		} else {
			projectStored := stored
			projectStored.scope = "PROJECT"
			assertMaskedConditionStored(t, listed, projectStored)
		}

		// And now the identical condition is a duplicate, which the preview did
		// not add. Bitbucket would: it stores a second identical condition
		// rather than refusing it, so the verdict is a create that says what it
		// duplicates.
		livePredictsSaying(t, "equivalent reviewer condition already exists", "reviewer", "condition", "create", condition, "--project", seeded.Key)
		if after := mustLiveCLI(t, "reviewer", "condition", "list", "--project", seeded.Key); after != projectListing {
			t.Fatalf("the dry run changed the project's conditions\nbefore: %s\nafter:  %s", projectListing, after)
		}

		// The repository scope is a separate code path with its own listing, so
		// it gets the same three questions rather than being assumed to follow.
		created := mustLiveCLI(t, "reviewer", "condition", "create", condition, "--repo", repoRef)
		conditionID := fmt.Sprintf("%d", int(decodeJSONMap(t, created)["id"].(float64)))
		repoListing := mustLiveCLI(t, "reviewer", "condition", "list", "--repo", repoRef)
		if listed, found := maskedConditionFrom(t, repoListing, conditionID); !found {
			t.Fatalf("the repository condition just created (id %s) is not in the listing:\n%s", conditionID, repoListing)
		} else {
			assertMaskedConditionStored(t, listed, stored)
		}

		livePredictsSaying(t, "equivalent reviewer condition already exists", "reviewer", "condition", "create", condition, "--repo", repoRef)
		livePredicts(t, jsonoutput.OutcomeWouldApply, "reviewer", "condition", "delete", conditionID, "--repo", repoRef)
		livePredictsSaying(t, "will be updated", "reviewer", "condition", "update", conditionID,
			strings.Replace(condition, `"requiredApprovals":1`, `"requiredApprovals":2`, 1), "--repo", repoRef)

		// A duplicate, a delete and two approvals were predicted, and none made.
		if after := mustLiveCLI(t, "reviewer", "condition", "list", "--repo", repoRef); after != repoListing {
			t.Fatalf("the dry runs changed the repository's conditions\nbefore: %s\nafter:  %s", repoListing, after)
		}
	})

	t.Run("repository permissions", func(t *testing.T) {
		user, err := harness.createLicensedUser(ctx)
		if err != nil {
			t.Fatalf("create user failed: %v", err)
		}
		if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, user.Username,
			openapigenerated.SetPermissionForUserParamsPermissionREPOWRITE); err != nil {
			t.Fatalf("grant repository permission failed: %v", err)
		}
		assertMutatedRepoPermissionLevel(t, repoRef, false, user.Username, "REPO_WRITE")

		livePredicts(t, jsonoutput.OutcomeNoOp, "repo", "settings", "security", "permissions", "users", "grant", user.Username, "repo_write")
		livePredicts(t, jsonoutput.OutcomeNoOp, "repo", "settings", "security", "permissions", "users", "revoke", "nobody-has-this-name")

		mustLiveCLI(t, "repo", "settings", "security", "permissions", "groups", "grant", licensedGroup, "repo_read")
		assertMutatedRepoPermissionLevel(t, repoRef, true, licensedGroup, "REPO_READ")

		livePredicts(t, jsonoutput.OutcomeNoOp, "repo", "settings", "security", "permissions", "groups", "grant", licensedGroup, "repo_read")
		livePredicts(t, jsonoutput.OutcomeWouldApply, "repo", "settings", "security", "permissions", "groups", "revoke", licensedGroup)

		// The revoke it predicted was not made.
		assertMutatedRepoPermissionLevel(t, repoRef, true, licensedGroup, "REPO_READ")
	})

	t.Run("workflow webhooks", func(t *testing.T) {
		const name = "predicted-hook"
		const url = "http://example.invalid/predicted"

		liveVerdictHolds(t, apperrors.KindNotFound, "repo", "settings", "workflow", "webhooks", "delete", "999999", "--yes")

		mustLiveCLI(t, "repo", "settings", "workflow", "webhooks", "create", name, url)
		listing := mustLiveCLI(t, "repo", "settings", "workflow", "webhooks", "list")
		hooks := repoCLIWebhooksIn(t, listing)
		if len(hooks) != 1 {
			t.Fatalf("want the one webhook just created, got %d: %v", len(hooks), hooks)
		}
		// Active and on repo:refs_changed are what bb sends when no flag says
		// otherwise.
		hook, _ := hooks[0].(map[string]any)
		repoCLIAssertWebhook(t, hook, name, url, true, "repo:refs_changed")

		// A second webhook with the same name and URL is one Bitbucket adds
		// beside the first rather than refusing, so the verdict is a create
		// that says so -- and the dry run adds nothing.
		livePredictsSaying(t, "already exists", "repo", "settings", "workflow", "webhooks", "create", name, url)
		if after := mustLiveCLI(t, "repo", "settings", "workflow", "webhooks", "list"); after != listing {
			t.Fatalf("the dry run changed the webhooks\nbefore: %s\nafter:  %s", listing, after)
		}
	})

	// The pull-request settings are read back and compared, so the preview has
	// to know what the repository is set to right now -- which is whatever
	// Bitbucket defaults a fresh repository to, not what a fixture says.
	t.Run("pull request settings", func(t *testing.T) {
		mustLiveCLI(t, "repo", "settings", "pull-requests", "update", "--required-all-tasks-complete=true")
		if settings := repoCLIPullRequestSettings(t); settings["requiredAllTasksComplete"] != true {
			t.Fatalf("requiredAllTasksComplete reads back as %v, want true", settings["requiredAllTasksComplete"])
		}
		livePredicts(t, jsonoutput.OutcomeNoOp, "repo", "settings", "pull-requests", "update", "--required-all-tasks-complete=true")

		mustLiveCLI(t, "repo", "settings", "pull-requests", "update-approvers", "--count", "2")
		if got := approverCountFrom(t, repoCLIPullRequestSettings(t)); got != "2" {
			t.Fatalf("requiredApprovers reads back as %s, want 2", got)
		}
		livePredicts(t, jsonoutput.OutcomeNoOp, "repo", "settings", "pull-requests", "update-approvers", "--count", "2")

		mustLiveCLI(t, "repo", "settings", "pull-requests", "set-strategy", "squash")
		if settings := repoCLIPullRequestSettings(t); settings["defaultMergeStrategy"] != "squash" || !lifecycleStrategyEnabled(settings, "squash") {
			t.Fatalf("merge strategies read back as %v, want squash enabled and the default", settings)
		}
		livePredicts(t, jsonoutput.OutcomeNoOp, "repo", "settings", "pull-requests", "set-strategy", "squash")

		// The other prediction, and the thing a preview must never do. Asking
		// for the opposite of what is set predicts an update -- and the
		// settings afterwards still say what they said, because a dry run that
		// writes is the defect the whole tier exists to prevent.
		livePredicts(t, jsonoutput.OutcomeWouldApply, "repo", "settings", "pull-requests", "update", "--required-all-tasks-complete=false")

		after := mustLiveCLI(t, "repo", "settings", "pull-requests", "get")
		if allTasks, _ := decodeJSONMap(t, after)["requiredAllTasksComplete"].(bool); !allTasks {
			t.Fatalf("the dry run wrote the change it only predicted:\n%s", after)
		}
	})

	t.Run("commit comments", func(t *testing.T) {
		const text = "a comment the preview compares against"
		commit := repo.CommitIDs[0]

		output := mustLiveCLI(t, "repo", "comment", "create", "--commit", commit, "--text", text)
		comment, _ := decodeJSONMap(t, output)["comment"].(map[string]any)
		id, ok := comment["id"].(float64)
		if !ok {
			t.Fatalf("the created comment has no id:\n%s", output)
		}
		commentID := strconv.Itoa(int(id))
		stored := commitCommentStored(t, seeded.Key, repo.Slug, commit, commentID)
		if stored["text"] != text {
			t.Fatalf("comment %s reads back as %q, want %q", commentID, stored["text"], text)
		}

		livePredicts(t, jsonoutput.OutcomeNoOp, "repo", "comment", "update", "--commit", commit, "--id", commentID, "--text", text)
		livePredicts(t, jsonoutput.OutcomeWouldApply, "repo", "comment", "update", "--commit", commit, "--id", commentID, "--text", text+" changed")

		// The text the update would have set is not there, and neither is the
		// version it would have moved to.
		if after := commitCommentStored(t, seeded.Key, repo.Slug, commit, commentID); after["text"] != text || after["version"] != stored["version"] {
			t.Fatalf("comment %s reads back as %q at version %v after the dry runs, was %q at %v",
				commentID, after["text"], after["version"], text, stored["version"])
		}
	})
}

// TestLiveDryRunRefusalsFailAsTheRealRunDoes holds each refusal a pull request
// or settings preview predicts to what the real run does.
//
// A would-fail verdict names the error the run would fail with, and a caller
// branches on its kind: not_found wants the target made, authorization a
// grant, conflict a change of state, validation a different request. Each case
// puts the target in the state the refusal is about, asks the dry run, then
// runs the command for real and requires the same kind from both.
func TestLiveDryRunRefusalsFailAsTheRealRunDoes(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	openPullRequest := func(t *testing.T, branch string) string {
		t.Helper()

		if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, strings.ReplaceAll(branch, "/", "-")+".txt"); err != nil {
			t.Fatalf("push %s failed: %v", branch, err)
		}
		id, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
		if err != nil {
			t.Fatalf("open a pull request from %s failed: %v", branch, err)
		}
		assertLifecyclePRHarnessStored(t, id, branch, "master")

		return id
	}

	const openBranch = "feature/refused-open"
	openPR := openPullRequest(t, openBranch)

	declinedPR := openPullRequest(t, "feature/refused-declined")
	mustLiveCLI(t, "pr", "decline", declinedPR)
	assertLifecyclePRStored(t, readLifecyclePR(t, declinedPR), map[string]any{"state": "DECLINED"})

	draftPR := openPullRequest(t, "feature/refused-draft")
	mustLiveCLI(t, "pr", "ready", draftPR, "--undo")
	assertLifecyclePRStored(t, readLifecyclePR(t, draftPR), map[string]any{"state": "OPEN", "draft": true})

	// Deleting what is not there is refused with a 404, not accepted as
	// nothing to do, so each of these is a verdict that the delete would fail.
	t.Run("deleting what is not there", func(t *testing.T) {
		for _, args := range [][]string{
			{"webhook", "delete", "999999"},
			{"project", "webhook", "delete", seeded.Key, "999999"},
			{"reviewer", "condition", "delete", "999999", "--repo", repoRef},
			{"reviewer-group", "delete", "999999", "--repo", repoRef},
			{"reviewer-group", "delete", "999999", "--project", seeded.Key},
			{"repo", "default-task", "delete", "999999"},
			{"repo", "comment", "delete", "--commit", repo.CommitIDs[0], "--id", "999999"},
		} {
			liveVerdictHolds(t, apperrors.KindNotFound, append(args, "--yes")...)
		}
	})

	t.Run("a pull request for branches that already have one", func(t *testing.T) {
		liveVerdictHolds(t, apperrors.KindConflict, "pr", "create", "--from-ref", openBranch, "--to-ref", "master", "--title", "Again")
	})

	t.Run("merging, rebasing or readying a declined pull request", func(t *testing.T) {
		liveVerdictHolds(t, apperrors.KindConflict, "pr", "merge", declinedPR)
		// Bitbucket's rebase check answers a declined pull request with no
		// vetoes at all, so this one is read from its state.
		liveVerdictHolds(t, apperrors.KindConflict, "pr", "rebase", declinedPR)
		liveVerdictHolds(t, apperrors.KindConflict, "pr", "ready", declinedPR)

		assertLifecyclePRStored(t, readLifecyclePR(t, declinedPR), map[string]any{"state": "DECLINED"})
	})

	t.Run("merging a draft", func(t *testing.T) {
		// Bitbucket's merge check does not know about drafts, and the merge
		// refuses one: the verdict has to come from the draft flag.
		liveVerdictHolds(t, apperrors.KindConflict, "pr", "merge", draftPR)

		assertLifecyclePRStored(t, readLifecyclePR(t, draftPR), map[string]any{"state": "OPEN", "draft": true})
	})

	t.Run("completing a review that was never started", func(t *testing.T) {
		liveVerdictHolds(t, apperrors.KindNotFound, "pr", "review", "complete", openPR)
	})

	t.Run("merging past a required approval", func(t *testing.T) {
		mustLiveCLI(t, "repo", "settings", "pull-requests", "update-approvers", "--count", "1")
		if got := approverCountFrom(t, repoCLIPullRequestSettings(t)); got != "1" {
			t.Fatalf("requiredApprovers reads back as %s, want 1", got)
		}

		liveVerdictHolds(t, apperrors.KindConflict, "pr", "merge", openPR)

		assertLifecyclePRStored(t, readLifecyclePR(t, openPR), map[string]any{"state": "OPEN"})
	})

	t.Run("rebasing a source branch the caller may not update", func(t *testing.T) {
		// Something to rebase onto, so the rebase would change the branch.
		if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, "master", "moved-on.txt"); err != nil {
			t.Fatalf("push to master failed: %v", err)
		}
		readOnly := restrictionID(t, mustLiveCLI(t, "branch", "restriction", "create",
			"--type", "read-only", "--matcher-type", "BRANCH", "--matcher-id", "refs/heads/"+openBranch))
		assertRestrictionStored(t, restrictionPayload(t, mustLiveCLI(t, "branch", "restriction", "get", readOnly)),
			storedRestriction{scope: "REPOSITORY", restrictionType: "read-only", matcherType: "BRANCH", matcherID: "refs/heads/" + openBranch})

		// Bitbucket reports the branch permission as a veto, and does not
		// refuse the rebase up front for it: the rebase runs, and updating the
		// branch is then vetoed with a 400.
		before := readLifecyclePR(t, openPR)
		liveVerdictHolds(t, apperrors.KindValidation, "pr", "rebase", openPR)
		assertLifecyclePRStored(t, readLifecyclePR(t, openPR), map[string]any{"version": before["version"]})
	})

	t.Run("updating a default task that is not there", func(t *testing.T) {
		liveVerdictHolds(t, apperrors.KindNotFound, "repo", "default-task", "update", "999999", "--description", "refused")
	})

	reviewerID, err := harness.userID(ctx, harness.username())
	if err != nil {
		t.Fatalf("look up the reviewer's id failed: %v", err)
	}
	condition := fmt.Sprintf(`{"sourceMatcher":{"id":"ANY_REF","type":{"id":"ANY_REF"}},`+
		`"targetMatcher":{"id":"ANY_REF","type":{"id":"ANY_REF"}},"reviewers":[{"id":%d}],"requiredApprovals":1}`, reviewerID)

	t.Run("updating a reviewer condition that is not there", func(t *testing.T) {
		liveVerdictHolds(t, apperrors.KindNotFound, "reviewer", "condition", "update", "999999", condition, "--project", seeded.Key)
		liveVerdictHolds(t, apperrors.KindNotFound, "reviewer", "condition", "update", "999999", condition, "--repo", repoRef)
	})

	t.Run("a reviewer condition Bitbucket will not store", func(t *testing.T) {
		before := mustLiveCLI(t, "reviewer", "condition", "list", "--project", seeded.Key)

		// Checked before anything else, the condition an update names included:
		// a body without its matchers is invalid whether or not 999999 exists.
		liveVerdictHolds(t, apperrors.KindValidation, "reviewer", "condition", "update", "999999", `{"requiredApprovals":2}`, "--repo", repoRef)
		liveVerdictHolds(t, apperrors.KindValidation, "reviewer", "condition", "create",
			strings.Replace(condition, fmt.Sprintf(`"reviewers":[{"id":%d}],`, reviewerID), "", 1), "--project", seeded.Key)
		liveVerdictHolds(t, apperrors.KindValidation, "reviewer", "condition", "create",
			strings.Replace(condition, `,"requiredApprovals":1`, "", 1), "--project", seeded.Key)
		// A reviewer by name is looked up as user -1.
		liveVerdictHolds(t, apperrors.KindNotFound, "reviewer", "condition", "create",
			strings.Replace(condition, fmt.Sprintf(`{"id":%d}`, reviewerID), fmt.Sprintf(`{"name":%q}`, harness.username()), 1), "--project", seeded.Key)

		if after := mustLiveCLI(t, "reviewer", "condition", "list", "--project", seeded.Key); after != before {
			t.Fatalf("a refused condition was stored\nbefore: %s\nafter:  %s", before, after)
		}
	})

	t.Run("reviewer groups that are missing or already there", func(t *testing.T) {
		const name = "refused-group"

		for _, scope := range []struct {
			flags []string
			name  string
		}{
			{[]string{"--project", seeded.Key}, "PROJECT"},
			{[]string{"--repo", repoRef}, "REPOSITORY"},
		} {
			liveVerdictHolds(t, apperrors.KindNotFound, append([]string{"reviewer-group", "update", "999999", "--description", "refused"}, scope.flags...)...)
			liveVerdictHolds(t, apperrors.KindNotFound, append([]string{"reviewer-group", "update", "no-such-group", "--description", "refused"}, scope.flags...)...)

			// The repository's own group: its listing has the project's beside
			// it by now, under the same name.
			mustLiveCLI(t, append([]string{"reviewer-group", "create", name, "--users", harness.username()}, scope.flags...)...)
			owned := 0
			for _, group := range liveReviewerGroupsNamed(t, mustLiveCLI(t, append([]string{"reviewer-group", "list"}, scope.flags...)...), name) {
				if group["scope"] == scope.name {
					owned++
				}
			}
			if owned != 1 {
				t.Fatalf("want the one %s group %q just created, found %d", scope.name, name, owned)
			}

			liveVerdictHolds(t, apperrors.KindConflict, append([]string{"reviewer-group", "create", name, "--users", harness.username()}, scope.flags...)...)
		}
	})
}

// TestLiveDryRunCommentVerdictsFollowWhoMayChangeThem covers who Bitbucket lets
// change a comment, which a preview has to know to say whether the run goes
// through.
//
// Only its author may edit a comment. Its author or a repository admin may
// delete one, and nobody may while it has replies. The preview refused every
// change to somebody else's comment as a conflict, so it refused an admin a
// delete Bitbucket performs, and it named the wrong kind for the refusals it
// did get right.
func TestLiveDryRunCommentVerdictsFollowWhoMayChangeThem(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	commit := repo.CommitIDs[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// May write to the repository, and so comment on it, and does not
	// administer it.
	writer, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, writer.Username,
		openapigenerated.SetPermissionForUserParamsPermissionREPOWRITE); err != nil {
		t.Fatalf("grant repository permission failed: %v", err)
	}
	assertMutatedRepoPermissionLevel(t, seeded.Key+"/"+repo.Slug, false, writer.Username, "REPO_WRITE")

	comment := func(t *testing.T, text string, extra ...string) string {
		t.Helper()

		output := mustLiveCLI(t, append([]string{"repo", "comment", "create", "--commit", commit, "--text", text}, extra...)...)
		created, _ := decodeJSONMap(t, output)["comment"].(map[string]any)
		id, ok := created["id"].(float64)
		if !ok {
			t.Fatalf("the created comment has no id:\n%s", output)
		}
		commentID := strconv.Itoa(int(id))
		if stored := commitCommentStored(t, seeded.Key, repo.Slug, commit, commentID); stored["text"] != text {
			t.Fatalf("comment %s reads back as %q, want %q", commentID, stored["text"], text)
		}

		return commentID
	}
	assertText := func(t *testing.T, commentID, text string) {
		t.Helper()

		if stored := commitCommentStored(t, seeded.Key, repo.Slug, commit, commentID); stored["text"] != text {
			t.Fatalf("comment %s reads back as %q, want %q", commentID, stored["text"], text)
		}
	}

	const adminText = "the admin's comment"
	adminComment := comment(t, adminText)

	t.Run("somebody else editing or deleting it", func(t *testing.T) {
		setLiveCredentials(t, writer)

		liveVerdictHolds(t, apperrors.KindAuthorization, "repo", "comment", "update", "--commit", commit, "--id", adminComment, "--text", "rewritten")
		liveVerdictHolds(t, apperrors.KindAuthorization, "repo", "comment", "delete", "--commit", commit, "--id", adminComment, "--yes")
		assertText(t, adminComment, adminText)
	})

	var writerComment string
	t.Run("the writer comments", func(t *testing.T) {
		setLiveCredentials(t, writer)

		writerComment = comment(t, "the writer's comment")
	})

	t.Run("a repository admin deleting somebody else's", func(t *testing.T) {
		liveGoesThroughAsPredicted(t, jsonoutput.OutcomeWouldApply, "will be deleted",
			"repo", "comment", "delete", "--commit", commit, "--id", writerComment, "--yes")
		assertCommitCommentGone(t, seeded.Key, repo.Slug, commit, writerComment)
	})

	t.Run("deleting one that has replies", func(t *testing.T) {
		parent := comment(t, "a comment with a reply")
		reply := comment(t, "its reply", "--parent", parent)

		liveVerdictHolds(t, apperrors.KindConflict, "repo", "comment", "delete", "--commit", commit, "--id", parent, "--yes")
		assertText(t, parent, "a comment with a reply")
		assertText(t, reply, "its reply")
	})
}

// liveReviewerGroupsNamed is the entries of a reviewer-group listing called
// name, with their scope: a repository's listing carries its project's groups
// too, and a group name can be in both.
func liveReviewerGroupsNamed(t *testing.T, listing, name string) []map[string]any {
	t.Helper()

	groups, ok := decodeJSONMap(t, listing)["reviewerGroups"].([]any)
	if !ok {
		t.Fatalf("expected a reviewerGroups array in: %s", listing)
	}

	var named []map[string]any
	for _, entry := range groups {
		if group, ok := entry.(map[string]any); ok && group["name"] == name {
			named = append(named, group)
		}
	}

	return named
}

// livePredicts runs one change under --dry-run and requires the verdict to be
// outcome.
func livePredicts(t *testing.T, outcome jsonoutput.Outcome, args ...string) {
	t.Helper()

	assertLivePreview(t, mustLiveCLI(t, append([]string{"--dry-run"}, args...)...), outcome)
}

// livePredictsSaying is livePredicts for a change that would go through, where
// the test is about which change it is: create or update, or a create beside
// something already there. The verdict calls all of them would-apply, and the
// reason says which.
func livePredictsSaying(t *testing.T, reason string, args ...string) {
	t.Helper()

	assertLivePreview(t, mustLiveCLI(t, append([]string{"--dry-run"}, args...)...), jsonoutput.OutcomeWouldApply, reason)
}

// liveRefuses runs one change under --dry-run and requires the verdict that it
// would fail, with an error of kind.
func liveRefuses(t *testing.T, kind apperrors.Kind, args ...string) {
	t.Helper()

	assertLiveRefusal(t, mustLiveCLI(t, append([]string{"--dry-run"}, args...)...), kind)
}

// liveVerdictHolds runs one change under --dry-run and then for real, and
// requires both to end the same way: failing with an error of kind.
//
// The real run is the only authority on what the real run does, which is the
// whole claim of a verdict (ADR-096). A kind worked out from the code agrees
// with the code by construction; half the refusals here predicted conflict, the
// default, where the real run failed with not_found, authorization or
// validation.
//
// The verdict is taken the way the binary reports it. A preview the command
// wrote carries it in preview.error. A failure its check ran into -- a 404
// from the lookup -- returns from Execute here, and cmd/bb writes that error
// into preview.error, so its kind is the verdict's.
func liveVerdictHolds(t *testing.T, kind apperrors.Kind, args ...string) {
	t.Helper()

	var predicted apperrors.Kind
	output, err := executeLiveCLI(t, append([]string{"--json", "--dry-run"}, args...)...)
	switch {
	case err == nil:
		predicted = assertLivePreview(t, output, jsonoutput.OutcomeWouldFail).Preview.Error.Kind
	case jsonoutput.IsVerdict(err):
		predicted = apperrors.KindOf(err)
	default:
		t.Fatalf("the dry run reached no verdict on %s: %v\n%s", strings.Join(args, " "), err, output)
	}

	realOutput, realErr := executeLiveCLI(t, append([]string{"--json"}, args...)...)
	if realErr == nil {
		t.Fatalf("the dry run said %s would fail with %s, and the real run went through:\n%s",
			strings.Join(args, " "), predicted, realOutput)
	}
	if real := apperrors.KindOf(realErr); predicted != kind || real != kind {
		t.Fatalf("%s: the dry run predicted %s and the real run failed with %s, want %s both times: %v",
			strings.Join(args, " "), predicted, real, kind, realErr)
	}
}

// liveGoesThroughAsPredicted runs one change under --dry-run, requires the
// verdict outcome with a reason saying reason, and then runs it for real,
// which has to go through. It returns what the real run wrote, for the caller
// to read back what it did.
//
// For the changes an earlier preview refused and Bitbucket does not: a
// prediction of failure is proven wrong by the real run succeeding, and that
// is what each of these shows before it shows what the success did.
func liveGoesThroughAsPredicted(t *testing.T, outcome jsonoutput.Outcome, reason string, args ...string) string {
	t.Helper()

	assertLivePreview(t, mustLiveCLI(t, append([]string{"--dry-run"}, args...)...), outcome, reason)

	return mustLiveCLI(t, args...)
}
