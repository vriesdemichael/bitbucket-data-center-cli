package reviewergroupcmd

import (
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/result"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

// User is a member of a reviewer group.
type User struct {
	ID           int32  `json:"id,omitempty" jsonschema:"User identifier."`
	Name         string `json:"name,omitempty" jsonschema:"Username."`
	DisplayName  string `json:"displayName,omitempty" jsonschema:"Human-readable name."`
	EmailAddress string `json:"emailAddress,omitempty" jsonschema:"Email address, when the instance exposes it."`
	Slug         string `json:"slug,omitempty" jsonschema:"URL-safe form of the username."`
	Active       bool   `json:"active" jsonschema:"Whether the account is enabled. An inactive member still counts as configured but cannot review."`
}

// Group is one reviewer group.
type Group struct {
	ID          int64  `json:"id,omitempty" jsonschema:"Group identifier, for bb reviewer-group update, delete and users."`
	Name        string `json:"name,omitempty" jsonschema:"Group name."`
	Description string `json:"description,omitempty" jsonschema:"Description, when one was given."`
	AvatarURL   string `json:"avatarUrl,omitempty" jsonschema:"Group avatar, when one is configured."`
	Scope       string `json:"scope,omitempty" jsonschema:"PROJECT when the group is defined on the project, REPOSITORY when on the repository itself."`
	Users       []User `json:"users,omitempty" jsonschema:"Members, when the endpoint returned them. Absent is not the same as an empty group: bb reviewer-group users answers that question directly."`
}

// Groups is what `bb reviewer-group list` returns.
type Groups struct {
	ReviewerGroups []Group `json:"reviewerGroups" jsonschema:"Reviewer groups in scope. Empty rather than absent when there are none."`
}

// Users is what `bb reviewer-group users` returns.
type Users struct {
	Users []User `json:"users" jsonschema:"Members of the group. Empty rather than absent when the group has none."`
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
		Name:        safeString(upstream.Name),
		Description: safeString(upstream.Description),
		AvatarURL:   safeString(upstream.AvatarUrl),
	}
	if upstream.Id != nil {
		converted.ID = *upstream.Id
	}
	if upstream.Scope != nil {
		converted.Scope = string(upstream.Scope.Type)
	}
	if upstream.Users != nil {
		converted.Users = usersFrom(*upstream.Users)
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

// userFrom converts one upstream user.
func userFrom(upstream openapigenerated.ApplicationUser) User {
	converted := User{
		Name:         safeString(upstream.Name),
		DisplayName:  safeString(upstream.DisplayName),
		EmailAddress: safeString(upstream.EmailAddress),
		Slug:         safeString(upstream.Slug),
	}
	if upstream.Id != nil {
		converted.ID = *upstream.Id
	}
	if upstream.Active != nil {
		converted.Active = *upstream.Active
	}

	return converted
}

// usersFrom converts a list, preserving order and never returning nil.
func usersFrom(upstream []openapigenerated.ApplicationUser) []User {
	converted := make([]User, 0, len(upstream))
	for _, one := range upstream {
		converted = append(converted, userFrom(one))
	}

	return converted
}

func safeString(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}

// restUsersFrom converts the other upstream user type.
//
// Bitbucket's generated client has two: ApplicationUser, nested inside a
// reviewer group, and RestApplicationUser, returned by the group members
// endpoint. They carry the same fields for the ones bb reports, so both
// converge here rather than the difference reaching the payload -- a caller
// asking for a group's members and a caller reading the members inside a group
// should not get two shapes.
func restUsersFrom(upstream []openapigenerated.RestApplicationUser) []User {
	converted := make([]User, 0, len(upstream))
	for _, one := range upstream {
		user := User{
			Name:         safeString(one.Name),
			DisplayName:  safeString(one.DisplayName),
			EmailAddress: safeString(one.EmailAddress),
			Slug:         safeString(one.Slug),
		}
		if one.Id != nil {
			user.ID = *one.Id
		}
		if one.Active != nil {
			user.Active = *one.Active
		}
		converted = append(converted, user)
	}

	return converted
}
