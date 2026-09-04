//go:build live

package live_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Dry-run previews, against real state.
//
// The mocks these replace built a preview from state their author supplied and
// asserted its shape. Two things were out of reach that way. The prediction
// itself rests on what the server currently holds -- a create that would
// conflict, a set that is already the value asked for -- and a mock deciding
// that state decides the answer too. And nothing checked the promise the whole
// feature rests on: that a dry run writes nothing.
//
// Each case here reads the state back afterwards. A preview that is right about
// what would happen and wrong about doing it is the failure that matters.
func TestLiveDryRunPreviewsAndLeaveNoTrace(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	user, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create user failed: %v", err)
	}

	t.Run("granting a repository permission the user does not have", func(t *testing.T) {
		// A grant to somebody with no entry is a create, not an update. The
		// distinction comes from the current permission listing, which is the
		// half a mock decides for itself.
		output := mustLiveCLI(t, "--dry-run", "repo", "settings", "security", "permissions", "users", "grant",
			user.Username, "REPO_READ", "--repo", repoRef)

		assertLivePreview(t, output, "create")

		listing := mustLiveCLI(t, "repo", "permissions", "list", "--repo", repoRef, "--all")
		if strings.Contains(listing, user.Username) {
			t.Fatalf("the dry run granted the permission:\n%s", listing)
		}
	})

	t.Run("creating a project", func(t *testing.T) {
		key := fmt.Sprintf("DRYP%d", time.Now().UnixNano()%100000)

		output := mustLiveCLI(t, "--dry-run", "project", "create", key, "--name", "Dry run project")
		assertLivePreview(t, output, "create")

		if _, err := executeLiveCLI(t, "--json", "project", "get", key); err == nil {
			t.Fatalf("the dry run created project %s", key)
		}
	})

	t.Run("creating a workflow webhook", func(t *testing.T) {
		output := mustLiveCLI(t, "--dry-run", "repo", "settings", "workflow", "webhooks", "create",
			"dry-hook", "http://example.invalid/dry", "--repo", repoRef)

		assertLivePreview(t, output, "create")

		listing := mustLiveCLI(t, "repo", "settings", "workflow", "webhooks", "list", "--repo", repoRef)
		if strings.Contains(listing, "dry-hook") {
			t.Fatalf("the dry run created the webhook:\n%s", listing)
		}
	})

	t.Run("setting a default branch it already has", func(t *testing.T) {
		// The prediction here depends on current state, which is the half a
		// mock decides for itself: setting the branch that is already default
		// is a no-op, not an update.
		current := currentLiveDefaultBranch(t)

		output := mustLiveCLI(t, "--dry-run", "branch", "default", "set", current)
		assertLivePreview(t, output, "no-op")

		if after := currentLiveDefaultBranch(t); after != current {
			t.Fatalf("the default branch moved to %q during a dry run", after)
		}
	})
}

// assertLivePreview checks a preview is well formed and predicts what the
// caller expects.
func assertLivePreview(t *testing.T, output, predictedAction string) {
	t.Helper()

	for _, want := range []string{
		`"planningMode": "stateful"`,
		fmt.Sprintf(`"predictedAction": %q`, predictedAction),
	} {
		if !strings.Contains(output, want) {
			t.Errorf("expected %s in the preview:\n%s", want, output)
		}
	}
}

// TestLiveAuthIdentity covers `auth identity` and its `whoami` alias against
// the account actually being used.
//
// The mock answered with a user it invented and asserted bb printed that slug,
// which proves the formatter and the fixture agree. Asking the server who it
// thinks you are is the only version of this question worth answering.
func TestLiveAuthIdentity(t *testing.T) {
	harness := newLiveHarness(t)

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", harness.config.BitbucketURL)
	t.Setenv("BITBUCKET_USERNAME", harness.config.BitbucketUsername)
	t.Setenv("BITBUCKET_PASSWORD", harness.config.BitbucketPassword)
	t.Setenv("BITBUCKET_TOKEN", harness.config.BitbucketToken)

	expected := harness.username()

	t.Run("identity names the authenticated account", func(t *testing.T) {
		output := mustLiveCLI(t, "auth", "identity")
		if !strings.Contains(output, expected) {
			t.Fatalf("expected %q in the identity output:\n%s", expected, output)
		}
	})

	t.Run("whoami is the same answer", func(t *testing.T) {
		output, err := executeLiveCLI(t, "auth", "whoami")
		if err != nil {
			t.Fatalf("auth whoami failed: %v\noutput: %s", err, output)
		}
		if !strings.Contains(output, expected) {
			t.Fatalf("expected %q from whoami:\n%s", expected, output)
		}
	})
}

// TestLiveReviewerConditionCreateDryRun completes the dry-run set.
func TestLiveReviewerConditionCreateDryRun(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	before := mustLiveCLI(t, "reviewer", "condition", "list", "--repo", repoRef)

	condition := `{"sourceMatcher":{"id":"ANY_REF","type":{"id":"ANY_REF"}},` +
		`"targetMatcher":{"id":"refs/heads/master","type":{"id":"BRANCH"}},"requiredApprovals":1}`
	output := mustLiveCLI(t, "--dry-run", "reviewer", "condition", "create", condition, "--repo", repoRef)

	assertLivePreview(t, output, "create")

	if after := mustLiveCLI(t, "reviewer", "condition", "list", "--repo", repoRef); after != before {
		t.Fatalf("the dry run changed the conditions\nbefore: %s\nafter:  %s", before, after)
	}
}
