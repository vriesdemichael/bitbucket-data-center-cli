//go:build live

package live_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

func TestLiveHarnessSeedsMultipleReposWithCommits(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{Repos: 3, Commits: 2, WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	if len(seeded.Repos) != 3 {
		t.Fatalf("expected 3 repositories, got %d", len(seeded.Repos))
	}

	for _, repo := range seeded.Repos {
		if len(repo.CommitIDs) < 2 {
			t.Fatalf("expected at least 2 commits for %s/%s, got %d", seeded.Key, repo.Slug, len(repo.CommitIDs))
		}

		if err := assertChangesEndpointWorks(ctx, harness, seeded.Key, repo.Slug, repo.CommitIDs); err != nil {
			t.Fatalf("changes endpoint assertion failed for %s/%s: %v", seeded.Key, repo.Slug, err)
		}
	}
}

func assertChangesEndpointWorks(ctx context.Context, harness *liveHarness, projectKey, repositorySlug string, commitIDs []string) error {
	until := commitIDs[0]
	since := commitIDs[len(commitIDs)-1]
	limit := float32(50)

	response, err := harness.client.GetChanges1WithResponse(ctx, projectKey, repositorySlug, &openapigenerated.GetChanges1Params{
		Until: &until,
		Since: &since,
		Limit: &limit,
	})
	if err != nil {
		return fmt.Errorf("get changes call failed: %w", err)
	}
	if response.StatusCode() < 200 || response.StatusCode() >= 300 {
		return fmt.Errorf("get changes returned status %d", response.StatusCode())
	}
	if response.ApplicationjsonCharsetUTF8200 == nil {
		return fmt.Errorf("get changes response body was empty")
	}
	if response.ApplicationjsonCharsetUTF8200.Values == nil || len(*response.ApplicationjsonCharsetUTF8200.Values) == 0 {
		return fmt.Errorf("get changes returned no file changes between seeded commits")
	}

	return nil
}
