//go:build live

package live_test

import (
	"context"
	"strings"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
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

	// Create
	// The name is derived too, not just the key. Bitbucket enforces unique
	// project names, so a constant one turned any interrupted run into a
	// permanent failure: the project survived, and every later run reported a
	// 409 from project create that read like the change under test had broken
	// project creation.
	//
	// It comes before the listing so that the name filter has a project to
	// leave out.
	newKey := seeded.Key + "X"
	createOutput, err := executeLiveCLI(t, "--json", "project", "create", newKey, "--name", "Test Project "+newKey)
	if err != nil {
		t.Fatalf("project create failed: %v\noutput: %s", err, createOutput)
	}
	deleted := false
	t.Cleanup(func() {
		if !deleted {
			_, _ = executeLiveCLI(t, "--json", "project", "delete", newKey, "--yes")
		}
	})
	createPayload := decodeJSONMap(t, createOutput)
	createObj, ok := createPayload["project"].(map[string]any)
	if !ok || asString(createObj["key"]) != newKey {
		t.Fatalf("expected new project key in create output, got: %s", createOutput)
	}
	assertProjectKeyAndName(t, mustLiveCLI(t, "project", "get", newKey), newKey, "Test Project "+newKey)

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

	// Only this project, which is the filter's effect: without it the listing
	// is every project on the instance, the one created above among them.
	var listed struct {
		Projects []struct {
			Key  string `json:"key"`
			Name string `json:"name"`
		} `json:"projects"`
	}
	filtered := mustLiveCLI(t, "project", "list", "--name", seeded.Name)
	if err := decodeJSONEnvelopeData(filtered, &listed); err != nil {
		t.Fatalf("project list returned invalid JSON: %v\n%s", err, filtered)
	}
	if len(listed.Projects) != 1 || listed.Projects[0].Key != seeded.Key || listed.Projects[0].Name != seeded.Name {
		t.Fatalf("--name %q listed %v, want only %s", seeded.Name, listed.Projects, seeded.Key)
	}

	// Get
	getOutput, err := executeLiveCLI(t, "project", "get", seeded.Key)
	if err != nil {
		t.Fatalf("project get failed: %v\noutput: %s", err, getOutput)
	}
	if !strings.Contains(getOutput, "Key: "+seeded.Key) {
		t.Fatalf("expected project key in get output, got: %s", getOutput)
	}

	// Update
	updateOutput, err := executeLiveCLI(t, "--json", "project", "update", newKey, "--name", "Updated Test Project "+newKey)
	if err != nil {
		t.Fatalf("project update failed: %v\noutput: %s", err, updateOutput)
	}
	updatePayload := decodeJSONMap(t, updateOutput)
	updateObj, ok := updatePayload["project"].(map[string]any)
	if !ok || asString(updateObj["name"]) != "Updated Test Project "+newKey {
		t.Fatalf("expected updated project name in output, got: %s", updateOutput)
	}
	assertProjectKeyAndName(t, mustLiveCLI(t, "project", "get", newKey), newKey, "Updated Test Project "+newKey)

	// Delete
	deleteOutput, err := executeLiveCLI(t, "--json", "project", "delete", newKey, "--yes")
	if err != nil {
		t.Fatalf("project delete failed: %v\noutput: %s", err, deleteOutput)
	}
	deleted = true
	deletePayload := decodeJSONMap(t, deleteOutput)
	if asString(deletePayload["status"]) != "ok" {
		t.Fatalf("expected project delete status ok, got: %s", deleteOutput)
	}
	if output, err := executeLiveCLI(t, "--json", "project", "get", newKey); !apperrors.IsKind(err, apperrors.KindNotFound) {
		t.Fatalf("project %s is still there after its delete answered ok: %v\n%s", newKey, err, output)
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
	assertProjectKeyAndName(t, getAfterOutput, seeded.Key, seeded.Name)
}

func TestLiveCLIProjectDeleteDryRunNoSideEffect(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// An empty project, not a seeded one. Bitbucket refuses to delete a project
	// that holds a repository, so a seeded project would survive a delete the
	// dry run had sent for real.
	projectKey, projectName, err := harness.createProject(ctx, "LT", "Live Test")
	if err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cleanupCancel()
		harness.deleteProjectAndContents(cleanupCtx, projectKey)
	})

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "project", "delete", projectKey, "--yes")
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
	getOutput, getErr := executeLiveCLI(t, "--json", "project", "get", projectKey)
	if getErr != nil {
		t.Fatalf("the delete dry-run removed project %s: %v\noutput: %s", projectKey, getErr, getOutput)
	}
	assertProjectKeyAndName(t, getOutput, projectKey, projectName)
}

// assertProjectKeyAndName reads the key and name out of `bb project get` and
// compares them with what was sent.
func assertProjectKeyAndName(t *testing.T, getOutput, key, name string) {
	t.Helper()

	project := nestedJSONMap(t, getOutput, "project")
	if project["key"] != key || project["name"] != name {
		t.Fatalf("project %s is stored as %v named %q, want %s named %q", key, project["key"], project["name"], key, name)
	}
}
