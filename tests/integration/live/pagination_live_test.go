//go:build live

package live_test

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"
)

func TestLiveCommitPaginationOverRealStream(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// Seed 30 commits into a single repo (Bitbucket DC default page size is 25)
	seeded, err := harness.seedRepo(ctx, repoSeed{Commits: 30})
	if err != nil {
		t.Fatalf("seed project with 30 commits failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// Fetch 28 commits: requires traversing page 1 (25 items) and fetching 3 items from page 2
	output, err := executeLiveCLI(t, "--json", "commit", "list", "--limit", "28")
	if err != nil {
		t.Fatalf("commit list --limit 28 failed: %v\noutput: %s", err, output)
	}

	var envelope struct {
		Data struct {
			Commits []map[string]any `json:"commits"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("failed to decode commit list output: %v\nraw: %s", err, output)
	}

	if len(envelope.Data.Commits) != 28 {
		t.Fatalf("expected exactly 28 commits across paginated stream, got %d", len(envelope.Data.Commits))
	}

	// Fetch 10 commits: must truncate on page 1 without requesting page 2
	outputShort, err := executeLiveCLI(t, "--json", "commit", "list", "--limit", "10")
	if err != nil {
		t.Fatalf("commit list --limit 10 failed: %v\noutput: %s", err, outputShort)
	}

	var envelopeShort struct {
		Data struct {
			Commits []map[string]any `json:"commits"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(outputShort), &envelopeShort); err != nil {
		t.Fatalf("failed to decode short commit list output: %v", err)
	}

	if len(envelopeShort.Data.Commits) != 10 {
		t.Fatalf("expected exactly 10 commits for short query, got %d", len(envelopeShort.Data.Commits))
	}

	// Twenty-eight different commits, the newest ten of them the ten the short
	// query returned: a second page that read the first again, or started in
	// the wrong place, would repeat or skip.
	ids := make([]string, 0, len(envelope.Data.Commits))
	for _, commit := range envelope.Data.Commits {
		ids = append(ids, asString(commit["id"]))
	}
	if distinct := slices.Compact(slices.Sorted(slices.Values(ids))); len(distinct) != len(ids) {
		t.Errorf("the 28 commits hold only %d different ones: %v", len(distinct), ids)
	}
	for index, commit := range envelopeShort.Data.Commits {
		if asString(commit["id"]) != ids[index] {
			t.Errorf("commit %d of the short query is %v, and of the long one %s", index, commit["id"], ids[index])
		}
	}
}

func TestLiveBranchPaginationOverRealStream(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{Commits: 2, WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// Create 28 branches to exceed Bitbucket DC's default page size of 25
	created := []string{"master"}
	for index := 1; index <= 28; index++ {
		branchName := fmt.Sprintf("feature/paginated-%02d", index)
		createOutput, createErr := executeLiveCLI(t, "--json", "branch", "create", branchName, "--start-point", "refs/heads/master")
		if createErr != nil {
			t.Fatalf("create branch %s failed: %v\noutput: %s", branchName, createErr, createOutput)
		}
		created = append(created, branchName)
	}

	// Read back, all of them at master's tip: the listings below are counted,
	// and a count cannot tell the branches created from other ones.
	stored, _ := decodeJSONMap(t, mustLiveCLI(t, "branch", "list", "--all"))["branches"].([]any)
	names := make([]string, 0, len(stored))
	for _, entry := range stored {
		branch, _ := entry.(map[string]any)
		names = append(names, asString(branch["displayId"]))
		if branch["latestCommit"] != repo.CommitIDs[0] {
			t.Errorf("branch %v is at %v, want master's tip %s", branch["displayId"], branch["latestCommit"], repo.CommitIDs[0])
		}
	}
	if slices.Sort(names); !slices.Equal(names, slices.Sorted(slices.Values(created))) {
		t.Fatalf("the repository holds branches %v, want %v", names, created)
	}

	// List branches with limit 26: forces pagination traversal
	output, err := executeLiveCLI(t, "--json", "branch", "list", "--limit", "26")
	if err != nil {
		t.Fatalf("branch list --limit 26 failed: %v\noutput: %s", err, output)
	}

	var envelope struct {
		Data struct {
			Branches []map[string]any `json:"branches"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("failed to decode branch list output: %v\nraw: %s", err, output)
	}

	if len(envelope.Data.Branches) != 26 {
		t.Fatalf("expected exactly 26 branches across paginated stream, got %d", len(envelope.Data.Branches))
	}
	listed := make([]string, 0, len(envelope.Data.Branches))
	for _, branch := range envelope.Data.Branches {
		listed = append(listed, asString(branch["displayId"]))
	}
	if distinct := slices.Compact(slices.Sorted(slices.Values(listed))); len(distinct) != len(listed) {
		t.Errorf("the 26 branches hold only %d different ones: %v", len(distinct), listed)
	}
}
