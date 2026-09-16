//go:build live

package live_test

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// Real mutating coverage for the commands #532 found were only ever run under
// --dry-run.
//
// A dry run exercises the planning path and deliberately does not send the
// mutation, so it says nothing about whether the command works. Sixteen
// mutating commands were reported as covered on that basis, and command reach
// read 100% while their mutating path had never once reached a server. That is
// how #503, #505, #506 and #511 all shipped green.
//
// Each test below mutates, reads the state back, and restores what it changed.

// TestLivePRReviewApprovalCycle covers `pr review approve` and
// `pr review unapprove`.
//
// Both need somebody other than the author: Bitbucket does not let an author
// approve their own pull request, so the commands run as a second user.
func TestLivePRReviewApprovalCycle(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]

	reviewer, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create reviewer failed: %v", err)
	}
	if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, reviewer.Username, "REPO_WRITE"); err != nil {
		t.Fatalf("grant reviewer write access failed: %v", err)
	}

	branch := "feature/approval-cycle"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "approval.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)
	prID := createLivePRForRegression(t, branch, "Approval cycle", "--no-default-reviewers", "--no-codeowners")
	assertMutatedPullRequestOpened(t, prID, "Approval cycle", branch)

	// Read as the admin, before the switch below: the level itself, because
	// approving needs only read access and a lower grant would pass the cycle.
	assertMutatedRepoPermissionLevel(t, seeded.Key+"/"+repo.Slug, false, reviewer.Username, "REPO_WRITE")

	// From here the CLI runs as the reviewer, not the author.
	configureLiveCLIEnvForUser(t, harness, seeded.Key, repo.Slug, reviewer)

	approveOutput, err := executeLiveCLI(t, "--json", "pr", "review", "approve", prID)
	if err != nil {
		t.Fatalf("pr review approve failed: %v\noutput: %s", err, approveOutput)
	}
	// The command's own reply has to name the pull request it acted on.
	// Bitbucket answers an approve with the participant, not the pull request,
	// and decoding that as a pull request left every field zero -- so this
	// reported #0 while approving the right one, and a machine caller reading
	// the id got 0 (#587).
	assertLiveReplyNamesPullRequest(t, approveOutput, prID)
	assertLiveReviewerApproval(t, prID, reviewer.Username, true)

	unapproveOutput, err := executeLiveCLI(t, "--json", "pr", "review", "unapprove", prID)
	if err != nil {
		t.Fatalf("pr review unapprove failed: %v\noutput: %s", err, unapproveOutput)
	}
	assertLiveReplyNamesPullRequest(t, unapproveOutput, prID)
	assertLiveReviewerApproval(t, prID, reviewer.Username, false)
}

// assertLiveReplyNamesPullRequest checks that a mutating command's own reply
// identifies the pull request, rather than returning a zero-valued one.
func assertLiveReplyNamesPullRequest(t *testing.T, output, prID string) {
	t.Helper()

	id, _ := extractPRData(decodeJSONMap(t, output))["id"].(float64)
	if got := fmt.Sprintf("%d", int(id)); got != prID {
		t.Fatalf("the reply names pull request %s, want %s:\n%s", got, prID, output)
	}
}

// assertLiveReviewerApproval reads the pull request back and checks one
// participant's approval state, so the assertion is the server's answer rather
// than the mutating command's own output.
func assertLiveReviewerApproval(t *testing.T, prID, username string, wantApproved bool) {
	t.Helper()

	output, err := executeLiveCLI(t, "--json", "pr", "get", prID)
	if err != nil {
		t.Fatalf("pr get failed: %v\noutput: %s", err, output)
	}

	pullRequest := extractPRData(decodeJSONMap(t, output))
	reviewers, _ := pullRequest["reviewers"].([]any)

	for _, entry := range reviewers {
		reviewer, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		name, _ := reviewer["name"].(string)
		if !strings.EqualFold(name, username) {
			continue
		}
		if approved, _ := reviewer["approved"].(bool); approved != wantApproved {
			t.Fatalf("%s approved = %v, want %v\noutput: %s", username, approved, wantApproved, output)
		}

		return
	}

	t.Fatalf("%s is not a reviewer on the pull request\noutput: %s", username, output)
}

// mutatedPullRequest reads a pull request back through `pr get`, after a
// command has changed it.
func mutatedPullRequest(t *testing.T, prID string) map[string]any {
	t.Helper()

	return extractPRData(decodeJSONMap(t, mustLiveCLI(t, "pr", "get", prID)))
}

// assertMutatedPullRequestOpened reads back the pull request a test opened.
// Every later step leans on it, so its title and branches are checked as
// stored rather than taken from the create's own reply.
func assertMutatedPullRequestOpened(t *testing.T, prID, title, sourceBranch string) {
	t.Helper()

	stored := mutatedPullRequest(t, prID)
	if stored["title"] != title || stored["sourceBranch"] != sourceBranch || stored["targetBranch"] != "master" {
		t.Fatalf("pull request %s is %q from %v into %v, want %q from %s into master",
			prID, stored["title"], stored["sourceBranch"], stored["targetBranch"], title, sourceBranch)
	}
}

// assertMutatedReviewerRoles checks that a pull request has exactly these
// reviewers, each in the role given.
//
// Presence is not enough: bb lists every participant, so a user stored as a
// PARTICIPANT rather than a REVIEWER is on the list too.
func assertMutatedReviewerRoles(t *testing.T, prID string, want map[string]string) {
	t.Helper()

	reviewers, _ := mutatedPullRequest(t, prID)["reviewers"].([]any)
	got := make(map[string]string, len(reviewers))
	for _, entry := range reviewers {
		fields, _ := entry.(map[string]any)
		name, _ := fields["name"].(string)
		role, _ := fields["role"].(string)
		got[name] = role
	}

	if !maps.Equal(got, want) {
		t.Fatalf("pull request %s has reviewers %v, want %v", prID, got, want)
	}
}

