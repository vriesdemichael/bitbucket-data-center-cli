//go:build live

package live_test

import (
	"context"
	"fmt"
	"regexp"
	"slices"
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
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
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

		id := createLifecyclePR(t, branch, "Clean", "--no-default-reviewers", "--no-codeowners")
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

		id := createLifecyclePR(t, branch, "Conflicting", "--no-default-reviewers", "--no-codeowners")

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

		// The count as stored. The blocker below shows only that something
		// vetoes the merge, which another count would do just as well.
		if got := approverCountFrom(t, decodeJSONMap(t, mustLiveCLI(t, "repo", "settings", "pull-requests", "get"))); got != "1" {
			t.Fatalf("requiredApprovers reads back as %s, want 1", got)
		}

		const branch = "feature/needs-approval"
		if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "needs-approval.txt"); err != nil {
			t.Fatalf("push commit on branch failed: %v", err)
		}
		id := createLifecyclePR(t, branch, "Needs approval", "--no-default-reviewers", "--no-codeowners")

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
		before := readLifecyclePR(t, id)
		preview := mustLiveCLI(t, "--dry-run", "pr", "merge", id)
		// A preview sends nothing: still open, at the version it was judged at.
		assertLifecyclePRStored(t, readLifecyclePR(t, id), map[string]any{"state": "OPEN", "version": before["version"]})
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

		id := createLifecyclePR(t, branch, "To be declined", "--no-default-reviewers", "--no-codeowners")
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
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	const branch = "feature/draft"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "draft.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	id := createLifecyclePR(t, branch, "A draft", "--draft", "--no-default-reviewers", "--no-codeowners")

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

	// Neither preview sent anything, the one asking for a draft included.
	if livePRIsDraft(t, id) {
		t.Error("a preview of --draft made the pull request a draft")
	}
	if after := currentLivePRVersion(t, id); after != version {
		t.Errorf("the version moved from %s to %s across two previews", version, after)
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
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	const branch = "feature/human-output"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "human.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}
	prID := createLifecyclePR(t, branch, "Human output", "--no-default-reviewers", "--no-codeowners")

	// A declined one beside it, for the listing below to ask for by state:
	// Bitbucket lists only open pull requests when it is sent no state, so this
	// one can appear only if the state bb sends arrives.
	const declinedBranch = "feature/human-output-declined"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, declinedBranch, "human-declined.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}
	declinedID := createLifecyclePR(t, declinedBranch, "Human output declined", "--no-default-reviewers", "--no-codeowners")
	mustLiveCLI(t, "pr", "decline", declinedID)
	assertLifecyclePRStored(t, readLifecyclePR(t, declinedID), map[string]any{"state": "DECLINED"})

	t.Run("the listing names the pull request and both refs", func(t *testing.T) {
		// Human output, so not through mustLiveCLI: that adds --json, and the
		// arrow and the # are what a person reads rather than a machine.
		output := mustLiveHumanCLI(t, "pr", "list", "--state", "all")

		if !strings.Contains(output, "#"+prID) {
			t.Errorf("expected the pull request id in the listing:\n%s", output)
		}
		// The arrow is how a reader sees direction at a glance, and getting the
		// refs the wrong way round is the mistake it exists to prevent.
		if !strings.Contains(output, branch+" -> master") {
			t.Errorf("expected %q in the listing:\n%s", branch+" -> master", output)
		}

		// Each row read as its columns -- id, state, refs, title -- rather than
		// searched for.
		rows := map[string][]string{}
		for _, line := range strings.Split(output, "\n") {
			columns := strings.Split(strings.TrimRight(line, "\r"), "\t")
			rows[columns[0]] = columns
		}
		for _, want := range [][]string{
			{"#" + prID, "OPEN", branch + " -> master", "Human output"},
			{"#" + declinedID, "DECLINED", declinedBranch + " -> master", "Human output declined"},
		} {
			if got := rows[want[0]]; !slices.Equal(got, want) {
				t.Errorf("the listing row for %s is %q, want %q:\n%s", want[0], got, want, output)
			}
		}
	})

	t.Run("an empty comment listing says so", func(t *testing.T) {
		output := mustLiveHumanCLI(t, "pr", "comment", "list", prID)
		if strings.TrimSpace(output) == "" {
			t.Fatal("an empty comment listing printed nothing at all")
		}
		// Saying so is this sentence: an error, or a listing that invented a
		// comment, is not empty either.
		if got := strings.TrimSpace(output); got != "No comments found" {
			t.Errorf("an empty comment listing printed %q, want %q", got, "No comments found")
		}
	})

	// `pr activity <id>` names the command group, which printed its help here
	// without an error, so this passed without listing anything. Nor is a
	// timeline ever empty: opening the pull request is its first entry, and here
	// its only one.
	t.Run("the activity listing names the opening", func(t *testing.T) {
		output := mustLiveHumanCLI(t, "pr", "activity", "list", prID)
		if strings.TrimSpace(output) == "" {
			t.Fatal("the activity listing printed nothing at all")
		}
		if !regexp.MustCompile(`^\[\d+ OPENED\]$`).MatchString(strings.TrimSpace(output)) {
			t.Errorf("expected the one OPENED entry, got:\n%s", output)
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
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
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
		ids = append(ids, createLifecyclePR(t, branch, "Filter "+branch,
			"--no-default-reviewers", "--no-codeowners"))
	}

	// One of them is declined, so the state filter has something to exclude.
	declined := ids[2]
	mustLiveCLI(t, "pr", "decline", declined)
	assertLifecyclePRStored(t, readLifecyclePR(t, declined), map[string]any{"state": "DECLINED"})

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

		closed := listedIDs(t, mustLiveCLI(t, "pr", "list", "--state", "closed"))
		if !contains(closed, declined) {
			t.Errorf("--state closed did not return the declined pull request: %v", closed)
		}
		for _, open := range ids[:2] {
			if contains(closed, open) {
				t.Errorf("the open pull request %s survived --state closed: %v", open, closed)
			}
		}
	})

	t.Run("a limit below the total cuts the answer", func(t *testing.T) {
		// The total first. --state all is also the one state only Bitbucket can
		// apply: bb sends it and keeps every answer, where a server that dropped
		// it would list the two open pull requests alone.
		all := listedIDs(t, mustLiveCLI(t, "pr", "list", "--state", "all"))
		if len(all) != len(ids) {
			t.Errorf("--state all returned %v, want the %d seeded %v", all, len(ids), ids)
		}
		for _, id := range ids {
			if !contains(all, id) {
				t.Errorf("--state all is missing %s: %v", id, all)
			}
		}

		limited := listedIDs(t, mustLiveCLI(t, "pr", "list", "--state", "all", "--limit", "1"))
		if len(limited) != 1 {
			t.Fatalf("--limit 1 returned %d pull requests: %v", len(limited), limited)
		}
		if !contains(ids, limited[0]) {
			t.Errorf("--limit 1 returned %s, which is not one of the seeded %v", limited[0], ids)
		}
	})

	t.Run("the source branch filter narrows to one", func(t *testing.T) {
		// A filter bb applies rather than one it sends: the request carries no
		// branch, so there is nothing on the server to read back, and only the
		// pull request from that branch can survive the narrowing.
		narrowed := listedIDs(t, mustLiveCLI(t, "pr", "list", "--state", "all", "--source-branch", branches[0]))
		if len(narrowed) != 1 || narrowed[0] != ids[0] {
			t.Fatalf("--source-branch %s returned %v, want just %s", branches[0], narrowed, ids[0])
		}
	})

	t.Run("pr status lists what is waiting on the caller", func(t *testing.T) {
		// A different endpoint entirely -- the cross-repository dashboard --
		// reached through the command that exists for it. --all, because the
		// dashboard spans every repository the suite has open.
		output := mustLiveCLI(t, "pr", "status", "--all")
		if !strings.Contains(output, ids[0]) {
			t.Errorf("pr status omitted a pull request the caller authored:\n%s", output)
		}

		// Parsed, and only this repository's entries: every parallel test's
		// first pull request is #1 by the same admin.
		section := func(name string) []string {
			listing, _ := decodeJSONMap(t, output)[name].(map[string]any)
			entries, _ := listing["pullRequests"].([]any)
			found := make([]string, 0, len(entries))
			for _, entry := range entries {
				pullRequest, _ := entry.(map[string]any)
				if location, _ := pullRequest["repository"].(map[string]any); location["projectKey"] == seeded.Key && location["slug"] == repo.Slug {
					found = append(found, trimNumeric(pullRequest["id"]))
				}
			}

			return found
		}

		// bb asks the dashboard for the caller's open pull requests as author,
		// and for what waits on their review: the declined one is authored too,
		// and nobody reviews their own.
		created := section("createdByYou")
		if !contains(created, ids[0]) || !contains(created, ids[1]) {
			t.Errorf("createdByYou lists %v from this repository, want the open %v", created, ids[:2])
		}
		if contains(created, declined) {
			t.Errorf("createdByYou lists the declined pull request %s: %v", declined, created)
		}
		if reviewing := section("requestingYourReview"); len(reviewing) != 0 {
			t.Errorf("requestingYourReview lists the caller's own pull requests %v", reviewing)
		}
	})
}
