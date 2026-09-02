package preflight_test

import (
	"context"
	"errors"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/preflight"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// checker records what it was asked, standing in for the real one.
//
// It carries a second method so it is not accidentally identical to
// preflight.RepoChecker: the command packages declare wider interfaces, and the
// helper has to accept those rather than only the minimal one.
type checker struct {
	calls  int
	ref    [2]string
	level  openapi.RepositoryPermission
	refuse error
}

func (c *checker) CheckRepoPermission(_ context.Context, projectKey, repoSlug string, permission openapigenerated.GetRepositories1ParamsPermission) error {
	c.calls++
	c.ref = [2]string{projectKey, repoSlug}
	c.level = permission
	return c.refuse
}

func (c *checker) CheckProjectAdmin(context.Context, string) error { return nil }

// wider is what a command package's PermissionChecker looks like: more methods
// than the helper needs.
type wider interface {
	CheckRepoPermission(ctx context.Context, projectKey, repoSlug string, permission openapigenerated.GetRepositories1ParamsPermission) error
	CheckProjectAdmin(ctx context.Context, projectKey string) error
}

func TestRepoPermissionAsksForTheLevelItWasGiven(t *testing.T) {
	t.Parallel()

	recorder := &checker{}
	err := preflight.RepoPermission(
		context.Background(),
		func(*openapigenerated.ClientWithResponses) wider { return recorder },
		nil, "PRJ", "payments", openapi.RepoWrite,
	)
	if err != nil {
		t.Fatalf("pre-flight: %v", err)
	}
	if recorder.calls != 1 {
		t.Fatalf("checked %d times, want once", recorder.calls)
	}
	if recorder.ref != [2]string{"PRJ", "payments"} {
		t.Errorf("checked %v, want the repository it was given", recorder.ref)
	}
	// The level is the one thing left to get wrong at a call site, so it has to
	// arrive unchanged. #481 was a site asking for read on an operation that
	// writes.
	if recorder.level != openapi.RepoWrite {
		t.Errorf("checked level %v, want RepoWrite", recorder.level)
	}
}

func TestRepoPermissionPropagatesARefusal(t *testing.T) {
	t.Parallel()

	refusal := errors.New("you may not")
	err := preflight.RepoPermission(
		context.Background(),
		func(*openapigenerated.ClientWithResponses) wider { return &checker{refuse: refusal} },
		nil, "PRJ", "payments", openapi.RepoAdmin,
	)
	if !errors.Is(err, refusal) {
		t.Fatalf("err = %v, want the checker's refusal to reach the caller", err)
	}
}

// TestRepoPermissionSkipsWhenUnwired covers both guards the call sites carried.
//
// A command constructed without a PermissionChecker, or a factory that declines
// to build one, runs without the pre-flight rather than failing. Every unit test
// in the command packages depends on that: they have no server to ask.
func TestRepoPermissionSkipsWhenUnwired(t *testing.T) {
	t.Parallel()

	t.Run("no factory", func(t *testing.T) {
		if err := preflight.RepoPermission[wider](
			context.Background(), nil, nil, "PRJ", "payments", openapi.RepoAdmin,
		); err != nil {
			t.Fatalf("an unwired checker refused: %v", err)
		}
	})

	t.Run("factory returns nothing", func(t *testing.T) {
		if err := preflight.RepoPermission(
			context.Background(),
			func(*openapigenerated.ClientWithResponses) wider { return nil },
			nil, "PRJ", "payments", openapi.RepoAdmin,
		); err != nil {
			t.Fatalf("a nil checker refused: %v", err)
		}
	})
}
