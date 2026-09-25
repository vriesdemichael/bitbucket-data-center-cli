//go:build live

package live_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

func TestLiveCLIRepoAdminLifecycle(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
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

	// The create's answer describes the request; a get of its own says what was
	// stored.
	created := repoAdminReadBack(t, seeded.Key+"/test-repo")
	if created["projectKey"] != seeded.Key || created["name"] != "test-repo" || created["description"] != "test desc" {
		t.Fatalf("the created repository reads back as %v", created)
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

	// The slug follows the name, so the renamed repository has a new address.
	if renamed := repoAdminReadBack(t, seeded.Key+"/test-repo-updated"); renamed["name"] != "test-repo-updated" {
		t.Fatalf("the renamed repository reads back as %v", renamed)
	}

	// Delete
	deleteOutput, err := executeLiveCLI(t, "--json", "repo", "admin", "delete", seeded.Key+"/test-repo", "--yes")
	if err != nil {
		t.Fatalf("repo delete failed: %v\noutput: %s", err, deleteOutput)
	}
	deletePayload := decodeJSONMap(t, deleteOutput)
	if asString(deletePayload["status"]) != "ok" {
		t.Fatalf("expected delete status ok, got: %s", deleteOutput)
	}

	// The old slug redirects to the new one, and Bitbucket answers a delete of a
	// repository that does not exist with 204, so the status above does not say
	// that anything was deleted.
	assertRepoAdminGone(t, seeded.Key+"/test-repo-updated")
}

func TestLiveCLIRepoAdminCreateDryRunNoSideEffect(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	name := testsupport.UniqueName("dryrun-repo-")

	listBefore := projectRepositoryListing(t, seeded.Key)

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "repo", "admin", "create", "--project", seeded.Key, "--name", name)
	if err != nil {
		t.Fatalf("repo admin create dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	assertLivePreviewOf(t, dryRunOutput, "repo create", jsonoutput.OutcomeWouldApply)

	if listAfter := projectRepositoryListing(t, seeded.Key); listAfter != listBefore {
		t.Fatalf("expected no repository side-effect from admin create dry-run\nbefore: %s\nafter: %s", listBefore, listAfter)
	}
}

func TestLiveCLIRepoAdminUpdateDryRunNoSideEffect(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	repoName := testsupport.UniqueName("dryrun-update-repo-")
	configureLiveCLIEnv(t, harness, seeded.Key, repoName)

	createOutput, err := executeLiveCLI(t, "--json", "repo", "admin", "create", "--project", seeded.Key, "--name", repoName)
	if err != nil {
		t.Fatalf("repo create fixture failed: %v\noutput: %s", err, createOutput)
	}
	if fixture := repoAdminReadBack(t, seeded.Key+"/"+repoName); fixture["projectKey"] != seeded.Key || fixture["name"] != repoName {
		t.Fatalf("the fixture repository reads back as %v", fixture)
	}

	listBefore := projectRepositoryListing(t, seeded.Key)

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "repo", "admin", "update", "--name", repoName+"-renamed")
	if err != nil {
		t.Fatalf("repo admin update dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	assertLivePreviewOf(t, dryRunOutput, "repo admin update", jsonoutput.OutcomeWouldApply)

	if listAfter := projectRepositoryListing(t, seeded.Key); listAfter != listBefore {
		t.Fatalf("expected no repository side-effect from admin update dry-run\nbefore: %s\nafter: %s", listBefore, listAfter)
	}

	_, _ = executeLiveCLI(t, "--json", "repo", "admin", "delete", seeded.Key+"/"+repoName, "--yes")
}

func TestLiveCLIRepoAdminDeleteDryRunNoSideEffect(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	repoName := testsupport.UniqueName("dryrun-delete-repo-")
	configureLiveCLIEnv(t, harness, seeded.Key, repoName)

	createOutput, err := executeLiveCLI(t, "--json", "repo", "admin", "create", "--project", seeded.Key, "--name", repoName)
	if err != nil {
		t.Fatalf("repo create fixture failed: %v\noutput: %s", err, createOutput)
	}
	if fixture := repoAdminReadBack(t, seeded.Key+"/"+repoName); fixture["projectKey"] != seeded.Key || fixture["name"] != repoName {
		t.Fatalf("the fixture repository reads back as %v", fixture)
	}

	listBefore := projectRepositoryListing(t, seeded.Key)

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "repo", "admin", "delete", "--yes")
	if err != nil {
		t.Fatalf("repo admin delete dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	assertLivePreviewOf(t, dryRunOutput, "repo delete", jsonoutput.OutcomeWouldApply)

	if listAfter := projectRepositoryListing(t, seeded.Key); listAfter != listBefore {
		t.Fatalf("expected no repository side-effect from admin delete dry-run\nbefore: %s\nafter: %s", listBefore, listAfter)
	}

	_, _ = executeLiveCLI(t, "--json", "repo", "admin", "delete", seeded.Key+"/"+repoName, "--yes")
}

func TestLiveCLIRepoAdminForkDryRunNoSideEffect(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	listBefore := projectRepositoryListing(t, seeded.Key)

	forkName := testsupport.UniqueName("dryrun-fork-")
	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "repo", "admin", "fork", "--repo", seeded.Key+"/"+repo.Slug, "--name", forkName)
	if err != nil {
		t.Fatalf("repo admin fork dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	assertLivePreviewOf(t, dryRunOutput, "repo fork", jsonoutput.OutcomeWouldApply)

	if listAfter := projectRepositoryListing(t, seeded.Key); listAfter != listBefore {
		t.Fatalf("expected no repository side-effect from admin fork dry-run\nbefore: %s\nafter: %s", listBefore, listAfter)
	}

	// A fork given no --project lands in the personal project of whoever forks,
	// not beside its origin, so that is where one would show up: the project
	// listing above could not have seen it.
	assertRepoAdminGone(t, "~"+strings.ToUpper(harness.username())+"/"+forkName)
}

func TestLiveCLIRepoLifecyclePromotedCanonical(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	repoName := testsupport.UniqueName("canon-repo-")
	forkName := testsupport.UniqueName("canon-fork-")

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

	created := repoAdminReadBack(t, seeded.Key+"/"+repoName)
	if created["projectKey"] != seeded.Key || created["name"] != repoName || created["description"] != "promoted canonical create" {
		t.Fatalf("the created repository reads back as %v", created)
	}

	// Canonical Fork
	//
	// Into the seeded project, where the delete below looks. Without --project
	// the fork went to the personal project of whoever forks, the delete of a
	// repository that was not there answered 204, and every run left a fork
	// behind in ~ADMIN.
	forkOutput, err := executeLiveCLI(t, "--json", "repo", "fork", "--repo", seeded.Key+"/"+repoName, "--name", forkName, "--project", seeded.Key)
	if err != nil {
		t.Fatalf("repo fork failed: %v\noutput: %s", err, forkOutput)
	}
	forkPayload := decodeJSONMap(t, forkOutput)
	forkObj, ok := forkPayload["repository"].(map[string]any)
	if !ok || asString(forkObj["name"]) != forkName {
		t.Fatalf("expected forked repo name %s, got: %s", forkName, forkOutput)
	}

	fork := repoAdminReadBack(t, seeded.Key+"/"+forkName)
	origin, _ := fork["origin"].(map[string]any)
	if fork["projectKey"] != seeded.Key || fork["name"] != forkName || origin["projectKey"] != seeded.Key || origin["slug"] != repoName {
		t.Fatalf("the fork reads back as %v, want %s in %s forked from %s", fork, forkName, seeded.Key, seeded.Key+"/"+repoName)
	}

	// Canonical Delete Fork
	deleteForkOutput, err := executeLiveCLI(t, "--json", "repo", "delete", "--repo", seeded.Key+"/"+forkName, "--yes")
	if err != nil {
		t.Fatalf("repo delete fork failed: %v\noutput: %s", err, deleteForkOutput)
	}
	assertRepoAdminGone(t, seeded.Key+"/"+forkName)

	// Canonical Delete Repo
	deleteOutput, err := executeLiveCLI(t, "--json", "repo", "delete", "--repo", seeded.Key+"/"+repoName, "--yes")
	if err != nil {
		t.Fatalf("repo delete failed: %v\noutput: %s", err, deleteOutput)
	}
	deletePayload := decodeJSONMap(t, deleteOutput)
	if asString(deletePayload["status"]) != "ok" {
		t.Fatalf("expected delete status ok, got: %s", deleteOutput)
	}
	assertRepoAdminGone(t, seeded.Key+"/"+repoName)
}

// repoAdminReadBack reads a repository's details through bb repo get, a
// request of its own rather than the answer to the write that set them.
func repoAdminReadBack(t *testing.T, repoRef string) map[string]any {
	t.Helper()

	output := mustLiveCLI(t, "repo", "get", "--repo", repoRef, "--readme=false")
	detail, ok := decodeJSONMap(t, output)["repository"].(map[string]any)
	if !ok {
		t.Fatalf("no repository in the repo get output for %s: %s", repoRef, output)
	}

	return detail
}

// assertRepoAdminGone checks that a repository reads back as not found.
func assertRepoAdminGone(t *testing.T, repoRef string) {
	t.Helper()

	output, err := executeLiveCLI(t, "--json", "repo", "get", "--repo", repoRef, "--readme=false")
	if err == nil {
		t.Fatalf("%s exists:\n%s", repoRef, output)
	}
	if !apperrors.IsKind(err, apperrors.KindNotFound) {
		t.Fatalf("reading %s back failed for a reason other than its absence: %v", repoRef, err)
	}
}
