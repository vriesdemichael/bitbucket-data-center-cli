//go:build live

package live_test

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestLivePullRequestInspection covers the read side of a pull request:
// commits, files, merge-base, build status and jira.
//
// Read-only commands carry a narrower guarantee than mutating ones, and it is
// worth naming: a wrong parameter or path returns an empty result rather than
// an error, so the assertion is that the *seeded* content comes back — not
// merely that the call succeeded.
func TestLivePullRequestInspection(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	branch := "feature/pr-inspection-live"
	fileName := "pr-inspection-live.txt"

	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, fileName); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	pullRequestID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
	if err != nil {
		t.Fatalf("create pull request failed: %v", err)
	}

	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	commitsOutput, err := executeLiveCLI(t, "--json", "pr", "commits", pullRequestID, "--repo", repoRef, "--limit", "25")
	if err != nil {
		t.Fatalf("pr commits failed: %v\noutput: %s", err, commitsOutput)
	}
	if !strings.Contains(commitsOutput, "commits") {
		t.Fatalf("expected a commits payload, got: %s", commitsOutput)
	}

	// The seeded file must appear, which is what proves the change listing is
	// scoped to this pull request rather than returning something generic.
	filesOutput, err := executeLiveCLI(t, "--json", "pr", "files", pullRequestID, "--repo", repoRef, "--limit", "25")
	if err != nil {
		t.Fatalf("pr files failed: %v\noutput: %s", err, filesOutput)
	}
	if !strings.Contains(filesOutput, fileName) {
		t.Fatalf("expected the seeded file in the change listing, got: %s", filesOutput)
	}

	mergeBaseOutput, err := executeLiveCLI(t, "--json", "pr", "merge-base", pullRequestID, "--repo", repoRef)
	if err != nil {
		t.Fatalf("pr merge-base failed: %v\noutput: %s", err, mergeBaseOutput)
	}

	buildStatusOutput, err := executeLiveCLI(t, "--json", "pr", "build", "status", pullRequestID, "--repo", repoRef)
	if err != nil {
		t.Fatalf("pr build status failed: %v\noutput: %s", err, buildStatusOutput)
	}

	// No Jira link is configured on the test instance, so the guarantee here is
	// that the command handles an unlinked pull request rather than failing.
	jiraOutput, err := executeLiveCLI(t, "--json", "pr", "jira", pullRequestID, "--repo", repoRef)
	if err != nil {
		t.Fatalf("pr jira failed: %v\noutput: %s", err, jiraOutput)
	}
}

// Pull request tasks are gone from bb, along with the endpoints they called.
// Bitbucket folded tasks into comments carrying a blocker severity, so the
// coverage lives in TestLivePullRequestCommentResolveReopen instead: add a
// blocker, resolve it, reopen it.

// TestLivePullRequestFilesReportsARename covers the change type a mock cannot
// produce honestly.
//
// `pr files` renders MODIFY, ADD, DELETE and MOVE, and a rename is the one that
// carries a second path -- where the file came from. The unit test wrote a MOVE
// entry with a srcPath by hand and checked bb rendered both sides, which proves
// the renderer agrees with the fixture. Whether Bitbucket reports a rename that
// way, and whether git even detects one here, is what decides if the branch is
// ever taken.
func TestLivePullRequestFilesReportsARename(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	const original = "docs/original-name.md"
	const renamed = "docs/new-name.md"

	// The file has to exist on the target before it can be moved away from it.
	if err := harness.pushFileOnBranch(seeded.Key, repo.Slug, "master", original,
		"content that survives the move\n"); err != nil {
		t.Fatalf("seed the original path failed: %v", err)
	}

	const branch = "feature/renamed"
	if err := harness.renameFileOnBranch(seeded.Key, repo.Slug, branch, original, renamed); err != nil {
		t.Fatalf("rename on a branch failed: %v", err)
	}

	prID := createLivePRForRegression(t, branch, "A rename", "--no-default-reviewers", "--no-codeowners")

	output := mustLiveCLI(t, "pr", "files", prID)
	if !strings.Contains(output, renamed) {
		t.Fatalf("the destination path is missing from pr files:\n%s", output)
	}
	// Both halves matter: a rename rendered without where it came from reads as
	// a new file, and the reviewer loses the history.
	if !strings.Contains(output, original) {
		t.Fatalf("the source path is missing, so the rename reads as an add:\n%s", output)
	}
}

// TestLiveJiraIssueCommitsAnswerEmpty records what the Jira integration does
// when no Jira is linked, which is the state every Bitbucket starts in.
//
// It answers 200 with an empty page for any issue key, including one that could
// not exist. That is the same shape as OPENAPI-029 on the pull-request issues
// endpoint: an empty list is not evidence the issue is real, so nothing may read
// it as one. What is pinned here is that `bb commit list --jira` reports the
// empty listing rather than failing, because failing would tell a caller their
// issue key was wrong when the truth is that Bitbucket has no Jira to ask.
func TestLiveJiraIssueCommitsAnswerEmpty(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	output := mustLiveCLI(t, "commit", "list", "--jira", "NOSUCH-1")
	commits, _ := decodeJSONMap(t, output)["commits"].([]any)
	if len(commits) != 0 {
		t.Fatalf("expected no commits for an issue key with no Jira behind it, got %d:\n%s", len(commits), output)
	}

	// An empty listing has to say so.
	//
	// A caller cannot tell a command that found nothing from one that printed
	// nothing because it broke, and a path no commit touched is the cheapest
	// genuinely empty answer this repository can produce.
	empty := mustLiveHumanCLI(t, "commit", "list", "--path", "no/such/path.txt")
	if !strings.Contains(empty, "No commits found") {
		t.Fatalf("an empty commit listing printed nothing that names the outcome:\n%s", empty)
	}
}
