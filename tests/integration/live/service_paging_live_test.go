//go:build live

package live_test

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/safederef"
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
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	const seededRepos = 3
	const seededCommits = 4

	seeded, err := harness.seedIsolatedProject(ctx, seededRepos, seededCommits)
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

		// The seeded commits exactly, newest first, and the cap their first two.
		ids := make([]string, 0, len(all))
		for _, commit := range all {
			ids = append(ids, safederef.String(commit.Id))
		}
		if !slices.Equal(ids, repo.CommitIDs) {
			t.Errorf("the commits listed are %v, want the seeded %v", ids, repo.CommitIDs)
		}
		for index, commit := range capped {
			if safederef.String(commit.Id) != repo.CommitIDs[index] {
				t.Errorf("capped commit %d is %s, want %s", index, safederef.String(commit.Id), repo.CommitIDs[index])
			}
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

		// The project's own repositories, which are the seeded ones and no more.
		slugs, want := make([]string, 0, len(all)), make([]string, 0, len(seeded.Repos))
		for _, listed := range all {
			slugs = append(slugs, listed.Slug)
		}
		for _, seededRepo := range seeded.Repos {
			want = append(want, seededRepo.Slug)
		}
		if slices.Sort(slugs); !slices.Equal(slugs, slices.Sorted(slices.Values(want))) {
			t.Errorf("project %s lists repositories %v, want the seeded %v", seeded.Key, slugs, want)
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
		want := make([]string, 0, tags)
		for index := range tags {
			name := fmt.Sprintf("v0.0.%d", index)
			if _, err := service.Create(ctx, repoRef, name, commits[0], ""); err != nil {
				t.Fatalf("create tag %s failed: %v", name, err)
			}
			want = append(want, name)
		}

		listed, err := service.List(ctx, repoRef, tagservice.ListOptions{MaxResults: tags + 10})
		if err != nil {
			t.Fatalf("list tags failed: %v", err)
		}
		if len(listed) < tags {
			t.Fatalf("paging stopped early: got %d tags, want at least %d", len(listed), tags)
		}

		// Every tag created, once, at the commit it was made at: a page read
		// twice repeats some and loses others while the count still adds up.
		names := make([]string, 0, len(listed))
		for _, tag := range listed {
			names = append(names, safederef.String(tag.DisplayId))
			if safederef.String(tag.LatestCommit) != commits[0] {
				t.Errorf("tag %s is at %s, want %s", safederef.String(tag.DisplayId), safederef.String(tag.LatestCommit), commits[0])
			}
		}
		if slices.Sort(names); !slices.Equal(names, slices.Sorted(slices.Values(want))) {
			t.Fatalf("the repository lists tags %v, want %v", names, want)
		}

		if capped, err := service.List(ctx, repoRef, tagservice.ListOptions{MaxResults: 5}); err != nil {
			t.Fatalf("list capped tags failed: %v", err)
		} else if len(capped) != 5 {
			t.Fatalf("MaxResults 5 returned %d tags", len(capped))
		}
	})
}