// TestLivePRReviewerAddAndRemove covers `pr review reviewer add` and
// `pr review reviewer remove`.
func TestLivePRReviewerAddAndRemove(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]

	reviewer, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create reviewer failed: %v", err)
	}
	if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, reviewer.Username, "REPO_READ"); err != nil {
		t.Fatalf("grant reviewer read access failed: %v", err)
	}

	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)
	repoRef := seeded.Key + "/" + repo.Slug
	assertMutatedRepoPermissionLevel(t, repoRef, false, reviewer.Username, "REPO_READ")

	branch := "feature/reviewer-add-remove"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "reviewers.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	prID := createLivePRForRegression(t, branch, "Reviewer add and remove", "--no-default-reviewers", "--no-codeowners")
	assertMutatedPullRequestOpened(t, prID, "Reviewer add and remove", branch)

	if names := currentLivePRReviewers(t, prID); len(names) != 0 {
		t.Fatalf("expected the pull request to start with no reviewers, got %v", names)
	}

	addOutput, err := executeLiveCLI(t, "--json", "pr", "review", "reviewer", "add", prID, "--user", reviewer.Username)
	if err != nil {
		t.Fatalf("pr review reviewer add failed: %v\noutput: %s", err, addOutput)
	}
	if names := currentLivePRReviewers(t, prID); !containsFold(names, reviewer.Username) {
		t.Fatalf("expected %s to be a reviewer, got %v", reviewer.Username, names)
	}
	assertMutatedReviewerRoles(t, prID, map[string]string{reviewer.Username: "REVIEWER"})

	// The id in the result is the pull request's, not the participant
	// response's -- that payload carries no id, so reading it from there
	// reported "#0". A unit test caught this against a written reply; here the
	// number has to be the one the pull request really has.
	if id, _ := extractPRData(decodeJSONMap(t, addOutput))["id"].(float64); fmt.Sprintf("%d", int(id)) != prID {
		t.Errorf("reviewer add reported pull request id %v, want %s:\n%s", id, prID, addOutput)
	}

	// A second reviewer, because adding the first one again reports "already
	// present" and never names the pull request.
	second, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create the second reviewer failed: %v", err)
	}
	if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, second.Username, "REPO_READ"); err != nil {
		t.Fatalf("grant the second reviewer read access failed: %v", err)
	}
	assertMutatedRepoPermissionLevel(t, repoRef, false, second.Username, "REPO_READ")

	humanAdd := mustLiveHumanCLI(t, "pr", "review", "reviewer", "add", prID, "--user", second.Username)
	if !strings.Contains(humanAdd, "pull request #"+prID) {
		t.Errorf("the human line names the wrong pull request:\n%s", humanAdd)
	}
	// The line above is the command's own account; the pull request is the
	// evidence that the second reviewer was stored beside the first.
	assertMutatedReviewerRoles(t, prID, map[string]string{reviewer.Username: "REVIEWER", second.Username: "REVIEWER"})

	removeOutput, err := executeLiveCLI(t, "--json", "pr", "review", "reviewer", "remove", prID, "--user", reviewer.Username, "--yes")
	if err != nil {
		t.Fatalf("pr review reviewer remove failed: %v\noutput: %s", err, removeOutput)
	}
	// Removing a participant answers with no body, which decoded as a pull
	// request reported #0 (#587).
	if removed, _ := decodeJSONMap(t, removeOutput)["pullRequest"].(map[string]any); fmt.Sprint(removed["id"]) != prID {
		t.Errorf("reviewer remove reported pull request %v, want %s:\n%s", removed["id"], prID, removeOutput)
	}
	if names := currentLivePRReviewers(t, prID); containsFold(names, reviewer.Username) {
		t.Fatalf("expected %s to have been removed, got %v", reviewer.Username, names)
	}
	// Only the one named: a remove that cleared every participant would pass the
	// check above.
	assertMutatedReviewerRoles(t, prID, map[string]string{second.Username: "REVIEWER"})
}

// TestLiveBranchDefaultSet covers `branch default set`.
func TestLiveBranchDefaultSet(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// A branch to move the default to. Setting it to the branch it already is
	// would pass whether or not the command did anything.
	const branch = "feature/new-default"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "default.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	before := currentLiveDefaultBranch(t)
	if before == branch {
		t.Fatalf("the repository already defaults to %s, so this would prove nothing", branch)
	}

	output, err := executeLiveCLI(t, "--json", "branch", "default", "set", branch)
	if err != nil {
		t.Fatalf("branch default set failed: %v\noutput: %s", err, output)
	}

	if after := currentLiveDefaultBranch(t); after != branch {
		t.Fatalf("default branch = %q, want %q", after, branch)
	}

	// Put it back, so a later test in the same repository is not surprised.
	if restore, err := executeLiveCLI(t, "--json", "branch", "default", "set", before); err != nil {
		t.Fatalf("restoring the default branch failed: %v\noutput: %s", err, restore)
	}
	if after := currentLiveDefaultBranch(t); after != before {
		t.Fatalf("default branch was not restored: got %q, want %q", after, before)
	}
}

func currentLiveDefaultBranch(t *testing.T) string {
	t.Helper()

	output, err := executeLiveCLI(t, "--json", "branch", "default", "get")
	if err != nil {
		t.Fatalf("branch default get failed: %v\noutput: %s", err, output)
	}

	branch, ok := decodeJSONMap(t, output)["defaultBranch"].(map[string]any)
	if !ok {
		t.Fatalf("no defaultBranch in: %s", output)
	}
	for _, key := range []string{"displayId", "id"} {
		if value, ok := branch[key].(string); ok && value != "" {
			return strings.TrimPrefix(value, "refs/heads/")
		}
	}

	t.Fatalf("could not read the default branch from: %s", output)

	return ""

}

// TestLiveBranchModelUpdate covers `branch model update`, which sets the
// default branch the branch model is built around.
func TestLiveBranchModelUpdate(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	const branch = "feature/model-default"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "model.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	before := currentLiveDefaultBranch(t)
	if before == branch {
		t.Fatalf("the repository already defaults to %s, so this would prove nothing", branch)
	}

	output, err := executeLiveCLI(t, "--json", "branch", "model", "update", branch)
	if err != nil {
		t.Fatalf("branch model update failed: %v\noutput: %s", err, output)
	}
	if after := currentLiveDefaultBranch(t); after != branch {
		t.Fatalf("after branch model update the default is %q, want %q", after, branch)
	}

	if restore, err := executeLiveCLI(t, "--json", "branch", "model", "update", before); err != nil {
		t.Fatalf("restoring the branch model default failed: %v\noutput: %s", err, restore)
	}
	// The restore is a second update, with a different branch, and gets the same
	// read.
	if after := currentLiveDefaultBranch(t); after != before {
		t.Fatalf("the branch model default was not restored: got %q, want %q", after, before)
	}
}

