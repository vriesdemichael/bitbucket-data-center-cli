//go:build live

package live_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli"
)

// Default reviewer resolution, proven against the server rather than against a
// mock of it.
//
// The unit tests these replace asserted the query bb sends -- that refs are
// qualified to refs/heads/..., that a repository id is included on both sides.
// Each of those is a belief about what Bitbucket wants, and a mock built from
// the same belief agrees with it however wrong it is. What matters is whether
// the server matches the condition, and only the server can answer that.
//
// The negative case is what gives the positive one meaning: a query carrying
// the wrong refs would either match nothing or match everything, and one of the
// two subtests catches each.
func TestLiveDefaultReviewersResolveAgainstTheCondition(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug

	reviewer, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create reviewer failed: %v", err)
	}
	if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, reviewer.Username, "REPO_READ"); err != nil {
		t.Fatalf("grant reviewer read access failed: %v", err)
	}

	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// A second target branch, so the two cases differ only in where the pull
	// request is aimed.
	const otherTarget = "release/1.x"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, otherTarget, "release.txt"); err != nil {
		t.Fatalf("push release branch failed: %v", err)
	}

	// The condition applies to master only.
	// The endpoint wants the reviewer by numeric id: a name is read as id -1
	// and refused.
	reviewerID, err := harness.userID(ctx, reviewer.Username)
	if err != nil {
		t.Fatalf("look up the reviewer id: %v", err)
	}

	condition := fmt.Sprintf(`{
		"sourceMatcher": {"id": "ANY_REF", "type": {"id": "ANY_REF"}},
		"targetMatcher": {"id": "refs/heads/master", "type": {"id": "BRANCH"}},
		"reviewers": [{"id": %d}],
		"requiredApprovals": 1
	}`, reviewerID)
	created := mustLiveCLI(t, "reviewer", "condition", "create", condition, "--repo", repoRef)

	// Both subtests reason from the condition, so it is read back before
	// either runs rather than taken from the create's answer.
	conditionID, ok := numericOrStringID(decodeJSONMap(t, created)["id"])
	if !ok {
		t.Fatalf("the condition create answered without an id:\n%s", created)
	}
	listing := mustLiveCLI(t, "reviewer", "condition", "list", "--repo", repoRef)
	stored, found := governanceCondition(t, listing, conditionID)
	if !found {
		t.Fatalf("condition %s is not in the listing:\n%s", conditionID, listing)
	}
	assertGovernanceConditionStored(t, stored, "", "refs/heads/master", reviewerID, 1)

	t.Run("a pull request the condition matches gets the reviewer", func(t *testing.T) {
		const branch = "feature/matches-the-condition"
		if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "matches.txt"); err != nil {
			t.Fatalf("push commit on branch failed: %v", err)
		}

		output := mustLiveCLI(t, "pr", "create",
			"--from-ref", branch, "--to-ref", "refs/heads/master",
			"--title", "Matches the condition", "--default-reviewers", "--no-codeowners")

		if names := decodeLivePRReviewers(t, decodeJSONMap(t, output)); !containsFold(names, reviewer.Username) {
			t.Fatalf("expected the default reviewer %s to be assigned, got %v", reviewer.Username, names)
		}
		governancePullRequestAsStored(t, output, "Matches the condition", branch, "master")
	})

	t.Run("a pull request it does not match gets nobody", func(t *testing.T) {
		const branch = "feature/other-target"
		if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "other.txt"); err != nil {
			t.Fatalf("push commit on branch failed: %v", err)
		}

		output := mustLiveCLI(t, "pr", "create",
			"--from-ref", branch, "--to-ref", "refs/heads/"+otherTarget,
			"--title", "Different target", "--default-reviewers", "--no-codeowners")

		// A resolution sending unqualified refs, or omitting the repository
		// ids, would fail here by matching everything.
		if names := decodeLivePRReviewers(t, decodeJSONMap(t, output)); containsFold(names, reviewer.Username) {
			t.Fatalf("the condition targets master only, but %s was assigned on a pull request into %s: %v",
				reviewer.Username, otherTarget, names)
		}
		governancePullRequestAsStored(t, output, "Different target", branch, otherTarget)
	})
}

