//go:build live

package live_test

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestLiveCLIProjectLifecycle(t *testing.T) {
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
	listOutput, err := executeLiveCLI(t, "project", "list", "--name", "Live Test")
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
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	listBeforeOutput, err := executeLiveCLI(t, "--json", "project", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("project list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "project", "delete", seeded.Key)
	if err != nil {
		t.Fatalf("project delete dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "project.delete"`) {
		t.Fatalf("expected project.delete intent, got: %s", dryRunOutput)
	}

	listAfterOutput, err := executeLiveCLI(t, "--json", "project", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("project list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no project side-effect from delete dry-run\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}
}
