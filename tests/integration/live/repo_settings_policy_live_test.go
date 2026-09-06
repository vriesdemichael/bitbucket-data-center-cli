//go:build live

package live_test

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestLiveRepoSettingsAutoDeclineLifecycle covers set, get and delete for the
// repository auto-decline policy, none of which had run against a real server.
//
// The read-back after each write is the point: a setting that reports success
// and does not persist is indistinguishable from one that works, and only the
// server can tell the two apart.
func TestLiveRepoSettingsAutoDeclineLifecycle(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	setOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "auto-decline", "set",
		"--enabled", "--inactivity-weeks", "4", "--repo", repoRef)
	if err != nil {
		t.Fatalf("auto-decline set failed: %v\noutput: %s", err, setOutput)
	}

	getOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "auto-decline", "get", "--repo", repoRef)
	if err != nil {
		t.Fatalf("auto-decline get failed: %v\noutput: %s", err, getOutput)
	}
	if !strings.Contains(getOutput, "true") {
		t.Fatalf("expected auto-decline to read back as enabled, got: %s", getOutput)
	}

	deleteOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "auto-decline", "delete", "--repo", repoRef)
	if err != nil {
		t.Fatalf("auto-decline delete failed: %v\noutput: %s", err, deleteOutput)
	}

	// Human output too: it is a separate rendering path, and the JSON passing
	// says nothing about it.
	humanOutput, err := executeLiveCLI(t, "repo", "settings", "auto-decline", "get", "--repo", repoRef)
	if err != nil {
		t.Fatalf("auto-decline get (human) failed: %v\noutput: %s", err, humanOutput)
	}
	if strings.TrimSpace(humanOutput) == "" {
		t.Fatalf("expected human auto-decline output, got nothing")
	}
}

// TestLiveRepoSettingsAutoMergeLifecycle covers the get and delete halves of
// the repository auto-merge setting. set is exercised by the auto-merge pull
// request test, which needs it enabled to arm anything.
func TestLiveRepoSettingsAutoMergeLifecycle(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	if output, err := executeLiveCLI(t, "repo", "settings", "auto-merge", "set", "--enabled", "--repo", repoRef); err != nil {
		t.Fatalf("auto-merge set failed: %v\noutput: %s", err, output)
	}

	getOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "auto-merge", "get", "--repo", repoRef)
	if err != nil {
		t.Fatalf("auto-merge get failed: %v\noutput: %s", err, getOutput)
	}
	if !strings.Contains(getOutput, "true") {
		t.Fatalf("expected auto-merge to read back as enabled, got: %s", getOutput)
	}

	deleteOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "auto-merge", "delete", "--repo", repoRef)
	if err != nil {
		t.Fatalf("auto-merge delete failed: %v\noutput: %s", err, deleteOutput)
	}
}
