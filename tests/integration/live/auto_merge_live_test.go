//go:build live

package live_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestLivePullRequestAutoMergeEnable is the test whose absence let #378 ship.
//
// bb posted to the auto-merge endpoint to arm auto-merge. That endpoint retries
// an existing request rather than creating one, so the call had never once
// succeeded against a real Bitbucket — and nothing noticed, because the unit
// tests stubbed the endpoint bb believed in. A stub can confirm bb called what
// bb thought it should call; only a real server can say that belief was wrong.
func TestLivePullRequestAutoMergeEnable(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	branch := "feature/auto-merge-live"

	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "auto-merge-live.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	pullRequestID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
	if err != nil {
		t.Fatalf("create pull request failed: %v", err)
	}

	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)
	assertLifecyclePRHarnessStored(t, pullRequestID, branch, "master")

	// The server rejects arming with 403 unless the repository permits
	// auto-merge at all.
	if output, err := executeLiveCLI(t, "repo", "settings", "auto-merge", "set", "--enabled", "--repo", repoRef); err != nil {
		t.Fatalf("enable repository auto-merge failed: %v\noutput: %s", err, output)
	}
	if settings := decodeJSONMap(t, mustLiveCLI(t, "repo", "settings", "auto-merge", "get", "--repo", repoRef)); settings["enabled"] != true {
		t.Fatalf("repository auto-merge reads back as enabled=%v, want true", settings["enabled"])
	}

	// A blocker, so the pull request cannot merge on the spot. Without one it
	// would merge immediately and the pending state — the thing that was broken
	// — would never be exercised.
	if output, err := executeLiveCLI(t, "repo", "settings", "pull-requests", "update-approvers", "--count", "1", "--repo", repoRef); err != nil {
		t.Fatalf("require an approver failed: %v\noutput: %s", err, output)
	}
	// Read back, because a pending auto-merge below shows only that something
	// blocks the merge, not that it is this count.
	if got := approverCountFrom(t, decodeJSONMap(t, mustLiveCLI(t, "repo", "settings", "pull-requests", "get", "--repo", repoRef))); got != "1" {
		t.Fatalf("requiredApprovers reads back as %s, want 1", got)
	}

	output, err := executeLiveCLI(t, "--json", "pr", "auto-merge", "enable", pullRequestID, "--repo", repoRef)
	if err != nil {
		t.Fatalf("pr auto-merge enable failed: %v\noutput: %s", err, output)
	}

	payload := decodeJSONMap(t, output)
	data, ok := payload["data"].(map[string]any)
	if !ok {
		data = payload
	}
	autoMerge, ok := data["autoMerge"].(map[string]any)
	if !ok {
		t.Fatalf("expected autoMerge in the payload, got: %s", output)
	}
	if autoMerge["enabled"] != true {
		t.Fatalf("expected auto-merge to be armed, got: %s", output)
	}

	// The server's own view, not bb's echo of its own request: this is what
	// AutoMergeNotRequestedException was telling us was missing.
	getOutput, err := executeLiveCLI(t, "--json", "pr", "auto-merge", "get", pullRequestID, "--repo", repoRef)
	if err != nil {
		t.Fatalf("pr auto-merge get failed: %v\noutput: %s", err, getOutput)
	}
	if !strings.Contains(getOutput, "\"enabled\": true") {
		t.Fatalf("expected the server to report auto-merge armed, got: %s", getOutput)
	}

	// Parsed, and the strategy with it. bb sends no-ff when no --strategy is
	// given, and Bitbucket reports no strategy at all for an auto-merge armed
	// without one, so a strategy it dropped reads back absent.
	armed, _ := decodeJSONMap(t, getOutput)["autoMerge"].(map[string]any)
	if armed["enabled"] != true || armed["strategyId"] != "no-ff" {
		t.Fatalf("auto-merge reads back as %v, want enabled with strategy no-ff", armed)
	}

	// And it can be cancelled again, which only works if something was armed.
	if disableOutput, err := executeLiveCLI(t, "pr", "auto-merge", "disable", pullRequestID, "--repo", repoRef); err != nil {
		t.Fatalf("pr auto-merge disable failed: %v\noutput: %s", err, disableOutput)
	}
	disabled, _ := decodeJSONMap(t, mustLiveCLI(t, "pr", "auto-merge", "get", pullRequestID, "--repo", repoRef))["autoMerge"].(map[string]any)
	if disabled["enabled"] != false {
		t.Fatalf("auto-merge reads back as %v after it was disabled", disabled)
	}

	// A selector that is neither a pull request id nor a branch with one.
	//
	// This is not local validation: bb takes anything that is not a number as
	// a branch name and asks the server which pull request is open on it, so
	// the refusal is Bitbucket answering that none is. A unit test held these
	// against a listing it had written, which decides the answer.
	for _, verb := range []string{"get", "enable", "disable"} {
		if output, err := executeLiveCLI(t, "pr", "auto-merge", verb, "not-a-branch-or-an-id", "--repo", repoRef); err == nil {
			t.Errorf("pr auto-merge %s accepted a selector that names nothing:\n%s", verb, output)
		}
	}
}

