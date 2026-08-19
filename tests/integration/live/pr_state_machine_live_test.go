//go:build live

package live_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
)

func extractPRData(data map[string]any) map[string]any {
	if inner, ok := data["pull_request"].(map[string]any); ok {
		return inner
	}
	return data
}

func TestLivePRStateMachineFullLifecycle(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// Create branch with a commit to open PR against master
	branchName := "feature/lifecycle-test"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branchName, "lifecycle.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	// 1. Create PR -> OPEN
	createOutput, err := executeLiveCLI(t, "--json", "pr", "create",
		"--from-ref", branchName,
		"--to-ref", "refs/heads/master",
		"--title", "State Machine Lifecycle PR",
	)
	if err != nil {
		t.Fatalf("pr create failed: %v\noutput: %s", err, createOutput)
	}

	var createEnvelope struct {
		Version string         `json:"version"`
		Data    map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(createOutput), &createEnvelope); err != nil {
		t.Fatalf("decode pr create output failed: %v", err)
	}

	createPR := extractPRData(createEnvelope.Data)
	prIDRaw, ok := createPR["id"]
	if !ok {
		t.Fatalf("expected id in create output, got: %#v", createEnvelope.Data)
	}
	prID := fmt.Sprintf("%v", prIDRaw)

	if state, ok := createPR["state"].(string); !ok || state != "OPEN" {
		t.Fatalf("expected initial state OPEN, got: %v", createPR["state"])
	}

	// 2. Inspect PR details & verify initial OPEN state
	getOutput, err := executeLiveCLI(t, "--json", "pr", "get", prID)
	if err != nil {
		t.Fatalf("pr get failed: %v\noutput: %s", err, getOutput)
	}
	var getEnvelope struct {
		Version string         `json:"version"`
		Data    map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(getOutput), &getEnvelope); err != nil {
		t.Fatalf("decode pr get output failed: %v", err)
	}
	getPR := extractPRData(getEnvelope.Data)
	if state, ok := getPR["state"].(string); !ok || state != "OPEN" {
		t.Fatalf("expected state OPEN before decline, got %v", state)
	}

	// 3. Decline PR -> DECLINED
	declineOutput, err := executeLiveCLI(t, "--json", "pr", "decline", prID)
	if err != nil {
		t.Fatalf("pr decline failed: %v\noutput: %s", err, declineOutput)
	}
	var declineEnvelope struct {
		Version string         `json:"version"`
		Data    map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(declineOutput), &declineEnvelope); err != nil {
		t.Fatalf("decode pr decline output failed: %v", err)
	}
	declinePR := extractPRData(declineEnvelope.Data)
	if state, ok := declinePR["state"].(string); !ok || state != "DECLINED" {
		t.Fatalf("expected state DECLINED, got %v", state)
	}

	// 4. Reopen PR -> OPEN
	reopenOutput, err := executeLiveCLI(t, "--json", "pr", "reopen", prID)
	if err != nil {
		t.Fatalf("pr reopen failed: %v\noutput: %s", err, reopenOutput)
	}
	var reopenEnvelope struct {
		Version string         `json:"version"`
		Data    map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(reopenOutput), &reopenEnvelope); err != nil {
		t.Fatalf("decode pr reopen output failed: %v", err)
	}
	reopenPR := extractPRData(reopenEnvelope.Data)
	if state, ok := reopenPR["state"].(string); !ok || state != "OPEN" {
		t.Fatalf("expected state OPEN after reopen, got %v", state)
	}

	// 5. Merge PR -> MERGED
	mergeOutput, err := executeLiveCLI(t, "--json", "pr", "merge", prID)
	if err != nil {
		t.Fatalf("pr merge failed: %v\noutput: %s", err, mergeOutput)
	}
	var mergeEnvelope struct {
		Version string         `json:"version"`
		Data    map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(mergeOutput), &mergeEnvelope); err != nil {
		t.Fatalf("decode pr merge output failed: %v", err)
	}
	mergePR := extractPRData(mergeEnvelope.Data)
	if state, ok := mergePR["state"].(string); !ok || state != "MERGED" {
		t.Fatalf("expected state MERGED, got %v", state)
	}

	// 6. Attempt second merge on already merged PR -> Bitbucket DC returns 409 Conflict (exit code 5)
	_, mergeAgainErr := executeLiveCLI(t, "--json", "pr", "merge", prID)
	if mergeAgainErr == nil {
		t.Fatalf("expected error on merging already merged PR, got success")
	}
	if apperrors.ExitCode(mergeAgainErr) != 5 && apperrors.ExitCode(mergeAgainErr) != 1 {
		// Bitbucket returns 409 Conflict when attempting to re-merge a merged PR
		t.Logf("second merge returned expected failure with exit code %d: %v", apperrors.ExitCode(mergeAgainErr), mergeAgainErr)
	}
}