// governancePullRequestAsStored reads back the pull request a `pr create`
// answered with, through `bb pr get`, and returns it.
//
// The create's answer is what Bitbucket said back, not a read of what it kept.
// So the stored pull request must hold the reviewers that answer reported,
// which carries every assertion a test makes on the answer over to the stored
// state, and the title and branches the create sent.
func governancePullRequestAsStored(t *testing.T, createOutput, title, sourceBranch, targetBranch string) map[string]any {
	t.Helper()

	created := extractPRData(decodeJSONMap(t, createOutput))
	id, ok := numericOrStringID(created["id"])
	if !ok {
		t.Fatalf("the create answered without a pull request id:\n%s", createOutput)
	}

	stored := extractPRData(decodeJSONMap(t, mustLiveCLI(t, "pr", "get", id)))
	if stored["title"] != title || stored["sourceBranch"] != sourceBranch || stored["targetBranch"] != targetBranch {
		t.Errorf("pull request %s is stored as %q from %v into %v, want %q from %s into %s",
			id, stored["title"], stored["sourceBranch"], stored["targetBranch"], title, sourceBranch, targetBranch)
	}

	createdReviewers, storedReviewers := decodeLivePRReviewers(t, created), decodeLivePRReviewers(t, stored)
	if !governanceSameNames(storedReviewers, createdReviewers) {
		t.Errorf("pull request %s holds reviewers %v, but its create answered with %v", id, storedReviewers, createdReviewers)
	}

	return stored
}

// TestLiveDefaultReviewersFromAFork covers the source side of the same query.
//
// A fork pull request is resolved with the fork as the source repository and
// the upstream as the target. The unit test this replaces asserted that the two
// ids differ in the request; what matters is that the server still matches the
// condition, which it can only do if both are right.
func TestLiveDefaultReviewersFromAFork(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	upstream := seeded.Repos[0]
	upstreamRef := seeded.Key + "/" + upstream.Slug

	reviewer, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create reviewer failed: %v", err)
	}
	if err := harness.grantRepoPermission(ctx, seeded.Key, upstream.Slug, reviewer.Username, "REPO_READ"); err != nil {
		t.Fatalf("grant reviewer read access failed: %v", err)
	}

	configureLiveCLIEnv(t, harness, seeded.Key, upstream.Slug)

	// The endpoint wants the reviewer by numeric id: a name is read as id -1
	// and refused.
	reviewerID, err := harness.userID(ctx, reviewer.Username)
	if err != nil {
		t.Fatalf("look up the reviewer id: %v", err)
	}

	condition := fmt.Sprintf(`{
		"sourceMatcher": {"id": "ANY_REF", "type": {"id": "ANY_REF"}},
		"targetMatcher": {"id": "refs/heads/master", "type": {"id": "BRANCH"}},
		"reviewers": [{"id": %d}],
		"requiredApprovals": 1
	}`, reviewerID)
	created := mustLiveCLI(t, "reviewer", "condition", "create", condition, "--repo", upstreamRef)

	conditionID, ok := numericOrStringID(decodeJSONMap(t, created)["id"])
	if !ok {
		t.Fatalf("the condition create answered without an id:\n%s", created)
	}
	listing := mustLiveCLI(t, "reviewer", "condition", "list", "--repo", upstreamRef)
	stored, found := governanceCondition(t, listing, conditionID)
	if !found {
		t.Fatalf("condition %s is not in the listing:\n%s", conditionID, listing)
	}
	assertGovernanceConditionStored(t, stored, "", "refs/heads/master", reviewerID, 1)

	// The fork is a fixture posted raw. Its name, slug and project are proven by
	// the push below, which goes to that project and slug and fails otherwise.
	forkSlug := upstream.Slug + "-reviewer-fork"
	postLiveJSON(t, fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s", seeded.Key, upstream.Slug), map[string]any{
		"name":    forkSlug,
		"slug":    forkSlug,
		"project": map[string]any{"key": seeded.Key},
	})

	const branch = "feature/from-the-fork"
	if err := harness.pushCommitOnBranch(seeded.Key, forkSlug, branch, "forked.txt"); err != nil {
		t.Fatalf("push commit on the fork failed: %v", err)
	}

	output := mustLiveCLI(t, "pr", "create",
		"--repo", upstreamRef,
		"--from-repo", seeded.Key+"/"+forkSlug,
		"--from-ref", branch,
		"--to-ref", "refs/heads/master",
		"--title", "From the fork with default reviewers",
		"--default-reviewers", "--no-codeowners")

	if names := decodeLivePRReviewers(t, decodeJSONMap(t, output)); !containsFold(names, reviewer.Username) {
		t.Fatalf("expected %s to be assigned on the fork pull request, got %v", reviewer.Username, names)
	}

	// --from-repo is what makes this a fork pull request at all, so the stored
	// one has to name the fork as its source and the upstream as its target.
	pr := governancePullRequestAsStored(t, output, "From the fork with default reviewers", branch, "master")
	assertLivePRRepository(t, pr, "sourceRepository", forkSlug)
	assertLivePRRepository(t, pr, "repository", upstream.Slug)
}

