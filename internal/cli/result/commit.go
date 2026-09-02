package result

import (
	"strings"

	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// Person is whoever authored or committed a change.
//
// Bitbucket's own spelling: name, emailAddress, avatarUrl. Kept exactly,
// per ADR-076, so a reader comparing bb output against the API documentation
// sees the same words.
type Person struct {
	Name         string `json:"name,omitempty" jsonschema:"Display name."`
	EmailAddress string `json:"emailAddress,omitempty" jsonschema:"Email address, when the instance exposes it."`
	AvatarURL    string `json:"avatarUrl,omitempty" jsonschema:"URL of the avatar image, when one is configured."`
}

// Commit is one commit, as bb reports it.
//
// Shared rather than per-package: `bb commit list`, `bb commit compare` and
// `bb search commits` all published {"repository": ..., "commits": ...}, the
// same shape written three times. Three copies of a shape are three chances
// for it to diverge, and nothing was holding them together.
//
// Parents are the commit ids only. The upstream object nests a full minimal
// commit per parent; a caller walking history wants the ids, and anything more
// is a second shape to keep right for a field nothing rendered.
type Commit struct {
	ID                 string   `json:"id,omitempty" jsonschema:"Full 40-character SHA1."`
	DisplayID          string   `json:"displayId,omitempty" jsonschema:"Abbreviated SHA1 as Bitbucket renders it."`
	Message            string   `json:"message,omitempty" jsonschema:"Full commit message, including the body."`
	Author             Person   `json:"author,omitzero" jsonschema:"Who wrote the change."`
	AuthorTimestamp    int64    `json:"authorTimestamp,omitempty" jsonschema:"When the change was written, in milliseconds since the epoch."`
	Committer          Person   `json:"committer,omitzero" jsonschema:"Who committed the change. Differs from the author for a rebase, a cherry-pick or a patch applied on someone's behalf."`
	CommitterTimestamp int64    `json:"committerTimestamp,omitempty" jsonschema:"When the change was committed, in milliseconds since the epoch."`
	Parents            []string `json:"parents,omitempty" jsonschema:"Commit ids of the parents. Two or more means a merge; none means the root commit."`
}

// Subject is the first line of the commit message.
//
// Rendered by every human listing, so it lives with the model rather than
// being re-split at each one.
func (commit Commit) Subject() string {
	return strings.SplitN(commit.Message, "\n", 2)[0]
}

// CommitFrom converts one upstream commit.
func CommitFrom(upstream openapigenerated.RestCommit) Commit {
	converted := Commit{
		ID:        stringValue(upstream.Id),
		DisplayID: stringValue(upstream.DisplayId),
		Message:   stringValue(upstream.Message),
	}

	if upstream.Author != nil {
		converted.Author = Person{
			Name:         upstream.Author.Name,
			EmailAddress: stringValue(upstream.Author.EmailAddress),
			AvatarURL:    stringValue(upstream.Author.AvatarUrl),
		}
	}
	if upstream.Committer != nil {
		converted.Committer = Person{
			Name:         upstream.Committer.Name,
			EmailAddress: stringValue(upstream.Committer.EmailAddress),
			AvatarURL:    stringValue(upstream.Committer.AvatarUrl),
		}
	}
	if upstream.AuthorTimestamp != nil {
		converted.AuthorTimestamp = *upstream.AuthorTimestamp
	}
	if upstream.CommitterTimestamp != nil {
		converted.CommitterTimestamp = *upstream.CommitterTimestamp
	}
	if upstream.Parents != nil {
		converted.Parents = make([]string, 0, len(*upstream.Parents))
		for _, parent := range *upstream.Parents {
			converted.Parents = append(converted.Parents, stringValue(parent.Id))
		}
	}

	return converted
}

// CommitsFrom converts a list, preserving order and never returning nil.
func CommitsFrom(upstream []openapigenerated.RestCommit) []Commit {
	converted := make([]Commit, 0, len(upstream))
	for _, one := range upstream {
		converted = append(converted, CommitFrom(one))
	}

	return converted
}

// stringValue dereferences an upstream optional string.
//
// The generated types make every field a pointer, so this appears wherever a
// model is built from one. It lives here rather than being redefined in each
// command package, which is where safeString already had 38 copies.
func stringValue(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}
