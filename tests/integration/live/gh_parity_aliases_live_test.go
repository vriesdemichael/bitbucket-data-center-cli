//go:build live

package live_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
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
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// A user and a group, each holding a permission, so every listing has
	// something in it. Empty listings are all equal, and a shallow spelling that
	// lost --group would still have matched the deep group listing.
	user, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, user.Username,
		openapigenerated.SetPermissionForUserParamsPermissionREPOWRITE); err != nil {
		t.Fatalf("grant the user repository write failed: %v", err)
	}
	assertMutatedRepoPermissionLevel(t, repoRef, false, user.Username, "REPO_WRITE")
	mustLiveCLI(t, "repo", "settings", "security", "permissions", "groups", "grant", licensedGroup, "repo_read")
	assertMutatedRepoPermissionLevel(t, repoRef, true, licensedGroup, "REPO_READ")

	deepUserList, err := executeLiveCLI(t, "--json", "repo", "settings", "security", "permissions", "users", "list", "--limit", "100")
	if err != nil {
		t.Fatalf("deep users list failed: %v\noutput: %s", err, deepUserList)
	}
	assertAliasListing(t, "deep users listing", deepUserList, user.Username, licensedGroup)
	shallowUserList, err := executeLiveCLI(t, "--json", "repo", "permissions", "list", "--limit", "100")
	if err != nil {
		t.Fatalf("shallow permissions list failed: %v\noutput: %s", err, shallowUserList)
	}
	assertAliasParity(t, "repo permissions list",
		deepUserList, "repo settings security permissions users list", shallowUserList, "repo permissions list")

	deepGroupList, err := executeLiveCLI(t, "--json", "repo", "settings", "security", "permissions", "groups", "list", "--limit", "100")
	if err != nil {
		t.Fatalf("deep groups list failed: %v\noutput: %s", err, deepGroupList)
	}
	assertAliasListing(t, "deep groups listing", deepGroupList, licensedGroup, user.Username)
	shallowGroupList, err := executeLiveCLI(t, "--json", "repo", "permissions", "list", "--group", "--limit", "100")
	if err != nil {
		t.Fatalf("shallow permissions list --group failed: %v\noutput: %s", err, shallowGroupList)
	}
	assertAliasParity(t, "repo permissions list --group",
		deepGroupList, "repo settings security permissions groups list", shallowGroupList, "repo permissions list")

	// grant and revoke are compared under --dry-run: the preview is the whole
	// of what distinguishes the two spellings, and comparing it does not leave
	// a mutation for the other half of the comparison to trip over.
	deepGrant, err := executeLiveCLI(t, "--json", "--dry-run", "repo", "settings", "security", "permissions", "users", "grant", "alias-parity-user", "REPO_WRITE")
	if err != nil {
		t.Fatalf("deep users grant dry-run failed: %v\noutput: %s", err, deepGrant)
	}
	shallowGrant, err := executeLiveCLI(t, "--json", "--dry-run", "repo", "permissions", "grant", "alias-parity-user", "REPO_WRITE")
	if err != nil {
		t.Fatalf("shallow permissions grant dry-run failed: %v\noutput: %s", err, shallowGrant)
	}
	assertAliasParity(t, "repo permissions grant",
		deepGrant, "repo settings security permissions users grant", shallowGrant, "repo permissions grant")

	deepRevoke, err := executeLiveCLI(t, "--json", "--dry-run", "repo", "settings", "security", "permissions", "groups", "revoke", "alias-parity-group", "--yes")
	if err != nil {
		t.Fatalf("deep groups revoke dry-run failed: %v\noutput: %s", err, deepRevoke)
	}
	shallowRevoke, err := executeLiveCLI(t, "--json", "--dry-run", "repo", "permissions", "revoke", "--group", "alias-parity-group", "--yes")
	if err != nil {
		t.Fatalf("shallow permissions revoke --group dry-run failed: %v\noutput: %s", err, shallowRevoke)
	}
	assertAliasParity(t, "repo permissions revoke --group",
		deepRevoke, "repo settings security permissions groups revoke", shallowRevoke, "repo permissions revoke")
}

