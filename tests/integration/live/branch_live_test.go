//go:build live

package live_test

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestLiveCLIBranchLifecycle(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	branchName := "feature/live-test-branch"
	startPoint := repo.CommitIDs[0]

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
	// --filter, --base and --details each add one query parameter, and a query
	// parameter built wrong does not fail: it comes back with the wrong branches
	// or with all of them. --filter is the one with a visible answer, so it is
	// the one asserted; the other two are asked for so that a request Bitbucket
	// refuses is a failure rather than a silent no-op.
	filtered := mustLiveCLI(t, "branch", "list", "--filter", branchName, "--all")
	if !strings.Contains(filtered, branchName) {
		t.Fatalf("--filter %s excluded the branch it names:\n%s", branchName, filtered)
	}
	if strings.Contains(filtered, `"displayId": "master"`) {
		t.Fatalf("--filter %s returned master as well, so it filtered nothing:\n%s", branchName, filtered)
	}

	mustLiveCLI(t, "branch", "list", "--base", "master", "--all")
	mustLiveCLI(t, "branch", "list", "--details", "--all")
	mustLiveCLI(t, "branch", "list", "--order-by", "ALPHABETICAL", "--all")

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
	deleteOutput, err := executeLiveCLI(t, "branch", "delete", branchName)
	if err != nil {
		t.Fatalf("branch delete failed: %v\noutput: %s", err, deleteOutput)
	}
}

func TestLiveCLIBranchRestrictionLifecycle(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
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

	// Get restriction
	getOutput, err := executeLiveCLI(t, "branch", "restriction", "get", restrictionID)
	if err != nil {
		t.Fatalf("restriction get failed: %v\noutput: %s", err, getOutput)
	}
	if !strings.Contains(getOutput, restrictionID) || !strings.Contains(getOutput, "read-only") {
		t.Fatalf("expected id and type in human get output, got: %s", getOutput)
	}

	updateOutput, err := executeLiveCLI(
		t, "--json", "branch", "restriction", "update", restrictionID,
		"--type", "no-deletes",
		"--matcher-type", "BRANCH",
		"--matcher-id", "refs/heads/master",
	)
	if err != nil {
		t.Fatalf("restriction update failed: %v\noutput: %s", err, updateOutput)
	}

	// Update the ID as it changed during delete+recreate (our update implementation for single restrictions)
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

	deleteOutput, err := executeLiveCLI(t, "--json", "branch", "restriction", "delete", restrictionID)
	if err != nil {
		t.Fatalf("restriction delete failed: %v\noutput: %s", err, deleteOutput)
	}
	if asString(decodeJSONMap(t, deleteOutput)["status"]) != "ok" {
		t.Fatalf("expected delete status ok, got: %s", deleteOutput)
	}
}

