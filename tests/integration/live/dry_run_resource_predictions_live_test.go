//go:build live

package live_test

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// TestLiveResourceDryRunPredictionsReadRealState is the third of these: the
// previews for branches, branch restrictions, build statuses, required build
// checks, projects, repositories and tags.
//
// Same shape as the other two. Each of these commands asks Bitbucket what it
// currently holds and predicts from the answer, so a fixture standing in for
// Bitbucket makes the prediction a restatement of the fixture. Here the states
// are cheap to reach for real: create the branch, then ask what creating it
// again would do.
func TestLiveResourceDryRunPredictionsReadRealState(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	predicts := func(t *testing.T, want string, args ...string) {
		t.Helper()

		output := mustLiveCLI(t, append([]string{"--dry-run"}, args...)...)
		if !strings.Contains(output, fmt.Sprintf(`"predictedAction": %q`, want)) {
			t.Fatalf("expected %s to predict %q:\n%s", strings.Join(args, " "), want, output)
		}
	}

	t.Run("branches", func(t *testing.T) {
		// master is what the seeded repository already has, so creating it is a
		// conflict and setting it as default is a no-op.
		predicts(t, "conflict", "branch", "create", "master", "--start-point", "master")
		predicts(t, "no-op", "branch", "default", "set", "master")
		predicts(t, "no-op", "branch", "model", "update", "master")
	})

	t.Run("branch restrictions", func(t *testing.T) {
		const matcher = "refs/heads/predicted"

		predicts(t, "no-op", "branch", "restriction", "delete", "999999", "--repo", repoRef)

		created := mustLiveCLI(t, "branch", "restriction", "create", "--repo", repoRef,
			"--type", "read-only", "--matcher-type", "BRANCH", "--matcher-id", matcher)
		restriction, _ := decodeJSONMap(t, created)["restriction"].(map[string]any)
		id, ok := restriction["id"].(float64)
		if !ok {
			t.Fatalf("the created restriction has no id:\n%s", created)
		}
		restrictionID := fmt.Sprintf("%d", int(id))
		assertRestrictionStored(t, restrictionPayload(t, mustLiveCLI(t, "branch", "restriction", "get", restrictionID, "--repo", repoRef)),
			storedRestriction{scope: "REPOSITORY", restrictionType: "read-only", matcherType: "BRANCH", matcherID: matcher})

		predicts(t, "conflict", "branch", "restriction", "create", "--repo", repoRef,
			"--type", "read-only", "--matcher-type", "BRANCH", "--matcher-id", matcher)
		predicts(t, "no-op", "branch", "restriction", "update", restrictionID, "--repo", repoRef,
			"--type", "read-only", "--matcher-type", "BRANCH", "--matcher-id", matcher)
	})

	t.Run("build statuses and required checks", func(t *testing.T) {
		commit := repo.CommitIDs[0]

		predicts(t, "no-op", "build", "required", "delete", "999999", "--repo", repoRef)
		noChecks := mustLiveCLI(t, "build", "required", "list", "--repo", repoRef)
		predicts(t, "create", "build", "required", "create", "--repo", repoRef,
			"--body", `{"buildParentKeys":["ci"],"refMatcher":{"id":"refs/heads/master","type":{"id":"BRANCH"}}}`)
		if after := mustLiveCLI(t, "build", "required", "list", "--repo", repoRef); after != noChecks {
			t.Fatalf("the dry run created the required build check it predicted\nbefore: %s\nafter:  %s", noChecks, after)
		}

		mustLiveCLI(t, "build", "status", "set", commit, "--key", "ci",
			"--state", "SUCCESSFUL", "--url", "http://example.invalid/ci")
		stored := map[string]any{"state": "SUCCESSFUL", "url": "http://example.invalid/ci"}
		commandCoverageAssertFields(t, "the build status", commandCoverageEntry(t, mustLiveCLI(t, "build", "status", "get", commit), "key", "ci"), stored)

		// Another state, so the update it predicts would show if it were made.
		predicts(t, "update", "build", "status", "set", commit, "--key", "ci",
			"--state", "FAILED", "--url", "http://example.invalid/ci")
		commandCoverageAssertFields(t, "the build status after its dry run", commandCoverageEntry(t, mustLiveCLI(t, "build", "status", "get", commit), "key", "ci"), stored)

		created := mustLiveCLI(t, "build", "required", "create", "--repo", repoRef,
			"--body", `{"buildParentKeys":["ci"],"refMatcher":{"id":"refs/heads/master","type":{"id":"BRANCH"}}}`)
		check := decodeJSONMap(t, created)
		id, ok := check["id"].(float64)
		if !ok {
			t.Fatalf("the created required build check has no id:\n%s", created)
		}
		checkID := fmt.Sprintf("%d", int(id))
		commandCoverageAssertRequiredCheck(t, mustLiveCLI(t, "build", "required", "list", "--repo", repoRef), checkID, "ci", "refs/heads/master", "BRANCH")

		// A second build key, which the check would require if the update were
		// made.
		predicts(t, "update", "build", "required", "update", checkID, "--repo", repoRef,
			"--body", `{"buildParentKeys":["ci","lint"],"refMatcher":{"id":"refs/heads/master","type":{"id":"BRANCH"}}}`)
		commandCoverageAssertRequiredCheck(t, mustLiveCLI(t, "build", "required", "list", "--repo", repoRef), checkID, "ci", "refs/heads/master", "BRANCH")
	})

	t.Run("projects and repositories", func(t *testing.T) {
		predicts(t, "conflict", "project", "create", seeded.Key, "--name", "Anything")

		// Deleting a project that is not there is exit 4, not a no-op preview.
		//
		// The mocked test asserted no-op, and the code still carries the branch
		// that would produce it -- but nothing can reach it: the admin preflight
		// runs first and asks Bitbucket whether the caller administers a project
		// that does not exist, which is a 404. The mock made the branch look
		// reachable because its permission lookup answered 200 for every project
		// while only the project itself 404'd.
		output, err := executeLiveCLI(t, "--json", "--dry-run", "project", "delete", "NOSUCHPROJECTKEY", "--yes")
		if err == nil {
			t.Fatalf("expected a missing project to fail, got:\n%s", output)
		}
		if code := apperrors.ExitCode(err); code != 4 {
			t.Errorf("exit code = %d, want 4 for a missing project: %v", code, err)
		}

		// The current name and description, so there is nothing to change.
		current := decodeJSONMap(t, mustLiveCLI(t, "project", "get", seeded.Key))
		project, _ := current["project"].(map[string]any)
		name, _ := project["name"].(string)
		predicts(t, "no-op", "project", "update", seeded.Key, "--name", name)

		predicts(t, "conflict", "repo", "admin", "create", "--project", seeded.Key, "--name", repo.Name)

		forkName := testsupport.UniqueName("forked-in-a-preview-")
		predicts(t, "create", "repo", "admin", "fork", "--repo", repoRef, "--name", forkName)
		assertNoLiveRepositoryNamed(t, forkName, repo.Name)

		predicts(t, "no-op", "repo", "admin", "update", "--repo", repoRef)
	})

	t.Run("tags", func(t *testing.T) {
		predicts(t, "no-op", "tag", "delete", "no-such-tag", "--repo", repoRef)

		mustLiveCLI(t, "tag", "create", "v1", "--repo", repoRef, "--start-point", "master")
		commandCoverageAssertFields(t, "the tag", decodeJSONMap(t, mustLiveCLI(t, "tag", "view", "v1", "--repo", repoRef)),
			map[string]any{"displayId": "v1", "latestCommit": repo.CommitIDs[0]})

		predicts(t, "conflict", "tag", "create", "v1", "--repo", repoRef, "--start-point", "master")

		// #470, against a repository with more tags than one page holds.
		//
		// The preview used to filter a capped listing, so a tag past the cap was
		// predicted as a create and the create then failed. Enough tags to cross
		// a page boundary is what tells a direct lookup from a scan; a
		// repository with one tag cannot, because both find it.
		const beyondAPage = 30
		wantTags := []string{"v1"}
		for index := range beyondAPage {
			name := fmt.Sprintf("v2.0.%d", index)
			mustLiveCLI(t, "tag", "create", name, "--repo", repoRef, "--start-point", "master")
			wantTags = append(wantTags, name)
		}
		// All of them, or the page boundary the next prediction is about is not
		// there to cross.
		if listed := commandCoverageFieldValues(t, mustLiveCLI(t, "tag", "list", "--repo", repoRef, "--limit", "100"), "displayId"); !slices.Equal(listed, slices.Sorted(slices.Values(wantTags))) {
			t.Fatalf("the repository holds tags %v, want %v", listed, slices.Sorted(slices.Values(wantTags)))
		}

		predicts(t, "conflict", "tag", "create", fmt.Sprintf("v2.0.%d", beyondAPage-1),
			"--repo", repoRef, "--start-point", "master")
	})
}

// assertNoLiveRepositoryNamed checks that no repository the caller can see is
// named name, through the instance-wide search.
//
// A fork lands in the caller's own project, and a lookup under a project key
// that was wrong would read as the fork being absent. So the search is asked
// first for controlName, a repository that does exist: an empty answer for name
// is then about the name.
func assertNoLiveRepositoryNamed(t *testing.T, name, controlName string) {
	t.Helper()

	named := func(query string) []any {
		t.Helper()

		values, ok := decodeJSONMap(t, mustLiveCLI(t, "api", "/rest/api/latest/repos?name="+url.QueryEscape(query)))["values"].([]any)
		if !ok {
			t.Fatalf("the repository search for %q answered with no values", query)
		}

		return values
	}

	if control := named(controlName); len(control) != 1 {
		t.Fatalf("the repository search found %d repositories named %q, which exists once", len(control), controlName)
	}
	if found := named(name); len(found) != 0 {
		t.Fatalf("a dry run left repository %q behind: %v", name, found)
	}
}