// TestLiveRepoAdminFork covers `repo admin fork`, whose live coverage was a
// dry run that by definition creates no fork.
func TestLiveRepoAdminFork(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	forkName := repo.Slug + "-admin-fork"

	output, err := executeLiveCLI(t, "--json", "repo", "admin", "fork", "--name", forkName, "--project", seeded.Key)
	if err != nil {
		t.Fatalf("repo admin fork failed: %v\noutput: %s", err, output)
	}

	// The fork has to exist on the server, not merely in the command's own
	// output, and it has to be a fork rather than a fresh repository: only a
	// fork can synchronize with an upstream.
	listOutput, err := executeLiveCLI(t, "--json", "repo", "list", "--project", seeded.Key)
	if err != nil {
		t.Fatalf("repo list failed: %v\noutput: %s", err, listOutput)
	}
	if !strings.Contains(listOutput, forkName) {
		t.Fatalf("the fork %s is not in the project listing:\n%s", forkName, listOutput)
	}
	var listed []map[string]any
	if err := decodeJSONEnvelopeData(listOutput, &listed); err != nil {
		t.Fatalf("repo list returned invalid JSON: %v\n%s", err, listOutput)
	}
	inProject := false
	for _, entry := range listed {
		if entry["slug"] == forkName && entry["projectKey"] == seeded.Key {
			inProject = true
		}
	}
	if !inProject {
		t.Fatalf("no repository %s/%s in the project listing:\n%s", seeded.Key, forkName, listOutput)
	}

	// The fields the command sent, as stored. Both differ from what Bitbucket
	// does without them: a fork with no project lands in the caller's personal
	// project, and one with no name takes its origin's. The origin is the
	// repository the request addressed.
	stored, ok := decodeJSONMap(t, mustLiveCLI(t, "repo", "get", "--repo", seeded.Key+"/"+forkName, "--readme=false"))["repository"].(map[string]any)
	if !ok {
		t.Fatalf("repo get returned no repository for %s/%s", seeded.Key, forkName)
	}
	if stored["name"] != forkName || stored["projectKey"] != seeded.Key {
		t.Errorf("the fork is stored as %v in %v, want %s in %s", stored["name"], stored["projectKey"], forkName, seeded.Key)
	}
	if origin, _ := stored["origin"].(map[string]any); origin["projectKey"] != seeded.Key || origin["slug"] != repo.Slug {
		t.Errorf("the fork's origin is %v, want %s/%s", stored["origin"], seeded.Key, repo.Slug)
	}

	syncOutput, err := executeLiveCLI(t, "--json", "repo", "sync", "status", "--repo", seeded.Key+"/"+forkName)
	if err != nil {
		t.Fatalf("repo sync status on the fork failed: %v\noutput: %s", err, syncOutput)
	}
	if !strings.Contains(syncOutput, `"available": true`) {
		t.Fatalf("the new repository does not report as a fork:\n%s", syncOutput)
	}
	if available, _ := decodeJSONMap(t, syncOutput)["available"].(bool); !available {
		t.Fatalf("the new repository does not report as a fork:\n%s", syncOutput)
	}
}

// TestLiveProjectPermissionsGrantAndRevoke covers `project permissions grant`,
// `project permissions revoke`, `project permissions users revoke` and
// `project permissions groups revoke`.
//
// Four commands, two of them aliases of a third with the subject fixed. All
// four had only dry-run coverage, and a dry run of a revoke leaves the grant
// exactly where it was, so nothing was ever shown to be removed.
func TestLiveProjectPermissionsGrantAndRevoke(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	configureLiveCLIEnv(t, harness, seeded.Key, seeded.Repos[0].Slug)

	user, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create user failed: %v", err)
	}

	// The generic revoke, with the subject taken from the argument.
	t.Run("grant then revoke a user", func(t *testing.T) {
		mustLiveCLI(t, "project", "permissions", "grant", seeded.Key, user.Username, "PROJECT_READ")
		assertLiveProjectPermission(t, seeded.Key, false, user.Username, true)
		assertMutatedProjectPermissionLevel(t, seeded.Key, false, user.Username, "PROJECT_READ")

		mustLiveCLI(t, "project", "permissions", "revoke", seeded.Key, user.Username, "--yes")
		assertLiveProjectPermission(t, seeded.Key, false, user.Username, false)
	})

	// The subject-specific spelling has to remove the grant too, not merely
	// report success.
	t.Run("grant then revoke through the users subcommand", func(t *testing.T) {
		mustLiveCLI(t, "project", "permissions", "grant", seeded.Key, user.Username, "PROJECT_WRITE")
		assertLiveProjectPermission(t, seeded.Key, false, user.Username, true)
		assertMutatedProjectPermissionLevel(t, seeded.Key, false, user.Username, "PROJECT_WRITE")

		mustLiveCLI(t, "project", "permissions", "users", "revoke", seeded.Key, user.Username, "--yes")
		assertLiveProjectPermission(t, seeded.Key, false, user.Username, false)
	})

	t.Run("grant then revoke a group", func(t *testing.T) {
		mustLiveCLI(t, "project", "permissions", "grant", "--group", seeded.Key, licensedGroup, "PROJECT_READ")
		assertLiveProjectPermission(t, seeded.Key, true, licensedGroup, true)
		assertMutatedProjectPermissionLevel(t, seeded.Key, true, licensedGroup, "PROJECT_READ")

		mustLiveCLI(t, "project", "permissions", "groups", "revoke", seeded.Key, licensedGroup, "--yes")
		assertLiveProjectPermission(t, seeded.Key, true, licensedGroup, false)
	})
}

// TestLiveRepoPermissionsGrantAndRevoke covers `repo permissions grant` and
// `repo permissions revoke`, and the two `repo settings security permissions`
// revokes that address the same grants.
func TestLiveRepoPermissionsGrantAndRevoke(t *testing.T) {
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

	user, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create user failed: %v", err)
	}

	t.Run("grant then revoke a user", func(t *testing.T) {
		mustLiveCLI(t, "repo", "permissions", "grant", user.Username, "REPO_READ", "--repo", repoRef)
		assertLiveRepoPermission(t, repoRef, false, user.Username, true)
		assertMutatedRepoPermissionLevel(t, repoRef, false, user.Username, "REPO_READ")

		mustLiveCLI(t, "repo", "permissions", "revoke", user.Username, "--repo", repoRef, "--yes")
		assertLiveRepoPermission(t, repoRef, false, user.Username, false)
	})

	t.Run("grant then revoke a group", func(t *testing.T) {
		mustLiveCLI(t, "repo", "permissions", "grant", "--group", licensedGroup, "REPO_READ", "--repo", repoRef)
		assertLiveRepoPermission(t, repoRef, true, licensedGroup, true)
		assertMutatedRepoPermissionLevel(t, repoRef, true, licensedGroup, "REPO_READ")

		mustLiveCLI(t, "repo", "permissions", "revoke", "--group", licensedGroup, "--repo", repoRef, "--yes")
		assertLiveRepoPermission(t, repoRef, true, licensedGroup, false)
	})

	// The same two grants, removed through the settings tree instead.
	t.Run("revoke through repo settings security permissions", func(t *testing.T) {
		mustLiveCLI(t, "repo", "permissions", "grant", user.Username, "REPO_WRITE", "--repo", repoRef)
		assertLiveRepoPermission(t, repoRef, false, user.Username, true)
		assertMutatedRepoPermissionLevel(t, repoRef, false, user.Username, "REPO_WRITE")

		mustLiveCLI(t, "repo", "settings", "security", "permissions", "users", "revoke", user.Username, "--repo", repoRef, "--yes")
		assertLiveRepoPermission(t, repoRef, false, user.Username, false)

		mustLiveCLI(t, "repo", "permissions", "grant", "--group", licensedGroup, "REPO_WRITE", "--repo", repoRef)
		assertLiveRepoPermission(t, repoRef, true, licensedGroup, true)
		assertMutatedRepoPermissionLevel(t, repoRef, true, licensedGroup, "REPO_WRITE")

		mustLiveCLI(t, "repo", "settings", "security", "permissions", "groups", "revoke", licensedGroup, "--repo", repoRef, "--yes")
		assertLiveRepoPermission(t, repoRef, true, licensedGroup, false)
	})
}

