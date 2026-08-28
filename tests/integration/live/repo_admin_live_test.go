//go:build live

package live_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestLiveCLIRepoAdminLifecycle(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	configureLiveCLIEnv(t, harness, seeded.Key, "test-repo")

	// Create
	createOutput, err := executeLiveCLI(t, "--json", "repo", "admin", "create", "--project", seeded.Key, "--name", "test-repo", "--description", "test desc")
	if err != nil {
		t.Fatalf("repo create failed: %v\noutput: %s", err, createOutput)
	}
	createPayload := decodeJSONMap(t, createOutput)
	repoObj, ok := createPayload["repository"].(map[string]any)
	if !ok || asString(repoObj["name"]) != "test-repo" {
		t.Fatalf("expected repository object with name, got: %s", createOutput)
	}

	// Update
	updateOutput, err := executeLiveCLI(t, "--json", "repo", "admin", "update", "--name", "test-repo-updated")
	if err != nil {
		t.Fatalf("repo update failed: %v\noutput: %s", err, updateOutput)
	}
	updatePayload := decodeJSONMap(t, updateOutput)
	updateObj, ok := updatePayload["repository"].(map[string]any)
	if !ok || asString(updateObj["name"]) != "test-repo-updated" {
		t.Fatalf("expected repository updated name, got: %s", updateOutput)
	}

	// Delete
	deleteOutput, err := executeLiveCLI(t, "--json", "repo", "admin", "delete")
	if err != nil {
		t.Fatalf("repo delete failed: %v\noutput: %s", err, deleteOutput)
	}
	deletePayload := decodeJSONMap(t, deleteOutput)
	if asString(deletePayload["status"]) != "ok" {
		t.Fatalf("expected delete status ok, got: %s", deleteOutput)
	}
}

