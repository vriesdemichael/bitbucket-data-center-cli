//go:build live

package live_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// The CLI output of diffs, comments, build statuses and code insights.
//
// These commands all had live coverage of the operation and none of what a
// caller reads back. The mocks that covered the output built the payload they
// then formatted, so the assertion was about the formatter and the fixture
// agreeing, never about the shape Bitbucket actually sends.
func TestLiveDiffCLIOutput(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{Commits: 2, WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	const branch = "feature/diffed"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "diffed.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}
	prID := createLivePRForRegression(t, branch, "Diffed", "--no-default-reviewers", "--no-codeowners")

	t.Run("--name-only lists files and no patch", func(t *testing.T) {
		output := mustLiveCLI(t, "diff", "refs", "master", branch, "--name-only")

		if !strings.Contains(output, "diffed.txt") {
			t.Fatalf("expected the changed file to be named:\n%s", output)
		}
		// The point of name-only is that the patch is not there.
		if strings.Contains(output, "@@") || strings.Contains(output, "diff --git") {
			t.Errorf("--name-only emitted patch content:\n%s", output)
		}
	})

	t.Run("a pull request diff names its files", func(t *testing.T) {
		output := mustLiveCLI(t, "diff", "pr", prID, "--name-only")
		if !strings.Contains(output, "diffed.txt") {
			t.Fatalf("expected the pull request diff to name the file:\n%s", output)
		}
	})

	t.Run("a commit diff carries a patch", func(t *testing.T) {
		commits, err := harness.listCommitIDs(ctx, seeded.Key, repo.Slug, 1)
		if err != nil || len(commits) == 0 {
			t.Fatalf("could not read a commit: %v", err)
		}

		// diff commit takes no output flags: the raw patch is what it produces.
		output := mustLiveCLI(t, "diff", "commit", commits[0])
		if !strings.Contains(output, "diff --git") && !strings.Contains(output, "@@") {
			t.Fatalf("expected patch content:\n%s", output)
		}
	})
}

// TestLiveCommentCLIOutput covers what the comment commands print, including
// the two shapes of a delete: one that resolved a version and one that had it.
func TestLiveCommentCLIOutput(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	commits, err := harness.listCommitIDs(ctx, seeded.Key, repo.Slug, 1)
	if err != nil || len(commits) == 0 {
		t.Fatalf("could not read a commit: %v", err)
	}
	commit := commits[0]

	t.Run("a created comment comes back with its id and version", func(t *testing.T) {
		output := mustLiveCLI(t, "repo", "comment", "create", "--commit", commit, "--text", "output shape")

		data := decodeJSONMap(t, output)
		if nested, ok := data["comment"].(map[string]any); ok {
			data = nested
		}
		if _, ok := data["id"]; !ok {
			t.Errorf("no id in the created comment:\n%s", output)
		}
		// Version is what a caller has to pass back to update or delete, so it
		// is part of the contract rather than incidental.
		if _, ok := data["version"]; !ok {
			t.Errorf("no version in the created comment:\n%s", output)
		}
	})

	t.Run("a delete resolves the version and says which it used", func(t *testing.T) {
		created := mustLiveCLI(t, "repo", "comment", "create", "--commit", commit, "--text", "to delete")
		id := commentIDFrom(t, created)

		// No --version: the command has to look it up, and the human output
		// names the one it used so the caller can see what it acted on.
		output := mustLiveCLI(t, "repo", "comment", "delete", "--commit", commit, "--id", id)
		if !strings.Contains(output, "version") {
			t.Errorf("expected the delete to report the version it used:\n%s", output)
		}
	})
}

// TestLiveQualityCLIOutput covers the build status and code insights commands
// through the CLI rather than the service.
func TestLiveQualityCLIOutput(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	commits, err := harness.listCommitIDs(ctx, seeded.Key, repo.Slug, 1)
	if err != nil || len(commits) == 0 {
		t.Fatalf("could not read a commit: %v", err)
	}
	commit := commits[0]

	t.Run("a build status is readable after it is set", func(t *testing.T) {
		mustLiveCLI(t, "build", "status", "set", commit,
			"--key", "cli-output", "--state", "SUCCESSFUL",
			"--url", "http://example.invalid/build", "--name", "CLI output")

		output := mustLiveCLI(t, "build", "status", "get", commit)
		for _, want := range []string{"cli-output", "SUCCESSFUL"} {
			if !strings.Contains(output, want) {
				t.Errorf("expected %q in the build status output:\n%s", want, output)
			}
		}
	})

	t.Run("an insights report is readable after it is set", func(t *testing.T) {
		const key = "cli.insights"
		body := fmt.Sprintf(`{"title":%q,"details":"set through the CLI","result":"PASS"}`, "CLI insights")

		mustLiveCLI(t, "insights", "report", "set", commit, key, "--body", body)

		output := mustLiveCLI(t, "insights", "report", "list", commit)
		if !strings.Contains(output, "CLI insights") {
			t.Fatalf("expected the report title in the listing:\n%s", output)
		}
	})
}
