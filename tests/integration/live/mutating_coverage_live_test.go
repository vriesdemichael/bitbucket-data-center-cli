//go:build live

package live_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
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
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
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

	// From here the CLI runs as the reviewer, not the author.
	configureLiveCLIEnvForUser(t, harness, seeded.Key, repo.Slug, reviewer)

	approveOutput, err := executeLiveCLI(t, "--json", "pr", "review", "approve", prID)
	if err != nil {
		t.Fatalf("pr review approve failed: %v\noutput: %s", err, approveOutput)
	}
	assertLiveReviewerApproval(t, prID, reviewer.Username, true)

	unapproveOutput, err := executeLiveCLI(t, "--json", "pr", "review", "unapprove", prID)
	if err != nil {
		t.Fatalf("pr review unapprove failed: %v\noutput: %s", err, unapproveOutput)
	}
	assertLiveReviewerApproval(t, prID, reviewer.Username, false)
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

// TestLivePRReviewerAddAndRemove covers `pr review reviewer add` and
// `pr review reviewer remove`.
func TestLivePRReviewerAddAndRemove(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
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

	branch := "feature/reviewer-add-remove"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "reviewers.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	prID := createLivePRForRegression(t, branch, "Reviewer add and remove", "--no-default-reviewers", "--no-codeowners")

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

	removeOutput, err := executeLiveCLI(t, "--json", "pr", "review", "reviewer", "remove", prID, "--user", reviewer.Username)
	if err != nil {
		t.Fatalf("pr review reviewer remove failed: %v\noutput: %s", err, removeOutput)
	}
	if names := currentLivePRReviewers(t, prID); containsFold(names, reviewer.Username) {
		t.Fatalf("expected %s to have been removed, got %v", reviewer.Username, names)
	}
}

// TestLiveBranchDefaultSet covers `branch default set`.
func TestLiveBranchDefaultSet(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
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
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
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
}

// TestLiveRepoAdminFork covers `repo admin fork`, whose live coverage was a
// dry run that by definition creates no fork.
func TestLiveRepoAdminFork(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
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

	syncOutput, err := executeLiveCLI(t, "--json", "repo", "sync", "status", "--repo", seeded.Key+"/"+forkName)
	if err != nil {
		t.Fatalf("repo sync status on the fork failed: %v\noutput: %s", err, syncOutput)
	}
	if !strings.Contains(syncOutput, `"available": true`) {
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
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
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

		mustLiveCLI(t, "project", "permissions", "revoke", seeded.Key, user.Username)
		assertLiveProjectPermission(t, seeded.Key, false, user.Username, false)
	})

	// The subject-specific spelling has to remove the grant too, not merely
	// report success.
	t.Run("grant then revoke through the users subcommand", func(t *testing.T) {
		mustLiveCLI(t, "project", "permissions", "grant", seeded.Key, user.Username, "PROJECT_WRITE")
		assertLiveProjectPermission(t, seeded.Key, false, user.Username, true)

		mustLiveCLI(t, "project", "permissions", "users", "revoke", seeded.Key, user.Username)
		assertLiveProjectPermission(t, seeded.Key, false, user.Username, false)
	})

	t.Run("grant then revoke a group", func(t *testing.T) {
		mustLiveCLI(t, "project", "permissions", "grant", "--group", seeded.Key, licensedGroup, "PROJECT_READ")
		assertLiveProjectPermission(t, seeded.Key, true, licensedGroup, true)

		mustLiveCLI(t, "project", "permissions", "groups", "revoke", seeded.Key, licensedGroup)
		assertLiveProjectPermission(t, seeded.Key, true, licensedGroup, false)
	})
}

