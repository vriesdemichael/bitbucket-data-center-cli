//go:build live

package live_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	commitservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/commit"
	repositoryservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/repository"
	tagservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/tag"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
)

// Bitbucket's paging convention is the server's, and every listing that pages
// runs its own loop over it.
//
// The unit tests these replace wrote isLastPage and nextPageStart by hand and
// then checked the loop followed the hand-written version. Both halves came
// from the same author, so the pair agreed by construction -- and a loop that
// stops after the first page returns a short answer that looks perfectly well
// formed: no error, no missing field, just fewer results than exist.
//
// These services take MaxResults as a cap on the total rather than as a page
// size, so the page size is fixed and only a listing longer than one page
// crosses a boundary. Tags are cheap enough to seed past it. For commits and
// repositories, seeding thirty of each costs minutes, so what is checked there
// is the contract a caller depends on -- a cap returns exactly the cap, and
// AllResults returns everything -- while the boundary itself is covered by the
// tags here and by branches in TestLiveListingsPageToTheEnd.
func TestLiveServiceListingsPageToTheEnd(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	const seededRepos = 3
	const seededCommits = 4

	seeded, err := harness.seedProjectWithRepositories(ctx, seededRepos, seededCommits)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]

	t.Run("commits honour the cap and return everything above it", func(t *testing.T) {
		service := commitservice.NewService(harness.client)
		repoRef := commitservice.RepositoryRef{ProjectKey: seeded.Key, Slug: repo.Slug}

		capped, err := service.List(ctx, repoRef, commitservice.ListOptions{MaxResults: 2})
		if err != nil {
			t.Fatalf("list commits failed: %v", err)
		}
		if len(capped) != 2 {
			t.Fatalf("MaxResults 2 returned %d commits", len(capped))
		}

		all, err := service.List(ctx, repoRef, commitservice.ListOptions{MaxResults: 100})
		if err != nil {
			t.Fatalf("list all commits failed: %v", err)
		}
		if len(all) < seededCommits {
			t.Fatalf("got %d commits, want at least the %d seeded", len(all), seededCommits)
		}
	})

	t.Run("repositories honour the cap and return everything above it", func(t *testing.T) {
		service := repositoryservice.NewService(httpclient.NewFromConfig(harness.config))

		capped, err := service.ListByProject(ctx, seeded.Key, repositoryservice.ListOptions{MaxResults: 2})
		if err != nil {
			t.Fatalf("list repositories failed: %v", err)
		}
		if len(capped) != 2 {
			t.Fatalf("MaxResults 2 returned %d repositories", len(capped))
		}

		all, err := service.ListByProject(ctx, seeded.Key, repositoryservice.ListOptions{MaxResults: 100})
		if err != nil {
			t.Fatalf("list all repositories failed: %v", err)
		}
		if len(all) < seededRepos {
			t.Fatalf("got %d repositories, want at least the %d seeded", len(all), seededRepos)
		}
	})

	t.Run("tags cross a real page boundary", func(t *testing.T) {
		service := tagservice.NewService(harness.client)
		repoRef := tagservice.RepositoryRef{ProjectKey: seeded.Key, Slug: repo.Slug}

		commits, err := harness.listCommitIDs(ctx, seeded.Key, repo.Slug, 1)
		if err != nil || len(commits) == 0 {
			t.Fatalf("could not read a commit to tag: %v", err)
		}

		// More than one page, so following the convention is what decides
		// whether the last ones come back at all.
		const tags = 30
		for index := range tags {
			name := fmt.Sprintf("v0.0.%d", index)
			if _, err := service.Create(ctx, repoRef, name, commits[0], ""); err != nil {
				t.Fatalf("create tag %s failed: %v", name, err)
			}
		}

		listed, err := service.List(ctx, repoRef, tagservice.ListOptions{MaxResults: tags + 10})
		if err != nil {
			t.Fatalf("list tags failed: %v", err)
		}
		if len(listed) < tags {
			t.Fatalf("paging stopped early: got %d tags, want at least %d", len(listed), tags)
		}

		if capped, err := service.List(ctx, repoRef, tagservice.ListOptions{MaxResults: 5}); err != nil {
			t.Fatalf("list capped tags failed: %v", err)
		} else if len(capped) != 5 {
			t.Fatalf("MaxResults 5 returned %d tags", len(capped))
		}
	})
}
