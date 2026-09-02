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

// concreteChecker is instantiated as a pointer rather than an interface, which
// is what the old `any(checker) == nil` guard could not see.
type concreteChecker struct {
	called bool
}

func (checker *concreteChecker) CheckRepoPermission(_ context.Context, _, _ string, _ openapigenerated.GetRepositories1ParamsPermission) error {
	checker.called = true

	return nil
}

func (checker *concreteChecker) CheckProjectAdmin(_ context.Context, _ string) error { return nil }
func (checker *concreteChecker) CheckProjectWrite(_ context.Context, _ string) error { return nil }
func (checker *concreteChecker) CheckProjectCreate(_ context.Context) error          { return nil }

// TestANilCheckerIsSkippedHoweverItIsSpelled is the defect the first version of
// this package shipped: the guard compared the checker as an interface, which
// is correct only while every call site instantiates C with one. Nothing in the
// constraint requires that, and rootOptions.permissionCheckerFor already
// returns a concrete pointer that is nil when there is no client -- so this
// would have been a panic where the documentation promises a skip.
func TestANilCheckerIsSkippedHoweverItIsSpelled(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// C is a concrete pointer type, and the factory returns a nil one.
	err := preflight.RepoPermission(ctx, func(*openapigenerated.ClientWithResponses) *concreteChecker {
		return nil
	}, nil, "PRJ", "repo", openapi.RepoWrite)
	if err != nil {
		t.Fatalf("a nil concrete checker should be skipped, got %v", err)
	}

	// C is an interface holding a typed nil pointer, which is what the root
	// command's own wiring produces.
	err = preflight.RepoPermission(ctx, func(*openapigenerated.ClientWithResponses) preflight.RepoChecker {
		var absent *concreteChecker

		return absent
	}, nil, "PRJ", "repo", openapi.RepoWrite)
	if err != nil {
		t.Fatalf("a typed nil in an interface should be skipped, got %v", err)
	}

	// And a real one is still asked.
	checker := &concreteChecker{}
	if err := preflight.RepoPermission(ctx, func(*openapigenerated.ClientWithResponses) *concreteChecker {
		return checker
	}, nil, "PRJ", "repo", openapi.RepoWrite); err != nil {
		t.Fatalf("check: %v", err)
	}
	if !checker.called {
		t.Error("a wired checker was not asked")
	}
}

// TestTheProjectHelpersGuardAndDelegate covers the three that had no test of
// their own -- 23 of the 101 converted call sites.
func TestTheProjectHelpersGuardAndDelegate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sentinel := errors.New("refused")

	for _, testCase := range []struct {
		name string
		call func(newChecker func(*openapigenerated.ClientWithResponses) *stubChecker) error
	}{
		{
			name: "project admin",
			call: func(newChecker func(*openapigenerated.ClientWithResponses) *stubChecker) error {
				return preflight.ProjectAdmin(ctx, newChecker, nil, "PRJ")
			},
		},
		{
			name: "project write",
			call: func(newChecker func(*openapigenerated.ClientWithResponses) *stubChecker) error {
				return preflight.ProjectWrite(ctx, newChecker, nil, "PRJ")
			},
		},
		{
			name: "project create",
			call: func(newChecker func(*openapigenerated.ClientWithResponses) *stubChecker) error {
				return preflight.ProjectCreate(ctx, newChecker, nil)
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if err := testCase.call(nil); err != nil {
				t.Errorf("an unwired factory should be skipped, got %v", err)
			}

			if err := testCase.call(func(*openapigenerated.ClientWithResponses) *stubChecker {
				return nil
			}); err != nil {
				t.Errorf("a nil checker should be skipped, got %v", err)
			}

			err := testCase.call(func(*openapigenerated.ClientWithResponses) *stubChecker {
				return &stubChecker{err: sentinel}
			})
			if !errors.Is(err, sentinel) {
				t.Errorf("refusal not returned unwrapped: %v", err)
			}
		})
	}
}

// stubChecker refuses with whatever it was given, on every method.
type stubChecker struct {
	err error
}

func (checker *stubChecker) CheckRepoPermission(_ context.Context, _, _ string, _ openapigenerated.GetRepositories1ParamsPermission) error {
	return checker.err
}
func (checker *stubChecker) CheckProjectAdmin(_ context.Context, _ string) error { return checker.err }
func (checker *stubChecker) CheckProjectWrite(_ context.Context, _ string) error { return checker.err }
func (checker *stubChecker) CheckProjectCreate(_ context.Context) error          { return checker.err }
