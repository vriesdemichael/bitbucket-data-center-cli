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
//
// The user holds a licence. These tests used an account without one, which
// Bitbucket refuses everything with NoAccessAuthenticationException before it
// reads a permission, so each passed whatever level had been granted, or none.
// Now the grant is read back as stored, the level as the account holds it, the
// refusal is traced to whoever made it, and what a refused command named is
// read back afterwards as the administrator.

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
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
// authorization error rather than producing a preview. It also makes sure the
// output holds no preview document, because that would mean the preview was
// written before the permission check fired.
//
// A refusal found during the check comes back from the command as its error,
// with nothing written: it is cmd/bb that turns it into the preview's error, and
// these tests run the command tree without it.
func assertDryRunAuthorizationError(t *testing.T, err error, output, context string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected authorization error from dry-run but command succeeded\noutput: %s", context, output)
	}
	if apperrors.ExitCode(err) != 3 {
		t.Fatalf("%s: expected dry-run exit code 3 (KindAuthorization), got %d\nerror: %v\noutput: %s",
			context, apperrors.ExitCode(err), err, output)
	}
	// The preview must NOT have been committed to output — the permission check must fire first.
	if _, written := parseLivePreview(output); written {
		t.Fatalf("%s: dry-run wrote a preview despite lacking permission\noutput: %s", context, output)
	}
}

// Who refused, as bb reports it. Bitbucket names an exception; bb's own
// pre-flight, finding the resource missing from a listing filtered by the
// permission, names none.
const (
	boundaryRefusedByBitbucket = "com.atlassian.bitbucket.AuthorisationException"
	boundaryRefusedForLicence  = "com.atlassian.bitbucket.auth.NoAccessAuthenticationException"
	boundaryRefusedByPreflight = ""
)

// assertBoundaryRefusal checks that a refusal was for want of permission, and
// made by whoever the test expects.
//
// The exit code cannot say: an account without a licence exits 3 as well,
// refused before Bitbucket reads any permission.
func assertBoundaryRefusal(t *testing.T, err error, output, upstream string) {
	t.Helper()

	if !apperrors.IsKind(err, apperrors.KindAuthorization) {
		t.Errorf("refused as %s, want authorization: %v\noutput: %s", apperrors.KindOf(err), err, output)
	}
	if got := apperrors.DetailsOf(err)["upstreamException"]; got != upstream {
		t.Errorf("refused with upstream exception %q, want %q: %v\noutput: %s", got, upstream, err, output)
	}
}

// asBoundaryAccount runs steps in a subtest whose CLI calls authenticate as the
// account. What the test reads before and after it is read as the
// administrator, who can see whatever a refused command would have changed.
func asBoundaryAccount(t *testing.T, harness *liveHarness, projectKey, repositorySlug string, account restrictedUser, steps func(t *testing.T)) {
	t.Helper()

	t.Run("as the account", func(t *testing.T) {
		configureLiveCLIEnvForUser(t, harness, projectKey, repositorySlug, account)
		steps(t)
	})
}

// assertBoundaryStoredLevel reads what a permission listing holds for one
// subject: exactly the level given, or no entry at all when want is empty.
//
// A grant answers 204 with no body, so the listing is the only evidence of
// which level Bitbucket kept. A listing without its entries, or cut at its
// page size, fails rather than reads as nobody holding anything.
func assertBoundaryStoredLevel(t *testing.T, listing, name, want string) {
	t.Helper()

	entries, ok := decodeJSONMap(t, listing)["entries"].([]any)
	if !ok {
		t.Fatalf("the listing carries no entries to find %s in: %s", name, listing)
	}
	var page struct {
		Meta struct {
			LimitReached bool `json:"limitReached"`
		} `json:"meta"`
	}
	if err := json.Unmarshal([]byte(listing), &page); err != nil {
		t.Fatalf("the listing's meta does not decode: %v\n%s", err, listing)
	}
	if page.Meta.LimitReached {
		t.Fatalf("the listing stopped at its page size, so %s may be past it: %s", name, listing)
	}

	var held []string
	for _, entry := range entries {
		record, _ := entry.(map[string]any)
		if subject, _ := record["name"].(string); strings.EqualFold(subject, name) {
			level, _ := record["permission"].(string)
			held = append(held, level)
		}
	}

	if want == "" && len(held) > 0 {
		t.Fatalf("%s holds %v, want no permission\nlisting: %s", name, held, listing)
	}
	if want != "" && (len(held) != 1 || held[0] != want) {
		t.Fatalf("%s holds %v, want exactly %s\nlisting: %s", name, held, want, listing)
	}
}

// assertBoundaryHeldLevels checks every level `bb * permissions show` reports
// the caller holds.
//
// The listing says which level was stored; this is what the account can do
// with it, and a call that succeeds as the account proves its password too.
func assertBoundaryHeldLevels(t *testing.T, output string, want map[string]bool) {
	t.Helper()

	held := grantedPermissions(t, decodeJSONMap(t, output), output)
	if len(held) != len(want) {
		t.Fatalf("reported levels %v, want %v\noutput: %s", held, want, output)
	}
	for level, granted := range want {
		if got, ok := held[level]; !ok || got != granted {
			t.Errorf("%s granted = %v, want %v\noutput: %s", level, got, granted, output)
		}
	}
}

// assertBoundaryHumanHeldLevels is the same for the output a person reads: a
// level name, with a colon after it for a project, then true or false. Every
// name is printed whether it is granted or not, so the value is the part that
// says anything.
func assertBoundaryHumanHeldLevels(t *testing.T, output string, want map[string]bool) {
	t.Helper()

	held := map[string]bool{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && (fields[1] == "true" || fields[1] == "false") {
			held[strings.TrimSuffix(fields[0], ":")] = fields[1] == "true"
		}
	}

	if len(held) != len(want) {
		t.Fatalf("human output reported levels %v, want %v:\n%s", held, want, output)
	}
	for level, granted := range want {
		if got, ok := held[level]; !ok || got != granted {
			t.Errorf("human output says %s is %v, want %v:\n%s", level, got, granted, output)
		}
	}
}

