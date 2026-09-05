//go:build live

package live_test

// Permission boundary tests (GitHub Issues #77, #81).
//
// These tests create a temporarily scoped Bitbucket user using admin credentials,
// grant them a specific (restricted) permission level, then assert that operations
// requiring higher privileges return KindAuthorization errors (exit code 3) from the CLI.
//
// Each test also includes a --dry-run variant to verify that the stateful planning
// engine surfaces the permission failure rather than silently producing a plan.
// The bb repo/project permissions show commands (issue #81) are also tested here.

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// assertAuthorizationError asserts that err is non-nil and has KindAuthorization (exit code 3).
func assertAuthorizationError(t *testing.T, err error, output, context string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected authorization error but command succeeded\noutput: %s", context, output)
	}
	if apperrors.ExitCode(err) != 3 {
		t.Fatalf("%s: expected exit code 3 (KindAuthorization), got %d\nerror: %v\noutput: %s",
			context, apperrors.ExitCode(err), err, output)
	}
}

// assertDryRunAuthorizationError asserts that a --dry-run invocation fails with an
// authorization error rather than producing a plan.  It also makes sure the output
// does NOT contain a successful planningMode entry, because that would mean the
// plan was produced before the permission check fired.
func assertDryRunAuthorizationError(t *testing.T, err error, output, context string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected authorization error from dry-run but command succeeded\noutput: %s", context, output)
	}
	if apperrors.ExitCode(err) != 3 {
		t.Fatalf("%s: expected dry-run exit code 3 (KindAuthorization), got %d\nerror: %v\noutput: %s",
			context, apperrors.ExitCode(err), err, output)
	}
	// The plan must NOT have been committed to output — the permission check must fire first.
	if strings.Contains(output, `"planningMode"`) && !strings.Contains(output, `"error"`) {
		t.Fatalf("%s: dry-run produced a plan despite lacking permission\noutput: %s", context, output)
	}
}

// ---------------------------------------------------------------------------
// Repo-read boundary: a user with no project/repo access should get 403 on
// operations that require at least REPO_READ.
// ---------------------------------------------------------------------------

func TestLivePermissionRepoReadDeniedWithoutAccess(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]

	// Create a user with NO permissions — not even project read.
	user, err := harness.createRestrictedUser(ctx)
	if err != nil {
		t.Fatalf("create restricted user failed: %v", err)
	}

	configureLiveCLIEnvForUser(t, harness, seeded.Key, repo.Slug, user)

	// repo list requires at least REPO_READ on the project.
	output, cliErr := executeLiveCLI(t, "--json", "repo", "list")
	assertAuthorizationError(t, cliErr, output, "repo list without any access")
}

// Dry-run: tag create without any access must surface authorization error.
// tag create --dry-run calls service.List (tag list) during planning, which requires at
// least REPO_READ. A user with no permissions at all gets 403 there.
func TestLivePermissionRepoReadDryRunDeniedWithoutAccess(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]

	// Create a user with NO permissions at all.
	user, err := harness.createRestrictedUser(ctx)
	if err != nil {
		t.Fatalf("create restricted user failed: %v", err)
	}

	commitID := repo.CommitIDs[0]
	configureLiveCLIEnvForUser(t, harness, seeded.Key, repo.Slug, user)

	tagName := "v-perm-dry-noaccess"
	output, cliErr := executeLiveCLI(t, "--json", "--dry-run", "tag", "create", tagName, "--start-point", commitID, "--message", "dry run perm test no access")
	assertDryRunAuthorizationError(t, cliErr, output, "tag create dry-run without any access")
}

// ---------------------------------------------------------------------------
// Repo-write boundary: a user with REPO_READ cannot create tags or branches.
// ---------------------------------------------------------------------------