func TestLiveCLIRepoAdminCreateDryRunNoSideEffect(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	name := fmt.Sprintf("dryrun-repo-%d", time.Now().UnixNano()%100000)

	listBeforeOutput, err := executeLiveCLI(t, "--json", "repo", "list")
	if err != nil {
		t.Fatalf("repo list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "repo", "admin", "create", "--project", seeded.Key, "--name", name)
	if err != nil {
		t.Fatalf("repo admin create dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"planning_mode": "stateful"`) {
		t.Fatalf("expected stateful planning mode, got: %s", dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "repo.admin.create"`) {
		t.Fatalf("expected repo.admin.create intent, got: %s", dryRunOutput)
	}

	listAfterOutput, err := executeLiveCLI(t, "--json", "repo", "list")
	if err != nil {
		t.Fatalf("repo list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no repository side-effect from admin create dry-run\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}
}

func TestLiveCLIRepoAdminUpdateDryRunNoSideEffect(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	repoName := fmt.Sprintf("dryrun-update-repo-%d", time.Now().UnixNano()%100000)
	configureLiveCLIEnv(t, harness, seeded.Key, repoName)

	createOutput, err := executeLiveCLI(t, "--json", "repo", "admin", "create", "--project", seeded.Key, "--name", repoName)
	if err != nil {
		t.Fatalf("repo create fixture failed: %v\noutput: %s", err, createOutput)
	}

	listBeforeOutput, err := executeLiveCLI(t, "--json", "repo", "list")
	if err != nil {
		t.Fatalf("repo list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "repo", "admin", "update", "--name", repoName+"-renamed")
	if err != nil {
		t.Fatalf("repo admin update dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "repo.admin.update"`) {
		t.Fatalf("expected repo.admin.update intent, got: %s", dryRunOutput)
	}

	listAfterOutput, err := executeLiveCLI(t, "--json", "repo", "list")
	if err != nil {
		t.Fatalf("repo list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no repository side-effect from admin update dry-run\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}

	_, _ = executeLiveCLI(t, "--json", "repo", "admin", "delete")
}

func TestLiveCLIRepoAdminDeleteDryRunNoSideEffect(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	repoName := fmt.Sprintf("dryrun-delete-repo-%d", time.Now().UnixNano()%100000)
	configureLiveCLIEnv(t, harness, seeded.Key, repoName)

	createOutput, err := executeLiveCLI(t, "--json", "repo", "admin", "create", "--project", seeded.Key, "--name", repoName)
	if err != nil {
		t.Fatalf("repo create fixture failed: %v\noutput: %s", err, createOutput)
	}

	listBeforeOutput, err := executeLiveCLI(t, "--json", "repo", "list")
	if err != nil {
		t.Fatalf("repo list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "repo", "admin", "delete")
	if err != nil {
		t.Fatalf("repo admin delete dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "repo.admin.delete"`) {
		t.Fatalf("expected repo.admin.delete intent, got: %s", dryRunOutput)
	}

	listAfterOutput, err := executeLiveCLI(t, "--json", "repo", "list")
	if err != nil {
		t.Fatalf("repo list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no repository side-effect from admin delete dry-run\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}

	_, _ = executeLiveCLI(t, "--json", "repo", "admin", "delete")
}

func TestLiveCLIRepoAdminForkDryRunNoSideEffect(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	listBeforeOutput, err := executeLiveCLI(t, "--json", "repo", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("repo list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	forkName := fmt.Sprintf("dryrun-fork-%d", time.Now().UnixNano()%100000)
	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "repo", "admin", "fork", "--repo", seeded.Key+"/"+repo.Slug, "--name", forkName)
	if err != nil {
		t.Fatalf("repo admin fork dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "repo.admin.fork"`) {
		t.Fatalf("expected repo.admin.fork intent, got: %s", dryRunOutput)
	}

	listAfterOutput, err := executeLiveCLI(t, "--json", "repo", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("repo list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no repository side-effect from admin fork dry-run\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}
}

func TestLiveCLIRepoLifecyclePromotedCanonical(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	repoName := fmt.Sprintf("canon-repo-%d", time.Now().UnixNano()%100000)
	forkName := fmt.Sprintf("canon-fork-%d", time.Now().UnixNano()%100000)

	configureLiveCLIEnv(t, harness, seeded.Key, repoName)

	// Canonical Create
	createOutput, err := executeLiveCLI(t, "--json", "repo", "create", "--project", seeded.Key, "--name", repoName, "--description", "promoted canonical create")
	if err != nil {
		t.Fatalf("repo create failed: %v\noutput: %s", err, createOutput)
	}
	createPayload := decodeJSONMap(t, createOutput)
	repoObj, ok := createPayload["repository"].(map[string]any)
	if !ok || asString(repoObj["name"]) != repoName {
		t.Fatalf("expected created repo name %s, got: %s", repoName, createOutput)
	}

	// Canonical Fork
	forkOutput, err := executeLiveCLI(t, "--json", "repo", "fork", "--repo", seeded.Key+"/"+repoName, "--name", forkName)
	if err != nil {
		t.Fatalf("repo fork failed: %v\noutput: %s", err, forkOutput)
	}
	forkPayload := decodeJSONMap(t, forkOutput)
	forkObj, ok := forkPayload["repository"].(map[string]any)
	if !ok || asString(forkObj["name"]) != forkName {
		t.Fatalf("expected forked repo name %s, got: %s", forkName, forkOutput)
	}

	// Canonical Delete Fork
	deleteForkOutput, err := executeLiveCLI(t, "--json", "repo", "delete", "--repo", seeded.Key+"/"+forkName)
	if err != nil {
		t.Fatalf("repo delete fork failed: %v\noutput: %s", err, deleteForkOutput)
	}

	// Canonical Delete Repo
	deleteOutput, err := executeLiveCLI(t, "--json", "repo", "delete", "--repo", seeded.Key+"/"+repoName)
	if err != nil {
		t.Fatalf("repo delete failed: %v\noutput: %s", err, deleteOutput)
	}
	deletePayload := decodeJSONMap(t, deleteOutput)
	if asString(deletePayload["status"]) != "ok" {
		t.Fatalf("expected delete status ok, got: %s", deleteOutput)
	}
}
