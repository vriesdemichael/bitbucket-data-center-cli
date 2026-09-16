//go:build live

package live_test

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
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
	anyRefID, ok := numericOrStringID(anyRefData["id"])
	if !ok {
		t.Fatalf("expected a task id in the add output: %s", anyRefOutput)
	}
	t.Cleanup(func() {
		_, _ = executeLiveCLI(t, "--json", "project", "default-task", "delete", seeded.Key, anyRefID, "--yes")
	})

	listOutput, err := executeLiveCLI(t, "--json", "project", "default-task", "list", seeded.Key)
	if err != nil {
		t.Fatalf("project default-task list failed: %v\noutput: %s", err, listOutput)
	}
	if !strings.Contains(listOutput, "live suite project task") {
		t.Fatalf("expected the added task in the listing, got: %s", listOutput)
	}

	// bb's listing carries each matcher's id but not its type, which bb
	// inferred from the ref and sent -- a glob as a pattern, anything else as a
	// branch. The type is read from Bitbucket's own listing instead.
	rawTasks := "/rest/default-tasks/latest/projects/" + seeded.Key + "/tasks"
	listed := projectDefaultTasksByID(t, listOutput)
	assertProjectDefaultTask(t, listed, taskID, "live suite project task", "refs/heads/feature/*", "refs/heads/master")
	assertProjectDefaultTask(t, listed, anyRefID, "live suite project any-ref task", "ANY_REF_MATCHER_ID", "ANY_REF_MATCHER_ID")
	stored := rawProjectDefaultTasksByID(t, mustLiveCLI(t, "api", rawTasks))
	assertProjectDefaultTaskMatcherTypes(t, stored, taskID, "PATTERN", "BRANCH")
	assertProjectDefaultTaskMatcherTypes(t, stored, anyRefID, "ANY_REF", "ANY_REF")

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
	// The matchers too, which the update did not name: bb sends the stored ones
	// again, and an update that reset them would widen the task to every pull
	// request in the project.
	assertProjectDefaultTask(t, projectDefaultTasksByID(t, afterUpdate), taskID, "live suite project task updated", "refs/heads/feature/*", "refs/heads/master")
	assertProjectDefaultTaskMatcherTypes(t, rawProjectDefaultTasksByID(t, mustLiveCLI(t, "api", rawTasks)), taskID, "PATTERN", "BRANCH")

	if _, err := executeLiveCLI(t, "--json", "project", "default-task", "delete", seeded.Key, taskID, "--yes"); err != nil {
		t.Fatalf("project default-task delete failed: %v", err)
	}

	afterDelete := projectDefaultTasksByID(t, mustLiveCLI(t, "project", "default-task", "list", seeded.Key))
	if task, found := afterDelete[taskID]; found {
		t.Errorf("task %s is still listed after its delete: %+v", taskID, task)
	}
	assertProjectDefaultTask(t, afterDelete, anyRefID, "live suite project any-ref task", "ANY_REF_MATCHER_ID", "ANY_REF_MATCHER_ID")
}

// projectDefaultTaskEntry is one default task as a listing reports it: bb's,
// which leaves the matcher types out, or Bitbucket's own, which has them.
type projectDefaultTaskEntry struct {
	ID            int64                     `json:"id"`
	Description   string                    `json:"description"`
	SourceMatcher projectDefaultTaskMatcher `json:"sourceMatcher"`
	TargetMatcher projectDefaultTaskMatcher `json:"targetMatcher"`
}

type projectDefaultTaskMatcher struct {
	ID   string `json:"id"`
	Type struct {
		ID string `json:"id"`
	} `json:"type"`
}

// projectDefaultTasksByID decodes `bb project default-task list --json`.
func projectDefaultTasksByID(t *testing.T, output string) map[string]projectDefaultTaskEntry {
	t.Helper()

	var tasks []projectDefaultTaskEntry
	if err := decodeJSONEnvelopeData(output, &tasks); err != nil {
		t.Fatalf("project default-task list returned invalid JSON: %v\n%s", err, output)
	}

	return indexProjectDefaultTasks(tasks)
}

// rawProjectDefaultTasksByID decodes the page `bb api` returns for the
// project's default-task endpoint.
func rawProjectDefaultTasksByID(t *testing.T, output string) map[string]projectDefaultTaskEntry {
	t.Helper()

	var page struct {
		Values []projectDefaultTaskEntry `json:"values"`
	}
	if err := decodeJSONEnvelopeData(output, &page); err != nil {
		t.Fatalf("the default-task endpoint returned invalid JSON: %v\n%s", err, output)
	}

	return indexProjectDefaultTasks(page.Values)
}

func indexProjectDefaultTasks(tasks []projectDefaultTaskEntry) map[string]projectDefaultTaskEntry {
	byID := make(map[string]projectDefaultTaskEntry, len(tasks))
	for _, task := range tasks {
		byID[strconv.FormatInt(task.ID, 10)] = task
	}

	return byID
}

