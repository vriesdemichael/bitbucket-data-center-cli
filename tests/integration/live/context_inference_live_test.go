//go:build live

package live_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLiveCLIInferRepoContextFromGitRemote(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 2)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, "WRONG", "wrong")

	pushURL, err := repositoryPushURL(harness.config, seeded.Key, repo.Slug)
	if err != nil {
		t.Fatalf("build push url: %v", err)
	}

	workingDirectory := t.TempDir()
	if err := runGit(workingDirectory, "init"); err != nil {
		t.Fatalf("git init failed: %v", err)
	}
	if err := runGit(workingDirectory, "remote", "add", "origin", pushURL); err != nil {
		t.Fatalf("git remote add failed: %v", err)
	}

	originalDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	if err := os.Chdir(workingDirectory); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalDirectory)
	})

	output, err := executeLiveCLI(t, "branch", "list", "--limit", "5")
	if err != nil {
		t.Fatalf("branch list with inferred context failed: %v\noutput: %s", err, output)
	}

	if !strings.Contains(output, "Using repository context from git remote \"origin\"") {
		t.Fatalf("expected inferred context notice, got: %s", output)
	}
	if !strings.Contains(strings.ToLower(output), "master") && !strings.Contains(strings.ToLower(output), "main") {
		t.Fatalf("expected branch output, got: %s", output)
	}
}

func TestLiveCLIInferRepoContextAmbiguity(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 2, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	configureLiveCLIEnv(t, harness, "WRONG", "wrong")

	originURL, err := repositoryPushURL(harness.config, seeded.Key, seeded.Repos[0].Slug)
	if err != nil {
		t.Fatalf("build origin url: %v", err)
	}
	upstreamURL, err := repositoryPushURL(harness.config, seeded.Key, seeded.Repos[1].Slug)
	if err != nil {
		t.Fatalf("build upstream url: %v", err)
	}

	workingDirectory := t.TempDir()
	if err := runGit(workingDirectory, "init"); err != nil {
		t.Fatalf("git init failed: %v", err)
	}
	if err := runGit(workingDirectory, "remote", "add", "origin", originURL); err != nil {
		t.Fatalf("git remote add origin failed: %v", err)
	}
	if err := runGit(workingDirectory, "remote", "add", "upstream", upstreamURL); err != nil {
		t.Fatalf("git remote add upstream failed: %v", err)
	}

	originalDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	if err := os.Chdir(workingDirectory); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalDirectory)
	})

	output, err := executeLiveCLI(t, "branch", "list", "--limit", "5")
	if err == nil {
		t.Fatalf("expected ambiguity error, got success output: %s", output)
	}
	if !strings.Contains(err.Error(), "ambiguous git remote context") {
		t.Fatalf("expected ambiguity guidance, got error=%v output=%s", err, output)
	}
}

func TestLiveCLIInferRepoContextJSONHasNoBannerNoise(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 2)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, "WRONG", "wrong")

	pushURL, err := repositoryPushURL(harness.config, seeded.Key, repo.Slug)
	if err != nil {
		t.Fatalf("build push url: %v", err)
	}

	workingDirectory := t.TempDir()
	if err := runGit(workingDirectory, "init"); err != nil {
		t.Fatalf("git init failed: %v", err)
	}
	if err := runGit(workingDirectory, "remote", "add", "origin", pushURL); err != nil {
		t.Fatalf("git remote add failed: %v", err)
	}

	originalDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	if err := os.Chdir(workingDirectory); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalDirectory)
	})

	output, err := executeLiveCLI(t, "--json", "branch", "list", "--limit", "5")
	if err != nil {
		t.Fatalf("branch list with inferred context and json failed: %v\noutput: %s", err, output)
	}

	trimmed := strings.TrimSpace(output)
	if strings.HasPrefix(trimmed, "Using repository context") {
		t.Fatalf("expected pure JSON output without banner noise, got: %s", output)
	}
	if !strings.Contains(trimmed, "\"branches\"") {
		t.Fatalf("expected branch JSON payload, got: %s", output)
	}
}

