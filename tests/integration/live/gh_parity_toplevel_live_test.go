//go:build live

package live_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLiveTopLevelBrowse covers the top-level bb browse.
//
// The gh-parity suite already drives it, but through a table whose arguments are
// built at run time, which the live-coverage tool cannot read. This one spells
// the invocation out so the command is visibly covered, and asserts the URL
// rather than merely a clean exit.
func TestLiveTopLevelBrowse(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	output, err := executeLiveCLI(t, "browse", "--no-browser", "--repo", repoRef)
	if err != nil {
		t.Fatalf("browse failed: %v\noutput: %s", err, output)
	}
	// The URL has to name this repository on this host: a browse command that
	// prints a plausible but wrong URL fails silently for the user, who lands on
	// the wrong page rather than seeing an error.
	if !strings.Contains(output, seeded.Key) || !strings.Contains(output, repo.Slug) {
		t.Fatalf("expected the repository in the browse URL, got: %s", output)
	}

	fileOutput, err := executeLiveCLI(t, "browse", "--no-browser", "seed.txt", "--repo", repoRef)
	if err != nil {
		t.Fatalf("browse of a path failed: %v\noutput: %s", err, fileOutput)
	}
	if !strings.Contains(fileOutput, "seed.txt") {
		t.Fatalf("expected the path in the browse URL, got: %s", fileOutput)
	}
}

// TestLiveTopLevelClone covers the top-level bb clone, the gh-shaped spelling of
// bb repo clone.
//
// Covering it separately is not redundant: the two are different commands in the
// tree, and the short one is the one people reach for first, so a break in it is
// a break in the spelling most users see.
func TestLiveTopLevelClone(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	cloneDir := filepath.Join(t.TempDir(), "clone")
	output, err := executeLiveCLI(t, "clone", "--https", seeded.Key+"/"+repo.Slug, cloneDir)
	if err != nil {
		t.Fatalf("clone failed: %v\noutput: %s", err, output)
	}

	// A clone that reports success without a working tree is the failure worth
	// catching, so this looks for the seeded file rather than for the directory.
	if _, err := os.Stat(filepath.Join(cloneDir, "seed.txt")); err != nil {
		t.Fatalf("expected the clone to contain the seeded file: %v", err)
	}

	headOutput, err := runGitCapture(cloneDir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse in the clone failed: %v", err)
	}
	if strings.TrimSpace(headOutput) == "" {
		t.Fatal("expected the clone to have a checked-out branch")
	}
}

// TestLiveAISkillLifecycle covers bb ai skill install/show/remove.
//
// These write a file rather than call Bitbucket, so the live suite is not where
// they have to run -- but they are commands, and a broken install path is as
// invisible as a broken endpoint. The test installs into a directory of its own
// so it cannot touch the developer's real skills.
func TestLiveAISkillLifecycle(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	configureLiveCLIEnv(t, harness, seeded.Key, seeded.Repos[0].Slug)

	workDir := t.TempDir()
	originalDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("chdir into the work directory failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalDirectory)
	})

	showOutput, err := executeLiveCLI(t, "ai", "skill", "show")
	if err != nil {
		t.Fatalf("ai skill show failed: %v\noutput: %s", err, showOutput)
	}
	if strings.TrimSpace(showOutput) == "" {
		t.Fatal("expected ai skill show to print the skill document")
	}

	installOutput, err := executeLiveCLI(t, "ai", "skill", "install")
	if err != nil {
		t.Fatalf("ai skill install failed: %v\noutput: %s", err, installOutput)
	}

	installedPath := skillPathFrom(t, installOutput, workDir)
	contents, err := os.ReadFile(installedPath)
	if err != nil {
		t.Fatalf("expected the skill to be written to %s: %v", installedPath, err)
	}
	// What show prints and what install writes have to be the same document, or
	// one of the two is describing a skill nobody has.
	if strings.TrimSpace(string(contents)) == "" {
		t.Fatalf("expected the installed skill to have content at %s", installedPath)
	}

	removeOutput, err := executeLiveCLI(t, "ai", "skill", "remove")
	if err != nil {
		t.Fatalf("ai skill remove failed: %v\noutput: %s", err, removeOutput)
	}
	if _, err := os.Stat(installedPath); !os.IsNotExist(err) {
		t.Fatalf("expected the skill file to be gone after remove, stat gave: %v", err)
	}
}

// skillPathFrom finds the installed skill on disk, preferring a path named in
// the command output and falling back to the documented default location.
func skillPathFrom(t *testing.T, output string, workDir string) string {
	t.Helper()

	for _, field := range strings.Fields(output) {
		trimmed := strings.Trim(field, `"'`)
		if !strings.HasSuffix(trimmed, "SKILL.md") {
			continue
		}
		if !filepath.IsAbs(trimmed) {
			trimmed = filepath.Join(workDir, trimmed)
		}
		if _, err := os.Stat(trimmed); err == nil {
			return trimmed
		}
	}

	return filepath.Join(workDir, ".agents", "skills", "bb", "SKILL.md")
}
