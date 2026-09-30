//go:build live

package live_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// bb pr create works out what it was not told from the checkout it is run in
// (#705): the branch that is checked out, the repository's default branch, and
// a title when the branch holds exactly one commit. It used to refuse naming
// all three, standing on a pushed branch that answered every one of them.
//
// Nobody is there to ask in a test, which is the case that matters: an
// inference is used only where it leaves no choice, and what is left is named.
func TestLivePullRequestCreateInfersFromTheCheckout(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnvVars(t, "WRONG", "wrong")

	const subject = "Teach the parser about tabs"
	if err := harness.pushCommitWithMessage(seeded.Key, repo.Slug, "only-one", "only-one.txt", subject+"\n\nA body, which is not the title."); err != nil {
		t.Fatalf("push the one-commit branch failed: %v", err)
	}
	if err := harness.pushCommitsOnBranch(seeded.Key, repo.Slug, "holds-two", 2); err != nil {
		t.Fatalf("push the two-commit branch failed: %v", err)
	}

	pushURL, err := repositoryPushURL(harness.config, seeded.Key, repo.Slug)
	if err != nil {
		t.Fatalf("build push url: %v", err)
	}
	checkout := t.TempDir()
	for _, args := range [][]string{
		{"init"},
		{"remote", "add", "origin", pushURL},
		{"fetch", "origin"},
		{"checkout", "-b", "only-one", "origin/only-one"},
	} {
		if err := runGit(checkout, args...); err != nil {
			t.Fatalf("git %s failed: %v", strings.Join(args, " "), err)
		}
	}

	originalDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	if err := os.Chdir(checkout); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalDirectory) })

	stored := func(id int64) map[string]any {
		t.Helper()
		var got struct {
			PullRequest map[string]any `json:"pullRequest"`
		}
		decodeJSONData(t, mustLiveCLI(t, "pr", "get", fmt.Sprintf("%d", id), "--no-review-summary"), &got)

		return got.PullRequest
	}

	// One commit: all three are inferred, and Bitbucket stored what was.
	var created livePullRequest
	decodeJSONData(t, mustLiveCLI(t, "pr", "create"), &created)
	if got := stored(created.PullRequest.ID); got["sourceBranch"] != "only-one" || got["targetBranch"] != "master" || got["title"] != subject {
		t.Fatalf("the inferred pull request is from %v to %v, titled %q; want only-one to master, titled %q",
			got["sourceBranch"], got["targetBranch"], got["title"], subject)
	}

	// Two commits: the branches are inferred and the title is not, so the
	// refusal names the title and nothing else.
	if err := runGit(checkout, "checkout", "-b", "holds-two", "origin/holds-two"); err != nil {
		t.Fatalf("git checkout holds-two failed: %v", err)
	}
	output, err := executeLiveCLI(t, "pr", "create")
	if !apperrors.IsKind(err, apperrors.KindValidation) || !strings.Contains(err.Error(), "required flag(s) --title not set") {
		t.Fatalf("a branch with two commits must be refused for its title alone, got %v\n%s", err, output)
	}
	if !strings.Contains(output, "Using --from-ref holds-two (the checked-out branch)") ||
		!strings.Contains(output, "Using --to-ref master (the repository's default branch)") {
		t.Fatalf("the run does not say which branches it inferred:\n%s", output)
	}

	decodeJSONData(t, mustLiveCLI(t, "pr", "create", "--title", "Two commits, one title"), &created)
	if got := stored(created.PullRequest.ID); got["sourceBranch"] != "holds-two" || got["targetBranch"] != "master" || got["title"] != "Two commits, one title" {
		t.Fatalf("the pull request is from %v to %v, titled %q", got["sourceBranch"], got["targetBranch"], got["title"])
	}

	// A repository named with --repo is not this checkout's to speak for, so
	// the branch underfoot is not taken as the source.
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, "named-repo", "named-repo.txt"); err != nil {
		t.Fatalf("push the third branch failed: %v", err)
	}
	for _, args := range [][]string{{"fetch", "origin"}, {"checkout", "-b", "named-repo", "origin/named-repo"}} {
		if err := runGit(checkout, args...); err != nil {
			t.Fatalf("git %s failed: %v", strings.Join(args, " "), err)
		}
	}
	output, err = executeLiveCLI(t, "pr", "create", "--repo", seeded.Key+"/"+repo.Slug)
	if !apperrors.IsKind(err, apperrors.KindValidation) || !strings.Contains(err.Error(), "--from-ref") {
		t.Fatalf("with --repo named, the source branch must still be asked for, got %v\n%s", err, output)
	}
}
