//go:build live

package live_test

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// TestLiveRepoContentCommands covers repo cat, compare, archive and edit —
// the commands that read and write repository content directly.
//
// repo edit is the one worth the seeding: it commits through the API rather
// than through git, so nothing else in the suite exercises that path.
func TestLiveRepoContentCommands(t *testing.T) {
	t.Parallel()

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
	// The harness lists them newest first, and compare's direction is
	// Bitbucket's: the newer commit is the side whose changes are wanted.
	if len(repo.CommitIDs) >= 2 {
		newer, older := repo.CommitIDs[0], repo.CommitIDs[1]

		compareOutput, err := executeLiveCLI(t, "--json", "repo", "compare", newer, older, "--repo", repoRef)
		if err != nil {
			t.Fatalf("repo compare failed: %v\noutput: %s", err, compareOutput)
		}
		if changes, _ := decodeJSONMap(t, compareOutput)["changes"].([]any); len(changes) == 0 {
			t.Fatalf("comparing the newer commit against the older one reported no changes:\n%s", compareOutput)
		}
		backwards := mustLiveCLI(t, "repo", "compare", older, newer, "--repo", repoRef)
		if changes, _ := decodeJSONMap(t, backwards)["changes"].([]any); len(changes) != 0 {
			t.Fatalf("the base-first order reported changes, so the direction is not Bitbucket's:\n%s", backwards)
		}

		// --diff has to produce the patch it promises. It used to read the JSON
		// diff endpoint, whose schema describes a single file, so a whole-repo
		// comparison decoded empty and printed two /dev/null lines -- which a
		// human reads as "the refs are identical" (#587).
		patchOutput, err := executeLiveCLI(t, "--json", "repo", "compare", newer, older, "--repo", repoRef, "--diff")
		if err != nil {
			t.Fatalf("repo compare --diff failed: %v\noutput: %s", err, patchOutput)
		}
		patch, _ := decodeJSONMap(t, patchOutput)["patch"].(string)
		if !strings.Contains(patch, "diff --git") {
			t.Fatalf("expected a unified diff between two commits that differ, got:\n%s", patch)
		}
		// The same direction as the listing. --diff passed compare's refs to the
		// patch endpoint unchanged, which takes them the other way round, so the
		// newer commit's additions came out as deletions.
		if strings.Contains(patch, "+++ /dev/null") {
			t.Fatalf("the patch deletes what the newer commit added, so it runs backwards:\n%s", patch)
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
	// And it is this repository's tree as a tar: gzip around anything else, or
	// around another repository's files, would pass the check above.
	seedContent, _ := decodeJSONMap(t, mustLiveCLI(t, "repo", "cat", "seed.txt", "--repo", repoRef))["content"].(string)
	if archived := repoContentArchivedFile(t, archiveBytes, "seed.txt"); archived != seedContent {
		t.Fatalf("seed.txt in the archive reads %q, the repository's reads %q", archived, seedContent)
	}

	// The edit goes to a branch that does not exist yet, started from master, so
	// each value it sends has a read that tells it apart from a default: a new
	// branch rather than the default one, cut from the branch named, carrying the
	// message and the content given.
	const editedBranch = "feature/live-edit"
	masterTip := nestedJSONMap(t, mustLiveCLI(t, "commit", "get", "master", "--repo", repoRef), "commit")["id"]
	editOutput, err := executeLiveCLI(t, "--json", "repo", "edit", "live-edit.txt",
		"--content", "written by the live suite\n",
		"--message", "live suite edit",
		"--branch", editedBranch,
		"--source-branch", "master",
		"--repo", repoRef)
	if err != nil {
		t.Fatalf("repo edit failed: %v\noutput: %s", err, editOutput)
	}

	// Read it back: an edit that reports success and commits nothing is the
	// failure this catches.
	if content := repoContentFileAt(t, repoRef, "live-edit.txt", editedBranch); content != "written by the live suite\n" {
		t.Fatalf("live-edit.txt on %s reads %q after the edit", editedBranch, content)
	}
	edited := nestedJSONMap(t, mustLiveCLI(t, "commit", "get", editedBranch, "--repo", repoRef), "commit")
	if edited["message"] != "live suite edit" {
		t.Errorf("the edit's commit message is %v, want %q", edited["message"], "live suite edit")
	}
	if parents, _ := edited["parents"].([]any); len(parents) != 1 || parents[0] != masterTip {
		t.Errorf("the edit's commit has parents %v, want [%v], master's tip", edited["parents"], masterTip)
	}
	// Only there: master never had the file.
	if _, err := executeLiveCLI(t, "--json", "repo", "cat", "live-edit.txt", "--at", "master", "--repo", repoRef); !apperrors.IsKind(err, apperrors.KindNotFound) {
		t.Errorf("live-edit.txt on master: want not found, got %v", err)
	}

	// --source-commit is the version the edit is made against. Behind the file,
	// the edit has to be refused and change nothing; at the branch tip it lands.
	if output, err := executeLiveCLI(t, "--json", "repo", "edit", "live-edit.txt",
		"--content", "not stored\n", "--message", "stale edit", "--branch", editedBranch,
		"--source-commit", asString(masterTip), "--repo", repoRef); err == nil {
		t.Fatalf("an edit against a commit that predates the file succeeded:\n%s", output)
	}
	if content := repoContentFileAt(t, repoRef, "live-edit.txt", editedBranch); content != "written by the live suite\n" {
		t.Fatalf("a refused edit changed live-edit.txt to %q", content)
	}
	mustLiveCLI(t, "repo", "edit", "live-edit.txt", "--content", "edited again\n", "--message", "current edit",
		"--branch", editedBranch, "--source-commit", asString(edited["id"]), "--repo", repoRef)
	if content := repoContentFileAt(t, repoRef, "live-edit.txt", editedBranch); content != "edited again\n" {
		t.Fatalf("an edit against the branch tip left live-edit.txt as %q", content)
	}
}

// repoContentFileAt reads a file's content at a ref through bb repo cat.
func repoContentFileAt(t *testing.T, repoRef, path, ref string) string {
	t.Helper()

	content, ok := decodeJSONMap(t, mustLiveCLI(t, "repo", "cat", path, "--at", ref, "--repo", repoRef))["content"].(string)
	if !ok {
		t.Fatalf("repo cat %s --at %s returned no content", path, ref)
	}
	return content
}

// repoContentArchivedFile reads one file out of a gzipped tar archive.
func repoContentArchivedFile(t *testing.T, archive []byte, name string) string {
	t.Helper()

	unzipped, err := gzip.NewReader(strings.NewReader(string(archive)))
	if err != nil {
		t.Fatalf("the archive is not gzip: %v", err)
	}
	reader := tar.NewReader(unzipped)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			t.Fatalf("no %s in the archive", name)
		}
		if err != nil {
			t.Fatalf("the archive is not a tar: %v", err)
		}
		if strings.TrimPrefix(header.Name, "./") != name {
			continue
		}
		content, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("read %s from the archive: %v", name, err)
		}
		return string(content)
	}
}