// mustLiveCLI runs a command with --json and fails the test if it errors.
func mustLiveCLI(t *testing.T, args ...string) string {
	t.Helper()

	output, err := executeLiveCLI(t, append([]string{"--json"}, args...)...)
	if err != nil {
		t.Fatalf("%s failed: %v\noutput: %s", strings.Join(args, " "), err, output)
	}

	return output
}

func assertLiveProjectPermission(t *testing.T, projectKey string, group bool, name string, want bool) {
	t.Helper()

	args := []string{"project", "permissions", "list", projectKey, "--all"}
	if group {
		args = append(args, "--group")
	}
	assertLivePermissionEntry(t, mustLiveCLI(t, args...), name, want)
}

func assertLiveRepoPermission(t *testing.T, repoRef string, group bool, name string, want bool) {
	t.Helper()

	args := []string{"repo", "permissions", "list", "--repo", repoRef, "--all"}
	if group {
		args = append(args, "--group")
	}
	assertLivePermissionEntry(t, mustLiveCLI(t, args...), name, want)
}

// assertLivePermissionEntry checks whether a listing names a subject, reading
// the entries rather than searching the raw text: a revoked user can still
// appear in the payload as a display name or an author elsewhere.
func assertLivePermissionEntry(t *testing.T, output, name string, want bool) {
	t.Helper()

	entries, _ := decodeJSONMap(t, output)["entries"].([]any)

	found := false
	for _, entry := range entries {
		record, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if subject, _ := record["name"].(string); strings.EqualFold(subject, name) {
			found = true

			break
		}
	}

	if found != want {
		verb := "should hold a permission and does not"
		if !want {
			verb = "still holds a permission after it was revoked"
		}
		t.Fatalf("%s %s\nlisting: %s", name, verb, output)
	}
}

func assertMutatedProjectPermissionLevel(t *testing.T, projectKey string, group bool, name, level string) {
	t.Helper()

	args := []string{"project", "permissions", "list", projectKey, "--all"}
	if group {
		args = append(args, "--group")
	}
	assertMutatedPermissionLevel(t, mustLiveCLI(t, args...), name, level)
}

func assertMutatedRepoPermissionLevel(t *testing.T, repoRef string, group bool, name, level string) {
	t.Helper()

	args := []string{"repo", "permissions", "list", "--repo", repoRef, "--all"}
	if group {
		args = append(args, "--group")
	}
	assertMutatedPermissionLevel(t, mustLiveCLI(t, args...), name, level)
}

// assertMutatedPermissionLevel checks the level a listing reports for one
// subject. assertLivePermissionEntry asks only whether the subject is listed,
// and a grant stored at any other level is listed too.
func assertMutatedPermissionLevel(t *testing.T, output, name, level string) {
	t.Helper()

	entries, _ := decodeJSONMap(t, output)["entries"].([]any)
	for _, entry := range entries {
		record, _ := entry.(map[string]any)
		if subject, _ := record["name"].(string); subject != name {
			continue
		}
		if got, _ := record["permission"].(string); got != level {
			t.Fatalf("%s holds %q, want %q\nlisting: %s", name, got, level, output)
		}

		return
	}

	t.Fatalf("%s holds no permission, want %q\nlisting: %s", name, level, output)
}

// TestLivePRRebase covers `pr rebase`, whose only live coverage was a dry run.
//
// The command is invoked the way a caller does, without --version. That is the
// same shape as #505: the version is an optimistic lock the caller has no
// reason to know, and omitting it there turned out to be rejected outright.
func TestLivePRRebase(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	branch := "feature/needs-rebase"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "rebase-me.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	prID := createLivePRForRegression(t, branch, "Needs a rebase", "--no-default-reviewers", "--no-codeowners")
	assertMutatedPullRequestOpened(t, prID, "Needs a rebase", branch)

	// The target has to move, or there is nothing to rebase onto and the
	// command could report success without doing anything.
	//
	// Bitbucket takes the move in asynchronously: it rescopes the pull request
	// and bumps its version after the push has returned (#598). A rebase sent
	// before that finds the pull request still on its old target and answers
	// that there is nothing to rebase, which is how this failed on a loaded CI
	// runner. So wait for the bump, and fail rather than rebase too early.
	versionBefore := currentLivePRVersion(t, prID)
	if err := harness.pushFileOnBranch(seeded.Key, repo.Slug, "master", "moved-ahead.txt", "the target moved\n"); err != nil {
		t.Fatalf("advancing master failed: %v", err)
	}
	waitForLivePRVersionAbove(t, prID, versionBefore)

	before := currentLivePRSourceCommit(t, prID)
	masterTip := mutatedBranchTip(t, "master")

	output, err := executeLiveCLI(t, "--json", "pr", "rebase", prID)
	if err != nil {
		t.Fatalf("pr rebase without --version failed: %v\noutput: %s", err, output)
	}

	// The ref change the server reports is the evidence, and it is in the same
	// response: a rebase rewrites the source branch, so the hashes must differ.
	//
	// This used to re-read the pull request instead and compare the source
	// commit. That is the same question asked of a second endpoint, and the
	// pull request's own view of its source ref lags the ref itself -- locally
	// by about the length of one round trip, on a loaded CI runner by enough to
	// fail. Reading what the command was told is both stronger and immune to it.
	result := decodeJSONMap(t, output)
	fromHash, _ := result["fromHash"].(string)
	toHash, _ := result["toHash"].(string)

	if fromHash != before {
		t.Errorf("the rebase started from %q, but the pull request was on %q", fromHash, before)
	}
	if toHash == "" || toHash == fromHash {
		t.Fatalf("nothing was rebased: fromHash=%q toHash=%q\noutput: %s", fromHash, toHash, output)
	}

	// The pull request catches up, and how long that takes is Bitbucket's
	// business rather than a reason to fail.
	waitForLivePRSourceCommit(t, prID, toHash)

	// The hashes above are the rebase's own report. What it did is on the
	// branch: the ref points at the new commit, and that commit sits directly on
	// the moved master. A merge would have two parents, and a replay onto the old
	// target would have that commit as its parent instead.
	if tip := mutatedBranchTip(t, branch); tip != toHash {
		t.Errorf("%s points at %s, the rebase reported %s", branch, tip, toHash)
	}
	rebased, _ := decodeJSONMap(t, mustLiveCLI(t, "commit", "get", toHash))["commit"].(map[string]any)
	if parents, _ := rebased["parents"].([]any); len(parents) != 1 || parents[0] != masterTip {
		t.Errorf("the rebased commit's parents are %v, want [%s], the tip of master", rebased["parents"], masterTip)
	}
}

