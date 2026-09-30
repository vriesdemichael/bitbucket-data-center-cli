//go:build live

package live_test

import (
	"context"
	"strings"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// detailedBranch is one branch in `bb branch list --details --json`. A pointer
// is nil when the listing left the field out.
type detailedBranch struct {
	DisplayID    string `json:"displayId"`
	LatestCommit string `json:"latestCommit"`
	Ahead        *int   `json:"ahead"`
	Behind       *int   `json:"behind"`
	LastCommit   *struct {
		ID string `json:"id"`
	} `json:"lastCommit"`
	Builds *struct {
		Successful int `json:"successful"`
		Failed     int `json:"failed"`
		InProgress int `json:"inProgress"`
		Unknown    int `json:"unknown"`
		Cancelled  int `json:"cancelled"`
	} `json:"builds"`
	PullRequests *struct {
		Open     int `json:"open"`
		Merged   int `json:"merged"`
		Declined int `json:"declined"`
		Only     *struct {
			ID    int    `json:"id"`
			Title string `json:"title"`
			State string `json:"state"`
		} `json:"only"`
	} `json:"pullRequests"`
}

// detailedListing runs `bb branch list --json --all` with the extra arguments
// and returns the branches by name.
func detailedListing(t *testing.T, extra ...string) map[string]detailedBranch {
	t.Helper()

	output := mustLiveCLI(t, append([]string{"branch", "list", "--all"}, extra...)...)

	var listing struct {
		Branches []detailedBranch `json:"branches"`
	}
	if err := decodeJSONEnvelopeData(output, &listing); err != nil {
		t.Fatalf("branch list returned invalid JSON: %v\n%s", err, output)
	}

	byName := map[string]detailedBranch{}
	for _, branch := range listing.Branches {
		byName[branch.DisplayID] = branch
	}

	return byName
}

// assertAheadBehind checks a branch's counts. want is nil for the base itself,
// which Bitbucket gives none.
func assertAheadBehind(t *testing.T, listing map[string]detailedBranch, name string, want *[2]int) {
	t.Helper()

	branch, listed := listing[name]
	switch {
	case !listed:
		t.Errorf("%s is not in the listing", name)
	case want == nil && (branch.Ahead != nil || branch.Behind != nil):
		t.Errorf("%s is the base, and is listed %d ahead and %d behind", name, *branch.Ahead, *branch.Behind)
	case want != nil && (branch.Ahead == nil || branch.Behind == nil):
		t.Errorf("%s carries no ahead and behind, want %d and %d", name, want[0], want[1])
	case want != nil && (*branch.Ahead != want[0] || *branch.Behind != want[1]):
		t.Errorf("%s is listed %d ahead and %d behind, want %d and %d", name, *branch.Ahead, *branch.Behind, want[0], want[1])
	}
}

// TestLiveCLIBranchListDetails: --details prints what Bitbucket's branch list
// shows beside each branch, and --base chooses what ahead and behind are
// measured against.
//
// bb sent both and printed neither, so every count here is one the test
// arranged: a branch one commit behind master, a branch one commit ahead of
// it, two builds reported for that branch's commit and two pull requests
// opened from it.
func TestLiveCLIBranchListDetails(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{Commits: 2, WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	const older, newer = "feature/older", "feature/newer"
	mustLiveCLI(t, "branch", "create", older, "--start-point", branchOlderSeedCommit(t, repo))
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, newer, "newer.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	// Without --details a branch is its name and its commit.
	for name, branch := range detailedListing(t) {
		if branch.Ahead != nil || branch.Behind != nil || branch.LastCommit != nil || branch.Builds != nil || branch.PullRequests != nil {
			t.Errorf("%s carries details nobody asked for: %+v", name, branch)
		}
	}

	// Against the default branch, which is what Bitbucket measures from when
	// no base is named.
	detailed := detailedListing(t, "--details")
	assertAheadBehind(t, detailed, "master", nil)
	assertAheadBehind(t, detailed, older, &[2]int{0, 1})
	assertAheadBehind(t, detailed, newer, &[2]int{1, 0})
	for _, name := range []string{"master", older, newer} {
		branch := detailed[name]
		if branch.LastCommit == nil || branch.LastCommit.ID == "" || branch.LastCommit.ID != branch.LatestCommit {
			t.Errorf("%s: the last commit is %+v, want the commit the branch points at, %s", name, branch.LastCommit, branch.LatestCommit)
		}
		if branch.Builds != nil || branch.PullRequests != nil {
			t.Errorf("%s has no build and no pull request yet, and is listed with %+v and %+v", name, branch.Builds, branch.PullRequests)
		}
	}

	// Against a named base the same three branches give other counts, and the
	// one without counts is the base that was named.
	against := detailedListing(t, "--details", "--base", newer)
	assertAheadBehind(t, against, newer, nil)
	assertAheadBehind(t, against, "master", &[2]int{0, 1})
	assertAheadBehind(t, against, older, &[2]int{0, 2})

	// --base asks for the details by itself: there is nothing else it could
	// change. The full ref names the same base.
	implied := detailedListing(t, "--base", "refs/heads/"+newer)
	assertAheadBehind(t, implied, newer, nil)
	assertAheadBehind(t, implied, older, &[2]int{0, 2})
	if output, err := executeLiveCLI(t, "--json", "branch", "list", "--base", "no-such-branch", "--all"); !apperrors.IsKind(err, apperrors.KindNotFound) {
		t.Fatalf("--base naming no branch was not refused as not found: %v\n%s", err, output)
	}

	// Two builds for the newer branch's commit, in two states.
	commit := detailed[newer].LatestCommit
	mustLiveCLI(t, "build", "status", "set", commit, "--key", "unit", "--state", "SUCCESSFUL", "--url", "http://example.invalid/unit")
	mustLiveCLI(t, "build", "status", "set", commit, "--key", "lint", "--state", "FAILED", "--url", "http://example.invalid/lint")

	// One pull request from it, which Bitbucket names.
	firstID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, newer, "master")
	if err != nil {
		t.Fatalf("create pull request failed: %v", err)
	}

	withOne := detailedListing(t, "--details")
	if builds := withOne[newer].Builds; builds == nil || builds.Successful != 1 || builds.Failed != 1 || builds.InProgress != 0 || builds.Unknown != 0 || builds.Cancelled != 0 {
		t.Errorf("%s: builds are %+v, want one successful and one failed", newer, builds)
	}
	if pullRequests := withOne[newer].PullRequests; pullRequests == nil || pullRequests.Only == nil {
		t.Errorf("%s: pull requests are %+v, want the one that was opened", newer, pullRequests)
	} else {
		if got := asString(pullRequests.Only.ID); got != firstID || pullRequests.Only.State != "OPEN" || pullRequests.Only.Title == "" {
			t.Errorf("%s: its pull request is %+v, want #%s, open, with its title", newer, *pullRequests.Only, firstID)
		}
		if pullRequests.Open != 1 || pullRequests.Merged != 0 || pullRequests.Declined != 0 {
			t.Errorf("%s: one open pull request is counted as %d open, %d merged, %d declined", newer, pullRequests.Open, pullRequests.Merged, pullRequests.Declined)
		}
	}
	for _, name := range []string{"master", older} {
		if branch := withOne[name]; branch.Builds != nil || branch.PullRequests != nil {
			t.Errorf("%s has no build and no pull request, and is listed with %+v and %+v", name, branch.Builds, branch.PullRequests)
		}
	}

	// A second pull request from the same branch: Bitbucket counts them by
	// state and names neither.
	mustLiveCLI(t, "pr", "decline", firstID)
	if _, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, newer, "master"); err != nil {
		t.Fatalf("create pull request failed: %v", err)
	}

	if pullRequests := detailedListing(t, "--details")[newer].PullRequests; pullRequests == nil ||
		pullRequests.Only != nil || pullRequests.Open != 1 || pullRequests.Declined != 1 || pullRequests.Merged != 0 {
		t.Errorf("%s: pull requests are %+v, want one open and one declined, and neither named", newer, pullRequests)
	}

	// The same, as a person reads it.
	text, err := executeLiveCLI(t, "branch", "list", "--details", "--all")
	if err != nil {
		t.Fatalf("branch list --details failed: %v\n%s", err, text)
	}
	for _, line := range strings.Split(text, "\n") {
		switch {
		case strings.Contains(line, newer):
			for _, want := range []string{"ahead=1 behind=0", "prs=1 open, 1 declined", "builds=1 successful, 1 failed"} {
				if !strings.Contains(line, want) {
					t.Errorf("the row of %s lacks %q: %q", newer, want, line)
				}
			}
		case strings.Contains(line, older):
			if !strings.Contains(line, "ahead=0 behind=1") || strings.Contains(line, "prs=") || strings.Contains(line, "builds=") {
				t.Errorf("the row of %s should say how far behind it is and nothing of builds or pull requests: %q", older, line)
			}
		case strings.Contains(line, "master"):
			if strings.Contains(line, "ahead=") {
				t.Errorf("the row of the base gives it ahead and behind counts: %q", line)
			}
		}
	}
	if !strings.Contains(text, newer) || !strings.Contains(text, older) {
		t.Fatalf("the listing lacks a branch:\n%s", text)
	}
}