// TestLiveRepoPermissionsGrantAndRevoke covers `repo permissions grant` and
// `repo permissions revoke`, and the two `repo settings security permissions`
// revokes that address the same grants.
func TestLiveRepoPermissionsGrantAndRevoke(t *testing.T) {
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

	user, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create user failed: %v", err)
	}

	t.Run("grant then revoke a user", func(t *testing.T) {
		mustLiveCLI(t, "repo", "permissions", "grant", user.Username, "REPO_READ", "--repo", repoRef)
		assertLiveRepoPermission(t, repoRef, false, user.Username, true)

		mustLiveCLI(t, "repo", "permissions", "revoke", user.Username, "--repo", repoRef)
		assertLiveRepoPermission(t, repoRef, false, user.Username, false)
	})

	t.Run("grant then revoke a group", func(t *testing.T) {
		mustLiveCLI(t, "repo", "permissions", "grant", "--group", licensedGroup, "REPO_READ", "--repo", repoRef)
		assertLiveRepoPermission(t, repoRef, true, licensedGroup, true)

		mustLiveCLI(t, "repo", "permissions", "revoke", "--group", licensedGroup, "--repo", repoRef)
		assertLiveRepoPermission(t, repoRef, true, licensedGroup, false)
	})

	// The same two grants, removed through the settings tree instead.
	t.Run("revoke through repo settings security permissions", func(t *testing.T) {
		mustLiveCLI(t, "repo", "permissions", "grant", user.Username, "REPO_WRITE", "--repo", repoRef)
		assertLiveRepoPermission(t, repoRef, false, user.Username, true)

		mustLiveCLI(t, "repo", "settings", "security", "permissions", "users", "revoke", user.Username, "--repo", repoRef)
		assertLiveRepoPermission(t, repoRef, false, user.Username, false)

		mustLiveCLI(t, "repo", "permissions", "grant", "--group", licensedGroup, "REPO_WRITE", "--repo", repoRef)
		assertLiveRepoPermission(t, repoRef, true, licensedGroup, true)

		mustLiveCLI(t, "repo", "settings", "security", "permissions", "groups", "revoke", licensedGroup, "--repo", repoRef)
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

// TestLivePRRebase covers `pr rebase`, whose only live coverage was a dry run.
//
// The command is invoked the way a caller does, without --version. That is the
// same shape as #505: the version is an optimistic lock the caller has no
// reason to know, and omitting it there turned out to be rejected outright.
func TestLivePRRebase(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
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

	// The target has to move, or there is nothing to rebase onto and the
	// command could report success without doing anything.
	if err := harness.pushFileOnBranch(seeded.Key, repo.Slug, "master", "moved-ahead.txt", "the target moved\n"); err != nil {
		t.Fatalf("advancing master failed: %v", err)
	}

	before := currentLivePRSourceCommit(t, prID)

	output, err := executeLiveCLI(t, "--json", "pr", "rebase", prID)
	if err != nil {
		t.Fatalf("pr rebase without --version failed: %v\noutput: %s", err, output)
	}

	// A rebase rewrites the source branch, so the commit it points at must
	// change. Without this the test would pass on a no-op.
	if after := currentLivePRSourceCommit(t, prID); after == before {
		t.Fatalf("the source commit is unchanged at %s, so nothing was rebased\noutput: %s", before, output)
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

	t.Run("repository scope", func(t *testing.T) {
		created := mustLiveCLI(t, "repo", "default-task", "add", "Check the changelog",
			"--repo", repoRef, "--source-ref", "feature/*", "--target-ref", "main")
		id := taskIDFrom(t, decodeJSONMap(t, created), "task")

		updated := mustLiveCLI(t, "repo", "default-task", "update", id,
			"--repo", repoRef, "--description", "Check the changelog and the ADR")
		assertTaskMatchers(t, decodeJSONMap(t, updated), "task", "feature/*", "main")

		// The escape hatch still has to exist: an empty ref widens on purpose.
		widened := mustLiveCLI(t, "repo", "default-task", "update", id,
			"--repo", repoRef, "--description", "Check the changelog and the ADR", "--source-ref", "")
		assertTaskMatchers(t, decodeJSONMap(t, widened), "task", anyRefMatcher, "main")

		// And an explicit ref still changes the one it names, only.
		retargeted := mustLiveCLI(t, "repo", "default-task", "update", id,
			"--repo", repoRef, "--description", "Check the changelog and the ADR", "--target-ref", "develop")
		assertTaskMatchers(t, decodeJSONMap(t, retargeted), "task", anyRefMatcher, "develop")
	})

	t.Run("project scope", func(t *testing.T) {
		created := mustLiveCLI(t, "project", "default-task", "add", seeded.Key, "Sign the release",
			"--source-ref", "release/*", "--target-ref", "main")
		id := taskIDFrom(t, decodeJSONMap(t, created), "")

		updated := mustLiveCLI(t, "project", "default-task", "update", seeded.Key, id,
			"--description", "Sign the release notes")
		assertTaskMatchers(t, decodeJSONMap(t, updated), "", "release/*", "main")
	})
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
