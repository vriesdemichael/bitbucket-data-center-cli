package branchcmd

import (
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	branchservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/branch"
)

func int32Pointer(value int32) *int32 { return &value }

func TestDetailCellsSayWhatBitbucketSaidAndNothingElse(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		branch ListedBranch
		want   string
	}{
		{name: "the base branch, which has only a commit", branch: ListedBranch{
			LastCommit: &result.Commit{AuthorTimestamp: 1790776819000, Author: result.Person{Name: "alice"}},
		}, want: "|2026-09-30 alice"},
		{name: "one pull request", branch: ListedBranch{
			Ahead: int32Pointer(2), Behind: int32Pointer(0),
			PullRequests: &BranchPullRequests{Open: 1, Only: &BranchPullRequest{ID: 42, State: "OPEN"}},
		}, want: "ahead=2 behind=0||pr=#42 OPEN"},
		{name: "several pull requests and builds", branch: ListedBranch{
			Ahead: int32Pointer(1), Behind: int32Pointer(3),
			PullRequests: &BranchPullRequests{Open: 2, Declined: 1},
			Builds:       &BranchBuilds{Successful: 3, Failed: 1},
		}, want: "ahead=1 behind=3||prs=2 open, 1 declined|builds=3 successful, 1 failed"},
		{name: "a tally of nothing", branch: ListedBranch{
			Builds: &BranchBuilds{},
		}, want: "|||builds=none"},
		{name: "nothing at all", branch: ListedBranch{}, want: ""},
	}

	for _, testCase := range testCases {
		if got := strings.Join(detailCells(testCase.branch), "|"); got != testCase.want {
			t.Errorf("%s: cells = %q, want %q", testCase.name, got, testCase.want)
		}
	}
}

func TestDetailedBranchesFromCarriesEveryPart(t *testing.T) {
	t.Parallel()

	name, commitID, message := "feature/x", "dc898d6e6936facfc06ef37ba07739e434b74bb3", "second"
	timestamp := int64(1790776819000)

	listed := detailedBranchesFrom([]branchservice.DetailedBranch{
		{RestBranch: openapigenerated.RestBranch{DisplayId: &name}},
		{
			RestBranch: openapigenerated.RestBranch{DisplayId: &name},
			Details: branchservice.BranchDetails{
				AheadBehind:  &branchservice.AheadBehind{Ahead: 1, Behind: 2},
				LatestCommit: &openapigenerated.RestCommit{Id: &commitID, Message: &message, AuthorTimestamp: &timestamp},
				Builds:       &openapigenerated.RestBuildStats{Failed: int32Pointer(1)},
				PullRequests: &branchservice.BranchPullRequests{Open: 1, Only: &branchservice.BranchPullRequest{ID: 7, Title: "T", State: "OPEN"}},
			},
		},
	})

	if len(listed) != 2 {
		t.Fatalf("got %d branches, want 2", len(listed))
	}
	if plain := listed[0]; plain.Ahead != nil || plain.Behind != nil || plain.LastCommit != nil || plain.Builds != nil || plain.PullRequests != nil {
		t.Errorf("a branch Bitbucket said nothing about carries details: %+v", plain)
	}

	detailed := listed[1]
	if detailed.Ahead == nil || *detailed.Ahead != 1 || detailed.Behind == nil || *detailed.Behind != 2 {
		t.Errorf("ahead and behind = %v and %v, want 1 and 2", detailed.Ahead, detailed.Behind)
	}
	if detailed.LastCommit == nil || detailed.LastCommit.ID != commitID || detailed.LastCommit.Message != message {
		t.Errorf("last commit = %+v", detailed.LastCommit)
	}
	if detailed.Builds == nil || detailed.Builds.Failed != 1 || detailed.Builds.Successful != 0 {
		t.Errorf("builds = %+v, want one failed", detailed.Builds)
	}
	if detailed.PullRequests == nil || detailed.PullRequests.Open != 1 || detailed.PullRequests.Only == nil || detailed.PullRequests.Only.ID != 7 {
		t.Errorf("pull requests = %+v, want the one open pull request", detailed.PullRequests)
	}
}
