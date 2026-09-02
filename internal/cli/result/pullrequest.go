package result

import (
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
)

// Reviewer is one person asked to review, and where they got to.
type Reviewer struct {
	Name        string `json:"name,omitempty" jsonschema:"Username."`
	DisplayName string `json:"displayName,omitempty" jsonschema:"Human-readable name."`
	Email       string `json:"email,omitempty" jsonschema:"Email address, when the instance exposes it."`
	Role        string `json:"role,omitempty" jsonschema:"REVIEWER or PARTICIPANT."`
	Status      string `json:"status,omitempty" jsonschema:"UNAPPROVED, NEEDS_WORK or APPROVED."`
	Approved    bool   `json:"approved" jsonschema:"Whether this reviewer has approved. A convenience for status == APPROVED."`
}

// MergeBlocker is one reason a merge would be refused.
type MergeBlocker struct {
	Summary string `json:"summary,omitempty" jsonschema:"Short reason, as Bitbucket phrases it."`
	Detail  string `json:"detail,omitempty" jsonschema:"Longer explanation, when the instance gives one."`
}

// Mergeability is whether the pull request can merge, and what stops it.
type Mergeability struct {
	Mergeable  bool           `json:"mergeable" jsonschema:"Whether Bitbucket would accept a merge right now."`
	Outcome    string         `json:"outcome,omitempty" jsonschema:"CLEAN, CONFLICTED or UNKNOWN."`
	Conflicted bool           `json:"conflicted" jsonschema:"Whether the merge would conflict. Distinct from mergeable, which is also false when a merge check fails without any conflict."`
	Blockers   []MergeBlocker `json:"blockers,omitempty" jsonschema:"Merge checks currently refusing. Absent when nothing is blocking."`
}

// PullRequest is one pull request, as bb reports it.
//
// Shared because three packages publish it: `bb commit pull-requests`,
// `bb search pull-requests` and the pr commands themselves.
type PullRequest struct {
	ID          int64  `json:"id" jsonschema:"Pull request number, unique within the repository."`
	Title       string `json:"title" jsonschema:"Pull request title."`
	Description string `json:"description,omitempty" jsonschema:"Body text, when one was written."`
	State       string `json:"state" jsonschema:"OPEN, MERGED or DECLINED."`
	Open        bool   `json:"open" jsonschema:"Whether the pull request is still open."`
	Closed      bool   `json:"closed" jsonschema:"Whether the pull request is merged or declined."`
	Draft       bool   `json:"draft,omitempty" jsonschema:"Whether it is marked as a draft. Bitbucket 8.0 and later."`

	Repository       Repository `json:"repository,omitzero" jsonschema:"Repository the pull request merges into."`
	SourceRepository Repository `json:"sourceRepository,omitzero" jsonschema:"Repository the source branch lives in. Differs from repository exactly when the pull request comes from a fork, which is the only way to tell a fork pull request from a same-repository one."`

	Version        int    `json:"version" jsonschema:"Optimistic-locking version. Pass it back when updating, or the update is refused. Always present: a never-updated pull request is at version 0, and omitting it there would drop the value at exactly the moment a caller needs it."`
	Author         string `json:"author,omitempty" jsonschema:"Display name of whoever opened it."`
	AuthorUsername string `json:"authorUsername,omitempty" jsonschema:"Username of whoever opened it."`
	SourceBranch   string `json:"sourceBranch,omitempty" jsonschema:"Branch being merged from."`
	TargetBranch   string `json:"targetBranch,omitempty" jsonschema:"Branch being merged into."`
	SourceCommit   string `json:"sourceCommit,omitempty" jsonschema:"Head commit of the source branch when this was read."`
	CreatedDate    int64  `json:"createdDate,omitempty" jsonschema:"When it was opened, in milliseconds since the epoch."`
	UpdatedDate    int64  `json:"updatedDate,omitempty" jsonschema:"When it last changed, in milliseconds since the epoch."`

	Reviewers    []Reviewer    `json:"reviewers,omitempty" jsonschema:"Everyone asked to review, with where they got to."`
	Mergeability *Mergeability `json:"mergeability,omitempty" jsonschema:"Whether it can merge. Absent when not requested or not computed."`

	// The counts stay pointers because absent and zero mean different things:
	// Bitbucket returns them in an undocumented properties object, so a missing
	// count means the server did not say, and reporting zero there would assert
	// something was checked and found empty.
	CommentCount      *int `json:"commentCount,omitempty" jsonschema:"Comments on the pull request. Absent when the instance did not report it, which is not the same as none."`
	OpenTaskCount     *int `json:"openTaskCount,omitempty" jsonschema:"Unresolved tasks. Absent when the instance did not report it."`
	ResolvedTaskCount *int `json:"resolvedTaskCount,omitempty" jsonschema:"Resolved tasks. Absent when the instance did not report it."`
}

