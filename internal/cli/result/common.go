package result

import (
	repositoryservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/repository"
)

// Repository names a repository in a payload.
//
// It exists because 49 payloads embedded a service's RepositoryRef directly,
// and those structs carry no JSON tags -- so bb published
// {"repository":{"ProjectKey":"PRJ","Slug":"payments"}}, Go field names and
// all. That is what happens when the payload is whatever struct was in scope
// rather than something chosen: the internal type becomes the contract without
// anyone deciding it should.
//
// The names follow ADR-076: camelCase, matching what the Bitbucket API calls
// the same things, so a reader comparing bb output against the Bitbucket API
// docs sees the same words.
type Repository struct {
	ProjectKey string `json:"projectKey" jsonschema:"Project key the repository belongs to."`
	Slug       string `json:"slug" jsonschema:"Repository slug."`
}

// Status reports the outcome of a command whose result is the fact that it
// worked.
//
// Embedded rather than used alone, so a command can add what it acted on:
//
//	type Deletion struct {
//	    result.Status
//	    Tag string `json:"tag"`
//	}
//
// The value is "ok" across the surface today, with a handful of commands
// saying "deleted" instead. Both are kept rather than unified here, because
// changing what a command reports is a contract change and this type exists to
// stop shapes drifting, not to quietly re-spell them.
type Status struct {
	Status string `json:"status" jsonschema:"Outcome of the command. A failure reports an error envelope instead of this payload."`
}

// OK is the common case: the command did what it was asked.
func OK() Status {
	return Status{Status: "ok"}
}

// RepositorySummary is a repository in a listing: the reference plus what a
// reader needs to pick one out.
//
// Distinct from Repository, which names a repository a payload is about. A
// listing needs the display name and visibility; a payload that is scoped to a
// repository does not, and carrying them there would publish two fields per
// command that nothing reads.
type RepositorySummary struct {
	ProjectKey string `json:"projectKey" jsonschema:"Project key the repository belongs to."`
	Slug       string `json:"slug" jsonschema:"Repository slug, as it appears in URLs."`
	Name       string `json:"name,omitempty" jsonschema:"Display name, which may differ from the slug."`
	Public     bool   `json:"public" jsonschema:"Whether the repository is readable without authentication."`
}

// RepositorySummariesFrom converts a service listing, preserving order and
// never returning nil.
func RepositorySummariesFrom(upstream []repositoryservice.Repository) []RepositorySummary {
	converted := make([]RepositorySummary, 0, len(upstream))
	for _, one := range upstream {
		converted = append(converted, RepositorySummary{
			ProjectKey: one.ProjectKey,
			Slug:       one.Slug,
			Name:       one.Name,
			Public:     one.Public,
		})
	}

	return converted
}
