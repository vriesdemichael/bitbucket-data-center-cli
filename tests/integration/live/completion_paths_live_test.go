//go:build live

package live_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// TestLiveCompletionPaths proves the path sources against a repository that
// really has a nested tree in it.
//
// The assertions are on the values offered rather than on the press having
// succeeded, for the reason the pull request test gives: a source that returns
// nothing looks, in every shell, exactly like a repository with nothing to
// offer. For a path there is a second way to be uselessly right -- answering
// with every file in the repository -- so what is asserted is as much what is
// left out as what is offered.
func TestLiveCompletionPaths(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	selector := seeded.Key + "/" + repo.Slug

	// Two entries under the directory, so Bitbucket cannot fold a chain of
	// single children into one component and change what a press offers.
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, "master", "lt-paths-dir/nested/file.txt"); err != nil {
		t.Fatalf("push the nested file failed: %v", err)
	}
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, "master", "lt-paths-dir/leaf.txt"); err != nil {
		t.Fatalf("push the second entry failed: %v", err)
	}

	branch := testsupport.UniqueName("lt-paths-branch-")
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "lt-paths-only-on-branch.txt"); err != nil {
		t.Fatalf("push the branch failed: %v", err)
	}

	t.Run("the top level offers a directory with a trailing slash", func(t *testing.T) {
		candidates, directive := completeLive(t, "repo", "cat", "--repo", selector, "")

		if _, offered := candidates["lt-paths-dir/"]; !offered {
			t.Fatalf("the directory was not offered with a trailing slash: %v", candidates)
		}
		if _, offered := candidates["seed.txt"]; !offered {
			t.Errorf("a file at the top level was not offered: %v", candidates)
		}
		// One segment at a time. A source that walked the tree would offer
		// this, and a repository with twenty thousand files would answer a
		// keystroke with all of them.
		if _, offered := candidates["lt-paths-dir/nested/file.txt"]; offered {
			t.Errorf("the top level offered a path from deeper in the tree: %v", candidates)
		}
		if !keepsTheCursor(directive) {
			t.Errorf("a directory was offered without the no-space bit, so the next press would start after a space: %d", directive)
		}
		if !forbidsFileNames(directive) {
			t.Errorf("expected the shell to be told not to fall back to file names, got directive %d", directive)
		}
	})

	t.Run("typing into the directory offers what is inside it", func(t *testing.T) {
		candidates, _ := completeLive(t, "repo", "cat", "--repo", selector, "lt-paths-dir/")

		if _, offered := candidates["lt-paths-dir/nested/"]; !offered {
			t.Fatalf("the nested directory was not offered: %v", candidates)
		}
		if _, offered := candidates["lt-paths-dir/leaf.txt"]; !offered {
			t.Errorf("the file beside it was not offered: %v", candidates)
		}
		if _, offered := candidates["seed.txt"]; offered {
			t.Errorf("listing a directory came back with the level above it: %v", candidates)
		}

		inside, _ := completeLive(t, "repo", "cat", "--repo", selector, "lt-paths-dir/nested/")
		if _, offered := inside["lt-paths-dir/nested/file.txt"]; !offered {
			t.Fatalf("the file inside the nested directory was not offered: %v", inside)
		}
	})

	t.Run("a path is read at the ref the line names", func(t *testing.T) {
		// A path exists at a commit rather than in the abstract, and --at is
		// how most of these commands say which one.
		onBranch, _ := completeLive(t, "repo", "browse", "tree", "--repo", selector, "--at", branch, "")
		if _, offered := onBranch["lt-paths-only-on-branch.txt"]; !offered {
			t.Fatalf("the branch's own file was not offered for --at %s: %v", branch, onBranch)
		}

		// Without one it is the default branch, which is where the command
		// itself would have looked -- and which does not carry that file.
		onDefault, _ := completeLive(t, "repo", "browse", "tree", "--repo", selector, "")
		if _, offered := onDefault["lt-paths-only-on-branch.txt"]; offered {
			t.Errorf("a file that exists only on a branch was offered for the default one: %v", onDefault)
		}
		if _, offered := onDefault["lt-paths-dir/"]; !offered {
			t.Errorf("the default branch's own directory was not offered: %v", onDefault)
		}
	})

	t.Run("an inline comment is offered only the files the pull request changes", func(t *testing.T) {
		pullRequestID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
		if err != nil {
			t.Fatalf("create pull request failed: %v", err)
		}

		candidates, _ := completeLive(t, "pr", "comment", "add", pullRequestID, "--repo", selector, "--path", "")

		if _, offered := candidates["lt-paths-only-on-branch.txt"]; !offered {
			t.Fatalf("the file the pull request changes was not offered: %v", candidates)
		}
		// Anchoring an inline comment to a path outside the diff is a value
		// Bitbucket refuses, whether or not the repository holds it.
		if _, offered := candidates["seed.txt"]; offered {
			t.Errorf("a file the pull request does not touch was offered: %v", candidates)
		}
		if _, offered := candidates["lt-paths-dir/leaf.txt"]; offered {
			t.Errorf("a file the pull request does not touch was offered: %v", candidates)
		}
	})

	// The narrowing below is asserted against the endpoint rather than through
	// a press, for the reason the refs test gives: run.go drops every
	// candidate that does not start with the typed word, so a listing that
	// came back with the whole repository is indistinguishable from one the
	// server narrowed. Bitbucket ignores a query parameter it does not
	// recognise rather than refusing it, which is how a misspelled one would
	// otherwise survive every assertion above.
	t.Run("Bitbucket lists the directory it is given, not the tree under it", func(t *testing.T) {
		root := browseChildren(t, harness, ctx, seeded.Key, repo.Slug, "", "")
		if !containsRef(root, "seed.txt") || !containsRef(root, "lt-paths-dir") {
			t.Fatalf("the root listing is missing what the assertions below rest on: %v", root)
		}
		if containsRef(root, "lt-paths-dir/nested/file.txt") || containsRef(root, "nested") {
			t.Errorf("the root listing walked into the tree: %v", root)
		}

		inside := browseChildren(t, harness, ctx, seeded.Key, repo.Slug, "lt-paths-dir", "")
		if !containsRef(inside, "nested") || !containsRef(inside, "leaf.txt") {
			t.Fatalf("the directory listing is missing its own entries: %v", inside)
		}
		if containsRef(inside, "seed.txt") {
			t.Errorf("the path in the request was ignored and the root came back instead: %v", inside)
		}
	})

	t.Run("Bitbucket applies the page size it is given", func(t *testing.T) {
		// The directory holds more than one entry, so a limit that survived
		// the round trip has something to cut.
		if limited := browseChildren(t, harness, ctx, seeded.Key, repo.Slug, "", "&limit=1"); len(limited) != 1 {
			t.Errorf("limit=1 came back with %d entries, so Bitbucket ignored it: %v", len(limited), limited)
		}
	})
}