func TestLivePermissionRepoWriteDeniedWithRepoReadOnly(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]

	user, err := harness.createRestrictedUser(ctx)
	if err != nil {
		t.Fatalf("create restricted user failed: %v", err)
	}

	// Grant only REPO_READ — the lowest privilege that lets the user see the repo.
	if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, user.Username, openapigenerated.SetPermissionForUserParamsPermissionREPOREAD); err != nil {
		t.Fatalf("grant repo read permission failed: %v", err)
	}

	commitID := repo.CommitIDs[0]
	configureLiveCLIEnvForUser(t, harness, seeded.Key, repo.Slug, user)

	// tag create requires REPO_WRITE.
	tagName := "v-perm-test-tag-ro"
	output, cliErr := executeLiveCLI(t, "--json", "tag", "create", tagName, "--start-point", commitID, "--message", "perm test")
	assertAuthorizationError(t, cliErr, output, "tag create with REPO_READ only")
}

// ---------------------------------------------------------------------------
// Dry-run: tag create with REPO_READ must surface authorization error, not plan.
// ---------------------------------------------------------------------------

func TestLivePermissionRepoWriteDryRunDeniedWithRepoReadOnly(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]

	user, err := harness.createRestrictedUser(ctx)
	if err != nil {
		t.Fatalf("create restricted user failed: %v", err)
	}

	if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, user.Username, openapigenerated.SetPermissionForUserParamsPermissionREPOREAD); err != nil {
		t.Fatalf("grant repo read permission failed: %v", err)
	}

	commitID := repo.CommitIDs[0]
	configureLiveCLIEnvForUser(t, harness, seeded.Key, repo.Slug, user)

	tagName := "v-perm-dry-tag-ro"
	output, cliErr := executeLiveCLI(t, "--json", "--dry-run", "tag", "create", tagName, "--start-point", commitID, "--message", "dry run perm test")
	assertDryRunAuthorizationError(t, cliErr, output, "tag create dry-run with REPO_READ only")
}

// ---------------------------------------------------------------------------
// Repo-admin boundary: a user with REPO_WRITE cannot change repo settings or
// manage hooks.
// ---------------------------------------------------------------------------

func TestLivePermissionRepoAdminDeniedWithRepoWriteOnly(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]

	user, err := harness.createRestrictedUser(ctx)
	if err != nil {
		t.Fatalf("create restricted user failed: %v", err)
	}

	if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, user.Username, openapigenerated.SetPermissionForUserParamsPermissionREPOWRITE); err != nil {
		t.Fatalf("grant repo write permission failed: %v", err)
	}

	configureLiveCLIEnvForUser(t, harness, seeded.Key, repo.Slug, user)

	// repo admin update requires REPO_ADMIN.
	output, cliErr := executeLiveCLI(t, "--json", "repo", "admin", "update", "--name", "should-be-denied")
	assertAuthorizationError(t, cliErr, output, "repo admin update with REPO_WRITE only")
}

// Dry-run: repo settings pull-requests update with REPO_WRITE must surface authorization error.
// GetRepositoryPullRequestSettings is called during planning — a REPO_ADMIN API — so the permission
// check fires before any plan is emitted.
func TestLivePermissionPullRequestSettingsDryRunDeniedWithRepoWriteOnly(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]

	user, err := harness.createRestrictedUser(ctx)
	if err != nil {
		t.Fatalf("create restricted user failed: %v", err)
	}

	if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, user.Username, openapigenerated.SetPermissionForUserParamsPermissionREPOWRITE); err != nil {
		t.Fatalf("grant repo write permission failed: %v", err)
	}

	configureLiveCLIEnvForUser(t, harness, seeded.Key, repo.Slug, user)

	output, cliErr := executeLiveCLI(t, "--json", "--dry-run", "repo", "settings", "pull-requests", "update", "--required-all-tasks-complete=false")
	assertDryRunAuthorizationError(t, cliErr, output, "repo settings pull-requests update dry-run with REPO_WRITE only")
}

// ---------------------------------------------------------------------------
// Project-admin boundary: a user with PROJECT_WRITE cannot delete a project or
// manage project-level permissions.
// ---------------------------------------------------------------------------

