//go:build live

package live_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
)

func TestLiveErrorTaxonomy404NotFound(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// Test non-existent repo in existing project
	output, err := executeLiveCLI(t, "repo", "get", seeded.Key+"/non-existent-repo-999")
	if err == nil {
		t.Fatalf("expected error for non-existent repo, got success:\n%s", output)
	}
	if apperrors.ExitCode(err) != 4 {
		t.Fatalf("expected exit code 4 (not found), got exit code %d (err: %v)", apperrors.ExitCode(err), err)
	}

	// Test non-existent PR on existing repo in JSON mode
	jsonOutput, jsonErr := executeLiveCLI(t, "--json", "pr", "get", "999999")
	if jsonErr == nil {
		t.Fatalf("expected error for non-existent PR, got success:\n%s", jsonOutput)
	}
	if apperrors.ExitCode(jsonErr) != 4 {
		t.Fatalf("expected exit code 4 (not found) for PR get, got %d (err: %v)", apperrors.ExitCode(jsonErr), jsonErr)
	}
	var errorEnvelope struct {
		Version string `json:"version"`
		Error   struct {
			Kind     string `json:"kind"`
			Message  string `json:"message"`
			ExitCode int    `json:"exit_code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(jsonOutput), &errorEnvelope); err == nil {
		if errorEnvelope.Error.ExitCode != 0 && errorEnvelope.Error.ExitCode != 4 {
			t.Fatalf("expected exit_code 4 in json error envelope, got: %#v", errorEnvelope)
		}
	}
}

func TestLiveErrorTaxonomy409Conflict(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	branchName := "feature/dup-test"

	// Create branch first time -> must succeed
	createOutput, createErr := executeLiveCLI(t, "--json", "branch", "create", branchName, "--start-point", "refs/heads/master")
	if createErr != nil {
		t.Fatalf("initial branch create failed: %v\noutput: %s", createErr, createOutput)
	}

	// Attempt duplicate branch creation -> Bitbucket DC returns 409 Conflict
	dupOutput, dupErr := executeLiveCLI(t, "--json", "branch", "create", branchName, "--start-point", "refs/heads/master")
	if dupErr == nil {
		t.Fatalf("expected 409 conflict error on duplicate branch create, got success:\n%s", dupOutput)
	}
	if apperrors.ExitCode(dupErr) != 5 {
		t.Fatalf("expected exit code 5 (conflict) on duplicate branch, got exit code %d (err: %v)", apperrors.ExitCode(dupErr), dupErr)
	}
	if !strings.Contains(strings.ToLower(dupOutput+dupErr.Error()), "already exists") && !strings.Contains(strings.ToLower(dupOutput+dupErr.Error()), "conflict") {
		t.Fatalf("expected error message to mention existing branch or conflict, got: %s", dupOutput)
	}
}
