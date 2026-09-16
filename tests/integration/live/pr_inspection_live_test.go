//go:build live

package live_test

import (
	"context"
	"slices"
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
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	// The branch is cut from master's head, which is therefore the merge base.
	masterHead := repo.CommitIDs[0]

	// Two commits, each adding a file, so a limit of one has something to cut.
	// No slash in the branch: pushCommitsOnBranch names the files after it.
	const branch = "pr-inspection-live"
	fileNames := []string{branch + "-0.txt", branch + "-1.txt"}
	if err := harness.pushCommitsOnBranch(seeded.Key, repo.Slug, branch, len(fileNames)); err != nil {
		t.Fatalf("push commits on branch failed: %v", err)
	}

	pullRequestID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
	if err != nil {
		t.Fatalf("create pull request failed: %v", err)
	}

	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)
	assertLifecyclePRHarnessStored(t, pullRequestID, branch, "master")
	head := currentLivePRSourceCommit(t, pullRequestID)

	// No --limit on the full listings: 25, the default, has nothing to cut from
	// two. The listings capped at one do.
	commitsOutput, err := executeLiveCLI(t, "--json", "pr", "commits", pullRequestID, "--repo", repoRef)
	if err != nil {
		t.Fatalf("pr commits failed: %v\noutput: %s", err, commitsOutput)
	}
	if !strings.Contains(commitsOutput, "commits") {
		t.Fatalf("expected a commits payload, got: %s", commitsOutput)
	}
	// Exactly the pull request's two, newest first: its head, then the first
	// commit pushed, known by its message because pr commits publishes no
	// parents. The repository's history would carry master's commit as well.
	commits := lifecycleListing(t, commitsOutput, "commits")
	if len(commits) != 2 || commits[0]["id"] != head || commits[1]["message"] != "add "+fileNames[0] {
		t.Fatalf("expected the head %s and the commit adding %s, got: %s", head, fileNames[0], commitsOutput)
	}
	if limited := lifecycleListing(t, mustLiveCLI(t, "pr", "commits", pullRequestID, "--repo", repoRef, "--limit", "1"), "commits"); len(limited) != 1 || limited[0]["id"] != head {
		t.Errorf("--limit 1 returned %v, want only the newest commit %s", limited, head)
	}

	// The seeded file must appear, which is what proves the change listing is
	// scoped to this pull request rather than returning something generic.
	filesOutput, err := executeLiveCLI(t, "--json", "pr", "files", pullRequestID, "--repo", repoRef)
	if err != nil {
		t.Fatalf("pr files failed: %v\noutput: %s", err, filesOutput)
	}
	if !strings.Contains(filesOutput, fileNames[0]) {
		t.Fatalf("expected the seeded file in the change listing, got: %s", filesOutput)
	}
	// Both files, each an addition, and nothing else.
	changes := lifecycleListing(t, filesOutput, "changes")
	added := make([]string, 0, len(changes))
	for _, change := range changes {
		if change["type"] == "ADD" {
			added = append(added, asString(change["path"]))
		}
	}
	slices.Sort(added)
	if len(changes) != len(fileNames) || !slices.Equal(added, fileNames) {
		t.Fatalf("expected %v added, got: %s", fileNames, filesOutput)
	}
	if limited := lifecycleListing(t, mustLiveCLI(t, "pr", "files", pullRequestID, "--repo", repoRef, "--limit", "1"), "changes"); len(limited) != 1 || !slices.Contains(fileNames, asString(limited[0]["path"])) {
		t.Errorf("--limit 1 returned %v, want one of %v", limited, fileNames)
	}

	mergeBaseOutput, err := executeLiveCLI(t, "--json", "pr", "merge-base", pullRequestID, "--repo", repoRef)
	if err != nil {
		t.Fatalf("pr merge-base failed: %v\noutput: %s", err, mergeBaseOutput)
	}
	if mergeBase, _ := decodeJSONMap(t, mergeBaseOutput)["mergeBase"].(map[string]any); mergeBase["id"] != masterHead {
		t.Errorf("expected the merge base %s the branch was cut from, got: %s", masterHead, mergeBaseOutput)
	}

	// Nothing has reported a build on these commits, and an empty answer cannot
	// say which pull request was read; TestLivePullRequestBuildStatuses reads
	// statuses that exist.
	buildStatusOutput, err := executeLiveCLI(t, "--json", "pr", "build", "status", pullRequestID, "--repo", repoRef)
	if err != nil {
		t.Fatalf("pr build status failed: %v\noutput: %s", err, buildStatusOutput)
	}
	if statuses := lifecycleListing(t, buildStatusOutput, "statuses"); len(statuses) != 0 {
		t.Errorf("expected no build statuses, got: %s", buildStatusOutput)
	}

	// No Jira link is configured on the test instance, so the guarantee here is
	// that the command handles an unlinked pull request rather than failing.
	jiraOutput, err := executeLiveCLI(t, "--json", "pr", "jira", pullRequestID, "--repo", repoRef)
	if err != nil {
		t.Fatalf("pr jira failed: %v\noutput: %s", err, jiraOutput)
	}
	if issues := lifecycleListing(t, jiraOutput, "issues"); len(issues) != 0 {
		t.Errorf("expected no linked issues, got: %s", jiraOutput)
	}
}

