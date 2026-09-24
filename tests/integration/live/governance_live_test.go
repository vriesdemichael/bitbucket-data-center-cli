//go:build live

package live_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

func TestLiveGovernanceCLI(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// --- Issue 31: Group Permissions ---
	// Test listing groups (even if empty)
	output, err := executeLiveCLI(t, "--json", "project", "permissions", "groups", "list", seeded.Key)
	if err != nil {
		t.Fatalf("project group permissions list failed: %v\noutput: %s", err, output)
	}
	if !strings.Contains(output, `"subject": "group"`) {
		t.Fatalf("expected a group listing in output: %s", output)
	}

	output, err = executeLiveCLI(t, "--json", "repo", "settings", "security", "permissions", "groups", "list", "--repo", seeded.Key+"/"+repo.Slug)
	if err != nil {
		t.Fatalf("repo group permissions list failed: %v\noutput: %s", err, output)
	}
	if !strings.Contains(output, `"subject": "group"`) {
		t.Fatalf("expected a group listing in output: %s", output)
	}

	// stash-users is the group every licensed user is in, so the grant is not a
	// maybe. Both of these discarded their result, which is a command that can
	// stop working without anything noticing.
	//
	// Write rather than read, and each read back from its own listing: a level
	// above the lowest cannot be matched by a grant that lost its level.
	grantOutput, err := executeLiveCLI(t, "project", "permissions", "groups", "grant", seeded.Key, "stash-users", "PROJECT_WRITE")
	if err != nil {
		t.Fatalf("project group permission grant failed: %v\noutput: %s", err, grantOutput)
	}
	projectGroups := mustLiveCLI(t, "project", "permissions", "groups", "list", seeded.Key)
	if held := governanceHeldPermission(t, projectGroups, "stash-users"); held != "PROJECT_WRITE" {
		t.Errorf("stash-users holds %q on the project, want PROJECT_WRITE:\n%s", held, projectGroups)
	}

	grantOutput, err = executeLiveCLI(t, "repo", "settings", "security", "permissions", "groups", "grant", "stash-users", "REPO_WRITE", "--repo", seeded.Key+"/"+repo.Slug)
	if err != nil {
		t.Fatalf("repo group permission grant failed: %v\noutput: %s", err, grantOutput)
	}
	repoGroups := mustLiveCLI(t, "repo", "settings", "security", "permissions", "groups", "list", "--repo", repoRef)
	if held := governanceHeldPermission(t, repoGroups, "stash-users"); held != "REPO_WRITE" {
		t.Errorf("stash-users holds %q on the repository, want REPO_WRITE:\n%s", held, repoGroups)
	}

	// Test listing reviewer conditions
	output, err = executeLiveCLI(t, "--json", "reviewer", "condition", "list", "--project", seeded.Key)
	if err != nil {
		t.Fatalf("project reviewer list failed: %v\noutput: %s", err, output)
	}
	if !strings.Contains(output, `"conditions"`) {
		t.Fatalf("expected conditions in output: %s", output)
	}

	// Reviewer condition lifecycle, asserted rather than attempted. Create,
	// update and delete used to run inside an `if err == nil` with their own
	// errors discarded, so all three were untested whenever the create failed --
	// and the id was sliced out of the text rather than decoded.
	//
	// The payload it used to send -- requiredApprovals on its own -- is one
	// Bitbucket refuses with "a sourceMatcher with ID and type is required",
	// which is how long that create had not run.
	reviewerID, err := harness.userID(ctx, harness.username())
	if err != nil {
		t.Fatalf("look up the reviewer id: %v", err)
	}
	condition := fmt.Sprintf(`{
		"sourceMatcher": {"id": "ANY_REF", "type": {"id": "ANY_REF"}},
		"targetMatcher": {"id": "refs/heads/master", "type": {"id": "BRANCH"}},
		"reviewers": [{"id": %d}],
		"requiredApprovals": 1
	}`, reviewerID)

	output, err = executeLiveCLI(t, "--json", "reviewer", "condition", "create", condition, "--repo", seeded.Key+"/"+repo.Slug)
	if err != nil {
		t.Fatalf("reviewer condition create failed: %v\noutput: %s", err, output)
	}
	conditionID, ok := numericOrStringID(decodeJSONMap(t, output)["id"])
	if !ok {
		t.Fatalf("the create answered without an id: %s", output)
	}

	// Each step is read back from a listing of its own rather than from what
	// the step answered.
	conditions := mustLiveCLI(t, "reviewer", "condition", "list", "--repo", repoRef)
	stored, found := governanceCondition(t, conditions, conditionID)
	if !found {
		t.Fatalf("condition %s is not in the listing after its create: %s", conditionID, conditions)
	}
	assertGovernanceConditionStored(t, stored, "", "refs/heads/master", reviewerID, 1)

	updateOutput, err := executeLiveCLI(t, "--json", "reviewer", "condition", "update", conditionID, strings.Replace(condition, "\"requiredApprovals\": 1", "\"requiredApprovals\": 2", 1), "--repo", seeded.Key+"/"+repo.Slug)
	if err != nil {
		t.Fatalf("reviewer condition update failed: %v\noutput: %s", err, updateOutput)
	}

	conditions = mustLiveCLI(t, "reviewer", "condition", "list", "--repo", repoRef)
	stored, found = governanceCondition(t, conditions, conditionID)
	if !found {
		t.Fatalf("condition %s is not in the listing after its update: %s", conditionID, conditions)
	}
	assertGovernanceConditionStored(t, stored, "", "refs/heads/master", reviewerID, 2)

	deleteOutput, err := executeLiveCLI(t, "--json", "reviewer", "condition", "delete", conditionID, "--repo", seeded.Key+"/"+repo.Slug, "--yes")
	if err != nil {
		t.Fatalf("reviewer condition delete failed: %v\noutput: %s", err, deleteOutput)
	}

	conditions = mustLiveCLI(t, "reviewer", "condition", "list", "--repo", repoRef)
	if _, found := governanceCondition(t, conditions, conditionID); found {
		t.Errorf("condition %s is still listed after its delete: %s", conditionID, conditions)
	}

	// --- Issue 33: PR Governance ---
	// Test getting PR settings
	output, err = executeLiveCLI(t, "--json", "repo", "settings", "pull-requests", "get", "--repo", seeded.Key+"/"+repo.Slug)
	if err != nil {
		t.Fatalf("repo PR settings get failed: %v\noutput: %s", err, output)
	}
	if !strings.Contains(output, `"requiredApprovers"`) {
		t.Fatalf("expected pull_request_settings in output: %s", output)
	}

	// Asserted, not attempted. This tolerated failure with a t.Logf, and so
	// hid that set-strategy could not set a strategy at all: it sent a default
	// with no enabled strategies, which Bitbucket refuses for every value.
	// A command that never worked looked covered.
	output, err = executeLiveCLI(t, "--json", "repo", "settings", "pull-requests", "set-strategy", "squash", "--repo", seeded.Key+"/"+repo.Slug)
	if err != nil {
		t.Fatalf("repo set-strategy failed: %v\noutput: %s", err, output)
	}
	if !strings.Contains(output, `"squash"`) {
		t.Fatalf("expected squash as the default merge strategy: %s", output)
	}

	// The other half of that defect, read back rather than recorded. Bitbucket
	// refuses a default that is not among the enabled strategies, so the
	// command has to send the enabled set along with it -- and it must send the
	// set that was there, not turn everything on. A unit test asserted this by
	// decoding the request body it had just been handed; here the settings are
	// read again afterwards and the server says what it kept.
	output, err = executeLiveCLI(t, "--json", "repo", "settings", "pull-requests", "get", "--repo", seeded.Key+"/"+repo.Slug)
	if err != nil {
		t.Fatalf("repo PR settings get after set-strategy failed: %v\noutput: %s", err, output)
	}

	var settingsPayload struct {
		DefaultMergeStrategy string `json:"defaultMergeStrategy"`
		MergeStrategies      []struct {
			ID      string `json:"id"`
			Enabled bool   `json:"enabled"`
		} `json:"mergeStrategies"`
	}
	if err := decodeJSONEnvelopeData(output, &settingsPayload); err != nil {
		t.Fatalf("decode pull request settings: %v\noutput: %s", err, output)
	}

	if settingsPayload.DefaultMergeStrategy != "squash" {
		t.Fatalf("the default strategy did not survive the write: %s", output)
	}
	enabled := map[string]bool{}
	for _, strategy := range settingsPayload.MergeStrategies {
		enabled[strategy.ID] = strategy.Enabled
	}
	if !enabled["squash"] {
		t.Errorf("squash is the default and is not enabled, which Bitbucket refuses:\n%s", output)
	}
	// no-ff is what a fresh repository has enabled, and it has to survive: the
	// command sends the enabled set with the default, and sending only the
	// default is the defect. The other direction would be as quiet -- turning
	// every strategy on because one was named -- so a strategy that was off
	// stays off.
	if !enabled["no-ff"] {
		t.Errorf("set-strategy turned off a strategy that was enabled:\n%s", output)
	}
	if enabled["ff-only"] {
		t.Errorf("set-strategy enabled a strategy nobody asked for:\n%s", output)
	}

	// Test listing merge checks
	output, err = executeLiveCLI(t, "--json", "repo", "settings", "pull-requests", "merge-checks", "list", "--repo", seeded.Key+"/"+repo.Slug)
	if err != nil {
		t.Fatalf("repo merge-checks list failed: %v\noutput: %s", err, output)
	}
	if !strings.Contains(output, `"checks"`) {
		t.Fatalf("expected merge_checks in output: %s", output)
	}
}

