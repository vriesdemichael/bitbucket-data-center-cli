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

func TestLiveReviewerConditionsLifecycle(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	username := harness.username()

	// 1. List initial conditions on repo
	listOutput, err := executeLiveCLI(t, "--json", "reviewer", "condition", "list", "--repo", seeded.Key+"/"+repo.Slug)
	if err != nil {
		t.Fatalf("initial reviewer condition list failed: %v\noutput: %s", err, listOutput)
	}

	// 2. Create reviewer condition with explicit branch matchers and reviewer
	//
	// The reviewer goes by numeric id. This test used to name them, which the
	// endpoint reads as id -1 and refuses, and it met that refusal with a log
	// line and a return: it passed on every run without reaching one assertion.
	reviewerID, err := harness.userID(ctx, username)
	if err != nil {
		t.Fatalf("look up the reviewer id: %v", err)
	}
	conditionJSON := fmt.Sprintf(`{
		"sourceMatcher": {"id": "ANY_REF", "type": {"id": "ANY_REF"}},
		"targetMatcher": {"id": "refs/heads/master", "type": {"id": "BRANCH"}},
		"reviewers": [{"id": %d}],
		"requiredApprovals": 1
	}`, reviewerID)

	createOutput, err := executeLiveCLI(t, "--json", "reviewer", "condition", "create", conditionJSON, "--repo", seeded.Key+"/"+repo.Slug)
	if err != nil {
		t.Fatalf("reviewer condition create failed: %v\noutput: %s", err, createOutput)
	}

	conditionID, ok := numericOrStringID(decodeJSONMap(t, createOutput)["id"])
	if !ok {
		t.Fatalf("the create answered without a condition id: %s", createOutput)
	}

	defer func() {
		_, _ = executeLiveCLI(t, "reviewer", "condition", "delete", conditionID, "--repo", seeded.Key+"/"+repo.Slug, "--yes")
	}()

	// 3. Verify condition appears in listing
	afterList, err := executeLiveCLI(t, "--json", "reviewer", "condition", "list", "--repo", seeded.Key+"/"+repo.Slug)
	if err != nil {
		t.Fatalf("reviewer condition list after create failed: %v\noutput: %s", err, afterList)
	}
	if !strings.Contains(afterList, conditionID) {
		t.Fatalf("expected condition ID %s in list output: %s", conditionID, afterList)
	}
	created, found := governanceCondition(t, afterList, conditionID)
	if !found {
		t.Fatalf("condition %s is not an entry of the listing: %s", conditionID, afterList)
	}
	assertGovernanceConditionStored(t, created, "", "refs/heads/master", reviewerID, 1)

	// 4. Update condition required approvals
	updateJSON := fmt.Sprintf(`{
		"sourceMatcher": {"id": "ANY_REF", "type": {"id": "ANY_REF"}},
		"targetMatcher": {"id": "refs/heads/master", "type": {"id": "BRANCH"}},
		"reviewers": [{"id": %d}],
		"requiredApprovals": 2
	}`, reviewerID)

	updateOutput, err := executeLiveCLI(t, "--json", "reviewer", "condition", "update", conditionID, updateJSON, "--repo", seeded.Key+"/"+repo.Slug)
	if err != nil {
		t.Fatalf("reviewer condition update failed: %v\noutput: %s", err, updateOutput)
	}

	afterUpdate := mustLiveCLI(t, "reviewer", "condition", "list", "--repo", repoRef)
	updated, found := governanceCondition(t, afterUpdate, conditionID)
	if !found {
		t.Fatalf("condition %s is not listed after its update: %s", conditionID, afterUpdate)
	}
	assertGovernanceConditionStored(t, updated, "", "refs/heads/master", reviewerID, 2)

	// 5. Delete condition with dry-run
	dryRunOutput, err := executeLiveCLI(t, "--dry-run", "reviewer", "condition", "delete", conditionID, "--repo", seeded.Key+"/"+repo.Slug, "--yes")
	if err != nil {
		t.Fatalf("reviewer condition delete dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(strings.ToLower(dryRunOutput), "dry-run") && !strings.Contains(strings.ToLower(dryRunOutput), "delete") {
		t.Fatalf("expected dry-run preview in output: %s", dryRunOutput)
	}

	afterDryRun := mustLiveCLI(t, "reviewer", "condition", "list", "--repo", repoRef)
	kept, found := governanceCondition(t, afterDryRun, conditionID)
	if !found {
		t.Fatalf("a dry-run delete removed condition %s: %s", conditionID, afterDryRun)
	}
	assertGovernanceConditionStored(t, kept, "", "refs/heads/master", reviewerID, 2)

	// 6. Delete condition for real
	deleteOutput, err := executeLiveCLI(t, "--json", "reviewer", "condition", "delete", conditionID, "--repo", seeded.Key+"/"+repo.Slug, "--yes")
	if err != nil {
		t.Fatalf("reviewer condition delete failed: %v\noutput: %s", err, deleteOutput)
	}

	afterDelete := mustLiveCLI(t, "reviewer", "condition", "list", "--repo", repoRef)
	if _, found := governanceCondition(t, afterDelete, conditionID); found {
		t.Errorf("condition %s is still listed after its delete: %s", conditionID, afterDelete)
	}
}

func TestLiveReviewerGroupsLifecycle(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	username := harness.username()
	groupName := testsupport.UniqueName("team-gov-")

	// 1. Create reviewer group on repository
	//
	// The member is not decoration: Bitbucket refuses a group with none, so
	// before --users existed this call failed on every run. The test carried on
	// with t.Logf and a bare return, so steps 2 to 4 never executed and the
	// suite stayed green while asserting nothing (#533).
	createOutput, err := executeLiveCLI(t, "--json", "reviewer-group", "create", groupName, "--repo", seeded.Key+"/"+repo.Slug, "--users", username)
	if err != nil {
		t.Fatalf("reviewer group create failed: %v\noutput: %s", err, createOutput)
	}

	groupID := fmt.Sprintf("%d", int64(decodeJSONMap(t, createOutput)["id"].(float64)))

	defer func() {
		_, _ = executeLiveCLI(t, "reviewer-group", "delete", groupID, "--repo", seeded.Key+"/"+repo.Slug, "--yes")
	}()

	// 2. List reviewer groups on repository
	listOutput, err := executeLiveCLI(t, "--json", "reviewer-group", "list", "--repo", seeded.Key+"/"+repo.Slug)
	if err != nil {
		t.Fatalf("reviewer group list failed: %v\noutput: %s", err, listOutput)
	}
	if !strings.Contains(listOutput, groupName) {
		t.Fatalf("expected group name %s in list output: %s", groupName, listOutput)
	}
	listed, found := governanceReviewerGroup(t, listOutput, groupID)
	if !found {
		t.Fatalf("group %s is not an entry of the listing: %s", groupID, listOutput)
	}
	if listed["name"] != groupName || listed["scope"] != "REPOSITORY" {
		t.Errorf("group %s is stored as %q with scope %v, want %q on the repository", groupID, listed["name"], listed["scope"], groupName)
	}

	// 3. The member is really in the group, which is what makes it usable
	usersOutput, err := executeLiveCLI(t, "--json", "reviewer-group", "users", groupID, "--repo", seeded.Key+"/"+repo.Slug)
	if err != nil {
		t.Fatalf("reviewer group users failed: %v\noutput: %s", err, usersOutput)
	}
	if !strings.Contains(usersOutput, username) {
		t.Fatalf("expected user %s in reviewer group users output: %s", username, usersOutput)
	}
	if members := governanceUserNames(decodeJSONMap(t, usersOutput)["users"]); !governanceSameNames(members, []string{username}) {
		t.Errorf("group members = %v, want just %s", members, username)
	}

	// 4. Delete reviewer group with dry-run
	dryRunOutput, err := executeLiveCLI(t, "--dry-run", "reviewer-group", "delete", groupID, "--repo", seeded.Key+"/"+repo.Slug, "--yes")
	if err != nil {
		t.Fatalf("reviewer group delete dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if _, found := governanceReviewerGroup(t, mustLiveCLI(t, "reviewer-group", "list", "--repo", repoRef), groupID); !found {
		t.Fatalf("a dry-run delete removed group %s", groupID)
	}

	// 5. Delete reviewer group for real
	deleteOutput, err := executeLiveCLI(t, "--json", "reviewer-group", "delete", groupID, "--repo", seeded.Key+"/"+repo.Slug, "--yes")
	if err != nil {
		t.Fatalf("reviewer group delete failed: %v\noutput: %s", err, deleteOutput)
	}
	if afterDelete := mustLiveCLI(t, "reviewer-group", "list", "--repo", repoRef); governanceReviewerGroupNamed(t, afterDelete, groupName) {
		t.Errorf("group %s is still listed after its delete: %s", groupName, afterDelete)
	}
}

// governanceCondition finds one default-reviewer condition, by id, in the
// output of `bb reviewer condition list`.
//
// The listing is the read-back for every condition write in these tests.
// Bitbucket answers success to a property it does not know and keeps nothing
// of it, so what a create or update answers says what bb was told back, not
// what is stored.
func governanceCondition(t *testing.T, listOutput, conditionID string) (map[string]any, bool) {
	t.Helper()

	for _, entry := range collectionFromCLI(t, listOutput, "conditions") {
		condition, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if id, ok := numericOrStringID(condition["id"]); ok && id == conditionID {
			return condition, true
		}
	}

	return nil, false
}

// assertGovernanceConditionStored compares a condition Bitbucket holds with
// the one a test sent: the source branch, or any ref when source is empty, the
// target branch, one reviewer by id, and the approvals required.
//
// An any-ref matcher is recognised by its type, because its id is not the
// caller's: {"id": "ANY_REF"} comes back as ANY_REF_MATCHER_ID.
func assertGovernanceConditionStored(t *testing.T, condition map[string]any, source, target string, reviewerID int64, approvals int) {
	t.Helper()

	if got, ok := condition["requiredApprovals"].(float64); !ok || int(got) != approvals {
		t.Errorf("requiredApprovals = %v, want %d: %v", condition["requiredApprovals"], approvals, condition)
	}

	sourceMatcher, _ := condition["sourceRefMatcher"].(map[string]any)
	if source == "" && sourceMatcher["type"] != "ANY_REF" {
		t.Errorf("sourceRefMatcher = %v, want any ref", sourceMatcher)
	}
	if source != "" && (sourceMatcher["type"] != "BRANCH" || sourceMatcher["id"] != source) {
		t.Errorf("sourceRefMatcher = %v, want the branch %s", sourceMatcher, source)
	}

	targetMatcher, _ := condition["targetRefMatcher"].(map[string]any)
	if targetMatcher["type"] != "BRANCH" || targetMatcher["id"] != target {
		t.Errorf("targetRefMatcher = %v, want the branch %s", targetMatcher, target)
	}

	reviewers, _ := condition["reviewers"].([]any)
	if len(reviewers) != 1 {
		t.Errorf("reviewers = %v, want the one sent, id %d", reviewers, reviewerID)
		return
	}
	reviewer, _ := reviewers[0].(map[string]any)
	if id, ok := reviewer["id"].(float64); !ok || int64(id) != reviewerID {
		t.Errorf("reviewer = %v, want id %d", reviewer, reviewerID)
	}
}

// governanceReviewerGroup finds one reviewer group, by id, in the output of
// `bb reviewer-group list`. The listing carries each group's name, description,
// scope and members, so it is the read-back for what a create or update sent.
func governanceReviewerGroup(t *testing.T, listOutput, groupID string) (map[string]any, bool) {
	t.Helper()

	for _, entry := range collectionFromCLI(t, listOutput, "reviewerGroups") {
		group, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if id, ok := numericOrStringID(group["id"]); ok && id == groupID {
			return group, true
		}
	}

	return nil, false
}

// governanceReviewerGroupNamed reports whether a `bb reviewer-group list`
// output holds a group of that name, for a group that should not exist.
func governanceReviewerGroupNamed(t *testing.T, listOutput, name string) bool {
	t.Helper()

	for _, entry := range collectionFromCLI(t, listOutput, "reviewerGroups") {
		if group, ok := entry.(map[string]any); ok && strings.EqualFold(asString(group["name"]), name) {
			return true
		}
	}

	return false
}

// governanceUserNames reads the usernames out of a users array, which
// `reviewer-group users`, a listed reviewer group and a default-reviewer
// answer all carry as objects with a name.
func governanceUserNames(users any) []string {
	entries, _ := users.([]any)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if user, ok := entry.(map[string]any); ok {
			names = append(names, asString(user["name"]))
		}
	}

	return names
}

// governanceSameNames reports whether two lists name the same users, in any
// order. Bitbucket keeps a username's case, and bb compares them folded
// everywhere else.
func governanceSameNames(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for _, name := range want {
		if !containsFold(got, name) {
			return false
		}
	}

	return true
}

// governanceHeldPermission reads the permission a user or group holds out of
// the output of a bb permissions listing, or "" when it holds none.
func governanceHeldPermission(t *testing.T, listOutput, name string) string {
	t.Helper()

	entries, _ := decodeJSONMap(t, listOutput)["entries"].([]any)
	for _, entry := range entries {
		if record, ok := entry.(map[string]any); ok && strings.EqualFold(asString(record["name"]), name) {
			return asString(record["permission"])
		}
	}

	return ""
}
