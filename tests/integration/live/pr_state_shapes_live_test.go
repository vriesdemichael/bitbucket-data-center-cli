//go:build live

package live_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestLivePullRequestMergeability covers how a pull request reports whether it
// can be merged, across the states a real one passes through.
//
// The unit tests these replace stood up a mock that answered the mergeability
// endpoint with a chosen payload, or with 404, or with 409, and asserted what
// the service did with each. Every one of those was a guess at when Bitbucket
// answers which way. What a caller needs to know is whether a conflicted pull
// request is reported as conflicted, and only a conflicted pull request can
// settle that.
func TestLivePullRequestMergeability(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	t.Run("a clean pull request reports itself mergeable", func(t *testing.T) {
		const branch = "feature/clean-merge"
		if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "clean.txt"); err != nil {
			t.Fatalf("push commit on branch failed: %v", err)
		}

		id := createLivePRForRegression(t, branch, "Clean", "--no-default-reviewers", "--no-codeowners")
		mergeable, outcome := livePRMergeability(t, id)
		if !mergeable {
			t.Errorf("expected a clean pull request to be mergeable, outcome=%q", outcome)
		}
	})

	t.Run("a conflicted pull request reports the conflict", func(t *testing.T) {
		// Both sides touch the same file with different content, which is the
		// only way to make the server say CONFLICTED rather than infer it.
		const contended = "contended.txt"
		const branch = "feature/conflicting"
		if err := harness.pushFileOnBranch(seeded.Key, repo.Slug, branch, contended, "written on the branch\n"); err != nil {
			t.Fatalf("push the branch side failed: %v", err)
		}
		if err := harness.pushFileOnBranch(seeded.Key, repo.Slug, "master", contended, "written on master\n"); err != nil {
			t.Fatalf("push the master side failed: %v", err)
		}

		id := createLivePRForRegression(t, branch, "Conflicting", "--no-default-reviewers", "--no-codeowners")

		mergeable, outcome := livePRMergeability(t, id)
		if mergeable {
			t.Errorf("expected a conflicted pull request not to be mergeable, outcome=%q", outcome)
		}
		if !strings.EqualFold(outcome, "CONFLICTED") {
			t.Errorf("outcome = %q, want CONFLICTED", outcome)
		}

		// A conflict is not a veto, which is worth stating because it is the
		// natural place to look for one. Bitbucket reports a conflicted merge
		// through its own flag and leaves the veto list empty, so `pr get`
		// prints "Merge conflicts: yes" and no blockers at all.
		human := mustLiveHumanCLI(t, "pr", "get", id)
		if !strings.Contains(human, "Merge conflicts: yes") {
			t.Errorf("expected the conflict to be named in the human output:\n%s", human)
		}
		if strings.Contains(human, "Merge blockers:") {
			t.Errorf("a conflict was reported as a veto, which is a shape this suite says it is not:\n%s", human)
		}
	})

	// The vetoes, against vetoes Bitbucket wrote.
	//
	// TestMergeBlockerLines covers the shapes a veto can take; what it cannot
	// say is whether a real refusal produces one at all, or whether the bullets
	// come out empty because the fields it reads are not the fields Bitbucket
	// fills in. A required-approver check is the cheapest way to make the server
	// refuse a pull request that merges cleanly.
	t.Run("a merge check appears as a named blocker", func(t *testing.T) {
		mustLiveCLI(t, "repo", "settings", "pull-requests", "update-approvers", "--count", "1")

		const branch = "feature/needs-approval"
		if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "needs-approval.txt"); err != nil {
			t.Fatalf("push commit on branch failed: %v", err)
		}
		id := createLivePRForRegression(t, branch, "Needs approval", "--no-default-reviewers", "--no-codeowners")

		human := mustLiveHumanCLI(t, "pr", "get", id)
		if !strings.Contains(human, "Merge blockers:") {
			t.Fatalf("a pull request short of its required approvals named no blockers:\n%s", human)
		}
		// The defect the rendering exists for: a veto with no summary printed
		// an empty bullet, which reads as a blocker with no name.
		for _, line := range strings.Split(human, "\n") {
			if strings.TrimSpace(line) == "-" {
				t.Errorf("a merge blocker printed an empty bullet:\n%s", human)
			}
		}

		// #479, against the refusal rather than a written one. The prediction
		// used to come from the pull request's state alone, so an open pull
		// request was "will be merged" at full confidence however many vetoes
		// stood against it -- the weakest prediction in the tool making the
		// strongest claim, about the one operation that cannot be undone.
		preview := mustLiveCLI(t, "--dry-run", "pr", "merge", id)
		if !strings.Contains(preview, `"predictedAction": "blocked"`) {
			t.Fatalf("a pull request the server will not merge was not predicted blocked:\n%s", preview)
		}
		if !strings.Contains(preview, "blockingReasons") || strings.Contains(preview, `"blockingReasons": []`) {
			t.Fatalf("the preview named no reason for the block:\n%s", preview)
		}
	})

	t.Run("a declined pull request is readable without a mergeability answer", func(t *testing.T) {
		const branch = "feature/declined"
		if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "declined.txt"); err != nil {
			t.Fatalf("push commit on branch failed: %v", err)
		}

		id := createLivePRForRegression(t, branch, "To be declined", "--no-default-reviewers", "--no-codeowners")
		mustLiveCLI(t, "pr", "decline", id)

		// The point is that reading it still works. A closed pull request has
		// nothing to merge, and asking anyway must not turn a readable pull
		// request into an error.
		output := mustLiveCLI(t, "pr", "get", id)
		if state, _ := extractPRData(decodeJSONMap(t, output))["state"].(string); state != "DECLINED" {
			t.Fatalf("state = %q, want DECLINED", state)
		}
	})
}

