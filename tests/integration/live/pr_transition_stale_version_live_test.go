//go:build live

package live_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// A merge into a branch makes Bitbucket rescope every other open pull request
// that targets it, and bump its version a moment later. bb pr decline and
// bb pr merge read the version themselves when none is given, so one run
// straight after a neighbour merged could read a version that was stale by the
// time its change arrived, and be refused as out of date. bb reads it again and
// sends the change once more, as it does for an update.
//
// The retry needs Bitbucket to move the version between bb's read and bb's
// write inside one invocation, a race the live suite can lose or win but not
// arrange: on a fast machine a read during the rescope waits for it. What can
// be pinned is the rest. A neighbour's merge does bump the version; a decline
// or merge carrying the old one is refused with PullRequestOutOfDateException,
// which is what the retry keys on, and changes nothing; and without a version
// the same change goes through.
func TestLivePRTransitionWithAStaleVersionNamesTheException(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	open := func(branch string) string {
		t.Helper()
		if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, branch+".txt"); err != nil {
			t.Fatalf("push %s failed: %v", branch, err)
		}
		var created livePullRequest
		decodeJSONData(t, mustLiveCLI(t, "pr", "create", "--from-ref", branch, "--to-ref", "master", "--title", "Change "+branch), &created)

		return fmt.Sprintf("%d", created.PullRequest.ID)
	}
	state := func(id string) string {
		t.Helper()
		var stored livePullRequest
		decodeJSONData(t, mustLiveCLI(t, "pr", "get", id, "--no-review-summary"), &stored)

		return stored.PullRequest.State
	}

	first := open("merged-first")
	transitions := map[string]struct{ id, becomes string }{
		"decline": {id: open("declined-after"), becomes: "DECLINED"},
		"merge":   {id: open("merged-after"), becomes: "MERGED"},
	}
	stale := map[string]string{}
	for action, neighbour := range transitions {
		stale[action] = currentLivePRVersion(t, neighbour.id)
	}

	mustLiveCLI(t, "pr", "merge", first)

	// The rescope is not instant, so it is waited for rather than assumed.
	for action, neighbour := range transitions {
		deadline := time.Now().Add(30 * time.Second)
		for currentLivePRVersion(t, neighbour.id) == stale[action] {
			if time.Now().After(deadline) {
				t.Fatalf("merging a neighbour left pull request %s at version %s", neighbour.id, stale[action])
			}
			time.Sleep(200 * time.Millisecond)
		}
	}

	// Written out rather than looped: command-reach reads the command words
	// out of the literal at the call site.
	for action, neighbour := range transitions {
		var output string
		var err error
		if action == "decline" {
			output, err = executeLiveCLI(t, "--json", "pr", "decline", neighbour.id, "--version", stale[action])
		} else {
			output, err = executeLiveCLI(t, "--json", "pr", "merge", neighbour.id, "--version", stale[action])
		}
		if err == nil {
			t.Fatalf("pr %s carrying a stale version succeeded:\n%s", action, output)
		}
		details := apperrors.DetailsOf(err)
		if details["upstreamStatus"] != "409" || details["upstreamException"] != "com.atlassian.bitbucket.pull.PullRequestOutOfDateException" {
			t.Fatalf("pr %s: expected a 409 naming PullRequestOutOfDateException, got %v: %v", action, details, err)
		}
		// Refused, so nothing changed.
		if got := state(neighbour.id); got != "OPEN" {
			t.Fatalf("the refused pr %s left pull request %s %s", action, neighbour.id, got)
		}
	}

	// With no version named, bb reads the one the server holds now.
	mustLiveCLI(t, "pr", "decline", transitions["decline"].id)
	mustLiveCLI(t, "pr", "merge", transitions["merge"].id)
	for action, neighbour := range transitions {
		if got := state(neighbour.id); got != neighbour.becomes {
			t.Errorf("pr %s left pull request %s %s, want %s", action, neighbour.id, got, neighbour.becomes)
		}
	}
}
