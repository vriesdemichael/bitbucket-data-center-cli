package branchcmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/safederef"
	branchservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/branch"
)

// Branch is one branch in a repository.
//
// There is no type field. The upstream declares RestBranch.type as an untyped
// value -- the generated client renders it as interface{} -- so bb has nothing
// to promise about it, and every branch these commands return is a branch.
type Branch struct {
	ID           string `json:"id,omitempty" jsonschema:"Full ref name, for example refs/heads/main."`
	DisplayID    string `json:"displayId,omitempty" jsonschema:"Short branch name, for example main. This is what bb branch delete takes."`
	LatestCommit string `json:"latestCommit,omitempty" jsonschema:"Commit the branch currently points at."`
	Default      bool   `json:"default" jsonschema:"Whether this is the repository default branch."`
}

// Branches is what `bb branch list` returns.
type Branches struct {
	Repository result.Repository `json:"repository"`
	Branches   []ListedBranch    `json:"branches" jsonschema:"Matching branches. Empty rather than absent when nothing matched."`
}

// ListedBranch is a branch as `bb branch list` prints it.
//
// The fields after the branch's own are what Bitbucket's branch list shows
// beside each branch. They are asked for with --details, and each is present
// only when Bitbucket has something to say: the base branch is neither ahead
// of nor behind itself, so it has neither count.
type ListedBranch struct {
	Branch
	Ahead        *int32              `json:"ahead,omitempty" jsonschema:"With --details: commits the branch has that the base lacks. Absent for the base itself."`
	Behind       *int32              `json:"behind,omitempty" jsonschema:"With --details: commits the base has that the branch lacks. Absent for the base itself."`
	LastCommit   *result.Commit      `json:"lastCommit,omitempty" jsonschema:"With --details: the commit the branch points at."`
	Builds       *BranchBuilds       `json:"builds,omitempty" jsonschema:"With --details: the builds reported for that commit, by state. Absent when it has none."`
	PullRequests *BranchPullRequests `json:"pullRequests,omitempty" jsonschema:"With --details: the pull requests opened from the branch. Absent when there are none."`
}

// BranchBuilds counts the builds of a branch's latest commit by state.
type BranchBuilds struct {
	Successful int32 `json:"successful" jsonschema:"Builds reporting SUCCESSFUL."`
	Failed     int32 `json:"failed" jsonschema:"Builds reporting FAILED."`
	InProgress int32 `json:"inProgress" jsonschema:"Builds reporting INPROGRESS."`
	Unknown    int32 `json:"unknown" jsonschema:"Builds reporting UNKNOWN."`
	Cancelled  int32 `json:"cancelled" jsonschema:"Builds reporting CANCELLED."`
}

// BranchPullRequests is the pull requests opened from a branch, by state.
type BranchPullRequests struct {
	Open     int32              `json:"open" jsonschema:"Pull requests from the branch that are open."`
	Merged   int32              `json:"merged" jsonschema:"Pull requests from the branch that were merged."`
	Declined int32              `json:"declined" jsonschema:"Pull requests from the branch that were declined."`
	Only     *BranchPullRequest `json:"only,omitempty" jsonschema:"The pull request itself, when the branch has exactly one. Bitbucket names it only then."`
}

// BranchPullRequest is the one pull request a branch has.
type BranchPullRequest struct {
	ID    int64  `json:"id" jsonschema:"Pull request number, unique within the repository."`
	Title string `json:"title" jsonschema:"Pull request title."`
	State string `json:"state" jsonschema:"OPEN, MERGED or DECLINED."`
}

// BranchCreation is what `bb branch create` returns.
type BranchCreation struct {
	Repository result.Repository `json:"repository"`
	Branch     Branch            `json:"branch"`
}

// BranchDeletion is what `bb branch delete` reports.
type BranchDeletion struct {
	result.Status
	Repository result.Repository `json:"repository"`
	Branch     string            `json:"branch" jsonschema:"Short name of the branch that was deleted."`
}

