//go:build live

package live_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLiveRepoContentCommands covers repo cat, compare, archive and edit —
// the commands that read and write repository content directly.
//
// repo edit is the one worth the seeding: it commits through the API rather
// than through git, so nothing else in the suite exercises that path.
func TestLiveRepoContentCommands(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{Commits: 2, WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// The seeded repositories carry seed.txt, which is what makes cat an
	// assertion about content rather than about the call succeeding.
	catOutput, err := executeLiveCLI(t, "repo", "cat", "seed.txt", "--repo", repoRef)
	if err != nil {
		t.Fatalf("repo cat failed: %v\noutput: %s", err, catOutput)
	}
	if strings.TrimSpace(catOutput) == "" {
		t.Fatalf("expected file content from repo cat, got nothing")
	}

	// Two seeded commits, so a comparison between them has something to report.
	if len(repo.CommitIDs) >= 2 {
		compareOutput, err := executeLiveCLI(t, "--json", "repo", "compare", repo.CommitIDs[1], repo.CommitIDs[0], "--repo", repoRef)
		if err != nil {
			t.Fatalf("repo compare failed: %v\noutput: %s", err, compareOutput)
		}
	}

	// A commit compared with itself: no changes, and no error either. A unit
	// test asserted this against a handwritten empty page, which cannot tell an
	// empty comparison from a comparison that was never made.
	sameOutput, err := executeLiveCLI(t, "--json", "repo", "compare", repo.CommitIDs[0], repo.CommitIDs[0], "--repo", repoRef)
	if err != nil {
		t.Fatalf("repo compare of a commit with itself failed: %v\noutput: %s", err, sameOutput)
	}
	if changes, _ := decodeJSONMap(t, sameOutput)["changes"].([]any); len(changes) != 0 {
		t.Fatalf("a commit compared with itself reported %d changes:\n%s", len(changes), sameOutput)
	}

	// Without -o the command writes <slug>.<format> into the working directory,
	// which for a test is the package directory. Naming the path keeps the
	// archive in the temp directory and gives the test something to open.
	archivePath := filepath.Join(t.TempDir(), "archive.tar.gz")
	archiveOutput, err := executeLiveCLI(t, "repo", "archive", "--repo", repoRef, "--format", "tar.gz", "-o", archivePath)
	if err != nil {
		t.Fatalf("repo archive failed: %v\noutput: %s", err, archiveOutput)
	}

	archiveBytes, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("expected an archive at %s: %v", archivePath, err)
	}
	// A gzip member starts 1f 8b. An empty or truncated stream written to the
	// right filename would otherwise pass for an archive.
	if len(archiveBytes) < 2 || archiveBytes[0] != 0x1f || archiveBytes[1] != 0x8b {
		t.Fatalf("expected a gzip archive, got %d bytes starting %x", len(archiveBytes), archiveBytes[:min(2, len(archiveBytes))])
	}

	editOutput, err := executeLiveCLI(t, "--json", "repo", "edit", "live-edit.txt",
		"--content", "written by the live suite\n",
		"--message", "live suite edit",
		"--branch", "master",
		"--repo", repoRef)
	if err != nil {
		t.Fatalf("repo edit failed: %v\noutput: %s", err, editOutput)
	}

	// Read it back: an edit that reports success and commits nothing is the
	// failure this catches.
	afterEdit, err := executeLiveCLI(t, "repo", "cat", "live-edit.txt", "--repo", repoRef)
	if err != nil {
		t.Fatalf("repo cat after edit failed: %v\noutput: %s", err, afterEdit)
	}
	if !strings.Contains(afterEdit, "written by the live suite") {
		t.Fatalf("expected the edited content to be readable, got: %s", afterEdit)
	}
}