// boundaryEmptyProject creates a project holding nothing, its name starting
// with the prefix given, removed when the test ends. It returns the key and
// the name.
//
// A refused delete is read back by the project still standing, and a seeded
// project would stand either way: Bitbucket refuses to delete a project that
// holds a repository, even to an administrator.
func boundaryEmptyProject(ctx context.Context, t *testing.T, harness *liveHarness, namePrefix string) (string, string) {
	t.Helper()

	key, name, err := harness.createProject(ctx, "LT", namePrefix)
	if err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		harness.deleteProjectAndContents(cleanupCtx, key)
	})

	return key, name
}

// assertBoundaryTagAbsent reads back the tag a refused create named.
func assertBoundaryTagAbsent(t *testing.T, tag string) {
	t.Helper()

	output, err := executeLiveCLI(t, "--json", "tag", "view", tag)
	if err == nil {
		t.Fatalf("tag %s exists after its create was refused:\n%s", tag, output)
	}
	if !apperrors.IsKind(err, apperrors.KindNotFound) {
		t.Fatalf("reading tag %s back failed as %s, want not found: %v\noutput: %s", tag, apperrors.KindOf(err), err, output)
	}
}

// assertBoundaryRepositoryAbsent reads back the repository a refused create
// named.
func assertBoundaryRepositoryAbsent(t *testing.T, repoRef string) {
	t.Helper()

	output, err := executeLiveCLI(t, "--json", "repo", "get", "--repo", repoRef)
	if err == nil {
		t.Fatalf("repository %s exists after its create was refused:\n%s", repoRef, output)
	}
	if !apperrors.IsKind(err, apperrors.KindNotFound) {
		t.Fatalf("reading repository %s back failed as %s, want not found: %v\noutput: %s", repoRef, apperrors.KindOf(err), err, output)
	}
}

// assertBoundaryProjectAbsent reads back the project a refused create named.
func assertBoundaryProjectAbsent(t *testing.T, key string) {
	t.Helper()

	output, err := executeLiveCLI(t, "--json", "project", "get", key)
	if err == nil {
		t.Fatalf("project %s exists after its create was refused:\n%s", key, output)
	}
	if !apperrors.IsKind(err, apperrors.KindNotFound) {
		t.Fatalf("reading project %s back failed as %s, want not found: %v\noutput: %s", key, apperrors.KindOf(err), err, output)
	}
}

// assertBoundaryProjectStands reads back the project a refused delete named.
func assertBoundaryProjectStands(t *testing.T, key string) {
	t.Helper()

	if project := nestedJSONMap(t, mustLiveCLI(t, "project", "get", key), "project"); project["key"] != key {
		t.Fatalf("project get %s answered for project %v", key, project["key"])
	}
}

// boundaryRequiredAllTasksComplete reads the pull request setting a refused
// update named, as whoever the test is at the time.
func boundaryRequiredAllTasksComplete(t *testing.T) bool {
	t.Helper()

	settings := decodeJSONMap(t, mustLiveCLI(t, "repo", "settings", "pull-requests", "get"))
	value, ok := settings["requiredAllTasksComplete"].(bool)
	if !ok {
		t.Fatalf("the pull request settings do not report requiredAllTasksComplete: %v", settings)
	}

	return value
}

// ---------------------------------------------------------------------------
// Repo-read boundary: a user with no project/repo access is refused operations
// that require at least REPO_READ. Bitbucket refuses with 401 and an
// AuthorisationException, not 403.
// ---------------------------------------------------------------------------

func TestLivePermissionRepoReadDeniedWithoutAccess(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]

	// Create a user with NO permissions — not even project read.
	user, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create licensed user failed: %v", err)
	}

	asBoundaryAccount(t, harness, seeded.Key, repo.Slug, user, func(t *testing.T) {
		assertBoundaryHeldLevels(t, mustLiveCLI(t, "repo", "permissions", "show"),
			map[string]bool{"REPO_READ": false, "REPO_WRITE": false, "REPO_ADMIN": false})

		// Listing a project's repositories requires at least REPO_READ on the
		// project. Without --project the listing is the instance's, which
		// Bitbucket filters rather than refuses.
		output, cliErr := executeLiveCLI(t, "--json", "repo", "list", "--project", seeded.Key)
		assertAuthorizationError(t, cliErr, output, "repo list without any access")
		assertBoundaryRefusal(t, cliErr, output, boundaryRefusedByBitbucket)

		// The filter, shown by the repository it has to leave out.
		var listed []map[string]any
		decodeJSONData(t, mustLiveCLI(t, "repo", "list", "--all"), &listed)
		for _, entry := range listed {
			if entry["projectKey"] == seeded.Key && entry["slug"] == repo.Slug {
				t.Errorf("repo list shows %s/%s to an account with no access to it", seeded.Key, repo.Slug)
			}
		}
	})

	// The refusal these tests used to rest on, asserted as what it is: an
	// account without a licence is refused before any permission is read, and
	// bb reports that as authorization too.
	t.Run("an account without a licence is refused before any permission is read", func(t *testing.T) {
		unlicensed, err := harness.createRestrictedUser(ctx)
		if err != nil {
			t.Fatalf("create restricted user failed: %v", err)
		}
		configureLiveCLIEnvForUser(t, harness, seeded.Key, repo.Slug, unlicensed)

		output, cliErr := executeLiveCLI(t, "--json", "repo", "list")
		assertAuthorizationError(t, cliErr, output, "repo list without a licence")
		assertBoundaryRefusal(t, cliErr, output, boundaryRefusedForLicence)
	})
}

// Dry-run: tag create without any access must surface authorization error.
// tag create --dry-run asks for the project's repositories filtered by
// REPO_WRITE during planning, and Bitbucket refuses that listing to a user who
// cannot see the project at all.
func TestLivePermissionRepoReadDryRunDeniedWithoutAccess(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]

	// Create a user with NO permissions at all.
	user, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create licensed user failed: %v", err)
	}

	commitID := repo.CommitIDs[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	tagName := "v-perm-dry-noaccess"
	asBoundaryAccount(t, harness, seeded.Key, repo.Slug, user, func(t *testing.T) {
		assertBoundaryHeldLevels(t, mustLiveCLI(t, "repo", "permissions", "show"),
			map[string]bool{"REPO_READ": false, "REPO_WRITE": false, "REPO_ADMIN": false})

		output, cliErr := executeLiveCLI(t, "--json", "--dry-run", "tag", "create", tagName, "--start-point", commitID, "--message", "dry run perm test no access")
		assertDryRunAuthorizationError(t, cliErr, output, "tag create dry-run without any access")
		assertBoundaryRefusal(t, cliErr, output, boundaryRefusedByBitbucket)
	})

	assertBoundaryTagAbsent(t, tagName)
}

