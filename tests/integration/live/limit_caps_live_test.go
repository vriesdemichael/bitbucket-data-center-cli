//go:build live

package live_test

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestLiveLimitActuallyCaps covers four --limit flags that returned everything.
//
// Each of them handed the caller's cap to a service that read it as a page
// size and then walked to the last page, with nothing trimming the result. The
// flag sized the requests and changed nothing about the answer, so `--limit 2`
// printed every commit, every changed file, every entry in the directory and
// every open pull request on the dashboard. Nothing failed and nothing warned;
// the number was simply ignored.
//
// This is the fourth and fifth and sixth and seventh instance of one mistake --
// see TestLiveBranchRestrictionLimitCaps and TestLiveCommitCompareLimitCaps for
// the first two -- which is why the fix was to stop services from taking a page
// size at all rather than to correct each of them. A page size is not something
// a CLI caller can act on: there is no cursor to advance, so the only number
// worth accepting from the outside is a cap. TestNoServiceOptionIsCalledLimit
// is the guard.
//
// Every case here asks for fewer than exist. A cap that is not honoured returns
// more, and a walk that stops early returns fewer, so both failures are visible
// in the same count.
func TestLiveLimitActuallyCaps(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// One commit per file, so the branch carries three of each.
	const branch = "capped"
	const changes = 3
	if err := harness.pushCommitsOnBranch(seeded.Key, repo.Slug, branch, changes); err != nil {
		t.Fatalf("push the capped branch failed: %v", err)
	}

	pullRequestID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
	if err != nil {
		t.Fatalf("create pull request failed: %v", err)
	}

	count := func(t *testing.T, output, key string) int {
		t.Helper()

		listed, _ := decodeJSONMap(t, output)[key].([]any)

		return len(listed)
	}

	t.Run("pr commits", func(t *testing.T) {
		all := mustLiveCLI(t, "pr", "commits", pullRequestID, "--all")
		if total := count(t, all, "commits"); total < changes {
			t.Fatalf("--all returned %d commits, want at least %d:\n%s", total, changes, all)
		}

		limited := mustLiveCLI(t, "pr", "commits", pullRequestID, "--limit", "2")
		if got := count(t, limited, "commits"); got != 2 {
			t.Fatalf("--limit 2 returned %d commits:\n%s", got, limited)
		}
	})

	t.Run("pr files", func(t *testing.T) {
		all := mustLiveCLI(t, "pr", "files", pullRequestID, "--all")
		if total := count(t, all, "changes"); total < changes {
			t.Fatalf("--all returned %d changes, want at least %d:\n%s", total, changes, all)
		}

		limited := mustLiveCLI(t, "pr", "files", pullRequestID, "--limit", "2")
		if got := count(t, limited, "changes"); got != 2 {
			t.Fatalf("--limit 2 returned %d changes:\n%s", got, limited)
		}
	})

	t.Run("repo browse tree", func(t *testing.T) {
		all := mustLiveCLI(t, "repo", "browse", "tree", "--at", branch, "--all")
		if total := count(t, all, "files"); total < changes {
			t.Fatalf("--all returned %d files, want at least %d:\n%s", total, changes, all)
		}

		limited := mustLiveCLI(t, "repo", "browse", "tree", "--at", branch, "--limit", "2")
		if got := count(t, limited, "files"); got != 2 {
			t.Fatalf("--limit 2 returned %d files:\n%s", got, limited)
		}
	})

	// The dashboard is the one the naming rule could not have caught: its option
	// was already called MaxResults and was already a page size. Converting the
	// loop is what found it.
	t.Run("pr status", func(t *testing.T) {
		const dashboardPullRequests = 3
		for index := 1; index < dashboardPullRequests; index++ {
			source := fmt.Sprintf("dashboard-%d", index)
			if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, source, source+".txt"); err != nil {
				t.Fatalf("push %s failed: %v", source, err)
			}
			if _, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, source, "master"); err != nil {
				t.Fatalf("create pull request from %s failed: %v", source, err)
			}
		}

		section := func(t *testing.T, output string) int {
			t.Helper()

			created, _ := decodeJSONMap(t, output)["createdByYou"].(map[string]any)
			listed, _ := created["pullRequests"].([]any)

			return len(listed)
		}

		all := mustLiveCLI(t, "pr", "status", "--all")
		if total := section(t, all); total < dashboardPullRequests {
			t.Fatalf("--all reported %d authored pull requests, want at least %d:\n%s", total, dashboardPullRequests, all)
		}

		limited := mustLiveCLI(t, "pr", "status", "--limit", "2")
		if got := section(t, limited); got != 2 {
			t.Fatalf("--limit 2 reported %d authored pull requests:\n%s", got, limited)
		}
	})

	// `bb branch model inspect` was converted alongside these and has no case
	// here, because its endpoint cannot produce one.
	//
	// /branch-utils/branches/info/{commitId} answers with exactly one ref -- the
	// branch Bitbucket considers the commit's home -- however many branches
	// point at it. Four branches created at one commit came back as the default
	// branch alone, and a commit reachable only from a feature branch came back
	// as that branch alone. So there is nothing to cap, and a test asserting
	// otherwise would be asserting a listing Bitbucket does not produce.
	// TestLiveBranchModelInspectAnswersWithOneRef pins the behaviour instead.
}

// TestLiveBranchModelInspectAnswersWithOneRef records what findByCommit does,
// because the name and the CLI's help both suggested otherwise.
//
// "Inspect branch refs that contain a commit" reads as a listing, and it is
// not one: several branches can contain a commit and the endpoint still names
// a single ref. Anything built on the plural -- a --limit worth capping, a
// caller iterating the refs a commit is on -- is built on a listing that does
// not exist.
func TestLiveBranchModelInspectAnswersWithOneRef(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	commits, err := harness.listCommitIDs(ctx, seeded.Key, repo.Slug, 1)
	if err != nil || len(commits) == 0 {
		t.Fatalf("could not read a commit to branch from: %v", err)
	}

	// Three more branches at the same commit. Every one of them contains it.
	for index := range 3 {
		name := fmt.Sprintf("at-the-same-commit-%d", index)
		if _, err := harness.liveJSON(ctx, "POST",
			fmt.Sprintf("/rest/branch-utils/latest/projects/%s/repos/%s/branches", seeded.Key, repo.Slug),
			map[string]any{"name": name, "startPoint": commits[0]}); err != nil {
			t.Fatalf("create branch %s: %v", name, err)
		}
	}

	output := mustLiveCLI(t, "branch", "model", "inspect", commits[0], "--repo", repoRef, "--all")
	refs, _ := decodeJSONMap(t, output)["refs"].([]any)
	if len(refs) != 1 {
		t.Fatalf("four branches point at the commit and %d refs came back; if Bitbucket has started "+
			"answering with all of them, `bb branch model inspect` can list and cap them:\n%s", len(refs), output)
	}
}