// TestLiveProjectPermissionShallowAliasesMatchDeepPaths is the project-tree
// twin of the test above.
func TestLiveProjectPermissionShallowAliasesMatchDeepPaths(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// The same reason as the repository twin: listings with nothing in them are
	// equal whatever each spelling asked for.
	user, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	if err := harness.grantProjectPermission(ctx, seeded.Key, user.Username, "PROJECT_READ"); err != nil {
		t.Fatalf("grant the user project read failed: %v", err)
	}
	assertMutatedProjectPermissionLevel(t, seeded.Key, false, user.Username, "PROJECT_READ")
	mustLiveCLI(t, "project", "permissions", "groups", "grant", seeded.Key, licensedGroup, "PROJECT_WRITE")
	assertMutatedProjectPermissionLevel(t, seeded.Key, true, licensedGroup, "PROJECT_WRITE")

	deepUserList, err := executeLiveCLI(t, "--json", "project", "permissions", "users", "list", seeded.Key, "--limit", "100")
	if err != nil {
		t.Fatalf("deep project users list failed: %v\noutput: %s", err, deepUserList)
	}
	assertAliasListing(t, "deep project users listing", deepUserList, user.Username, licensedGroup)
	shallowUserList, err := executeLiveCLI(t, "--json", "project", "permissions", "list", seeded.Key, "--limit", "100")
	if err != nil {
		t.Fatalf("shallow project permissions list failed: %v\noutput: %s", err, shallowUserList)
	}
	assertAliasParity(t, "project permissions list",
		deepUserList, "project permissions users list", shallowUserList, "project permissions list")

	deepGroupList, err := executeLiveCLI(t, "--json", "project", "permissions", "groups", "list", seeded.Key, "--limit", "100")
	if err != nil {
		t.Fatalf("deep project groups list failed: %v\noutput: %s", err, deepGroupList)
	}
	assertAliasListing(t, "deep project groups listing", deepGroupList, licensedGroup, user.Username)
	shallowGroupList, err := executeLiveCLI(t, "--json", "project", "permissions", "list", "--group", seeded.Key, "--limit", "100")
	if err != nil {
		t.Fatalf("shallow project permissions list --group failed: %v\noutput: %s", err, shallowGroupList)
	}
	assertAliasParity(t, "project permissions list --group",
		deepGroupList, "project permissions groups list", shallowGroupList, "project permissions list")

	deepGrant, err := executeLiveCLI(t, "--json", "--dry-run", "project", "permissions", "users", "grant", seeded.Key, "alias-parity-user", "PROJECT_WRITE")
	if err != nil {
		t.Fatalf("deep project users grant dry-run failed: %v\noutput: %s", err, deepGrant)
	}
	shallowGrant, err := executeLiveCLI(t, "--json", "--dry-run", "project", "permissions", "grant", seeded.Key, "alias-parity-user", "PROJECT_WRITE")
	if err != nil {
		t.Fatalf("shallow project permissions grant dry-run failed: %v\noutput: %s", err, shallowGrant)
	}
	assertAliasParity(t, "project permissions grant",
		deepGrant, "project permissions users grant", shallowGrant, "project permissions grant")

	deepRevoke, err := executeLiveCLI(t, "--json", "--dry-run", "project", "permissions", "groups", "revoke", seeded.Key, "alias-parity-group", "--yes")
	if err != nil {
		t.Fatalf("deep project groups revoke dry-run failed: %v\noutput: %s", err, deepRevoke)
	}
	shallowRevoke, err := executeLiveCLI(t, "--json", "--dry-run", "project", "permissions", "revoke", "--group", seeded.Key, "alias-parity-group", "--yes")
	if err != nil {
		t.Fatalf("shallow project permissions revoke --group dry-run failed: %v\noutput: %s", err, shallowRevoke)
	}
	assertAliasParity(t, "project permissions revoke --group",
		deepRevoke, "project permissions groups revoke", shallowRevoke, "project permissions revoke")
}

