//go:build live

package live_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// The five commands command-reach reported as masked: their only live coverage
// threw the result away, so the suite passed whether or not they worked.
//
// `_, _ = executeLiveCLI(...)` runs the command and learns nothing, which is
// the same error as counting a --dry-run invocation. Each of these now changes
// something and reads it back.
func TestLiveReviewerConditionUpdateAndDelete(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
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

	t.Run("update changes the approvals", func(t *testing.T) {
		update := fmt.Sprintf(`{"sourceMatcher":{"id":"ANY_REF","type":{"id":"ANY_REF"}},`+
			`"targetMatcher":{"id":"refs/heads/master","type":{"id":"BRANCH"}},`+
			`"reviewers":[{"id":%d}],"requiredApprovals":2}`, reviewerID)
		mustLiveCLI(t, "reviewer", "condition", "update", id, update, "--repo", repoRef)

		listing := mustLiveCLI(t, "reviewer", "condition", "list", "--repo", repoRef)
		if !strings.Contains(listing, `"requiredApprovals": 2`) {
			t.Fatalf("expected the update to take, got:\n%s", listing)
		}
	})

	t.Run("delete removes it", func(t *testing.T) {
		mustLiveCLI(t, "reviewer", "condition", "delete", id, "--repo", repoRef)

		listing := mustLiveCLI(t, "reviewer", "condition", "list", "--repo", repoRef)
		if strings.Contains(listing, `"id": `+id) {
			t.Fatalf("the condition survived the delete:\n%s", listing)
		}
	})
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
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
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

	if listing := mustLiveCLI(t, "reviewer-group", "list", "--repo", repoRef); !strings.Contains(listing, original) {
		t.Fatalf("the dry run renamed the group:\n%s", listing)
	}

	mustLiveCLI(t, "reviewer-group", "update", original, "--name", "after_rename", "--repo", repoRef)

	listing := mustLiveCLI(t, "reviewer-group", "list", "--repo", repoRef)
	if !strings.Contains(listing, "after_rename") {
		t.Fatalf("the rename did not take:\n%s", listing)
	}
	if strings.Contains(listing, original) {
		t.Fatalf("the old name is still there:\n%s", listing)
	}

	// A rename must not empty the group, which is the #511 question and the one
	// the discarded-result test could never have asked.
	users := mustLiveCLI(t, "reviewer-group", "users", "after_rename", "--repo", repoRef)
	if !strings.Contains(users, member.Username) {
		t.Fatalf("the rename lost the group's member:\n%s", users)
	}
}

// TestLiveGroupPermissionGrants covers the two group grants whose only
// coverage discarded the result.
func TestLiveGroupPermissionGrants(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
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

		mustLiveCLI(t, "project", "permissions", "groups", "revoke", seeded.Key, licensedGroup)
	})

	t.Run("repo settings security permissions groups grant", func(t *testing.T) {
		mustLiveCLI(t, "repo", "settings", "security", "permissions", "groups", "grant",
			licensedGroup, "REPO_READ", "--repo", repoRef)

		listing := mustLiveCLI(t, "repo", "permissions", "list", "--repo", repoRef, "--group", "--all")
		if !strings.Contains(listing, licensedGroup) {
			t.Fatalf("the group grant did not take:\n%s", listing)
		}

		mustLiveCLI(t, "repo", "settings", "security", "permissions", "groups", "revoke",
			licensedGroup, "--repo", repoRef)
	})
}
