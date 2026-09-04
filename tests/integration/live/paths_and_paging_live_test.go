//go:build live

package live_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
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

// TestLiveBranchCommandSurfaces covers the three things the branch mocks in
// root_test.go asserted: what an empty listing prints, what a missing resource
// maps to, and what a dry run predicts.
//
// All three were fabricated. An empty listing was an empty values array the
// author wrote, a 404 was a status the author chose, and the dry-run preview
// was built from state the author supplied. A real repository produces all
// three without anyone deciding what they look like.
func TestLiveBranchCommandSurfaces(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	t.Run("an empty listing says so rather than printing nothing", func(t *testing.T) {
		// A fresh repository has no branch restrictions, so this is genuinely
		// empty rather than emptied for the test.
		output := mustLiveCLI(t, "branch", "restriction", "list", "--repo", repoRef)
		if strings.TrimSpace(output) == "" {
			t.Fatal("an empty listing printed nothing at all, which reads as a broken command")
		}
	})

	t.Run("a repository that is not there maps to not found", func(t *testing.T) {
		output, err := executeLiveCLI(t, "--json", "branch", "list", "--repo", seeded.Key+"/no-such-repository")
		if err == nil {
			t.Fatalf("expected a missing repository to fail, got:\n%s", output)
		}
		if code := apperrors.ExitCode(err); code != 4 {
			t.Errorf("exit code = %d, want 4 for a missing repository (%v)", code, err)
		}
	})

	t.Run("a dry run predicts the create without making it", func(t *testing.T) {
		const branch = "feature/predicted"

		output := mustLiveCLI(t, "--dry-run", "branch", "create", branch, "--start-point", "master")
		for _, want := range []string{`"planningMode": "stateful"`, `"predictedAction": "create"`} {
			if !strings.Contains(output, want) {
				t.Errorf("expected %s in the preview:\n%s", want, output)
			}
		}

		// The half a mock cannot check: that nothing happened.
		listing := mustLiveCLI(t, "branch", "list", "--all")
		if strings.Contains(listing, branch) {
			t.Fatalf("the dry run created the branch:\n%s", listing)
		}
	})
}

// TestLiveBranchRestrictionLimitCaps covers a flag that did nothing.
//
// `--limit` reached the service as MaxResults, which was used as the page size
// and capped nothing: the walk ran to the last page and the CLI does not
// truncate afterwards, so every restriction came back however small the number
// asked for. The name said cap, the code said page, and no test asked.
func TestLiveBranchRestrictionLimitCaps(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	const restrictions = 4
	for index := range restrictions {
		if _, err := harness.liveJSON(ctx, http.MethodPost,
			fmt.Sprintf("/rest/branch-permissions/latest/projects/%s/repos/%s/restrictions", seeded.Key, repo.Slug),
			map[string]any{
				"type":    "read-only",
				"matcher": map[string]any{"id": fmt.Sprintf("refs/heads/capped-%d", index), "type": map[string]any{"id": "BRANCH"}},
				"users":   []string{},
				"groups":  []string{},
			}); err != nil {
			t.Fatalf("create restriction %d failed: %v", index, err)
		}
	}

	// Counted from the decoded list rather than by matching "id": in the text.
	// Each restriction carries a nested matcher with an id of its own, so the
	// text count is double and reads as a failure that is not there.
	countRestrictions := func(t *testing.T, output string) int {
		t.Helper()

		listed, _ := decodeJSONMap(t, output)["restrictions"].([]any)

		return len(listed)
	}

	all := mustLiveCLI(t, "branch", "restriction", "list", "--repo", repoRef, "--all")
	if count := countRestrictions(t, all); count < restrictions {
		t.Fatalf("--all returned %d restrictions, want at least %d:\n%s", count, restrictions, all)
	}

	limited := mustLiveCLI(t, "branch", "restriction", "list", "--repo", repoRef, "--limit", "2")
	if count := countRestrictions(t, limited); count != 2 {
		t.Fatalf("--limit 2 returned %d restrictions:\n%s", count, limited)
	}
}

// TestLiveCommitCompareLimitCaps is the branch-restriction defect in a second
// place, found by converting its loop rather than by a test.
//
// MaxResults reached the service and was used as the page size, and nothing
// capped the results: `bb commit compare --limit 2` walked to the last page and
// returned every commit between the two refs.
func TestLiveCommitCompareLimitCaps(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	const commits = 6
	seeded, err := harness.seedProjectWithRepositories(ctx, 1, commits)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// The repository's own history is the range: oldest to newest has every
	// commit but the first in it.
	if len(repo.CommitIDs) < 3 {
		t.Fatalf("expected several seeded commits, got %d", len(repo.CommitIDs))
	}
	oldest := repo.CommitIDs[len(repo.CommitIDs)-1]
	newest := repo.CommitIDs[0]

	countCommits := func(t *testing.T, output string) int {
		t.Helper()

		listed, _ := decodeJSONMap(t, output)["commits"].([]any)

		return len(listed)
	}

	all := mustLiveCLI(t, "commit", "compare", newest, oldest, "--all")
	if total := countCommits(t, all); total < 2 {
		t.Fatalf("the seeded range has %d commits, too few to cap:\n%s", total, all)
	}

	limited := mustLiveCLI(t, "commit", "compare", newest, oldest, "--limit", "1")
	if count := countCommits(t, limited); count != 1 {
		t.Fatalf("--limit 1 returned %d commits:\n%s", count, limited)
	}
}
