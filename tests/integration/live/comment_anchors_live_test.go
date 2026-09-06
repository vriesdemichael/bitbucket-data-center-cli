//go:build live

package live_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Comment anchoring, replies and resolution, against a real server.
//
// The mocks these replace asserted the anchor object bb builds -- the path, the
// line, which side of the diff it names -- by reading it back out of the
// request. Whether Bitbucket then attaches the comment to that line is the part
// that matters, and the part a mock cannot answer. An anchor the server rejects
// or quietly drops looks identical to one it honours.
func TestLiveInlineCommentAnchoring(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	const (
		branch = "feature/inline-comments"
		file   = "annotated.txt"
	)
	if err := harness.pushFileOnBranch(seeded.Key, repo.Slug, branch, file, "first\nsecond\nthird\n"); err != nil {
		t.Fatalf("push the annotated file failed: %v", err)
	}

	prID := createLivePRForRegression(t, branch, "Inline comments", "--no-default-reviewers", "--no-codeowners")

	var rootID string

	t.Run("an anchored comment comes back on its file", func(t *testing.T) {
		created := mustLiveCLI(t, "repo", "comment", "create", "--pr", prID,
			"--text", "anchored to the added line",
			"--path", file, "--line", "1", "--line-type", "ADDED")

		rootID = commentIDFrom(t, created)
		if rootID == "" {
			t.Fatalf("no comment id in:\n%s", created)
		}

		// Listing is what makes the anchor observable: an anchor the server
		// dropped leaves the comment off the file entirely.
		// The listing is a view on one file, not a collection, so it takes the
		// path the comment is anchored to (ADR-077).
		listing := mustLiveCLI(t, "repo", "comment", "list", "--pr", prID, "--path", file)
		if !strings.Contains(listing, "anchored to the added line") {
			t.Fatalf("the anchored comment is missing from the listing:\n%s", listing)
		}
		if !strings.Contains(listing, file) {
			t.Errorf("expected the anchor path %q in the listing:\n%s", file, listing)
		}
	})

	// #473: --limit on a comment listing documents a maximum, and did nothing.
	//
	// The service took it as a page size and read to exhaustion, so a smaller
	// --limit made more round trips and printed the same complete answer. The
	// unit test that covered this served two hand-written pages; asking for
	// fewer comments than a file has is the same question, and only the server
	// can say how many it has.
	t.Run("--limit caps the comments returned", func(t *testing.T) {
		const wanted = 4
		for index := range wanted {
			mustLiveCLI(t, "repo", "comment", "create", "--pr", prID,
				"--text", fmt.Sprintf("capped comment %d", index),
				"--path", file, "--line", "1", "--line-type", "ADDED")
		}

		all := mustLiveCLI(t, "repo", "comment", "list", "--pr", prID, "--path", file, "--all")
		if total := strings.Count(all, `"text"`); total <= wanted {
			t.Fatalf("the file carries %d comments, too few to cap:\n%s", total, all)
		}

		limited := mustLiveCLI(t, "repo", "comment", "list", "--pr", prID, "--path", file, "--limit", "3")
		if got := strings.Count(limited, `"text"`); got > 3 {
			t.Errorf("--limit 3 returned %d comments; the flag documents a maximum, not a page size:\n%s", got, limited)
		}
	})

	t.Run("a reply is attached to its parent", func(t *testing.T) {
		if rootID == "" {
			t.Skip("no root comment to reply to")
		}

		created := mustLiveCLI(t, "repo", "comment", "create", "--pr", prID,
			"--text", "a reply", "--parent", rootID)

		replyID := commentIDFrom(t, created)
		if replyID == "" || replyID == rootID {
			t.Fatalf("expected a distinct reply id, got %q from:\n%s", replyID, created)
		}

		// The reply has to hang off the thread, not start one. Listing threads
		// is where that shows.
		listing := mustLiveCLI(t, "pr", "comment", "list", prID)
		if !strings.Contains(listing, "a reply") {
			t.Fatalf("the reply is missing from the thread listing:\n%s", listing)
		}
	})

	t.Run("a comment resolves and reopens", func(t *testing.T) {
		if rootID == "" {
			t.Skip("no comment to resolve")
		}

		mustLiveCLI(t, "pr", "comment", "resolve", prID, rootID)
		if !liveCommentResolved(t, prID, rootID) {
			t.Fatal("expected the comment to be resolved")
		}

		mustLiveCLI(t, "pr", "comment", "reopen", prID, rootID)
		if liveCommentResolved(t, prID, rootID) {
			t.Fatal("expected the comment to be reopened")
		}
	})
}

func commentIDFrom(t *testing.T, output string) string {
	t.Helper()

	data := decodeJSONMap(t, output)
	if nested, ok := data["comment"].(map[string]any); ok {
		data = nested
	}
	if id, ok := data["id"]; ok {
		return trimNumeric(id)
	}

	return ""
}

// liveCommentResolved reads the comment back and reports whether the server
// considers it resolved.
//
// The state field is the answer, not the resolved one. Bitbucket sends
// threadResolved false beside state RESOLVED on a comment it has just
// resolved: the comment is resolved, the thread around it is not. Reading the
// wrong one made this test look like a resolve bug.
func liveCommentResolved(t *testing.T, prID, commentID string) bool {
	t.Helper()

	data := decodeJSONMap(t, mustLiveCLI(t, "pr", "comment", "get", prID, commentID))
	if nested, ok := data["comment"].(map[string]any); ok {
		data = nested
	}

	// State, not the resolved field: Bitbucket answers threadResolved false
	if state, ok := data["state"].(string); ok {
		return strings.EqualFold(state, "RESOLVED")
	}

	t.Fatalf("no state field on the comment: %v", data)

	return false
}

// TestLiveCommentVersionHandling covers the version an update and a delete
// carry, which several mocks asserted by reading the query they sent.
//
// The comment endpoints resolve a missing version already, so what is worth
// pinning is that both spellings work and that a stale one is still refused --
// the guard is the point of the version, and a resolution that silently
// overwrote it would look like success.
func TestLiveCommentVersionHandling(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	commits, err := harness.listCommitIDs(ctx, seeded.Key, repo.Slug, 1)
	if err != nil || len(commits) == 0 {
		t.Fatalf("could not read a commit to comment on: %v", err)
	}
	commit := commits[0]

	created := mustLiveCLI(t, "repo", "comment", "create", "--commit", commit, "--text", "version probe")
	id := commentIDFrom(t, created)

	t.Run("an update without a version resolves it", func(t *testing.T) {
		mustLiveCLI(t, "repo", "comment", "update", "--commit", commit, "--id", id, "--text", "updated once")
	})

	t.Run("a stale version is refused", func(t *testing.T) {
		output, err := executeLiveCLI(t, "--json", "repo", "comment", "update",
			"--commit", commit, "--id", id, "--text", "updated again", "--version", "0")
		if err == nil {
			t.Fatalf("expected a stale version to be refused, got:\n%s", output)
		}
	})

	t.Run("a delete without a version resolves it", func(t *testing.T) {
		mustLiveCLI(t, "repo", "comment", "delete", "--commit", commit, "--id", id)

		// An unanchored commit comment belongs to no file, so there is no
		// listing to read it out of. Fetching it by id is the check.
		output, err := executeLiveCLI(t, "--json", "repo", "comment", "get", "--commit", commit, "--id", id)
		if err == nil {
			t.Fatalf("the comment survived the delete:\n%s", output)
		}
	})
}
