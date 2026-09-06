//go:build live

package live_test

import (
	"context"
	stderrors "errors"
	"fmt"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	tagservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/tag"
)

func TestLiveTagLifecycle(t *testing.T) {
	harness := newLiveHarness(t)
	service := tagservice.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{Commits: 2, WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	tagName := fmt.Sprintf("v-live-%d", time.Now().UnixNano()%100000)

	created, err := service.Create(
		ctx,
		tagservice.RepositoryRef{ProjectKey: seeded.Key, Slug: repo.Slug},
		tagName,
		repo.CommitIDs[0],
		"live test tag",
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
