//go:build live

package live_test

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// TestLiveRepoPermissionShallowAliasesMatchDeepPaths pins the shallow spellings
// added for issue #338 to the canonical deep ones.
//
// The point of an alias is that it is not a second implementation. Byte
// equality against a real Bitbucket is what keeps that true: if the two ever
// stop being the same command, this fails rather than the two quietly drifting
// the way hand-maintained duplicates do.
func TestLiveRepoPermissionShallowAliasesMatchDeepPaths(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	deepUserList, err := executeLiveCLI(t, "--json", "repo", "settings", "security", "permissions", "users", "list", "--limit", "100")
	if err != nil {
		t.Fatalf("deep users list failed: %v\noutput: %s", err, deepUserList)
	}
	shallowUserList, err := executeLiveCLI(t, "--json", "repo", "permissions", "list", "--limit", "100")
	if err != nil {
		t.Fatalf("shallow permissions list failed: %v\noutput: %s", err, shallowUserList)
	}
	if deepUserList != shallowUserList {
		t.Fatalf("repo permissions list diverged from the deep path\ndeep:    %s\nshallow: %s", deepUserList, shallowUserList)
	}

	deepGroupList, err := executeLiveCLI(t, "--json", "repo", "settings", "security", "permissions", "groups", "list", "--limit", "100")
	if err != nil {
		t.Fatalf("deep groups list failed: %v\noutput: %s", err, deepGroupList)
	}
	shallowGroupList, err := executeLiveCLI(t, "--json", "repo", "permissions", "list", "--group", "--limit", "100")
	if err != nil {
		t.Fatalf("shallow permissions list --group failed: %v\noutput: %s", err, shallowGroupList)
	}
	if deepGroupList != shallowGroupList {
		t.Fatalf("repo permissions list --group diverged from the deep path\ndeep:    %s\nshallow: %s", deepGroupList, shallowGroupList)
	}

	// grant and revoke are compared under --dry-run: the predicted plan is the
	// whole of what distinguishes the two spellings, and comparing it does not
	// leave a mutation for the other half of the comparison to trip over.
	deepGrant, err := executeLiveCLI(t, "--json", "--dry-run", "repo", "settings", "security", "permissions", "users", "grant", "alias-parity-user", "REPO_WRITE")
	if err != nil {
		t.Fatalf("deep users grant dry-run failed: %v\noutput: %s", err, deepGrant)
	}
	shallowGrant, err := executeLiveCLI(t, "--json", "--dry-run", "repo", "permissions", "grant", "alias-parity-user", "REPO_WRITE")
	if err != nil {
		t.Fatalf("shallow permissions grant dry-run failed: %v\noutput: %s", err, shallowGrant)
	}
	if deepGrant != shallowGrant {
		t.Fatalf("repo permissions grant diverged from the deep path\ndeep:    %s\nshallow: %s", deepGrant, shallowGrant)
	}

	deepRevoke, err := executeLiveCLI(t, "--json", "--dry-run", "repo", "settings", "security", "permissions", "groups", "revoke", "alias-parity-group")
	if err != nil {
		t.Fatalf("deep groups revoke dry-run failed: %v\noutput: %s", err, deepRevoke)
	}
	shallowRevoke, err := executeLiveCLI(t, "--json", "--dry-run", "repo", "permissions", "revoke", "--group", "alias-parity-group")
	if err != nil {
		t.Fatalf("shallow permissions revoke --group dry-run failed: %v\noutput: %s", err, shallowRevoke)
	}
	if deepRevoke != shallowRevoke {
		t.Fatalf("repo permissions revoke --group diverged from the deep path\ndeep:    %s\nshallow: %s", deepRevoke, shallowRevoke)
	}
}

