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

	// A second create is a conflict, and exit 5 is the only right answer.
	//
	// This used to accept 2 or 5, because Bitbucket answers 400 and 400 is
	// validation everywhere else -- so the test allowed both rather than decide.
	// The error registry settles it: the body carries
	// com.atlassian.bitbucket.repository.DuplicateRefException, which says the
	// request was well formed and the branch already exists. Exit 2 sends the
	// caller to check what they typed; exit 5 describes the repository.
	dupOutput, dupErr := executeLiveCLI(t, "--json", "branch", "create", branchName, "--start-point", "refs/heads/master")
	if dupErr == nil {
		t.Fatalf("expected a conflict on duplicate branch create, got success:\n%s", dupOutput)
	}
	if code := apperrors.ExitCode(dupErr); code != 5 {
		t.Fatalf("exit code %d on a duplicate branch, want 5 (conflict): %v", code, dupErr)
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

// TestLiveEveryServiceMapsItsFailures asks one question of every service
// package that talks to Bitbucket: when the thing you asked for is not there,
// does the caller get not_found?
//
// The unit tests this replaces asked it per service by standing up a server
// that answered 403 or 404 and checking the kind that came back. That is one
// assertion about openapi.MapStatusError, which is a single pure function with
// its own table test, wearing a service's clothes -- and it made the mapping
// look like a per-service concern until ten packages had grown their own copy
// of the same table.
//
// What is worth asking per service is narrower and cannot be answered by a
// fixture: is this service wired to the taxonomy at all, against a server that
// really refuses. One path per package settles that. Every path per package
// would not be feasible, and is not what these ever claimed.
//
// A package missing from this table has no live proof that its failures carry
// the right kind. Adding a service means adding a row.
func TestLiveEveryServiceMapsItsFailures(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	missingRepo := seeded.Key + "/no-such-repository"
	const missingProject = "NOSUCHPROJECT99999"
	const missingCommit = "0000000000000000000000000000000000000000"

	cases := []struct {
		service string
		args    []string
	}{
		{service: "branch", args: []string{"branch", "list", "--repo", missingRepo}},
		{service: "browse", args: []string{"repo", "browse", "tree", "docs", "--repo", missingRepo}},
		{service: "comment", args: []string{"pr", "comment", "list", "999999"}},
		{service: "commit", args: []string{"commit", "list", "--repo", missingRepo}},
		{service: "diff", args: []string{"pr", "diff", "999999"}},
		{service: "jira", args: []string{"pr", "jira", "999999"}},
		{service: "project", args: []string{"project", "get", missingProject}},
		{service: "pullrequest", args: []string{"pr", "get", "999999"}},
		{service: "pullrequestactivity", args: []string{"pr", "activity", "list", "999999"}},
		{service: "quality", args: []string{"insights", "report", "list", missingCommit, "--repo", missingRepo}},
		{service: "reposettings", args: []string{"repo", "settings", "pull-requests", "get", "--repo", missingRepo}},
		{service: "repository", args: []string{"repo", "archive", "--repo", missingRepo}},
		{service: "reviewer", args: []string{"reviewer", "condition", "list", "--project", missingProject}},
		{service: "project permissions", args: []string{"project", "permissions", "list", missingProject}},
		{service: "tag", args: []string{"tag", "view", "no-such-tag", "--repo", missingRepo}},
	}

	for _, testCase := range cases {
		t.Run(testCase.service, func(t *testing.T) {
			output, err := executeLiveCLI(t, append([]string{"--json"}, testCase.args...)...)
			if err == nil {
				t.Fatalf("expected a missing resource to fail, got:\n%s", output)
			}

			if code := apperrors.ExitCode(err); code != 4 {
				t.Fatalf("exit code = %d, want 4 (not_found): %v", code, err)
			}
			// A permanent absence reported as transient tells an agent to retry
			// forever, which is the failure the taxonomy exists to prevent.
			if strings.Contains(err.Error(), "transient") {
				t.Errorf("a missing resource was reported as transient: %v", err)
			}
		})
	}
}

// TestLiveJiraIssuesOnARealPullRequest covers the other half of OPENAPI-029:
// a pull request that does exist and has no linked issues must still report
// none, rather than being turned into an error by the existence check that
// empty answer now triggers.
func TestLiveJiraIssuesOnARealPullRequest(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	const branch = "feature/jira-none"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "jira.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}
	prID := createLivePRForRegression(t, branch, "No linked issues", "--no-default-reviewers", "--no-codeowners")

	output, err := executeLiveCLI(t, "pr", "jira", prID)
	if err != nil {
		t.Fatalf("a pull request with no linked issues must not fail: %v\noutput: %s", err, output)
	}
	if !strings.Contains(output, "No Jira issues") {
		t.Fatalf("expected the empty message, got:\n%s", output)
	}
}

// TestLiveErrorTaxonomyRejectedCredentials pins the other end of the taxonomy:
// a request Bitbucket refuses to authenticate is exit 3, not 10.
//
// The difference decides what an agent does next. A transient code says retry,
// and retrying a bad token is a loop that ends when the account locks.
//
// A unit test asserted this by answering 401 to everything, which is a claim
// about Bitbucket in a test that could not check it. A token that is not a
// token is a refusal any instance will really give.
func TestLiveErrorTaxonomyRejectedCredentials(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("BITBUCKET_PASSWORD", "")
	t.Setenv("BITBUCKET_TOKEN", "not-a-token")

	for _, testCase := range []struct {
		name string
		args []string
	}{
		{name: "repo list", args: []string{"--json", "repo", "list"}},
		{name: "project list", args: []string{"--json", "project", "list"}},
		{name: "pr list", args: []string{"--json", "pr", "list"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			output, err := executeLiveCLI(t, testCase.args...)
			if err == nil {
				t.Fatalf("a rejected token listed successfully:\n%s", output)
			}
			if code := apperrors.ExitCode(err); code != 3 {
				t.Fatalf("exit code = %d, want 3 (authorization): %v", code, err)
			}
		})
	}
}
