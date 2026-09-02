package commitcmd

import (
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
)

// Commits is what `bb commit list` and `bb commit compare` return.
//
// The same shape served both before this, written twice; `bb search commits`
// wrote it a third time in another package. It is one type now, in result,
// because three copies of a shape are three chances for it to drift.
type Commits struct {
	Repository result.Repository `json:"repository"`
	Commits    []result.Commit   `json:"commits" jsonschema:"Matching commits, newest first. Empty rather than absent when nothing matched."`
}

// SingleCommit is what `bb commit get` returns.
type SingleCommit struct {
	Repository result.Repository `json:"repository"`
	Commit     result.Commit     `json:"commit"`
}

// ContainingPullRequests is what `bb commit pull-requests` returns: the pull
// requests a given commit appears in.
type ContainingPullRequests struct {
	Repository   result.Repository    `json:"repository"`
	PullRequests []result.PullRequest `json:"pullRequests" jsonschema:"Pull requests containing the commit. Empty rather than absent when it appears in none."`
}

func init() {
	result.Declare("commit list", result.For[Commits](nil))
	result.Declare("commit compare", result.For[Commits](nil))
	result.Declare("commit get", result.For[SingleCommit](nil))
	result.Declare("commit prs", result.For[ContainingPullRequests](map[string][]string{
		"pullRequests.state":                result.PullRequestStates,
		"pullRequests.reviewers.role":       result.ReviewerRoles,
		"pullRequests.reviewers.status":     result.ReviewerStatuses,
		"pullRequests.mergeability.outcome": result.MergeOutcomes,
	}))
}
