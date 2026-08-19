//go:build live

package live_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLiveHybridGitWireAndRESTRoundtrip(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// 1. Clone repository via bb repo clone into temp dir
	workDir1 := filepath.Join(t.TempDir(), "client-1")
	cloneOutput, err := executeLiveCLI(t, "repo", "clone", seeded.Key+"/"+repo.Slug, workDir1)
	if err != nil {
		t.Fatalf("repo clone to client-1 failed: %v\noutput: %s", err, cloneOutput)
	}

	// 2. In client-1, create a feature branch, author commit, and git push
	_ = runGit(workDir1, "config", "user.name", "bb-live-test")
	_ = runGit(workDir1, "config", "user.email", "bb-live-test@example.local")
	if err := runGit(workDir1, "checkout", "-b", "feature/hybrid-test"); err != nil {
		t.Fatalf("git checkout -b failed: %v", err)
	}
	proofFile := filepath.Join(workDir1, "hybrid-proof.txt")
	if err := os.WriteFile(proofFile, []byte("hybrid-roundtrip-verified\n"), 0o644); err != nil {
		t.Fatalf("write proof file failed: %v", err)
	}
	if err := runGit(workDir1, "add", "hybrid-proof.txt"); err != nil {
		t.Fatalf("git add failed: %v", err)
	}
	if err := runGit(workDir1, "commit", "-m", "add hybrid proof file"); err != nil {
		t.Fatalf("git commit failed: %v", err)
	}
	if err := runGit(workDir1, "push", "-u", "origin", "feature/hybrid-test"); err != nil {
		t.Fatalf("git push feature/hybrid-test failed: %v", err)
	}

	// 3. Create PR via bb pr create (REST API)
	prCreateOutput, err := executeLiveCLI(t, "--json", "pr", "create",
		"--from-ref", "feature/hybrid-test",
		"--to-ref", "refs/heads/master",
		"--title", "Hybrid Roundtrip PR",
	)
	if err != nil {
		t.Fatalf("pr create failed: %v\noutput: %s", err, prCreateOutput)
	}

	var createEnvelope struct {
		Version string         `json:"version"`
		Data    map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(prCreateOutput), &createEnvelope); err != nil {
		t.Fatalf("decode pr create output failed: %v", err)
	}
	prData := createEnvelope.Data
	if inner, ok := createEnvelope.Data["pull_request"].(map[string]any); ok {
		prData = inner
	}
	prID := fmt.Sprintf("%v", prData["id"])

	// 4. In client-2 (separate clone), run bb pr checkout
	workDir2 := filepath.Join(t.TempDir(), "client-2")
	clone2Output, err := executeLiveCLI(t, "repo", "clone", seeded.Key+"/"+repo.Slug, workDir2)
	if err != nil {
		t.Fatalf("repo clone to client-2 failed: %v\noutput: %s", err, clone2Output)
	}

	originalDir, _ := os.Getwd()
	_ = os.Chdir(workDir2)
	defer func() { _ = os.Chdir(originalDir) }()

	checkoutOutput, err := executeLiveCLI(t, "pr", "checkout", prID)
	if err != nil {
		t.Fatalf("bb pr checkout failed in client-2: %v\noutput: %s", err, checkoutOutput)
	}

	// Verify that the file exists in client-2
	if _, err := os.Stat(filepath.Join(workDir2, "hybrid-proof.txt")); err != nil {
		t.Fatalf("expected hybrid-proof.txt to exist after bb pr checkout: %v", err)
	}

	// 5. Merge PR via REST API
	_ = os.Chdir(originalDir)
	mergeOutput, err := executeLiveCLI(t, "--json", "pr", "merge", prID)
	if err != nil {
		t.Fatalf("pr merge failed: %v\noutput: %s", err, mergeOutput)
	}

	// 6. In client-1, switch to master and git pull -> verify commit is now on master
	if err := runGit(workDir1, "checkout", "master"); err != nil {
		t.Fatalf("checkout master in client-1 failed: %v", err)
	}
	if err := runGit(workDir1, "pull", "origin", "master"); err != nil {
		t.Fatalf("git pull master in client-1 failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(workDir1, "hybrid-proof.txt")); err != nil {
		t.Fatalf("expected hybrid-proof.txt on master in client-1 after merge & pull: %v", err)
	}
}