// livePRMergeability reads the mergeability a pull request reports, if it
// reports one at all.
func livePRMergeability(t *testing.T, prID string) (mergeable bool, outcome string) {
	t.Helper()

	pullRequest := extractPRData(decodeJSONMap(t, mustLiveCLI(t, "pr", "get", prID)))

	details, ok := pullRequest["mergeability"].(map[string]any)
	if !ok {
		t.Fatalf("no mergeability in the pull request payload: %v", pullRequest)
	}

	mergeable, _ = details["mergeable"].(bool)
	outcome, _ = details["outcome"].(string)

	return mergeable, outcome
}

// TestLivePullRequestDraftState covers creating a draft and taking it out of
// draft, which had no live coverage at all.
//
// Draft is a flag on create and a separate field on update, and the unit tests
// asserted the payload each builds. Whether Bitbucket then treats the pull
// request as a draft is the part that matters and the part they could not see.
func TestLivePullRequestDraftState(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	const branch = "feature/draft"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "draft.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	id := createLivePRForRegression(t, branch, "A draft", "--draft", "--no-default-reviewers", "--no-codeowners")

	if !livePRIsDraft(t, id) {
		t.Fatal("expected the pull request to be created as a draft")
	}

	// --draft=false is how a draft is marked ready, and it goes through update
	// rather than create, so it is a different payload on a different endpoint.
	mustLiveCLI(t, "pr", "update", id, "--version", currentLivePRVersion(t, id), "--draft=false")

	if livePRIsDraft(t, id) {
		t.Fatal("expected --draft=false to take the pull request out of draft")
	}

	// The previews, against the state the two calls above left. A unit test
	// asked for these from a pull request whose draft field it had written, so
	// the prediction it checked was the fixture's own flag read back.
	version := currentLivePRVersion(t, id)

	toDraft := mustLiveCLI(t, "--dry-run", "pr", "update", id, "--version", version, "--draft")
	if !strings.Contains(toDraft, `"predictedAction": "update"`) {
		t.Errorf("making a ready pull request a draft again was not predicted an update:\n%s", toDraft)
	}

	alreadyReady := mustLiveCLI(t, "--dry-run", "pr", "update", id, "--version", version, "--draft=false")
	if !strings.Contains(alreadyReady, `"predictedAction": "no-op"`) {
		t.Errorf("asking for the draft state it already holds was not predicted a no-op:\n%s", alreadyReady)
	}
}

func livePRIsDraft(t *testing.T, prID string) bool {
	t.Helper()

	draft, _ := extractPRData(decodeJSONMap(t, mustLiveCLI(t, "pr", "get", prID)))["draft"].(bool)

	return draft
}

// TestLivePullRequestHumanOutput covers what the pull request commands print
// for a person, which the mocks asserted against pull requests they invented.
func TestLivePullRequestHumanOutput(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	const branch = "feature/human-output"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "human.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}
	prID := createLivePRForRegression(t, branch, "Human output", "--no-default-reviewers", "--no-codeowners")

	t.Run("the listing names the pull request and both refs", func(t *testing.T) {
		// Human output, so not through mustLiveCLI: that adds --json, and the
		// arrow and the # are what a person reads rather than a machine.
		output := mustLiveHumanCLI(t, "pr", "list", "--state", "open")

		if !strings.Contains(output, "#"+prID) {
			t.Errorf("expected the pull request id in the listing:\n%s", output)
		}
		// The arrow is how a reader sees direction at a glance, and getting the
		// refs the wrong way round is the mistake it exists to prevent.
		if !strings.Contains(output, branch+" -> master") {
			t.Errorf("expected %q in the listing:\n%s", branch+" -> master", output)
		}
	})

	t.Run("an empty comment listing says so", func(t *testing.T) {
		output := mustLiveHumanCLI(t, "pr", "comment", "list", prID)
		if strings.TrimSpace(output) == "" {
			t.Fatal("an empty comment listing printed nothing at all")
		}
	})

	t.Run("an empty activity listing says so", func(t *testing.T) {
		output := mustLiveHumanCLI(t, "pr", "activity", prID)
		if strings.TrimSpace(output) == "" {
			t.Fatal("an empty activity listing printed nothing at all")
		}
	})
}

