//go:build live

package live_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
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

	// Test non-existent project lookup
	output, err := executeLiveCLI(t, "project", "get", "NONEXISTENT_KEY_99999")
	if err == nil {
		t.Fatalf("expected error for non-existent project, got success:\n%s", output)
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
		Error struct {
			Kind     string `json:"kind"`
			Message  string `json:"message"`
			ExitCode int    `json:"exitCode"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(jsonOutput), &errorEnvelope); err == nil {
		if errorEnvelope.Error.ExitCode != 0 && errorEnvelope.Error.ExitCode != 4 {
			t.Fatalf("expected exitCode 4 in json error envelope, got: %#v", errorEnvelope)
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

	// Attempt duplicate branch creation -> Bitbucket DC returns 400 (validation/DuplicateRefException) or 409 Conflict
	dupOutput, dupErr := executeLiveCLI(t, "--json", "branch", "create", branchName, "--start-point", "refs/heads/master")
	if dupErr == nil {
		t.Fatalf("expected conflict/validation error on duplicate branch create, got success:\n%s", dupOutput)
	}
	if apperrors.ExitCode(dupErr) != 2 && apperrors.ExitCode(dupErr) != 5 {
		t.Fatalf("expected exit code 2 (validation) or 5 (conflict) on duplicate branch, got exit code %d (err: %v)", apperrors.ExitCode(dupErr), dupErr)
	}
	if !strings.Contains(strings.ToLower(dupOutput+dupErr.Error()), "already exists") && !strings.Contains(strings.ToLower(dupOutput+dupErr.Error()), "conflict") {
		t.Fatalf("expected error message to mention existing branch or conflict, got: %s", dupOutput)
	}
}

// TestLiveGovernanceCommandsMapTheirFailures covers the failure side of the
// governance commands -- reviewer conditions, project and repository
// permissions, pull request settings -- against a real refusal.
//
// The unit tests these replace stood up a server that answered one status to
// every request and asserted only that an error came back. That holds whatever
// the command did: it cannot tell a 404 for the resource from a 404 for a route
// bb got wrong, and it never looked at the exit code, which is the part an
// agent branches on. Here the resources are genuinely absent and the codes are
// checked.
func TestLiveGovernanceCommandsMapTheirFailures(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	const missingProject = "NOSUCHPROJECT99999"

	cases := []struct {
		name     string
		args     []string
		wantExit int
	}{
		{
			name:     "reviewer condition list on a project that is not there",
			args:     []string{"reviewer", "condition", "list", "--project", missingProject},
			wantExit: 4,
		},
		{
			name:     "reviewer condition delete of an id that is not there",
			args:     []string{"reviewer", "condition", "delete", "999999", "--project", seeded.Key},
			wantExit: 4,
		},
		{
			name:     "project permissions list on a project that is not there",
			args:     []string{"project", "permissions", "list", missingProject},
			wantExit: 4,
		},
		{
			name:     "project permissions revoke on a project that is not there",
			args:     []string{"project", "permissions", "users", "revoke", missingProject, "nobody"},
			wantExit: 4,
		},
		{
			name:     "repository permissions grant to a user that is not there",
			args:     []string{"repo", "settings", "security", "permissions", "users", "grant", "nosuchuser99999", "REPO_READ"},
			wantExit: 4,
		},
		{
			name:     "pull request settings on a repository that is not there",
			args:     []string{"repo", "settings", "pull-requests", "get", "--repo", seeded.Key + "/no-such-repository"},
			wantExit: 4,
		},
		{
			name:     "declining a pull request that is not there",
			args:     []string{"pr", "decline", "999999"},
			wantExit: 4,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			output, err := executeLiveCLI(t, append([]string{"--json"}, testCase.args...)...)
			if err == nil {
				t.Fatalf("expected a failure, got:\n%s", output)
			}
			if code := apperrors.ExitCode(err); code != testCase.wantExit {
				t.Fatalf("exit code = %d, want %d (%v)", code, testCase.wantExit, err)
			}
			// A transient kind here would tell an agent to retry something that
			// can never succeed, which is the failure this taxonomy exists to
			// prevent.
			if strings.Contains(err.Error(), "transient") {
				t.Errorf("a permanent failure was reported as transient: %v", err)
			}
		})
	}
}