// mutatedBranchTip reads the commit a branch points at from the branch listing:
// the ref itself, which moves before a pull request's view of it does.
func mutatedBranchTip(t *testing.T, branch string) string {
	t.Helper()

	output := mustLiveCLI(t, "branch", "list", "--filter", branch, "--all")
	branches, _ := decodeJSONMap(t, output)["branches"].([]any)
	for _, entry := range branches {
		fields, _ := entry.(map[string]any)
		if fields["displayId"] != branch {
			continue
		}
		tip, _ := fields["latestCommit"].(string)
		if tip == "" {
			t.Fatalf("branch %s has no latestCommit: %s", branch, output)
		}

		return tip
	}

	t.Fatalf("no branch %s in the listing: %s", branch, output)

	return ""
}

// waitForLivePRVersionAbove waits for Bitbucket to bump the pull request's
// version past the one given, which it does once it has rescoped the pull
// request after its target moved.
func waitForLivePRVersionAbove(t *testing.T, prID, version string) {
	t.Helper()

	floor, err := strconv.Atoi(version)
	if err != nil {
		t.Fatalf("pull request version %q is not a number: %v", version, err)
	}

	deadline := time.Now().Add(30 * time.Second)
	for {
		got := currentLivePRVersion(t, prID)
		if current, err := strconv.Atoi(got); err == nil && current > floor {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the pull request is still at version %s; Bitbucket did not rescope it after its target moved (want above %s)", got, version)
		}
		time.Sleep(time.Second)
	}
}

// TestLivePRRebaseWithAnExplicitVersionStillReportsAConflict is the boundary on
// the retry TestLivePRRebase depends on.
//
// bb recovers from a stale version it read itself, because #532 made reading it
// bb's job. A caller who passes --version did the opposite: they asserted a
// specific lock, and a conflict is the answer they asked for. Recovering there
// would rebase against a version they did not name, which is the one thing the
// flag exists to prevent.
func TestLivePRRebaseWithAnExplicitVersionStillReportsAConflict(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	branch := "feature/explicit-stale-version"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "rebase-me.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	prID := createLivePRForRegression(t, branch, "Rebased with a stale version", "--no-default-reviewers", "--no-codeowners")
	assertMutatedPullRequestOpened(t, prID, "Rebased with a stale version", branch)

	// Bump the version before the target moves, not after.
	//
	// Advancing master makes Bitbucket rescope the pull request and bump the
	// version asynchronously -- which is the subject of #598 -- so a read taken
	// after the push can already be stale by the time the next call lands. This
	// test did the push first and failed on CI for exactly that: the update
	// meant to bump the version was itself rejected as out of date.
	//
	// Nothing else is touching the pull request yet, so this update is the only
	// writer, and the number read before it is behind afterwards whatever the
	// rescope below does.
	stale := currentLivePRVersion(t, prID)
	if output, err := executeLiveCLI(t, "--json", "pr", "update", prID, "--title", "Bumped", "--version", stale); err != nil {
		t.Fatalf("bumping the version failed: %v\noutput: %s", err, output)
	}

	// The bump is this test's premise, so it is read back rather than assumed:
	// an update that stored nothing leaves the version where it was, and then
	// only the rescope below could make `stale` stale.
	bumped := mutatedPullRequest(t, prID)
	if bumped["title"] != "Bumped" {
		t.Errorf("the title is %v after the update, want Bumped", bumped["title"])
	}
	staleVersion, err := strconv.Atoi(stale)
	if err != nil {
		t.Fatalf("pull request version %q is not a number: %v", stale, err)
	}
	if version, _ := bumped["version"].(float64); int(version) <= staleVersion {
		t.Fatalf("the update left the pull request at version %v, want above %d", bumped["version"], staleVersion)
	}

	// Now give the rebase something to replay. Whatever this does to the
	// version, the number above is behind it.
	if err := harness.pushFileOnBranch(seeded.Key, repo.Slug, "master", "moved-ahead.txt", "the target moved\n"); err != nil {
		t.Fatalf("advancing master failed: %v", err)
	}

	tipBefore := mutatedBranchTip(t, branch)

	// The caller named a version that is genuinely behind.
	output, err := executeLiveCLI(t, "--json", "pr", "rebase", prID, "--version", stale)
	if err == nil {
		t.Fatalf("expected a conflict for an explicitly stale version, got success:\n%s", output)
	}
	if !strings.Contains(output, "409") && !strings.Contains(err.Error(), "409") {
		t.Fatalf("expected a 409 conflict, got: %v\noutput: %s", err, output)
	}

	// Refused means nothing was rebased: the branch has not moved.
	if tip := mutatedBranchTip(t, branch); tip != tipBefore {
		t.Errorf("%s moved from %s to %s under a refused rebase", branch, tipBefore, tip)
	}
}

// waitForLivePRSourceCommit waits for the pull request to report a source
// commit, because the ref moves before the pull request's view of it does.
func waitForLivePRSourceCommit(t *testing.T, prID, want string) {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)
	for {
		got := currentLivePRSourceCommit(t, prID)
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the pull request still reports source commit %s, want %s", got, want)
		}
		time.Sleep(time.Second)
	}
}

// TestLivePRRebaseWithNothingToDo covers the outcome that is not in the spec.
//
// A branch already on the tip of its target answers 204 with no body, which the
// generated client has nowhere to put, so bb read the absent payload as a
// broken response and reported "internal: unexpected empty rebase response
// body" at exit 1 -- for a pull request already exactly where it was asked to
// be (OPENAPI-028).
func TestLivePRRebaseWithNothingToDo(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	branch := "feature/nothing-to-rebase"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "already-current.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	// The target is left alone, so the branch is already on top of it.
	prID := createLivePRForRegression(t, branch, "Nothing to rebase", "--no-default-reviewers", "--no-codeowners")
	assertMutatedPullRequestOpened(t, prID, "Nothing to rebase", branch)
	before := currentLivePRSourceCommit(t, prID)
	tipBefore := mutatedBranchTip(t, branch)

	output, err := executeLiveCLI(t, "--json", "pr", "rebase", prID)
	if err != nil {
		t.Fatalf("a rebase with nothing to do must succeed: %v\noutput: %s", err, output)
	}

	// Nothing moved, and the payload must not claim otherwise.
	result := decodeJSONMap(t, output)
	if toHash, _ := result["toHash"].(string); toHash != "" {
		t.Errorf("expected no ref change in the payload, got toHash %q", toHash)
	}
	if after := currentLivePRSourceCommit(t, prID); after != before {
		t.Errorf("the source commit moved from %s to %s with nothing to rebase", before, after)
	}

	human, err := executeLiveCLI(t, "pr", "rebase", prID)
	if err != nil {
		t.Fatalf("a rebase with nothing to do must succeed: %v\noutput: %s", err, human)
	}
	if !strings.Contains(human, "nothing to rebase") {
		t.Errorf("expected the human output to say nothing happened, got:\n%s", human)
	}

	// The second rebase was sent as well, and the words above are its own
	// account. The pull request's view of its source lags the ref, so the ref is
	// read too: a rebase that did move the branch shows there first.
	if after := currentLivePRSourceCommit(t, prID); after != before {
		t.Errorf("the source commit moved from %s to %s with nothing to rebase", before, after)
	}
	if tip := mutatedBranchTip(t, branch); tip != tipBefore {
		t.Errorf("%s moved from %s to %s with nothing to rebase", branch, tipBefore, tip)
	}
}

