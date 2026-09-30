//go:build live

package live_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// livePullRequest is the part of a reported pull request these tests read.
type livePullRequest struct {
	PullRequest struct {
		ID          int64  `json:"id"`
		State       string `json:"state"`
		URL         string `json:"url"`
		CreatedDate int64  `json:"createdDate"`
		ClosedDate  int64  `json:"closedDate"`
	} `json:"pullRequest"`
}

// A pull request says where it is (#705). pr create prints the link, which is
// what a person wants next, and every reported pull request carries it as
// Bitbucket links to it. closedDate is there once it is merged and not before.
func TestLivePullRequestCarriesItsLinkAndClosedDate(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, "feature/link", "link.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	created, err := executeLiveCLI(t, "pr", "create", "--from-ref", "feature/link", "--to-ref", "master", "--title", "A pull request with a link")
	if err != nil {
		t.Fatalf("pr create failed: %v\n%s", err, created)
	}
	lines := strings.Split(strings.TrimSpace(created), "\n")
	link := strings.TrimSpace(lines[len(lines)-1])
	path := fmt.Sprintf("/projects/%s/repos/%s/pull-requests/", seeded.Key, repo.Slug)
	if !strings.HasPrefix(link, "http") || !strings.Contains(link, path) {
		t.Fatalf("pr create did not print the pull request's link on its last line: %q", created)
	}
	id := link[strings.LastIndex(link, "/")+1:]

	var open livePullRequest
	decodeJSONData(t, mustLiveCLI(t, "pr", "get", id, "--no-review-summary"), &open)
	if open.PullRequest.URL != link {
		t.Fatalf("pr get reports url %q, and pr create printed %q", open.PullRequest.URL, link)
	}
	if open.PullRequest.ClosedDate != 0 {
		t.Fatalf("an open pull request reports closedDate %d", open.PullRequest.ClosedDate)
	}
	if human, err := executeLiveCLI(t, "pr", "get", id, "--no-review-summary"); err != nil || !strings.Contains(human, "URL: "+link) {
		t.Fatalf("pr get does not show the link: %v\n%s", err, human)
	}

	var merged livePullRequest
	decodeJSONData(t, mustLiveCLI(t, "pr", "merge", id), &merged)
	if merged.PullRequest.State != "MERGED" || merged.PullRequest.ClosedDate < merged.PullRequest.CreatedDate || merged.PullRequest.ClosedDate == 0 {
		t.Fatalf("a merged pull request reports state %s, createdDate %d, closedDate %d",
			merged.PullRequest.State, merged.PullRequest.CreatedDate, merged.PullRequest.ClosedDate)
	}
}

// pr merge --strategy merges the way it was asked to (#705), which Bitbucket
// shows in the commit it leaves on the target: a squash has one parent where
// the default, no-ff, leaves a merge commit with two. A strategy the
// repository has not enabled is refused, and the dry run says so first.
func TestLivePullRequestMergeStrategy(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	open := func(branch string) string {
		t.Helper()
		if err := harness.pushCommitsOnBranch(seeded.Key, repo.Slug, branch, 2); err != nil {
			t.Fatalf("push commits on %s failed: %v", branch, err)
		}
		var created livePullRequest
		decodeJSONData(t, mustLiveCLI(t, "pr", "create", "--from-ref", branch, "--to-ref", "master", "--title", "Merge "+branch), &created)

		return fmt.Sprintf("%d", created.PullRequest.ID)
	}
	parentsOfTargetHead := func() int {
		t.Helper()
		commits, err := harness.liveJSON(ctx, "GET", fmt.Sprintf("/rest/api/1.0/projects/%s/repos/%s/commits?until=refs/heads/master&limit=1", seeded.Key, repo.Slug), nil)
		if err != nil {
			t.Fatalf("read the target's head commit: %v", err)
		}
		values, _ := commits["values"].([]any)
		if len(values) != 1 {
			t.Fatalf("expected the target's head commit, got %v", commits)
		}
		head, _ := values[0].(map[string]any)
		parents, _ := head["parents"].([]any)

		return len(parents)
	}

	// A new repository enables no-ff and nothing else, so squash is refused:
	// by the dry run, and by Bitbucket with the same kind of error.
	squashed := open("squash-me")
	liveVerdictHolds(t, apperrors.KindValidation, "pr", "merge", squashed, "--strategy", "squash")

	settings := fmt.Sprintf("/rest/api/1.0/projects/%s/repos/%s/settings/pull-requests", seeded.Key, repo.Slug)
	if _, err := harness.liveJSON(ctx, "POST", settings, map[string]any{"mergeConfig": map[string]any{
		"defaultStrategy": map[string]any{"id": "no-ff"},
		"strategies":      []map[string]any{{"id": "no-ff"}, {"id": "squash"}},
	}}); err != nil {
		t.Fatalf("enable the squash strategy: %v", err)
	}

	liveGoesThroughAsPredicted(t, jsonoutput.OutcomeWouldApply, "will be merged", "pr", "merge", squashed, "--strategy", "squash")
	if parents := parentsOfTargetHead(); parents != 1 {
		t.Fatalf("a squash merge left a commit with %d parents on the target, want 1", parents)
	}

	// No --strategy is the repository's default, which is still no-ff.
	mustLiveCLI(t, "pr", "merge", open("default-merge"))
	if parents := parentsOfTargetHead(); parents != 2 {
		t.Fatalf("a merge with the default strategy left a commit with %d parents on the target, want 2", parents)
	}
}