// TestLiveReviewerGroupResolutionShapes covers what the reviewer-group lookup
// meets on a real server.
//
// One of the unit tests it replaces asserted the behaviour for a group with no
// members. Bitbucket refuses to create one, so that test described a state the
// server cannot be in and agreeing with it proved nothing. The refusal is
// recorded here instead.
func TestLiveReviewerGroupResolutionShapes(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
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
		t.Fatalf("grant member read access failed: %v", err)
	}
	// Read back, because the group below would not notice a grant that was not
	// kept: Bitbucket takes a member who cannot see the repository.
	grants := mustLiveCLI(t, "repo", "settings", "security", "permissions", "users", "list", "--repo", repoRef)
	if held := governanceHeldPermission(t, grants, member.Username); held != "REPO_READ" {
		t.Fatalf("%s holds %q on the repository, want REPO_READ:\n%s", member.Username, held, grants)
	}

	const groupName = "resolution_shapes"
	if err := harness.createReviewerGroup(ctx, seeded.Key, repo.Slug, groupName, member.Username); err != nil {
		t.Fatalf("create reviewer group failed: %v", err)
	}

	t.Run("members come back for a group that exists", func(t *testing.T) {
		output := mustLiveCLI(t, "reviewer-group", "users", groupName, "--repo", repoRef)
		if !strings.Contains(output, member.Username) {
			t.Fatalf("expected %s in the group members, got:\n%s", member.Username, output)
		}
		if members := governanceUserNames(decodeJSONMap(t, output)["users"]); !governanceSameNames(members, []string{member.Username}) {
			t.Errorf("group members = %v, want just %s", members, member.Username)
		}
	})

	t.Run("a group that is not there is refused, not silently empty", func(t *testing.T) {
		output, err := executeLiveCLI(t, "--json", "reviewer-group", "users", "no_such_group", "--repo", repoRef)
		if err == nil {
			t.Fatalf("expected a failure for a group that is not there, got:\n%s", output)
		}
	})

	t.Run("the server refuses a group with no members", func(t *testing.T) {
		_, err := harness.liveJSON(ctx, http.MethodPost,
			fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/settings/reviewer-groups", seeded.Key, repo.Slug),
			map[string]any{"name": "empty_group", "scope": map[string]any{"resourceId": 1, "type": "REPOSITORY"}})
		if err == nil {
			t.Fatal("expected the server to refuse an empty reviewer group")
		}
		if !strings.Contains(err.Error(), "1 or more reviewer") {
			t.Errorf("expected the empty-group refusal, got: %v", err)
		}
		if listing := mustLiveCLI(t, "reviewer-group", "list", "--repo", repoRef); governanceReviewerGroupNamed(t, listing, "empty_group") {
			t.Errorf("the refused create left a group behind:\n%s", listing)
		}
	})
}

// TestLiveReviewerConditionInputRoutes covers the two ways a condition can be
// handed to bb that are not an argument: --config-file and stdin.
//
// Unit tests drove both against a handler that answered 201 to any POST whose
// path contained "condition", so a body that was not a condition at all would
// have passed. The proof here is that the condition exists afterwards and says
// what the file said.
func TestLiveReviewerConditionInputRoutes(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	reviewer, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create reviewer failed: %v", err)
	}
	if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, reviewer.Username, "REPO_READ"); err != nil {
		t.Fatalf("grant reviewer read access failed: %v", err)
	}
	reviewerID, err := harness.userID(ctx, reviewer.Username)
	if err != nil {
		t.Fatalf("look up the reviewer id: %v", err)
	}

	condition := func(approvals int) string {
		return fmt.Sprintf(`{
			"sourceMatcher": {"id": "ANY_REF", "type": {"id": "ANY_REF"}},
			"targetMatcher": {"id": "refs/heads/master", "type": {"id": "BRANCH"}},
			"reviewers": [{"id": %d}],
			"requiredApprovals": %d
		}`, reviewerID, approvals)
	}

	configPath := filepath.Join(t.TempDir(), "condition.json")
	if err := os.WriteFile(configPath, []byte(condition(1)), 0o600); err != nil {
		t.Fatalf("write condition file: %v", err)
	}

	created := mustLiveCLI(t, "reviewer", "condition", "create", "--config-file", configPath, "--repo", repoRef)
	conditionID := asString(decodeJSONMap(t, created)["id"])
	if conditionID == "" {
		if inner, ok := decodeJSONMap(t, created)["condition"].(map[string]any); ok {
			conditionID = asString(inner["id"])
		}
	}
	if conditionID == "" {
		t.Fatalf("the created condition has no id:\n%s", created)
	}

	listed := mustLiveCLI(t, "reviewer", "condition", "list", "--repo", repoRef)
	if !strings.Contains(listed, conditionID) {
		t.Fatalf("the condition created from a file is not in the listing:\n%s", listed)
	}
	fromFile, found := governanceCondition(t, listed, conditionID)
	if !found {
		t.Fatalf("condition %s is not an entry of the listing:\n%s", conditionID, listed)
	}
	assertGovernanceConditionStored(t, fromFile, "", "refs/heads/master", reviewerID, 1)

	// stdin, on the update. Two required approvals rather than one, so the
	// listing afterwards says which body arrived.
	updateCommand := cli.NewRootCommand()
	updateCommand.SetIn(strings.NewReader(condition(2)))
	updateOutput := &strings.Builder{}
	updateCommand.SetOut(updateOutput)
	updateCommand.SetErr(updateOutput)
	updateCommand.SetArgs([]string{"--json", "reviewer", "condition", "update", conditionID, "-", "--repo", repoRef})
	if err := updateCommand.Execute(); err != nil {
		t.Fatalf("reviewer condition update from stdin failed: %v\noutput: %s", err, updateOutput.String())
	}

	afterUpdate := mustLiveCLI(t, "reviewer", "condition", "list", "--repo", repoRef)
	if !strings.Contains(afterUpdate, `"requiredApprovals": 2`) {
		t.Fatalf("the update read from stdin did not take:\n%s", afterUpdate)
	}
	fromStdin, found := governanceCondition(t, afterUpdate, conditionID)
	if !found {
		t.Fatalf("condition %s is not an entry of the listing after its update:\n%s", conditionID, afterUpdate)
	}
	assertGovernanceConditionStored(t, fromStdin, "", "refs/heads/master", reviewerID, 2)
}