func currentLivePRSourceCommit(t *testing.T, prID string) string {
	t.Helper()

	output, err := executeLiveCLI(t, "--json", "pr", "get", prID)
	if err != nil {
		t.Fatalf("pr get failed: %v\noutput: %s", err, output)
	}

	commit, _ := extractPRData(decodeJSONMap(t, output))["sourceCommit"].(string)
	if commit == "" {
		t.Fatalf("no sourceCommit in: %s", output)
	}

	return commit
}

// TestLiveDefaultTaskUpdateKeepsItsMatchers is the #511 defect in another
// place, found by asking the same question of every update command: does
// changing one field quietly change another.
//
// A default task carries two ref matchers, and both are mandatory on the wire.
// On create an unset flag rightly becomes an any-ref matcher; update reused
// that reasoning, so `--description` on a task scoped to feature/* -> main reset
// both matchers and the checklist started applying to every pull request. The
// output said nothing.
func TestLiveDefaultTaskUpdateKeepsItsMatchers(t *testing.T) {
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

	// Every write below is read back from the listing as well as from its own
	// reply, and each update sends a description the task does not already
	// have, so one that was dropped cannot read back as stored.
	t.Run("repository scope", func(t *testing.T) {
		tasks := fmt.Sprintf("/rest/default-tasks/latest/projects/%s/repos/%s/tasks", seeded.Key, repo.Slug)

		created := mustLiveCLI(t, "repo", "default-task", "add", "Check the changelog",
			"--repo", repoRef, "--source-ref", "feature/*", "--target-ref", "main")
		id := taskIDFrom(t, decodeJSONMap(t, created), "task")
		assertMutatedDefaultTask(t, harness, mustLiveCLI(t, "repo", "default-task", "list", "--repo", repoRef), tasks, id,
			mutatedDefaultTask{"Check the changelog", "feature/*", "PATTERN", "main", "BRANCH"})

		updated := mustLiveCLI(t, "repo", "default-task", "update", id,
			"--repo", repoRef, "--description", "Check the changelog and the ADR")
		assertTaskMatchers(t, decodeJSONMap(t, updated), "task", "feature/*", "main")
		assertMutatedDefaultTask(t, harness, mustLiveCLI(t, "repo", "default-task", "list", "--repo", repoRef), tasks, id,
			mutatedDefaultTask{"Check the changelog and the ADR", "feature/*", "PATTERN", "main", "BRANCH"})

		// The escape hatch still has to exist: an empty ref widens on purpose.
		widened := mustLiveCLI(t, "repo", "default-task", "update", id,
			"--repo", repoRef, "--description", "Check the changelog and the ADR on every branch", "--source-ref", "")
		assertTaskMatchers(t, decodeJSONMap(t, widened), "task", anyRefMatcher, "main")
		assertMutatedDefaultTask(t, harness, mustLiveCLI(t, "repo", "default-task", "list", "--repo", repoRef), tasks, id,
			mutatedDefaultTask{"Check the changelog and the ADR on every branch", anyRefMatcher, "ANY_REF", "main", "BRANCH"})

		// And an explicit ref still changes the one it names, only.
		retargeted := mustLiveCLI(t, "repo", "default-task", "update", id,
			"--repo", repoRef, "--description", "Check the changelog and the ADR before develop", "--target-ref", "develop")
		assertTaskMatchers(t, decodeJSONMap(t, retargeted), "task", anyRefMatcher, "develop")
		assertMutatedDefaultTask(t, harness, mustLiveCLI(t, "repo", "default-task", "list", "--repo", repoRef), tasks, id,
			mutatedDefaultTask{"Check the changelog and the ADR before develop", anyRefMatcher, "ANY_REF", "develop", "BRANCH"})
	})

	t.Run("project scope", func(t *testing.T) {
		tasks := fmt.Sprintf("/rest/default-tasks/latest/projects/%s/tasks", seeded.Key)

		created := mustLiveCLI(t, "project", "default-task", "add", seeded.Key, "Sign the release",
			"--source-ref", "release/*", "--target-ref", "main")
		id := taskIDFrom(t, decodeJSONMap(t, created), "")
		assertMutatedDefaultTask(t, harness, mustLiveCLI(t, "project", "default-task", "list", seeded.Key), tasks, id,
			mutatedDefaultTask{"Sign the release", "release/*", "PATTERN", "main", "BRANCH"})

		updated := mustLiveCLI(t, "project", "default-task", "update", seeded.Key, id,
			"--description", "Sign the release notes")
		assertTaskMatchers(t, decodeJSONMap(t, updated), "", "release/*", "main")
		assertMutatedDefaultTask(t, harness, mustLiveCLI(t, "project", "default-task", "list", seeded.Key), tasks, id,
			mutatedDefaultTask{"Sign the release notes", "release/*", "PATTERN", "main", "BRANCH"})
	})
}

// mutatedDefaultTask is a default task as it should be stored: its description,
// and each matcher's ref with the kind of matcher it is.
type mutatedDefaultTask struct {
	description        string
	source, sourceKind string
	target, targetKind string
}

