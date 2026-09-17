//go:build live

package live_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
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
	prID := createLifecyclePR(t, branch, "Diffed", "--no-default-reviewers", "--no-codeowners")

	t.Run("--name-only lists files and no patch", func(t *testing.T) {
		output := mustLiveCLI(t, "diff", "refs", "master", branch, "--name-only")

		// Exactly the file the branch adds, and nothing besides.
		if names := diffOutputNames(t, output); !slices.Equal(names, []string{"diffed.txt"}) {
			t.Fatalf("diff refs master %s --name-only named %v, want [diffed.txt]:\n%s", branch, names, output)
		}
		// The point of name-only is that the patch is not there.
		if strings.Contains(output, "@@") || strings.Contains(output, "diff --git") {
			t.Errorf("--name-only emitted patch content:\n%s", output)
		}
	})

	t.Run("a pull request diff names its files", func(t *testing.T) {
		output := mustLiveCLI(t, "diff", "pr", prID, "--name-only")
		if names := diffOutputNames(t, output); !slices.Equal(names, []string{"diffed.txt"}) {
			t.Fatalf("diff pr %s --name-only named %v, want [diffed.txt]:\n%s", prID, names, output)
		}
	})

	t.Run("a commit diff carries a patch", func(t *testing.T) {
		commits, err := harness.listCommitIDs(ctx, seeded.Key, repo.Slug, 1)
		if err != nil || len(commits) == 0 {
			t.Fatalf("could not read a commit: %v", err)
		}

		// diff commit takes no output flags: the raw patch is what it produces.
		output := mustLiveCLI(t, "diff", "commit", commits[0])
		patch := asString(decodeJSONMap(t, output)["patch"])
		if !strings.Contains(patch, "diff --git") && !strings.Contains(patch, "@@") {
			t.Fatalf("expected patch content:\n%s", output)
		}
		// The newest seeded commit adds commit-2 to seed.txt, and a patch of any
		// other commit changes something else.
		if files, added := commandCoverageDiffFiles(patch), commandCoverageAddedLines(patch); !slices.Equal(files, []string{"seed.txt"}) || !slices.Equal(added, []string{"commit-2"}) {
			t.Errorf("diff commit %s changes %v adding %q, want seed.txt adding [commit-2]:\n%s", commits[0], files, added, patch)
		}
	})
}

// diffOutputNames reads the paths a name-only diff reports.
func diffOutputNames(t *testing.T, output string) []string {
	t.Helper()

	entries, ok := decodeJSONMap(t, output)["names"].([]any)
	if !ok {
		t.Fatalf("no names in the name-only output:\n%s", output)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, asString(entry))
	}

	return names
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
		const text = "output shape"
		output := mustLiveCLI(t, "repo", "comment", "create", "--commit", commit, "--text", text)

		data := decodeJSONMap(t, output)
		if nested, ok := data["comment"].(map[string]any); ok {
			data = nested
		}
		id, ok := numericOrStringID(data["id"])
		if !ok {
			t.Fatalf("no id in the created comment:\n%s", output)
		}
		// Version is what a caller has to pass back to update or delete, so it
		// is part of the contract rather than incidental.
		version, ok := numericOrStringID(data["version"])
		if !ok {
			t.Errorf("no version in the created comment:\n%s", output)
		}

		stored := commitCommentStored(t, seeded.Key, repo.Slug, commit, id)
		if storedVersion, _ := numericOrStringID(stored["version"]); stored["text"] != text || storedVersion != version {
			t.Errorf("comment %s is stored with text %v at version %v; the create sent %q and reported version %s",
				id, stored["text"], stored["version"], text, version)
		}
	})

	t.Run("a delete resolves the version and says which it used", func(t *testing.T) {
		created := mustLiveCLI(t, "repo", "comment", "create", "--commit", commit, "--text", "to delete")
		id := commentIDFrom(t, created)
		storedVersion, _ := numericOrStringID(commitCommentStored(t, seeded.Key, repo.Slug, commit, id)["version"])

		// No --version: the command has to look it up, and the output names the
		// one it used so the caller can see what it acted on.
		output := mustLiveCLI(t, "repo", "comment", "delete", "--commit", commit, "--id", id, "--yes")
		if !strings.Contains(output, "version") {
			t.Errorf("expected the delete to report the version it used:\n%s", output)
		}
		if reported, _ := numericOrStringID(decodeJSONMap(t, output)["version"]); reported != storedVersion {
			t.Errorf("the delete reports version %q, and comment %s was stored at version %s:\n%s", reported, id, storedVersion, output)
		}
		assertCommitCommentGone(t, seeded.Key, repo.Slug, commit, id)
	})
}

// commitCommentPath is where the REST API keeps one comment on a commit.
func commitCommentPath(projectKey, slug, commit, id string) string {
	return "/rest/api/latest/projects/" + projectKey + "/repos/" + slug + "/commits/" + commit + "/comments/" + id
}

// commitCommentStored reads a commit comment back by id through bb api.
//
// Not from a listing: Bitbucket lists a commit's comments one file at a time,
// so a comment on no file is in none of them. And not through the comment
// commands, whose requests are what is being checked.
func commitCommentStored(t *testing.T, projectKey, slug, commit, id string) map[string]any {
	t.Helper()

	return decodeJSONMap(t, mustLiveCLI(t, "api", commitCommentPath(projectKey, slug, commit, id)))
}

// assertCommitCommentGone checks a deleted commit comment cannot be read by id,
// for the reason that it is not there: any failure of the read would otherwise
// pass for a delete.
func assertCommitCommentGone(t *testing.T, projectKey, slug, commit, id string) {
	t.Helper()

	output, err := executeLiveCLI(t, "--json", "api", commitCommentPath(projectKey, slug, commit, id))
	if code := apperrors.ExitCode(err); code != 4 {
		t.Fatalf("comment %s on commit %s after its delete: exit %d, want 4 (not_found): %v\n%s", id, commit, code, err, output)
	}
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
		// Every value the set sent, on the status stored under its key.
		commandCoverageAssertFields(t, "the build status", commandCoverageEntry(t, output, "key", "cli-output"),
			map[string]any{"state": "SUCCESSFUL", "url": "http://example.invalid/build", "name": "CLI output"})
	})

	t.Run("an insights report is readable after it is set", func(t *testing.T) {
		const key = "cli.insights"
		body := fmt.Sprintf(`{"title":%q,"details":"set through the CLI","result":"PASS"}`, "CLI insights")

		mustLiveCLI(t, "insights", "report", "set", commit, key, "--body", body)

		output := mustLiveCLI(t, "insights", "report", "list", commit)
		if !strings.Contains(output, "CLI insights") {
			t.Fatalf("expected the report title in the listing:\n%s", output)
		}
		// Every value the body sent, on the report stored under its key.
		commandCoverageAssertFields(t, "the insights report", decodeJSONMap(t, mustLiveCLI(t, "insights", "report", "get", commit, key)),
			map[string]any{"key": key, "title": "CLI insights", "details": "set through the CLI", "result": "PASS"})
	})
}
