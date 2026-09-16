//go:build live

package live_test

import (
	"context"
	stderrors "errors"
	"strings"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/safederef"
	tagservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/tag"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

func TestLiveTagLifecycle(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	service := tagservice.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{Commits: 2, WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	tagName := testsupport.UniqueName("v-live-")

	// The older of the two commits: at the tip, a start point that never
	// reached Bitbucket could not be told from one that did.
	if len(repo.CommitIDs) < 2 {
		t.Fatalf("want two seeded commits, got %v", repo.CommitIDs)
	}
	startPoint := repo.CommitIDs[1]
	const message = "live test tag"

	created, err := service.Create(
		ctx,
		tagservice.RepositoryRef{ProjectKey: seeded.Key, Slug: repo.Slug},
		tagName,
		startPoint,
		message,
	)
	if err != nil {
		t.Fatalf("create tag failed: %v", err)
	}
	if created.DisplayId == nil || *created.DisplayId == "" {
		t.Fatalf("created tag display id missing: %#v", created)
	}

	fetched, err := service.Get(ctx, tagservice.RepositoryRef{ProjectKey: seeded.Key, Slug: repo.Slug}, tagName)
	if err != nil {
		t.Fatalf("get tag failed: %v", err)
	}
	if fetched.DisplayId == nil || *fetched.DisplayId != tagName {
		t.Fatalf("expected fetched tag=%s, got %#v", tagName, fetched.DisplayId)
	}
	if commit := safederef.String(fetched.LatestCommit); commit != startPoint {
		t.Errorf("the tag points at %q, want the start point %s", commit, startPoint)
	}
	if stored := readLiveTagMessage(t, harness, seeded.Key, repo.Slug, tagName); stored != message {
		t.Errorf("the tag's message is %q, want %q", stored, message)
	}

	if err := service.Delete(ctx, tagservice.RepositoryRef{ProjectKey: seeded.Key, Slug: repo.Slug}, tagName); err != nil {
		t.Fatalf("delete tag failed: %v", err)
	}

	_, err = service.Get(ctx, tagservice.RepositoryRef{ProjectKey: seeded.Key, Slug: repo.Slug}, tagName)
	if err == nil {
		t.Fatalf("expected not found error after tag delete")
	}

	var appErr *errors.AppError
	if !stderrors.As(err, &appErr) || appErr.Kind != errors.KindNotFound {
		t.Fatalf("expected not_found error, got: %v", err)
	}
}

// readLiveTagMessage fetches a tag into an empty repository and reads its
// message there.
//
// Git is the only place Bitbucket gives the message back. The REST tag names
// the commit and the tag object's hash, and nothing the object says -- and
// every tag Bitbucket creates is an annotated object, with a message or
// without, so the hash being there does not show that one was kept either.
func readLiveTagMessage(t *testing.T, harness *liveHarness, projectKey, repositorySlug, tagName string) string {
	t.Helper()

	directory := t.TempDir()
	if err := runGit(directory, "init"); err != nil {
		t.Fatalf("git init failed: %v", err)
	}

	fetchURL, err := repositoryPushURL(harness.config, projectKey, repositorySlug)
	if err != nil {
		t.Fatalf("repository URL: %v", err)
	}

	ref := "refs/tags/" + tagName
	if err := runGit(directory, "fetch", fetchURL, ref+":"+ref); err != nil {
		t.Fatalf("fetch %s failed: %v", ref, err)
	}

	message, err := runGitCapture(directory, "for-each-ref", "--format=%(contents)", ref)
	if err != nil {
		t.Fatalf("read the message of %s: %v", ref, err)
	}

	return strings.TrimSpace(message)
}
