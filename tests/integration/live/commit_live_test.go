//go:build live

package live_test

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

func TestLiveCLICommitAndRefLifecycle(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{Commits: 3, WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// Commit list
	listOutput, err := executeLiveCLI(t, "--json", "commit", "list", "--limit", "2")
	if err != nil {
		t.Fatalf("commit list failed: %v\noutput: %s", err, listOutput)
	}
	listPayload := decodeJSONMap(t, listOutput)
	commitsList, ok := listPayload["commits"].([]any)
	if !ok || len(commitsList) == 0 {
		t.Fatalf("expected commits list in output, got: %s", listOutput)
	}
	// Three commits are seeded, so a limit of two has one to leave out, and the
	// two it keeps are the newest.
	if got := commitLiveIDs(commitsList); !slices.Equal(got, repo.CommitIDs[:2]) {
		t.Errorf("commit list --limit 2 returned %v, want the two newest %v", got, repo.CommitIDs[:2])
	}

	commitID := repo.CommitIDs[0]

	// Commit get
	getOutput, err := executeLiveCLI(t, "commit", "get", commitID)
	if err != nil {
		t.Fatalf("commit get failed: %v\noutput: %s", err, getOutput)
	}
	if !strings.Contains(getOutput, commitID) {
		t.Fatalf("expected commit id in get output, got: %s", getOutput)
	}
	// The id is what the request was addressed by, so it is the message and the
	// parent that show the commit that came back is that one.
	fetched := nestedJSONMap(t, mustLiveCLI(t, "commit", "get", commitID), "commit")
	wantMessage := fmt.Sprintf("seed commit 3 for %s/%s", seeded.Key, repo.Slug)
	if fetched["id"] != commitID || fetched["message"] != wantMessage || !slices.Equal(commitLiveIDs(fetched["parents"]), repo.CommitIDs[1:2]) {
		t.Errorf("commit get %s returned id %v, message %v, parents %v; want message %q and parent %s",
			commitID, fetched["id"], fetched["message"], fetched["parents"], wantMessage, repo.CommitIDs[1])
	}

	// Commit compare
	//
	// From the middle commit to the oldest, which leaves exactly the middle one.
	// The head of master would not do as from: the endpoint puts the default
	// branch in place of a from it did not get, so dropping it changed nothing.
	// Without the to, the answer is empty.
	from := repo.CommitIDs[1]
	to := repo.CommitIDs[len(repo.CommitIDs)-1]
	compareOutput, err := executeLiveCLI(t, "--json", "commit", "compare", from, to)
	if err != nil {
		t.Fatalf("commit compare failed: %v\noutput: %s", err, compareOutput)
	}
	comparePayload := decodeJSONMap(t, compareOutput)
	compareList, ok := comparePayload["commits"].([]any)
	if !ok || len(compareList) == 0 {
		t.Fatalf("expected commits compare list in output, got: %s", compareOutput)
	}
	if got := commitLiveIDs(compareList); !slices.Equal(got, []string{from}) {
		t.Errorf("commit compare %s %s returned %v, want only %s", from, to, got, from)
	}

	// Ref list
	refListOutput, err := executeLiveCLI(t, "ref", "list")
	if err != nil {
		t.Fatalf("ref list failed: %v\noutput: %s", err, refListOutput)
	}
	if !strings.Contains(refListOutput, "master") && !strings.Contains(refListOutput, "main") {
		t.Fatalf("expected master/main in ref list output, got: %s", refListOutput)
	}
	// The seeded repository has master and nothing else.
	refs := decodeJSONMap(t, mustLiveCLI(t, "ref", "list"))["refs"]
	if want := []any{map[string]any{"id": "refs/heads/master", "displayId": "master", "type": "BRANCH"}}; !reflect.DeepEqual(refs, want) {
		t.Errorf("ref list returned %v, want master alone", refs)
	}

	// Ref resolve
	//
	// The name goes to Bitbucket as a filter, and bb picks the exact match out
	// of the first fifty branches that come back. With master among the first
	// fifty, a filter the server ignored would find it all the same, so fifty
	// branches that list ahead of master go in first.
	if err := harness.pushRefResolveDecoys(seeded.Key, repo.Slug, 50); err != nil {
		t.Fatalf("push the decoy branches failed: %v", err)
	}

	resolveOutput, err := executeLiveCLI(t, "--json", "ref", "resolve", "master")
	if err != nil {
		t.Fatalf("ref resolve failed: %v\noutput: %s", err, resolveOutput)
	}
	resolvePayload := decodeJSONMap(t, resolveOutput)
	refObj, ok := resolvePayload["ref"].(map[string]any)
	if !ok || asString(refObj["displayId"]) != "master" {
		t.Fatalf("expected ref object in resolve output, got: %s", resolveOutput)
	}
	// bb only returns a ref whose name is the one asked for; the rest is the
	// server's.
	if refObj["id"] != "refs/heads/master" || refObj["type"] != "BRANCH" {
		t.Errorf("ref resolve master returned id %v, type %v; want refs/heads/master, BRANCH", refObj["id"], refObj["type"])
	}

	// Not found ref
	_, errNotFound := executeLiveCLI(t, "ref", "resolve", "nonexistent-ref-abc")
	if errNotFound == nil {
		t.Fatalf("expected error when resolving non-existent ref")
	}
	if code := apperrors.ExitCode(errNotFound); code != 4 {
		t.Errorf("resolving a ref that does not exist exited %d, want 4 (not found): %v", code, errNotFound)
	}
}

// commitLiveIDs reads commit ids out of decoded JSON: a list of commits, each
// with an id, or a list of the ids themselves.
func commitLiveIDs(value any) []string {
	entries, _ := value.([]any)
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if commit, ok := entry.(map[string]any); ok {
			entry = commit["id"]
		}
		ids = append(ids, asString(entry))
	}

	return ids
}

// pushRefResolveDecoys puts count branches on an empty commit on top of master,
// in one push, so that an unfiltered branch listing reaches them before master.
//
// Bitbucket lists branches newest commit first and by name among equals. The
// commit is made after master's, and decoy- sorts before master for when the
// clock has not moved on in between.
func (h *liveHarness) pushRefResolveDecoys(projectKey, repositorySlug string, count int) error {
	tempDir := h.t.TempDir()

	pushURL, err := repositoryPushURL(h.config, projectKey, repositorySlug)
	if err != nil {
		return err
	}

	push := []string{"push", "origin"}
	for index := range count {
		push = append(push, fmt.Sprintf("HEAD:refs/heads/decoy-%02d", index))
	}

	for _, args := range [][]string{
		{"init"},
		{"config", "user.name", "bb-live-test"},
		{"config", "user.email", "bb-live-test@example.local"},
		{"remote", "add", "origin", pushURL},
		{"fetch", "origin", "master"},
		{"checkout", "-b", "decoys", "FETCH_HEAD"},
		{"commit", "--allow-empty", "-m", "decoy branches for ref resolve"},
		push,
	} {
		if err := runGit(tempDir, args...); err != nil {
			return fmt.Errorf("git %s failed: %w", args[0], err)
		}
	}

	return nil
}