// TestLivePullRequestAutoMergeMergesImmediately covers the other outcome.
//
// The test above deliberately blocks the merge so the pending state is
// exercised. With nothing blocking, arming auto-merge merges the pull request
// on the spot, and Bitbucket says so in the same response -- there is no
// pending auto-merge afterwards, because there is nothing left to wait for.
//
// The unit test that covered this built the answer from a fixture, so it could
// only confirm that bb reads the field it was handed. Whether the server
// reports an immediate merge this way, and whether the pull request really is
// merged, are the parts that matter.
func TestLivePullRequestAutoMergeMergesImmediately(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	const branch = "feature/auto-merge-immediate"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "auto-merge-now.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}
	prID := createLifecyclePR(t, branch, "Merges immediately", "--no-default-reviewers", "--no-codeowners")

	if output, err := executeLiveCLI(t, "repo", "settings", "auto-merge", "set", "--enabled", "--repo", repoRef); err != nil {
		t.Fatalf("enable repository auto-merge failed: %v\noutput: %s", err, output)
	}
	if settings := decodeJSONMap(t, mustLiveCLI(t, "repo", "settings", "auto-merge", "get", "--repo", repoRef)); settings["enabled"] != true {
		t.Fatalf("repository auto-merge reads back as enabled=%v, want true", settings["enabled"])
	}

	// Squash offered beside no-ff, and no-ff left the default, so the merge
	// below comes out squashed only if the strategy it names is the one used.
	mustLiveCLI(t, "repo", "settings", "pull-requests", "set-strategy", "squash", "--repo", repoRef)
	mustLiveCLI(t, "repo", "settings", "pull-requests", "set-strategy", "no-ff", "--repo", repoRef)
	strategies := decodeJSONMap(t, mustLiveCLI(t, "repo", "settings", "pull-requests", "get", "--repo", repoRef))
	if strategies["defaultMergeStrategy"] != "no-ff" || !lifecycleStrategyEnabled(strategies, "squash") {
		t.Fatalf("merge strategies read back as %v, want squash enabled and no-ff the default", strategies)
	}

	sourceCommit := currentLivePRSourceCommit(t, prID)
	baseCommit, _ := lifecycleMasterHead(t, repoRef)

	// Nothing blocks this one, so arming it should merge it.
	output := mustLiveCLI(t, "pr", "auto-merge", "enable", prID, "--strategy", "squash", "--repo", repoRef)

	autoMerge, ok := decodeJSONMap(t, output)["autoMerge"].(map[string]any)
	if !ok {
		t.Fatalf("expected autoMerge in the payload, got:\n%s", output)
	}
	if autoMerge["mergedImmediately"] != true {
		t.Fatalf("expected mergedImmediately, got: %#v", autoMerge)
	}
	// Reporting an armed auto-merge here would describe a state that will never
	// fire: there is nothing left to merge.
	if autoMerge["enabled"] == true {
		t.Errorf("an immediate merge left auto-merge armed: %#v", autoMerge)
	}

	state, _ := extractPRData(decodeJSONMap(t, mustLiveCLI(t, "pr", "get", prID)))["state"].(string)
	if state != "MERGED" {
		t.Fatalf("state = %q, want MERGED", state)
	}

	// Nothing is left pending on the server either.
	pending, _ := decodeJSONMap(t, mustLiveCLI(t, "pr", "auto-merge", "get", prID, "--repo", repoRef))["autoMerge"].(map[string]any)
	if pending["enabled"] != false {
		t.Errorf("an immediate merge reads back with auto-merge %v", pending)
	}

	// The strategy by its effect: a squash is one new commit whose only parent
	// is where master was. no-ff, the default, would have made a merge commit
	// with the source commit as its second parent.
	head, parents := lifecycleMasterHead(t, repoRef)
	if head == sourceCommit || !slices.Equal(parents, []string{baseCommit}) {
		t.Errorf("master is at %s with parents %v; a squash of %s onto %s has one parent, the latter", head, parents, sourceCommit, baseCommit)
	}

	// The human line has to say the same thing as the payload.
	//
	// A second pull request, because the first is merged and cannot be armed
	// again. Telling a person auto-merge is enabled when the pull request has
	// already merged describes a state that will never fire, and it is the
	// rendering rather than the payload that most people read.
	const second = "feature/auto-merge-immediate-human"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, second, "auto-merge-now-2.txt"); err != nil {
		t.Fatalf("push the second branch failed: %v", err)
	}
	secondID := createLifecyclePR(t, second, "Merges immediately too", "--no-default-reviewers", "--no-codeowners")

	human := mustLiveHumanCLI(t, "pr", "auto-merge", "enable", secondID, "--repo", repoRef)
	if !strings.Contains(human, "immediately") {
		t.Errorf("the human output does not report the immediate merge:\n%s", human)
	}
	if strings.Contains(human, "Enabled auto-merge") {
		t.Errorf("the human output claims a pending auto-merge after an immediate merge:\n%s", human)
	}
	// The line reports a merge, so the pull request has to be merged.
	assertLifecyclePRStored(t, readLifecyclePR(t, secondID), map[string]any{"state": "MERGED"})
}

// lifecycleStrategyEnabled reports whether repo settings pull-requests get lists
// a merge strategy as enabled.
func lifecycleStrategyEnabled(settings map[string]any, strategyID string) bool {
	strategies, _ := settings["mergeStrategies"].([]any)
	for _, entry := range strategies {
		if strategy, ok := entry.(map[string]any); ok && strategy["id"] == strategyID {
			return strategy["enabled"] == true
		}
	}

	return false
}

// lifecycleMasterHead reads the newest commit on master, the default branch of
// every seeded repository, and the ids of its parents.
func lifecycleMasterHead(t *testing.T, repoRef string) (string, []string) {
	t.Helper()

	output := mustLiveCLI(t, "commit", "list", "--limit", "1", "--repo", repoRef)
	commits, _ := decodeJSONMap(t, output)["commits"].([]any)
	if len(commits) != 1 {
		t.Fatalf("expected the head of master, got:\n%s", output)
	}

	head, _ := commits[0].(map[string]any)
	listed, _ := head["parents"].([]any)
	parents := make([]string, 0, len(listed))
	for _, parent := range listed {
		parents = append(parents, asString(parent))
	}

	return asString(head["id"]), parents
}
