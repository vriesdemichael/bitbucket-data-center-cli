package searchcmd

import (
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
)

// Commits is what `bb search commits` returns.
//
// The same shape `bb commit list` and `bb commit compare` return, which is why
// the pieces live in result rather than here: before this, the shape existed
// three times in two packages with nothing holding the copies together.
type Commits struct {
	Repository result.Repository `json:"repository"`
	Commits    []result.Commit   `json:"commits" jsonschema:"Matching commits, newest first. Empty rather than absent when nothing matched."`
}

// PullRequests is what `bb search prs` returns.
//
// No repository field: the search spans repositories, so each pull request
// names its own. That is why it is not the same shape as commit prs, which is
// scoped to one repository and says so once.
type PullRequests struct {
	PullRequests []result.PullRequest `json:"pullRequests" jsonschema:"Matching pull requests. Empty rather than absent when nothing matched."`
}

func init() {
	result.Declare("search repos", result.List[result.RepositorySummary](nil))
	result.Declare("search commits", result.For[Commits](nil))
	result.Declare("search prs", result.For[PullRequests](map[string][]string{
		"pullRequests.state":                result.PullRequestStates,
		"pullRequests.reviewers.role":       result.ReviewerRoles,
		"pullRequests.reviewers.status":     result.ReviewerStatuses,
		"pullRequests.mergeability.outcome": result.MergeOutcomes,
	}))
}
