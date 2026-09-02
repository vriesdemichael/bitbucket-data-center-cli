package result

import (
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

// User is a Bitbucket account.
//
// Bitbucket's generated client has two spellings of the same account --
// ApplicationUser and RestApplicationUser -- and which one a caller saw
// depended on which endpoint answered. Both converge here so a user is one
// shape wherever bb reports one.
//
// links and avatarUrl are dropped: they are navigation for the Bitbucket web
// UI, not facts about the account, and publishing them would commit bb to a
// URL shape it does not control.
type User struct {
	ID           int32  `json:"id,omitempty" jsonschema:"User identifier."`
	Name         string `json:"name,omitempty" jsonschema:"Username."`
	DisplayName  string `json:"displayName,omitempty" jsonschema:"Human-readable name."`
	EmailAddress string `json:"emailAddress,omitempty" jsonschema:"Email address, when the instance exposes it."`
	Slug         string `json:"slug,omitempty" jsonschema:"URL-safe form of the username."`
	Type         string `json:"type,omitempty" jsonschema:"Account kind: NORMAL for a person, SERVICE for an integration account."`
	Active       bool   `json:"active" jsonschema:"Whether the account is enabled. An inactive user still counts as configured but cannot act."`
}

// UserFrom converts the nested account type.
func UserFrom(upstream openapigenerated.ApplicationUser) User {
	converted := User{
		Name:         stringValue(upstream.Name),
		DisplayName:  stringValue(upstream.DisplayName),
		EmailAddress: stringValue(upstream.EmailAddress),
		Slug:         stringValue(upstream.Slug),
	}
	if upstream.Id != nil {
		converted.ID = *upstream.Id
	}
	if upstream.Type != nil {
		converted.Type = string(*upstream.Type)
	}
	if upstream.Active != nil {
		converted.Active = *upstream.Active
	}

	return converted
}

// UsersFrom converts a list, preserving order and never returning nil.
func UsersFrom(upstream []openapigenerated.ApplicationUser) []User {
	converted := make([]User, 0, len(upstream))
	for _, one := range upstream {
		converted = append(converted, UserFrom(one))
	}

	return converted
}

// RestUserFrom converts the top-level account type.
func RestUserFrom(upstream openapigenerated.RestApplicationUser) User {
	converted := User{
		Name:         stringValue(upstream.Name),
		DisplayName:  stringValue(upstream.DisplayName),
		EmailAddress: stringValue(upstream.EmailAddress),
		Slug:         stringValue(upstream.Slug),
	}
	if upstream.Id != nil {
		converted.ID = *upstream.Id
	}
	if upstream.Type != nil {
		converted.Type = string(*upstream.Type)
	}
	if upstream.Active != nil {
		converted.Active = *upstream.Active
	}

	return converted
}

// RestUsersFrom converts a list, preserving order and never returning nil.
func RestUsersFrom(upstream []openapigenerated.RestApplicationUser) []User {
	converted := make([]User, 0, len(upstream))
	for _, one := range upstream {
		converted = append(converted, RestUserFrom(one))
	}

	return converted
}