// TestLiveProjectPermissionShallowAliasesMatchDeepPaths is the project-tree
// twin of the test above.
func TestLiveProjectPermissionShallowAliasesMatchDeepPaths(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	deepUserList, err := executeLiveCLI(t, "--json", "project", "permissions", "users", "list", seeded.Key, "--limit", "100")
	if err != nil {
		t.Fatalf("deep project users list failed: %v\noutput: %s", err, deepUserList)
	}
	shallowUserList, err := executeLiveCLI(t, "--json", "project", "permissions", "list", seeded.Key, "--limit", "100")
	if err != nil {
		t.Fatalf("shallow project permissions list failed: %v\noutput: %s", err, shallowUserList)
	}
	if deepUserList != shallowUserList {
		t.Fatalf("project permissions list diverged\ndeep:    %s\nshallow: %s", deepUserList, shallowUserList)
	}

	deepGroupList, err := executeLiveCLI(t, "--json", "project", "permissions", "groups", "list", seeded.Key, "--limit", "100")
	if err != nil {
		t.Fatalf("deep project groups list failed: %v\noutput: %s", err, deepGroupList)
	}
	shallowGroupList, err := executeLiveCLI(t, "--json", "project", "permissions", "list", "--group", seeded.Key, "--limit", "100")
	if err != nil {
		t.Fatalf("shallow project permissions list --group failed: %v\noutput: %s", err, shallowGroupList)
	}
	if deepGroupList != shallowGroupList {
		t.Fatalf("project permissions list --group diverged\ndeep:    %s\nshallow: %s", deepGroupList, shallowGroupList)
	}

	deepGrant, err := executeLiveCLI(t, "--json", "--dry-run", "project", "permissions", "users", "grant", seeded.Key, "alias-parity-user", "PROJECT_WRITE")
	if err != nil {
		t.Fatalf("deep project users grant dry-run failed: %v\noutput: %s", err, deepGrant)
	}
	shallowGrant, err := executeLiveCLI(t, "--json", "--dry-run", "project", "permissions", "grant", seeded.Key, "alias-parity-user", "PROJECT_WRITE")
	if err != nil {
		t.Fatalf("shallow project permissions grant dry-run failed: %v\noutput: %s", err, shallowGrant)
	}
	if deepGrant != shallowGrant {
		t.Fatalf("project permissions grant diverged\ndeep:    %s\nshallow: %s", deepGrant, shallowGrant)
	}

	deepRevoke, err := executeLiveCLI(t, "--json", "--dry-run", "project", "permissions", "groups", "revoke", seeded.Key, "alias-parity-group")
	if err != nil {
		t.Fatalf("deep project groups revoke dry-run failed: %v\noutput: %s", err, deepRevoke)
	}
	shallowRevoke, err := executeLiveCLI(t, "--json", "--dry-run", "project", "permissions", "revoke", "--group", seeded.Key, "alias-parity-group")
	if err != nil {
		t.Fatalf("shallow project permissions revoke --group dry-run failed: %v\noutput: %s", err, shallowRevoke)
	}
	if deepRevoke != shallowRevoke {
		t.Fatalf("project permissions revoke --group diverged\ndeep:    %s\nshallow: %s", deepRevoke, shallowRevoke)
	}
}

