//go:build live

package live_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

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
	grantOutput, err := executeLiveCLI(t, "project", "permissions", "groups", "grant", seeded.Key, "stash-users", "PROJECT_READ")
	if err != nil {
		t.Fatalf("project group permission grant failed: %v\noutput: %s", err, grantOutput)
	}

	grantOutput, err = executeLiveCLI(t, "repo", "settings", "security", "permissions", "groups", "grant", "stash-users", "REPO_READ", "--repo", seeded.Key+"/"+repo.Slug)
	if err != nil {
		t.Fatalf("repo group permission grant failed: %v\noutput: %s", err, grantOutput)
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

	updateOutput, err := executeLiveCLI(t, "--json", "reviewer", "condition", "update", conditionID, strings.Replace(condition, "\"requiredApprovals\": 1", "\"requiredApprovals\": 2", 1), "--repo", seeded.Key+"/"+repo.Slug)
	if err != nil {
		t.Fatalf("reviewer condition update failed: %v\noutput: %s", err, updateOutput)
	}

	deleteOutput, err := executeLiveCLI(t, "--json", "reviewer", "condition", "delete", conditionID, "--repo", seeded.Key+"/"+repo.Slug, "--yes")
	if err != nil {
		t.Fatalf("reviewer condition delete failed: %v\noutput: %s", err, deleteOutput)
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

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "project", "permissions", "users", "grant", seeded.Key, username, "PROJECT_WRITE")
	if err != nil {
		t.Fatalf("project permissions users grant dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"planningMode": "stateful"`) {
		t.Fatalf("expected stateful planning mode, got: %s", dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "project.permission.user.grant"`) {
		t.Fatalf("expected intent in dry-run output, got: %s", dryRunOutput)
	}

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

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "project", "permissions", "groups", "grant", seeded.Key, group, "PROJECT_READ")
	if err != nil {
		t.Fatalf("project permissions groups grant dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "project.permission.group.grant"`) {
		t.Fatalf("expected project.permission.group.grant intent, got: %s", dryRunOutput)
	}

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

	listBeforeOutput, err := executeLiveCLI(t, "--json", "project", "permissions", "users", "list", seeded.Key, "--limit", "200")
	if err != nil {
		t.Fatalf("project permissions users list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "project", "permissions", "users", "revoke", seeded.Key, "dryrun-missing-user", "--yes")
	if err != nil {
		t.Fatalf("project permissions users revoke dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "project.permission.user.revoke"`) {
		t.Fatalf("expected project.permission.user.revoke intent, got: %s", dryRunOutput)
	}

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

	listBeforeOutput, err := executeLiveCLI(t, "--json", "project", "permissions", "groups", "list", seeded.Key, "--limit", "200")
	if err != nil {
		t.Fatalf("project permissions groups list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "project", "permissions", "groups", "revoke", seeded.Key, "dryrun-missing-group", "--yes")
	if err != nil {
		t.Fatalf("project permissions groups revoke dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "project.permission.group.revoke"`) {
		t.Fatalf("expected project.permission.group.revoke intent, got: %s", dryRunOutput)
	}

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

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "reviewer", "condition", "create", `{"requiredApprovals":1}`, "--repo", seeded.Key+"/"+seeded.Repos[0].Slug)
	if err != nil {
		t.Fatalf("reviewer condition create dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"planningMode": "stateful"`) {
		t.Fatalf("expected stateful planning mode, got: %s", dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "reviewer.condition.create"`) {
		t.Fatalf("expected reviewer.condition.create intent, got: %s", dryRunOutput)
	}

	listAfterOutput, err := executeLiveCLI(t, "--json", "reviewer", "condition", "list", "--repo", seeded.Key+"/"+seeded.Repos[0].Slug)
	if err != nil {
		t.Fatalf("reviewer condition list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no reviewer condition side-effect from dry-run create\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
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

	listBeforeOutput, err := executeLiveCLI(t, "--json", "reviewer", "condition", "list", "--repo", seeded.Key+"/"+seeded.Repos[0].Slug)
	if err != nil {
		t.Fatalf("reviewer condition list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "reviewer", "condition", "update", "999999", `{"requiredApprovals":2}`, "--repo", seeded.Key+"/"+seeded.Repos[0].Slug)
	if err != nil {
		t.Fatalf("reviewer condition update dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "reviewer.condition.update"`) {
		t.Fatalf("expected reviewer.condition.update intent, got: %s", dryRunOutput)
	}

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

	listBeforeOutput, err := executeLiveCLI(t, "--json", "reviewer", "condition", "list", "--repo", seeded.Key+"/"+seeded.Repos[0].Slug)
	if err != nil {
		t.Fatalf("reviewer condition list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "reviewer", "condition", "delete", "999999", "--repo", seeded.Key+"/"+seeded.Repos[0].Slug, "--yes")
	if err != nil {
		t.Fatalf("reviewer condition delete dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "reviewer.condition.delete"`) {
		t.Fatalf("expected reviewer.condition.delete intent, got: %s", dryRunOutput)
	}

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
	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "project", "create", newKey, "--name", "Dry Run Project")
	if err != nil {
		t.Fatalf("project create dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"planningMode": "stateful"`) {
		t.Fatalf("expected stateful planning mode, got: %s", dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "project.create"`) {
		t.Fatalf("expected project.create intent, got: %s", dryRunOutput)
	}

	if getOutput, getErr := executeLiveCLI(t, "--json", "project", "get", newKey); getErr == nil {
		t.Fatalf("the create dry-run made project %s: %s", newKey, getOutput)
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
	if !strings.Contains(dryRunOut, `"intent": "reviewer-group.create"`) {
		t.Fatalf("expected intent in dry-run create, got: %s", dryRunOut)
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

	updateDryRunOut, err := executeLiveCLI(t, "--json", "--dry-run", "reviewer-group", "update", groupID, "--repo", repoRef, "--description", "new live desc")
	if err != nil {
		t.Fatalf("reviewer-group update dry-run failed: %v\noutput: %s", err, updateDryRunOut)
	}

	updateOut, err := executeLiveCLI(t, "--json", "reviewer-group", "update", groupID, "--repo", repoRef, "--description", "new live desc")
	if err != nil {
		t.Fatalf("reviewer-group update failed: %v\noutput: %s", err, updateOut)
	}
	if !strings.Contains(updateOut, "new live desc") {
		t.Fatalf("the update did not report the new description: %s", updateOut)
	}

	usersOut, err := executeLiveCLI(t, "--json", "reviewer-group", "users", groupID, "--repo", repoRef)
	if err != nil {
		t.Fatalf("reviewer-group users failed: %v\noutput: %s", err, usersOut)
	}
	if !strings.Contains(usersOut, `"users"`) {
		t.Fatalf("expected users in output: %s", usersOut)
	}

	deleteDryRunOut, err := executeLiveCLI(t, "--json", "--dry-run", "reviewer-group", "delete", groupID, "--repo", repoRef, "--yes")
	if err != nil {
		t.Fatalf("reviewer-group delete dry-run failed: %v\noutput: %s", err, deleteDryRunOut)
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

	// 5. Default reviewers.
	repoResp, err := harness.client.GetRepositoryWithResponse(ctx, seeded.Key, repo.Slug)
	if err != nil {
		t.Fatalf("failed to get repository details: %v", err)
	}
	repoID := fmt.Sprintf("%d", *repoResp.ApplicationjsonCharsetUTF8200.Id)

	defOut, err := executeLiveCLI(t, "--json", "pr", "default-reviewers", "--repo", repoRef, "--source-ref", "refs/heads/master", "--target-ref", "refs/heads/master", "--source-repo-id", repoID, "--target-repo-id", repoID)
	if err != nil {
		t.Fatalf("pr default-reviewers failed: %v\noutput: %s", err, defOut)
	}
	if !strings.Contains(defOut, `"defaultReviewers"`) {
		t.Fatalf("expected defaultReviewers in output: %s", defOut)
	}
}
