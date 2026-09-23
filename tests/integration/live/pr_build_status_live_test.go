//go:build live

package live_test

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// TestLivePullRequestBuildStatuses covers reading build statuses through a pull
// request, and what --limit does to the result.
//
// The unit tests these replace drove a mock through pages of a listing the
// author had shaped, asserting the page size sent and the number of items kept.
// Both are claims about how Bitbucket paginates. Posting several statuses to a
// real commit and reading them back through the pull request settles the same
// questions without either claim.
func TestLivePullRequestBuildStatuses(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	const branch = "feature/build-statuses"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "built.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	prID := createLifecyclePR(t, branch, "Has build statuses", "--no-default-reviewers", "--no-codeowners")

	// The statuses hang off the source commit, so it has to be the pull
	// request's own head rather than any commit in the repository.
	commit := currentLivePRSourceCommit(t, prID)

	// Each status differs from the others in every field, and its name from its
	// key, so a field filled from somewhere else -- one state for all three, the
	// key for the name -- cannot read back as the one sent.
	const statusCount = 3
	states := [statusCount]string{"SUCCESSFUL", "FAILED", "INPROGRESS"}
	sent := make(map[string]map[string]any, statusCount)
	for index := range statusCount {
		key := fmt.Sprintf("build-%d", index)
		name := fmt.Sprintf("Build number %d", index)
		mustLiveCLI(t, "build", "status", "set", commit,
			"--key", key,
			"--state", states[index],
			"--url", "http://example.invalid/"+key,
			"--name", name)
		sent[key] = map[string]any{"key": key, "state": states[index], "url": "http://example.invalid/" + key, "name": name}
	}

	t.Run("the statuses are readable through the pull request", func(t *testing.T) {
		output := mustLiveCLI(t, "pr", "build", "status", prID, "--all")
		for index := range statusCount {
			key := fmt.Sprintf("build-%d", index)
			if !strings.Contains(output, key) {
				t.Errorf("expected %s in the pull request build statuses:\n%s", key, output)
			}
		}

		statuses := lifecycleListing(t, output, "statuses")
		if len(statuses) != statusCount {
			t.Errorf("expected %d build statuses, got %d:\n%s", statusCount, len(statuses), output)
		}
		for _, status := range statuses {
			if want := sent[asString(status["key"])]; !reflect.DeepEqual(status, want) {
				t.Errorf("a build status reads back as %v, want %v", status, want)
			}
		}
	})

	t.Run("bb pr checks is the same command under the gh spelling", func(t *testing.T) {
		// ADR-050 asks an alias to prove it produces what the canonical path
		// produces. Asserted here rather than in a unit test because the two
		// are separate registrations built from one constructor: they could
		// diverge in what they send, and only a real call sees that.
		canonical := mustLiveCLI(t, "pr", "build", "status", prID, "--all")
		alias := mustLiveCLI(t, "pr", "checks", prID, "--all")

		if alias != canonical {
			t.Errorf("pr checks and pr build status disagree.\nchecks:\n%s\nbuild status:\n%s", alias, canonical)
		}
	})

	t.Run("--limit truncates the result", func(t *testing.T) {
		// The flag has to mean "give me at most this many", not "fetch this
		// many per page and return everything".
		output := mustLiveCLI(t, "pr", "build", "status", prID, "--limit", "1")

		found := 0
		for index := range statusCount {
			if strings.Contains(output, fmt.Sprintf("build-%d", index)) {
				found++
			}
		}
		if found != 1 {
			t.Errorf("--limit 1 returned %d of %d statuses:\n%s", found, statusCount, output)
		}

		if statuses := lifecycleListing(t, output, "statuses"); len(statuses) != 1 || !reflect.DeepEqual(statuses[0], sent[asString(statuses[0]["key"])]) {
			t.Errorf("--limit 1 returned %v, want one of the statuses sent", statuses)
		}
	})
}

// TestLivePullRequestChecksExitStatus covers ADR-091. Without --json, bb pr
// checks exits as gh pr checks does: 1 when a build failed, 8 while one has
// not finished, and 0 otherwise, a cancelled build included. With --json it
// exits 0, with every state in the document.
//
// Builds are only ever added, each under a key of its own, so nothing depends
// on how Bitbucket treats a second status under one key. Each is read back
// through the listing before the exit status is asserted, so the status
// answers for builds that are really there.
func TestLivePullRequestChecksExitStatus(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	pullRequest := func(branch string) (string, string) {
		t.Helper()

		if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, strings.ReplaceAll(branch, "/", "-")+".txt"); err != nil {
			t.Fatalf("push commit on %s failed: %v", branch, err)
		}
		prID := createLifecyclePR(t, branch, "Checks gate "+branch, "--no-default-reviewers", "--no-codeowners")

		return prID, currentLivePRSourceCommit(t, prID)
	}

	build := func(prID, commit, key, state string, listed map[string]string) {
		t.Helper()

		mustLiveCLI(t, "build", "status", "set", commit, "--key", key, "--state", state, "--url", "http://example.invalid/"+key)
		listed[key] = state

		got := map[string]string{}
		for _, status := range lifecycleListing(t, mustLiveCLI(t, "pr", "build", "status", prID, "--all"), "statuses") {
			got[asString(status["key"])] = asString(status["state"])
		}
		if !reflect.DeepEqual(got, listed) {
			t.Fatalf("the pull request's builds read back as %v, want %v", got, listed)
		}
	}

	// exitOf is the exit status bb gives a text-mode run, which is where the
	// status reports the builds. It takes the run's results rather than its
	// words, so each call keeps the command words literal for command-reach.
	exitOf := func(output string, err error) int {
		t.Helper()

		code := apperrors.ExitCode(err)
		t.Logf("exit %d (%v)\n%s", code, err, output)

		return code
	}

	gated, gatedCommit := pullRequest("feature/checks-gated")
	gatedBuilds := map[string]string{}

	build(gated, gatedCommit, "still-running", "INPROGRESS", gatedBuilds)
	if got := exitOf(executeLiveCLI(t, "pr", "checks", gated)); got != 8 {
		t.Errorf("a build in progress: bb pr checks exited %d, want 8 as gh does", got)
	}

	build(gated, gatedCommit, "broken", "FAILED", gatedBuilds)
	if got := exitOf(executeLiveCLI(t, "pr", "checks", gated)); got != 1 {
		t.Errorf("a failed build beside one in progress: bb pr checks exited %d, want 1, the failure winning", got)
	}
	if got := exitOf(executeLiveCLI(t, "pr", "build", "status", gated)); got != 1 {
		t.Errorf("bb pr build status, the canonical spelling, exited %d where bb pr checks exits 1", got)
	}
	if output, err := executeLiveCLI(t, "--json", "pr", "checks", gated); err != nil {
		t.Errorf("under --json the states are in the document and the exit status is 0, got %v\n%s", err, output)
	}

	settled, settledCommit := pullRequest("feature/checks-settled")
	settledBuilds := map[string]string{}
	build(settled, settledCommit, "passed", "SUCCESSFUL", settledBuilds)
	build(settled, settledCommit, "called-off", "CANCELLED", settledBuilds)
	if got := exitOf(executeLiveCLI(t, "pr", "checks", settled)); got != 0 {
		t.Errorf("a successful build and a cancelled one: bb pr checks exited %d, want 0 as gh does", got)
	}
}
