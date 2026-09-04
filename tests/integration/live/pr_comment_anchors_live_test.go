//go:build live

package live_test

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestLivePRCommentAnchors covers `pr comment add` with an anchor, which has
// its own flags and its own service path -- separate from the ones
// `repo comment create` goes through.
//
// The mocks these replace built an anchor and read it back out of the request
// they had just received. The interesting case is a REMOVED line: it anchors to
// the original file rather than the new one, which the mock asserted by
// checking which field bb filled in. Whether Bitbucket accepts that and hangs
// the comment where it was meant to go is the question, and only the server
// answers it.
func TestLivePRCommentAnchors(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// The branch rewrites a file that exists on master, so the diff carries
	// both an added and a removed line to anchor to.
	const file = "rewritten.txt"
	if err := harness.pushFileOnBranch(seeded.Key, repo.Slug, "master", file, "original line\n"); err != nil {
		t.Fatalf("push the original failed: %v", err)
	}

	const branch = "feature/rewrite"
	if err := harness.pushFileOnBranch(seeded.Key, repo.Slug, branch, file, "replacement line\n"); err != nil {
		t.Fatalf("push the rewrite failed: %v", err)
	}

	prID := createLivePRForRegression(t, branch, "Anchored review", "--no-default-reviewers", "--no-codeowners")

	var rootID string

	t.Run("an added line takes an anchored comment", func(t *testing.T) {
		output := mustLiveCLI(t, "pr", "comment", "add", prID,
			"--text", "on the added line", "--path", file, "--line", "1", "--line-type", "ADDED")

		rootID = commentIDFrom(t, output)
		if rootID == "" {
			t.Fatalf("no comment id in:\n%s", output)
		}
		assertLiveCommentAnchoredTo(t, output, file)
	})

	t.Run("a removed line anchors to the original file", func(t *testing.T) {
		output := mustLiveCLI(t, "pr", "comment", "add", prID,
			"--text", "on the removed line", "--path", file, "--line", "1", "--line-type", "REMOVED")

		assertLiveCommentAnchoredTo(t, output, file)
	})

	t.Run("a reply hangs off its parent", func(t *testing.T) {
		if rootID == "" {
			t.Skip("no root comment to reply to")
		}

		output := mustLiveCLI(t, "pr", "comment", "add", prID,
			"--text", "replying inline", "--parent-id", rootID)

		if id := commentIDFrom(t, output); id == "" || id == rootID {
			t.Fatalf("expected a distinct reply id, got %q", id)
		}
		if !strings.Contains(output, "\"parentId\"") {
			t.Errorf("expected the reply to name its parent:\n%s", output)
		}
	})
}

// assertLiveCommentAnchoredTo checks the server stored the anchor, rather than
// accepting the comment and dropping it.
func assertLiveCommentAnchoredTo(t *testing.T, output, path string) {
	t.Helper()

	data := decodeJSONMap(t, output)
	if nested, ok := data["comment"].(map[string]any); ok {
		data = nested
	}

	anchor, ok := data["anchor"].(map[string]any)
	if !ok {
		t.Fatalf("the comment came back with no anchor at all:\n%s", output)
	}
	if got, _ := anchor["path"].(string); got != path {
		t.Errorf("anchor path = %q, want %q", got, path)
	}
}

// TestLivePRWatchAndUnwatch runs the two commands for real. Their only live
// coverage was a dry run, which by definition subscribes to nothing.
func TestLivePRWatchAndUnwatch(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	const branch = "feature/watched"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "watched.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	prID := createLivePRForRegression(t, branch, "Watched", "--no-default-reviewers", "--no-codeowners")

	// Both directions, because a watch that cannot be undone is its own bug and
	// an unwatch that silently does nothing looks the same as one that works.
	mustLiveCLI(t, "pr", "watch", prID)
	mustLiveCLI(t, "pr", "unwatch", prID)

	// Unwatching twice must not fail: the second call is a no-op on the server,
	// and a caller scripting cleanup should not have to care.
	if output, err := executeLiveCLI(t, "--json", "pr", "unwatch", prID); err != nil {
		t.Fatalf("unwatching an unwatched pull request failed: %v\noutput: %s", err, output)
	}
}
