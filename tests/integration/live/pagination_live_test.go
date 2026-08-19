//go:build live

package live_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestLiveCommitPaginationOverRealStream(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// Seed 30 commits into a single repo (Bitbucket DC default page size is 25)
	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 30)
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
		Version string           `json:"version"`
		Data    []map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("failed to decode commit list output: %v\nraw: %s", err, output)
	}

	if len(envelope.Data) != 28 {
		t.Fatalf("expected exactly 28 commits across paginated stream, got %d", len(envelope.Data))
	}

	// Fetch 10 commits: must truncate on page 1 without requesting page 2
	outputShort, err := executeLiveCLI(t, "--json", "commit", "list", "--limit", "10")
	if err != nil {
		t.Fatalf("commit list --limit 10 failed: %v\noutput: %s", err, outputShort)
	}

	var envelopeShort struct {
		Version string           `json:"version"`
		Data    []map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(outputShort), &envelopeShort); err != nil {
		t.Fatalf("failed to decode short commit list output: %v", err)
	}

	if len(envelopeShort.Data) != 10 {
		t.Fatalf("expected exactly 10 commits for short query, got %d", len(envelopeShort.Data))
	}
}

func TestLiveBranchPaginationOverRealStream(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 2)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// Create 28 branches to exceed Bitbucket DC's default page size of 25
	for index := 1; index <= 28; index++ {
		branchName := fmt.Sprintf("feature/paginated-%02d", index)
		createOutput, createErr := executeLiveCLI(t, "--json", "branch", "create", branchName, "--target", "refs/heads/master")
		if createErr != nil {
			t.Fatalf("create branch %s failed: %v\noutput: %s", branchName, createErr, createOutput)
		}
	}

	// List branches with limit 26: forces pagination traversal
	output, err := executeLiveCLI(t, "--json", "branch", "list", "--limit", "26")
	if err != nil {
		t.Fatalf("branch list --limit 26 failed: %v\noutput: %s", err, output)
	}

	var envelope struct {
		Version string           `json:"version"`
		Data    []map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("failed to decode branch list output: %v\nraw: %s", err, output)
	}

	if len(envelope.Data) != 26 {
		t.Fatalf("expected exactly 26 branches across paginated stream, got %d", len(envelope.Data))
	}
}
