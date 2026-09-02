package reviewergroupcmd

import (
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/safederef"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// Group is one reviewer group.
type Group struct {
	ID          int64         `json:"id,omitempty" jsonschema:"Group identifier, for bb reviewer-group update, delete and users."`
	Name        string        `json:"name,omitempty" jsonschema:"Group name."`
	Description string        `json:"description,omitempty" jsonschema:"Description, when one was given."`
	AvatarURL   string        `json:"avatarUrl,omitempty" jsonschema:"Group avatar, when one is configured."`
	Scope       string        `json:"scope,omitempty" jsonschema:"PROJECT when the group is defined on the project, REPOSITORY when on the repository itself."`
	Users       []result.User `json:"users,omitempty" jsonschema:"Members, when the endpoint returned them. Absent is not the same as an empty group: bb reviewer-group users answers that question directly."`
}

// Groups is what `bb reviewer-group list` returns.
type Groups struct {
	ReviewerGroups []Group `json:"reviewerGroups" jsonschema:"Reviewer groups in scope. Empty rather than absent when there are none."`
}

// Users is what `bb reviewer-group users` returns.
type Users struct {
	Users []result.User `json:"users" jsonschema:"Members of the group. Empty rather than absent when the group has none."`
}

// Deletion is what `bb reviewer-group delete` reports.
type Deletion struct {
	result.Status
	ID string `json:"id" jsonschema:"Identifier of the group that was deleted."`
}

var groupScopes = []string{"PROJECT", "REPOSITORY"}

func init() {
	result.Declare("reviewer-group list", result.For[Groups](map[string][]string{"reviewerGroups.scope": groupScopes}))
	result.Declare("reviewer-group create", result.For[Group](map[string][]string{"scope": groupScopes}))
	result.Declare("reviewer-group update", result.For[Group](map[string][]string{"scope": groupScopes}))
	result.Declare("reviewer-group users", result.For[Users](nil))
	result.Declare("reviewer-group delete", result.For[Deletion](nil))
}

// groupFrom converts one upstream reviewer group.
func groupFrom(upstream openapigenerated.RestReviewerGroup) Group {
	converted := Group{
		Name:        safederef.String(upstream.Name),
		Description: safederef.String(upstream.Description),
		AvatarURL:   safederef.String(upstream.AvatarUrl),
	}
	if upstream.Id != nil {
		converted.ID = *upstream.Id
	}
	if upstream.Scope != nil {
		converted.Scope = string(upstream.Scope.Type)
	}
	if upstream.Users != nil {
		converted.Users = result.UsersFrom(*upstream.Users)
	}

	return converted
}

// groupsFrom converts a list, preserving order and never returning nil.
func groupsFrom(upstream []openapigenerated.RestReviewerGroup) []Group {
	converted := make([]Group, 0, len(upstream))
	for _, one := range upstream {
		converted = append(converted, groupFrom(one))
	}

	return converted
}