// lifecycleListing parses the entries of one list out of a command's JSON
// output, failing when the list is not there at all.
func lifecycleListing(t *testing.T, output, key string) []map[string]any {
	t.Helper()

	listed, ok := decodeJSONMap(t, output)[key].([]any)
	if !ok {
		t.Fatalf("no %s list in the output:\n%s", key, output)
	}

	entries := make([]map[string]any, 0, len(listed))
	for _, entry := range listed {
		fields, _ := entry.(map[string]any)
		entries = append(entries, fields)
	}

	return entries
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
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
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

	prID := createLifecyclePR(t, branch, "A rename", "--no-default-reviewers", "--no-codeowners")

	output := mustLiveCLI(t, "pr", "files", prID)
	if !strings.Contains(output, renamed) {
		t.Fatalf("the destination path is missing from pr files:\n%s", output)
	}
	// Both halves matter: a rename rendered without where it came from reads as
	// a new file, and the reviewer loses the history.
	if !strings.Contains(output, original) {
		t.Fatalf("the source path is missing, so the rename reads as an add:\n%s", output)
	}
	// And as one change: a delete of the original beside an add of the new
	// path carries both paths too, and is exactly the case this guards.
	if changes := lifecycleListing(t, output, "changes"); len(changes) != 1 ||
		changes[0]["type"] != "MOVE" || changes[0]["path"] != renamed || changes[0]["srcPath"] != original {
		t.Fatalf("expected one MOVE from %s to %s, got:\n%s", original, renamed, output)
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
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// The key cannot be read back: with no Jira linked, any key answers empty.
	output := mustLiveCLI(t, "commit", "list", "--jira", "NOSUCH-1")
	commits, _ := decodeJSONMap(t, output)["commits"].([]any)
	if len(commits) != 0 {
		t.Fatalf("expected no commits for an issue key with no Jira behind it, got %d:\n%s", len(commits), output)
	}

	// An empty listing has to say so.
	//
	// A caller cannot tell a command that found nothing from one that printed
	// nothing because it broke, and a path no commit touched is the cheapest
	// genuinely empty answer this repository can produce. The path is proven by
	// that emptiness: the repository holds a seeded commit, which a listing
	// that lost the path would print.
	empty := mustLiveHumanCLI(t, "commit", "list", "--path", "no/such/path.txt")
	if !strings.Contains(empty, "No commits found") {
		t.Fatalf("an empty commit listing printed nothing that names the outcome:\n%s", empty)
	}
}
