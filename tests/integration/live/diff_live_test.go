//go:build live

package live_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	diffservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/diff"
)

func TestLiveDiffRefs(t *testing.T) {
	harness := newLiveHarness(t)
	service := diffservice.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 2)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	if len(repo.CommitIDs) < 2 {
		t.Fatalf("expected at least 2 commits, got %d", len(repo.CommitIDs))
	}

	from := repo.CommitIDs[len(repo.CommitIDs)-1]
	to := repo.CommitIDs[0]
	result, err := service.DiffRefs(ctx, diffservice.DiffRefsInput{
		Repository: diffservice.RepositoryRef{ProjectKey: seeded.Key, Slug: repo.Slug},
		From:       from,
		To:         to,
		Output:     diffservice.OutputKindRaw,
	})
	if err != nil {
		t.Fatalf("diff refs failed: %v", err)
	}
	if result.Patch == "" {
		t.Fatal("expected non-empty raw diff output")
	}
}

func TestLiveDiffPullRequest(t *testing.T) {
	harness := newLiveHarness(t)
	service := diffservice.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	branch := fmt.Sprintf("lt-feature-%d", time.Now().UnixNano()%100000)
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "feature.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	pullRequestID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
	if err != nil {
		t.Fatalf("create pull request failed: %v", err)
	}

	result, err := service.DiffPR(ctx, diffservice.DiffPRInput{
		Repository:    diffservice.RepositoryRef{ProjectKey: seeded.Key, Slug: repo.Slug},
		PullRequestID: pullRequestID,
		Output:        diffservice.OutputKindRaw,
	})
	if err != nil {
		t.Fatalf("pull request diff failed: %v", err)
	}
	if result.Patch == "" {
		t.Fatal("expected non-empty pull request diff output")
	}
}