func assertProjectDefaultTask(t *testing.T, tasks map[string]projectDefaultTaskEntry, id, description, source, target string) {
	t.Helper()

	task, found := tasks[id]
	if !found {
		t.Fatalf("task %s is not listed: %+v", id, tasks)
	}
	if task.Description != description || task.SourceMatcher.ID != source || task.TargetMatcher.ID != target {
		t.Errorf("task %s is %q from %s to %s, want %q from %s to %s", id,
			task.Description, task.SourceMatcher.ID, task.TargetMatcher.ID, description, source, target)
	}
}

func assertProjectDefaultTaskMatcherTypes(t *testing.T, tasks map[string]projectDefaultTaskEntry, id, source, target string) {
	t.Helper()

	task, found := tasks[id]
	if !found {
		t.Fatalf("task %s is not in Bitbucket's listing: %+v", id, tasks)
	}
	if task.SourceMatcher.Type.ID != source || task.TargetMatcher.Type.ID != target {
		t.Errorf("task %s matches %s to %s, want %s to %s", id, task.SourceMatcher.Type.ID, task.TargetMatcher.Type.ID, source, target)
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

	// --matcher-display is sent and Bitbucket ignores it: it derives a
	// matcher's display id itself, and for a project pattern that is the id.
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
	// The update below replaces the restriction, and this is reassigned to the
	// one that replaced it.
	t.Cleanup(func() {
		_, _ = executeLiveCLI(t, "--json", "project", "branch-restriction", "delete", seeded.Key, restrictionID, "--yes")
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
	stored := restrictionPayload(t, getOutput)
	assertRestrictionStored(t, stored, storedRestriction{restrictionType: "no-deletes", matcherType: "PATTERN", matcherID: "refs/heads/release/*"})
	if matcher, _ := stored["matcher"].(map[string]any); matcher["displayId"] != "refs/heads/release/*" {
		t.Errorf("matcher.displayId = %v, want the id, which Bitbucket derived while ignoring --matcher-display", matcher["displayId"])
	}

	listOutput, err := executeLiveCLI(t, "--json", "project", "branch-restriction", "list", seeded.Key)
	if err != nil {
		t.Fatalf("project branch-restriction list failed: %v\noutput: %s", err, listOutput)
	}
	if !strings.Contains(listOutput, "refs/heads/release/*") {
		t.Fatalf("expected the restriction in the listing, got: %s", listOutput)
	}
	if listed := restrictionsMatching(t, listOutput, "no-deletes", "refs/heads/release/*"); len(listed) != 1 {
		t.Errorf("want one no-deletes restriction on release/* in the listing, got %d: %v", len(listed), listed)
	} else if id, _ := numericOrStringID(listed[0]["id"]); id != restrictionID {
		t.Errorf("the listing holds restriction %s on release/*, the create answered with %s", id, restrictionID)
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

	// Another matcher is another restriction: the update created it and removed
	// the one on release/*, so the id to delete is the new one.
	replacedID := restrictionID
	restrictionID, ok = numericOrStringID(restrictionPayload(t, updateOutput)["id"])
	if !ok {
		t.Fatalf("expected a restriction id in the update output: %s", updateOutput)
	}
	afterUpdate := mustLiveCLI(t, "project", "branch-restriction", "list", seeded.Key)
	if old := restrictionsMatching(t, afterUpdate, "no-deletes", "refs/heads/release/*"); len(old) != 0 {
		t.Errorf("restriction %s on release/* survived an update that moved it: %v", replacedID, old)
	}
	if moved := restrictionsMatching(t, afterUpdate, "no-deletes", "refs/heads/hotfix/*"); len(moved) != 1 {
		t.Errorf("want one no-deletes restriction on hotfix/*, got %d: %v", len(moved), moved)
	} else {
		assertRestrictionStored(t, moved[0], storedRestriction{restrictionType: "no-deletes", matcherType: "PATTERN", matcherID: "refs/heads/hotfix/*"})
		if id, _ := numericOrStringID(moved[0]["id"]); id != restrictionID {
			t.Errorf("the listing holds restriction %s on hotfix/*, the update answered with %s", id, restrictionID)
		}
		if matcher, _ := moved[0]["matcher"].(map[string]any); matcher["displayId"] != "refs/heads/hotfix/*" {
			t.Errorf("matcher.displayId = %v, want the id, which Bitbucket derived while ignoring --matcher-display", matcher["displayId"])
		}
	}

	deleteOutput, err := executeLiveCLI(t, "--json", "project", "branch-restriction", "delete", seeded.Key, restrictionID, "--yes")
	if err != nil {
		t.Fatalf("project branch-restriction delete failed: %v\noutput: %s", err, deleteOutput)
	}

	// Bitbucket answers a delete of a restriction id that does not exist with
	// 204 too, so the delete succeeding shows nothing on its own.
	if output, err := executeLiveCLI(t, "--json", "project", "branch-restriction", "get", seeded.Key, restrictionID); !apperrors.IsKind(err, apperrors.KindNotFound) {
		t.Errorf("restriction %s can still be read after its delete: %v\n%s", restrictionID, err, output)
	}
	if remaining := restrictionsMatching(t, mustLiveCLI(t, "project", "branch-restriction", "list", seeded.Key), "no-deletes", "refs/heads/hotfix/*"); len(remaining) != 0 {
		t.Errorf("the restriction on hotfix/* is still listed after its delete: %v", remaining)
	}
}
