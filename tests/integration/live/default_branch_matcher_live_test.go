//go:build live

package live_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/compat"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// defaultBranchMatcherSince is the first release with the DEFAULT_BRANCH ref
// matcher: 10.1.5 refused it in a required build with a 400 and failed on it in
// a default reviewer condition with a 500, and 10.2.7 stored both. Stated here
// rather than read from internal/compat, so a boundary set wrong there fails on
// a release.
var defaultBranchMatcherSince = compat.Release{Major: 10, Minor: 2}

// TestLiveDefaultBranchMatcher covers a DEFAULT_BRANCH ref matcher in a
// required build and in a default reviewer condition, on the release under
// test.
//
// From its release both are stored with the matcher, by a create and by an
// update. Before it bb refuses each create, each update and their dry runs as
// unsupported, and the test proves nothing was made or changed.
func TestLiveDefaultBranchMatcher(t *testing.T) {
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
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	reviewerID, err := harness.userID(ctx, harness.username())
	if err != nil {
		t.Fatalf("look up the reviewer's id failed: %v", err)
	}

	const defaultBranch = `{"id":"#","type":{"id":"DEFAULT_BRANCH"}}`
	const onBranch = `{"id":"refs/heads/elsewhere","type":{"id":"BRANCH"}}`
	buildKey := testsupport.UniqueName("default-branch-")
	build := func(refMatcher, exempt string) string {
		body := fmt.Sprintf(`{"buildParentKeys":[%q],"refMatcher":%s`, buildKey, refMatcher)
		if exempt != "" {
			body += `,"exemptRefMatcher":` + exempt
		}
		return body + "}"
	}
	condition := func(target string) string {
		return fmt.Sprintf(`{"sourceMatcher":{"id":"ANY_REF","type":{"id":"ANY_REF"}},"targetMatcher":%s,"reviewers":[{"id":%d}],"requiredApprovals":1}`,
			target, reviewerID)
	}

	// A check and a condition on a plain branch, which every release stores:
	// what the updates act on.
	buildID := createRequiredBuildCheckWithRetry(t, build(onBranch, ""))
	conditionID := conditionIDFrom(t, mustLiveCLI(t, "reviewer", "condition", "create", condition(onBranch), "--repo", repoRef))

	release := harness.release(t)
	if release.Before(defaultBranchMatcherSince) {
		// The command words stay in the literal each row spreads, which is the
		// shape tools/command-reach can read.
		for _, args := range [][]string{
			append([]string{"--json", "--dry-run", "build", "required", "create", "--body"}, build(defaultBranch, "")),
			append([]string{"--json", "build", "required", "create", "--body"}, build(defaultBranch, "")),
			append([]string{"--json", "build", "required", "create", "--body"}, build(onBranch, defaultBranch)),
			append([]string{"--json", "--dry-run", "build", "required", "update"}, buildID, "--body", build(defaultBranch, "")),
			append([]string{"--json", "build", "required", "update"}, buildID, "--body", build(defaultBranch, "")),
			append([]string{"--json", "--dry-run", "reviewer", "condition", "create"}, condition(defaultBranch), "--repo", repoRef),
			append([]string{"--json", "reviewer", "condition", "create"}, condition(defaultBranch), "--repo", repoRef),
			append([]string{"--json", "--dry-run", "reviewer", "condition", "update"}, conditionID, condition(defaultBranch), "--repo", repoRef),
			append([]string{"--json", "reviewer", "condition", "update"}, conditionID, condition(defaultBranch), "--repo", repoRef),
		} {
			output, err := executeLiveCLI(t, args...)
			assertUnsupportedOn(t, release, err, output)
		}

		assertStoredMatchers(t, harness, ctx, seeded.Key, repo.Slug, repoRef,
			map[string]string{buildID: "BRANCH"}, map[string]string{conditionID: "BRANCH"})
		return
	}

	createdBuild := createRequiredBuildCheckWithRetry(t, build(defaultBranch, ""))
	mustLiveCLI(t, "build", "required", "update", buildID, "--body", build(onBranch, defaultBranch))
	createdCondition := conditionIDFrom(t, mustLiveCLI(t, "reviewer", "condition", "create", condition(defaultBranch), "--repo", repoRef))
	mustLiveCLI(t, "reviewer", "condition", "update", conditionID, condition(defaultBranch), "--repo", repoRef)

	assertStoredMatchers(t, harness, ctx, seeded.Key, repo.Slug, repoRef,
		map[string]string{createdBuild: "DEFAULT_BRANCH", buildID: "BRANCH exempt DEFAULT_BRANCH"},
		map[string]string{createdCondition: "DEFAULT_BRANCH", conditionID: "DEFAULT_BRANCH"})
}

// assertStoredMatchers reads back the repository's required builds and
// default reviewer conditions, and checks that exactly these are stored: each
// required build with its matcher, and its exemption after "exempt", and each
// condition with its target matcher.
func assertStoredMatchers(t *testing.T, harness *liveHarness, ctx context.Context, project, slug, repoRef string, builds, conditions map[string]string) {
	t.Helper()

	listed, err := harness.liveJSON(ctx, http.MethodGet,
		fmt.Sprintf("/rest/required-builds/latest/projects/%s/repos/%s/conditions?limit=100", project, slug), nil)
	if err != nil {
		t.Fatalf("read the required builds back: %v", err)
	}
	storedBuilds := map[string]string{}
	values, _ := listed["values"].([]any)
	for _, value := range values {
		check, _ := value.(map[string]any)
		id, _ := numericOrStringID(check["id"])
		described := matcherType(check["refMatcher"])
		if exempt := matcherType(check["exemptRefMatcher"]); exempt != "" {
			described += " exempt " + exempt
		}
		storedBuilds[id] = described
	}
	if fmt.Sprint(storedBuilds) != fmt.Sprint(builds) {
		t.Errorf("required builds stored %v, want %v", storedBuilds, builds)
	}

	storedConditions := map[string]string{}
	listing, _ := decodeJSONMap(t, mustLiveCLI(t, "reviewer", "condition", "list", "--repo", repoRef))["conditions"].([]any)
	for _, entry := range listing {
		condition, _ := entry.(map[string]any)
		if condition["scope"] != "REPOSITORY" {
			continue
		}
		id, _ := numericOrStringID(condition["id"])
		target, _ := condition["targetRefMatcher"].(map[string]any)
		storedConditions[id], _ = target["type"].(string)
	}
	if fmt.Sprint(storedConditions) != fmt.Sprint(conditions) {
		t.Errorf("default reviewer conditions stored %v, want %v", storedConditions, conditions)
	}
}

// matcherType is the type of a matcher as Bitbucket answers it, or "" when
// there is none.
func matcherType(value any) string {
	matcher, _ := value.(map[string]any)
	kind, _ := matcher["type"].(map[string]any)
	id, _ := kind["id"].(string)
	return id
}