// ---------------------------------------------------------------------------
// Repo-write boundary: a user with REPO_READ cannot create tags or branches.
// ---------------------------------------------------------------------------

func TestLivePermissionRepoWriteDeniedWithRepoReadOnly(t *testing.T) {
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

	user, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create licensed user failed: %v", err)
	}

	// Grant only REPO_READ — the lowest privilege that lets the user see the repo.
	if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, user.Username, openapigenerated.SetPermissionForUserParamsPermissionREPOREAD); err != nil {
		t.Fatalf("grant repo read permission failed: %v", err)
	}

	commitID := repo.CommitIDs[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)
	assertBoundaryStoredLevel(t, mustLiveCLI(t, "repo", "permissions", "list", "--repo", repoRef, "--all"), user.Username, "REPO_READ")

	// tag create requires REPO_WRITE.
	tagName := "v-perm-test-tag-ro"
	asBoundaryAccount(t, harness, seeded.Key, repo.Slug, user, func(t *testing.T) {
		assertBoundaryHeldLevels(t, mustLiveCLI(t, "repo", "permissions", "show"),
			map[string]bool{"REPO_READ": true, "REPO_WRITE": false, "REPO_ADMIN": false})

		output, cliErr := executeLiveCLI(t, "--json", "tag", "create", tagName, "--start-point", commitID, "--message", "perm test")
		assertAuthorizationError(t, cliErr, output, "tag create with REPO_READ only")
		assertBoundaryRefusal(t, cliErr, output, boundaryRefusedByBitbucket)
	})

	assertBoundaryTagAbsent(t, tagName)
}

// ---------------------------------------------------------------------------
// Dry-run: tag create with REPO_READ must surface authorization error, not plan.
// ---------------------------------------------------------------------------

func TestLivePermissionRepoWriteDryRunDeniedWithRepoReadOnly(t *testing.T) {
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

	user, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create licensed user failed: %v", err)
	}

	if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, user.Username, openapigenerated.SetPermissionForUserParamsPermissionREPOREAD); err != nil {
		t.Fatalf("grant repo read permission failed: %v", err)
	}

	commitID := repo.CommitIDs[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)
	assertBoundaryStoredLevel(t, mustLiveCLI(t, "repo", "permissions", "list", "--repo", repoRef, "--all"), user.Username, "REPO_READ")

	tagName := "v-perm-dry-tag-ro"
	asBoundaryAccount(t, harness, seeded.Key, repo.Slug, user, func(t *testing.T) {
		assertBoundaryHeldLevels(t, mustLiveCLI(t, "repo", "permissions", "show"),
			map[string]bool{"REPO_READ": true, "REPO_WRITE": false, "REPO_ADMIN": false})

		output, cliErr := executeLiveCLI(t, "--json", "--dry-run", "tag", "create", tagName, "--start-point", commitID, "--message", "dry run perm test")
		assertDryRunAuthorizationError(t, cliErr, output, "tag create dry-run with REPO_READ only")
		// A reader may list the repository, just not filtered by REPO_WRITE,
		// so the pre-flight is what refuses.
		assertBoundaryRefusal(t, cliErr, output, boundaryRefusedByPreflight)
	})

	assertBoundaryTagAbsent(t, tagName)
}

// ---------------------------------------------------------------------------
// Repo-admin boundary: a user with REPO_WRITE cannot change repo settings or
// manage hooks.
// ---------------------------------------------------------------------------

func TestLivePermissionRepoAdminDeniedWithRepoWriteOnly(t *testing.T) {
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

	user, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create licensed user failed: %v", err)
	}

	if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, user.Username, openapigenerated.SetPermissionForUserParamsPermissionREPOWRITE); err != nil {
		t.Fatalf("grant repo write permission failed: %v", err)
	}

	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)
	assertBoundaryStoredLevel(t, mustLiveCLI(t, "repo", "permissions", "list", "--repo", repoRef, "--all"), user.Username, "REPO_WRITE")

	asBoundaryAccount(t, harness, seeded.Key, repo.Slug, user, func(t *testing.T) {
		assertBoundaryHeldLevels(t, mustLiveCLI(t, "repo", "permissions", "show"),
			map[string]bool{"REPO_READ": true, "REPO_WRITE": true, "REPO_ADMIN": false})

		// repo admin update requires REPO_ADMIN.
		output, cliErr := executeLiveCLI(t, "--json", "repo", "admin", "update", "--name", "should-be-denied")
		assertAuthorizationError(t, cliErr, output, "repo admin update with REPO_WRITE only")
		assertBoundaryRefusal(t, cliErr, output, boundaryRefusedByBitbucket)
	})

	if stored := nestedJSONMap(t, mustLiveCLI(t, "repo", "get"), "repository"); stored["name"] != repo.Name {
		t.Errorf("repository name = %v after a refused rename, want %q", stored["name"], repo.Name)
	}
}

// Dry-run: repo settings pull-requests update with REPO_WRITE must surface
// authorization error. The pre-flight asks for REPO_ADMIN before planning reads
// the current settings, which a REPO_WRITE user may read, so the refusal comes
// before any plan is emitted.
func TestLivePermissionPullRequestSettingsDryRunDeniedWithRepoWriteOnly(t *testing.T) {
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

	user, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create licensed user failed: %v", err)
	}

	if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, user.Username, openapigenerated.SetPermissionForUserParamsPermissionREPOWRITE); err != nil {
		t.Fatalf("grant repo write permission failed: %v", err)
	}

	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)
	assertBoundaryStoredLevel(t, mustLiveCLI(t, "repo", "permissions", "list", "--repo", repoRef, "--all"), user.Username, "REPO_WRITE")

	// The dry run asks for the opposite of what is stored, so a dry run that
	// wrote it would show. It asked for false, which a fresh repository holds.
	before := boundaryRequiredAllTasksComplete(t)

	asBoundaryAccount(t, harness, seeded.Key, repo.Slug, user, func(t *testing.T) {
		assertBoundaryHeldLevels(t, mustLiveCLI(t, "repo", "permissions", "show"),
			map[string]bool{"REPO_READ": true, "REPO_WRITE": true, "REPO_ADMIN": false})

		output, cliErr := executeLiveCLI(t, "--json", "--dry-run", "repo", "settings", "pull-requests", "update", "--required-all-tasks-complete="+strconv.FormatBool(!before))
		assertDryRunAuthorizationError(t, cliErr, output, "repo settings pull-requests update dry-run with REPO_WRITE only")
		assertBoundaryRefusal(t, cliErr, output, boundaryRefusedByPreflight)
	})

	if after := boundaryRequiredAllTasksComplete(t); after != before {
		t.Errorf("requiredAllTasksComplete = %v after a refused dry run, want %v", after, before)
	}
}

