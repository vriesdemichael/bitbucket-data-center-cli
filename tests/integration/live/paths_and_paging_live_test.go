//go:build live

package live_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestLiveBrowsePathEscaping covers how a file path becomes a URL.
//
// The mocks these replace read the path off the request they had just received
// and asserted it was escaped one way and not another -- that a space became
// %20, that the separators in a nested path survived rather than being escaped
// into one segment. Both are claims about what Bitbucket accepts, checked
// against a mock built from the same claim, and either one could be wrong in
// the same direction as the code.
//
// Reading the file back is the whole test: a path the server does not
// understand returns nothing, whatever it looked like on the wire.
func TestLiveBrowsePathEscaping(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// A nested path whose separators must survive, and a segment with a space
	// in it that must not.
	const path = "docs/release notes/2026 q1.md"
	const content = "the file behind an awkward path\n"

	if err := harness.pushFileOnBranch(seeded.Key, repo.Slug, "master", path, content); err != nil {
		t.Fatalf("push the awkward path failed: %v", err)
	}

	t.Run("the file is readable at its path", func(t *testing.T) {
		output := mustLiveCLI(t, "repo", "cat", path)
		if !strings.Contains(output, "awkward path") {
			t.Fatalf("expected the file content back, got:\n%s", output)
		}
	})

	t.Run("the directory it sits in lists it", func(t *testing.T) {
		// Escaping the separators would collapse this into one segment and the
		// listing would come back empty rather than wrong.
		output := mustLiveCLI(t, "repo", "browse", "tree", "docs/release notes")
		if !strings.Contains(output, "2026 q1.md") {
			t.Fatalf("expected the nested directory listing to name the file, got:\n%s", output)
		}
	})
}

// TestLiveListingsPageToTheEnd covers Bitbucket's paging convention, which the
// mocks described with a hand-written isLastPage and nextPageStart.
//
// The convention is the server's, so a listing has to be driven past a page
// boundary against the server to mean anything. --limit below the total and
// --all above it are the two answers that matter.
func TestLiveListingsPageToTheEnd(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	const branches = 30
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, "paged-000", "paged.txt"); err != nil {
		t.Fatalf("push the first branch failed: %v", err)
	}
	commits, err := harness.listCommitIDs(ctx, seeded.Key, repo.Slug, 1)
	if err != nil || len(commits) == 0 {
		t.Fatalf("could not read a commit to branch from: %v", err)
	}
	for index := 1; index < branches; index++ {
		name := fmt.Sprintf("paged-%03d", index)
		if _, err := harness.liveJSON(ctx, "POST",
			fmt.Sprintf("/rest/branch-utils/latest/projects/%s/repos/%s/branches", seeded.Key, repo.Slug),
			map[string]any{"name": name, "startPoint": commits[0]}); err != nil {
			t.Fatalf("create branch %s: %v", name, err)
		}
	}

	t.Run("--limit stops where it says", func(t *testing.T) {
		output := mustLiveCLI(t, "branch", "list", "--limit", "5")
		if got := strings.Count(output, "\"displayId\""); got != 5 {
			t.Fatalf("--limit 5 returned %d branches:\n%s", got, output)
		}
	})

	t.Run("--all crosses the page boundary", func(t *testing.T) {
		// The default page is smaller than the branch count, so everything
		// coming back means the pages were followed.
		output := mustLiveCLI(t, "branch", "list", "--all")
		if got := strings.Count(output, "\"displayId\""); got < branches {
			t.Fatalf("--all returned %d of at least %d branches:\n%s", got, branches, output)
		}
	})
}