// browseChildren reads one directory straight from the endpoint the source
// uses, and answers with the names of its children.
func browseChildren(
	t *testing.T,
	harness *liveHarness,
	ctx context.Context,
	projectKey, slug, directory, extraQuery string,
) []string {
	t.Helper()

	path := "/rest/api/latest/projects/" + projectKey + "/repos/" + slug + "/browse/" + directory + "?at=refs/heads/master" + extraQuery

	response, err := harness.liveJSON(ctx, "GET", path, nil)
	if err != nil {
		t.Fatalf("browse %s failed: %v", path, err)
	}

	children, ok := response["children"].(map[string]any)
	if !ok {
		t.Fatalf("the browse response carried no children: %v", response)
	}

	values, ok := children["values"].([]any)
	if !ok {
		t.Fatalf("the browse response carried no child values: %v", children)
	}

	names := make([]string, 0, len(values))
	for _, value := range values {
		child, ok := value.(map[string]any)
		if !ok {
			continue
		}

		childPath, ok := child["path"].(map[string]any)
		if !ok {
			continue
		}

		if name, ok := childPath["toString"].(string); ok && strings.TrimSpace(name) != "" {
			names = append(names, name)
		}
	}

	return names
}

// keepsTheCursor reports the bit that leaves the cursor against the value, so
// the next press continues into the directory just completed rather than
// starting a new word.
func keepsTheCursor(directive int) bool {
	return directive&int(cobra.ShellCompDirectiveNoSpace) != 0
}
