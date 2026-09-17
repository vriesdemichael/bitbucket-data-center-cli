//go:build live

package live_test

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// The five commands command-reach reported as masked: their only live coverage
// threw the result away, so the suite passed whether or not they worked.
//
// `_, _ = executeLiveCLI(...)` runs the command and learns nothing, which is
// the same error as counting a --dry-run invocation. Each of these now changes
// something and reads it back.
func TestLiveReviewerConditionUpdateAndDelete(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// The endpoint refuses a condition with no reviewers, and wants them by
	// numeric id (OPENAPI-026).
	reviewerID, err := harness.userID(ctx, harness.username())
	if err != nil {
		t.Fatalf("look up the reviewer id: %v", err)
	}

	create := fmt.Sprintf(`{"sourceMatcher":{"id":"ANY_REF","type":{"id":"ANY_REF"}},`+
		`"targetMatcher":{"id":"refs/heads/master","type":{"id":"BRANCH"}},`+
		`"reviewers":[{"id":%d}],"requiredApprovals":1}`, reviewerID)
	created := mustLiveCLI(t, "reviewer", "condition", "create", create, "--repo", repoRef)

	id := conditionIDFrom(t, created)
	if id == "" {
		t.Fatalf("no condition id in:\n%s", created)
	}

	stored, found := maskedConditionFrom(t, mustLiveCLI(t, "reviewer", "condition", "list", "--repo", repoRef), id)
	if !found {
		t.Fatalf("the condition just created (id %s) is not in the listing", id)
	}
	assertMaskedConditionStored(t, stored, maskedCondition{
		approvals: 1, reviewerID: reviewerID,
		sourceType: "ANY_REF",
		targetID:   "refs/heads/master", targetType: "BRANCH",
	})

	t.Run("update changes the approvals", func(t *testing.T) {
		// The matchers change as well. An update carrying the create's matchers
		// again reads back the same whether or not they arrived.
		update := fmt.Sprintf(`{"sourceMatcher":{"id":"feature/*","type":{"id":"PATTERN"}},`+
			`"targetMatcher":{"id":"refs/heads/develop","type":{"id":"BRANCH"}},`+
			`"reviewers":[{"id":%d}],"requiredApprovals":2}`, reviewerID)
		mustLiveCLI(t, "reviewer", "condition", "update", id, update, "--repo", repoRef)

		listing := mustLiveCLI(t, "reviewer", "condition", "list", "--repo", repoRef)
		if !strings.Contains(listing, `"requiredApprovals": 2`) {
			t.Fatalf("expected the update to take, got:\n%s", listing)
		}

		updated, found := maskedConditionFrom(t, listing, id)
		if !found {
			t.Fatalf("the updated condition (id %s) is not in the listing:\n%s", id, listing)
		}
		assertMaskedConditionStored(t, updated, maskedCondition{
			approvals: 2, reviewerID: reviewerID,
			sourceID: "feature/*", sourceType: "PATTERN",
			targetID: "refs/heads/develop", targetType: "BRANCH",
		})
	})

	t.Run("delete removes it", func(t *testing.T) {
		mustLiveCLI(t, "reviewer", "condition", "delete", id, "--repo", repoRef, "--yes")

		listing := mustLiveCLI(t, "reviewer", "condition", "list", "--repo", repoRef)
		if strings.Contains(listing, `"id": `+id) {
			t.Fatalf("the condition survived the delete:\n%s", listing)
		}
		if _, found := maskedConditionFrom(t, listing, id); found {
			t.Fatalf("the condition survived the delete:\n%s", listing)
		}
	})
}

// maskedCondition is what a default-reviewer condition is expected to hold.
type maskedCondition struct {
	// scope is PROJECT for a condition written to a project, and REPOSITORY
	// when left empty.
	scope      string
	approvals  float64
	reviewerID int64
	// sourceID is left empty for a source matching any ref, which Bitbucket
	// stores under its own id for that matcher whatever id was sent.
	sourceID, sourceType string
	targetID, targetType string
}

// maskedConditionFrom finds a condition by id in `reviewer condition list`
// output.
func maskedConditionFrom(t *testing.T, listing, id string) (map[string]any, bool) {
	t.Helper()

	conditions, _ := decodeJSONMap(t, listing)["conditions"].([]any)
	for _, entry := range conditions {
		if condition, ok := entry.(map[string]any); ok && trimNumeric(condition["id"]) == id {
			return condition, true
		}
	}

	return nil, false
}