// TestLivePullRequestStatus exercises bb pr status against a real Bitbucket.
//
// The two dashboard sections are cross-repository and always answerable. The
// current-branch section is not: the live CLI does not run inside a Bitbucket
// checkout, so it reports why rather than failing, and asserting that is
// asserting the degradation actually degrades.
func TestLivePullRequestStatus(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	output, err := executeLiveCLI(t, "--json", "pr", "status")
	if err != nil {
		t.Fatalf("pr status failed: %v\noutput: %s", err, output)
	}

	payload := decodeJSONMap(t, output)
	for _, section := range []string{"currentBranch", "createdByYou", "requestingYourReview"} {
		value, ok := payload[section].(map[string]any)
		if !ok {
			t.Fatalf("expected %q section in pr status output: %s", section, output)
		}
		if _, ok := value["pullRequests"]; !ok {
			t.Fatalf("expected pull_requests in %q section: %s", section, output)
		}
	}

	humanOutput, err := executeLiveCLI(t, "pr", "status")
	if err != nil {
		t.Fatalf("pr status (human) failed: %v\noutput: %s", err, humanOutput)
	}
	for _, heading := range []string{"Current branch", "Created by you", "Requesting a code review from you"} {
		if !strings.Contains(humanOutput, heading) {
			t.Fatalf("expected %q heading in pr status output, got: %s", heading, humanOutput)
		}
	}

	// Every section empty, which is a real state rather than a written one: a
	// user made a moment ago has authored nothing and been asked to review
	// nothing, and the branch checked out here has no pull request in the
	// project just seeded. The admin cannot show this -- the rest of the live
	// suite fills their board.
	t.Run("nothing anywhere", func(t *testing.T) {
		newcomer, err := harness.createLicensedUser(ctx)
		if err != nil {
			t.Fatalf("create user failed: %v", err)
		}
		if err := harness.grantProjectPermission(ctx, seeded.Key, newcomer.Username, "PROJECT_READ"); err != nil {
			t.Fatalf("grant project permission failed: %v", err)
		}

		configureLiveCLIEnvForUser(t, harness, seeded.Key, repo.Slug, newcomer)

		empty, err := executeLiveCLI(t, "pr", "status")
		if err != nil {
			t.Fatalf("pr status as a new user failed: %v\noutput: %s", err, empty)
		}
		for _, message := range []string{
			"No pull request for the current branch",
			"You have no open pull requests",
			"You have no pull requests to review",
		} {
			if !strings.Contains(empty, message) {
				t.Fatalf("an empty section printed nothing that says so, want %q:\n%s", message, empty)
			}
		}
	})

	// The narrowing behind the section's name.
	//
	// role=REVIEWER alone means "you are a reviewer", which keeps listing pull
	// requests you already approved. What makes the section mean "waiting on
	// you" is participantStatus=UNAPPROVED, and it has to be asked for --
	// Bitbucket's default is every status. A unit test asserted this against a
	// dashboard it answered itself, which is the query deciding its own result.
	t.Run("only reviews not yet given", func(t *testing.T) {
		reviewer, err := harness.createLicensedUser(ctx)
		if err != nil {
			t.Fatalf("create reviewer failed: %v", err)
		}
		if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, reviewer.Username,
			openapigenerated.SetPermissionForUserParamsPermissionREPOWRITE); err != nil {
			t.Fatalf("grant the reviewer write access failed: %v", err)
		}

		// Two pull requests, both with the same reviewer on them. One gets
		// approved and one does not, so the section has something to leave out
		// as well as something to show.
		ids := make([]string, 0, 2)
		for index, branch := range []string{"feature/status-approved", "feature/status-pending"} {
			if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch,
				fmt.Sprintf("status-%d.txt", index)); err != nil {
				t.Fatalf("push %s failed: %v", branch, err)
			}
			id, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
			if err != nil {
				t.Fatalf("create the pull request on %s failed: %v", branch, err)
			}
			if _, err := harness.liveJSON(ctx, http.MethodPost,
				fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/pull-requests/%s/participants",
					seeded.Key, repo.Slug, id),
				map[string]any{"user": map[string]any{"name": reviewer.Username}, "role": "REVIEWER"}); err != nil {
				t.Fatalf("add the reviewer to %s failed: %v", branch, err)
			}
			ids = append(ids, id)
		}

		configureLiveCLIEnvForUser(t, harness, seeded.Key, repo.Slug, reviewer)

		approved, pending := ids[0], ids[1]
		if output, err := executeLiveCLI(t, "--json", "pr", "review", "approve", approved); err != nil {
			t.Fatalf("approve failed: %v\noutput: %s", err, output)
		}

		output, err := executeLiveCLI(t, "--json", "pr", "status")
		if err != nil {
			t.Fatalf("pr status as the reviewer failed: %v\noutput: %s", err, output)
		}
		section, ok := decodeJSONMap(t, output)["requestingYourReview"].(map[string]any)
		if !ok {
			t.Fatalf("pr status carries no requestingYourReview section:\n%s", output)
		}
		entries, _ := section["pullRequests"].([]any)

		listed := make([]string, 0, len(entries))
		for _, entry := range entries {
			pullRequest, _ := entry.(map[string]any)
			id, _ := pullRequest["id"].(float64)
			listed = append(listed, fmt.Sprintf("%d", int(id)))
		}
		if !slices.Contains(listed, pending) {
			t.Errorf("the pull request still waiting on this reviewer is missing: %v\n%s", listed, output)
		}
		if slices.Contains(listed, approved) {
			t.Errorf("a pull request this reviewer already approved is still being asked for: %v\n%s", listed, output)
		}
	})
}