// DefaultBranch is what `bb branch default get` returns.
type DefaultBranch struct {
	Repository    result.Repository `json:"repository"`
	DefaultBranch result.Ref        `json:"defaultBranch"`
}

// DefaultBranchChange is what `bb branch default set` and `bb branch model
// update` report.
//
// One type for two commands because they report the same fact: which branch is
// now the default. They already emitted identical payloads, by coincidence
// rather than by design.
type DefaultBranchChange struct {
	result.Status
	Repository    result.Repository `json:"repository"`
	DefaultBranch string            `json:"defaultBranch" jsonschema:"Short name of the branch that is now the default."`
}

// CommitRefs is what `bb branch model inspect` returns.
type CommitRefs struct {
	Repository result.Repository `json:"repository"`
	Commit     string            `json:"commit" jsonschema:"Commit that was inspected, as it was given on the command line."`
	Refs       []result.Ref      `json:"refs" jsonschema:"Branches containing the commit. Empty rather than absent when none do."`
}

// Restrictions is what `bb branch restriction list` returns.
type Restrictions struct {
	Repository   result.Repository    `json:"repository"`
	Restrictions []result.Restriction `json:"restrictions" jsonschema:"Branch restrictions in scope. Empty rather than absent when there are none."`
}

// SingleRestriction is what `bb branch restriction get`, `create` and `update`
// return.
type SingleRestriction struct {
	Repository  result.Repository  `json:"repository"`
	Restriction result.Restriction `json:"restriction"`
}

// RestrictionDeletion is what `bb branch restriction delete` reports.
type RestrictionDeletion struct {
	result.Status
	Repository    result.Repository `json:"repository"`
	RestrictionID string            `json:"restrictionId" jsonschema:"Identifier of the restriction that was deleted, as it was given on the command line."`
}

func init() {
	result.Declare("branch list", result.For[Branches](nil))
	result.Declare("branch create", result.For[BranchCreation](nil))
	result.Declare("branch delete", result.For[BranchDeletion](nil))

	result.Declare("branch default get", result.For[DefaultBranch](map[string][]string{"defaultBranch.type": result.RefTypes}))
	result.Declare("branch default set", result.For[DefaultBranchChange](nil))

	result.Declare("branch model inspect", result.For[CommitRefs](map[string][]string{"refs.type": result.RefTypes}))
	result.Declare("branch model update", result.For[DefaultBranchChange](nil))

	listEnums := map[string][]string{
		"restrictions.type":         result.RestrictionTypes,
		"restrictions.matcher.type": result.RefMatcherTypes,
		"restrictions.scope":        result.RestrictionScopes,
	}
	singleEnums := map[string][]string{
		"restriction.type":         result.RestrictionTypes,
		"restriction.matcher.type": result.RefMatcherTypes,
		"restriction.scope":        result.RestrictionScopes,
	}

	result.Declare("branch restriction list", result.For[Restrictions](listEnums))
	result.Declare("branch restriction get", result.For[SingleRestriction](singleEnums))
	result.Declare("branch restriction create", result.For[SingleRestriction](singleEnums))
	result.Declare("branch restriction update", result.For[SingleRestriction](singleEnums))
	result.Declare("branch restriction delete", result.For[RestrictionDeletion](nil))
}

// repositoryOf converts the service reference used throughout this package.
func repositoryOf(repo branchservice.RepositoryRef) result.Repository {
	return result.Repository{ProjectKey: repo.ProjectKey, Slug: repo.Slug}
}

// branchFrom converts one upstream branch.
func branchFrom(upstream openapigenerated.RestBranch) Branch {
	converted := Branch{
		ID:           safederef.String(upstream.Id),
		DisplayID:    safederef.String(upstream.DisplayId),
		LatestCommit: safederef.String(upstream.LatestCommit),
	}
	if upstream.IsDefault != nil {
		converted.Default = *upstream.IsDefault
	}

	return converted
}

