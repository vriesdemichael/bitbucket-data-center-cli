//go:build live

package live_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// TestLiveScopeNamedInsteadOfRepoInsideACheckout covers #725: a command that
// takes --project, or --role, in place of --repo had --repo filled in from the
// git remote beside it.
//
// Run from a clone, `bb reviewer-group list --project PROJ` was refused as
// naming both, `bb repo ssh-key add --project PROJ` the same, and `bb search
// prs --role author` as combining a role with a repository. `bb reviewer
// condition` refused nothing: it preferred --repo, so a condition created for
// the project was created on the repository instead.
//
// Every call here runs from the clone and names nothing but the scope under
// test, and every write is read back from the scope it was meant for.
//
// Not parallel: the clone is the working directory, and that belongs to the
// process.
func TestLiveScopeNamedInsteadOfRepoInsideACheckout(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	// A second repository for the pull request the dashboard has to find: one
	// in the checkout's own repository would be found by a repository search
	// too, and could not tell the two apart.
	seeded, err := harness.seedRepo(ctx, repoSeed{Repos: 2})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	elsewhere := seeded.Repos[1]
	elsewhereRef := seeded.Key + "/" + elsewhere.Slug

	user := harness.username()
	userID, err := harness.userID(ctx, user)
	if err != nil {
		t.Fatalf("look up %s's id failed: %v", user, err)
	}

	pushURL, err := repositoryPushURL(harness.config, seeded.Key, repo.Slug)
	if err != nil {
		t.Fatalf("build push url: %v", err)
	}
	checkout := t.TempDir()
	if err := runGit(checkout, "init"); err != nil {
		t.Fatalf("git init failed: %v", err)
	}
	if err := runGit(checkout, "remote", "add", "origin", pushURL); err != nil {
		t.Fatalf("git remote add failed: %v", err)
	}
	originalDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	if err := os.Chdir(checkout); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalDirectory) })

	// A group on the repository, named explicitly, which a listing of the
	// project must not hold.
	repoGroup := testsupport.UniqueName("repo-group-")
	mustLiveCLIUnscoped(t, "reviewer-group", "create", repoGroup, "--users", user, "--repo", repoRef)

	// Every case below is about inference not happening, and would pass just
	// the same in a directory where it never happens. This is what says it does
	// happen here when nothing else names the scope.
	t.Run("without another scope the checkout is the repository", func(t *testing.T) {
		output, err := executeLiveCLIUnscoped(t, "reviewer-group", "list")
		if err != nil {
			t.Fatalf("reviewer-group list failed inside the checkout: %v\n%s", err, output)
		}
		if !strings.Contains(output, `Using repository context from git remote "origin"`) {
			t.Fatalf("the repository was not inferred from the checkout, so nothing below tests anything:\n%s", output)
		}
		if !strings.Contains(output, repoGroup) {
			t.Errorf("the inferred listing lacks the repository's group %s:\n%s", repoGroup, output)
		}
	})

	t.Run("reviewer-group takes --project", func(t *testing.T) {
		projectGroup := testsupport.UniqueName("project-group-")
		if output, err := executeLiveCLIUnscoped(t, "--json", "reviewer-group", "create", projectGroup, "--users", user, "--project", seeded.Key); err != nil {
			t.Fatalf("reviewer-group create --project was refused inside the checkout: %v\n%s", err, output)
		}

		listing, err := executeLiveCLIUnscoped(t, "--json", "reviewer-group", "list", "--project", seeded.Key)
		if err != nil {
			t.Fatalf("reviewer-group list --project was refused inside the checkout: %v\n%s", err, listing)
		}
		groups := liveReviewerGroupsNamed(t, listing, projectGroup)
		if len(groups) != 1 || groups[0]["scope"] != "PROJECT" {
			t.Fatalf("want the project's group %q, got %v in:\n%s", projectGroup, groups, listing)
		}
		if held := liveReviewerGroupsNamed(t, listing, repoGroup); len(held) != 0 {
			t.Errorf("the project's listing holds the repository's group %q, so it listed the repository: %v", repoGroup, held)
		}

		id := trimNumeric(groups[0]["id"])
		if output, err := executeLiveCLIUnscoped(t, "--json", "reviewer-group", "delete", id, "--project", seeded.Key, "--yes"); err != nil {
			t.Fatalf("reviewer-group delete --project was refused inside the checkout: %v\n%s", err, output)
		}
		after := mustLiveCLIUnscoped(t, "reviewer-group", "list", "--project", seeded.Key)
		if left := liveReviewerGroupsNamed(t, after, projectGroup); len(left) != 0 {
			t.Errorf("the project's group %q is still there after the delete: %v", projectGroup, left)
		}
	})

	t.Run("reviewer condition takes --project", func(t *testing.T) {
		condition := fmt.Sprintf(
			`{"sourceMatcher":{"id":"ANY_REF","type":{"id":"ANY_REF"}},`+
				`"targetMatcher":{"id":"ANY_REF","type":{"id":"ANY_REF"}},`+
				`"reviewers":[{"id":%d}],"requiredApprovals":1}`, userID)

		created, err := executeLiveCLIUnscoped(t, "--json", "reviewer", "condition", "create", condition, "--project", seeded.Key)
		if err != nil {
			t.Fatalf("reviewer condition create --project failed inside the checkout: %v\n%s", err, created)
		}
		id := conditionIDFrom(t, created)

		// Read from the repository by name, which does not depend on the fix:
		// it lists the project's conditions as inherited, and its own as the
		// repository's. The create went to the repository when it says so.
		repoListing := mustLiveCLIUnscoped(t, "reviewer", "condition", "list", "--repo", repoRef)
		listed, found := maskedConditionFrom(t, repoListing, id)
		if !found {
			t.Fatalf("condition %s is not in %s's listing at all:\n%s", id, repoRef, repoListing)
		}
		if listed["scope"] != "PROJECT" {
			t.Fatalf("condition %s has scope %v, so --project wrote it to the repository", id, listed["scope"])
		}

		projectListing := mustLiveCLIUnscoped(t, "reviewer", "condition", "list", "--project", seeded.Key)
		if listed, found := maskedConditionFrom(t, projectListing, id); !found || listed["scope"] != "PROJECT" {
			t.Errorf("list --project inside the checkout does not hold the project's condition %s:\n%s", id, projectListing)
		}

		// Deleted through the repository, an inherited condition is refused,
		// and the refusal says --project deletes it. So --project has to.
		if output, err := executeLiveCLIUnscoped(t, "--json", "reviewer", "condition", "delete", id, "--project", seeded.Key, "--yes"); err != nil {
			t.Fatalf("reviewer condition delete --project failed inside the checkout: %v\n%s", err, output)
		}
		after := mustLiveCLIUnscoped(t, "reviewer", "condition", "list", "--repo", repoRef)
		if _, found := maskedConditionFrom(t, after, id); found {
			t.Errorf("condition %s is still inherited by %s after the delete:\n%s", id, repoRef, after)
		}
	})

	t.Run("repo ssh-key takes --project", func(t *testing.T) {
		label := testsupport.UniqueName("live-suite-project-key-")
		publicKey := generateSSHPublicKey(t, testsupport.UniqueName("live-suite-comment-"))

		added, err := executeLiveCLIUnscoped(t, "--json", "repo", "ssh-key", "add", publicKey,
			"--label", label, "--read-write", "--project", seeded.Key)
		if err != nil {
			t.Fatalf("repo ssh-key add --project was refused inside the checkout: %v\n%s", err, added)
		}
		addData := decodeJSONMap(t, added)
		keyObject, ok := addData["key"].(map[string]any)
		if !ok {
			keyObject = addData
		}
		keyID, ok := numericOrStringID(keyObject["id"])
		if !ok {
			t.Fatalf("expected a key id in the add output: %s", added)
		}
		removed := false
		t.Cleanup(func() {
			if !removed {
				_, _ = executeLiveCLIUnscoped(t, "--json", "repo", "ssh-key", "remove", keyID, "--project", seeded.Key, "--yes")
			}
		})

		listing, err := executeLiveCLIUnscoped(t, "--json", "repo", "ssh-key", "list", "--project", seeded.Key)
		if err != nil {
			t.Fatalf("repo ssh-key list --project was refused inside the checkout: %v\n%s", err, listing)
		}
		stored, ok := listedKeyByID(t, listing, keyID)
		if !ok {
			t.Fatalf("access key %s is not in the project's listing: %s", keyID, listing)
		}
		if stored["permission"] != "PROJECT_WRITE" {
			t.Errorf("permission = %v, want PROJECT_WRITE", stored["permission"])
		}
		if stored["label"] != label {
			t.Errorf("label = %v, want %s", stored["label"], label)
		}

		if output, err := executeLiveCLIUnscoped(t, "--json", "repo", "ssh-key", "remove", keyID, "--project", seeded.Key, "--yes"); err != nil {
			t.Fatalf("repo ssh-key remove --project was refused inside the checkout: %v\n%s", err, output)
		}
		removed = true
		if _, still := listedKeyByID(t, mustLiveCLIUnscoped(t, "repo", "ssh-key", "list", "--project", seeded.Key), keyID); still {
			t.Errorf("access key %s is still on the project after the remove", keyID)
		}
	})

	t.Run("search prs --role searches the dashboard", func(t *testing.T) {
		if err := harness.pushCommitOnBranch(seeded.Key, elsewhere.Slug, "feature/elsewhere", "elsewhere.txt"); err != nil {
			t.Fatalf("push commit on branch failed: %v", err)
		}
		prID, err := harness.createPullRequest(ctx, seeded.Key, elsewhere.Slug, "feature/elsewhere", "master")
		if err != nil {
			t.Fatalf("create pull request failed: %v", err)
		}

		output, err := executeLiveCLIUnscoped(t, "--json", "search", "prs", "--role", "author", "--limit", dashboardPageArg)
		if err != nil {
			t.Fatalf("search prs --role was refused inside the checkout: %v\n%s", err, output)
		}
		entries := collectionFromCLI(t, output, "pullRequests")
		if len(entries) >= dashboardPage {
			t.Fatalf("the answer filled the %d-row page, so a missing pull request means the page ran out", dashboardPage)
		}
		for _, entry := range entries {
			pullRequest, _ := entry.(map[string]any)
			repository, _ := pullRequest["repository"].(map[string]any)
			if repository != nil && asString(repository["projectKey"])+"/"+asString(repository["slug"]) == elsewhereRef &&
				asString(pullRequest["id"]) == prID {
				return
			}
		}
		t.Errorf("the caller's pull request %s in %s is not in the dashboard search, so it searched something else:\n%s", prID, elsewhereRef, output)
	})
}