// assertAliasParity compares what a shallow spelling wrote with what its deep
// path wrote, byte for byte, but for meta.command: that names the spelling
// that ran, the one --describe answers for, so each has to name its own and the
// rest has to be the same document.
func assertAliasParity(t *testing.T, what, deep, deepCommand, shallow, shallowCommand string) {
	t.Helper()

	// Each document's own name, swapped for one they share.
	unnamed := func(output, command string) string {
		t.Helper()

		name := `"command": "` + command + `"`
		if strings.Count(output, name) != 1 {
			t.Fatalf("%s: expected meta.command to name bb %s:\n%s", what, command, output)
		}

		return strings.Replace(output, name, `"command": "<spelling>"`, 1)
	}

	if unnamed(deep, deepCommand) != unnamed(shallow, shallowCommand) {
		t.Fatalf("%s diverged from the deep path\ndeep:    %s\nshallow: %s", what, deep, shallow)
	}
}

// assertAliasListing checks that a permission listing names holds and does not
// name lacks: the user in a users listing and not the group, or the other way
// about. Other entries may be there too -- a project's creator holds its admin
// permission explicitly.
func assertAliasListing(t *testing.T, listing, output, holds, lacks string) {
	t.Helper()

	entries, _ := decodeJSONMap(t, output)["entries"].([]any)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		record, _ := entry.(map[string]any)
		names = append(names, asString(record["name"]))
	}
	if !slices.Contains(names, holds) || slices.Contains(names, lacks) {
		t.Fatalf("the %s names %v, want %s among them and not %s:\n%s", listing, names, holds, lacks, output)
	}
}

// TestLivePullRequestStatus exercises bb pr status against a real Bitbucket.
//
// The two dashboard sections are cross-repository and always answerable. The
// current-branch section depends on where the command is standing, so the
// subtest that reads it stands somewhere known rather than wherever the suite
// was started.
func TestLivePullRequestStatus(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
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
	// user made a moment ago has authored nothing, has been asked to review
	// nothing, and stands on a branch with no pull request open on it. The
	// admin cannot show this -- the rest of the live suite fills their board.
	t.Run("nothing anywhere", func(t *testing.T) {
		newcomer, err := harness.createLicensedUser(ctx)
		if err != nil {
			t.Fatalf("create user failed: %v", err)
		}
		if err := harness.grantProjectPermission(ctx, seeded.Key, newcomer.Username, "PROJECT_READ"); err != nil {
			t.Fatalf("grant project permission failed: %v", err)
		}

		configureLiveCLIEnvForUser(t, harness, seeded.Key, repo.Slug, newcomer)

		// A repository of its own, standing on a branch that has no pull
		// request. The section reads the branch from the working directory, and
		// CI checks out a merge ref, so running here reported "not on a branch"
		// -- true, and a different sentence from the one this is about. A
		// branch with nothing open on it is the state being tested, and it has
		// to be arranged rather than inherited from wherever the suite runs.
		workingDirectory := t.TempDir()
		if err := runGit(workingDirectory, "init"); err != nil {
			t.Fatalf("git init failed: %v", err)
		}
		// No commit: symbolic-ref answers on an unborn branch, which is what
		// the command reads.
		if err := runGit(workingDirectory, "checkout", "-b", "feature/no-pull-request-here"); err != nil {
			t.Fatalf("git checkout -b failed: %v", err)
		}

		originalDirectory, wdErr := os.Getwd()
		if wdErr != nil {
			t.Fatalf("getwd failed: %v", wdErr)
		}
		if err := os.Chdir(workingDirectory); err != nil {
			t.Fatalf("chdir failed: %v", err)
		}
		t.Cleanup(func() {
			_ = os.Chdir(originalDirectory)
		})

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

		// What the section is narrowed on, read back from each pull request
		// rather than inferred from the section: the reviewer the harness added,
		// approved on one and not yet on the other.
		for id, want := range map[string]string{approved: "APPROVED", pending: "UNAPPROVED"} {
			participant, found := repoCLIReviewerIn(t, mustLiveCLI(t, "pr", "get", id), reviewer.Username)
			if !found || participant["role"] != "REVIEWER" || participant["status"] != want {
				t.Fatalf("%s is on pull request %s as %v, want a %s REVIEWER", reviewer.Username, id, participant, want)
			}
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
