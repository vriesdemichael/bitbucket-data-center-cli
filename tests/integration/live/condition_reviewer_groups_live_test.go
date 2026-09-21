//go:build live

package live_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/compat"
)

// conditionReviewerGroupsSince is the first release that keeps the reviewer
// groups a default reviewer condition names: 9.4.24 answered 200 and dropped
// them, 9.5.2 kept them. Stated here rather than read from internal/compat, so
// a boundary set wrong there fails on a release.
var conditionReviewerGroupsSince = compat.Release{Major: 9, Minor: 5}

// TestLiveConditionReviewerGroups covers a repository's default reviewer
// condition naming a reviewer group, on the release under test.
//
// From 9.5 the group is stored on the condition, by a create and by an update.
// Before it Bitbucket drops the groups and answers 200 -- a condition naming
// only a group is stored with nobody to add -- so bb refuses both, and their dry
// runs, as unsupported, and the test proves nothing was made or changed.
func TestLiveConditionReviewerGroups(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	member, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create the group member failed: %v", err)
	}
	if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, member.Username, "REPO_READ"); err != nil {
		t.Fatalf("grant the member read access failed: %v", err)
	}
	memberID, err := harness.userID(ctx, member.Username)
	if err != nil {
		t.Fatalf("look up the member's id failed: %v", err)
	}
	group := decodeJSONMap(t, mustLiveCLI(t, "reviewer-group", "create", "cond-group", "--repo", repoRef, "--users", member.Username))
	groupID := int64(group["id"].(float64))

	anyRef := `"sourceMatcher":{"id":"ANY_REF","type":{"id":"ANY_REF"}},"targetMatcher":{"id":"ANY_REF","type":{"id":"ANY_REF"}}`
	byGroup := fmt.Sprintf(`{%s,"reviewers":[],"reviewerGroups":[{"id":%d}],"requiredApprovals":1}`, anyRef, groupID)
	byUser := fmt.Sprintf(`{%s,"reviewers":[{"id":%d}],"requiredApprovals":1}`, anyRef, memberID)
	// The update carries a second change every release stores, so a refusal is
	// told apart from a release that took the update and dropped the groups.
	byUserAndGroup := fmt.Sprintf(`{%s,"reviewers":[{"id":%d}],"reviewerGroups":[{"id":%d}],"requiredApprovals":0}`, anyRef, memberID, groupID)

	// A condition naming the user alone, which every release stores: what an
	// update adding the group acts on.
	userConditionID := conditionIDFrom(t, mustLiveCLI(t, "reviewer", "condition", "create", byUser, "--repo", repoRef))

	release := harness.release(t)
	if release.Before(conditionReviewerGroupsSince) {
		// The command words stay in the literal each row spreads, which is the
		// shape tools/command-reach can read.
		for _, args := range [][]string{
			append([]string{"--json", "--dry-run", "reviewer", "condition", "create"}, byGroup, "--repo", repoRef),
			append([]string{"--json", "reviewer", "condition", "create"}, byGroup, "--repo", repoRef),
			append([]string{"--json", "--dry-run", "reviewer", "condition", "update"}, userConditionID, byUserAndGroup, "--repo", repoRef),
			append([]string{"--json", "reviewer", "condition", "update"}, userConditionID, byUserAndGroup, "--repo", repoRef),
		} {
			output, err := executeLiveCLI(t, args...)
			assertUnsupportedOn(t, release, err, output)
		}

		// Nothing but the user's condition, and that one as it was: the refused
		// update would have set its approvals to zero.
		conditions, _ := decodeJSONMap(t, mustLiveCLI(t, "reviewer", "condition", "list", "--repo", repoRef))["conditions"].([]any)
		if len(conditions) != 1 {
			t.Fatalf("want the one condition naming the user, got %d: %v", len(conditions), conditions)
		}
		assertConditionReviewers(t, conditions[0], userConditionID, []string{member.Username}, nil)
		if approvals, _ := conditions[0].(map[string]any); approvals["requiredApprovals"] != float64(1) {
			t.Errorf("condition %s requires %v approvals after the refused update, want the 1 it was created with", userConditionID, approvals["requiredApprovals"])
		}

		return
	}

	groupConditionID := conditionIDFrom(t, mustLiveCLI(t, "reviewer", "condition", "create", byGroup, "--repo", repoRef))
	mustLiveCLI(t, "reviewer", "condition", "update", userConditionID, byUserAndGroup, "--repo", repoRef)

	listing := mustLiveCLI(t, "reviewer", "condition", "list", "--repo", repoRef)
	for id, want := range map[string]struct{ users, groups []string }{
		groupConditionID: {users: nil, groups: []string{"cond-group"}},
		userConditionID:  {users: []string{member.Username}, groups: []string{"cond-group"}},
	} {
		condition, found := maskedConditionFrom(t, listing, id)
		if !found {
			t.Fatalf("condition %s is not in the listing:\n%s", id, listing)
		}
		assertConditionReviewers(t, condition, id, want.users, want.groups)
	}
	// The update's other change, which says the update was taken whole.
	updated, _ := maskedConditionFrom(t, listing, userConditionID)
	if updated["requiredApprovals"] != float64(0) {
		t.Errorf("condition %s requires %v approvals after the update, want the 0 it set", userConditionID, updated["requiredApprovals"])
	}
}

// assertConditionReviewers checks the users and groups a listed condition names,
// exactly.
func assertConditionReviewers(t *testing.T, entry any, id string, users, groups []string) {
	t.Helper()

	condition, _ := entry.(map[string]any)
	if got, _ := numericOrStringID(condition["id"]); got != id {
		t.Fatalf("the condition listed is %v, want %s", condition["id"], id)
	}
	for field, want := range map[string][]string{"reviewers": users, "reviewerGroups": groups} {
		got := governanceUserNames(condition[field])
		if !governanceSameNames(got, want) {
			t.Errorf("condition %s names %s %v, want %v", id, field, got, want)
		}
	}
}
