//go:build live

package live_test

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
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
	// The branch rewrites the middle line of a file master already has, so the
	// diff carries unchanged lines around the change, and the comments here
	// anchor to the first of them. ADDED is what bb sends without --line-type,
	// and FROM is the file type Bitbucket stores for a CONTEXT line given none,
	// so CONTEXT and TO read back only if both were sent and kept.
	if err := harness.pushFileOnBranch(seeded.Key, repo.Slug, "master", file, "first\nsecond\nthird\n"); err != nil {
		t.Fatalf("push the original file failed: %v", err)
	}
	if err := harness.pushFileOnBranch(seeded.Key, repo.Slug, branch, file, "first\nchanged\nthird\n"); err != nil {
		t.Fatalf("push the annotated file failed: %v", err)
	}
	onContextLine := func(text string) map[string]any {
		return map[string]any{"text": text, "anchor.path": file, "anchor.line": float64(1), "anchor.lineType": "CONTEXT", "anchor.fileType": "TO"}
	}

	prID := createLivePRForRegression(t, branch, "Inline comments", "--no-default-reviewers", "--no-codeowners")

	var rootID string

	t.Run("an anchored comment comes back on its file", func(t *testing.T) {
		// bb always sends diffType EFFECTIVE, which Bitbucket also stores when none is sent, so no read shows it arrived.
		created := mustLiveCLI(t, "repo", "comment", "create", "--pr", prID,
			"--text", "anchored to a context line",
			"--path", file, "--line", "1", "--line-type", "CONTEXT")

		rootID = commentIDFrom(t, created)
		if rootID == "" {
			t.Fatalf("no comment id in:\n%s", created)
		}

		// Listing is what makes the anchor observable: an anchor the server
		// dropped leaves the comment off the file entirely.
		// The listing is a view on one file, not a collection, so it takes the
		// path the comment is anchored to (ADR-077).
		listing := mustLiveCLI(t, "repo", "comment", "list", "--pr", prID, "--path", file)
		if !strings.Contains(listing, "anchored to a context line") {
			t.Fatalf("the anchored comment is missing from the listing:\n%s", listing)
		}
		if !strings.Contains(listing, file) {
			t.Errorf("expected the anchor path %q in the listing:\n%s", file, listing)
		}

		// Only the whole anchor, though. One that loses just its line type is
		// kept on the file with no line at all, so it is the fields that tell.
		stored, ok := commentListedWithID(commentsListedOnFile(t, listing), rootID)
		if !ok {
			t.Fatalf("comment %s is not in the listing:\n%s", rootID, listing)
		}
		assertCommentReadBack(t, stored, onContextLine("anchored to a context line"))
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
		capped := map[string]string{}
		for index := range wanted {
			text := fmt.Sprintf("capped comment %d", index)
			// diffType EFFECTIVE, as above: not readable as sent.
			created := mustLiveCLI(t, "repo", "comment", "create", "--pr", prID,
				"--text", text,
				"--path", file, "--line", "1", "--line-type", "CONTEXT")
			capped[commentIDFrom(t, created)] = text
		}

		all := mustLiveCLI(t, "repo", "comment", "list", "--pr", prID, "--path", file, "--all")
		if total := strings.Count(all, `"text"`); total <= wanted {
			t.Fatalf("the file carries %d comments, too few to cap:\n%s", total, all)
		}

		// Read field by field: the comment above and the four just written, each
		// with its own text on the line it was sent to.
		listed := commentsListedOnFile(t, all)
		if len(listed) != wanted+1 {
			t.Fatalf("the file carries %d comments, want %d:\n%s", len(listed), wanted+1, all)
		}
		for id, text := range capped {
			stored, ok := commentListedWithID(listed, id)
			if !ok {
				t.Errorf("comment %s (%q) is not listed on the file", id, text)
				continue
			}
			assertCommentReadBack(t, stored, onContextLine(text))
		}

		limited := mustLiveCLI(t, "repo", "comment", "list", "--pr", prID, "--path", file, "--limit", "3")
		if got := strings.Count(limited, `"text"`); got > 3 {
			t.Errorf("--limit 3 returned %d comments; the flag documents a maximum, not a page size:\n%s", got, limited)
		}
		// Exactly three, each one the file has: a bound alone passed with none.
		kept := commentsListedOnFile(t, limited)
		if len(kept) != 3 {
			t.Errorf("--limit 3 returned %d comments, want 3:\n%s", len(kept), limited)
		}
		for _, comment := range kept {
			if _, ok := commentListedWithID(listed, trimNumeric(comment["id"])); !ok {
				t.Errorf("--limit 3 returned comment %v, which the file does not carry", comment["id"])
			}
		}
	})

	t.Run("a reply is attached to its parent", func(t *testing.T) {
		if rootID == "" {
			t.Fatal("the anchored comment above was never created, so there is no parent to reply to. This used to skip, which reported the earlier failure once and dropped this case without saying it had gone.")
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

		// A read of the reply alone does not carry its parent. The listing on the
		// file does: Bitbucket nests a reply under its root and bb reports the
		// nesting as parentId, where a parent that was dropped would leave the
		// reply off the file altogether.
		onFile := mustLiveCLI(t, "repo", "comment", "list", "--pr", prID, "--path", file)
		stored, ok := commentListedWithID(commentsListedOnFile(t, onFile), replyID)
		if !ok {
			t.Fatalf("reply %s is not listed on the file its parent is anchored to:\n%s", replyID, onFile)
		}
		assertCommentReadBack(t, stored, map[string]any{"text": "a reply", "parentId": commentIDAsJSONNumber(t, rootID), "reply": true})
	})

	t.Run("a comment resolves and reopens", func(t *testing.T) {
		if rootID == "" {
			t.Fatal("the anchored comment above was never created, so there is nothing to resolve. This used to skip, which dropped the case silently.")
		}

		mustLiveCLI(t, "pr", "comment", "resolve", prID, rootID)
		if !liveCommentResolved(t, prID, rootID) {
			t.Fatal("expected the comment to be resolved")
		}

		mustLiveCLI(t, "pr", "comment", "reopen", prID, rootID)
		if liveCommentResolved(t, prID, rootID) {
			t.Fatal("expected the comment to be reopened")
		}
		// Not resolved is any state but RESOLVED; OPEN is the one reopen sends.
		if state := commentStoredOnPR(t, prID, rootID)["state"]; state != "OPEN" {
			t.Fatalf("state after reopening = %v, want OPEN", state)
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

// commentsListedOnFile reads the comments out of a comment listing: `repo
// comment list`, or `pr comment list --full`.
func commentsListedOnFile(t *testing.T, output string) []map[string]any {
	t.Helper()

	entries, ok := decodeJSONMap(t, output)["comments"].([]any)
	if !ok {
		t.Fatalf("no comments in the listing:\n%s", output)
	}

	comments := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		if comment, ok := entry.(map[string]any); ok {
			comments = append(comments, comment)
		}
	}

	return comments
}

// commentListedWithID finds one comment in a listing by id.
func commentListedWithID(comments []map[string]any, id string) (map[string]any, bool) {
	for _, comment := range comments {
		if trimNumeric(comment["id"]) == id {
			return comment, true
		}
	}

	return nil, false
}

// commentIDAsJSONNumber is a comment id as a decoded JSON field holds it.
func commentIDAsJSONNumber(t *testing.T, id string) float64 {
	t.Helper()

	number, err := strconv.ParseFloat(id, 64)
	if err != nil {
		t.Fatalf("comment id %q is not a number: %v", id, err)
	}

	return number
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

	// No bb command reads one commit comment, and an unanchored one is on no
	// file a listing could find it in, so reads go to the comment through bb api.
	commentPath := fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/commits/%s/comments/%s", seeded.Key, repo.Slug, commit, id)
	assertCommentReadBack(t, decodeJSONMap(t, mustLiveCLI(t, "api", commentPath)), map[string]any{
		"text": "version probe", "version": float64(0),
	})

	t.Run("an update without a version resolves it", func(t *testing.T) {
		mustLiveCLI(t, "repo", "comment", "update", "--commit", commit, "--id", id, "--text", "updated once")

		assertCommentReadBack(t, decodeJSONMap(t, mustLiveCLI(t, "api", commentPath)), map[string]any{
			"text": "updated once", "version": float64(1),
		})
	})

	t.Run("a stale version is refused", func(t *testing.T) {
		output, err := executeLiveCLI(t, "--json", "repo", "comment", "update",
			"--commit", commit, "--id", id, "--text", "updated again", "--version", "0")
		if err == nil {
			t.Fatalf("expected a stale version to be refused, got:\n%s", output)
		}
		// Refused as a conflict, and refused outright: the comment still holds
		// what the update above left. A version bb did not send would have been
		// resolved to the current one, and the update would have gone through.
		if code := apperrors.ExitCode(err); code != 5 {
			t.Errorf("a stale version exited %d, want 5 (conflict):\n%s", code, output)
		}
		assertCommentReadBack(t, decodeJSONMap(t, mustLiveCLI(t, "api", commentPath)), map[string]any{
			"text": "updated once", "version": float64(1),
		})
	})

	t.Run("a delete without a version resolves it", func(t *testing.T) {
		mustLiveCLI(t, "repo", "comment", "delete", "--commit", commit, "--id", id, "--yes")

		// An unanchored commit comment belongs to no file, so there is no
		// listing to read it out of. Fetching it by id is the check -- through
		// bb api, since the repo comment get this used to call does not exist
		// and failed on its unknown --id flag whether or not the delete worked.
		output, err := executeLiveCLI(t, "--json", "api", commentPath)
		if err == nil {
			t.Fatalf("the comment survived the delete:\n%s", output)
		}
		if code := apperrors.ExitCode(err); code != 4 {
			t.Errorf("reading the deleted comment exited %d, want 4 (not found):\n%s", code, output)
		}
	})
}