// TestLiveRepoDefaultTaskLifecycle covers repo default-task add, list, update
// and delete.
//
// These are the default-tasks endpoints, which are unrelated to the removed
// pull request task API — see #386. They exist and work.
func TestLiveRepoDefaultTaskLifecycle(t *testing.T) {
	t.Parallel()

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
			_, _ = executeLiveCLI(t, "--json", "repo", "default-task", "delete", anyRefID, "--repo", repoRef, "--yes")
		})
	}

	// The replies above are the writes' own; the listing is what was stored.
	stored := repoContentDefaultTask(t, repoRef, taskID)
	if stored["description"] != "live suite default task" {
		t.Errorf("task %s is stored as %q", taskID, stored["description"])
	}
	assertMatcherID(t, stored, "sourceMatcher", "refs/heads/feature/*")
	assertMatcherID(t, stored, "targetMatcher", "refs/heads/master")
	if anyRefID, ok := numericOrStringID(anyRefData["id"]); ok {
		anyRef := repoContentDefaultTask(t, repoRef, anyRefID)
		assertMatcherID(t, anyRef, "sourceMatcher", "ANY_REF_MATCHER_ID")
		assertMatcherID(t, anyRef, "targetMatcher", "ANY_REF_MATCHER_ID")
	}

	if _, err := executeLiveCLI(t, "--json", "repo", "default-task", "update", taskID,
		"--description", "live suite default task updated", "--repo", repoRef); err != nil {
		t.Fatalf("repo default-task update failed: %v", err)
	}

	// The description changed and the matchers the update did not name stayed.
	updated := repoContentDefaultTask(t, repoRef, taskID)
	if updated["description"] != "live suite default task updated" {
		t.Errorf("task %s is stored as %q after the update", taskID, updated["description"])
	}
	assertMatcherID(t, updated, "sourceMatcher", "refs/heads/feature/*")
	assertMatcherID(t, updated, "targetMatcher", "refs/heads/master")

	if _, err := executeLiveCLI(t, "--json", "repo", "default-task", "delete", taskID, "--repo", repoRef, "--yes"); err != nil {
		t.Fatalf("repo default-task delete failed: %v", err)
	}
	var remaining any
	if err := decodeJSONEnvelopeData(mustLiveCLI(t, "repo", "default-task", "list", "--repo", repoRef), &remaining); err != nil {
		t.Fatalf("repo default-task list returned invalid JSON: %v", err)
	}
	if _, found := findByID(remaining, taskID); found {
		t.Errorf("task %s is still listed after its delete", taskID)
	}
}

// repoContentDefaultTask reads one default task back from the repository's
// listing.
func repoContentDefaultTask(t *testing.T, repoRef, id string) map[string]any {
	t.Helper()

	var listed any
	listing := mustLiveCLI(t, "repo", "default-task", "list", "--repo", repoRef)
	if err := decodeJSONEnvelopeData(listing, &listed); err != nil {
		t.Fatalf("repo default-task list returned invalid JSON: %v\n%s", err, listing)
	}
	task, found := findByID(listed, id)
	if !found {
		t.Fatalf("task %s is not in the listing: %s", id, listing)
	}
	return task
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