// ---------------------------------------------------------------------------
// Project-admin boundary: a user with PROJECT_WRITE cannot delete a project or
// manage project-level permissions.
// ---------------------------------------------------------------------------

func TestLivePermissionProjectDeleteDeniedWithProjectWriteOnly(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	// Empty, so that the delete is refused for the permission alone and the
	// project standing afterwards shows it was.
	projectKey, _ := boundaryEmptyProject(ctx, t, harness, "Live Test")

	user, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create licensed user failed: %v", err)
	}

	if err := harness.grantProjectPermission(ctx, projectKey, user.Username, "PROJECT_WRITE"); err != nil {
		t.Fatalf("grant project write permission failed: %v", err)
	}

	assertBoundaryStoredLevel(t, mustLiveCLI(t, "project", "permissions", "list", projectKey, "--all"), user.Username, "PROJECT_WRITE")

	// No repository context: nothing here addresses one.
	asBoundaryAccount(t, harness, projectKey, "", user, func(t *testing.T) {
		assertBoundaryHeldLevels(t, mustLiveCLI(t, "project", "permissions", "show", projectKey),
			map[string]bool{"PROJECT_READ": true, "PROJECT_WRITE": true, "PROJECT_ADMIN": false})

		// project delete requires PROJECT_ADMIN.
		output, cliErr := executeLiveCLI(t, "--json", "project", "delete", projectKey, "--yes")
		assertAuthorizationError(t, cliErr, output, "project delete with PROJECT_WRITE only")
		assertBoundaryRefusal(t, cliErr, output, boundaryRefusedByBitbucket)
	})

	assertBoundaryProjectStands(t, projectKey)
}

// Dry-run: project delete with PROJECT_WRITE must surface authorization error.
func TestLivePermissionProjectDeleteDryRunDeniedWithProjectWriteOnly(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	// Empty, for the same reason as the delete that is not a dry run.
	projectKey, _ := boundaryEmptyProject(ctx, t, harness, "Live Test")

	user, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create licensed user failed: %v", err)
	}

	if err := harness.grantProjectPermission(ctx, projectKey, user.Username, "PROJECT_WRITE"); err != nil {
		t.Fatalf("grant project write permission failed: %v", err)
	}

	assertBoundaryStoredLevel(t, mustLiveCLI(t, "project", "permissions", "list", projectKey, "--all"), user.Username, "PROJECT_WRITE")

	asBoundaryAccount(t, harness, projectKey, "", user, func(t *testing.T) {
		assertBoundaryHeldLevels(t, mustLiveCLI(t, "project", "permissions", "show", projectKey),
			map[string]bool{"PROJECT_READ": true, "PROJECT_WRITE": true, "PROJECT_ADMIN": false})

		output, cliErr := executeLiveCLI(t, "--json", "--dry-run", "project", "delete", projectKey, "--yes")
		assertDryRunAuthorizationError(t, cliErr, output, "project delete dry-run with PROJECT_WRITE only")
		// The pre-flight reads the project's permission listing, which
		// Bitbucket keeps to project administrators.
		assertBoundaryRefusal(t, cliErr, output, boundaryRefusedByBitbucket)
	})

	assertBoundaryProjectStands(t, projectKey)
}

// ---------------------------------------------------------------------------
// Project permissions boundary: a user with PROJECT_WRITE cannot manage
// project-level user permissions.
// ---------------------------------------------------------------------------