func TestLivePermissionProjectDeleteDeniedWithProjectWriteOnly(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	user, err := harness.createRestrictedUser(ctx)
	if err != nil {
		t.Fatalf("create restricted user failed: %v", err)
	}

	if err := harness.grantProjectPermission(ctx, seeded.Key, user.Username, "PROJECT_WRITE"); err != nil {
		t.Fatalf("grant project write permission failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnvForUser(t, harness, seeded.Key, repo.Slug, user)

	// project delete requires PROJECT_ADMIN.
	output, cliErr := executeLiveCLI(t, "--json", "project", "delete", seeded.Key)
	assertAuthorizationError(t, cliErr, output, "project delete with PROJECT_WRITE only")
}

// Dry-run: project delete with PROJECT_WRITE must surface authorization error.
func TestLivePermissionProjectDeleteDryRunDeniedWithProjectWriteOnly(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	user, err := harness.createRestrictedUser(ctx)
	if err != nil {
		t.Fatalf("create restricted user failed: %v", err)
	}

	if err := harness.grantProjectPermission(ctx, seeded.Key, user.Username, "PROJECT_WRITE"); err != nil {
		t.Fatalf("grant project write permission failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnvForUser(t, harness, seeded.Key, repo.Slug, user)

	output, cliErr := executeLiveCLI(t, "--json", "--dry-run", "project", "delete", seeded.Key)
	assertDryRunAuthorizationError(t, cliErr, output, "project delete dry-run with PROJECT_WRITE only")
}

// ---------------------------------------------------------------------------
// Project permissions boundary: a user with PROJECT_WRITE cannot manage
// project-level user permissions.
// ---------------------------------------------------------------------------

func TestLivePermissionProjectPermissionGrantDeniedWithProjectWriteOnly(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	user, err := harness.createRestrictedUser(ctx)
	if err != nil {
		t.Fatalf("create restricted user failed: %v", err)
	}

	if err := harness.grantProjectPermission(ctx, seeded.Key, user.Username, "PROJECT_WRITE"); err != nil {
		t.Fatalf("grant project write permission failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnvForUser(t, harness, seeded.Key, repo.Slug, user)

	// Granting project permissions requires PROJECT_ADMIN.
	output, cliErr := executeLiveCLI(t, "--json", "project", "permissions", "users", "grant", seeded.Key, user.Username, "PROJECT_READ")
	assertAuthorizationError(t, cliErr, output, "project permission grant with PROJECT_WRITE only")
}

// Dry-run: project permission grant with PROJECT_WRITE must surface authorization error.
func TestLivePermissionProjectPermissionGrantDryRunDeniedWithProjectWriteOnly(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	user, err := harness.createRestrictedUser(ctx)
	if err != nil {
		t.Fatalf("create restricted user failed: %v", err)
	}

	if err := harness.grantProjectPermission(ctx, seeded.Key, user.Username, "PROJECT_WRITE"); err != nil {
		t.Fatalf("grant project write permission failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnvForUser(t, harness, seeded.Key, repo.Slug, user)

	output, cliErr := executeLiveCLI(t, "--json", "--dry-run", "project", "permissions", "users", "grant", seeded.Key, user.Username, "PROJECT_READ")
	assertDryRunAuthorizationError(t, cliErr, output, "project permission grant dry-run with PROJECT_WRITE only")
}

// Dry-run: project create requires global create-project permission. A user with
// only project-scoped admin on an existing project must be denied before any plan
// is emitted.
func TestLivePermissionProjectCreateDryRunDeniedWithProjectAdminOnly(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	user, err := harness.createRestrictedUser(ctx)
	if err != nil {
		t.Fatalf("create restricted user failed: %v", err)
	}

	if err := harness.grantProjectPermission(ctx, seeded.Key, user.Username, "PROJECT_ADMIN"); err != nil {
		t.Fatalf("grant project admin permission failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnvForUser(t, harness, seeded.Key, repo.Slug, user)

	output, cliErr := executeLiveCLI(t, "--json", "--dry-run", "project", "create", "DRYDENY", "--name", "dry deny")
	assertDryRunAuthorizationError(t, cliErr, output, "project create dry-run with PROJECT_ADMIN only")
}

// Dry-run ownership boundary: approving a pull request should be denied up-front if
// the caller cannot even read the repo. This exercises the conservative ownership-aware
// precheck path for PR review commands.
func TestLivePermissionPRApproveDryRunDeniedWithoutRepoRead(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 2)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	repo := seeded.Repos[0]
	branch := "perm-pr-approve-dry"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "perm-pr-approve.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	prID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
	if err != nil {
		t.Fatalf("create pull request failed: %v", err)
	}

	user, err := harness.createRestrictedUser(ctx)
	if err != nil {
		t.Fatalf("create restricted user failed: %v", err)
	}

	configureLiveCLIEnvForUser(t, harness, seeded.Key, repo.Slug, user)

	output, cliErr := executeLiveCLI(t, "--json", "--dry-run", "pr", "review", "approve", prID)
	assertDryRunAuthorizationError(t, cliErr, output, "pr review approve dry-run without repo access")
}

// Dry-run ownership boundary: updating a comment should be denied up-front if the
// caller cannot read the repo. This exercises the conservative ownership-aware
// precheck path for comment mutation commands.
func TestLivePermissionCommentUpdateDryRunDeniedWithoutRepoRead(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 2)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	prBranch := "perm-comment-update-dry"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, prBranch, "perm-comment-update.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	prID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, prBranch, "master")
	if err != nil {
		t.Fatalf("create pull request failed: %v", err)
	}

	commentOutput, err := executeLiveCLI(t, "--json", "repo", "comment", "create", "--pr", prID, "--text", "ownership precheck fixture")
	if err != nil {
		t.Fatalf("create comment fixture failed: %v\noutput: %s", err, commentOutput)
	}
	commentPayload := decodeJSONMap(t, commentOutput)
	commentObj, ok := commentPayload["comment"].(map[string]any)
	if !ok {
		t.Fatalf("expected comment object in output: %s", commentOutput)
	}
	commentID := asString(commentObj["id"])
	if commentID == "" {
		t.Fatalf("expected comment id in output: %s", commentOutput)
	}

	user, err := harness.createRestrictedUser(ctx)
	if err != nil {
		t.Fatalf("create restricted user failed: %v", err)
	}

	configureLiveCLIEnvForUser(t, harness, seeded.Key, repo.Slug, user)

	output, cliErr := executeLiveCLI(t, "--json", "--dry-run", "repo", "comment", "update", "--pr", prID, "--id", commentID, "--text", "denied update")
	assertDryRunAuthorizationError(t, cliErr, output, "repo comment update dry-run without repo access")
}

// ---------------------------------------------------------------------------
// Repo-admin boundary via pull-request settings: a user with REPO_WRITE cannot
// read pull-request settings (requires REPO_ADMIN).
// ---------------------------------------------------------------------------

func TestLivePermissionPullRequestSettingsDeniedWithRepoWriteOnly(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	// We only need a seeded project so we have something to grant.
	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	user, err := harness.createRestrictedUser(ctx)
	if err != nil {
		t.Fatalf("create restricted user failed: %v", err)
	}

	repo := seeded.Repos[0]

	// Grant only REPO_WRITE — insufficient to read pull-request settings
	// (requires REPO_ADMIN).
	if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, user.Username, openapigenerated.SetPermissionForUserParamsPermissionREPOWRITE); err != nil {
		t.Fatalf("grant repo write permission failed: %v", err)
	}

	configureLiveCLIEnvForUser(t, harness, seeded.Key, repo.Slug, user)

	output, cliErr := executeLiveCLI(t, "--json", "repo", "settings", "pull-requests", "get")
	assertAuthorizationError(t, cliErr, output, "repo settings pull-requests get with REPO_WRITE only")
}

// ---------------------------------------------------------------------------
// Repo admin create boundary: PROJECT_READ cannot create repositories.
// ---------------------------------------------------------------------------

func TestLivePermissionRepoCreateDeniedWithProjectReadOnly(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	user, err := harness.createRestrictedUser(ctx)
	if err != nil {
		t.Fatalf("create restricted user failed: %v", err)
	}

	// Only grant PROJECT_READ — insufficient to create repos (requires PROJECT_WRITE+).
	if err := harness.grantProjectPermission(ctx, seeded.Key, user.Username, "PROJECT_READ"); err != nil {
		t.Fatalf("grant project read permission failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnvForUser(t, harness, seeded.Key, repo.Slug, user)

	output, cliErr := executeLiveCLI(t, "--json", "repo", "admin", "create", "--project", seeded.Key, "--name", "denied-repo")
	assertAuthorizationError(t, cliErr, output, "repo create with PROJECT_READ only")
}

// Dry-run: repo create with PROJECT_READ must surface authorization error.
func TestLivePermissionRepoCreateDryRunDeniedWithProjectReadOnly(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	user, err := harness.createRestrictedUser(ctx)
	if err != nil {
		t.Fatalf("create restricted user failed: %v", err)
	}

	if err := harness.grantProjectPermission(ctx, seeded.Key, user.Username, "PROJECT_READ"); err != nil {
		t.Fatalf("grant project read permission failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnvForUser(t, harness, seeded.Key, repo.Slug, user)

	output, cliErr := executeLiveCLI(t, "--json", "--dry-run", "repo", "admin", "create", "--project", seeded.Key, "--name", "denied-repo-dry")
	assertDryRunAuthorizationError(t, cliErr, output, "repo create dry-run with PROJECT_READ only")
}

// ---------------------------------------------------------------------------
// bb repo permissions show — effective permission inspection for the caller.
//
// Note: the dev Bitbucket license restricts additional user seats, so these
// tests run as the harness admin user (who always has full access) and verify
// that all three levels are reported as true. The unit tests in
// permission_checker_test.go cover the partial-permission (false) paths.
// ---------------------------------------------------------------------------

// TestLiveRepoPermissionsShowAsAdmin verifies that an admin-level caller sees
// REPO_READ=true, REPO_WRITE=true, and REPO_ADMIN=true on their own repository,
// and that both JSON and human output contain the expected fields.
func TestLiveRepoPermissionsShowAsAdmin(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug

	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// JSON output
	output, cliErr := executeLiveCLI(t, "--json", "repo", "permissions", "show", "--repo", repoRef)
	if cliErr != nil {
		t.Fatalf("repo permissions show (json) failed: %v\noutput: %s", cliErr, output)
	}

	result := decodeJSONMap(t, output)
	repository, ok := result["repository"].(map[string]any)
	if !ok || asString(repository["projectKey"]) != seeded.Key || asString(repository["slug"]) != repo.Slug {
		t.Errorf("expected the repository named as an object, got %v", result["repository"])
	}
	if !grantedPermissions(t, result, output)["REPO_READ"] ||
		!grantedPermissions(t, result, output)["REPO_WRITE"] ||
		!grantedPermissions(t, result, output)["REPO_ADMIN"] {
		t.Errorf("expected every level granted for an admin user, got: %s", output)
	}

	// Human output
	humanOutput, cliErr := executeLiveCLI(t, "repo", "permissions", "show", "--repo", repoRef)
	if cliErr != nil {
		t.Fatalf("repo permissions show (human) failed: %v\noutput: %s", cliErr, humanOutput)
	}
	for _, level := range []string{"REPO_READ", "REPO_WRITE", "REPO_ADMIN"} {
		if !strings.Contains(humanOutput, level) {
			t.Errorf("expected human output to contain %s, got: %s", level, humanOutput)
		}
	}
}

// ---------------------------------------------------------------------------
// bb project permissions show <key> — effective permission inspection for the caller.
//
// Same note as above: restricted users cannot authenticate on the dev license,
// so tests run as the admin user and assert full access.
// ---------------------------------------------------------------------------

// TestLiveProjectPermissionsShowAsAdmin verifies that an admin-level caller sees
// all three project permission levels as true, and that both JSON and human output
// contain the expected fields.
func TestLiveProjectPermissionsShowAsAdmin(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	configureLiveCLIEnv(t, harness, seeded.Key, seeded.Repos[0].Slug)

	// JSON output
	output, cliErr := executeLiveCLI(t, "--json", "project", "permissions", "show", seeded.Key)
	if cliErr != nil {
		t.Fatalf("project permissions show (json) failed: %v\noutput: %s", cliErr, output)
	}

	result := decodeJSONMap(t, output)
	if asString(result["project"]) != seeded.Key {
		t.Errorf("expected project=%q, got %q", seeded.Key, asString(result["project"]))
	}
	granted := grantedPermissions(t, result, output)
	for _, level := range []string{"PROJECT_READ", "PROJECT_WRITE", "PROJECT_ADMIN"} {
		if !granted[level] {
			t.Errorf("expected %s granted for an admin user, got: %s", level, output)
		}
	}

	// Human output
	humanOutput, cliErr := executeLiveCLI(t, "project", "permissions", "show", seeded.Key)
	if cliErr != nil {
		t.Fatalf("project permissions show (human) failed: %v\noutput: %s", cliErr, humanOutput)
	}
	for _, level := range []string{"PROJECT_READ", "PROJECT_WRITE", "PROJECT_ADMIN"} {
		if !strings.Contains(humanOutput, level) {
			t.Errorf("expected human output to contain %s, got: %s", level, humanOutput)
		}
	}
}

// grantedPermissions reads the permission list `bb * permissions show` returns.
//
// A list rather than a map keyed by permission name: the keys would be
// Bitbucket's SCREAMING_SNAKE constants in Go's randomised map order, and a
// fixed list is what --describe can state.
func grantedPermissions(t *testing.T, payload map[string]any, output string) map[string]bool {
	t.Helper()

	entries, ok := payload["permissions"].([]any)
	if !ok {
		t.Fatalf("expected a permissions list in output: %s", output)
	}

	granted := map[string]bool{}
	for _, entry := range entries {
		permission, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("expected each permission to be an object: %s", output)
		}
		granted[asString(permission["permission"])] = permission["granted"] == true
	}

	return granted
}

// TestLivePermissionAliasSubjects is the `--group` flag, asked of a real
// Bitbucket.
//
// A pile of unit tests drove these commands against a handwritten permissions
// endpoint: which route a --group grant reached, which subject the JSON named,
// which action the dry run predicted. Every one of those answers came out of a
// fixture that already held "alice with REPO_READ" and "admins with
// REPO_ADMIN", so the prediction it checked was a lookup in the same file.
//
// Here the entries are ones the command itself created a moment earlier, and
// the no-op, update, create and delete predictions are read against them.
func TestLivePermissionAliasSubjects(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	holder, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	// stash-users is the group every licensed account is in, so it is a group
	// Bitbucket will accept without one being created for the test.
	const group = "stash-users"

	t.Run("the subject reaches the right route and is named in the payload", func(t *testing.T) {
		userGrant := mustLiveCLI(t, "repo", "permissions", "grant", holder.Username, "repo_write")
		if !strings.Contains(userGrant, `"subject": "user"`) || !strings.Contains(userGrant, `"name": "`+holder.Username+`"`) {
			t.Fatalf("a user grant did not name its subject:\n%s", userGrant)
		}

		groupGrant := mustLiveCLI(t, "repo", "permissions", "grant", "--group", group, "repo_read")
		if !strings.Contains(groupGrant, `"subject": "group"`) || !strings.Contains(groupGrant, `"name": "`+group+`"`) {
			t.Fatalf("a group grant did not name its subject:\n%s", groupGrant)
		}

		// The route, read back rather than recorded: a --group grant that went
		// to the users endpoint would have created a user by that name or
		// failed, and either way the group listing would not hold it.
		users := mustLiveCLI(t, "repo", "permissions", "list")
		if !strings.Contains(users, holder.Username) {
			t.Errorf("the user grant is not in the user listing:\n%s", users)
		}
		if strings.Contains(users, `"name": "`+group+`"`) {
			t.Errorf("the group grant landed in the user listing:\n%s", users)
		}

		groups := mustLiveCLI(t, "repo", "permissions", "list", "--group")
		if !strings.Contains(groups, group) {
			t.Errorf("the group grant is not in the group listing:\n%s", groups)
		}
	})

	t.Run("human output names a group as one and a user plainly", func(t *testing.T) {
		userOutput := mustLiveHumanCLI(t, "repo", "permissions", "grant", holder.Username, "repo_write")
		if !strings.Contains(userOutput, "to "+holder.Username) || strings.Contains(userOutput, "to group") {
			t.Errorf("expected a bare user name, got: %s", userOutput)
		}

		groupOutput := mustLiveHumanCLI(t, "repo", "permissions", "revoke", "--group", group)
		if !strings.Contains(groupOutput, "for group "+group) {
			t.Errorf("expected the group to be named as one, got: %s", groupOutput)
		}
	})

	t.Run("the dry-run intent follows the subject", func(t *testing.T) {
		userPreview := mustLiveCLI(t, "--dry-run", "repo", "permissions", "grant", holder.Username, "repo_write")
		if !strings.Contains(userPreview, `"repo.permission.user.grant"`) {
			t.Errorf("expected a user grant intent, got: %s", userPreview)
		}

		groupPreview := mustLiveCLI(t, "--dry-run", "repo", "permissions", "grant", "--group", group, "repo_admin")
		if !strings.Contains(groupPreview, `"repo.permission.group.grant"`) {
			t.Errorf("expected a group grant intent, got: %s", groupPreview)
		}
		if !strings.Contains(groupPreview, `"subject": "group"`) || !strings.Contains(groupPreview, `"name": "`+group+`"`) {
			t.Errorf("expected the group named in the dry-run target, got: %s", groupPreview)
		}
	})

	// The four predictions, against entries the commands above left behind:
	// holder holds REPO_WRITE and nobody else holds anything.
	t.Run("the prediction reads the permissions that are there", func(t *testing.T) {
		stranger, err := harness.createLicensedUser(ctx)
		if err != nil {
			t.Fatalf("create user failed: %v", err)
		}

		for _, testCase := range []struct {
			name string
			want string
			args []string
		}{
			{name: "granting what is already held", want: "no-op", args: []string{"grant", holder.Username, "repo_write"}},
			{name: "granting a different level", want: "update", args: []string{"grant", holder.Username, "repo_admin"}},
			{name: "granting someone with nothing", want: "create", args: []string{"grant", stranger.Username, "repo_read"}},
			{name: "revoking what is held", want: "delete", args: []string{"revoke", holder.Username}},
			{name: "revoking what is not", want: "no-op", args: []string{"revoke", stranger.Username}},
		} {
			t.Run(testCase.name, func(t *testing.T) {
				output := mustLiveCLI(t, append([]string{"--dry-run", "repo", "permissions"}, testCase.args...)...)
				if !strings.Contains(output, `"predictedAction": "`+testCase.want+`"`) {
					t.Fatalf("expected %q, got:\n%s", testCase.want, output)
				}
			})
		}

		// A dry run that predicted all that must not have changed any of it.
		after := mustLiveCLI(t, "repo", "permissions", "list")
		if !strings.Contains(after, holder.Username) {
			t.Errorf("a dry run revoked a permission it only predicted revoking:\n%s", after)
		}
		if strings.Contains(after, stranger.Username) {
			t.Errorf("a dry run granted a permission it only predicted granting:\n%s", after)
		}
	})
}

// TestLivePermissionAliasSubjectsForProjects is the project twin.
//
// It carries one assertion the repository version does not: a user grant and a
// group grant have to publish the same field names. Naming the subject in a
// field rather than in the key is what lets `--describe` state one shape for
// one command, and a consumer needs two code paths the moment they diverge.
func TestLivePermissionAliasSubjectsForProjects(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	configureLiveCLIEnv(t, harness, seeded.Key, seeded.Repos[0].Slug)

	holder, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	const group = "stash-users"

	userGrant := mustLiveCLI(t, "project", "permissions", "grant", seeded.Key, holder.Username, "project_write")
	if !strings.Contains(userGrant, `"subject": "user"`) ||
		!strings.Contains(userGrant, `"name": "`+holder.Username+`"`) ||
		!strings.Contains(userGrant, `"project": "`+seeded.Key+`"`) {
		t.Fatalf("a user grant did not name its subject, name and project:\n%s", userGrant)
	}

	groupGrant := mustLiveCLI(t, "project", "permissions", "grant", "--group", seeded.Key, group, "project_read")
	if !strings.Contains(groupGrant, `"subject": "group"`) || !strings.Contains(groupGrant, `"name": "`+group+`"`) {
		t.Fatalf("a group grant did not name its subject:\n%s", groupGrant)
	}

	if userKeys, groupKeys := jsonFieldNames(t, userGrant), jsonFieldNames(t, groupGrant); userKeys != groupKeys {
		t.Fatalf("user and group grants published different shapes\nuser:  %s\ngroup: %s", userKeys, groupKeys)
	}

	users := mustLiveCLI(t, "project", "permissions", "list", seeded.Key)
	if !strings.Contains(users, holder.Username) {
		t.Errorf("the user grant is not in the user listing:\n%s", users)
	}
	groups := mustLiveCLI(t, "project", "permissions", "list", "--group", seeded.Key)
	if !strings.Contains(groups, group) {
		t.Errorf("the group grant is not in the group listing:\n%s", groups)
	}
	if listKeys, groupListKeys := jsonFieldNames(t, users), jsonFieldNames(t, groups); listKeys != groupListKeys {
		t.Errorf("user and group listings published different shapes\nusers:  %s\ngroups: %s", listKeys, groupListKeys)
	}

	humanGrant := mustLiveHumanCLI(t, "project", "permissions", "grant", seeded.Key, holder.Username, "project_write")
	if !strings.Contains(humanGrant, "Granted PROJECT_WRITE") ||
		!strings.Contains(humanGrant, "to "+holder.Username+" for project "+seeded.Key) {
		t.Errorf("expected a bare user name in the human grant, got: %s", humanGrant)
	}

	humanRevoke := mustLiveHumanCLI(t, "project", "permissions", "revoke", "--group", seeded.Key, group)
	if !strings.Contains(humanRevoke, "Revoked permission") ||
		!strings.Contains(humanRevoke, "for group "+group+" on project "+seeded.Key) {
		t.Errorf("expected the group to be named as one, got: %s", humanRevoke)
	}

	userPreview := mustLiveCLI(t, "--dry-run", "project", "permissions", "grant", seeded.Key, holder.Username, "project_write")
	if !strings.Contains(userPreview, `"project.permission.user.grant"`) {
		t.Errorf("expected a user grant intent, got: %s", userPreview)
	}
	groupPreview := mustLiveCLI(t, "--dry-run", "project", "permissions", "grant", "--group", seeded.Key, group, "project_admin")
	if !strings.Contains(groupPreview, `"project.permission.group.grant"`) {
		t.Errorf("expected a group grant intent, got: %s", groupPreview)
	}
}

// jsonFieldNames returns every field name in a document, sorted, so two
// payloads can be compared on shape rather than on values.
func jsonFieldNames(t *testing.T, document string) string {
	t.Helper()

	var decoded any
	if err := json.Unmarshal([]byte(document), &decoded); err != nil {
		t.Fatalf("payload is not JSON: %v\n%s", err, document)
	}

	names := map[string]struct{}{}
	var walk func(node any, prefix string)
	walk = func(node any, prefix string) {
		switch typed := node.(type) {
		case map[string]any:
			for key, value := range typed {
				names[prefix+key] = struct{}{}
				walk(value, prefix+key+".")
			}
		case []any:
			for _, item := range typed {
				walk(item, prefix)
			}
		}
	}
	walk(decoded, "")

	collected := make([]string, 0, len(names))
	for name := range names {
		collected = append(collected, name)
	}
	sort.Strings(collected)

	return strings.Join(collected, ",")
}
