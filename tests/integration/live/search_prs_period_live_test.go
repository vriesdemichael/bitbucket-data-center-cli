//go:build live

package live_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// bb search prs lists your own pull requests across repositories, by state and
// by period (#697). The dashboard it reads has no "all" state and answered 400
// to one, so --state all and --state closed both failed; it takes merged and
// declined, which bb did not offer; and nothing bounded a listing by date.
//
// Every listing here is the whole instance's, since the suite runs as one
// user, so each is narrowed to this test's own project before it is read.
func TestLiveSearchPullRequestsByStateAndPeriod(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// A minute's margin either side of the pull requests, for a clock that is
	// not the server's.
	before := time.Now().Add(-time.Minute).Format(time.RFC3339)

	open := func(branch, title string) int64 {
		t.Helper()
		if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, branch+".txt"); err != nil {
			t.Fatalf("push %s failed: %v", branch, err)
		}
		var created livePullRequest
		decodeJSONData(t, mustLiveCLI(t, "pr", "create", "--from-ref", branch, "--to-ref", "master", "--title", title), &created)

		return created.PullRequest.ID
	}
	stillOpen := open("stays-open", "LIVE-42: stays open")
	merged := open("gets-merged", "Gets merged")
	declined := open("gets-declined", "Gets declined")
	mustLiveCLI(t, "pr", "merge", fmt.Sprintf("%d", merged))
	mustLiveCLI(t, "pr", "decline", fmt.Sprintf("%d", declined))

	type listed struct {
		ID         int64    `json:"id"`
		State      string   `json:"state"`
		URL        string   `json:"url"`
		ClosedDate int64    `json:"closedDate"`
		IssueKeys  []string `json:"issueKeys"`
		Repository struct {
			ProjectKey string `json:"projectKey"`
		} `json:"repository"`
	}
	search := func(args ...string) []listed {
		t.Helper()
		var found struct {
			PullRequests []listed `json:"pullRequests"`
		}
		decodeJSONData(t, mustLiveCLIUnscoped(t, append([]string{"search", "prs"}, args...)...), &found)

		own := []listed{}
		for _, pullRequest := range found.PullRequests {
			if pullRequest.Repository.ProjectKey == seeded.Key {
				own = append(own, pullRequest)
			}
		}
		return own
	}
	ids := func(pullRequests []listed) []int64 {
		found := []int64{}
		for _, pullRequest := range pullRequests {
			found = append(found, pullRequest.ID)
		}
		slices.Sort(found)
		return found
	}
	expect := func(what string, got []listed, want ...int64) {
		t.Helper()
		slices.Sort(want)
		if !slices.Equal(ids(got), want) {
			t.Errorf("%s lists %v of this test's pull requests, want %v", what, ids(got), want)
		}
	}

	// Each state, on the dashboard. --since bounds the walk to this test's own
	// minutes, and asks for the whole of them.
	expect("--state open", search("--role", "author", "--state", "open", "--since", before), stillOpen)
	expect("--state merged", search("--role", "author", "--state", "merged", "--since", before), merged)
	expect("--state declined", search("--role", "author", "--state", "declined", "--since", before), declined)
	expect("--state closed", search("--role", "author", "--state", "closed", "--since", before), merged, declined)
	everything := search("--role", "author", "--state", "all", "--since", before)
	expect("--state all", everything, stillOpen, merged, declined)

	// What each carries: where it is, when it closed, and the issue it names.
	for _, pullRequest := range everything {
		if !strings.HasSuffix(pullRequest.URL, fmt.Sprintf("/projects/%s/repos/%s/pull-requests/%d", seeded.Key, repo.Slug, pullRequest.ID)) {
			t.Errorf("pull request %d reports url %q", pullRequest.ID, pullRequest.URL)
		}
		if closed := pullRequest.ClosedDate != 0; closed != (pullRequest.ID != stillOpen) {
			t.Errorf("pull request %d (%s) reports closedDate %d", pullRequest.ID, pullRequest.State, pullRequest.ClosedDate)
		}
		if named := slices.Equal(pullRequest.IssueKeys, []string{"LIVE-42"}); named != (pullRequest.ID == stillOpen) {
			t.Errorf("pull request %d reports issue keys %v", pullRequest.ID, pullRequest.IssueKeys)
		}
	}

	// The period, on each side and on another date. The one still open has
	// never closed, so it is in no period on the closed date.
	later := time.Now().Add(time.Hour).Format(time.RFC3339)
	expect("a period that begins later", search("--role", "author", "--state", "all", "--since", later))
	expect("a period around them", search("--role", "author", "--state", "all", "--since", before, "--until", later), stillOpen, merged, declined)
	expect("closed in the period", search("--state", "all", "--date-field", "closed", "--since", before), merged, declined)

	// Within one repository the same flags apply. A period with only an end
	// has nothing to stop the walk, so it is asked of this repository alone
	// rather than of every pull request on the instance.
	scoped := seeded.Key + "/" + repo.Slug
	expect("merged in the repository", search("--repo", scoped, "--state", "merged", "--date-field", "closed", "--since", before), merged)
	expect("everything in the repository", search("--repo", scoped, "--state", "all", "--since", before), stillOpen, merged, declined)
	expect("a period that ended before them", search("--repo", scoped, "--state", "all", "--until", before, "--all"))

	// Grouped by repository, the text output puts each under its own heading.
	output, err := executeLiveCLIUnscoped(t, "search", "prs", "--role", "author", "--state", "all", "--since", before, "--group-by", "repo")
	if err != nil {
		t.Fatalf("search prs --group-by repo failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, scoped+"\n") || !strings.Contains(output, "LIVE-42: stays open") {
		t.Errorf("the grouped output has no heading for %s, or not its pull requests:\n%s", scoped, output)
	}
}
