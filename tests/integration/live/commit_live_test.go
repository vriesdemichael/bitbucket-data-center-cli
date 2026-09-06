//go:build live

package live_test

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestLiveCLICommitAndRefLifecycle(t *testing.T) {
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

	commitID := repo.CommitIDs[0]

	// Commit get
	getOutput, err := executeLiveCLI(t, "commit", "get", commitID)
	if err != nil {
		t.Fatalf("commit get failed: %v\noutput: %s", err, getOutput)
	}
	if !strings.Contains(getOutput, commitID) {
		t.Fatalf("expected commit id in get output, got: %s", getOutput)
	}

	// Commit compare
	// Comparing from head to tail usually yields the commits in between
	from := repo.CommitIDs[0]
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

	// Ref list
	refListOutput, err := executeLiveCLI(t, "ref", "list")
	if err != nil {
		t.Fatalf("ref list failed: %v\noutput: %s", err, refListOutput)
	}
	if !strings.Contains(refListOutput, "master") && !strings.Contains(refListOutput, "main") {
		t.Fatalf("expected master/main in ref list output, got: %s", refListOutput)
	}

	// Ref resolve
	resolveOutput, err := executeLiveCLI(t, "--json", "ref", "resolve", "master")
	if err != nil {
		t.Fatalf("ref resolve failed: %v\noutput: %s", err, resolveOutput)
	}
	resolvePayload := decodeJSONMap(t, resolveOutput)
	refObj, ok := resolvePayload["ref"].(map[string]any)
	if !ok || asString(refObj["displayId"]) != "master" {
		t.Fatalf("expected ref object in resolve output, got: %s", resolveOutput)
	}

	// Not found ref
	_, errNotFound := executeLiveCLI(t, "ref", "resolve", "nonexistent-ref-abc")
	if errNotFound == nil {
		t.Fatalf("expected error when resolving non-existent ref")
	}
}
