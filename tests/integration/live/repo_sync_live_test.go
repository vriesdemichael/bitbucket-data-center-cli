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
	t.Parallel()

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
	upstreamRef := seeded.Key + "/" + upstream.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, forkSlug)

	fork := repoAdminReadBack(t, forkRef)
	if origin, _ := fork["origin"].(map[string]any); origin["projectKey"] != seeded.Key || origin["slug"] != upstream.Slug {
		t.Fatalf("%s reads back as %v, want a fork of %s", forkRef, fork, upstreamRef)
	}

	statusOutput, err := executeLiveCLI(t, "--json", "repo", "sync", "status", "--repo", forkRef)
	if err != nil {
		t.Fatalf("repo sync status failed: %v\noutput: %s", err, statusOutput)
	}
	// A fork can synchronize with its upstream, which is what distinguishes it
	// from the repository it was forked from.
	if !strings.Contains(statusOutput, `"available": true`) {
		t.Fatalf("expected a fork to report synchronization as available, got: %s", statusOutput)
	}
	if status := decodeJSONMap(t, statusOutput); status["available"] != true || status["enabled"] != false {
		t.Fatalf("a new fork reports available=%v enabled=%v, want available and not yet enabled: %s", status["available"], status["enabled"], statusOutput)
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
	if enabled := decodeJSONMap(t, afterEnable)["enabled"]; enabled != true {
		t.Fatalf("synchronization reads back as enabled=%v after enable: %s", enabled, afterEnable)
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
	// Bitbucket looks at the push in the background, about half a minute later,
	// and until it has marked the ref diverged a manual synchronization answers
	// "already synchronized" and does nothing -- which is how this call used to
	// pass without DISCARD ever taking effect. Waiting for the mark leaves the
	// call one outcome, and the fork's master something to prove it by.
	waitForRepoSyncDivergence(t, forkRef, "refs/heads/master")

	// Until recently the request never reached the ref-level logic: bb sent an
	// empty body and the server answered 500 for every fork in every state,
	// because both the ref and the action are required despite the schema
	// marking them optional.
	syncOutput, syncErr := executeLiveCLI(t, "--json", "repo", "sync", "--repo", forkRef, "--action", "DISCARD")
	if syncErr != nil {
		t.Fatalf("repo sync failed: %v\noutput: %s", syncErr, syncOutput)
	}
	// The ref is resolved from the fork's default branch rather than asked for.
	if !strings.Contains(syncOutput, "refs/heads/master") {
		t.Fatalf("expected the default branch to be the ref synchronized, got: %s", syncOutput)
	}

	// DISCARD throws the fork's own commit away, so its master is upstream's
	// again and the contended file says what upstream wrote. A MERGE would have
	// left a merge commit, and a request that changed nothing the fork's commit.
	forkHead := decodeJSONMap(t, mustLiveCLI(t, "commit", "get", "master", "--repo", forkRef))["commit"].(map[string]any)["id"]
	upstreamHead := decodeJSONMap(t, mustLiveCLI(t, "commit", "get", "master", "--repo", upstreamRef))["commit"].(map[string]any)["id"]
	if forkHead != upstreamHead {
		t.Fatalf("after DISCARD the fork's master is %v and upstream's is %v; want the same commit", forkHead, upstreamHead)
	}
	if content := decodeJSONMap(t, mustLiveCLI(t, "repo", "cat", contendedFile, "--repo", forkRef))["content"]; content != "written by the upstream\n" {
		t.Fatalf("after DISCARD the fork's %s reads %q, want upstream's version", contendedFile, content)
	}

	// And again with no --action, which is the case that used to send an empty
	// field. The action is required despite the schema marking it optional, so
	// a default that did not arrive is a 500 for every fork in every state --
	// the two answers accepted here are both proof that one did.
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
	if enabled := decodeJSONMap(t, afterDisable)["enabled"]; enabled != false {
		t.Fatalf("synchronization reads back as enabled=%v after disable: %s", enabled, afterDisable)
	}
}

// waitForRepoSyncDivergence waits for Bitbucket to list a fork's ref among the
// refs that have diverged from upstream.
func waitForRepoSyncDivergence(t *testing.T, forkRef, refID string) {
	t.Helper()

	deadline := time.Now().Add(3 * time.Minute)
	for {
		output := mustLiveCLI(t, "repo", "sync", "status", "--repo", forkRef)
		diverged, _ := decodeJSONMap(t, output)["divergedRefs"].([]any)
		for _, ref := range diverged {
			if entry, _ := ref.(map[string]any); entry["id"] == refID {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("Bitbucket had not marked %s diverged on %s after three minutes: %s", refID, forkRef, output)
		}
		time.Sleep(2 * time.Second)
	}
}
