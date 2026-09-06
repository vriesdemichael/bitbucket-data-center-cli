//go:build live

package live_test

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestLiveProjectDefaultTaskLifecycle is the project twin of the repository
// default-task test. It is worth having both: the two live in separate services
// that built the same request body from separate copies of the same code, and
// both copies were wrong in the same way.
func TestLiveProjectDefaultTaskLifecycle(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	configureLiveCLIEnv(t, harness, seeded.Key, seeded.Repos[0].Slug)

	addOutput, err := executeLiveCLI(t, "--json", "project", "default-task", "add", seeded.Key, "live suite project task",
		"--source-ref", "refs/heads/feature/*", "--target-ref", "refs/heads/master")
	if err != nil {
		t.Fatalf("project default-task add failed: %v\noutput: %s", err, addOutput)
	}
	addData := decodeJSONMap(t, addOutput)
	assertMatcherID(t, addData, "sourceMatcher", "refs/heads/feature/*")
	assertMatcherID(t, addData, "targetMatcher", "refs/heads/master")

	taskID, ok := numericOrStringID(addData["id"])
	if !ok {
		t.Fatalf("expected a task id in the add output: %s", addOutput)
	}

	anyRefOutput, err := executeLiveCLI(t, "--json", "project", "default-task", "add", seeded.Key, "live suite project any-ref task")
	if err != nil {
		t.Fatalf("project default-task add without matchers failed: %v\noutput: %s", err, anyRefOutput)
	}
	anyRefData := decodeJSONMap(t, anyRefOutput)
	assertMatcherID(t, anyRefData, "sourceMatcher", "ANY_REF_MATCHER_ID")
	assertMatcherID(t, anyRefData, "targetMatcher", "ANY_REF_MATCHER_ID")
	if anyRefID, ok := numericOrStringID(anyRefData["id"]); ok {
		t.Cleanup(func() {
			_, _ = executeLiveCLI(t, "--json", "project", "default-task", "delete", seeded.Key, anyRefID)
		})
	}

	listOutput, err := executeLiveCLI(t, "--json", "project", "default-task", "list", seeded.Key)
	if err != nil {
		t.Fatalf("project default-task list failed: %v\noutput: %s", err, listOutput)
	}
	if !strings.Contains(listOutput, "live suite project task") {
		t.Fatalf("expected the added task in the listing, got: %s", listOutput)
	}

	if _, err := executeLiveCLI(t, "--json", "project", "default-task", "update", seeded.Key, taskID,
		"--description", "live suite project task updated"); err != nil {
		t.Fatalf("project default-task update failed: %v", err)
	}

	afterUpdate, err := executeLiveCLI(t, "--json", "project", "default-task", "list", seeded.Key)
	if err != nil {
		t.Fatalf("project default-task list after update failed: %v\noutput: %s", err, afterUpdate)
	}
	if !strings.Contains(afterUpdate, "live suite project task updated") {
		t.Fatalf("expected the update to persist, got: %s", afterUpdate)
	}

	if _, err := executeLiveCLI(t, "--json", "project", "default-task", "delete", seeded.Key, taskID); err != nil {
		t.Fatalf("project default-task delete failed: %v", err)
	}
}

// TestLiveProjectBranchRestrictionLifecycle covers create/get/list/update/delete.
//
// The restriction body is the same matcher-shaped payload as the default tasks,
// built independently again, so the same class of mistake is possible; here the
// live server is the only thing that says whether the matcher was understood.
func TestLiveProjectBranchRestrictionLifecycle(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	configureLiveCLIEnv(t, harness, seeded.Key, seeded.Repos[0].Slug)

	createOutput, err := executeLiveCLI(t, "--json", "project", "branch-restriction", "create", seeded.Key,
		"--type", "no-deletes", "--matcher-type", "PATTERN", "--matcher-id", "refs/heads/release/*",
		"--matcher-display", "release/*")
	if err != nil {
		t.Fatalf("project branch-restriction create failed: %v\noutput: %s", err, createOutput)
	}
	createData := decodeJSONMap(t, createOutput)
	restriction, ok := createData["restriction"].(map[string]any)
	if !ok {
		t.Fatalf("expected a restriction object in the create output: %s", createOutput)
	}
	restrictionID, ok := numericOrStringID(restriction["id"])
	if !ok {
		t.Fatalf("expected a restriction id in the create output: %s", createOutput)
	}
	t.Cleanup(func() {
		_, _ = executeLiveCLI(t, "--json", "project", "branch-restriction", "delete", seeded.Key, restrictionID)
	})

	getOutput, err := executeLiveCLI(t, "--json", "project", "branch-restriction", "get", seeded.Key, restrictionID)
	if err != nil {
		t.Fatalf("project branch-restriction get failed: %v\noutput: %s", err, getOutput)
	}
	// The matcher round-tripping is the point: a restriction stored against the
	// wrong ref silently protects nothing.
	if !strings.Contains(getOutput, "refs/heads/release/*") {
		t.Fatalf("expected the matcher in the get output, got: %s", getOutput)
	}

	listOutput, err := executeLiveCLI(t, "--json", "project", "branch-restriction", "list", seeded.Key)
	if err != nil {
		t.Fatalf("project branch-restriction list failed: %v\noutput: %s", err, listOutput)
	}
	if !strings.Contains(listOutput, "refs/heads/release/*") {
		t.Fatalf("expected the restriction in the listing, got: %s", listOutput)
	}

	updateOutput, err := executeLiveCLI(t, "--json", "project", "branch-restriction", "update", seeded.Key, restrictionID,
		"--type", "no-deletes", "--matcher-type", "PATTERN", "--matcher-id", "refs/heads/hotfix/*",
		"--matcher-display", "hotfix/*")
	if err != nil {
		t.Fatalf("project branch-restriction update failed: %v\noutput: %s", err, updateOutput)
	}
	if !strings.Contains(updateOutput, "refs/heads/hotfix/*") {
		t.Fatalf("expected the updated matcher in the output, got: %s", updateOutput)
	}

	deleteOutput, err := executeLiveCLI(t, "--json", "project", "branch-restriction", "delete", seeded.Key, restrictionID)
	if err != nil {
		t.Fatalf("project branch-restriction delete failed: %v\noutput: %s", err, deleteOutput)
	}
}