func TestLivePermissionProjectPermissionGrantDeniedWithProjectWriteOnly(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	user, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create licensed user failed: %v", err)
	}

	if err := harness.grantProjectPermission(ctx, seeded.Key, user.Username, "PROJECT_WRITE"); err != nil {
		t.Fatalf("grant project write permission failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)
	assertBoundaryStoredLevel(t, mustLiveCLI(t, "project", "permissions", "list", seeded.Key, "--all"), user.Username, "PROJECT_WRITE")

	asBoundaryAccount(t, harness, seeded.Key, repo.Slug, user, func(t *testing.T) {
		assertBoundaryHeldLevels(t, mustLiveCLI(t, "project", "permissions", "show", seeded.Key),
			map[string]bool{"PROJECT_READ": true, "PROJECT_WRITE": true, "PROJECT_ADMIN": false})

		// Granting project permissions requires PROJECT_ADMIN.
		output, cliErr := executeLiveCLI(t, "--json", "project", "permissions", "users", "grant", seeded.Key, user.Username, "PROJECT_READ")
		assertAuthorizationError(t, cliErr, output, "project permission grant with PROJECT_WRITE only")
		assertBoundaryRefusal(t, cliErr, output, boundaryRefusedByBitbucket)
	})

	// The grant asked for a level the user does not hold, so a stored one would show.
	assertBoundaryStoredLevel(t, mustLiveCLI(t, "project", "permissions", "list", seeded.Key, "--all"), user.Username, "PROJECT_WRITE")
}

// Dry-run: project permission grant with PROJECT_WRITE must surface authorization error.
func TestLivePermissionProjectPermissionGrantDryRunDeniedWithProjectWriteOnly(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	user, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create licensed user failed: %v", err)
	}

	if err := harness.grantProjectPermission(ctx, seeded.Key, user.Username, "PROJECT_WRITE"); err != nil {
		t.Fatalf("grant project write permission failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)
	assertBoundaryStoredLevel(t, mustLiveCLI(t, "project", "permissions", "list", seeded.Key, "--all"), user.Username, "PROJECT_WRITE")

	asBoundaryAccount(t, harness, seeded.Key, repo.Slug, user, func(t *testing.T) {
		assertBoundaryHeldLevels(t, mustLiveCLI(t, "project", "permissions", "show", seeded.Key),
			map[string]bool{"PROJECT_READ": true, "PROJECT_WRITE": true, "PROJECT_ADMIN": false})

		output, cliErr := executeLiveCLI(t, "--json", "--dry-run", "project", "permissions", "users", "grant", seeded.Key, user.Username, "PROJECT_READ")
		assertDryRunAuthorizationError(t, cliErr, output, "project permission grant dry-run with PROJECT_WRITE only")
		assertBoundaryRefusal(t, cliErr, output, boundaryRefusedByBitbucket)
	})

	assertBoundaryStoredLevel(t, mustLiveCLI(t, "project", "permissions", "list", seeded.Key, "--all"), user.Username, "PROJECT_WRITE")
}

// Dry-run: project create requires global create-project permission. A user with
// only project-scoped admin on an existing project must be denied before any plan
// is emitted.
func TestLivePermissionProjectCreateDryRunDeniedWithProjectAdminOnly(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	user, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create licensed user failed: %v", err)
	}

	if err := harness.grantProjectPermission(ctx, seeded.Key, user.Username, "PROJECT_ADMIN"); err != nil {
		t.Fatalf("grant project admin permission failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)
	assertBoundaryStoredLevel(t, mustLiveCLI(t, "project", "permissions", "list", seeded.Key, "--all"), user.Username, "PROJECT_ADMIN")

	// A key no other run shares, so reading it back as absent cannot find a
	// project somebody else left behind (ADR-085).
	projectKey := strings.ToUpper("DRYDENY" + testsupport.UniqueSuffix())

	asBoundaryAccount(t, harness, seeded.Key, repo.Slug, user, func(t *testing.T) {
		assertBoundaryHeldLevels(t, mustLiveCLI(t, "project", "permissions", "show", seeded.Key),
			map[string]bool{"PROJECT_READ": true, "PROJECT_WRITE": true, "PROJECT_ADMIN": true})

		output, cliErr := executeLiveCLI(t, "--json", "--dry-run", "project", "create", projectKey, "--name", "dry deny")
		assertDryRunAuthorizationError(t, cliErr, output, "project create dry-run with PROJECT_ADMIN only")
		assertBoundaryRefusal(t, cliErr, output, boundaryRefusedByBitbucket)
	})

	assertBoundaryProjectAbsent(t, projectKey)
}

// Dry-run ownership boundary: approving a pull request should be denied up-front if
// the caller cannot even read the repo. This exercises the conservative ownership-aware
// precheck path for PR review commands.
func TestLivePermissionPRApproveDryRunDeniedWithoutRepoRead(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 2)
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

	// The fixture read back: the pull request the dry run aims at merges the
	// branch pushed for it.
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)
	fixture := extractPRData(decodeJSONMap(t, mustLiveCLI(t, "pr", "get", prID)))
	if fixture["sourceBranch"] != branch || fixture["targetBranch"] != "master" {
		t.Fatalf("pull request %s merges %v into %v, want %s into master", prID, fixture["sourceBranch"], fixture["targetBranch"], branch)
	}

	user, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create licensed user failed: %v", err)
	}

	asBoundaryAccount(t, harness, seeded.Key, repo.Slug, user, func(t *testing.T) {
		assertBoundaryHeldLevels(t, mustLiveCLI(t, "repo", "permissions", "show"),
			map[string]bool{"REPO_READ": false, "REPO_WRITE": false, "REPO_ADMIN": false})

		output, cliErr := executeLiveCLI(t, "--json", "--dry-run", "pr", "review", "approve", prID)
		assertDryRunAuthorizationError(t, cliErr, output, "pr review approve dry-run without repo access")
		assertBoundaryRefusal(t, cliErr, output, boundaryRefusedByBitbucket)
	})

	// Read through the client: bb reports reviewers only, and an approval from
	// anyone else is recorded as a participant.
	limit := float32(100)
	participants, err := harness.client.ListParticipantsWithResponse(ctx, seeded.Key, repo.Slug, prID, &openapigenerated.ListParticipantsParams{Limit: &limit})
	if err != nil {
		t.Fatalf("list participants call failed: %v", err)
	}
	if participants.StatusCode() != http.StatusOK || participants.ApplicationjsonCharsetUTF8200 == nil {
		t.Fatalf("list participants returned status %d: %s", participants.StatusCode(), participants.Body)
	}
	// The whole list, and this pull request's: its author is always on it, so
	// an empty page cannot pass for nobody having approved.
	page := participants.ApplicationjsonCharsetUTF8200
	if page.IsLastPage == nil || !*page.IsLastPage {
		t.Fatalf("the participants of pull request %s do not fit one page: %s", prID, participants.Body)
	}
	author := false
	if page.Values != nil {
		for _, participant := range *page.Values {
			name := ""
			if participant.User != nil {
				name = participant.User.Name
			}
			approved := participant.Approved != nil && *participant.Approved
			if strings.EqualFold(name, user.Username) || approved {
				t.Errorf("a refused dry run left participant %q approved=%v on pull request %s", name, approved, prID)
			}
			if participant.Role != nil && *participant.Role == openapigenerated.RestPullRequestParticipantRoleAUTHOR {
				author = true
			}
		}
	}
	if !author {
		t.Errorf("the participants read back for pull request %s name no author: %s", prID, participants.Body)
	}
}

// Dry-run ownership boundary: updating a comment should be denied up-front if the
// caller cannot read the repo. This exercises the conservative ownership-aware
// precheck path for comment mutation commands.
func TestLivePermissionCommentUpdateDryRunDeniedWithoutRepoRead(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 2)
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
	if fixture := extractPRData(decodeJSONMap(t, mustLiveCLI(t, "pr", "get", prID))); fixture["sourceBranch"] != prBranch || fixture["targetBranch"] != "master" {
		t.Fatalf("pull request %s merges %v into %v, want %s into master", prID, fixture["sourceBranch"], fixture["targetBranch"], prBranch)
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

	// The fixture read back through its own request, with the version a refused
	// update has to leave where it is.
	stored := nestedJSONMap(t, mustLiveCLI(t, "pr", "comment", "get", prID, commentID), "comment")
	if stored["text"] != "ownership precheck fixture" {
		t.Fatalf("comment %s holds text %v, want the fixture's", commentID, stored["text"])
	}

	user, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create licensed user failed: %v", err)
	}

	asBoundaryAccount(t, harness, seeded.Key, repo.Slug, user, func(t *testing.T) {
		assertBoundaryHeldLevels(t, mustLiveCLI(t, "repo", "permissions", "show"),
			map[string]bool{"REPO_READ": false, "REPO_WRITE": false, "REPO_ADMIN": false})

		output, cliErr := executeLiveCLI(t, "--json", "--dry-run", "repo", "comment", "update", "--pr", prID, "--id", commentID, "--text", "denied update")
		assertDryRunAuthorizationError(t, cliErr, output, "repo comment update dry-run without repo access")
		assertBoundaryRefusal(t, cliErr, output, boundaryRefusedByBitbucket)
	})

	after := nestedJSONMap(t, mustLiveCLI(t, "pr", "comment", "get", prID, commentID), "comment")
	if after["text"] != stored["text"] || after["version"] != stored["version"] {
		t.Errorf("comment %s changed after a refused dry run: text %v version %v, want text %v version %v",
			commentID, after["text"], after["version"], stored["text"], stored["version"])
	}
}

