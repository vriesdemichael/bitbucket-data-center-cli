//go:build live

package live_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	repositoryservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/repository"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
)

// holdsTheCursorAtTheSlash reports the bit that keeps the cursor against
// PROJECT/, so the slug can be typed without deleting a space first.
func holdsTheCursorAtTheSlash(directive int) bool {
	return directive&int(cobra.ShellCompDirectiveNoSpace) != 0
}

// ranksItsAnswer reports the bit that asks the shell to show the candidates in
// the order they were given rather than sorting them.
func ranksItsAnswer(directive int) bool {
	return directive&int(cobra.ShellCompDirectiveKeepOrder) != 0
}

// TestLiveCompletionReposOffersProjectsAndRepositories asserts the values a
// repository selector offers, in both of its stages.
//
// Completion is the surface where a wrong answer is invisible -- a source that
// returns nothing looks exactly like an instance with nothing to offer, in
// every shell, with stderr discarded -- so every assertion here is on the
// seeded key and slug themselves rather than on the call having succeeded.
func TestLiveCompletionReposOffersProjectsAndRepositories(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	projectKey, repositories := seedCompletionProject(t, harness, 2)
	first, second := repositories[0], repositories[1]

	t.Run("a project key is offered with the project's name", func(t *testing.T) {
		candidates, directive := completeLive(t, "project", "get", completionProjectPrefix)

		description, offered := candidates[projectKey]
		if !offered {
			t.Fatalf("project %s was not offered for `bb project get`; got %v", projectKey, candidates)
		}
		if !strings.Contains(description, completionProjectPrefix) {
			t.Errorf("expected the project's name beside its key, got %q", description)
		}
		if !forbidsFileNames(directive) {
			t.Errorf("expected the no-file-completion bit to be set, got directive %d", directive)
		}
	})

	t.Run("a half-typed selector offers a project to open, not every repository", func(t *testing.T) {
		// The first stage. An instance holds thousands of repositories and one
		// keystroke is not a reason to list them, so what comes back is the
		// project key with the slash already on it.
		candidates, directive := completeLive(t, "repo", "clone", completionProjectPrefix)

		description, offered := candidates[projectKey+"/"]
		if !offered {
			t.Fatalf("project %s/ was not offered as a stage to open; got %v", projectKey, candidates)
		}
		if !strings.Contains(description, completionProjectPrefix) {
			t.Errorf("expected the project's name beside its key, got %q", description)
		}
		if _, offered := candidates[projectKey+"/"+first.Slug]; offered {
			t.Errorf("repository %s/%s was offered before a project was chosen", projectKey, first.Slug)
		}
		if !holdsTheCursorAtTheSlash(directive) {
			t.Errorf("expected the cursor kept against the slash, got directive %d", directive)
		}
		if !forbidsFileNames(directive) {
			t.Errorf("expected the no-file-completion bit to be set, got directive %d", directive)
		}
	})

	t.Run("a chosen project offers the slugs inside it", func(t *testing.T) {
		// The second stage, reached by the slash the first one left behind.
		candidates, directive := completeLive(t, "repo", "clone", projectKey+"/")

		for _, repository := range repositories {
			description, offered := candidates[projectKey+"/"+repository.Slug]
			if !offered {
				t.Fatalf("repository %s/%s was not offered; got %v", projectKey, repository.Slug, candidates)
			}
			if !strings.Contains(description, repository.Name) {
				t.Errorf("expected the repository's name beside its slug, got %q", description)
			}
		}
		if holdsTheCursorAtTheSlash(directive) {
			t.Errorf("expected a space after a complete selector, got directive %d", directive)
		}
	})

	t.Run("what is typed after the slash narrows the slugs", func(t *testing.T) {
		prefix := distinguishingPrefix(first.Slug, second.Slug)

		candidates, _ := completeLive(t, "repo", "clone", projectKey+"/"+prefix)

		if _, offered := candidates[projectKey+"/"+first.Slug]; !offered {
			t.Fatalf("repository %s/%s was not offered for the prefix it starts with; got %v",
				projectKey, first.Slug, candidates)
		}
		if _, offered := candidates[projectKey+"/"+second.Slug]; offered {
			t.Errorf("repository %s/%s was offered for a prefix it does not start with", projectKey, second.Slug)
		}
	})

	t.Run("a repository flag completes the same way as the argument", func(t *testing.T) {
		// --repo is declared 37 times across the tree and means a repository
		// selector in all of them, so the flag and the argument have to answer
		// alike.
		candidates, _ := completeLive(t, "pr", "merge", "--repo", projectKey+"/")

		if _, offered := candidates[projectKey+"/"+first.Slug]; !offered {
			t.Fatalf("repository %s/%s was not offered for --repo; got %v", projectKey, first.Slug, candidates)
		}
	})

	t.Run("a destructive target offers the repository without ranking one first", func(t *testing.T) {
		// `bb repo delete` counts a repository named here as an explicit
		// target, which is what --yes applies to (ADR-073). It still completes,
		// with the same values as anywhere else, but it never asks the shell to
		// keep an order -- a ranking is how one tab press would put the
		// repository the caller merely stands in under the cursor and make the
		// safety flag apply to it.
		//
		// Which repository would have been ranked first is unit-tested; this
		// run has no checkout of the instance to promote.
		candidates, directive := completeLive(t, "repo", "delete", projectKey+"/")

		if _, offered := candidates[projectKey+"/"+first.Slug]; !offered {
			t.Fatalf("repository %s/%s was not offered to `bb repo delete`; got %v",
				projectKey, first.Slug, candidates)
		}
		if ranksItsAnswer(directive) {
			t.Errorf("a destructive target asked the shell to keep a ranking, got directive %d", directive)
		}
	})
}

