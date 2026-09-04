//go:build live

package live_test

import (
	"context"
	"strings"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// The CLI surface of tags, build checks and code insights: what the commands
// print, what an empty listing looks like, and what a missing resource maps to.
//
// These had live coverage of the operations already, but through the service
// rather than the CLI, so the output half was only ever checked against
// fabricated payloads. What a caller actually reads -- the JSON envelope, the
// human line, the exit code -- was never produced by a real server.
func TestLiveTagCLISurface(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	const tag = "v9.9.9-live"

	t.Run("a dry run predicts the tag without making it", func(t *testing.T) {
		output := mustLiveCLI(t, "--dry-run", "tag", "create", tag, "--start-point", "master")
		if !strings.Contains(output, `"predictedAction": "create"`) {
			t.Errorf("expected a create prediction:\n%s", output)
		}

		if listing := mustLiveCLI(t, "tag", "list", "--all"); strings.Contains(listing, tag) {
			t.Fatalf("the dry run created the tag:\n%s", listing)
		}
	})

	t.Run("creating one answers with the tag", func(t *testing.T) {
		output := mustLiveCLI(t, "tag", "create", tag, "--start-point", "master")
		if !strings.Contains(output, tag) {
			t.Fatalf("expected the created tag in the output:\n%s", output)
		}
	})

	t.Run("it appears in the listing and can be read back", func(t *testing.T) {
		if listing := mustLiveCLI(t, "tag", "list", "--all"); !strings.Contains(listing, tag) {
			t.Fatalf("the tag is missing from the listing:\n%s", listing)
		}
		if view := mustLiveCLI(t, "tag", "view", tag); !strings.Contains(view, tag) {
			t.Fatalf("reading the tag back did not name it:\n%s", view)
		}
	})

	t.Run("deleting it removes it", func(t *testing.T) {
		mustLiveCLI(t, "tag", "delete", tag)
		if listing := mustLiveCLI(t, "tag", "list", "--all"); strings.Contains(listing, tag) {
			t.Fatalf("the tag survived the delete:\n%s", listing)
		}
	})

	t.Run("a tag that is not there maps to not found", func(t *testing.T) {
		output, err := executeLiveCLI(t, "--json", "tag", "view", "v0.0.0-absent")
		if err == nil {
			t.Fatalf("expected a missing tag to fail, got:\n%s", output)
		}
		if code := apperrors.ExitCode(err); code != 4 {
			t.Errorf("exit code = %d, want 4 (%v)", code, err)
		}
	})
}

// TestLiveEmptyListingsSaySo covers what the build and insights listings print
// when there is nothing to print.
//
// The mock asserted a particular phrase against an empty array it wrote itself.
// A fresh repository and an untouched commit are genuinely empty, so the same
// assertion holds without anyone deciding what empty looks like.
func TestLiveEmptyListingsSaySo(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	commits, err := harness.listCommitIDs(ctx, seeded.Key, repo.Slug, 1)
	if err != nil || len(commits) == 0 {
		t.Fatalf("could not read a commit: %v", err)
	}

	for _, listing := range [][]string{
		{"build", "required", "list"},
		{"insights", "report", "list", commits[0]},
		{"tag", "list"},
	} {
		t.Run(strings.Join(listing, " "), func(t *testing.T) {
			output := mustLiveCLI(t, listing...)
			// An empty listing that prints nothing is indistinguishable from a
			// command that failed silently.
			if strings.TrimSpace(output) == "" {
				t.Fatalf("printed nothing at all for an empty result")
			}
		})
	}
}