// mustLiveHumanCLI runs a command without --json, for the output a person
// reads. mustLiveCLI adds --json, which is the wrong surface for asserting a
// table or an empty-listing notice.
func mustLiveHumanCLI(t *testing.T, args ...string) string {
	t.Helper()

	output, err := executeLiveCLI(t, args...)
	if err != nil {
		t.Fatalf("%s failed: %v\noutput: %s", strings.Join(args, " "), err, output)
	}

	return output
}

// TestLivePullRequestListingFilters covers what `pr list` asks the server for.
//
// The mocks these replace read the query string off a request they had just
// received and asserted the parameters were the ones the author expected --
// state, role, at, limit, start. That proves bb sent them. Whether Bitbucket
// applies them is the question, and the only evidence is which pull requests
// come back.
//
// Three are seeded so a limit below the total has something to cut, and the
// filters are checked by the pull requests they include and exclude rather
// than by the parameters that carried them.
func TestLivePullRequestListingFilters(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	branches := []string{"feature/filter-a", "feature/filter-b", "feature/filter-c"}
	ids := make([]string, 0, len(branches))
	for index, branch := range branches {
		if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, fmt.Sprintf("filter-%d.txt", index)); err != nil {
			t.Fatalf("push %s failed: %v", branch, err)
		}
		ids = append(ids, createLivePRForRegression(t, branch, "Filter "+branch,
			"--no-default-reviewers", "--no-codeowners"))
	}

	// One of them is declined, so the state filter has something to exclude.
	declined := ids[2]
	mustLiveCLI(t, "pr", "decline", declined)

	// Takes the output rather than the command words: a helper that spreads a
	// variadic into mustLiveCLI hides them from tools/command-reach, which
	// fails rather than quietly dropping the command from the report.
	listedIDs := func(t *testing.T, output string) []string {
		t.Helper()

		data := decodeJSONMap(t, output)
		entries, _ := data["pullRequests"].([]any)
		found := make([]string, 0, len(entries))
		for _, entry := range entries {
			pullRequest, _ := entry.(map[string]any)
			found = append(found, trimNumeric(pullRequest["id"]))
		}

		return found
	}

	contains := func(haystack []string, needle string) bool {
		for _, candidate := range haystack {
			if candidate == needle {
				return true
			}
		}

		return false
	}

	t.Run("the state filter excludes what it says", func(t *testing.T) {
		open := listedIDs(t, mustLiveCLI(t, "pr", "list", "--state", "open"))
		if contains(open, declined) {
			t.Errorf("the declined pull request %s survived --state open: %v", declined, open)
		}
		if !contains(open, ids[0]) {
			t.Errorf("an open pull request is missing from --state open: %v", open)
		}

		if closed := listedIDs(t, mustLiveCLI(t, "pr", "list", "--state", "closed")); !contains(closed, declined) {
			t.Errorf("--state closed did not return the declined pull request: %v", closed)
		}
	})

	t.Run("a limit below the total cuts the answer", func(t *testing.T) {
		if limited := listedIDs(t, mustLiveCLI(t, "pr", "list", "--state", "all", "--limit", "1")); len(limited) != 1 {
			t.Fatalf("--limit 1 returned %d pull requests: %v", len(limited), limited)
		}
	})

	t.Run("the source branch filter narrows to one", func(t *testing.T) {
		// A filter the server applies: only the pull request from that branch
		// can come back, and getting it wrong returns the others.
		narrowed := listedIDs(t, mustLiveCLI(t, "pr", "list", "--state", "all", "--source-branch", branches[0]))
		if len(narrowed) != 1 || narrowed[0] != ids[0] {
			t.Fatalf("--source-branch %s returned %v, want just %s", branches[0], narrowed, ids[0])
		}
	})

	t.Run("pr status lists what is waiting on the caller", func(t *testing.T) {
		// A different endpoint entirely -- the cross-repository dashboard --
		// reached through the command that exists for it.
		output := mustLiveCLI(t, "pr", "status")
		if !strings.Contains(output, ids[0]) {
			t.Errorf("pr status omitted a pull request the caller authored:\n%s", output)
		}
	})
}
