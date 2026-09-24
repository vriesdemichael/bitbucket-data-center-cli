//go:build live

package live_test

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

func TestLiveCLIBranchLifecycle(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Two commits, so that the branch can start from the older one: from
	// master's tip, a branch whose start point went astray would look the same.
	seeded, err := harness.seedRepo(ctx, repoSeed{Commits: 2, WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	branchName := "feature/live-test-branch"
	startPoint := branchOlderSeedCommit(t, repo)

	// Create branch
	createOutput, err := executeLiveCLI(t, "--json", "branch", "create", branchName, "--start-point", startPoint)
	if err != nil {
		t.Fatalf("branch create failed: %v\noutput: %s", err, createOutput)
	}
	createPayload := decodeJSONMap(t, createOutput)
	branchObj, ok := createPayload["branch"].(map[string]any)
	if !ok {
		branchObj = createPayload
	}
	if asString(branchObj["displayId"]) != branchName {
		t.Fatalf("expected branch displayId %s, got: %s", branchName, createOutput)
	}
	assertOnlyBranchNamed(t, branchName, startPoint)

	// List branches (human output)
	listOutput, err := executeLiveCLI(t, "branch", "list")
	if err != nil {
		t.Fatalf("branch list failed: %v\noutput: %s", err, listOutput)
	}
	if !strings.Contains(listOutput, branchName) {
		t.Fatalf("expected branch %s in list output, got: %s", branchName, listOutput)
	}

	// The listing options, which nothing had driven.
	//
	// --filter, --base, --details and --order-by each add one query parameter,
	// and a query parameter built wrong does not fail: it comes back with the
	// wrong branches, or all of them, or in another order. So each is checked by
	// what it changes. --all has nothing to page through in this repository;
	// TestLiveListingsPageToTheEnd is where it is proven.
	filtered := mustLiveCLI(t, "branch", "list", "--filter", branchName, "--all")
	if !strings.Contains(filtered, branchName) {
		t.Fatalf("--filter %s excluded the branch it names:\n%s", branchName, filtered)
	}
	if strings.Contains(filtered, `"displayId": "master"`) {
		t.Fatalf("--filter %s returned master as well, so it filtered nothing:\n%s", branchName, filtered)
	}
	if branches := branchesInListing(t, filtered); len(branches) != 1 || branches[0].DisplayID != branchName {
		t.Fatalf("--filter %s listed %+v, want that branch alone", branchName, branches)
	}

	// --base and --details change only the metadata Bitbucket attaches to each
	// branch, which bb's output leaves out. A base that names no branch shows
	// both were sent: Bitbucket resolves the base only when details are asked
	// for, and refuses one it cannot find. On its own, --base has no effect at
	// all, so the first call here has nothing to read back.
	mustLiveCLI(t, "branch", "list", "--base", "master", "--all")
	mustLiveCLI(t, "branch", "list", "--details", "--all")
	// The same base without --details has to be accepted, or the refusal below
	// would not need --details to have been sent.
	mustLiveCLI(t, "branch", "list", "--base", "no-such-branch", "--all")
	if output, err := executeLiveCLI(t, "--json", "branch", "list", "--base", "no-such-branch", "--details", "--all"); !apperrors.IsKind(err, apperrors.KindNotFound) {
		t.Fatalf("--base naming no branch, with --details, was not refused as not found: %v\n%s", err, output)
	}

	// --order-by ALPHABETICAL against Bitbucket's default, which is by
	// modification. The branch pushed here sorts last by name and first by
	// modification. Git dates a commit to the second, so the push waits for the
	// next one: a commit in the same second as the seeded ones would tie with
	// them, and the two orders could agree.
	const newestBranch = "release/live-test-newest"
	time.Sleep(time.Until(time.Now().Truncate(time.Second).Add(time.Second)))
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, newestBranch, "newest.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}
	if byDefault := branchesInListing(t, mustLiveCLI(t, "branch", "list", "--all")); len(byDefault) == 0 || byDefault[0].DisplayID != newestBranch {
		t.Fatalf("the default order does not list %s first, so it cannot tell a dropped --order-by apart: %+v", newestBranch, byDefault)
	}
	var alphabetical []string
	for _, branch := range branchesInListing(t, mustLiveCLI(t, "branch", "list", "--order-by", "ALPHABETICAL", "--all")) {
		alphabetical = append(alphabetical, branch.DisplayID)
	}
	if want := []string{branchName, "master", newestBranch}; !slices.Equal(alphabetical, want) {
		t.Fatalf("--order-by ALPHABETICAL listed %v, want %v", alphabetical, want)
	}

	// The listing marks the default branch. bb read the mark from a field
	// Bitbucket never sends, and so reported every branch as not the default
	// (#658). The answer is checked against Bitbucket's own default-branch
	// route, which is a separate request.
	defaultBranch, err := harness.liveJSON(ctx, http.MethodGet,
		fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/branches/default", seeded.Key, repo.Slug), nil)
	if err != nil {
		t.Fatalf("read the default branch: %v", err)
	}
	defaultName := asString(defaultBranch["displayId"])
	if defaultName == "" {
		t.Fatalf("Bitbucket named no default branch: %v", defaultBranch)
	}
	marked := 0
	for _, branch := range branchesInListing(t, mustLiveCLI(t, "branch", "list", "--all")) {
		if branch.Default {
			marked++
		}
		if branch.Default != (branch.DisplayID == defaultName) {
			t.Errorf("branch %s is listed with default=%v, and Bitbucket's default branch is %s", branch.DisplayID, branch.Default, defaultName)
		}
	}
	if marked != 1 {
		t.Errorf("the listing marks %d branches as the default, want 1", marked)
	}

	// Get default branch
	defaultOutput, err := executeLiveCLI(t, "--json", "branch", "default", "get")
	if err != nil {
		t.Fatalf("branch default get failed: %v\noutput: %s", err, defaultOutput)
	}
	defaultPayload := decodeJSONMap(t, defaultOutput)
	defaultBranchObj, ok := defaultPayload["defaultBranch"].(map[string]any)
	if !ok {
		defaultBranchObj = defaultPayload
	}
	if asString(defaultBranchObj["displayId"]) == "" && asString(defaultBranchObj["id"]) == "" {
		t.Fatalf("expected default branch displayId or id, got: %s", defaultOutput)
	}
	assertMasterIsDefaultBranch(t, defaultOutput)

	/*
		// Find by commit
		time.Sleep(1 * time.Second)
		findOutput, err := executeLiveCLI(t, "--json", "branch", "model", "inspect", startPoint)
		if err != nil {
			t.Fatalf("branch model inspect failed: %v\noutput: %s", err, findOutput)
		}
		findPayload := decodeJSONMap(t, findOutput)
		refs, ok := findPayload["refs"].([]any)
		if !ok || len(refs) == 0 {
			t.Fatalf("expected refs in branch model inspect output, got: %s", findOutput)
		}
	*/

	// Delete branch
	deleteOutput, err := executeLiveCLI(t, "branch", "delete", branchName, "--yes")
	if err != nil {
		t.Fatalf("branch delete failed: %v\noutput: %s", err, deleteOutput)
	}
	if remaining := branchesInListing(t, mustLiveCLI(t, "branch", "list", "--filter", branchName, "--all")); len(remaining) != 0 {
		t.Fatalf("branch %s is still listed after its delete: %+v", branchName, remaining)
	}
}

func TestLiveCLIBranchRestrictionLifecycle(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// Create restriction
	createOutput, err := executeLiveCLI(
		t, "--json", "branch", "restriction", "create",
		"--type", "read-only",
		"--matcher-id", "refs/heads/master",
	)
	if err != nil {
		t.Fatalf("restriction create failed: %v\noutput: %s", err, createOutput)
	}
	createPayload := decodeJSONMap(t, createOutput)
	restrictionID := ""
	if restriction, ok := createPayload["restriction"].(map[string]any); ok {
		restrictionID = asString(restriction["id"])
	} else {
		restrictionID = asString(createPayload["id"])
	}

	if restrictionID == "" {
		t.Fatalf("expected restriction id in output, got: %s", createOutput)
	}
	// BRANCH is the matcher type --matcher-type defaults to.
	created := storedRestriction{scope: "REPOSITORY", restrictionType: "read-only", matcherType: "BRANCH", matcherID: "refs/heads/master"}
	assertRestrictionStored(t, restrictionPayload(t, mustLiveCLI(t, "branch", "restriction", "get", restrictionID)), created)
	// The listing as well: Bitbucket answers a get for a restriction id through
	// any repository's path, so only the listing shows it was stored on this one.
	assertOnlyRestrictionListed(t, mustLiveCLI(t, "branch", "restriction", "list"), restrictionID, created)

	// Get restriction
	getOutput, err := executeLiveCLI(t, "branch", "restriction", "get", restrictionID)
	if err != nil {
		t.Fatalf("restriction get failed: %v\noutput: %s", err, getOutput)
	}
	if !strings.Contains(getOutput, restrictionID) || !strings.Contains(getOutput, "read-only") {
		t.Fatalf("expected id and type in human get output, got: %s", getOutput)
	}

	// PATTERN rather than the BRANCH the restriction was created with, so the
	// matcher type read back is the one this update sent.
	updateOutput, err := executeLiveCLI(
		t, "--json", "branch", "restriction", "update", restrictionID,
		"--type", "no-deletes",
		"--matcher-type", "PATTERN",
		"--matcher-id", "refs/heads/master",
	)
	if err != nil {
		t.Fatalf("restriction update failed: %v\noutput: %s", err, updateOutput)
	}

	// Another type is another restriction: the update created one and removed
	// the restriction it replaces, so the id to follow is the new one.
	updatePayload := decodeJSONMap(t, updateOutput)
	if restriction, ok := updatePayload["restriction"].(map[string]any); ok {
		restrictionID = asString(restriction["id"])
	} else {
		restrictionID = asString(updatePayload["id"])
	}

	listOutput, err := executeLiveCLI(t, "branch", "restriction", "list")
	if err != nil {
		t.Fatalf("restriction list failed: %v\noutput: %s", err, listOutput)
	}
	if !strings.Contains(listOutput, restrictionID) || !strings.Contains(listOutput, "no-deletes") {
		t.Fatalf("expected restriction %s in human list output, got: %s", restrictionID, listOutput)
	}
	updated := storedRestriction{scope: "REPOSITORY", restrictionType: "no-deletes", matcherType: "PATTERN", matcherID: "refs/heads/master"}
	assertOnlyRestrictionListed(t, mustLiveCLI(t, "branch", "restriction", "list"), restrictionID, updated)

	// A restriction id that is not an id at all. A unit test asked these of a
	// stub whose default was 404, so what it checked was the stub's default; a
	// real instance is what says whether "abc" is refused and not, say, read as
	// zero. Bitbucket does not route it, and its 404 for a get is one the client
	// cannot decode, which surfaced as a transient failure. So bb refuses the id
	// before sending it -- there is no upstream status -- and the restriction
	// that exists meanwhile shows that nothing reached it.
	for _, args := range [][]string{
		{"branch", "restriction", "get", "abc"},
		{"branch", "restriction", "update", "abc", "--type", "no-deletes", "--matcher-type", "PATTERN", "--matcher-id", "refs/heads/master"},
		{"branch", "restriction", "delete", "abc", "--yes"},
	} {
		output, err := executeLiveCLI(t, args...)
		if !apperrors.IsKind(err, apperrors.KindValidation) || !strings.Contains(apperrors.MessageOf(err), "restriction id") || apperrors.DetailsOf(err)["upstreamStatus"] != "" {
			t.Fatalf("%s was not refused before it was sent: %v (details %v)\n%s", strings.Join(args, " "), err, apperrors.DetailsOf(err), output)
		}
	}
	assertOnlyRestrictionListed(t, mustLiveCLI(t, "branch", "restriction", "list"), restrictionID, updated)

	deleteOutput, err := executeLiveCLI(t, "--json", "branch", "restriction", "delete", restrictionID, "--yes")
	if err != nil {
		t.Fatalf("restriction delete failed: %v\noutput: %s", err, deleteOutput)
	}
	if asString(decodeJSONMap(t, deleteOutput)["status"]) != "ok" {
		t.Fatalf("expected delete status ok, got: %s", deleteOutput)
	}

	// And one that is a number, for a restriction that is gone: the delete
	// above makes this the same id that worked a moment ago. It is also what
	// shows the delete happened, since Bitbucket answers the delete of an id it
	// does not hold with 204 as well.
	output, err := executeLiveCLI(t, "branch", "restriction", "get", restrictionID)
	if err == nil {
		t.Fatalf("restriction get found a restriction that was deleted:\n%s", output)
	}
	if !apperrors.IsKind(err, apperrors.KindNotFound) {
		t.Fatalf("restriction get of the deleted restriction failed with %v, want not found", err)
	}
	if remaining := restrictionsInListing(t, mustLiveCLI(t, "branch", "restriction", "list")); len(remaining) != 0 {
		t.Fatalf("restrictions remain after the delete: %v", remaining)
	}
}

func TestLiveCLIBranchDeleteDryRunHasNoSideEffect(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{Commits: 2, WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	branchName := "feature/live-dry-run-delete"
	startPoint := branchOlderSeedCommit(t, repo)

	createOutput, err := executeLiveCLI(t, "--json", "branch", "create", branchName, "--start-point", startPoint)
	if err != nil {
		t.Fatalf("branch create failed: %v\noutput: %s", err, createOutput)
	}
	assertOnlyBranchNamed(t, branchName, startPoint)

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "branch", "delete", branchName, "--yes")
	if err != nil {
		t.Fatalf("branch delete dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	assertLivePreviewOf(t, dryRunOutput, "branch delete", jsonoutput.OutcomeWouldApply)

	listOutput, err := executeLiveCLI(t, "--json", "branch", "list")
	if err != nil {
		t.Fatalf("branch list failed: %v\noutput: %s", err, listOutput)
	}
	if !strings.Contains(listOutput, branchName) {
		t.Fatalf("expected branch %s to remain after dry-run delete, got: %s", branchName, listOutput)
	}
	assertOnlyBranchNamed(t, branchName, startPoint)

	deleteOutput, err := executeLiveCLI(t, "branch", "delete", branchName, "--yes")
	if err != nil {
		t.Fatalf("branch delete cleanup failed: %v\noutput: %s", err, deleteOutput)
	}
	if remaining := branchesInListing(t, mustLiveCLI(t, "branch", "list", "--filter", branchName, "--all")); len(remaining) != 0 {
		t.Fatalf("branch %s is still listed after its delete: %+v", branchName, remaining)
	}
}

func TestLiveCLIBranchCreateDryRunHasNoSideEffect(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	branchName := "feature/live-dry-run-create"
	startPoint := repo.CommitIDs[0]

	listBeforeOutput, err := executeLiveCLI(t, "--json", "branch", "list")
	if err != nil {
		t.Fatalf("branch list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "branch", "create", branchName, "--start-point", startPoint)
	if err != nil {
		t.Fatalf("branch create dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	assertLivePreviewOf(t, dryRunOutput, "branch create", jsonoutput.OutcomeWouldApply)

	listAfterOutput, err := executeLiveCLI(t, "--json", "branch", "list")
	if err != nil {
		t.Fatalf("branch list after failed: %v\noutput: %s", err, listAfterOutput)
	}
	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no branch side-effect from create dry-run\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}
	if created := branchesInListing(t, mustLiveCLI(t, "branch", "list", "--filter", branchName, "--all")); len(created) != 0 {
		t.Fatalf("the create dry-run created branch %s: %+v", branchName, created)
	}
}

func TestLiveCLIBranchDefaultSetDryRunHasNoSideEffect(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{Commits: 2, WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// Another branch to ask for. master is the default already, so asking for it
	// leaves the default unchanged whether or not the dry run sent the change.
	const otherBranch = "feature/live-dry-run-default"
	startPoint := branchOlderSeedCommit(t, repo)
	mustLiveCLI(t, "branch", "create", otherBranch, "--start-point", startPoint)
	assertOnlyBranchNamed(t, otherBranch, startPoint)

	defaultBeforeOutput, err := executeLiveCLI(t, "--json", "branch", "default", "get")
	if err != nil {
		t.Fatalf("branch default get before failed: %v\noutput: %s", err, defaultBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "branch", "default", "set", otherBranch)
	if err != nil {
		t.Fatalf("branch default set dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	assertLivePreviewOf(t, dryRunOutput, "branch default set", jsonoutput.OutcomeWouldApply)

	defaultAfterOutput, err := executeLiveCLI(t, "--json", "branch", "default", "get")
	if err != nil {
		t.Fatalf("branch default get after failed: %v\noutput: %s", err, defaultAfterOutput)
	}
	if defaultBeforeOutput != defaultAfterOutput {
		t.Fatalf("expected no default-branch side-effect from dry-run\nbefore: %s\nafter: %s", defaultBeforeOutput, defaultAfterOutput)
	}
	assertMasterIsDefaultBranch(t, defaultAfterOutput)
}

func TestLiveCLIBranchRestrictionCreateDryRunHasNoSideEffect(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// --limit has nothing to cut in a repository this test made;
	// TestLiveBranchRestrictionLimitCaps is where it is proven.
	listBeforeOutput, err := executeLiveCLI(t, "--json", "branch", "restriction", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("restriction list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "branch", "restriction", "create", "--type", "read-only", "--matcher-type", "BRANCH", "--matcher-id", "master")
	if err != nil {
		t.Fatalf("restriction create dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	assertLivePreviewOf(t, dryRunOutput, "branch restriction create", jsonoutput.OutcomeWouldApply)

	listAfterOutput, err := executeLiveCLI(t, "--json", "branch", "restriction", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("restriction list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no restriction side-effect from create dry-run\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}
	if created := restrictionsInListing(t, listAfterOutput); len(created) != 0 {
		t.Fatalf("the create dry-run left a restriction behind: %v", created)
	}
}

func TestLiveCLIBranchRestrictionDeleteDryRunHasNoSideEffect(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	createOutput, err := executeLiveCLI(t, "--json", "branch", "restriction", "create", "--type", "read-only", "--matcher-id", "refs/heads/master")
	if err != nil {
		t.Fatalf("restriction create fixture failed: %v\noutput: %s", err, createOutput)
	}

	restrictionID := ""
	if restriction, ok := decodeJSONMap(t, createOutput)["restriction"].(map[string]any); ok {
		restrictionID = asString(restriction["id"])
	}
	if restrictionID == "" {
		t.Fatalf("expected restriction id in create output: %s", createOutput)
	}
	// BRANCH is the matcher type --matcher-type defaults to.
	fixture := storedRestriction{scope: "REPOSITORY", restrictionType: "read-only", matcherType: "BRANCH", matcherID: "refs/heads/master"}

	// --limit has nothing to cut in a repository this test made;
	// TestLiveBranchRestrictionLimitCaps is where it is proven.
	listBeforeOutput, err := executeLiveCLI(t, "--json", "branch", "restriction", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("restriction list before failed: %v\noutput: %s", err, listBeforeOutput)
	}
	assertOnlyRestrictionListed(t, listBeforeOutput, restrictionID, fixture)

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "branch", "restriction", "delete", restrictionID, "--yes")
	if err != nil {
		t.Fatalf("restriction delete dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	assertLivePreviewOf(t, dryRunOutput, "branch restriction delete", jsonoutput.OutcomeWouldApply)

	listAfterOutput, err := executeLiveCLI(t, "--json", "branch", "restriction", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("restriction list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no restriction side-effect from delete dry-run\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}
	assertOnlyRestrictionListed(t, listAfterOutput, restrictionID, fixture)

	if output, err := executeLiveCLI(t, "--json", "branch", "restriction", "delete", restrictionID, "--yes"); err != nil {
		t.Fatalf("restriction delete cleanup failed: %v\noutput: %s", err, output)
	}
	if remaining := restrictionsInListing(t, mustLiveCLI(t, "branch", "restriction", "list")); len(remaining) != 0 {
		t.Fatalf("restrictions remain after the delete: %v", remaining)
	}
}

func TestLiveCLIBranchModelUpdateDryRunHasNoSideEffect(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{Commits: 2, WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// Another branch to ask for. master is the default already, so asking for it
	// leaves the default unchanged whether or not the dry run sent the change.
	const otherBranch = "feature/live-dry-run-model"
	startPoint := branchOlderSeedCommit(t, repo)
	mustLiveCLI(t, "branch", "create", otherBranch, "--start-point", startPoint)
	assertOnlyBranchNamed(t, otherBranch, startPoint)

	defaultBeforeOutput, err := executeLiveCLI(t, "--json", "branch", "default", "get")
	if err != nil {
		t.Fatalf("branch default get before failed: %v\noutput: %s", err, defaultBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "branch", "model", "update", otherBranch)
	if err != nil {
		t.Fatalf("branch model update dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	assertLivePreviewOf(t, dryRunOutput, "branch model update", jsonoutput.OutcomeWouldApply)

	defaultAfterOutput, err := executeLiveCLI(t, "--json", "branch", "default", "get")
	if err != nil {
		t.Fatalf("branch default get after failed: %v\noutput: %s", err, defaultAfterOutput)
	}
	if defaultBeforeOutput != defaultAfterOutput {
		t.Fatalf("expected no default-branch side-effect from model update dry-run\nbefore: %s\nafter: %s", defaultBeforeOutput, defaultAfterOutput)
	}
	assertMasterIsDefaultBranch(t, defaultAfterOutput)
}

func TestLiveCLIBranchRestrictionUpdateDryRunHasNoSideEffect(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// PATTERN rather than BRANCH, which is what --matcher-type defaults to.
	createOutput, err := executeLiveCLI(
		t, "--json", "branch", "restriction", "create",
		"--type", "read-only",
		"--matcher-type", "PATTERN",
		"--matcher-id", "refs/heads/release/*",
	)
	if err != nil {
		t.Fatalf("restriction create fixture failed: %v\noutput: %s", err, createOutput)
	}

	restrictionID := ""
	if restriction, ok := decodeJSONMap(t, createOutput)["restriction"].(map[string]any); ok {
		restrictionID = asString(restriction["id"])
	}
	if restrictionID == "" {
		t.Fatalf("expected restriction id in create output: %s", createOutput)
	}
	fixture := storedRestriction{scope: "REPOSITORY", restrictionType: "read-only", matcherType: "PATTERN", matcherID: "refs/heads/release/*"}

	// --limit has nothing to cut in a repository this test made;
	// TestLiveBranchRestrictionLimitCaps is where it is proven.
	listBeforeOutput, err := executeLiveCLI(t, "--json", "branch", "restriction", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("restriction list before failed: %v\noutput: %s", err, listBeforeOutput)
	}
	assertOnlyRestrictionListed(t, listBeforeOutput, restrictionID, fixture)

	// An update that changes something. The restriction as it stands would leave
	// the listing as it was whether or not the dry run sent the update.
	dryRunOutput, err := executeLiveCLI(
		t, "--json", "--dry-run", "branch", "restriction", "update", restrictionID,
		"--type", "read-only",
		"--matcher-type", "PATTERN",
		"--matcher-id", "refs/heads/release/*",
		"--group", "stash-users",
	)
	if err != nil {
		t.Fatalf("restriction update dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	assertLivePreviewOf(t, dryRunOutput, "branch restriction update", jsonoutput.OutcomeWouldApply)

	listAfterOutput, err := executeLiveCLI(t, "--json", "branch", "restriction", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("restriction list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no restriction side-effect from update dry-run\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}
	assertOnlyRestrictionListed(t, listAfterOutput, restrictionID, fixture)

	if output, err := executeLiveCLI(t, "--json", "branch", "restriction", "delete", restrictionID, "--yes"); err != nil {
		t.Fatalf("restriction delete cleanup failed: %v\noutput: %s", err, output)
	}
	if remaining := restrictionsInListing(t, mustLiveCLI(t, "branch", "restriction", "list")); len(remaining) != 0 {
		t.Fatalf("restrictions remain after the delete: %v", remaining)
	}
}

// branchOlderSeedCommit is the first of the two commits a repository was seeded
// with, which is second in the ids because Bitbucket lists the newest first. A
// branch started there points somewhere master does not.
func branchOlderSeedCommit(t *testing.T, repo seededRepository) string {
	t.Helper()

	if len(repo.CommitIDs) < 2 {
		t.Fatalf("want two seeded commits, the repository has %v", repo.CommitIDs)
	}

	return repo.CommitIDs[1]
}

// listedBranch is one branch in `bb branch list --json`.
type listedBranch struct {
	ID           string `json:"id"`
	DisplayID    string `json:"displayId"`
	LatestCommit string `json:"latestCommit"`
	Default      bool   `json:"default"`
}

// branchesInListing decodes the branches out of `bb branch list --json`, in
// the order they were listed.
func branchesInListing(t *testing.T, output string) []listedBranch {
	t.Helper()

	var listing struct {
		Branches []listedBranch `json:"branches"`
	}
	if err := decodeJSONEnvelopeData(output, &listing); err != nil {
		t.Fatalf("branch list returned invalid JSON: %v\n%s", err, output)
	}

	return listing.Branches
}

// assertOnlyBranchNamed reads a branch back through a listing filtered by its
// name, and checks the commit it points at.
func assertOnlyBranchNamed(t *testing.T, name, commit string) {
	t.Helper()

	branches := branchesInListing(t, mustLiveCLI(t, "branch", "list", "--filter", name, "--all"))
	if len(branches) != 1 || branches[0].DisplayID != name || branches[0].ID != "refs/heads/"+name || branches[0].LatestCommit != commit {
		t.Fatalf("want branch %s alone, at %s; the listing filtered by its name holds %+v", name, commit, branches)
	}
}

// assertMasterIsDefaultBranch reads `bb branch default get --json`.
func assertMasterIsDefaultBranch(t *testing.T, output string) {
	t.Helper()

	defaultBranch := nestedJSONMap(t, output, "defaultBranch")
	if defaultBranch["id"] != "refs/heads/master" || defaultBranch["displayId"] != "master" {
		t.Fatalf("the default branch is %v (%v), want master (refs/heads/master)", defaultBranch["displayId"], defaultBranch["id"])
	}
}

// restrictionsInListing decodes the restrictions out of a restriction list
// command's JSON output, for either scope.
func restrictionsInListing(t *testing.T, output string) []map[string]any {
	t.Helper()

	var listing struct {
		Restrictions []map[string]any `json:"restrictions"`
	}
	if err := decodeJSONEnvelopeData(output, &listing); err != nil {
		t.Fatalf("restriction list returned invalid JSON: %v\n%s", err, output)
	}

	return listing.Restrictions
}

// assertOnlyRestrictionListed checks that a listing holds one restriction, the
// one given, stored as the test expects.
func assertOnlyRestrictionListed(t *testing.T, output, id string, want storedRestriction) {
	t.Helper()

	restrictions := restrictionsInListing(t, output)
	if len(restrictions) != 1 {
		t.Fatalf("want restriction %s alone, the listing holds %d: %v", id, len(restrictions), restrictions)
	}
	if got, _ := numericOrStringID(restrictions[0]["id"]); got != id {
		t.Fatalf("want restriction %s alone, the listing holds %s: %v", id, got, restrictions[0])
	}
	assertRestrictionStored(t, restrictions[0], want)
}
