//go:build live

package live_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/compat"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// requiredBuildScopeSince is the first release that stores a required build's
// scope: 10.1.5 answered 200 to both fields and dropped them, 10.2.7 stored
// them. Stated here rather than read from internal/compat, so that a boundary
// set wrong there fails on a release instead of agreeing with itself.
var requiredBuildScopeSince = compat.Release{Major: 10, Minor: 2}

// TestLiveRequiredBuildScope checks what a required build's scope does on the
// release under test.
//
// From 10.2 a build required for the merge queue alone is stored with that
// scope and leaves pull requests mergeable. An earlier release answers 200 to
// both fields and drops them, so the build would block every pull request while
// bb printed the scope that was asked for. bb refuses it there instead, and the
// test proves both halves: the refusal, and that nothing was made.
//
// Either way a check created without a scope reads back as applying to pull
// requests, as every release enforces it. From 10.2 Bitbucket applies it to the
// merge queue as well; before it there is no merge queue, and bb reports that
// rather than the absent fields as false.
func TestLiveRequiredBuildScope(t *testing.T) {
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

	release := harness.release(t)

	// A check on a branch nothing below targets: one on master applying to pull
	// requests would block the pull request opened there.
	plainKey := testsupport.UniqueName("unscoped-")
	plainID := createRequiredBuildCheckWithRetry(t,
		fmt.Sprintf(`{"buildParentKeys":[%q],"refMatcher":{"id":"refs/heads/unscoped","type":{"id":"BRANCH"}}}`, plainKey))
	// From 10.2 Bitbucket applies it to the merge queue too, and says so. Before
	// it there is no merge queue, and bb says that.
	forMergeQueue := !release.Before(requiredBuildScopeSince)

	t.Run("a check without a scope applies to pull requests", func(t *testing.T) {
		assertRequiredBuildScope(t, "build required list", mustLiveCLI(t, "build", "required", "list"), plainID, true, forMergeQueue)
		assertRequiredBuildScope(t, "repo settings pull-requests merge-checks list",
			mustLiveCLI(t, "repo", "settings", "pull-requests", "merge-checks", "list", "--repo", repoRef), plainID, true, forMergeQueue)
	})

	buildKey := testsupport.UniqueName("merge-queue-only-")
	body := fmt.Sprintf(`{"buildParentKeys":[%q],"refMatcher":{"id":"refs/heads/master","type":{"id":"BRANCH"}},"requiredForPullRequest":false,"requiredForMergeQueue":true}`, buildKey)

	if release.Before(requiredBuildScopeSince) {
		t.Run("a release without the scope refuses it and changes nothing", func(t *testing.T) {
			// The update carries the same scope against the check that exists,
			// so a refusal that came too late would be visible on it.
			update := fmt.Sprintf(`{"buildParentKeys":[%q],"refMatcher":{"id":"refs/heads/unscoped","type":{"id":"BRANCH"}},"requiredForPullRequest":false,"requiredForMergeQueue":true}`, plainKey)
			// The command words stay in the literal each row spreads, which is
			// the shape tools/command-reach can read.
			for _, args := range [][]string{
				append([]string{"--json", "--dry-run", "build", "required", "create", "--body"}, body),
				append([]string{"--json", "build", "required", "create", "--body"}, body),
				append([]string{"--json", "--dry-run", "build", "required", "update"}, plainID, "--body", update),
				append([]string{"--json", "build", "required", "update"}, plainID, "--body", update),
			} {
				output, err := executeLiveCLI(t, args...)
				assertUnsupportedOn(t, release, err, output)
			}

			if listed := mustLiveCLI(t, "build", "required", "list"); strings.Contains(listed, buildKey) {
				t.Fatalf("a refused create left a check for %s behind:\n%s", buildKey, listed)
			}
			assertRequiredBuildScope(t, "build required list", mustLiveCLI(t, "build", "required", "list"), plainID, true, false)
		})

		return
	}

	requiredID := createRequiredBuildCheckWithRetry(t, body)
	assertRequiredBuildScope(t, "build required list", mustLiveCLI(t, "build", "required", "list"), requiredID, false, true)

	const branch = "feature/merge-queue-only"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "merge-queue-only.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}
	id := createLivePRForRegression(t, branch, "Merge queue only", "--no-default-reviewers", "--no-codeowners")

	// No build has reported on the branch, so a build required on pull requests
	// would veto the merge. One required only for the merge queue must not.
	if mergeable, outcome := livePRMergeability(t, id); !mergeable {
		t.Errorf("a build required only for the merge queue blocked a pull request, outcome=%q", outcome)
	}
	if human := mustLiveHumanCLI(t, "pr", "get", id); strings.Contains(human, "Merge blockers:") {
		t.Errorf("a build required only for the merge queue is named as a merge blocker:\n%s", human)
	}
}

// assertRequiredBuildScope reads one check's scope out of a listing of required
// builds.
func assertRequiredBuildScope(t *testing.T, listing, output, id string, forPullRequest, forMergeQueue bool) {
	t.Helper()

	var listed any
	if err := decodeJSONEnvelopeData(output, &listed); err != nil {
		t.Fatalf("%s returned invalid JSON: %v\n%s", listing, err, output)
	}
	check, ok := findByID(listed, id)
	if !ok {
		t.Fatalf("check %s is not in %s:\n%s", id, listing, output)
	}
	if check["requiredForPullRequest"] != forPullRequest || check["requiredForMergeQueue"] != forMergeQueue {
		t.Errorf("%s reads check %s as requiredForPullRequest %v, requiredForMergeQueue %v; want %t, %t",
			listing, id, check["requiredForPullRequest"], check["requiredForMergeQueue"], forPullRequest, forMergeQueue)
	}
}

// assertUnsupportedOn checks a command was refused as unsupported by the
// release under test, in the words a caller reads.
func assertUnsupportedOn(t *testing.T, release compat.Release, err error, output string) {
	t.Helper()

	if code := apperrors.ExitCode(err); code != 14 {
		t.Fatalf("exit %d on %s, want 14 (unsupported): %v\n%s", code, release, err, output)
	}
	if message := err.Error(); !strings.Contains(message, "not supported by this Bitbucket version ("+release.String()+")") {
		t.Errorf("the refusal does not name the release it was refused on: %v", err)
	}
}

// findByID walks decoded JSON for the object whose id is the one given.
func findByID(value any, id string) (map[string]any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		if candidate, ok := numericOrStringID(typed["id"]); ok && candidate == id {
			return typed, true
		}
		for _, child := range typed {
			if found, ok := findByID(child, id); ok {
				return found, true
			}
		}
	case []any:
		for _, child := range typed {
			if found, ok := findByID(child, id); ok {
				return found, true
			}
		}
	}
	return nil, false
}