func TestLiveCLIProjectPermissionsUserGrantDryRunNoSideEffect(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	username := harness.username()

	configureLiveCLIEnv(t, harness, seeded.Key, seeded.Repos[0].Slug)

	listBeforeOutput, err := executeLiveCLI(t, "--json", "project", "permissions", "users", "list", seeded.Key, "--limit", "200")
	if err != nil {
		t.Fatalf("project permissions users list before failed: %v\noutput: %s", err, listBeforeOutput)
	}
	// The user must not hold the level already, or a grant that went through
	// would change nothing and the comparison below could not fail.
	if held := governanceHeldPermission(t, listBeforeOutput, username); held == "PROJECT_WRITE" {
		t.Fatalf("%s already holds PROJECT_WRITE, so the dry run could not show a side effect:\n%s", username, listBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "project", "permissions", "users", "grant", seeded.Key, username, "PROJECT_WRITE")
	if err != nil {
		t.Fatalf("project permissions users grant dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	assertLivePreviewOf(t, dryRunOutput, "project permissions users grant", jsonoutput.OutcomeWouldApply)

	listAfterOutput, err := executeLiveCLI(t, "--json", "project", "permissions", "users", "list", seeded.Key, "--limit", "200")
	if err != nil {
		t.Fatalf("project permissions users list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no project permission side-effect from dry-run\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}
}

func TestLiveCLIProjectPermissionsGroupGrantDryRunNoSideEffect(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	configureLiveCLIEnv(t, harness, seeded.Key, seeded.Repos[0].Slug)

	group := "stash-users"
	listBeforeOutput, err := executeLiveCLI(t, "--json", "project", "permissions", "groups", "list", seeded.Key, "--limit", "200")
	if err != nil {
		t.Fatalf("project permissions groups list before failed: %v\noutput: %s", err, listBeforeOutput)
	}
	// The group must not hold the level already, or a grant that went through
	// would change nothing and the comparison below could not fail.
	if held := governanceHeldPermission(t, listBeforeOutput, group); held == "PROJECT_READ" {
		t.Fatalf("%s already holds PROJECT_READ, so the dry run could not show a side effect:\n%s", group, listBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "project", "permissions", "groups", "grant", seeded.Key, group, "PROJECT_READ")
	if err != nil {
		t.Fatalf("project permissions groups grant dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	assertLivePreviewOf(t, dryRunOutput, "project permissions groups grant", jsonoutput.OutcomeWouldApply)

	listAfterOutput, err := executeLiveCLI(t, "--json", "project", "permissions", "groups", "list", seeded.Key, "--limit", "200")
	if err != nil {
		t.Fatalf("project permissions groups list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no project group permission side-effect from dry-run\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}
}

func TestLiveCLIProjectPermissionsUserRevokeDryRunNoSideEffect(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	configureLiveCLIEnv(t, harness, seeded.Key, seeded.Repos[0].Slug)

	// A user who holds a permission for the dry run to revoke. It used to name
	// a user who did not exist, and revoking nobody changes no listing, so the
	// comparison below could not have failed.
	holder, err := harness.createRestrictedUser(ctx)
	if err != nil {
		t.Fatalf("create the permission holder failed: %v", err)
	}
	mustLiveCLI(t, "project", "permissions", "users", "grant", seeded.Key, holder.Username, "PROJECT_READ")

	listBeforeOutput, err := executeLiveCLI(t, "--json", "project", "permissions", "users", "list", seeded.Key, "--limit", "200")
	if err != nil {
		t.Fatalf("project permissions users list before failed: %v\noutput: %s", err, listBeforeOutput)
	}
	if held := governanceHeldPermission(t, listBeforeOutput, holder.Username); held != "PROJECT_READ" {
		t.Fatalf("%s holds %q before the dry run, want PROJECT_READ:\n%s", holder.Username, held, listBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "project", "permissions", "users", "revoke", seeded.Key, holder.Username, "--yes")
	if err != nil {
		t.Fatalf("project permissions users revoke dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	assertLivePreviewOf(t, dryRunOutput, "project permissions users revoke", jsonoutput.OutcomeWouldApply)

	listAfterOutput, err := executeLiveCLI(t, "--json", "project", "permissions", "users", "list", seeded.Key, "--limit", "200")
	if err != nil {
		t.Fatalf("project permissions users list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no project user permission side-effect from dry-run revoke\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}
}

func TestLiveCLIProjectPermissionsGroupRevokeDryRunNoSideEffect(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	configureLiveCLIEnv(t, harness, seeded.Key, seeded.Repos[0].Slug)

	// A group that holds a permission for the dry run to revoke. It used to
	// name a group that did not exist, and Bitbucket answers a real revoke of
	// one with 204 and changes nothing, so this passed whether or not the dry
	// run held back.
	const group = "stash-users"
	mustLiveCLI(t, "project", "permissions", "groups", "grant", seeded.Key, group, "PROJECT_READ")

	listBeforeOutput, err := executeLiveCLI(t, "--json", "project", "permissions", "groups", "list", seeded.Key, "--limit", "200")
	if err != nil {
		t.Fatalf("project permissions groups list before failed: %v\noutput: %s", err, listBeforeOutput)
	}
	if held := governanceHeldPermission(t, listBeforeOutput, group); held != "PROJECT_READ" {
		t.Fatalf("%s holds %q before the dry run, want PROJECT_READ:\n%s", group, held, listBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "project", "permissions", "groups", "revoke", seeded.Key, group, "--yes")
	if err != nil {
		t.Fatalf("project permissions groups revoke dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	assertLivePreviewOf(t, dryRunOutput, "project permissions groups revoke", jsonoutput.OutcomeWouldApply)

	listAfterOutput, err := executeLiveCLI(t, "--json", "project", "permissions", "groups", "list", seeded.Key, "--limit", "200")
	if err != nil {
		t.Fatalf("project permissions groups list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no project group permission side-effect from dry-run revoke\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}
}

func TestLiveCLIReviewerConditionCreateDryRunNoSideEffect(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	configureLiveCLIEnv(t, harness, seeded.Key, seeded.Repos[0].Slug)

	listBeforeOutput, err := executeLiveCLI(t, "--json", "reviewer", "condition", "list", "--repo", seeded.Key+"/"+seeded.Repos[0].Slug)
	if err != nil {
		t.Fatalf("reviewer condition list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	// A condition Bitbucket would accept. The dry run used to send
	// {"requiredApprovals":1}, which a real create refuses, so an unchanged
	// listing said nothing about whether the dry run held back.
	reviewerID, err := harness.userID(ctx, harness.username())
	if err != nil {
		t.Fatalf("look up the reviewer id: %v", err)
	}
	condition := fmt.Sprintf(`{
		"sourceMatcher": {"id": "ANY_REF", "type": {"id": "ANY_REF"}},
		"targetMatcher": {"id": "refs/heads/master", "type": {"id": "BRANCH"}},
		"reviewers": [{"id": %d}],
		"requiredApprovals": 1
	}`, reviewerID)

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "reviewer", "condition", "create", condition, "--repo", seeded.Key+"/"+seeded.Repos[0].Slug)
	if err != nil {
		t.Fatalf("reviewer condition create dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	assertLivePreviewOf(t, dryRunOutput, "reviewer condition create", jsonoutput.OutcomeWouldApply)

	listAfterOutput, err := executeLiveCLI(t, "--json", "reviewer", "condition", "list", "--repo", seeded.Key+"/"+seeded.Repos[0].Slug)
	if err != nil {
		t.Fatalf("reviewer condition list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no reviewer condition side-effect from dry-run create\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}
	if listed := collectionFromCLI(t, listAfterOutput, "conditions"); len(listed) != 0 {
		t.Fatalf("a fresh repository holds conditions after a dry-run create: %v", listed)
	}
}

func TestLiveCLIReviewerConditionUpdateDryRunNoSideEffect(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	configureLiveCLIEnv(t, harness, seeded.Key, seeded.Repos[0].Slug)

	// A condition that exists, updated to one Bitbucket would accept. The dry
	// run used to name id 999999 with a body a real update refuses, so there was
	// nothing a leaked update could have changed.
	conditionID, reviewerID := governanceSeedCondition(t, ctx, harness, seeded.Key+"/"+seeded.Repos[0].Slug)

	listBeforeOutput, err := executeLiveCLI(t, "--json", "reviewer", "condition", "list", "--repo", seeded.Key+"/"+seeded.Repos[0].Slug)
	if err != nil {
		t.Fatalf("reviewer condition list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	update := fmt.Sprintf(`{
		"sourceMatcher": {"id": "ANY_REF", "type": {"id": "ANY_REF"}},
		"targetMatcher": {"id": "refs/heads/master", "type": {"id": "BRANCH"}},
		"reviewers": [{"id": %d}],
		"requiredApprovals": 2
	}`, reviewerID)

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "reviewer", "condition", "update", conditionID, update, "--repo", seeded.Key+"/"+seeded.Repos[0].Slug)
	if err != nil {
		t.Fatalf("reviewer condition update dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	assertLivePreviewOf(t, dryRunOutput, "reviewer condition update", jsonoutput.OutcomeWouldApply)

	listAfterOutput, err := executeLiveCLI(t, "--json", "reviewer", "condition", "list", "--repo", seeded.Key+"/"+seeded.Repos[0].Slug)
	if err != nil {
		t.Fatalf("reviewer condition list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no reviewer condition side-effect from dry-run update\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}
}

func TestLiveCLIReviewerConditionDeleteDryRunNoSideEffect(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	configureLiveCLIEnv(t, harness, seeded.Key, seeded.Repos[0].Slug)

	// A condition that exists. The dry run used to name id 999999, which a
	// leaked delete could not have removed from the listing.
	conditionID, _ := governanceSeedCondition(t, ctx, harness, seeded.Key+"/"+seeded.Repos[0].Slug)

	listBeforeOutput, err := executeLiveCLI(t, "--json", "reviewer", "condition", "list", "--repo", seeded.Key+"/"+seeded.Repos[0].Slug)
	if err != nil {
		t.Fatalf("reviewer condition list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "reviewer", "condition", "delete", conditionID, "--repo", seeded.Key+"/"+seeded.Repos[0].Slug, "--yes")
	if err != nil {
		t.Fatalf("reviewer condition delete dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	assertLivePreviewOf(t, dryRunOutput, "reviewer condition delete", jsonoutput.OutcomeWouldApply)

	listAfterOutput, err := executeLiveCLI(t, "--json", "reviewer", "condition", "list", "--repo", seeded.Key+"/"+seeded.Repos[0].Slug)
	if err != nil {
		t.Fatalf("reviewer condition list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no reviewer condition side-effect from dry-run delete\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}
}

func TestLiveCLIProjectCreateDryRunNoSideEffect(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	configureLiveCLIEnv(t, harness, seeded.Key, seeded.Repos[0].Slug)

	// A key nothing else will pick, so asking whether it exists afterwards is a
	// question about this dry run and not about the instance.
	//
	// The check used to be a byte comparison of `bb project list` before and
	// after. Instance-wide, that is a claim about every project on the server:
	// with the suite running in parallel, another test seeding one between the
	// two calls made the listings differ for a reason that had nothing to do
	// with the dry run.
	// Upper-cased, as Bitbucket stores a project key (ADR-085).
	newKey := strings.ToUpper("DRY" + uniqueSuffix())
	// The name as unique as the key. Bitbucket refuses a name already in use as
	// well, and the preview says so, which would make its verdict a question
	// about the instance too.
	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "project", "create", newKey, "--name", "Dry Run Project "+newKey)
	if err != nil {
		t.Fatalf("project create dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	assertLivePreviewOf(t, dryRunOutput, "project create", jsonoutput.OutcomeWouldApply)

	if getOutput, getErr := executeLiveCLI(t, "--json", "project", "get", newKey); getErr == nil {
		t.Fatalf("the create dry-run made project %s: %s", newKey, getOutput)
	} else if !apperrors.IsKind(getErr, apperrors.KindNotFound) {
		// Only an answer that the project is not there says the dry run made
		// nothing. Any other failure leaves the question open.
		t.Fatalf("asking for project %s failed for another reason than its absence: %v", newKey, getErr)
	}
}

func TestLiveReviewerGroupsAndDefaultReviewersCLI(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// Every call below is asserted rather than logged. Half of this test used to
	// run its command, log whatever came back and carry on, so a reviewer-group
	// surface that stopped working -- or one whose id could no longer be read
	// out of the create -- was a green run with a line in the output nobody
	// reads. A dependency that cannot be reached fails the test.
	repoRef := seeded.Key + "/" + repo.Slug
	groupName := testsupport.UniqueName("lt-reviewer-group-")
	// Bitbucket refuses an empty reviewer group, so every create below names a
	// member. The permissive version of this test never found that out: its
	// create failed and the whole lifecycle was skipped.
	member := harness.username()

	// 1. List project-scoped reviewer groups (empty is an answer).
	output, err := executeLiveCLI(t, "--json", "reviewer-group", "list", "--project", seeded.Key)
	if err != nil {
		t.Fatalf("project reviewer-group list failed: %v\noutput: %s", err, output)
	}
	if !strings.Contains(output, `"reviewerGroups"`) {
		t.Fatalf("expected reviewerGroups in output: %s", output)
	}

	// 2. List repository-scoped reviewer groups.
	output, err = executeLiveCLI(t, "--json", "reviewer-group", "list", "--repo", repoRef)
	if err != nil {
		t.Fatalf("repo reviewer-group list failed: %v\noutput: %s", err, output)
	}
	if !strings.Contains(output, `"reviewerGroups"`) {
		t.Fatalf("expected reviewerGroups in output: %s", output)
	}

	// 3. Dry-run create.
	dryRunOut, err := executeLiveCLI(t, "--json", "--dry-run", "reviewer-group", "create", groupName, "--repo", repoRef, "--users", member)
	if err != nil {
		t.Fatalf("reviewer-group create dry-run failed: %v\noutput: %s", err, dryRunOut)
	}
	assertLivePreviewOf(t, dryRunOut, "reviewer-group create", jsonoutput.OutcomeWouldApply)
	if listing := mustLiveCLI(t, "reviewer-group", "list", "--repo", repoRef); governanceReviewerGroupNamed(t, listing, groupName) {
		t.Fatalf("the create dry run made group %s: %s", groupName, listing)
	}

	// 4. Create, and read the id back rather than slicing it out of the text.
	createOut, err := executeLiveCLI(t, "--json", "reviewer-group", "create", groupName, "--repo", repoRef, "--users", member, "--description", "live desc")
	if err != nil {
		t.Fatalf("reviewer-group create failed: %v\noutput: %s", err, createOut)
	}
	groupID, ok := numericOrStringID(decodeJSONMap(t, createOut)["id"])
	if !ok {
		t.Fatalf("the create answered without an id, so nothing below could run: %s", createOut)
	}

	// Every write to the group from here is read back from the listing, which
	// carries the name, description and members as Bitbucket holds them.
	listing := mustLiveCLI(t, "reviewer-group", "list", "--repo", repoRef)
	group, found := governanceReviewerGroup(t, listing, groupID)
	if !found {
		t.Fatalf("group %s is not in the listing after its create: %s", groupID, listing)
	}
	if group["name"] != groupName || group["description"] != "live desc" || group["scope"] != "REPOSITORY" {
		t.Errorf("group %s is stored as %v, %v, scope %v; want %s, live desc, REPOSITORY",
			groupID, group["name"], group["description"], group["scope"], groupName)
	}
	if members := governanceUserNames(group["users"]); !governanceSameNames(members, []string{member}) {
		t.Errorf("group %s holds %v, want just %s", groupID, members, member)
	}

	updateDryRunOut, err := executeLiveCLI(t, "--json", "--dry-run", "reviewer-group", "update", groupID, "--repo", repoRef, "--description", "new live desc")
	if err != nil {
		t.Fatalf("reviewer-group update dry-run failed: %v\noutput: %s", err, updateDryRunOut)
	}
	listing = mustLiveCLI(t, "reviewer-group", "list", "--repo", repoRef)
	if group, _ := governanceReviewerGroup(t, listing, groupID); group["description"] != "live desc" {
		t.Errorf("after the update dry run the description is %v, want it unchanged: %s", group["description"], listing)
	}

	updateOut, err := executeLiveCLI(t, "--json", "reviewer-group", "update", groupID, "--repo", repoRef, "--description", "new live desc")
	if err != nil {
		t.Fatalf("reviewer-group update failed: %v\noutput: %s", err, updateOut)
	}
	if !strings.Contains(updateOut, "new live desc") {
		t.Fatalf("the update did not report the new description: %s", updateOut)
	}
	listing = mustLiveCLI(t, "reviewer-group", "list", "--repo", repoRef)
	if group, _ := governanceReviewerGroup(t, listing, groupID); group["description"] != "new live desc" || group["name"] != groupName {
		t.Errorf("after the update group %s is stored as %v with %v, want %s with new live desc", groupID, group["name"], group["description"], groupName)
	}

	usersOut, err := executeLiveCLI(t, "--json", "reviewer-group", "users", groupID, "--repo", repoRef)
	if err != nil {
		t.Fatalf("reviewer-group users failed: %v\noutput: %s", err, usersOut)
	}
	if !strings.Contains(usersOut, `"users"`) {
		t.Fatalf("expected users in output: %s", usersOut)
	}
	if members := governanceUserNames(decodeJSONMap(t, usersOut)["users"]); !governanceSameNames(members, []string{member}) {
		t.Errorf("reviewer-group users = %v, want just %s", members, member)
	}

	deleteDryRunOut, err := executeLiveCLI(t, "--json", "--dry-run", "reviewer-group", "delete", groupID, "--repo", repoRef, "--yes")
	if err != nil {
		t.Fatalf("reviewer-group delete dry-run failed: %v\noutput: %s", err, deleteDryRunOut)
	}
	if _, found := governanceReviewerGroup(t, mustLiveCLI(t, "reviewer-group", "list", "--repo", repoRef), groupID); !found {
		t.Fatalf("the delete dry run removed group %s", groupID)
	}

	deleteOut, err := executeLiveCLI(t, "--json", "reviewer-group", "delete", groupID, "--repo", repoRef, "--yes")
	if err != nil {
		t.Fatalf("reviewer-group delete failed: %v\noutput: %s", err, deleteOut)
	}

	// The state is read back, because a delete that reports success and leaves
	// the group behind is the failure this whole lifecycle exists to catch.
	afterOut, err := executeLiveCLI(t, "--json", "reviewer-group", "list", "--repo", repoRef)
	if err != nil {
		t.Fatalf("repo reviewer-group list after delete failed: %v\noutput: %s", err, afterOut)
	}
	if strings.Contains(afterOut, groupName) {
		t.Fatalf("the deleted group is still listed: %s", afterOut)
	}
	if _, found := governanceReviewerGroup(t, afterOut, groupID); found {
		t.Fatalf("group %s is still an entry of the listing: %s", groupID, afterOut)
	}

	// 5. Default reviewers.
	repoResp, err := harness.client.GetRepositoryWithResponse(ctx, seeded.Key, repo.Slug)
	if err != nil {
		t.Fatalf("failed to get repository details: %v", err)
	}
	repoID := fmt.Sprintf("%d", *repoResp.ApplicationjsonCharsetUTF8200.Id)

	// The refs are filters, and with no condition on the repository every query
	// answers nobody whatever it sends. So a condition is seeded that each query
	// below can miss in its own way: it applies from one branch into master.
	// Bitbucket refuses a query missing either ref or either repository id, so
	// the one that matches also shows all four arrived.
	const source = "feature/default-reviewers"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, source, "default-reviewers.txt"); err != nil {
		t.Fatalf("push the condition's source branch failed: %v", err)
	}
	reviewerID, err := harness.userID(ctx, member)
	if err != nil {
		t.Fatalf("look up the reviewer id: %v", err)
	}
	created := mustLiveCLI(t, "reviewer", "condition", "create", fmt.Sprintf(`{
		"sourceMatcher": {"id": "refs/heads/%s", "type": {"id": "BRANCH"}},
		"targetMatcher": {"id": "refs/heads/master", "type": {"id": "BRANCH"}},
		"reviewers": [{"id": %d}],
		"requiredApprovals": 1
	}`, source, reviewerID), "--repo", repoRef)
	conditionID, ok := numericOrStringID(decodeJSONMap(t, created)["id"])
	if !ok {
		t.Fatalf("the condition create answered without an id: %s", created)
	}
	conditions := mustLiveCLI(t, "reviewer", "condition", "list", "--repo", repoRef)
	stored, found := governanceCondition(t, conditions, conditionID)
	if !found {
		t.Fatalf("condition %s is not in the listing: %s", conditionID, conditions)
	}
	assertGovernanceConditionStored(t, stored, "refs/heads/"+source, "refs/heads/master", reviewerID, 1)

	defOut, err := executeLiveCLI(t, "--json", "pr", "default-reviewers", "--repo", repoRef, "--source-ref", "refs/heads/master", "--target-ref", "refs/heads/master", "--source-repo-id", repoID, "--target-repo-id", repoID)
	if err != nil {
		t.Fatalf("pr default-reviewers failed: %v\noutput: %s", err, defOut)
	}
	if !strings.Contains(defOut, `"defaultReviewers"`) {
		t.Fatalf("expected defaultReviewers in output: %s", defOut)
	}
	if names := governanceDefaultReviewerNames(t, defOut); len(names) != 0 {
		t.Errorf("a query from master drew %v from a condition that applies from %s", names, source)
	}

	matched := mustLiveCLI(t, "pr", "default-reviewers", "--repo", repoRef, "--source-ref", "refs/heads/"+source, "--target-ref", "refs/heads/master", "--source-repo-id", repoID, "--target-repo-id", repoID)
	if names := governanceDefaultReviewerNames(t, matched); !governanceSameNames(names, []string{member}) {
		t.Errorf("the condition's own refs drew %v, want just %s: %s", names, member, matched)
	}

	elsewhere := mustLiveCLI(t, "pr", "default-reviewers", "--repo", repoRef, "--source-ref", "refs/heads/"+source, "--target-ref", "refs/heads/"+source, "--source-repo-id", repoID, "--target-repo-id", repoID)
	if names := governanceDefaultReviewerNames(t, elsewhere); len(names) != 0 {
		t.Errorf("a query into %s drew %v from a condition that applies into master", source, names)
	}

	// Those queries name one repository on both sides, so they cannot tell the
	// two repository ids apart. A fork can: Bitbucket looks each ref up in the
	// repository given for its side and refuses one that is not there, so a
	// branch only the fork has, into one only the upstream has, is answered
	// only when each id went to its own side.
	forkSlug := repo.Slug + "-reviewers-fork"
	fork := postLiveJSON(t, fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s", seeded.Key, repo.Slug), map[string]any{
		"name":    forkSlug,
		"slug":    forkSlug,
		"project": map[string]any{"key": seeded.Key},
	})
	forkID, ok := numericOrStringID(fork["id"])
	if !ok {
		t.Fatalf("the fork answered without an id: %v", fork)
	}
	const forkOnly, upstreamOnly = "feature/fork-only", "feature/upstream-only"
	if err := harness.pushCommitOnBranch(seeded.Key, forkSlug, forkOnly, "fork-only.txt"); err != nil {
		t.Fatalf("push a branch to the fork failed: %v", err)
	}
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, upstreamOnly, "upstream-only.txt"); err != nil {
		t.Fatalf("push a branch to the upstream failed: %v", err)
	}

	fromFork, err := executeLiveCLI(t, "--json", "pr", "default-reviewers", "--repo", repoRef, "--source-ref", "refs/heads/"+forkOnly, "--target-ref", "refs/heads/"+upstreamOnly, "--source-repo-id", forkID, "--target-repo-id", repoID)
	if err != nil {
		t.Fatalf("a query from the fork's branch into the upstream's was refused, so a repository id went to the wrong side: %v\noutput: %s", err, fromFork)
	}
	if names := governanceDefaultReviewerNames(t, fromFork); len(names) != 0 {
		t.Errorf("a query from %s into %s drew %v from a condition on neither", forkOnly, upstreamOnly, names)
	}
}

// governanceSeedCondition creates a condition for a dry run to be tried
// against, and reads it back so the dry run starts from a known condition:
// any ref into master, the harness user as reviewer, one approval. It returns
// the condition's id and the reviewer's.
func governanceSeedCondition(t *testing.T, ctx context.Context, harness *liveHarness, repoRef string) (string, int64) {
	t.Helper()

	reviewerID, err := harness.userID(ctx, harness.username())
	if err != nil {
		t.Fatalf("look up the reviewer id: %v", err)
	}

	created := mustLiveCLI(t, "reviewer", "condition", "create", fmt.Sprintf(`{
		"sourceMatcher": {"id": "ANY_REF", "type": {"id": "ANY_REF"}},
		"targetMatcher": {"id": "refs/heads/master", "type": {"id": "BRANCH"}},
		"reviewers": [{"id": %d}],
		"requiredApprovals": 1
	}`, reviewerID), "--repo", repoRef)
	conditionID, ok := numericOrStringID(decodeJSONMap(t, created)["id"])
	if !ok {
		t.Fatalf("the condition create answered without an id: %s", created)
	}

	listing := mustLiveCLI(t, "reviewer", "condition", "list", "--repo", repoRef)
	stored, found := governanceCondition(t, listing, conditionID)
	if !found {
		t.Fatalf("condition %s is not in the listing: %s", conditionID, listing)
	}
	assertGovernanceConditionStored(t, stored, "", "refs/heads/master", reviewerID, 1)

	return conditionID, reviewerID
}

// governanceDefaultReviewerNames reads the usernames out of the output of
// `bb pr default-reviewers`, across every condition it reports.
func governanceDefaultReviewerNames(t *testing.T, output string) []string {
	t.Helper()

	names := []string{}
	for _, entry := range collectionFromCLI(t, output, "defaultReviewers") {
		if condition, ok := entry.(map[string]any); ok {
			names = append(names, governanceUserNames(condition["reviewers"])...)
		}
	}

	return names
}