// assertMutatedDefaultTask reads a default task back twice. bb's listing gives
// the description and the refs. The matcher kinds come from Bitbucket directly,
// because bb's output leaves them out -- and a kind is not a detail: a BRANCH
// matcher on feature/* matches only a branch called exactly that.
func assertMutatedDefaultTask(t *testing.T, harness *liveHarness, listing, restPath, id string, want mutatedDefaultTask) {
	t.Helper()

	var listed any
	if err := decodeJSONEnvelopeData(listing, &listed); err != nil {
		t.Fatalf("default-task list returned invalid JSON: %v\n%s", err, listing)
	}
	task, ok := findByID(listed, id)
	if !ok {
		t.Fatalf("default task %s is not in the listing: %s", id, listing)
	}
	source, _ := task["sourceMatcher"].(map[string]any)
	target, _ := task["targetMatcher"].(map[string]any)
	if task["description"] != want.description || source["displayId"] != want.source || target["displayId"] != want.target {
		t.Errorf("default task %s is %q from %v to %v, want %q from %s to %s",
			id, task["description"], source["displayId"], target["displayId"], want.description, want.source, want.target)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	page, err := harness.liveJSON(ctx, http.MethodGet, restPath, nil)
	if err != nil {
		t.Fatalf("read the default tasks back: %v", err)
	}
	stored, ok := findByID(page["values"], id)
	if !ok {
		t.Fatalf("default task %s is not in %s: %v", id, restPath, page)
	}
	if got := mutatedMatcherKind(stored["sourceMatcher"]); got != want.sourceKind {
		t.Errorf("default task %s has a %s source matcher, want %s", id, got, want.sourceKind)
	}
	if got := mutatedMatcherKind(stored["targetMatcher"]); got != want.targetKind {
		t.Errorf("default task %s has a %s target matcher, want %s", id, got, want.targetKind)
	}
}

// mutatedMatcherKind is the type id of a ref matcher as Bitbucket returns it,
// {"type": {"id": "PATTERN", ...}}.
func mutatedMatcherKind(matcher any) string {
	fields, _ := matcher.(map[string]any)
	kind, _ := fields["type"].(map[string]any)
	id, _ := kind["id"].(string)

	return id
}

// anyRefMatcher is the id Bitbucket echoes for "matches any ref". It is not the
// value that is sent to set one, which is ANY_REF.
const anyRefMatcher = "ANY_REF_MATCHER_ID"

func taskIDFrom(t *testing.T, data map[string]any, wrapper string) string {
	t.Helper()

	task := taskPayload(t, data, wrapper)
	id, ok := task["id"]
	if !ok {
		t.Fatalf("no task id in: %v", data)
	}

	return fmt.Sprintf("%v", id)
}

func assertTaskMatchers(t *testing.T, data map[string]any, wrapper, wantSource, wantTarget string) {
	t.Helper()

	task := taskPayload(t, data, wrapper)
	for _, check := range []struct {
		key  string
		want string
	}{
		{"sourceMatcher", wantSource},
		{"targetMatcher", wantTarget},
	} {
		matcher, ok := task[check.key].(map[string]any)
		if !ok {
			t.Fatalf("no %s in: %v", check.key, task)
		}
		if got, _ := matcher["displayId"].(string); got != check.want {
			t.Errorf("%s = %q, want %q", check.key, got, check.want)
		}
	}
}

// taskPayload unwraps the task from a response, which the repository-scoped
// command nests under "task" and the project-scoped one does not.
func taskPayload(t *testing.T, data map[string]any, wrapper string) map[string]any {
	t.Helper()

	if wrapper == "" {
		return data
	}
	task, ok := data[wrapper].(map[string]any)
	if !ok {
		t.Fatalf("no %q object in: %v", wrapper, data)
	}

	return task
}

// TestLiveDefaultBranchMustExist is what comparing a dry run against reality
// turned up: `branch default set` accepted a branch that is not there.
//
// Bitbucket answers 204 and leaves the repository pointing at nothing, and its
// own UI only ever offers real branches, so a typo was silent and the default
// branch stayed broken until somebody noticed.
func TestLiveDefaultBranchMustExist(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	before := currentLiveDefaultBranch(t)

	// --json sits in each row rather than in an append around the spread, which
	// is the shape tools/command-reach reads: prepended, the command words were
	// out of its sight and these two invocations went uncounted.
	for _, command := range [][]string{
		{"--json", "branch", "default", "set", "does-not-exist"},
		{"--json", "branch", "model", "update", "does-not-exist"},
	} {
		t.Run(strings.Join(command[1:], " "), func(t *testing.T) {
			output, err := executeLiveCLI(t, command...)
			if err == nil {
				t.Fatalf("a branch that does not exist must be refused, got:\n%s", output)
			}
			if after := currentLiveDefaultBranch(t); after != before {
				t.Fatalf("the default branch moved to %q despite the failure", after)
			}
		})
	}

	// A real branch still works, or the guard has simply broken the command.
	t.Run("a real branch is still accepted", func(t *testing.T) {
		const branch = "feature/real-default"
		if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "real.txt"); err != nil {
			t.Fatalf("push commit on branch failed: %v", err)
		}
		mustLiveCLI(t, "branch", "default", "set", branch)
		if after := currentLiveDefaultBranch(t); after != branch {
			t.Fatalf("default branch = %q, want %q", after, branch)
		}
		mustLiveCLI(t, "branch", "default", "set", before)
		if after := currentLiveDefaultBranch(t); after != before {
			t.Fatalf("default branch was not restored: got %q, want %q", after, before)
		}
	})
}

// TestLiveReviewerGroupDeleteAcceptsAName is the other half of the same sweep.
//
// Every other reviewer-group flag takes a name, and delete took one too -- then
// sent it where the endpoint wants a numeric id. Bitbucket answered with a body
// the client could not decode, which surfaced as a transient error and exit 10:
// retry advice for something that could never work, on a group the caller had
// named correctly.
func TestLiveReviewerGroupDeleteAcceptsAName(t *testing.T) {
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
	const groupName = "deletable_by_name"
	if err := harness.createReviewerGroup(ctx, seeded.Key, repo.Slug, groupName, member.Username); err != nil {
		t.Fatalf("create reviewer group failed: %v", err)
	}
	// A second group beside it, so a delete can be seen to take only the group
	// it names: with one group, a name resolved to the wrong id and a delete of
	// the right one leave the same empty listing.
	keptName := testsupport.UniqueName("kept_")
	if err := harness.createReviewerGroup(ctx, seeded.Key, repo.Slug, keptName, member.Username); err != nil {
		t.Fatalf("create the second reviewer group failed: %v", err)
	}

	// Both fixtures as stored: the name, the scope and the member each was sent
	// with. The members come from the listing, not from `reviewer-group users`:
	// that endpoint answers with the members who can review in the repository,
	// and this member was never given access to it, so it reads as empty.
	groups := mutatedReviewerGroups(t, mustLiveCLI(t, "reviewer-group", "list", "--repo", repoRef))
	if len(groups) != 2 {
		t.Fatalf("want exactly the two groups just created, got %v", groups)
	}
	for _, name := range []string{groupName, keptName} {
		if stored := groups[name]; stored.scope != "REPOSITORY" || !slices.Equal(stored.members, []string{member.Username}) {
			t.Errorf("group %s is %+v, want scope REPOSITORY and members [%s]", name, stored, member.Username)
		}
	}
	keptID := groups[keptName].id

	t.Run("a name that exists is deleted", func(t *testing.T) {
		mustLiveCLI(t, "reviewer-group", "delete", groupName, "--repo", repoRef, "--yes")

		listing := mustLiveCLI(t, "reviewer-group", "list", "--repo", repoRef)
		if strings.Contains(listing, groupName) {
			t.Fatalf("the group survived the delete:\n%s", listing)
		}
		if after := mutatedReviewerGroups(t, listing); len(after) != 1 || after[keptName].id != keptID {
			t.Fatalf("want only %s (id %s) left after the delete, got %v", keptName, keptID, after)
		}
	})

	t.Run("a name that does not exist is not found, not transient", func(t *testing.T) {
		output, err := executeLiveCLI(t, "--json", "reviewer-group", "delete", "no_such_group", "--repo", repoRef, "--yes")
		if err == nil {
			t.Fatalf("expected a failure, got:\n%s", output)
		}
		// Kind transient is exit 10: retry advice for something that can never
		// succeed. The kind is on the error, not in the buffer, because the
		// envelope for a failure is written above Execute.
		if apperrors.IsKind(err, apperrors.KindTransient) {
			t.Errorf("a missing group is not a transient failure: %v", err)
		}
		if !apperrors.IsKind(err, apperrors.KindNotFound) {
			t.Errorf("expected kind not_found, got: %v\noutput: %s", err, output)
		}

		// And nothing was deleted in its place.
		if after := mutatedReviewerGroups(t, mustLiveCLI(t, "reviewer-group", "list", "--repo", repoRef)); len(after) != 1 || after[keptName].id != keptID {
			t.Errorf("want %s (id %s) untouched by a refused delete, got %v", keptName, keptID, after)
		}
	})
}

