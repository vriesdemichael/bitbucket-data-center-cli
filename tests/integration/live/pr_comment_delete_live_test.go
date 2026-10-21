//go:build live

package live_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// bb pr comment delete deletes a pull request comment or a task, which
// Bitbucket keeps as a comment (#785). Each delete is read back as gone, and
// each refusal Bitbucket makes -- a comment with replies, a stale version,
// somebody else's comment -- is predicted by the dry run and met by the real
// run with the same kind.
func TestLivePullRequestCommentDelete(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	branch := testsupport.UniqueName("lt-comment-delete-")
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "comment-delete.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}
	prID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
	if err != nil {
		t.Fatalf("create pull request failed: %v", err)
	}

	add := func(t *testing.T, args ...string) string {
		t.Helper()
		var added struct {
			Comment struct {
				ID any `json:"id"`
			} `json:"comment"`
		}
		decodeJSONData(t, mustLiveCLI(t, append([]string{"pr", "comment", "add", prID}, args...)...), &added)
		id, ok := numericOrStringID(added.Comment.ID)
		if !ok {
			t.Fatalf("the added comment has no id: %v", added.Comment.ID)
		}
		return id
	}
	requireGone := func(t *testing.T, id string) {
		t.Helper()
		if output, err := executeLiveCLI(t, "--json", "pr", "comment", "get", prID, id); !apperrors.IsKind(err, apperrors.KindNotFound) {
			t.Fatalf("comment %s reads back after its delete: %v\n%s", id, err, output)
		}
	}

	t.Run("a comment is deleted, as the dry run said", func(t *testing.T) {
		id := add(t, "--text", "deleted by the live suite")
		liveGoesThroughAsPredicted(t, jsonoutput.OutcomeWouldApply, "comment will be deleted",
			"pr", "comment", "delete", prID, id, "--yes")
		requireGone(t, id)
	})

	t.Run("a task is deleted", func(t *testing.T) {
		id := add(t, "--text", "a task for the live suite", "--blocker")
		var deleted struct {
			CommentID     string `json:"commentId"`
			PullRequestID string `json:"pullRequestId"`
		}
		decodeJSONData(t, mustLiveCLI(t, "pr", "comment", "delete", prID, id, "--yes"), &deleted)
		if deleted.CommentID != id || deleted.PullRequestID != prID {
			t.Errorf("the delete reported comment %q on %q, want %s on %s", deleted.CommentID, deleted.PullRequestID, id, prID)
		}
		requireGone(t, id)
	})

	t.Run("a comment with replies is refused until they are gone", func(t *testing.T) {
		parent := add(t, "--text", "a remark that gets an answer")
		reply := add(t, "--text", "the answer", "--parent-id", parent)

		liveVerdictHolds(t, apperrors.KindConflict, "pr", "comment", "delete", prID, parent, "--yes")
		// The refusal says why, so the person knows to delete the replies.
		if _, err := executeLiveCLI(t, "--json", "pr", "comment", "delete", prID, parent, "--yes"); err == nil || !strings.Contains(strings.ToLower(err.Error()), "repl") {
			t.Errorf("the refusal does not say the comment has replies: %v", err)
		}
		if output, err := executeLiveCLI(t, "--json", "pr", "comment", "get", prID, parent); err != nil {
			t.Fatalf("the refused comment does not read back: %v\n%s", err, output)
		}

		mustLiveCLI(t, "pr", "comment", "delete", prID, reply, "--yes")
		mustLiveCLI(t, "pr", "comment", "delete", prID, parent, "--yes")
		requireGone(t, reply)
		requireGone(t, parent)
	})

	t.Run("a stale version is refused", func(t *testing.T) {
		id := add(t, "--text", "edited after it was read")
		// Version 7 of a comment at version 0.
		liveVerdictHolds(t, apperrors.KindConflict, "pr", "comment", "delete", prID, id, "--version", "7", "--yes")
		if output, err := executeLiveCLI(t, "--json", "pr", "comment", "get", prID, id); err != nil {
			t.Fatalf("the refused comment does not read back: %v\n%s", err, output)
		}
	})

	// A token login has no configured username, so only Bitbucket can say the
	// comment is somebody else's.
	t.Run("somebody else's comment is refused to a non-admin on a token", func(t *testing.T) {
		id := add(t, "--text", "the administrator's remark, for a token")

		other, err := harness.createLicensedUser(ctx)
		if err != nil {
			t.Fatalf("create the other user failed: %v", err)
		}
		if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, other.Username, "REPO_WRITE"); err != nil {
			t.Fatalf("grant the other user write access failed: %v", err)
		}
		token, err := harness.liveJSONAs(ctx, other, http.MethodPut, "/rest/access-tokens/latest/users/"+url.PathEscape(userSlug(ctx, t, harness, other.Username)),
			map[string]any{"name": testsupport.UniqueName("lt-comment-delete-"), "permissions": []string{"REPO_WRITE"}, "expiryDays": 1})
		if err != nil {
			t.Fatalf("create the other user's token failed: %v", err)
		}
		setLiveCredentials(t, restrictedUser{Token: stringField(token, "token")})

		liveVerdictHolds(t, apperrors.KindAuthorization, "pr", "comment", "delete", prID, id, "--yes")
		if output, err := executeLiveCLI(t, "--json", "pr", "comment", "get", prID, id); err != nil {
			t.Fatalf("the refused comment does not read back: %v\n%s", err, output)
		}
	})

	t.Run("somebody else's comment is refused to a non-admin", func(t *testing.T) {
		id := add(t, "--text", "the administrator's remark")

		other, err := harness.createLicensedUser(ctx)
		if err != nil {
			t.Fatalf("create the other user failed: %v", err)
		}
		if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, other.Username, "REPO_WRITE"); err != nil {
			t.Fatalf("grant the other user write access failed: %v", err)
		}
		setLiveCredentials(t, other)

		liveVerdictHolds(t, apperrors.KindAuthorization, "pr", "comment", "delete", prID, id, "--yes")
		if output, err := executeLiveCLI(t, "--json", "pr", "comment", "get", prID, id); err != nil {
			t.Fatalf("the refused comment does not read back: %v\n%s", err, output)
		}
	})

	t.Run("a comment that is not there is not found", func(t *testing.T) {
		liveVerdictHolds(t, apperrors.KindNotFound, "pr", "comment", "delete", prID, fmt.Sprintf("%d", 999999999), "--yes")
	})

}