func TestLiveCLIExplicitRepoOverridesAmbiguousInference(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 2, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	configureLiveCLIEnv(t, harness, "WRONG", "wrong")

	originURL, err := repositoryPushURL(harness.config, seeded.Key, seeded.Repos[0].Slug)
	if err != nil {
		t.Fatalf("build origin url: %v", err)
	}
	upstreamURL, err := repositoryPushURL(harness.config, seeded.Key, seeded.Repos[1].Slug)
	if err != nil {
		t.Fatalf("build upstream url: %v", err)
	}

	workingDirectory := t.TempDir()
	if err := runGit(workingDirectory, "init"); err != nil {
		t.Fatalf("git init failed: %v", err)
	}
	if err := runGit(workingDirectory, "remote", "add", "origin", originURL); err != nil {
		t.Fatalf("git remote add origin failed: %v", err)
	}
	if err := runGit(workingDirectory, "remote", "add", "upstream", upstreamURL); err != nil {
		t.Fatalf("git remote add upstream failed: %v", err)
	}

	originalDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	if err := os.Chdir(workingDirectory); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalDirectory)
	})

	target := seeded.Key + "/" + seeded.Repos[0].Slug
	output, err := executeLiveCLI(t, "branch", "list", "--limit", "5", "--repo", target)
	if err != nil {
		t.Fatalf("expected explicit --repo to bypass ambiguity, got err=%v output=%s", err, output)
	}

	if strings.Contains(output, "ambiguous git remote context") {
		t.Fatalf("did not expect ambiguity message when --repo is explicit, got: %s", output)
	}
}

func TestLiveCLINonGitDirectoryPreservesRepositoryRequiredError(t *testing.T) {
	harness := newLiveHarness(t)
	configureLiveCLIEnv(t, harness, "", "")

	workingDirectory := t.TempDir()
	originalDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	if err := os.Chdir(workingDirectory); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalDirectory)
	})

	output, err := executeLiveCLI(t, "branch", "list", "--limit", "5")
	if err == nil {
		t.Fatalf("expected repository required error in non-git directory, got output: %s", output)
	}
	if !strings.Contains(err.Error(), "repository is required") {
		t.Fatalf("expected repository required guidance, got err=%v output=%s", err, output)
	}
}

func TestLiveCLIAuthServerContextSwitchingFlow(t *testing.T) {
	harness := newLiveHarness(t)

	configPath := t.TempDir() + "/config.yaml"
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")
	t.Setenv("BITBUCKET_URL", harness.config.BitbucketURL)
	t.Setenv("BITBUCKET_USERNAME", harness.config.BitbucketUsername)
	t.Setenv("BITBUCKET_PASSWORD", harness.config.BitbucketPassword)
	t.Setenv("BITBUCKET_TOKEN", harness.config.BitbucketToken)

	primaryHost := harness.config.BitbucketURL
	secondaryHost := "http://secondary.invalid:7990"

	if output, err := executeLiveCLIWithStdin(t, "primary-token", "auth", "login", primaryHost, "--token-stdin", "--set-default=true"); err != nil {
		t.Fatalf("auth login primary failed: %v\noutput: %s", err, output)
	}
	if output, err := executeLiveCLIWithStdin(t, "secondary-token", "auth", "login", secondaryHost, "--token-stdin", "--set-default=false"); err != nil {
		t.Fatalf("auth login secondary failed: %v\noutput: %s", err, output)
	}

	listOutput, err := executeLiveCLI(t, "auth", "server", "list")
	if err != nil {
		t.Fatalf("auth server list failed: %v\noutput: %s", err, listOutput)
	}
	if !strings.Contains(listOutput, primaryHost) || !strings.Contains(listOutput, secondaryHost) {
		t.Fatalf("expected both hosts in server list output, got: %s", listOutput)
	}

	useOutput, err := executeLiveCLI(t, "auth", "server", "use", "--host", secondaryHost)
	if err != nil {
		t.Fatalf("auth server use failed: %v\noutput: %s", err, useOutput)
	}
	if !strings.Contains(useOutput, "Active server set to "+secondaryHost) {
		t.Fatalf("expected active server confirmation, got: %s", useOutput)
	}
	t.Setenv("BITBUCKET_URL", "")

	statusOutput, err := executeLiveCLI(t, "auth", "status")
	if err != nil {
		t.Fatalf("auth status after server switch failed: %v\noutput: %s", err, statusOutput)
	}
	if !strings.Contains(statusOutput, secondaryHost) {
		t.Fatalf("expected auth status to reflect switched host, got: %s", statusOutput)
	}
}