// TestLiveCompletionReposFiltersOnTheServer proves the narrowing is Bitbucket's
// and not this process's.
//
// It cannot be proved through the completion output: a source that fetched
// everything and filtered the page itself answers one keystroke identically,
// and only diverges on an instance too large to seed. So the listings the
// source issues are issued here directly, and compared against the same
// listings without the filter.
//
// Bitbucket ignores a query parameter it does not know rather than refusing it,
// which is the trap this pins: an ignored filter returns the whole collection
// and looks exactly like a filter that matched all of it.
func TestLiveCompletionReposFiltersOnTheServer(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	projectKey, repositories := seedCompletionProject(t, harness, 2)
	first, second := repositories[0], repositories[1]

	t.Run("projectkey scopes the instance-wide listing", func(t *testing.T) {
		listed := listRepositoriesLive(ctx, t, harness, projectKey, "")

		slugs := map[string]bool{}
		for _, repository := range listed {
			if repository.Project == nil || repository.Project.Key != projectKey {
				t.Fatalf("a repository from project %v came back from a listing scoped to %s",
					repository.Project, projectKey)
			}
			if repository.Slug != nil {
				slugs[*repository.Slug] = true
			}
		}

		for _, repository := range repositories {
			if !slugs[repository.Slug] {
				t.Errorf("seeded repository %s is missing from its project's listing: %v", repository.Slug, slugs)
			}
		}
	})

	t.Run("name narrows it further", func(t *testing.T) {
		listed := listRepositoriesLive(ctx, t, harness, projectKey, first.Name)

		slugs := map[string]bool{}
		for _, repository := range listed {
			if repository.Slug != nil {
				slugs[*repository.Slug] = true
			}
		}

		if !slugs[first.Slug] {
			t.Fatalf("the filter excluded the repository it names: %s not in %v", first.Slug, slugs)
		}
		if slugs[second.Slug] {
			t.Fatalf("the name filter was ignored: %s came back from a listing filtered to %q (%v)",
				second.Slug, first.Name, slugs)
		}
	})

	t.Run("the project-scoped listing is not a substitute for it", func(t *testing.T) {
		// /projects/{key}/repos accepts a name parameter and does nothing with
		// it, which is why the source scopes the instance-wide listing with
		// projectkey instead of asking a project for its own repositories.
		//
		// If this ever starts failing, Bitbucket has grown the filter and
		// listRepositories in internal/cli/completion can lose a call.
		service := repositoryservice.NewService(httpclient.NewFromConfig(harness.config))

		listed, err := service.ListByProject(ctx, projectKey, repositoryservice.ListOptions{
			Name:       first.Name,
			MaxResults: 25,
		})
		if err != nil {
			t.Fatalf("list %s by project: %v", projectKey, err)
		}

		filtered := false
		for _, repository := range listed {
			if repository.Slug == second.Slug {
				filtered = true
			}
		}

		if !filtered {
			t.Fatalf("the project-scoped listing honoured a name filter it used to ignore; got %v", listed)
		}
	})
}

// completionProjectPrefix is both the key prefix and the name prefix of the
// project these tests seed.
//
// The harness's usual pair -- key LT1234, name "Live Test 1234" -- shares
// nothing between the two, and Bitbucket filters projects by their display
// name while the value being completed is the key. A project named after its
// key is therefore reachable by either half of the listing, so the assertion
// does not depend on how many other projects the instance is holding.
const completionProjectPrefix = "LTCMP"

// seedCompletionProject gives a test a project of its own with repositories in
// it, and takes it away again afterwards.
func seedCompletionProject(t *testing.T, harness *liveHarness, repositories int) (string, []seededRepository) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	projectKey, projectName, err := harness.createProject(ctx, completionProjectPrefix, completionProjectPrefix)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		harness.deleteProjectAndContents(cleanupCtx, projectKey)
	})

	seeded, err := harness.seedRepositories(ctx, projectKey, projectName, repositories, 1, false, false)
	if err != nil {
		t.Fatalf("seed %d repositories in %s: %v", repositories, projectKey, err)
	}

	return projectKey, seeded.Repos
}

// listRepositoriesLive is the listing internal/cli/completion issues for the
// second stage of a selector, with the same parameters.
func listRepositoriesLive(
	ctx context.Context,
	t *testing.T,
	harness *liveHarness,
	projectKey string,
	name string,
) []openapigenerated.RestRepository {
	t.Helper()

	limit := float32(25)
	params := &openapigenerated.GetRepositories1Params{Limit: &limit, Projectkey: &projectKey}
	if strings.TrimSpace(name) != "" {
		params.Name = &name
	}

	response, err := harness.client.GetRepositories1WithResponse(ctx, params)
	if err != nil {
		t.Fatalf("list repositories: %v", err)
	}
	if response.StatusCode() < 200 || response.StatusCode() >= 300 {
		t.Fatalf("list repositories returned status %d: %s", response.StatusCode(), response.Body)
	}

	page := response.ApplicationjsonCharsetUTF8200
	if page == nil || page.Values == nil {
		return nil
	}

	return *page.Values
}

// distinguishingPrefix is the shortest prefix of one slug that the other does
// not share, which is what a narrowing assertion needs to be about narrowing
// rather than about the names the harness happened to pick.
func distinguishingPrefix(slug string, other string) string {
	for index := 0; index < len(slug); index++ {
		if index >= len(other) || slug[index] != other[index] {
			return slug[:index+1]
		}
	}

	return slug
}