func assertMaskedConditionStored(t *testing.T, condition map[string]any, want maskedCondition) {
	t.Helper()

	if condition["requiredApprovals"] != want.approvals {
		t.Errorf("requiredApprovals = %v, want %v", condition["requiredApprovals"], want.approvals)
	}
	// --repo decides where the condition is written; a project condition
	// would be listed here too, inherited, and only its scope tells them apart.
	scope := want.scope
	if scope == "" {
		scope = "REPOSITORY"
	}
	if condition["scope"] != scope {
		t.Errorf("scope = %v, want %s", condition["scope"], scope)
	}

	reviewers, _ := condition["reviewers"].([]any)
	reviewer := map[string]any{}
	if len(reviewers) == 1 {
		reviewer, _ = reviewers[0].(map[string]any)
	}
	if len(reviewers) != 1 || trimNumeric(reviewer["id"]) != strconv.FormatInt(want.reviewerID, 10) {
		t.Errorf("reviewers = %v, want the one with id %d", condition["reviewers"], want.reviewerID)
	}

	matchers := []struct {
		field, id, kind string
	}{
		{"sourceRefMatcher", want.sourceID, want.sourceType},
		{"targetRefMatcher", want.targetID, want.targetType},
	}
	for _, matcher := range matchers {
		stored, _ := condition[matcher.field].(map[string]any)
		if stored["type"] != matcher.kind || (matcher.id != "" && stored["id"] != matcher.id) {
			t.Errorf("%s = %v, want type %s id %q", matcher.field, condition[matcher.field], matcher.kind, matcher.id)
		}
	}
}

func conditionIDFrom(t *testing.T, output string) string {
	t.Helper()

	data := decodeJSONMap(t, output)
	for _, key := range []string{"condition", "reviewerCondition"} {
		if nested, ok := data[key].(map[string]any); ok {
			data = nested

			break
		}
	}
	if id, ok := data["id"]; ok {
		return trimNumeric(id)
	}

	return ""
}

// TestLiveReviewerGroupUpdate covers the rename, whose only coverage discarded
// its result.
func TestLiveReviewerGroupUpdate(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
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
		t.Fatalf("create member failed: %v", err)
	}
	if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, member.Username, "REPO_READ"); err != nil {
		t.Fatalf("grant read access failed: %v", err)
	}

	const original = "before_rename"
	if err := harness.createReviewerGroup(ctx, seeded.Key, repo.Slug, original, member.Username); err != nil {
		t.Fatalf("create reviewer group failed: %v", err)
	}

	// The dry run has to reach the same conclusion as the run. It resolved the
	// argument as a numeric id only, so a group addressed by name was predicted
	// "blocked: reviewer group not found" by the preview and renamed by the
	// command -- a preview that contradicts the run is worse than none.
	preview := mustLiveCLI(t, "--dry-run", "reviewer-group", "update", original,
		"--name", "after_rename", "--repo", repoRef)
	if !strings.Contains(preview, `"predictedAction": "update"`) {
		t.Fatalf("the dry run disagrees with the run it previews:\n%s", preview)
	}

	beforeRun := mustLiveCLI(t, "reviewer-group", "list", "--repo", repoRef)
	if !strings.Contains(beforeRun, original) {
		t.Fatalf("the dry run renamed the group:\n%s", beforeRun)
	}
	groupID := maskedReviewerGroupID(t, beforeRun, original)

	mustLiveCLI(t, "reviewer-group", "update", original, "--name", "after_rename", "--repo", repoRef)

	listing := mustLiveCLI(t, "reviewer-group", "list", "--repo", repoRef)
	if !strings.Contains(listing, "after_rename") {
		t.Fatalf("the rename did not take:\n%s", listing)
	}
	if strings.Contains(listing, original) {
		t.Fatalf("the old name is still there:\n%s", listing)
	}
	// The same group under the new name, not a second group beside it.
	if renamed := maskedReviewerGroupID(t, listing, "after_rename"); renamed != groupID {
		t.Fatalf("after_rename is group %s, want the renamed group %s:\n%s", renamed, groupID, listing)
	}

	// A rename must not empty the group, which is the #511 question and the one
	// the discarded-result test could never have asked.
	users := mustLiveCLI(t, "reviewer-group", "users", "after_rename", "--repo", repoRef)
	if !strings.Contains(users, member.Username) {
		t.Fatalf("the rename lost the group's member:\n%s", users)
	}
	members, _ := decodeJSONMap(t, users)["users"].([]any)
	if len(members) != 1 {
		t.Fatalf("want the one member the group was created with, got:\n%s", users)
	}
	if name, _ := members[0].(map[string]any)["name"].(string); name != member.Username {
		t.Fatalf("the group's member is %q, want %q:\n%s", name, member.Username, users)
	}

	// The other side of the id-or-name resolution: a name that is not there.
	//
	// It has to say so rather than fail on the decode, because the resolution
	// happens before the request and the endpoint would otherwise be sent a
	// group name where it expects a number -- which came back as a transient
	// error and told a caller to retry a name that will never exist.
	t.Run("a group that is not there is reported as missing", func(t *testing.T) {
		for _, args := range [][]string{
			{"reviewer-group", "update", "no-such-group", "--name", "x", "--repo", repoRef},
			{"reviewer-group", "delete", "no-such-group", "--repo", repoRef, "--yes"},
			{"reviewer-group", "update", "no-such-group", "--name", "x", "--project", seeded.Key},
			{"reviewer-group", "delete", "no-such-group", "--project", seeded.Key, "--yes"},
		} {
			output, err := executeLiveCLI(t, append([]string{"--json"}, args...)...)
			if err == nil {
				t.Errorf("%s succeeded for a group that does not exist:\n%s", strings.Join(args, " "), output)

				continue
			}
			if code := apperrors.ExitCode(err); code != 4 {
				t.Errorf("%s exited %d, want 4 (not_found): %v", strings.Join(args, " "), code, err)
			}
		}

		// Refused, and nothing changed on the way: the group is still there
		// under its name, and neither scope gained one called x.
		repoGroups := mustLiveCLI(t, "reviewer-group", "list", "--repo", repoRef)
		if kept := maskedReviewerGroupID(t, repoGroups, "after_rename"); kept != groupID {
			t.Errorf("after_rename is group %s after the refused calls, want %s", kept, groupID)
		}
		for _, groups := range []string{repoGroups, mustLiveCLI(t, "reviewer-group", "list", "--project", seeded.Key)} {
			if maskedReviewerGroupNamed(t, groups, "x") {
				t.Errorf("a refused update created or renamed a group to x:\n%s", groups)
			}
		}
	})
}

