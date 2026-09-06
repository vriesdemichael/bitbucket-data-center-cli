//go:build live

package live_test

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestLiveCLIBrowseLifecycle(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{Commits: 2})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// Tree
	treeOutput, err := executeLiveCLI(t, "repo", "browse", "tree", "--limit", "25")
	if err != nil {
		t.Fatalf("tree failed: %v\noutput: %s", err, treeOutput)
	}
	if !strings.Contains(treeOutput, "seed.txt") {
		t.Fatalf("expected seed.txt in tree output, got: %s", treeOutput)
	}

	// Raw
	rawOutput, err := executeLiveCLI(t, "repo", "browse", "raw", "seed.txt")
	if err != nil {
		t.Fatalf("raw failed: %v\noutput: %s", err, rawOutput)
	}
	if !strings.Contains(rawOutput, "commit-") {
		t.Fatalf("expected raw output content, got: %s", rawOutput)
	}

	// File
	fileOutput, err := executeLiveCLI(t, "repo", "browse", "file", "seed.txt")
	if err != nil {
		t.Fatalf("file failed: %v\noutput: %s", err, fileOutput)
	}
	if !strings.Contains(fileOutput, "commit-") {
		t.Fatalf("expected structured file output content, got: %s", fileOutput)
	}

	// A text file that fits in one page says both: not binary, and complete.
	// Without them a caller cannot tell an empty file from a binary one, nor a
	// whole file from the first page of a longer one.
	fileJSON, err := executeLiveCLI(t, "--json", "repo", "browse", "file", "seed.txt")
	if err != nil {
		t.Fatalf("file (json) failed: %v\noutput: %s", err, fileJSON)
	}
	if !strings.Contains(fileJSON, `"binary": false`) || !strings.Contains(fileJSON, `"complete": true`) {
		t.Errorf("expected binary=false and complete=true for a short text file, got: %s", fileJSON)
	}

	// Blame
	blameOutput, err := executeLiveCLI(t, "repo", "browse", "blame", "seed.txt")
	if err != nil {
		t.Fatalf("blame failed: %v\noutput: %s", err, blameOutput)
	}
	if !strings.Contains(blameOutput, "commit-") {
		t.Fatalf("expected blame output content, got: %s", blameOutput)
	}

	// History
	historyOutput, err := executeLiveCLI(t, "repo", "browse", "history", "seed.txt")
	if err != nil {
		t.Fatalf("history failed: %v\noutput: %s", err, historyOutput)
	}
	// history should show commits that modified seed.txt
	if !strings.Contains(historyOutput, "seed commit") {
		t.Fatalf("expected history output content, got: %s", historyOutput)
	}
}
