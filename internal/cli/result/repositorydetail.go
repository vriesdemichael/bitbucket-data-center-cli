package result

import (
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

// RepositoryDetail is a repository as the administrative commands report it.
//
// Distinct from Repository, which names a repository a payload is about, and
// from RepositorySummary, which is a row in a listing. This is what you get
// back when you create, fork or update one: the settings that were applied.
//
// links and relatedLinks are dropped -- navigation for the Bitbucket web UI --
// and so is the fork's origin repository, which nests its own project and, for
// a fork of a fork, its own origin again. What a caller needs from the origin
// is which repository it is, so origin carries just that.
type RepositoryDetail struct {
	ID            int32       `json:"id,omitempty" jsonschema:"Repository identifier."`
	ProjectKey    string      `json:"projectKey,omitempty" jsonschema:"Project the repository belongs to."`
	Slug          string      `json:"slug,omitempty" jsonschema:"Repository slug, as it appears in URLs."`
	Name          string      `json:"name,omitempty" jsonschema:"Display name, which may differ from the slug."`
	Description   string      `json:"description,omitempty" jsonschema:"Description, when one was given."`
	DefaultBranch string      `json:"defaultBranch,omitempty" jsonschema:"Branch a clone checks out, when the instance reports it."`
	Public        bool        `json:"public" jsonschema:"Whether the repository is readable without authentication."`
	Forkable      bool        `json:"forkable" jsonschema:"Whether users may fork it."`
	Archived      bool        `json:"archived" jsonschema:"Whether it is archived and therefore read-only."`
	ScmID         string      `json:"scmId,omitempty" jsonschema:"Source control system. git on every supported instance."`
	State         string      `json:"state,omitempty" jsonschema:"AVAILABLE, INITIALISING or INITIALISATION_FAILED."`
	StatusMessage string      `json:"statusMessage,omitempty" jsonschema:"What the state means, when Bitbucket explains it."`
	Origin        *Repository `json:"origin,omitempty" jsonschema:"Repository this one was forked from. Absent when it is not a fork."`
}

// RepositoryStates is the closed set Bitbucket uses.
var RepositoryStates = []string{"AVAILABLE", "INITIALISING", "INITIALISATION_FAILED"}

// RepositoryDetailFrom converts one upstream repository.
func RepositoryDetailFrom(upstream openapigenerated.RestRepository) RepositoryDetail {
	converted := RepositoryDetail{
		Slug:          stringValue(upstream.Slug),
		Name:          stringValue(upstream.Name),
		Description:   stringValue(upstream.Description),
		DefaultBranch: stringValue(upstream.DefaultBranch),
		ScmID:         stringValue(upstream.ScmId),
		StatusMessage: stringValue(upstream.StatusMessage),
	}
	if upstream.Id != nil {
		converted.ID = *upstream.Id
	}
	if upstream.Project != nil {
		converted.ProjectKey = upstream.Project.Key
	}
	if upstream.Public != nil {
		converted.Public = *upstream.Public
	}
	if upstream.Forkable != nil {
		converted.Forkable = *upstream.Forkable
	}
	if upstream.Archived != nil {
		converted.Archived = *upstream.Archived
	}
	if upstream.State != nil {
		converted.State = string(*upstream.State)
	}
	if upstream.Origin != nil {
		origin := Repository{Slug: stringValue(upstream.Origin.Slug)}
		if upstream.Origin.Project != nil {
			origin.ProjectKey = upstream.Origin.Project.Key
		}
		converted.Origin = &origin
	}

	return converted
}