// ---------------------------------------------------------------------------
// Repo-admin boundary via pull-request settings: a user with REPO_WRITE cannot
// change pull-request settings (requires REPO_ADMIN).
//
// This said such a user could not read them either, and asserted a refusal of
// `get`. Reading takes REPO_READ; the refusal it saw was the missing licence.
// ---------------------------------------------------------------------------

func TestLivePermissionPullRequestSettingsDeniedWithRepoWriteOnly(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	// We only need a seeded project so we have something to grant.
	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	user, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create licensed user failed: %v", err)
	}

	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug

	// Grant only REPO_WRITE — insufficient to change pull-request settings
	// (requires REPO_ADMIN).
	if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, user.Username, openapigenerated.SetPermissionForUserParamsPermissionREPOWRITE); err != nil {
		t.Fatalf("grant repo write permission failed: %v", err)
	}

	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)
	assertBoundaryStoredLevel(t, mustLiveCLI(t, "repo", "permissions", "list", "--repo", repoRef, "--all"), user.Username, "REPO_WRITE")
	before := boundaryRequiredAllTasksComplete(t)

	asBoundaryAccount(t, harness, seeded.Key, repo.Slug, user, func(t *testing.T) {
		assertBoundaryHeldLevels(t, mustLiveCLI(t, "repo", "permissions", "show"),
			map[string]bool{"REPO_READ": true, "REPO_WRITE": true, "REPO_ADMIN": false})

		// What the user may do: read the settings the update below may not change.
		if got := boundaryRequiredAllTasksComplete(t); got != before {
			t.Errorf("the user reads requiredAllTasksComplete=%v, the administrator %v", got, before)
		}

		output, cliErr := executeLiveCLI(t, "--json", "repo", "settings", "pull-requests", "update", "--required-all-tasks-complete="+strconv.FormatBool(!before))
		assertAuthorizationError(t, cliErr, output, "repo settings pull-requests update with REPO_WRITE only")
		assertBoundaryRefusal(t, cliErr, output, boundaryRefusedByBitbucket)
	})

	if after := boundaryRequiredAllTasksComplete(t); after != before {
		t.Errorf("requiredAllTasksComplete = %v after a refused update, want %v", after, before)
	}
}

// ---------------------------------------------------------------------------
// Repo admin create boundary: PROJECT_READ cannot create repositories.
// ---------------------------------------------------------------------------

func TestLivePermissionRepoCreateDeniedWithProjectReadOnly(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	user, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create licensed user failed: %v", err)
	}

	// Only grant PROJECT_READ — insufficient to create repos (requires PROJECT_WRITE+).
	if err := harness.grantProjectPermission(ctx, seeded.Key, user.Username, "PROJECT_READ"); err != nil {
		t.Fatalf("grant project read permission failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)
	assertBoundaryStoredLevel(t, mustLiveCLI(t, "project", "permissions", "list", seeded.Key, "--all"), user.Username, "PROJECT_READ")

	asBoundaryAccount(t, harness, seeded.Key, repo.Slug, user, func(t *testing.T) {
		assertBoundaryHeldLevels(t, mustLiveCLI(t, "project", "permissions", "show", seeded.Key),
			map[string]bool{"PROJECT_READ": true, "PROJECT_WRITE": false, "PROJECT_ADMIN": false})

		output, cliErr := executeLiveCLI(t, "--json", "repo", "admin", "create", "--project", seeded.Key, "--name", "denied-repo")
		assertAuthorizationError(t, cliErr, output, "repo create with PROJECT_READ only")
		assertBoundaryRefusal(t, cliErr, output, boundaryRefusedByBitbucket)
	})

	assertBoundaryRepositoryAbsent(t, seeded.Key+"/denied-repo")
}

// Dry-run: repo create with PROJECT_READ must surface authorization error.
func TestLivePermissionRepoCreateDryRunDeniedWithProjectReadOnly(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	user, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create licensed user failed: %v", err)
	}

	if err := harness.grantProjectPermission(ctx, seeded.Key, user.Username, "PROJECT_READ"); err != nil {
		t.Fatalf("grant project read permission failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)
	assertBoundaryStoredLevel(t, mustLiveCLI(t, "project", "permissions", "list", seeded.Key, "--all"), user.Username, "PROJECT_READ")

	asBoundaryAccount(t, harness, seeded.Key, repo.Slug, user, func(t *testing.T) {
		assertBoundaryHeldLevels(t, mustLiveCLI(t, "project", "permissions", "show", seeded.Key),
			map[string]bool{"PROJECT_READ": true, "PROJECT_WRITE": false, "PROJECT_ADMIN": false})

		output, cliErr := executeLiveCLI(t, "--json", "--dry-run", "repo", "admin", "create", "--project", seeded.Key, "--name", "denied-repo-dry")
		assertDryRunAuthorizationError(t, cliErr, output, "repo create dry-run with PROJECT_READ only")
		// A reader finds the project, just not among those filtered by
		// PROJECT_WRITE, so the pre-flight is what refuses.
		assertBoundaryRefusal(t, cliErr, output, boundaryRefusedByPreflight)
	})

	assertBoundaryRepositoryAbsent(t, seeded.Key+"/denied-repo-dry")
}

// ---------------------------------------------------------------------------
// bb repo permissions show — effective permission inspection for the caller.
//
// The administrator passes every level on every repository, so its answer is
// the same whether the probes filter by level and repository or not. Each show
// test therefore also asks as an account holding one level on one resource and
// nothing on another, which only probes that apply both can answer.
// ---------------------------------------------------------------------------

// TestLiveRepoPermissionsShowAsAdmin verifies that an admin-level caller sees
// REPO_READ=true, REPO_WRITE=true, and REPO_ADMIN=true on their own repository,
// and that both JSON and human output contain the expected fields.
func TestLiveRepoPermissionsShowAsAdmin(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	// The second repository is the one the account holds nothing on.
	seeded, err := harness.seedIsolatedProject(ctx, 2, 1)
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
	assertBoundaryHumanHeldLevels(t, humanOutput, map[string]bool{"REPO_READ": true, "REPO_WRITE": true, "REPO_ADMIN": true})

	account, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create licensed user failed: %v", err)
	}
	if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, account.Username, openapigenerated.SetPermissionForUserParamsPermissionREPOWRITE); err != nil {
		t.Fatalf("grant repo write permission failed: %v", err)
	}
	assertBoundaryStoredLevel(t, mustLiveCLI(t, "repo", "permissions", "list", "--repo", repoRef, "--all"), account.Username, "REPO_WRITE")

	otherRef := seeded.Key + "/" + seeded.Repos[1].Slug
	asBoundaryAccount(t, harness, seeded.Key, repo.Slug, account, func(t *testing.T) {
		writer := map[string]bool{"REPO_READ": true, "REPO_WRITE": true, "REPO_ADMIN": false}
		assertBoundaryHeldLevels(t, mustLiveCLI(t, "repo", "permissions", "show", "--repo", repoRef), writer)
		assertBoundaryHumanHeldLevels(t, mustLiveHumanCLI(t, "repo", "permissions", "show", "--repo", repoRef), writer)

		// The same project, so only matching the slug keeps this one apart.
		assertBoundaryHeldLevels(t, mustLiveCLI(t, "repo", "permissions", "show", "--repo", otherRef),
			map[string]bool{"REPO_READ": false, "REPO_WRITE": false, "REPO_ADMIN": false})
	})
}