// branchesFrom converts a list, preserving order and never returning nil.
func branchesFrom(upstream []openapigenerated.RestBranch) []ListedBranch {
	converted := make([]ListedBranch, 0, len(upstream))
	for _, one := range upstream {
		converted = append(converted, ListedBranch{Branch: branchFrom(one)})
	}

	return converted
}

// detailCells are the cells --details adds to a branch's row, one per detail
// and empty where Bitbucket had nothing to say, so the columns line up. Empty
// cells at the end of a row are dropped, since the table would pad them with
// spaces nothing follows.
func detailCells(branch ListedBranch) []string {
	cells := allDetailCells(branch)
	for len(cells) > 0 && cells[len(cells)-1] == "" {
		cells = cells[:len(cells)-1]
	}

	return cells
}

func allDetailCells(branch ListedBranch) []string {
	cells := make([]string, 4)

	if branch.Ahead != nil && branch.Behind != nil {
		cells[0] = fmt.Sprintf("ahead=%d behind=%d", *branch.Ahead, *branch.Behind)
	}
	if commit := branch.LastCommit; commit != nil && commit.AuthorTimestamp != 0 {
		cells[1] = strings.TrimSpace(time.UnixMilli(commit.AuthorTimestamp).UTC().Format("2006-01-02") + " " + commit.Author.Name)
	}
	if pullRequests := branch.PullRequests; pullRequests != nil {
		if only := pullRequests.Only; only != nil {
			cells[2] = fmt.Sprintf("pr=#%d %s", only.ID, only.State)
		} else {
			cells[2] = "prs=" + counted(
				count{pullRequests.Open, "open"}, count{pullRequests.Merged, "merged"}, count{pullRequests.Declined, "declined"})
		}
	}
	if builds := branch.Builds; builds != nil {
		cells[3] = "builds=" + counted(
			count{builds.Successful, "successful"}, count{builds.Failed, "failed"}, count{builds.InProgress, "in progress"},
			count{builds.Unknown, "unknown"}, count{builds.Cancelled, "cancelled"})
	}

	return cells
}

// count is a number of things in one state.
type count struct {
	number int32
	state  string
}

// counted writes the states that have any, as "2 open, 1 declined", or "none"
// when none has.
func counted(counts ...count) string {
	var parts []string
	for _, each := range counts {
		if each.number > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", each.number, each.state))
		}
	}
	if len(parts) == 0 {
		return "none"
	}

	return strings.Join(parts, ", ")
}

// detailedBranchesFrom converts a list that came with details, preserving
// order and never returning nil.
func detailedBranchesFrom(upstream []branchservice.DetailedBranch) []ListedBranch {
	converted := make([]ListedBranch, 0, len(upstream))
	for _, one := range upstream {
		listed := ListedBranch{Branch: branchFrom(one.RestBranch)}

		if counts := one.Details.AheadBehind; counts != nil {
			listed.Ahead, listed.Behind = &counts.Ahead, &counts.Behind
		}
		if commit := one.Details.LatestCommit; commit != nil {
			lastCommit := result.CommitFrom(*commit)
			listed.LastCommit = &lastCommit
		}
		if builds := one.Details.Builds; builds != nil {
			listed.Builds = &BranchBuilds{
				Successful: safederef.Int32(builds.Successful),
				Failed:     safederef.Int32(builds.Failed),
				InProgress: safederef.Int32(builds.InProgress),
				Unknown:    safederef.Int32(builds.Unknown),
				Cancelled:  safederef.Int32(builds.Cancelled),
			}
		}
		if pullRequests := one.Details.PullRequests; pullRequests != nil {
			listed.PullRequests = &BranchPullRequests{
				Open:     pullRequests.Open,
				Merged:   pullRequests.Merged,
				Declined: pullRequests.Declined,
			}
			if only := pullRequests.Only; only != nil {
				listed.PullRequests.Only = &BranchPullRequest{ID: only.ID, Title: only.Title, State: only.State}
			}
		}

		converted = append(converted, listed)
	}

	return converted
}