// mutatedReviewerGroup is one reviewer group as a listing reports it.
type mutatedReviewerGroup struct {
	id, scope string
	members   []string
}

// mutatedReviewerGroups reads a reviewer-group listing into its groups, by name.
func mutatedReviewerGroups(t *testing.T, output string) map[string]mutatedReviewerGroup {
	t.Helper()

	entries, _ := decodeJSONMap(t, output)["reviewerGroups"].([]any)
	groups := make(map[string]mutatedReviewerGroup, len(entries))
	for _, entry := range entries {
		fields, _ := entry.(map[string]any)
		name, _ := fields["name"].(string)
		group := mutatedReviewerGroup{}
		group.id, _ = numericOrStringID(fields["id"])
		group.scope, _ = fields["scope"].(string)
		users, _ := fields["users"].([]any)
		for _, user := range users {
			member, _ := user.(map[string]any)
			username, _ := member["name"].(string)
			group.members = append(group.members, username)
		}
		groups[name] = group
	}

	return groups
}

// TestLiveDefaultBranchFoundPastTheFirstPage is the boundary the existence
// check has to survive.
//
// filterText is a substring match on the branch being looked for, so what
// crowds the result is not a shared prefix but other branches that *contain*
// the name. "release" is matched by "a-release-000" too, and those sort before
// it, so the branch actually being checked lands last.
//
// A scan capped at one page would then report a branch that exists as missing
// and refuse the operation, which is the worse of the two failures: it blocks
// work that should succeed, where the typo this guard catches only lets through
// work that should not.
func TestLiveDefaultBranchFoundPastTheFirstPage(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// The branch the test actually sets. Every decoy contains this string, so
	// every decoy comes back from the same filter.
	const target = "release"

	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, target, "release.txt"); err != nil {
		t.Fatalf("push the target branch failed: %v", err)
	}

	commits, err := harness.listCommitIDs(ctx, seeded.Key, repo.Slug, 1)
	if err != nil || len(commits) == 0 {
		t.Fatalf("could not read a commit to branch from: %v", err)
	}

	// Named to sort before the target, so it cannot be on the first page.
	// Refs are made directly: pushing 120 branches through git would dominate
	// the runtime for no extra signal.
	const decoys = 120
	wantDecoys := make(map[string]bool, decoys)
	for index := range decoys {
		name := fmt.Sprintf("a-%s-%03d", target, index)
		if _, err := harness.liveJSON(ctx, http.MethodPost,
			fmt.Sprintf("/rest/branch-utils/latest/projects/%s/repos/%s/branches", seeded.Key, repo.Slug),
			map[string]any{"name": name, "startPoint": commits[0]}); err != nil {
			t.Fatalf("create branch %s: %v", name, err)
		}
		wantDecoys[name] = true
	}

	// The premise, read back rather than assumed. Every decoy is stored, at the
	// commit it was started from, and the target is not on the first page of
	// the filtered listing -- the page a one-page scan would stop at. Were the
	// server to sort the target first, this test would pass without the guard
	// ever reading a second page.
	matchingOutput := mustLiveCLI(t, "branch", "list", "--filter", target, "--all")
	matching, _ := decodeJSONMap(t, matchingOutput)["branches"].([]any)
	if len(matching) != decoys+1 {
		t.Fatalf("want %d branches matching %q, got %d:\n%s", decoys+1, target, len(matching), matchingOutput)
	}
	for _, entry := range matching {
		fields, _ := entry.(map[string]any)
		name, _ := fields["displayId"].(string)
		if name == target {
			continue
		}
		if !wantDecoys[name] {
			t.Fatalf("branch %s matches %q and is neither the target nor a decoy", name, target)
		}
		if fields["latestCommit"] != commits[0] {
			t.Errorf("decoy %s points at %v, want %s", name, fields["latestCommit"], commits[0])
		}
	}
	firstPage, _ := decodeJSONMap(t, mustLiveCLI(t, "branch", "list", "--filter", target, "--limit", "25"))["branches"].([]any)
	for _, entry := range firstPage {
		if fields, _ := entry.(map[string]any); fields["displayId"] == target {
			t.Fatalf("%s is on the first page of its own filter, so this proves nothing past it", target)
		}
	}

	before := currentLiveDefaultBranch(t)
	mustLiveCLI(t, "branch", "default", "set", target)

	if after := currentLiveDefaultBranch(t); after != target {
		t.Fatalf("default branch = %q, want %q", after, target)
	}

	// The guard still has to refuse a branch that really is absent, with this
	// many near-misses in the way.
	if output, err := executeLiveCLI(t, "--json", "branch", "default", "set", target+"-does-not-exist"); err == nil {
		t.Fatalf("expected a branch that does not exist to be refused, got:\n%s", output)
	}
	if after := currentLiveDefaultBranch(t); after != target {
		t.Fatalf("the default branch moved to %q despite the refusal, want %q", after, target)
	}

	mustLiveCLI(t, "branch", "default", "set", before)
	if after := currentLiveDefaultBranch(t); after != before {
		t.Fatalf("default branch was not restored: got %q, want %q", after, before)
	}
}

// TestLiveAdminHealthReportsLimitedAuth covers what `admin health` says when
// the server is reachable but the caller is not authenticated.
//
// The mock it replaces fabricated a health payload with authentication off and
// asserted bb printed "auth=limited". That is two guesses at once: what the
// server answers an unauthenticated health check, and what bb makes of it.
// Wrong credentials against a real server produce the state, so only the second
// is under test -- and the distinction matters, because reporting a reachable
// server as unhealthy would send someone looking at the wrong thing.
func TestLiveAdminHealthReportsLimitedAuth(t *testing.T) {
	harness := newLiveHarness(t)

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", harness.config.BitbucketURL)
	t.Setenv("BITBUCKET_USERNAME", "nobody")
	t.Setenv("BITBUCKET_PASSWORD", "certainly-not-the-password")
	t.Setenv("BITBUCKET_TOKEN", "")

	output, err := executeLiveCLI(t, "admin", "health")
	if err != nil {
		t.Fatalf("a reachable server must still report health: %v\noutput: %s", err, output)
	}

	if !strings.Contains(output, "auth=limited") {
		t.Fatalf("expected the health output to report limited auth, got:\n%s", output)
	}
	// Reachable is the other half: the server answered, so this is not an
	// outage and must not read like one.
	if !strings.Contains(output, "OK") {
		t.Fatalf("expected the server to be reported reachable, got:\n%s", output)
	}

}
