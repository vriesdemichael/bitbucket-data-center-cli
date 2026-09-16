//go:build live

package live_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// TestLiveRequiredBuildScope checks that a required build scoped to the merge
// queue alone is stored with that scope, and leaves pull requests mergeable.
//
// The scope arrived with merge queues. A server from before them answers 200
// to both fields and drops them (observed on 10.0.2), so the build is required
// on every pull request -- while bb, which turns the absent fields into false,
// still prints requiredForPullRequest=false, exactly what was asked for.
// Reading the check back through bb cannot see that half of the drop, so the
// test also asks Bitbucket whether the build blocks a pull request.
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
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	buildKey := testsupport.UniqueName("merge-queue-only-")
	body := fmt.Sprintf(`{"buildParentKeys":[%q],"refMatcher":{"id":"refs/heads/master","type":{"id":"BRANCH"}},"requiredForPullRequest":false,"requiredForMergeQueue":true}`, buildKey)
	requiredID := createRequiredBuildCheckWithRetry(t, body)

	var listed any
	if err := decodeJSONEnvelopeData(mustLiveCLI(t, "build", "required", "list"), &listed); err != nil {
		t.Fatalf("build required list returned invalid JSON: %v", err)
	}
	check, ok := findByID(listed, requiredID)
	if !ok {
		t.Fatalf("the check just created (id %s) is not in the list: %v", requiredID, listed)
	}
	if check["requiredForPullRequest"] != false || check["requiredForMergeQueue"] != true {
		t.Errorf("stored scope = requiredForPullRequest %v, requiredForMergeQueue %v; want false, true",
			check["requiredForPullRequest"], check["requiredForMergeQueue"])
	}

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