// maskedReviewerGroupID returns the id of the one group of that name in
// `reviewer-group list` output, and fails the test when there is not exactly
// one.
func maskedReviewerGroupID(t *testing.T, listing, name string) string {
	t.Helper()

	id, matches := "", 0
	groups, _ := decodeJSONMap(t, listing)["reviewerGroups"].([]any)
	for _, entry := range groups {
		if group, ok := entry.(map[string]any); ok && group["name"] == name {
			id = trimNumeric(group["id"])
			matches++
		}
	}
	if matches != 1 || id == "" {
		t.Fatalf("want one reviewer group named %s, found %d:\n%s", name, matches, listing)
	}

	return id
}

func maskedReviewerGroupNamed(t *testing.T, listing, name string) bool {
	t.Helper()

	groups, _ := decodeJSONMap(t, listing)["reviewerGroups"].([]any)
	for _, entry := range groups {
		if group, ok := entry.(map[string]any); ok && group["name"] == name {
			return true
		}
	}

	return false
}

// TestLiveGroupPermissionGrants covers the two group grants whose only
// coverage discarded the result.
func TestLiveGroupPermissionGrants(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	t.Run("project permissions groups grant", func(t *testing.T) {
		mustLiveCLI(t, "project", "permissions", "groups", "grant", seeded.Key, licensedGroup, "PROJECT_READ")

		listing := mustLiveCLI(t, "project", "permissions", "list", seeded.Key, "--group", "--all")
		if !strings.Contains(listing, licensedGroup) {
			t.Fatalf("the group grant did not take:\n%s", listing)
		}
		assertMaskedGroupPermission(t, listing, licensedGroup, "PROJECT_READ")

		mustLiveCLI(t, "project", "permissions", "groups", "revoke", seeded.Key, licensedGroup, "--yes")
		assertMaskedGroupPermission(t, mustLiveCLI(t, "project", "permissions", "list", seeded.Key, "--group", "--all"), licensedGroup, "")
	})

	t.Run("repo settings security permissions groups grant", func(t *testing.T) {
		mustLiveCLI(t, "repo", "settings", "security", "permissions", "groups", "grant",
			licensedGroup, "REPO_READ", "--repo", repoRef)

		listing := mustLiveCLI(t, "repo", "permissions", "list", "--repo", repoRef, "--group", "--all")
		if !strings.Contains(listing, licensedGroup) {
			t.Fatalf("the group grant did not take:\n%s", listing)
		}
		assertMaskedGroupPermission(t, listing, licensedGroup, "REPO_READ")

		mustLiveCLI(t, "repo", "settings", "security", "permissions", "groups", "revoke",
			licensedGroup, "--repo", repoRef, "--yes")
		assertMaskedGroupPermission(t, mustLiveCLI(t, "repo", "permissions", "list", "--repo", repoRef, "--group", "--all"), licensedGroup, "")
	})
}

// assertMaskedGroupPermission checks the permission a group holds in a
// permissions listing, or with want empty that it holds none.
func assertMaskedGroupPermission(t *testing.T, listing, group, want string) {
	t.Helper()

	data := decodeJSONMap(t, listing)
	if data["subject"] != "group" {
		t.Fatalf("the listing is of %v, want group:\n%s", data["subject"], listing)
	}

	held := ""
	entries, _ := data["entries"].([]any)
	for _, entry := range entries {
		if record, ok := entry.(map[string]any); ok && record["name"] == group {
			held, _ = record["permission"].(string)
			if held == "" {
				held = "an entry with no permission"
			}
		}
	}

	if held != want {
		t.Fatalf("%s holds %q, want %q:\n%s", group, held, want, listing)
	}
}
