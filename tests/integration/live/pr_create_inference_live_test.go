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

// bb pr create on a branch Bitbucket does not have says to push it, and says
// nothing else (#767). It used to ask for --title, because inferring one asks
// Bitbucket for the branch's commits and found none, and with a title given it
// warned that default reviewers and code owners could not be resolved before
// failing on the branch, none of it saying to push.
//
// A branch whose name holds the local one's is on Bitbucket, so the lookup is
// held to the exact name, and the checkout's remote is not called origin, so
// the push the message names is the remote the repository was inferred from.
func TestLivePullRequestCreateSaysToPushABranchBitbucketDoesNotHave(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnvVars(t, "WRONG", "wrong")

	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, "local-only-elsewhere", "elsewhere.txt"); err != nil {
		t.Fatalf("push the branch whose name holds the local one failed: %v", err)
	}

	pushURL, err := repositoryPushURL(harness.config, seeded.Key, repo.Slug)
	if err != nil {
		t.Fatalf("build push url: %v", err)
	}
	checkout := t.TempDir()
	for _, args := range [][]string{
		{"init"},
		{"remote", "add", "upstream", pushURL},
		{"fetch", "upstream"},
		{"checkout", "-b", "local-only", "upstream/master"},
		{"-c", "user.name=Live Test", "-c", "user.email=live@example.local", "commit", "--allow-empty", "-m", "A change nobody pushed"},
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

	const push = "git push -u upstream local-only"
	for name, args := range map[string][]string{
		"everything inferred": {"pr", "create"},
		"everything given":    {"pr", "create", "--from-ref", "local-only", "--to-ref", "master", "--title", "Given"},
	} {
		output, err := executeLiveCLI(t, append(args, "--no-input")...)
		if !apperrors.IsKind(err, apperrors.KindNotFound) || !strings.Contains(err.Error(), "branch local-only is not on Bitbucket") || !strings.Contains(err.Error(), push) {
			t.Errorf("%s: want not_found saying to run %q, got %v\n%s", name, push, err, output)
		}
		for _, misleading := range []string{"--title", "default reviewers", "code owners"} {
			if strings.Contains(output, misleading) || (err != nil && strings.Contains(err.Error(), misleading)) {
				t.Errorf("%s: the refusal mentions %s, which is not the problem:\n%v\n%s", name, misleading, err, output)
			}
		}
	}

	// Nothing was opened from it.
	var listed struct {
		PullRequests []map[string]any `json:"pullRequests"`
	}
	decodeJSONData(t, mustLiveCLI(t, "pr", "list", "--state", "all"), &listed)
	if len(listed.PullRequests) != 0 {
		t.Errorf("a pull request exists after the refusals: %v", listed.PullRequests)
	}
}
