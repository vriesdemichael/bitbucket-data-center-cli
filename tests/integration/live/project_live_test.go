//go:build live

package live_test

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestLiveCLIProjectLifecycle(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// List
	// Filtered by this project's own name, not the "Live Test" prefix every
	// seeded project shares: the prefix matches the whole suite, so the first
	// page of the answer is whichever tests seeded most recently.
	listOutput, err := executeLiveCLI(t, "project", "list", "--name", seeded.Name)
	if err != nil {
		t.Fatalf("project list failed: %v\noutput: %s", err, listOutput)
	}
	if !strings.Contains(listOutput, seeded.Key) {
		t.Fatalf("expected seeded project in list output, got: %s", listOutput)
	}

	// Get
	getOutput, err := executeLiveCLI(t, "project", "get", seeded.Key)
	if err != nil {
		t.Fatalf("project get failed: %v\noutput: %s", err, getOutput)
	}
	if !strings.Contains(getOutput, "Key: "+seeded.Key) {
		t.Fatalf("expected project key in get output, got: %s", getOutput)
	}

	// Create
	newKey := seeded.Key + "X"
	createOutput, err := executeLiveCLI(t, "--json", "project", "create", newKey, "--name", "Test Project X")
	if err != nil {
		t.Fatalf("project create failed: %v\noutput: %s", err, createOutput)
	}
	createPayload := decodeJSONMap(t, createOutput)
	createObj, ok := createPayload["project"].(map[string]any)
	if !ok || asString(createObj["key"]) != newKey {
		t.Fatalf("expected new project key in create output, got: %s", createOutput)
	}

	// Update
	updateOutput, err := executeLiveCLI(t, "--json", "project", "update", newKey, "--name", "Updated Test Project X")
	if err != nil {
		t.Fatalf("project update failed: %v\noutput: %s", err, updateOutput)
	}
	updatePayload := decodeJSONMap(t, updateOutput)
	updateObj, ok := updatePayload["project"].(map[string]any)
	if !ok || asString(updateObj["name"]) != "Updated Test Project X" {
		t.Fatalf("expected updated project name in output, got: %s", updateOutput)
	}

	// Delete
	deleteOutput, err := executeLiveCLI(t, "--json", "project", "delete", newKey)
	if err != nil {
		t.Fatalf("project delete failed: %v\noutput: %s", err, deleteOutput)
	}
	deletePayload := decodeJSONMap(t, deleteOutput)
	if asString(deletePayload["status"]) != "ok" {
		t.Fatalf("expected project delete status ok, got: %s", deleteOutput)
	}
}

func TestLiveCLIProjectUpdateDryRunNoSideEffect(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	getBeforeOutput, err := executeLiveCLI(t, "--json", "project", "get", seeded.Key)
	if err != nil {
		t.Fatalf("project get before failed: %v\noutput: %s", err, getBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "project", "update", seeded.Key, "--name", "Dry Run Updated Name")
	if err != nil {
		t.Fatalf("project update dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "project.update"`) {
		t.Fatalf("expected project.update intent, got: %s", dryRunOutput)
	}

	getAfterOutput, err := executeLiveCLI(t, "--json", "project", "get", seeded.Key)
	if err != nil {
		t.Fatalf("project get after failed: %v\noutput: %s", err, getAfterOutput)
	}

	if getBeforeOutput != getAfterOutput {
		t.Fatalf("expected no project side-effect from update dry-run\nbefore: %s\nafter: %s", getBeforeOutput, getAfterOutput)
	}
}

func TestLiveCLIProjectDeleteDryRunNoSideEffect(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "project", "delete", seeded.Key)
	if err != nil {
		t.Fatalf("project delete dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "project.delete"`) {
		t.Fatalf("expected project.delete intent, got: %s", dryRunOutput)
	}

	// The project itself, rather than a byte comparison of the instance-wide
	// listing before and after: that listing changes whenever any other test
	// seeds or removes a project, which under a parallel suite is constantly,
	// and none of it is this dry run's doing. What the dry run must not have
	// done is delete this one.
	if getOutput, getErr := executeLiveCLI(t, "--json", "project", "get", seeded.Key); getErr != nil {
		t.Fatalf("the delete dry-run removed project %s: %v\noutput: %s", seeded.Key, getErr, getOutput)
	}
}
