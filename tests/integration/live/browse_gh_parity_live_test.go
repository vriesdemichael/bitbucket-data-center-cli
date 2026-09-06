//go:build live

package live_test

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestLiveBrowseParityURLs(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{Commits: 2, WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	tests := []struct {
		name          string
		args          []string
		expectPath    string
		expectSnippet string
	}{
		{name: "home", args: []string{"browse", "--no-browser"}, expectPath: "/projects/" + seeded.Key + "/repos/" + repo.Slug},
		{name: "settings", args: []string{"browse", "--no-browser", "--settings"}, expectPath: "/projects/" + seeded.Key + "/repos/" + repo.Slug + "/settings"},
		{name: "releases", args: []string{"browse", "--no-browser", "--releases"}, expectPath: "/projects/" + seeded.Key + "/repos/" + repo.Slug + "/tags"},
		{name: "path line", args: []string{"browse", "--no-browser", "seed.txt:1"}, expectPath: "/projects/" + seeded.Key + "/repos/" + repo.Slug + "/browse/seed.txt", expectSnippet: "line=1"},
		{name: "path blame", args: []string{"browse", "--no-browser", "seed.txt:1", "--blame"}, expectPath: "/projects/" + seeded.Key + "/repos/" + repo.Slug + "/browse/seed.txt", expectSnippet: "blame=true"},
		{name: "path branch", args: []string{"browse", "--no-browser", "seed.txt", "--branch", "master"}, expectPath: "/projects/" + seeded.Key + "/repos/" + repo.Slug + "/browse/seed.txt", expectSnippet: "at=master"},
		{name: "path commit", args: []string{"browse", "--no-browser", "seed.txt", "--commit", repo.CommitIDs[0]}, expectPath: "/projects/" + seeded.Key + "/repos/" + repo.Slug + "/browse/seed.txt", expectSnippet: "at=" + repo.CommitIDs[0]},
		{name: "commit page", args: []string{"browse", "--no-browser", repo.CommitIDs[0]}, expectPath: "/projects/" + seeded.Key + "/repos/" + repo.Slug + "/commits/" + repo.CommitIDs[0]},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			output, execErr := executeLiveCLI(t, testCase.args...)
			if execErr != nil {
				t.Fatalf("browse failed: %v\noutput: %s", execErr, output)
			}

			if !strings.Contains(output, testCase.expectPath) {
				t.Fatalf("expected browse URL path %q, got: %s", testCase.expectPath, output)
			}

			if strings.TrimSpace(testCase.expectSnippet) != "" && !strings.Contains(output, testCase.expectSnippet) {
				t.Fatalf("expected browse output to contain %q, got: %s", testCase.expectSnippet, output)
			}
		})
	}
}

func TestLiveBrowseRejectsUnsupportedGhFlags(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	unsupported := [][]string{
		{"browse", "--wiki"},
		{"browse", "--actions"},
		{"browse", "--projects"},
	}

	for _, args := range unsupported {
		output, execErr := executeLiveCLI(t, args...)
		if execErr == nil {
			t.Fatalf("expected unsupported flag to fail for args %v; output: %s", args, output)
		}
		if !strings.Contains(strings.ToLower(execErr.Error()), "unknown flag") {
			t.Fatalf("expected unknown flag error for args %v, got: %v\noutput: %s", args, execErr, output)
		}
	}
}
