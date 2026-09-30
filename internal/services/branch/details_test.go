package branch

import (
	"encoding/json"
	"strings"
	"testing"

	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// What Bitbucket 10.4.3 attached to a branch with one commit the base lacks,
// one commit behind it, a passing build and one open pull request. The live
// suite holds bb to what a real instance answers; this holds the decoder to a
// payload one answered with, including the providers bb does not read.
const recordedDetails = `{
	"com.atlassian.bitbucket.server.bitbucket-jira:branch-list-jira-issues": [],
	"com.atlassian.bitbucket.server.bitbucket-branch:ahead-behind-metadata-provider": {"ahead": 1, "behind": 1},
	"com.atlassian.bitbucket.server.bitbucket-branch:latest-commit-metadata": {
		"id": "dc898d6e6936facfc06ef37ba07739e434b74bb3",
		"displayId": "dc898d6e693",
		"author": {"name": "admin", "emailAddress": "admin@example.com"},
		"authorTimestamp": 1790776819000,
		"message": "second"
	},
	"com.atlassian.bitbucket.server.bitbucket-build:build-status-metadata": {"cancelled": 0, "successful": 1, "inProgress": 0, "failed": 0, "unknown": 0},
	"com.atlassian.bitbucket.server.bitbucket-ref-metadata:outgoing-pull-request-metadata": {"pullRequest": {"id": 1, "title": "Probe", "state": "OPEN"}}
}`

func metadataOf(t *testing.T, raw string) map[string]json.RawMessage {
	t.Helper()

	var metadata map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
		t.Fatalf("the test's own payload does not parse: %v", err)
	}

	return metadata
}

func TestBranchDetailsReadsEachPart(t *testing.T) {
	t.Parallel()

	details, err := branchDetails(metadataOf(t, recordedDetails))
	if err != nil {
		t.Fatalf("branchDetails: %v", err)
	}

	if details.AheadBehind == nil || details.AheadBehind.Ahead != 1 || details.AheadBehind.Behind != 1 {
		t.Errorf("ahead and behind = %+v, want 1 and 1", details.AheadBehind)
	}
	if commit := details.LatestCommit; commit == nil || commit.Id == nil || *commit.Id != "dc898d6e6936facfc06ef37ba07739e434b74bb3" ||
		commit.Message == nil || *commit.Message != "second" || commit.AuthorTimestamp == nil || *commit.AuthorTimestamp != 1790776819000 {
		t.Errorf("latest commit = %+v, want the one Bitbucket named", commit)
	}
	if builds := details.Builds; builds == nil || builds.Successful == nil || *builds.Successful != 1 {
		t.Errorf("builds = %+v, want one successful", builds)
	}
	if pullRequests := details.PullRequests; pullRequests == nil || pullRequests.Only == nil ||
		pullRequests.Only.ID != 1 || pullRequests.Only.Title != "Probe" || pullRequests.Open != 1 {
		t.Errorf("pull requests = %+v, want the one open pull request, counted as open", pullRequests)
	}
}

// The base branch has no ahead and behind, and a branch nobody built or opened
// a pull request from has neither of those parts. A part Bitbucket left out
// stays out, rather than reading as zero.
func TestBranchDetailsLeaveOutWhatBitbucketLeftOut(t *testing.T) {
	t.Parallel()

	details, err := branchDetails(metadataOf(t, `{"com.atlassian.bitbucket.server.bitbucket-jira:branch-list-jira-issues": []}`))
	if err != nil {
		t.Fatalf("branchDetails: %v", err)
	}
	if details.AheadBehind != nil || details.LatestCommit != nil || details.Builds != nil || details.PullRequests != nil {
		t.Errorf("details = %+v, want none", details)
	}
}

// Bitbucket names the pull request when a branch has exactly one, whatever its
// state, and counts them by state when it has more.
func TestBranchPullRequestsReadsBothShapes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		raw      string
		want     BranchPullRequests
		wantOnly bool
	}{
		{name: "one, open", raw: `{"pullRequest":{"id":7,"title":"A","state":"OPEN"}}`, want: BranchPullRequests{Open: 1}, wantOnly: true},
		{name: "one, merged", raw: `{"pullRequest":{"id":7,"title":"A","state":"MERGED"}}`, want: BranchPullRequests{Merged: 1}, wantOnly: true},
		{name: "one, declined", raw: `{"pullRequest":{"id":7,"title":"A","state":"DECLINED"}}`, want: BranchPullRequests{Declined: 1}, wantOnly: true},
		{name: "several", raw: `{"declined":1,"merged":0,"open":2}`, want: BranchPullRequests{Open: 2, Declined: 1}},
	}

	for _, testCase := range testCases {
		got, err := branchPullRequests(json.RawMessage(testCase.raw))
		if err != nil {
			t.Fatalf("%s: %v", testCase.name, err)
		}
		if got.Open != testCase.want.Open || got.Merged != testCase.want.Merged || got.Declined != testCase.want.Declined {
			t.Errorf("%s: counts = %d open, %d merged, %d declined, want %+v", testCase.name, got.Open, got.Merged, got.Declined, testCase.want)
		}
		if (got.Only != nil) != testCase.wantOnly {
			t.Errorf("%s: named the pull request = %t, want %t", testCase.name, got.Only != nil, testCase.wantOnly)
		}
	}
}

// A part that does not decode fails the listing. Left out instead, it would
// read as a branch Bitbucket had nothing to say about.
func TestBranchDetailsFailOnAPartThatDoesNotDecode(t *testing.T) {
	t.Parallel()

	for provider, part := range map[string]string{
		aheadBehindProvider:  "ahead and behind",
		latestCommitProvider: "latest commit",
		buildStatusProvider:  "builds",
		pullRequestProvider:  "pull requests",
	} {
		metadata := map[string]json.RawMessage{provider: json.RawMessage(`"not an object"`)}
		if _, err := branchDetails(metadata); err == nil || !strings.Contains(err.Error(), part) {
			t.Errorf("%s: err = %v, want one naming %q", provider, err, part)
		}
	}
}

func TestDetailedBranchesPairsEachBranchWithItsDetails(t *testing.T) {
	t.Parallel()

	main, feature := "main", "feature/x"
	branches := []openapigenerated.RestBranch{{DisplayId: &main}, {DisplayId: &feature}}
	body := `{"values":[{"displayId":"main","metadata":{}},{"displayId":"feature/x","metadata":` + recordedDetails + `}]}`

	detailed, err := detailedBranches(branches, []byte(body))
	if err != nil {
		t.Fatalf("detailedBranches: %v", err)
	}
	if len(detailed) != 2 || detailed[0].Details.AheadBehind != nil || detailed[1].Details.AheadBehind == nil {
		t.Fatalf("details = %+v, want none for main and ahead and behind for feature/x", detailed)
	}
	if *detailed[1].DisplayId != "feature/x" {
		t.Errorf("the second branch is %q, want feature/x", *detailed[1].DisplayId)
	}
}

func TestDetailedBranchesRefusesWhatItCannotPair(t *testing.T) {
	t.Parallel()

	main := "main"
	branches := []openapigenerated.RestBranch{{DisplayId: &main}}

	for name, body := range map[string]string{
		"a body that is not JSON":          `<html>`,
		"details for another number":       `{"values":[]}`,
		"a part of a branch that is wrong": `{"values":[{"metadata":{"` + aheadBehindProvider + `":"nope"}}]}`,
	} {
		if _, err := detailedBranches(branches, []byte(body)); err == nil || !strings.Contains(err.Error(), "failed to decode") {
			t.Errorf("%s: err = %v, want a decode failure", name, err)
		}
	}
}
