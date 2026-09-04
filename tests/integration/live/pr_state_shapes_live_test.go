//go:build live

package live_test

import (
	"context"
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