// ---------------------------------------------------------------------------
// bb project permissions show <project-key> — effective permission inspection for the caller.
//
// Same as above: the administrator's answer, then an account's.
// ---------------------------------------------------------------------------

// TestLiveProjectPermissionsShowAsAdmin verifies that an admin-level caller sees
// all three project permission levels as true, and that both JSON and human output
// contain the expected fields.
func TestLiveProjectPermissionsShowAsAdmin(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	configureLiveCLIEnv(t, harness, seeded.Key, seeded.Repos[0].Slug)

	// A project listed ahead of this one when projects are asked for by this
	// one's name, because Bitbucket matches every name that contains it and
	// sorts by name. The probes behind show ask that way, so they have to look
	// past it.
	decoy, decoyName := boundaryEmptyProject(ctx, t, harness, "A "+seeded.Name)
	if stored := nestedJSONMap(t, mustLiveCLI(t, "project", "get", decoy), "project"); stored["name"] != decoyName {
		t.Fatalf("project %s is named %v, want %q", decoy, stored["name"], decoyName)
	}
	var named struct {
		Projects []map[string]any `json:"projects"`
	}
	decodeJSONData(t, mustLiveCLI(t, "project", "list", "--name", seeded.Name), &named)
	if len(named.Projects) != 2 || named.Projects[0]["key"] != decoy || named.Projects[1]["key"] != seeded.Key {
		t.Fatalf("projects listed for the name %q are %v, want %s ahead of %s", seeded.Name, named.Projects, decoy, seeded.Key)
	}

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
	assertBoundaryHumanHeldLevels(t, humanOutput, map[string]bool{"PROJECT_READ": true, "PROJECT_WRITE": true, "PROJECT_ADMIN": true})

	account, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create licensed user failed: %v", err)
	}
	if err := harness.grantProjectPermission(ctx, seeded.Key, account.Username, "PROJECT_WRITE"); err != nil {
		t.Fatalf("grant project write permission failed: %v", err)
	}
	assertBoundaryStoredLevel(t, mustLiveCLI(t, "project", "permissions", "list", seeded.Key, "--all"), account.Username, "PROJECT_WRITE")

	// A project the account holds nothing on.
	other, _ := boundaryEmptyProject(ctx, t, harness, "Live Test")

	asBoundaryAccount(t, harness, seeded.Key, seeded.Repos[0].Slug, account, func(t *testing.T) {
		writer := map[string]bool{"PROJECT_READ": true, "PROJECT_WRITE": true, "PROJECT_ADMIN": false}
		assertBoundaryHeldLevels(t, mustLiveCLI(t, "project", "permissions", "show", seeded.Key), writer)
		assertBoundaryHumanHeldLevels(t, mustLiveHumanCLI(t, "project", "permissions", "show", seeded.Key), writer)

		assertBoundaryHeldLevels(t, mustLiveCLI(t, "project", "permissions", "show", other),
			map[string]bool{"PROJECT_READ": false, "PROJECT_WRITE": false, "PROJECT_ADMIN": false})
	})
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
//
// Each grant asks for a level its subject does not already hold, so reading
// the level back tells a write that was stored from one that was dropped.
func TestLivePermissionAliasSubjects(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
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
		// REPO_ADMIN here, so the human grant of REPO_WRITE below is a change.
		userGrant := mustLiveCLI(t, "repo", "permissions", "grant", holder.Username, "repo_admin")
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
		assertBoundaryStoredLevel(t, users, holder.Username, "REPO_ADMIN")
		assertBoundaryStoredLevel(t, users, group, "")

		groups := mustLiveCLI(t, "repo", "permissions", "list", "--group")
		if !strings.Contains(groups, group) {
			t.Errorf("the group grant is not in the group listing:\n%s", groups)
		}
		assertBoundaryStoredLevel(t, groups, group, "REPO_READ")
	})

	t.Run("human output names a group as one and a user plainly", func(t *testing.T) {
		userOutput := mustLiveHumanCLI(t, "repo", "permissions", "grant", holder.Username, "repo_write")
		if !strings.Contains(userOutput, "to "+holder.Username) || strings.Contains(userOutput, "to group") {
			t.Errorf("expected a bare user name, got: %s", userOutput)
		}
		assertBoundaryStoredLevel(t, mustLiveCLI(t, "repo", "permissions", "list"), holder.Username, "REPO_WRITE")

		groupOutput := mustLiveHumanCLI(t, "repo", "permissions", "revoke", "--group", group, "--yes")
		if !strings.Contains(groupOutput, "for group "+group) {
			t.Errorf("expected the group to be named as one, got: %s", groupOutput)
		}
		assertBoundaryStoredLevel(t, mustLiveCLI(t, "repo", "permissions", "list", "--group"), group, "")
	})

	t.Run("the dry-run target follows the subject", func(t *testing.T) {
		userPreview := mustLiveCLI(t, "--dry-run", "repo", "permissions", "grant", holder.Username, "repo_read")
		assertPermissionPreviewSubject(t, userPreview, "user", holder.Username)

		groupPreview := mustLiveCLI(t, "--dry-run", "repo", "permissions", "grant", "--group", group, "repo_admin")
		assertPermissionPreviewSubject(t, groupPreview, "group", group)

		assertBoundaryStoredLevel(t, mustLiveCLI(t, "repo", "permissions", "list"), holder.Username, "REPO_WRITE")
		assertBoundaryStoredLevel(t, mustLiveCLI(t, "repo", "permissions", "list", "--group"), group, "")
	})

	// The four predictions, against entries the commands above left behind:
	// holder holds REPO_WRITE and nobody else holds anything.
	t.Run("the prediction reads the permissions that are there", func(t *testing.T) {
		stranger, err := harness.createLicensedUser(ctx)
		if err != nil {
			t.Fatalf("create user failed: %v", err)
		}

		for _, testCase := range []struct {
			name    string
			outcome jsonoutput.Outcome
			// reason tells a create, an update and a delete apart, which the
			// outcome calls would-apply alike.
			reason string
			args   []string
		}{
			{name: "granting what is already held", outcome: jsonoutput.OutcomeNoOp, reason: "already has", args: []string{"grant", holder.Username, "repo_write"}},
			{name: "granting a different level", outcome: jsonoutput.OutcomeWouldApply, reason: "will be updated", args: []string{"grant", holder.Username, "repo_admin"}},
			{name: "granting someone with nothing", outcome: jsonoutput.OutcomeWouldApply, reason: "will create", args: []string{"grant", stranger.Username, "repo_read"}},
			{name: "revoking what is held", outcome: jsonoutput.OutcomeWouldApply, reason: "will be removed", args: []string{"revoke", holder.Username}},
			{name: "revoking what is not", outcome: jsonoutput.OutcomeNoOp, reason: "does not currently have", args: []string{"revoke", stranger.Username}},
		} {
			t.Run(testCase.name, func(t *testing.T) {
				output := mustLiveCLI(t, append([]string{"--dry-run", "repo", "permissions"}, testCase.args...)...)
				assertLivePreview(t, output, testCase.outcome, testCase.reason)
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
		// The level too: a dry run that updated holder would leave the name listed.
		assertBoundaryStoredLevel(t, after, holder.Username, "REPO_WRITE")
		assertBoundaryStoredLevel(t, after, stranger.Username, "")
	})
}

// TestLivePermissionAliasSubjectsForProjects is the project twin.
//
// It carries one assertion the repository version does not: a user grant and a
// group grant have to publish the same field names. Naming the subject in a
// field rather than in the key is what lets `--describe` state one shape for
// one command, and a consumer needs two code paths the moment they diverge.
func TestLivePermissionAliasSubjectsForProjects(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	configureLiveCLIEnv(t, harness, seeded.Key, seeded.Repos[0].Slug)

	holder, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	const group = "stash-users"

	// PROJECT_ADMIN here, so the human grant of PROJECT_WRITE below is a change.
	userGrant := mustLiveCLI(t, "project", "permissions", "grant", seeded.Key, holder.Username, "project_admin")
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
	assertBoundaryStoredLevel(t, users, holder.Username, "PROJECT_ADMIN")
	assertBoundaryStoredLevel(t, groups, group, "PROJECT_READ")

	humanGrant := mustLiveHumanCLI(t, "project", "permissions", "grant", seeded.Key, holder.Username, "project_write")
	if !strings.Contains(humanGrant, "Granted PROJECT_WRITE") ||
		!strings.Contains(humanGrant, "to "+holder.Username+" for project "+seeded.Key) {
		t.Errorf("expected a bare user name in the human grant, got: %s", humanGrant)
	}
	assertBoundaryStoredLevel(t, mustLiveCLI(t, "project", "permissions", "list", seeded.Key), holder.Username, "PROJECT_WRITE")

	humanRevoke := mustLiveHumanCLI(t, "project", "permissions", "revoke", "--group", seeded.Key, group, "--yes")
	if !strings.Contains(humanRevoke, "Revoked permission") ||
		!strings.Contains(humanRevoke, "for group "+group+" on project "+seeded.Key) {
		t.Errorf("expected the group to be named as one, got: %s", humanRevoke)
	}
	assertBoundaryStoredLevel(t, mustLiveCLI(t, "project", "permissions", "list", "--group", seeded.Key), group, "")

	// Levels the subjects do not hold, so a dry run that wrote one would show.
	userPreview := mustLiveCLI(t, "--dry-run", "project", "permissions", "grant", seeded.Key, holder.Username, "project_read")
	assertPermissionPreviewSubject(t, userPreview, "user", holder.Username)
	groupPreview := mustLiveCLI(t, "--dry-run", "project", "permissions", "grant", "--group", seeded.Key, group, "project_admin")
	assertPermissionPreviewSubject(t, groupPreview, "group", group)
	assertBoundaryStoredLevel(t, mustLiveCLI(t, "project", "permissions", "list", seeded.Key), holder.Username, "PROJECT_WRITE")
	assertBoundaryStoredLevel(t, mustLiveCLI(t, "project", "permissions", "list", "--group", seeded.Key), group, "")
}

// assertPermissionPreviewSubject checks a permission dry run is about the
// subject the invocation named. The effect's target is where the preview says
// so: a --group that did not reach the command would leave a user grant of the
// same name, previewed as confidently.
func assertPermissionPreviewSubject(t *testing.T, output, subject, name string) {
	t.Helper()

	target := decodeLivePreview(t, output).effect(t, output).Target
	if target["subject"] != subject || target["name"] != name {
		t.Errorf("expected the %s %s in the dry-run target, got %v:\n%s", subject, name, target, output)
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
