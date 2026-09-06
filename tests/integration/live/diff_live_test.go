//go:build live

package live_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	diffservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/diff"
)

func TestLiveDiffRefs(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	service := diffservice.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{Commits: 2, WithCommitIDs: true})
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
	t.Parallel()

	harness := newLiveHarness(t)
	service := diffservice.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{WithCommitIDs: true})
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

// TestLiveDiffOutputModes covers the output kinds beside raw, and a ref that is
// not there.
//
// The mocks these replace served a canned patch and asserted what bb made of
// it -- which files it listed for name_only, what it counted for stat, how it
// mapped a 404. Each is a claim about what Bitbucket sends and when. A real
// diff of a real commit settles all of them, and a ref that does not exist
// produces the 404 rather than describing it.
func TestLiveDiffOutputModes(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	service := diffservice.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{Commits: 2, WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	if len(repo.CommitIDs) < 2 {
		t.Fatalf("expected at least 2 commits, got %d", len(repo.CommitIDs))
	}
	repository := diffservice.RepositoryRef{ProjectKey: seeded.Key, Slug: repo.Slug}
	from := repo.CommitIDs[len(repo.CommitIDs)-1]
	to := repo.CommitIDs[0]

	t.Run("name_only lists the files that changed", func(t *testing.T) {
		result, err := service.DiffRefs(ctx, diffservice.DiffRefsInput{
			Repository: repository, From: from, To: to, Output: diffservice.OutputKindNameOnly,
		})
		if err != nil {
			t.Fatalf("name_only diff failed: %v", err)
		}
		if len(result.Names) == 0 {
			t.Fatalf("expected at least one changed file, got %#v", result)
		}
	})

	t.Run("stat counts the change", func(t *testing.T) {
		result, err := service.DiffRefs(ctx, diffservice.DiffRefsInput{
			Repository: repository, From: from, To: to, Output: diffservice.OutputKindStat,
		})
		if err != nil {
			t.Fatalf("stat diff failed: %v", err)
		}
		if len(result.Stats) == 0 {
			t.Fatalf("expected stat to report a summary, got %#v", result)
		}
	})

	t.Run("a ref that does not exist is not found", func(t *testing.T) {
		_, err := service.DiffRefs(ctx, diffservice.DiffRefsInput{
			Repository: repository, From: "refs/heads/does-not-exist", To: to,
			Output: diffservice.OutputKindRaw,
		})
		if err == nil {
			t.Fatal("expected a missing ref to fail")
		}
		if apperrors.IsKind(err, apperrors.KindTransient) {
			t.Errorf("a missing ref is not a transient failure: %v", err)
		}
	})
}
