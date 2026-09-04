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
