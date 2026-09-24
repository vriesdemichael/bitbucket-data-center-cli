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

	t.Run("merging one that is already merged", func(t *testing.T) {
		livePredicts(t, jsonoutput.OutcomeNoOp, "pr", "merge", mergedPR)
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
		livePredicts(t, jsonoutput.OutcomeNoOp, "pr", "decline", declinedPR)
	})

	t.Run("reopening one that is open", func(t *testing.T) {
		livePredicts(t, jsonoutput.OutcomeNoOp, "pr", "reopen", openPR)
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
		if added := mcpLiveReviewer(t, readLifecyclePR(t, openPR), reviewer.Username); added["role"] != "REVIEWER" {
			t.Fatalf("%s is on pull request %s as %v, want REVIEWER", reviewer.Username, openPR, added["role"])
		}

		livePredicts(t, jsonoutput.OutcomeNoOp, "pr", "review", "reviewer", "add", openPR, "--user", reviewer.Username)
	})

	t.Run("removing a reviewer who is not one", func(t *testing.T) {
		livePredicts(t, jsonoutput.OutcomeNoOp, "pr", "review", "reviewer", "remove", openPR, "--user", "no-such-reviewer")
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
		livePredicts(t, jsonoutput.OutcomeNoOp, "reviewer", "condition", "delete", "999999", "--project", seeded.Key)
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

		livePredicts(t, jsonoutput.OutcomeNoOp, "repo", "settings", "workflow", "webhooks", "delete", "999999")

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
