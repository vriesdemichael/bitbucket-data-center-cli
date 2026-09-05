//go:build live

package live_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	commentservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/comment"
)

func TestLiveCommentFlowCommit(t *testing.T) {
	harness := newLiveHarness(t)
	service := commentservice.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 2)
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

	fetched, err := service.Get(ctx, target, fmt.Sprintf("%d", *created.Id))
	if err != nil {
		t.Fatalf("get commit comment failed: %v", err)
	}
	if fetched.Id == nil || *fetched.Id != *created.Id {
		t.Fatalf("expected fetched commit comment id=%d, got %#v", *created.Id, fetched.Id)
	}

	updated, err := service.Update(ctx, target, fmt.Sprintf("%d", *created.Id), "live commit comment updated", nil)
	if err != nil {
		t.Fatalf("update commit comment failed: %v", err)
	}
	if updated.Text == nil || *updated.Text != "live commit comment updated" {
		t.Fatalf("expected updated text, got: %#v", updated.Text)
	}

	if _, err := service.Delete(ctx, target, fmt.Sprintf("%d", *created.Id), nil); err != nil {
		t.Fatalf("delete commit comment failed: %v", err)
	}
}

func TestLiveCommentFlowPullRequest(t *testing.T) {
	harness := newLiveHarness(t)
	service := commentservice.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	branch := fmt.Sprintf("lt-comment-%d", time.Now().UnixNano()%100000)
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

	fetched, err := service.Get(ctx, target, fmt.Sprintf("%d", *created.Id))
	if err != nil {
		t.Fatalf("get pull request comment failed: %v", err)
	}
	if fetched.Id == nil || *fetched.Id != *created.Id {
		t.Fatalf("expected fetched pull request comment id=%d, got %#v", *created.Id, fetched.Id)
	}

	updated, err := service.Update(ctx, target, fmt.Sprintf("%d", *created.Id), "live pull request comment updated", nil)
	if err != nil {
		t.Fatalf("update pull request comment failed: %v", err)
	}
	if updated.Text == nil || *updated.Text != "live pull request comment updated" {
		t.Fatalf("expected updated text, got: %#v", updated.Text)
	}

	if _, err := service.Delete(ctx, target, fmt.Sprintf("%d", *created.Id), nil); err != nil {
		t.Fatalf("delete pull request comment failed: %v", err)
	}
}

func TestLiveBlockerCommentReactionsAndSuggestionsFlow(t *testing.T) {
	harness := newLiveHarness(t)
	service := commentservice.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	repo := seeded.Repos[0]
	branch := fmt.Sprintf("lt-blocker-%d", time.Now().UnixNano()%100000)
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

	// 1. Create Blocker Comment
	created, err := service.Create(ctx, target, "this is a blocker comment")
	if err != nil {
		t.Fatalf("create blocker comment failed: %v", err)
	}
	if created.Id == nil {
		t.Fatal("created blocker comment missing id")
	}

	// 2. Get Blocker Comment
	fetched, err := service.Get(ctx, target, fmt.Sprintf("%d", *created.Id))
	if err != nil {
		t.Fatalf("get blocker comment failed: %v", err)
	}
	if fetched.Id == nil || *fetched.Id != *created.Id {
		t.Fatalf("expected fetched blocker comment id=%d, got %v", *created.Id, fetched.Id)
	}

	// 3. List Blocker Comments
	list, err := service.List(ctx, target, "", 25)
	if err != nil || len(list) == 0 {
		t.Fatalf("list blocker comments failed: %v (len=%d)", err, len(list))
	}

	// 4. Update Blocker Comment
	updated, err := service.Update(ctx, target, fmt.Sprintf("%d", *created.Id), "updated blocker comment", nil)
	if err != nil {
		t.Fatalf("update blocker comment failed: %v", err)
	}
	if updated.Text == nil || *updated.Text != "updated blocker comment" {
		t.Fatalf("expected updated text, got %v", updated.Text)
	}

	// 5. Add Reaction
	reaction, err := service.React(ctx, target.Repository, pullRequestID, fmt.Sprintf("%d", *created.Id), "thumbsup")
	if err != nil {
		t.Fatalf("add reaction failed: %v", err)
	}
	if reaction.Emoticon == nil || *reaction.Emoticon.Shortcut != "thumbsup" {
		t.Fatalf("expected thumbsup reaction, got %v", reaction)
	}

	// 6. Remove Reaction
	err = service.UnReact(ctx, target.Repository, pullRequestID, fmt.Sprintf("%d", *created.Id), "thumbsup")
	if err != nil {
		t.Fatalf("remove reaction failed: %v", err)
	}

	// 7. Delete Blocker Comment
	if _, err := service.Delete(ctx, target, fmt.Sprintf("%d", *created.Id), nil); err != nil {
		t.Fatalf("delete blocker comment failed: %v", err)
	}
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
	harness := newLiveHarness(t)
	service := commentservice.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]

	branch := fmt.Sprintf("lt-comment-state-%d", time.Now().UnixNano()%100000)
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

		reopened, err := service.SetState(ctx, target, commentID, commentservice.CommentStateOpen, nil)
		if err != nil {
			t.Fatalf("reopen comment failed: %v", err)
		}
		if state := safeCommentState(reopened); state != "OPEN" {
			t.Fatalf("state = %q after reopening, want OPEN", state)
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
	})
}

func safeCommentState(comment openapigenerated.RestComment) string {
	if comment.State == nil {
		return ""
	}

	return *comment.State
}
