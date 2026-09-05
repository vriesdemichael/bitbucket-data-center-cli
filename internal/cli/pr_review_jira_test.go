package cli

import (
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// The mocked suite that lived here drove `pr jira`, `pr comment add --pending`
// and the three `pr review` subcommands against a hand-written Bitbucket.
//
// Every one of its assertions was about a payload the author had written on one
// side and read on the other: that a draft comment lists as "[10 v0] draft1",
// that finishing a review sends participantStatus APPROVED, that discarding one
// prints "Discarded review". The server agreed because the same person wrote
// both halves, and none of it said whether Bitbucket does any of it.
//
// It is live now, and more of it than was here:
// TestLivePullRequestReviewSurfaces adds a real draft, sees it in review get,
// discards it, adds another and watches complete publish it;
// TestLivePullRequestReviewDryRuns runs the three previews and checks the draft
// survived each one; TestLiveJiraIssuesOnARealPullRequest covers an issue list
// that is genuinely empty, which is the case a mock cannot pose since Bitbucket
// answers the same 200 [] for a pull request that does not exist (OPENAPI-029);
// TestLiveEveryServiceMapsItsFailures covers the failure statuses this injected.
//
// What is left is what never reached a server.

// TestReviewAndJiraCommandsNeedARepository covers the arguments these commands
// resolve before they connect to anything.
//
// A pull request id is not enough on its own: the repository comes from a flag,
// the configuration or the checkout, and with none of them the command has
// nowhere to send the request. Refusing here is what turns that into exit 2
// rather than a request to a URL built from empty strings.
func TestReviewAndJiraCommandsNeedARepository(t *testing.T) {
	commands := [][]string{
		{"pr", "jira", "42"},
		{"pr", "review", "get", "42"},
		{"pr", "review", "complete", "42"},
		{"pr", "review", "discard", "42"},
	}

	t.Run("no repository", func(t *testing.T) {
		// A URL is configured, so reaching the server is what would happen next
		// if the repository check did not stop it.
		t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
		t.Setenv("BITBUCKET_URL", "http://bitbucket.invalid")
		t.Setenv("BITBUCKET_TOKEN", "unused")
		t.Setenv("BITBUCKET_PROJECT_KEY", "")
		t.Setenv("BITBUCKET_REPO_SLUG", "")

		for _, args := range commands {
			_, err := executeTestCLI(t, args...)
			if err == nil {
				t.Errorf("%s ran without a repository", strings.Join(args, " "))

				continue
			}
			// The URL points at a host that does not resolve, so "it failed" is
			// not the assertion: a command that tried to connect would fail too,
			// and the point is that none of them got that far.
			if code := apperrors.ExitCode(err); code != 2 {
				t.Errorf("%s exited %d, want 2 for a missing repository: %v",
					strings.Join(args, " "), code, err)
			}
			if !strings.Contains(err.Error(), "repository is required") {
				t.Errorf("%s failed for another reason: %v", strings.Join(args, " "), err)
			}
		}
	})

	t.Run("no bitbucket url", func(t *testing.T) {
		t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
		t.Setenv("BITBUCKET_URL", "")
		t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
		t.Setenv("BITBUCKET_REPO_SLUG", "demo")

		for _, args := range commands {
			if _, err := executeTestCLI(t, args...); err == nil {
				t.Errorf("%s ran without a configured Bitbucket", strings.Join(args, " "))
			}
		}
	})
}
