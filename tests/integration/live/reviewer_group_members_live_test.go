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
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
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
	// Read back, because nothing below would notice a grant that was not kept:
	// Bitbucket takes a reviewer group member who cannot see the repository.
	repoGrants := mustLiveCLI(t, "repo", "settings", "security", "permissions", "users", "list", "--repo", repoRef)
	for _, user := range []string{first.Username, second.Username} {
		if held := governanceHeldPermission(t, repoGrants, user); held != "REPO_READ" {
			t.Fatalf("%s holds %q on the repository, want REPO_READ:\n%s", user, held, repoGrants)
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

	// listedGroup reads a group back from the repository's listing, which
	// carries the name and description a create or update sent.
	listedGroup := func(t *testing.T, groupID string) map[string]any {
		t.Helper()
		output := mustLiveCLI(t, "reviewer-group", "list", "--repo", repoRef)
		group, found := governanceReviewerGroup(t, output, groupID)
		if !found {
			t.Fatalf("group %s is not in the repository's listing:\n%s", groupID, output)
		}
		return group
	}

	// projectReadHeld reads a project grant back, for the same reason as the
	// repository grants above.
	projectReadHeld := func(t *testing.T, username string) {
		t.Helper()
		output := mustLiveCLI(t, "project", "permissions", "users", "list", seeded.Key)
		if held := governanceHeldPermission(t, output, username); held != "PROJECT_READ" {
			t.Fatalf("%s holds %q on the project, want PROJECT_READ:\n%s", username, held, output)
		}
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
		if name := listedGroup(t, groupID)["name"]; name != "qa-repo" {
			t.Errorf("group %s is stored as %v, want qa-repo", groupID, name)
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
		if name := listedGroup(t, groupID)["name"]; name != "qa-replace" {
			t.Errorf("group %s is stored as %v after a members-only update, want qa-replace", groupID, name)
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
		// And the description it did send is the one stored.
		if listed := listedGroup(t, groupID); listed["description"] != "renamed only" || listed["name"] != "qa-keep" {
			t.Errorf("group %s is stored as %v with description %v, want qa-keep with %q",
				groupID, listed["name"], listed["description"], "renamed only")
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
		if listing := mustLiveCLI(t, "reviewer-group", "list", "--repo", repoRef); governanceReviewerGroupNamed(t, listing, "qa-empty") {
			t.Errorf("the refused create left a group behind:\n%s", listing)
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
		if listing := mustLiveCLI(t, "reviewer-group", "list", "--repo", repoRef); governanceReviewerGroupNamed(t, listing, "qa-unknown") {
			t.Errorf("the refused create left a group behind:\n%s", listing)
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
		if name := listedGroup(t, groupID)["name"]; name != "qa-intact" {
			t.Errorf("group %s is stored as %v after a refused update, want qa-intact", groupID, name)
		}
	})

	t.Run("the project scope takes members too", func(t *testing.T) {
		if err := harness.grantProjectPermission(ctx, seeded.Key, first.Username, "PROJECT_READ"); err != nil {
			t.Fatalf("grant project read failed: %v", err)
		}
		projectReadHeld(t, first.Username)

		// Unscoped: a project-scoped reviewer group is named by --project, which
		// the command refuses alongside the --repo the harness would supply.
		output := mustLiveCLIUnscoped(t, "reviewer-group", "create", "qa-project",
			"--project", seeded.Key, "--users", first.Username)

		group := decodeJSONMap(t, output)
		if scope, _ := group["scope"].(string); scope != "PROJECT" {
			t.Errorf("scope = %q, want PROJECT:\n%s", scope, output)
		}
		if users, _ := group["users"].([]any); len(users) != 1 {
			t.Errorf("project group returned %d members, want 1:\n%s", len(users), output)
		}

		// What the create answered is asked again of the project's listing,
		// which has no users command of its own and carries the members inline.
		groupID, _ := numericOrStringID(group["id"])
		listing := mustLiveCLIUnscoped(t, "reviewer-group", "list", "--project", seeded.Key)
		listed, found := governanceReviewerGroup(t, listing, groupID)
		if !found {
			t.Fatalf("project group %q is not in the project's listing:\n%s", groupID, listing)
		}
		if listed["name"] != "qa-project" || listed["scope"] != "PROJECT" {
			t.Errorf("project group %s is stored as %v with scope %v, want qa-project on the project", groupID, listed["name"], listed["scope"])
		}
		if members := governanceUserNames(listed["users"]); !governanceSameNames(members, []string{first.Username}) {
			t.Errorf("project group members = %v, want just %s", members, first.Username)
		}
	})

	t.Run("the project scope replaces and refuses like the repository one", func(t *testing.T) {
		// The project paths are a second copy of the repository ones, so they
		// are a second place for the members to go missing.
		if err := harness.grantProjectPermission(ctx, seeded.Key, second.Username, "PROJECT_READ"); err != nil {
			t.Fatalf("grant project read failed: %v", err)
		}
		projectReadHeld(t, second.Username)

		output := mustLiveCLIUnscoped(t, "reviewer-group", "create", "qa-project-2",
			"--project", seeded.Key, "--users", first.Username)
		groupID := fmt.Sprintf("%d", int64(decodeJSONMap(t, output)["id"].(float64)))

		updated := mustLiveCLIUnscoped(t, "reviewer-group", "update", groupID,
			"--project", seeded.Key, "--users", second.Username)

		users, _ := decodeJSONMap(t, updated)["users"].([]any)
		if len(users) != 1 {
			t.Fatalf("the project update returned %d members, want 1:\n%s", len(users), updated)
		}
		if name, _ := users[0].(map[string]any)["name"].(string); !strings.EqualFold(name, second.Username) {
			t.Errorf("member = %q, want %s", name, second.Username)
		}

		listing := mustLiveCLIUnscoped(t, "reviewer-group", "list", "--project", seeded.Key)
		listed, found := governanceReviewerGroup(t, listing, groupID)
		if !found {
			t.Fatalf("project group %s is not in the project's listing:\n%s", groupID, listing)
		}
		if members := governanceUserNames(listed["users"]); !governanceSameNames(members, []string{second.Username}) {
			t.Errorf("after the update the project group holds %v, want just %s", members, second.Username)
		}
		if listed["name"] != "qa-project-2" {
			t.Errorf("project group %s is stored as %v, want qa-project-2", groupID, listed["name"])
		}

		// Unscoped like the calls above, and for a second reason: with a --repo
		// injected this refusal would be about the two scope flags rather than
		// the member, so the test would pass without checking anything.
		_, err := executeLiveCLIUnscoped(t, "--json", "reviewer-group", "create", "qa-project-unknown",
			"--project", seeded.Key, "--users", "nobody-by-that-name")
		if err == nil {
			t.Error("an unknown member was accepted on a project create")
		} else if strings.Contains(err.Error(), "--repo") {
			t.Errorf("the refusal was about the flags rather than the member: %v", err)
		}
		if listing := mustLiveCLIUnscoped(t, "reviewer-group", "list", "--project", seeded.Key); governanceReviewerGroupNamed(t, listing, "qa-project-unknown") {
			t.Errorf("the refused project create left a group behind:\n%s", listing)
		}
	})
}