// PullRequestFrom converts the service's pull request into the reported shape.
func PullRequestFrom(upstream pullrequestservice.PullRequest) PullRequest {
	converted := PullRequest{
		ID:                upstream.ID,
		Title:             upstream.Title,
		Description:       upstream.Description,
		State:             upstream.State,
		Open:              upstream.Open,
		Closed:            upstream.Closed,
		Draft:             upstream.Draft,
		Version:           upstream.Version,
		Author:            upstream.Author,
		AuthorUsername:    upstream.AuthorUsername,
		SourceBranch:      upstream.SourceBranch,
		TargetBranch:      upstream.TargetBranch,
		SourceCommit:      upstream.SourceCommit,
		CreatedDate:       upstream.CreatedDate,
		UpdatedDate:       upstream.UpdatedDate,
		CommentCount:      upstream.CommentCount,
		OpenTaskCount:     upstream.OpenTaskCount,
		ResolvedTaskCount: upstream.ResolvedTaskCount,
	}

	if upstream.Repository != nil {
		converted.Repository = Repository{ProjectKey: upstream.Repository.ProjectKey, Slug: upstream.Repository.Slug}
	}
	if upstream.SourceRepository != nil {
		converted.SourceRepository = Repository{ProjectKey: upstream.SourceRepository.ProjectKey, Slug: upstream.SourceRepository.Slug}
	}

	if len(upstream.Reviewers) > 0 {
		converted.Reviewers = make([]Reviewer, 0, len(upstream.Reviewers))
		for _, reviewer := range upstream.Reviewers {
			converted.Reviewers = append(converted.Reviewers, Reviewer{
				Name:        reviewer.Name,
				DisplayName: reviewer.DisplayName,
				Email:       reviewer.Email,
				Role:        reviewer.Role,
				Status:      reviewer.Status,
				Approved:    reviewer.Approved,
			})
		}
	}

	if upstream.Mergeability != nil {
		mergeability := Mergeability{
			Mergeable:  upstream.Mergeability.Mergeable,
			Outcome:    upstream.Mergeability.Outcome,
			Conflicted: upstream.Mergeability.Conflicted,
		}
		if len(upstream.Mergeability.Blockers) > 0 {
			mergeability.Blockers = make([]MergeBlocker, 0, len(upstream.Mergeability.Blockers))
			for _, blocker := range upstream.Mergeability.Blockers {
				mergeability.Blockers = append(mergeability.Blockers, MergeBlocker{Summary: blocker.Summary, Detail: blocker.Detail})
			}
		}
		converted.Mergeability = &mergeability
	}

	return converted
}

// PullRequestsFrom converts a list, preserving order and never returning nil.
func PullRequestsFrom(upstream []pullrequestservice.PullRequest) []PullRequest {
	converted := make([]PullRequest, 0, len(upstream))
	for _, one := range upstream {
		converted = append(converted, PullRequestFrom(one))
	}

	return converted
}

// PullRequestStates, ReviewerRoles and ReviewerStatuses are the closed sets
// Bitbucket uses, for declaring enums on a payload that carries them.
var (
	PullRequestStates = []string{"OPEN", "MERGED", "DECLINED"}
	ReviewerRoles     = []string{"AUTHOR", "REVIEWER", "PARTICIPANT"}
	ReviewerStatuses  = []string{"UNAPPROVED", "NEEDS_WORK", "APPROVED"}
	MergeOutcomes     = []string{"CLEAN", "CONFLICTED", "UNKNOWN"}
)
