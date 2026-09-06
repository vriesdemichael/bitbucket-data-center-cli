//go:build live

package live_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestLiveReviewerGroupMembership covers #533: bb could create only reviewer
// groups Bitbucket refuses.
//
// A reviewer group with no members is rejected outright --
// EmptyReviewerGroupException, "Reviewer groups must contain 1 or more
// reviewer(s)" -- and `reviewer-group create` had no way to name one, on either
// scope. So the command could not succeed, and a group that did exist could not
// have its membership changed through bb at all.
//
// Members are recognised by numeric id only. A member given as {"name": ...} is
// dropped silently and the request fails as though nobody had been named, which
// is why the resolution happens in the service rather than being left to the
// caller: the username is the only thing a person has.
func TestLiveReviewerGroupMembership(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	first, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create the first member failed: %v", err)
	}
	second, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create the second member failed: %v", err)
	}
	for _, user := range []string{first.Username, second.Username} {
		if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, user, "REPO_READ"); err != nil {
			t.Fatalf("grant %s read access failed: %v", user, err)
		}
	}

	membersOf := func(t *testing.T, groupID string) []string {
		t.Helper()
		output := mustLiveCLI(t, "reviewer-group", "users", groupID, "--repo", repoRef)
		names := make([]string, 0, 2)
		for _, entry := range collectionFromCLI(t, output, "users") {
			if user, ok := entry.(map[string]any); ok {
				names = append(names, asString(user["name"]))
			}
		}
		return names
	}

	t.Run("create names its members", func(t *testing.T) {
		output := mustLiveCLI(t, "reviewer-group", "create", "qa-repo",
			"--repo", repoRef, "--users", first.Username+","+second.Username)

		group := decodeJSONMap(t, output)
		groupID := fmt.Sprintf("%d", int64(group["id"].(float64)))

		// The create response carries the members, so a caller does not have to
		// ask again to know the group is usable.
		if users, _ := group["users"].([]any); len(users) != 2 {
			t.Errorf("create returned %d members, want 2:\n%s", len(users), output)
		}

		got := membersOf(t, groupID)
		if len(got) != 2 || !containsFold(got, first.Username) || !containsFold(got, second.Username) {
			t.Errorf("group members = %v, want %s and %s", got, first.Username, second.Username)
		}
	})

	t.Run("update replaces the membership", func(t *testing.T) {
		output := mustLiveCLI(t, "reviewer-group", "create", "qa-replace",
			"--repo", repoRef, "--users", first.Username)
		groupID := fmt.Sprintf("%d", int64(decodeJSONMap(t, output)["id"].(float64)))

		mustLiveCLI(t, "reviewer-group", "update", groupID, "--repo", repoRef, "--users", second.Username)

		got := membersOf(t, groupID)
		if len(got) != 1 || !containsFold(got, second.Username) {
			t.Errorf("after --users the members are %v, want just %s", got, second.Username)
		}
	})

	t.Run("update without --users keeps the members", func(t *testing.T) {
		// Bitbucket preserves them itself on a partial update -- unlike the
		// pull request endpoint, where an absent reviewers array means "remove
		// them all" (#511). Pinned because the two behave differently and
		// nothing but a real server says which is which.
		output := mustLiveCLI(t, "reviewer-group", "create", "qa-keep",
			"--repo", repoRef, "--users", first.Username)
		groupID := fmt.Sprintf("%d", int64(decodeJSONMap(t, output)["id"].(float64)))

		mustLiveCLI(t, "reviewer-group", "update", groupID, "--repo", repoRef, "--description", "renamed only")

		if got := membersOf(t, groupID); len(got) != 1 || !containsFold(got, first.Username) {
			t.Errorf("a description-only update changed the members to %v, want just %s", got, first.Username)
		}
	})

	t.Run("create refuses before it asks when no member is named", func(t *testing.T) {
		// The server's own refusal is "Reviewer groups must contain 1 or more
		// reviewer(s)", which does not say that bb has a flag for it.
		output, err := executeLiveCLI(t, "--json", "reviewer-group", "create", "qa-empty", "--repo", repoRef)
		if err == nil {
			t.Fatalf("a group with no members was accepted:\n%s", output)
		}
		if !strings.Contains(err.Error(), "--users") {
			t.Errorf("the refusal does not name the flag that fixes it: %v", err)
		}
		if !strings.Contains(err.Error(), "validation") {
			t.Errorf("kind should be validation, got: %v", err)
		}
	})

	t.Run("an unknown member is named in the refusal", func(t *testing.T) {
		output, err := executeLiveCLI(t, "--json", "reviewer-group", "create", "qa-unknown",
			"--repo", repoRef, "--users", "nobody-by-that-name")
		if err == nil {
			t.Fatalf("an unknown member was accepted:\n%s", output)
		}
		if !strings.Contains(err.Error(), "nobody-by-that-name") {
			t.Errorf("the refusal does not name the user it could not resolve: %v", err)
		}
	})

	t.Run("update refuses an unknown member and changes nothing", func(t *testing.T) {
		// The resolution happens before the request, so a name that resolves to
		// nobody must leave the group as it was rather than half-applying.
		output := mustLiveCLI(t, "reviewer-group", "create", "qa-intact",
			"--repo", repoRef, "--users", first.Username)
		groupID := fmt.Sprintf("%d", int64(decodeJSONMap(t, output)["id"].(float64)))

		refused, err := executeLiveCLI(t, "--json", "reviewer-group", "update", groupID,
			"--repo", repoRef, "--users", "nobody-by-that-name")
		if err == nil {
			t.Fatalf("an unknown member was accepted on update:\n%s", refused)
		}
		if !strings.Contains(err.Error(), "nobody-by-that-name") {
			t.Errorf("the refusal does not name the user: %v", err)
		}

		if got := membersOf(t, groupID); len(got) != 1 || !containsFold(got, first.Username) {
			t.Errorf("a refused update changed the members to %v, want just %s", got, first.Username)
		}
	})

	t.Run("the project scope takes members too", func(t *testing.T) {
		if err := harness.grantProjectPermission(ctx, seeded.Key, first.Username, "PROJECT_READ"); err != nil {
			t.Fatalf("grant project read failed: %v", err)
		}

		output := mustLiveCLI(t, "reviewer-group", "create", "qa-project",
			"--project", seeded.Key, "--users", first.Username)

		group := decodeJSONMap(t, output)
		if scope, _ := group["scope"].(string); scope != "PROJECT" {
			t.Errorf("scope = %q, want PROJECT:\n%s", scope, output)
		}
		if users, _ := group["users"].([]any); len(users) != 1 {
			t.Errorf("project group returned %d members, want 1:\n%s", len(users), output)
		}
	})

	t.Run("the project scope replaces and refuses like the repository one", func(t *testing.T) {
		// The project paths are a second copy of the repository ones, so they
		// are a second place for the members to go missing.
		if err := harness.grantProjectPermission(ctx, seeded.Key, second.Username, "PROJECT_READ"); err != nil {
			t.Fatalf("grant project read failed: %v", err)
		}

		output := mustLiveCLI(t, "reviewer-group", "create", "qa-project-2",
			"--project", seeded.Key, "--users", first.Username)
		groupID := fmt.Sprintf("%d", int64(decodeJSONMap(t, output)["id"].(float64)))

		updated := mustLiveCLI(t, "reviewer-group", "update", groupID,
			"--project", seeded.Key, "--users", second.Username)

		users, _ := decodeJSONMap(t, updated)["users"].([]any)
		if len(users) != 1 {
			t.Fatalf("the project update returned %d members, want 1:\n%s", len(users), updated)
		}
		if name, _ := users[0].(map[string]any)["name"].(string); !strings.EqualFold(name, second.Username) {
			t.Errorf("member = %q, want %s", name, second.Username)
		}

		if _, err := executeLiveCLI(t, "--json", "reviewer-group", "create", "qa-project-unknown",
			"--project", seeded.Key, "--users", "nobody-by-that-name"); err == nil {
			t.Error("an unknown member was accepted on a project create")
		}
	})
}