// TestLiveRepoDefaultTaskLifecycle covers repo default-task add, list, update
// and delete.
//
// These are the default-tasks endpoints, which are unrelated to the removed
// pull request task API — see #386. They exist and work.
func TestLiveRepoDefaultTaskLifecycle(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	addOutput, err := executeLiveCLI(t, "--json", "repo", "default-task", "add", "live suite default task",
		"--source-ref", "refs/heads/feature/*", "--target-ref", "refs/heads/master", "--repo", repoRef)
	if err != nil {
		t.Fatalf("repo default-task add failed: %v\noutput: %s", err, addOutput)
	}

	taskID, ok := numericOrStringID(nestedJSONMap(t, addOutput, "task")["id"])
	if !ok {
		t.Fatalf("expected a task id in the add output: %s", addOutput)
	}

	// The matcher the server actually stored, echoed back. Both matchers were
	// sent with a type id that is not in the schema enum until recently, so the
	// request never got as far as creating anything.
	addData := nestedJSONMap(t, addOutput, "task")
	assertMatcherID(t, addData, "sourceMatcher", "refs/heads/feature/*")
	assertMatcherID(t, addData, "targetMatcher", "refs/heads/master")

	// Both matcher flags are optional on the command but the fields are not
	// optional on the API, so omitting them has to mean "any ref" rather than
	// "leave them out".
	anyRefOutput, err := executeLiveCLI(t, "--json", "repo", "default-task", "add", "live suite any-ref task", "--repo", repoRef)
	if err != nil {
		t.Fatalf("repo default-task add without matchers failed: %v\noutput: %s", err, anyRefOutput)
	}
	anyRefData := nestedJSONMap(t, anyRefOutput, "task")
	assertMatcherID(t, anyRefData, "sourceMatcher", "ANY_REF_MATCHER_ID")
	assertMatcherID(t, anyRefData, "targetMatcher", "ANY_REF_MATCHER_ID")
	if anyRefID, ok := numericOrStringID(anyRefData["id"]); ok {
		t.Cleanup(func() {
			_, _ = executeLiveCLI(t, "--json", "repo", "default-task", "delete", anyRefID, "--repo", repoRef)
		})
	}

	listOutput, err := executeLiveCLI(t, "--json", "repo", "default-task", "list", "--repo", repoRef)
	if err != nil {
		t.Fatalf("repo default-task list failed: %v\noutput: %s", err, listOutput)
	}
	if !strings.Contains(listOutput, "live suite default task") {
		t.Fatalf("expected the added task in the listing, got: %s", listOutput)
	}

	if _, err := executeLiveCLI(t, "--json", "repo", "default-task", "update", taskID,
		"--description", "live suite default task updated", "--repo", repoRef); err != nil {
		t.Fatalf("repo default-task update failed: %v", err)
	}

	afterUpdate, err := executeLiveCLI(t, "--json", "repo", "default-task", "list", "--repo", repoRef)
	if err != nil {
		t.Fatalf("repo default-task list after update failed: %v\noutput: %s", err, afterUpdate)
	}
	if !strings.Contains(afterUpdate, "live suite default task updated") {
		t.Fatalf("expected the update to persist, got: %s", afterUpdate)
	}

	if _, err := executeLiveCLI(t, "--json", "repo", "default-task", "delete", taskID, "--repo", repoRef); err != nil {
		t.Fatalf("repo default-task delete failed: %v", err)
	}
}

// assertMatcherID checks the id of a matcher on a default-task payload. The id
// is the only part of the matcher bb surfaces, and it is enough: the server
// rewrites an any-ref matcher's id to ANY_REF_MATCHER_ID, so seeing it back
// proves the ANY_REF type was accepted rather than merely echoed.
func assertMatcherID(t *testing.T, payload map[string]any, field string, want string) {
	t.Helper()
	matcher, ok := payload[field].(map[string]any)
	if !ok {
		t.Fatalf("expected a %s in the payload, got: %v", field, payload)
	}
	if got := asString(matcher["id"]); got != want {
		t.Fatalf("%s id = %q, want %q", field, got, want)
	}
}
