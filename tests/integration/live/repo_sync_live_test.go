//go:build live

package live_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestLiveRepositoryForkSync covers bb repo sync and its enable/disable/status
// subcommands.
//
// Every one of them only means anything on a fork, so the test makes one: ref
// synchronization is a property of the relationship between two repositories,
// and against a standalone repository the endpoints answer without saying
// anything about whether bb asked the right question.
func TestLiveRepositoryForkSync(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	upstream := seeded.Repos[0]

	forkSlug := upstream.Slug + "-fork"
	postLiveJSON(t, fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s", seeded.Key, upstream.Slug), map[string]any{
		"name":    forkSlug,
		"slug":    forkSlug,
		"project": map[string]any{"key": seeded.Key},
	})

	forkRef := seeded.Key + "/" + forkSlug
	configureLiveCLIEnv(t, harness, seeded.Key, forkSlug)

	statusOutput, err := executeLiveCLI(t, "--json", "repo", "sync", "status", "--repo", forkRef)
	if err != nil {
		t.Fatalf("repo sync status failed: %v\noutput: %s", err, statusOutput)
	}
	// A fork can synchronize with its upstream, which is what distinguishes it
	// from the repository it was forked from.
	if !strings.Contains(statusOutput, `"available": true`) {
		t.Fatalf("expected a fork to report synchronization as available, got: %s", statusOutput)
	}

	// A fork is created with synchronization available but switched off, so
	// enabling it is the first thing any of this needs.
	enableOutput, err := executeLiveCLI(t, "--json", "repo", "sync", "enable", "--repo", forkRef)
	if err != nil {
		t.Fatalf("repo sync enable failed: %v\noutput: %s", err, enableOutput)
	}

	afterEnable, err := executeLiveCLI(t, "--json", "repo", "sync", "status", "--repo", forkRef)
	if err != nil {
		t.Fatalf("repo sync status after enable failed: %v\noutput: %s", err, afterEnable)
	}
	if !strings.Contains(afterEnable, `"enabled": true`) {
		t.Fatalf("expected synchronization to read as enabled, got: %s", afterEnable)
	}

	// A manual synchronization only has something to do once the fork has
	// diverged, and the two sides have to touch the same file with different
	// content: ref synchronization merges divergence on its own when the merge
	// is clean, so a pair of unrelated commits resolves itself before the manual
	// call arrives.
	const contendedFile = "contended.txt"
	if err := harness.pushFileOnBranch(seeded.Key, forkSlug, "master", contendedFile, "written by the fork\n"); err != nil {
		t.Fatalf("push a commit on the fork failed: %v", err)
	}
	if err := harness.pushFileOnBranch(seeded.Key, upstream.Slug, "master", contendedFile, "written by the upstream\n"); err != nil {
		t.Fatalf("push a commit upstream failed: %v", err)
	}

	// The bare command triggers a manual synchronization of one ref, which is a
	// different endpoint from the settings the three subcommands read and write.
	//
	// Whether there is anything left to synchronize is not under the test's
	// control: ref synchronization runs in the background and resolves ordinary
	// divergence by itself, so a fork set up to be behind is often level again by
	// the time the call lands. Either answer proves what this is here to prove --
	// that the request is well formed and reaches the ref-level logic. Until
	// recently it never did: bb sent an empty body and the server answered 500
	// for every fork in every state, because both the ref and the action are
	// required despite the schema marking them optional.
	syncOutput, syncErr := executeLiveCLI(t, "--json", "repo", "sync", "--repo", forkRef, "--action", "DISCARD")
	switch {
	case syncErr == nil:
		// The ref is resolved from the fork's default branch rather than asked for.
		if !strings.Contains(syncOutput, "refs/heads/master") {
			t.Fatalf("expected the default branch to be the ref synchronized, got: %s", syncOutput)
		}
	case strings.Contains(syncErr.Error(), "already synchronized"):
		// Nothing to do, and the server said so about the specific ref bb named.
		if !strings.Contains(syncErr.Error(), "refs/heads/master") {
			t.Fatalf("expected the refusal to name the default branch, got: %v", syncErr)
		}
	default:
		t.Fatalf("repo sync failed: %v\noutput: %s", syncErr, syncOutput)
	}

	// And again with no --action, which is the case that used to send an empty
	// field. The action is required despite the schema marking it optional, so
	// a default that did not arrive is a 500 for every fork in every state --
	// the two answers accepted above are both proof that one did.
	//
	// A unit test asserted this by decoding the body it had just been handed,
	// which says what we send and not whether the server takes it.
	defaultOutput, defaultErr := executeLiveCLI(t, "--json", "repo", "sync", "--repo", forkRef)
	switch {
	case defaultErr == nil:
	case strings.Contains(defaultErr.Error(), "already synchronized"):
	default:
		t.Fatalf("repo sync with no --action failed: %v\noutput: %s", defaultErr, defaultOutput)
	}

	disableOutput, err := executeLiveCLI(t, "--json", "repo", "sync", "disable", "--repo", forkRef)
	if err != nil {
		t.Fatalf("repo sync disable failed: %v\noutput: %s", err, disableOutput)
	}

	afterDisable, err := executeLiveCLI(t, "--json", "repo", "sync", "status", "--repo", forkRef)
	if err != nil {
		t.Fatalf("repo sync status after disable failed: %v\noutput: %s", err, afterDisable)
	}
	if !strings.Contains(afterDisable, `"enabled": false`) {
		t.Fatalf("expected synchronization to read as disabled, got: %s", afterDisable)
	}
}
