//go:build live

package live_test

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/safederef"
	commentservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/comment"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

func TestLiveCommentFlowCommit(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	service := commentservice.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{Commits: 2, WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	target := commentservice.Target{
		Repository: commentservice.RepositoryRef{ProjectKey: seeded.Key, Slug: repo.Slug},
		CommitID:   repo.CommitIDs[0],
	}

	created, err := service.Create(ctx, target, "live commit comment")
	if err != nil {
		t.Fatalf("create commit comment failed: %v", err)
	}
	if created.Id == nil {
		t.Fatal("created commit comment missing id")
	}
	commentID := fmt.Sprintf("%d", *created.Id)

	fetched, err := service.Get(ctx, target, commentID)
	if err != nil {
		t.Fatalf("get commit comment failed: %v", err)
	}
	if fetched.Id == nil || *fetched.Id != *created.Id {
		t.Fatalf("expected fetched commit comment id=%d, got %#v", *created.Id, fetched.Id)
	}
	// The id is what the read was addressed by. The text is what the create sent.
	assertRestCommentReadBack(t, fetched, "live commit comment", 0)

	// Bitbucket finds a commit comment only through the commit it is on, so
	// this read failing is what places the comment on the commit named above.
	elsewhere := target
	elsewhere.CommitID = repo.CommitIDs[1]
	if _, err := service.Get(ctx, elsewhere, commentID); !apperrors.IsKind(err, apperrors.KindNotFound) {
		t.Errorf("comment %s read through another commit answered %v, want not found", commentID, err)
	}

	updated, err := service.Update(ctx, target, commentID, "live commit comment updated", nil)
	if err != nil {
		t.Fatalf("update commit comment failed: %v", err)
	}
	if updated.Text == nil || *updated.Text != "live commit comment updated" {
		t.Fatalf("expected updated text, got: %#v", updated.Text)
	}

	// Update answers with the write's own response, and with its request when
	// the response carries no comment, so only a read says what was stored.
	reread, err := service.Get(ctx, target, commentID)
	if err != nil {
		t.Fatalf("re-read the updated commit comment failed: %v", err)
	}
	assertRestCommentReadBack(t, reread, "live commit comment updated", 1)

	if _, err := service.Delete(ctx, target, commentID, nil); err != nil {
		t.Fatalf("delete commit comment failed: %v", err)
	}
	if _, err := service.Get(ctx, target, commentID); !apperrors.IsKind(err, apperrors.KindNotFound) {
		t.Errorf("comment %s read after its delete answered %v, want not found", commentID, err)
	}
}

func TestLiveCommentFlowPullRequest(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	service := commentservice.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	branch := testsupport.UniqueName("lt-comment-")
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "comment-feature.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	pullRequestID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
	if err != nil {
		t.Fatalf("create pull request failed: %v", err)
	}

	target := commentservice.Target{
		Repository:    commentservice.RepositoryRef{ProjectKey: seeded.Key, Slug: repo.Slug},
		PullRequestID: pullRequestID,
	}

	created, err := service.Create(ctx, target, "live pull request comment")
	if err != nil {
		t.Fatalf("create pull request comment failed: %v", err)
	}
	if created.Id == nil {
		t.Fatal("created pull request comment missing id")
	}
	commentID := fmt.Sprintf("%d", *created.Id)

	fetched, err := service.Get(ctx, target, commentID)
	if err != nil {
		t.Fatalf("get pull request comment failed: %v", err)
	}
	if fetched.Id == nil || *fetched.Id != *created.Id {
		t.Fatalf("expected fetched pull request comment id=%d, got %#v", *created.Id, fetched.Id)
	}
	assertCommentReadBack(t, commentStoredOnPR(t, pullRequestID, commentID), map[string]any{
		"text": "live pull request comment", "version": float64(0),
	})

	updated, err := service.Update(ctx, target, commentID, "live pull request comment updated", nil)
	if err != nil {
		t.Fatalf("update pull request comment failed: %v", err)
	}
	if updated.Text == nil || *updated.Text != "live pull request comment updated" {
		t.Fatalf("expected updated text, got: %#v", updated.Text)
	}
	assertCommentReadBack(t, commentStoredOnPR(t, pullRequestID, commentID), map[string]any{
		"text": "live pull request comment updated", "version": float64(1),
	})

	if _, err := service.Delete(ctx, target, commentID, nil); err != nil {
		t.Fatalf("delete pull request comment failed: %v", err)
	}
	assertPRCommentGone(t, pullRequestID, commentID)
}

func TestLiveBlockerCommentReactionsAndSuggestionsFlow(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	service := commentservice.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	branch := testsupport.UniqueName("lt-blocker-")
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "blocker-feature.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	pullRequestID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
	if err != nil {
		t.Fatalf("create pull request failed: %v", err)
	}

	target := commentservice.Target{
		Repository:    commentservice.RepositoryRef{ProjectKey: seeded.Key, Slug: repo.Slug},
		PullRequestID: pullRequestID,
		Blocker:       true,
	}

	// 0. An ordinary comment, before any blocker, for the blocker listing to
	// leave out. The listing is oldest first, so one that did not keep to
	// blockers would lead with this.
	ordinaryTarget := target
	ordinaryTarget.Blocker = false
	ordinary, err := service.Create(ctx, ordinaryTarget, "an ordinary comment beside the blockers")
	if err != nil || ordinary.Id == nil {
		t.Fatalf("create the ordinary comment failed: %v", err)
	}
	assertCommentReadBack(t, commentStoredOnPR(t, pullRequestID, fmt.Sprintf("%d", *ordinary.Id)), map[string]any{
		"text": "an ordinary comment beside the blockers", "severity": "NORMAL",
	})

	// 1. Create Blocker Comment
	created, err := service.Create(ctx, target, "this is a blocker comment")
	if err != nil {
		t.Fatalf("create blocker comment failed: %v", err)
	}
	if created.Id == nil {
		t.Fatal("created blocker comment missing id")
	}
	blockerID := fmt.Sprintf("%d", *created.Id)

	// The body is the one an ordinary comment sends; BLOCKER is the blocker
	// endpoint's doing, and NORMAL what the comment would otherwise be. Read
	// through the ordinary endpoint, since the blocker one answers for any
	// comment it is asked about.
	assertCommentReadBack(t, commentStoredOnPR(t, pullRequestID, blockerID), map[string]any{
		"text": "this is a blocker comment", "severity": "BLOCKER",
	})

	// 2. Get Blocker Comment
	fetched, err := service.Get(ctx, target, blockerID)
	if err != nil {
		t.Fatalf("get blocker comment failed: %v", err)
	}
	if fetched.Id == nil || *fetched.Id != *created.Id {
		t.Fatalf("expected fetched blocker comment id=%d, got %v", *created.Id, fetched.Id)
	}

	// 3. List Blocker Comments
	//
	// A second blocker, so that a cap of one has a blocker to leave out.
	second, err := service.Create(ctx, target, "a second blocker comment")
	if err != nil || second.Id == nil {
		t.Fatalf("create the second blocker comment failed: %v", err)
	}
	assertCommentReadBack(t, commentStoredOnPR(t, pullRequestID, fmt.Sprintf("%d", *second.Id)), map[string]any{
		"text": "a second blocker comment", "severity": "BLOCKER",
	})

	list, err := service.List(ctx, target, "", 1)
	if err != nil || len(list) == 0 {
		t.Fatalf("list blocker comments failed: %v (len=%d)", err, len(list))
	}
	// The first blocker alone: the ordinary comment ahead of it is not a
	// blocker, and the second blocker is past the cap.
	if ids := restCommentIDs(list); !slices.Equal(ids, []int64{*created.Id}) {
		t.Errorf("blocker listing capped at one returned %v, want [%d]", ids, *created.Id)
	}

	// 4. Update Blocker Comment
	updated, err := service.Update(ctx, target, blockerID, "updated blocker comment", nil)
	if err != nil {
		t.Fatalf("update blocker comment failed: %v", err)
	}
	if updated.Text == nil || *updated.Text != "updated blocker comment" {
		t.Fatalf("expected updated text, got %v", updated.Text)
	}
	assertCommentReadBack(t, commentStoredOnPR(t, pullRequestID, blockerID), map[string]any{
		"text": "updated blocker comment", "version": float64(1), "severity": "BLOCKER",
	})

	// 5. Add Reaction
	reaction, err := service.React(ctx, target.Repository, pullRequestID, blockerID, "thumbsup")
	if err != nil {
		t.Fatalf("add reaction failed: %v", err)
	}
	if reaction.Emoticon == nil || *reaction.Emoticon.Shortcut != "thumbsup" {
		t.Fatalf("expected thumbsup reaction, got %v", reaction)
	}
	// The answer above is the reaction endpoint repeating it. The comment
	// carries its reactions among its properties.
	if users := commentReactionUsers(commentStoredOnPR(t, pullRequestID, blockerID), "thumbsup"); !slices.Equal(users, []string{harness.username()}) {
		t.Errorf("thumbsup on comment %s is from %v after reacting, want [%s]", blockerID, users, harness.username())
	}

	// 6. Remove Reaction
	err = service.UnReact(ctx, target.Repository, pullRequestID, blockerID, "thumbsup")
	if err != nil {
		t.Fatalf("remove reaction failed: %v", err)
	}
	if users := commentReactionUsers(commentStoredOnPR(t, pullRequestID, blockerID), "thumbsup"); len(users) != 0 {
		t.Errorf("thumbsup on comment %s is still from %v after removing it", blockerID, users)
	}

	// 7. Delete Blocker Comment
	if _, err := service.Delete(ctx, target, blockerID, nil); err != nil {
		t.Fatalf("delete blocker comment failed: %v", err)
	}
	assertPRCommentGone(t, pullRequestID, blockerID)
}

// TestLiveCommentStateAndPending covers the two comment operations that had no
// live coverage at all: moving a comment between OPEN and RESOLVED, and
// creating one that is still a draft.
//
// Both were unit tests over a mock that answered whatever state the fixture
// named, so the assertion was that bb echoed the fixture back. Whether
// Bitbucket accepts the state on that endpoint, and whether it holds, is the
// question -- and resolving a comment is what an agent does at the end of a
// review, so a silent failure here is expensive.
func TestLiveCommentStateAndPending(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	service := commentservice.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	branch := testsupport.UniqueName("lt-comment-state-")
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "state.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	pullRequestID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
	if err != nil {
		t.Fatalf("create pull request failed: %v", err)
	}

	target := commentservice.Target{
		Repository:    commentservice.RepositoryRef{ProjectKey: seeded.Key, Slug: repo.Slug},
		PullRequestID: pullRequestID,
	}

	t.Run("a comment can be resolved and opened again", func(t *testing.T) {
		created, err := service.Create(ctx, target, "needs a second look")
		if err != nil {
			t.Fatalf("create comment failed: %v", err)
		}
		commentID := fmt.Sprintf("%d", *created.Id)

		resolved, err := service.SetState(ctx, target, commentID, commentservice.CommentStateResolved, nil)
		if err != nil {
			t.Fatalf("resolve comment failed: %v", err)
		}
		if state := safeCommentState(resolved); state != "RESOLVED" {
			t.Fatalf("state = %q after resolving, want RESOLVED", state)
		}

		// Read it back rather than trusting the write's own answer: the two
		// disagreeing is exactly the failure a mock cannot show.
		fetched, err := service.Get(ctx, target, commentID)
		if err != nil {
			t.Fatalf("re-read the comment failed: %v", err)
		}
		if state := safeCommentState(fetched); state != "RESOLVED" {
			t.Fatalf("the resolution did not stick, state = %q", state)
		}
		if text := safederef.String(fetched.Text); text != "needs a second look" {
			t.Errorf("stored text = %q, want %q", text, "needs a second look")
		}

		reopened, err := service.SetState(ctx, target, commentID, commentservice.CommentStateOpen, nil)
		if err != nil {
			t.Fatalf("reopen comment failed: %v", err)
		}
		if state := safeCommentState(reopened); state != "OPEN" {
			t.Fatalf("state = %q after reopening, want OPEN", state)
		}
		// The same goes for reopening, and OPEN is also what SetState answers
		// with when the response carries no comment.
		if state := commentStoredOnPR(t, pullRequestID, commentID)["state"]; state != "OPEN" {
			t.Fatalf("the reopen did not stick, state = %v", state)
		}
	})

	t.Run("a blocker comment can be resolved", func(t *testing.T) {
		// A task is a blocker comment, and resolving one is how a review is
		// signed off. It travels the same endpoint but a different payload.
		blockerTarget := target
		blockerTarget.Blocker = true

		created, err := service.Create(ctx, blockerTarget, "fix this before merging")
		if err != nil {
			t.Fatalf("create blocker comment failed: %v", err)
		}

		resolved, err := service.SetState(ctx, target, fmt.Sprintf("%d", *created.Id), commentservice.CommentStateResolved, nil)
		if err != nil {
			t.Fatalf("resolve blocker comment failed: %v", err)
		}
		if state := safeCommentState(resolved); state != "RESOLVED" {
			t.Fatalf("state = %q after resolving a blocker, want RESOLVED", state)
		}
		// Read back, and still the blocker it was created as.
		assertCommentReadBack(t, commentStoredOnPR(t, pullRequestID, fmt.Sprintf("%d", *created.Id)), map[string]any{
			"state": "RESOLVED", "severity": "BLOCKER", "text": "fix this before merging",
		})
	})

	t.Run("a pending comment is created as a draft", func(t *testing.T) {
		pendingTarget := target
		pendingTarget.Pending = true

		created, err := service.Create(ctx, pendingTarget, "a draft review note")
		if err != nil {
			t.Fatalf("create pending comment failed: %v", err)
		}
		if state := safeCommentState(created); state != "PENDING" {
			t.Fatalf("state = %q, want PENDING for a draft comment", state)
		}
		// Create's answer falls back to its request, which says PENDING too. A
		// state that did not arrive would read back as OPEN.
		assertCommentReadBack(t, commentStoredOnPR(t, pullRequestID, fmt.Sprintf("%d", *created.Id)), map[string]any{
			"state": "PENDING", "text": "a draft review note",
		})
	})
}

func safeCommentState(comment openapigenerated.RestComment) string {
	if comment.State == nil {
		return ""
	}

	return *comment.State
}

// commentStoredOnPR reads a pull request comment back through `bb pr comment
// get`, the command a caller asks what Bitbucket kept.
func commentStoredOnPR(t *testing.T, prID, commentID string) map[string]any {
	t.Helper()

	return nestedJSONMap(t, mustLiveCLI(t, "pr", "comment", "get", prID, commentID), "comment")
}

// assertCommentReadBack compares fields of a comment read back with the values
// that were written. The values are scalars, and a number is a float64 because
// that is what JSON decodes it to.
func assertCommentReadBack(t *testing.T, comment map[string]any, want map[string]any) {
	t.Helper()

	for field, value := range want {
		if comment[field] != value {
			t.Errorf("comment %v: %s = %#v, want %#v", comment["id"], field, comment[field], value)
		}
	}
}

// assertRestCommentReadBack is assertCommentReadBack for a comment read through
// the comment service.
func assertRestCommentReadBack(t *testing.T, comment openapigenerated.RestComment, text string, version int32) {
	t.Helper()

	id := safederef.Int64(comment.Id)
	if comment.Text == nil || *comment.Text != text {
		t.Errorf("comment %d: text = %q, want %q", id, safederef.String(comment.Text), text)
	}
	if comment.Version == nil {
		t.Errorf("comment %d: no version, want %d", id, version)
	} else if *comment.Version != version {
		t.Errorf("comment %d: version = %d, want %d", id, *comment.Version, version)
	}
}

// assertPRCommentGone reads a deleted pull request comment and expects not found.
func assertPRCommentGone(t *testing.T, prID, commentID string) {
	t.Helper()

	output, err := executeLiveCLI(t, "--json", "pr", "comment", "get", prID, commentID)
	if code := apperrors.ExitCode(err); code != 4 {
		t.Errorf("reading comment %s after its delete exited %d, want 4 (not found):\n%s", commentID, code, output)
	}
}

// commentReactionUsers names who reacted to a comment with one emoticon, from
// the properties Bitbucket keeps reactions in.
func commentReactionUsers(comment map[string]any, shortcut string) []string {
	properties, _ := comment["properties"].(map[string]any)
	reactions, _ := properties["reactions"].([]any)

	var users []string
	for _, entry := range reactions {
		reaction, _ := entry.(map[string]any)
		emoticon, _ := reaction["emoticon"].(map[string]any)
		if emoticon["shortcut"] != shortcut {
			continue
		}
		reacted, _ := reaction["users"].([]any)
		for _, user := range reacted {
			if fields, ok := user.(map[string]any); ok {
				users = append(users, asString(fields["name"]))
			}
		}
	}

	return users
}

// restCommentIDs lists the ids of comments the comment service returned, in
// order.
func restCommentIDs(comments []openapigenerated.RestComment) []int64 {
	ids := make([]int64, 0, len(comments))
	for _, comment := range comments {
		if comment.Id != nil {
			ids = append(ids, *comment.Id)
		}
	}

	return ids
}
