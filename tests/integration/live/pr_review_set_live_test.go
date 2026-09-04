//go:build live

package live_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// TestLivePullRequestReviewSetCommand covers `bb pr review set`, the CLI
// verb for the one field a participant holds.
//
// It lives beside the MCP review test because it needs the same setup and the
// same discovery: Bitbucket refuses the author's own review, so a second
// licensed account is not a convenience here (OPENAPI-027).
//
// Each transition is read back rather than trusted from the write's own
// answer, and the pair that matters is NEEDS_WORK following APPROVED: the
// status replaces what was held rather than joining it.
func TestLivePullRequestReviewSetCommand(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	branch := fmt.Sprintf("lt-review-status-%d", time.Now().UnixNano()%100000)
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "review-status.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}
	prID := createLivePRForRegression(t, branch, "Review status", "--no-default-reviewers", "--no-codeowners")

	reviewer, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create reviewer failed: %v", err)
	}
	if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, reviewer.Username, "REPO_WRITE"); err != nil {
		t.Fatalf("grant the reviewer write access failed: %v", err)
	}
	if _, err := harness.liveJSON(ctx, http.MethodPost,
		fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/pull-requests/%s/participants",
			seeded.Key, repo.Slug, prID),
		map[string]any{"user": map[string]any{"name": reviewer.Username}, "role": "REVIEWER"}); err != nil {
		t.Fatalf("add the reviewer failed: %v", err)
	}

	configureLiveCLIEnvForUser(t, harness, seeded.Key, repo.Slug, reviewer)

	held := func(t *testing.T) string {
		t.Helper()

		data := extractPRData(decodeJSONMap(t, mustLiveCLI(t, "pr", "get", prID, "--repo", repoRef)))
		reviewers, _ := data["reviewers"].([]any)
		for _, value := range reviewers {
			entry, _ := value.(map[string]any)
			if name, _ := entry["name"].(string); name == reviewer.Username {
				status, _ := entry["status"].(string)

				return status
			}
		}

		return ""
	}

	t.Run("a dry run predicts the change without making it", func(t *testing.T) {
		before := held(t)

		output := mustLiveCLI(t, "--dry-run", "pr", "review", "set", prID, "NEEDS_WORK", "--repo", repoRef)
		assertLivePreview(t, output, "update")

		if after := held(t); after != before {
			t.Fatalf("the dry run changed the status from %q to %q", before, after)
		}
	})

	t.Run("APPROVED", func(t *testing.T) {
		mustLiveCLI(t, "pr", "review", "set", prID, "APPROVED", "--repo", repoRef)

		if status := held(t); status != "APPROVED" {
			t.Fatalf("status = %q, want APPROVED", status)
		}
	})

	t.Run("NEEDS_WORK replaces the approval", func(t *testing.T) {
		mustLiveCLI(t, "pr", "review", "set", prID, "NEEDS_WORK", "--repo", repoRef)

		if status := held(t); status != "NEEDS_WORK" {
			t.Fatalf("status = %q, want NEEDS_WORK", status)
		}
	})

	t.Run("a dry run over the status already held predicts a no-op", func(t *testing.T) {
		output := mustLiveCLI(t, "--dry-run", "pr", "review", "set", prID, "NEEDS_WORK", "--repo", repoRef)
		assertLivePreview(t, output, "no-op")
	})

	t.Run("the no-op is found under a token with no configured username", func(t *testing.T) {
		// Finding the caller among the reviewers is what separates a no-op from
		// an update, and the configured username is the wrong place to look for
		// them: under a token there may not be one. The prediction then quietly
		// degrades to "update" for a status already held -- safe, and wrong.
		//
		// Bitbucket names the caller on every authenticated response, so the
		// answer costs one request and no configuration.
		tokenName := fmt.Sprintf("live-review-set-%d", time.Now().UnixNano()%100000)
		createOutput := mustLiveCLI(t, "auth", "token", "create", tokenName,
			"--user", reviewer.Username, "--permission", "REPO_WRITE", "--expiry-days", "1")

		created := decodeJSONMap(t, createOutput)
		token, _ := created["token"].(string)
		if token == "" {
			t.Fatalf("no token in the create output:\n%s", createOutput)
		}
		if tokenID, ok := numericOrStringID(created["id"]); ok {
			defer func() {
				_, _ = executeLiveCLI(t, "auth", "token", "revoke", tokenID, "--user", reviewer.Username)
			}()
		}

		t.Setenv("BITBUCKET_USERNAME", "")
		t.Setenv("BITBUCKET_PASSWORD", "")
		t.Setenv("BITBUCKET_TOKEN", token)

		output := mustLiveCLI(t, "--dry-run", "pr", "review", "set", prID, "NEEDS_WORK", "--repo", repoRef)
		assertLivePreview(t, output, "no-op")
	})

	t.Run("unapprove over NEEDS_WORK predicts an update, not a no-op", func(t *testing.T) {
		// `unapprove` clears whichever status is held, so with NEEDS_WORK held
		// it has work to do. Its preview asked whether the caller had approved
		// -- which they had not -- and called it a no-op, while the command
		// went on to clear the request for changes.
		output := mustLiveCLI(t, "--dry-run", "pr", "review", "unapprove", prID, "--repo", repoRef)
		assertLivePreview(t, output, "update")

		if status := held(t); status != "NEEDS_WORK" {
			t.Fatalf("the dry run changed the status to %q", status)
		}
	})

	t.Run("UNAPPROVED withdraws the request for changes", func(t *testing.T) {
		// The behaviour `unapprove` performs but does not describe.
		mustLiveCLI(t, "pr", "review", "set", prID, "UNAPPROVED", "--repo", repoRef)

		if status := held(t); status != "UNAPPROVED" {
			t.Fatalf("status = %q, want UNAPPROVED", status)
		}
	})
}
