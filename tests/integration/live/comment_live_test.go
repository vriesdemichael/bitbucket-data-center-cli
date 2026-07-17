//go:build live

package live_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	commentservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/comment"
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