// TestLiveReviewerGroupFlagsExpandAndAccumulate covers the two flag spellings
// and the prefix, against groups a real Bitbucket holds.
//
// Two things are under test and only one of them is bb's arithmetic.
// --reviewer-group and --reviewer-groups are the same flag under two names,
// and pflag tracks "has this been set" per flag -- bound twice, the second
// binding silently discards what was given under the first. The other is that
// each name expands to its members at all, which is what #503 broke for the
// "@reviewer-group/" spelling.
func TestLiveReviewerGroupFlagsExpandAndAccumulate(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	core := liveCodeOwner(t, ctx, harness, seeded.Key, repo.Slug)
	gopher := liveCodeOwner(t, ctx, harness, seeded.Key, repo.Slug)
	if err := harness.createReviewerGroup(ctx, seeded.Key, repo.Slug, "core-team", core.Username); err != nil {
		t.Fatalf("create core-team failed: %v", err)
	}
	if err := harness.createReviewerGroup(ctx, seeded.Key, repo.Slug, "go-team", gopher.Username); err != nil {
		t.Fatalf("create go-team failed: %v", err)
	}

	createWith := func(t *testing.T, branch string, args ...string) []string {
		t.Helper()

		if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, branch+".txt"); err != nil {
			t.Fatalf("push %s failed: %v", branch, err)
		}

		output := mustLiveCLI(t, append([]string{"pr", "create",
			"--from-ref", branch, "--to-ref", "refs/heads/master",
			"--title", branch, "--no-default-reviewers", "--no-codeowners",
		}, args...)...)

		// The stored pull request must hold the reviewers returned here, so the
		// subtests' checks on them are checks on what Bitbucket kept.
		governancePullRequestAsStored(t, output, branch, branch, "master")

		return decodeLivePRReviewers(t, decodeJSONMap(t, output))
	}

	t.Run("both spellings accumulate rather than one discarding the other", func(t *testing.T) {
		names := createWith(t, "feature/flag-aliases",
			"--reviewer-group", "core-team", "--reviewer-groups", "go-team")

		for _, want := range []string{core.Username, gopher.Username} {
			if !containsFold(names, want) {
				t.Errorf("expected %s from one of the two flags, got %v", want, names)
			}
		}
	})

	t.Run("the reviewer-group prefix is accepted by both flags", func(t *testing.T) {
		for index, flags := range [][]string{
			{"--reviewers", "@reviewer-group/core-team"},
			{"--reviewer-group", "reviewer-group/core-team"},
			{"--reviewer-group", "@reviewer-group/core-team"},
		} {
			t.Run(strings.Join(flags, " "), func(t *testing.T) {
				names := createWith(t, fmt.Sprintf("feature/prefix-%d", index), append(flags, "--repo", repoRef)...)

				if !containsFold(names, core.Username) {
					t.Errorf("expected the group to expand to %s, got %v", core.Username, names)
				}
				for _, name := range names {
					if strings.Contains(strings.ToLower(name), "reviewer-group/") {
						t.Errorf("the group token was sent as a username: %v", names)
					}
				}
			})
		}
	})
}