func TestLiveCLIBranchDeleteDryRunHasNoSideEffect(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	branchName := "feature/live-dry-run-delete"
	startPoint := repo.CommitIDs[0]

	createOutput, err := executeLiveCLI(t, "--json", "branch", "create", branchName, "--start-point", startPoint)
	if err != nil {
		t.Fatalf("branch create failed: %v\noutput: %s", err, createOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "branch", "delete", branchName)
	if err != nil {
		t.Fatalf("branch delete dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"planningMode": "stateful"`) {
		t.Fatalf("expected stateful dry-run output, got: %s", dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "branch.delete"`) {
		t.Fatalf("expected branch.delete intent, got: %s", dryRunOutput)
	}

	listOutput, err := executeLiveCLI(t, "--json", "branch", "list")
	if err != nil {
		t.Fatalf("branch list failed: %v\noutput: %s", err, listOutput)
	}
	if !strings.Contains(listOutput, branchName) {
		t.Fatalf("expected branch %s to remain after dry-run delete, got: %s", branchName, listOutput)
	}

	deleteOutput, err := executeLiveCLI(t, "branch", "delete", branchName)
	if err != nil {
		t.Fatalf("branch delete cleanup failed: %v\noutput: %s", err, deleteOutput)
	}
}

func TestLiveCLIBranchCreateDryRunHasNoSideEffect(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
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
	if !strings.Contains(dryRunOutput, `"planningMode": "stateful"`) {
		t.Fatalf("expected stateful dry-run output, got: %s", dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "branch.create"`) {
		t.Fatalf("expected branch.create intent, got: %s", dryRunOutput)
	}

	listAfterOutput, err := executeLiveCLI(t, "--json", "branch", "list")
	if err != nil {
		t.Fatalf("branch list after failed: %v\noutput: %s", err, listAfterOutput)
	}
	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no branch side-effect from create dry-run\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}
}

func TestLiveCLIBranchDefaultSetDryRunHasNoSideEffect(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	defaultBeforeOutput, err := executeLiveCLI(t, "--json", "branch", "default", "get")
	if err != nil {
		t.Fatalf("branch default get before failed: %v\noutput: %s", err, defaultBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "branch", "default", "set", "master")
	if err != nil {
		t.Fatalf("branch default set dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"planningMode": "stateful"`) {
		t.Fatalf("expected stateful dry-run output, got: %s", dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "branch.default.set"`) {
		t.Fatalf("expected branch.default.set intent, got: %s", dryRunOutput)
	}

	defaultAfterOutput, err := executeLiveCLI(t, "--json", "branch", "default", "get")
	if err != nil {
		t.Fatalf("branch default get after failed: %v\noutput: %s", err, defaultAfterOutput)
	}
	if defaultBeforeOutput != defaultAfterOutput {
		t.Fatalf("expected no default-branch side-effect from dry-run\nbefore: %s\nafter: %s", defaultBeforeOutput, defaultAfterOutput)
	}
}

func TestLiveCLIBranchRestrictionCreateDryRunHasNoSideEffect(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	listBeforeOutput, err := executeLiveCLI(t, "--json", "branch", "restriction", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("restriction list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "branch", "restriction", "create", "--type", "read-only", "--matcher-type", "BRANCH", "--matcher-id", "master")
	if err != nil {
		t.Fatalf("restriction create dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "branch.restriction.create"`) {
		t.Fatalf("expected branch.restriction.create intent, got: %s", dryRunOutput)
	}

	listAfterOutput, err := executeLiveCLI(t, "--json", "branch", "restriction", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("restriction list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no restriction side-effect from create dry-run\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}
}

func TestLiveCLIBranchRestrictionDeleteDryRunHasNoSideEffect(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
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

	listBeforeOutput, err := executeLiveCLI(t, "--json", "branch", "restriction", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("restriction list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "branch", "restriction", "delete", restrictionID)
	if err != nil {
		t.Fatalf("restriction delete dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "branch.restriction.delete"`) {
		t.Fatalf("expected branch.restriction.delete intent, got: %s", dryRunOutput)
	}

	listAfterOutput, err := executeLiveCLI(t, "--json", "branch", "restriction", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("restriction list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no restriction side-effect from delete dry-run\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}

	_, _ = executeLiveCLI(t, "--json", "branch", "restriction", "delete", restrictionID)
}

func TestLiveCLIBranchModelUpdateDryRunHasNoSideEffect(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	defaultBeforeOutput, err := executeLiveCLI(t, "--json", "branch", "default", "get")
	if err != nil {
		t.Fatalf("branch default get before failed: %v\noutput: %s", err, defaultBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "branch", "model", "update", "master")
	if err != nil {
		t.Fatalf("branch model update dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"planningMode": "stateful"`) {
		t.Fatalf("expected stateful dry-run output, got: %s", dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "branch.model.update"`) {
		t.Fatalf("expected branch.model.update intent, got: %s", dryRunOutput)
	}

	defaultAfterOutput, err := executeLiveCLI(t, "--json", "branch", "default", "get")
	if err != nil {
		t.Fatalf("branch default get after failed: %v\noutput: %s", err, defaultAfterOutput)
	}
	if defaultBeforeOutput != defaultAfterOutput {
		t.Fatalf("expected no default-branch side-effect from model update dry-run\nbefore: %s\nafter: %s", defaultBeforeOutput, defaultAfterOutput)
	}
}

func TestLiveCLIBranchRestrictionUpdateDryRunHasNoSideEffect(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	createOutput, err := executeLiveCLI(
		t, "--json", "branch", "restriction", "create",
		"--type", "read-only",
		"--matcher-type", "BRANCH",
		"--matcher-id", "refs/heads/master",
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

	listBeforeOutput, err := executeLiveCLI(t, "--json", "branch", "restriction", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("restriction list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(
		t, "--json", "--dry-run", "branch", "restriction", "update", restrictionID,
		"--type", "read-only",
		"--matcher-type", "BRANCH",
		"--matcher-id", "refs/heads/master",
	)
	if err != nil {
		t.Fatalf("restriction update dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "branch.restriction.update"`) {
		t.Fatalf("expected branch.restriction.update intent, got: %s", dryRunOutput)
	}

	listAfterOutput, err := executeLiveCLI(t, "--json", "branch", "restriction", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("restriction list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no restriction side-effect from update dry-run\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}

	_, _ = executeLiveCLI(t, "--json", "branch", "restriction", "delete", restrictionID)
}
