package result

import (
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

// AccessKey is an SSH key granted an exemption from a branch restriction.
//
// Only the key's identity is published. The upstream object nests the whole
// project and repository the key is scoped to -- and, for a fork, that
// repository's origin and its project again -- none of which says anything
// about the restriction being described.
type AccessKey struct {
	ID          int32  `json:"id,omitempty" jsonschema:"Access key identifier."`
	Label       string `json:"label,omitempty" jsonschema:"Label the key was registered under."`
	Fingerprint string `json:"fingerprint,omitempty" jsonschema:"Fingerprint of the public key."`
	Permission  string `json:"permission,omitempty" jsonschema:"Access level the key is granted."`
}

// Restriction is one branch restriction.
//
// Shared by the repository-scoped `bb branch restriction` commands and the
// project-scoped `bb project branch-restriction` ones. They read the same
// Bitbucket object and both used to publish it raw, so the shape was whatever
// the generated client happened to hold.
type Restriction struct {
	ID         int32       `json:"id,omitempty" jsonschema:"Restriction identifier, which get, update and delete address."`
	Type       string      `json:"type,omitempty" jsonschema:"What the restriction forbids: read-only, no-deletes, fast-forward-only, pull-request-only or no-creates."`
	Matcher    RefMatcher  `json:"matcher,omitzero" jsonschema:"Which refs the restriction applies to."`
	Users      []User      `json:"users,omitempty" jsonschema:"Users exempt from the restriction."`
	Groups     []string    `json:"groups,omitempty" jsonschema:"Groups exempt from the restriction."`
	AccessKeys []AccessKey `json:"accessKeys,omitempty" jsonschema:"SSH access keys exempt from the restriction."`
	Scope      string      `json:"scope,omitempty" jsonschema:"PROJECT when the restriction is set on the project, REPOSITORY when on the repository itself."`
}

// RestrictionTypes is the closed set of things a restriction can forbid.
var RestrictionTypes = []string{"read-only", "no-deletes", "fast-forward-only", "pull-request-only", "no-creates"}

// RestrictionScopes is where a restriction was defined.
var RestrictionScopes = []string{"PROJECT", "REPOSITORY"}

// RestrictionFrom converts one upstream branch restriction.
func RestrictionFrom(upstream openapigenerated.RestRefRestriction) Restriction {
	converted := Restriction{Type: stringValue(upstream.Type)}
	if upstream.Id != nil {
		converted.ID = *upstream.Id
	}
	if upstream.Groups != nil {
		converted.Groups = *upstream.Groups
	}
	if upstream.Matcher != nil {
		converted.Matcher = RefMatcher{
			ID:        stringValue(upstream.Matcher.Id),
			DisplayID: stringValue(upstream.Matcher.DisplayId),
		}
		if upstream.Matcher.Type != nil {
			converted.Matcher.Type = string(upstream.Matcher.Type.Id)
		}
	}
	if upstream.Scope != nil {
		converted.Scope = string(upstream.Scope.Type)
	}
	if upstream.Users != nil {
		converted.Users = RestUsersFrom(*upstream.Users)
	}
	if upstream.AccessKeys != nil {
		converted.AccessKeys = make([]AccessKey, 0, len(*upstream.AccessKeys))
		for _, key := range *upstream.AccessKeys {
			entry := AccessKey{}
			if key.Key != nil {
				if key.Key.Id != nil {
					entry.ID = *key.Key.Id
				}
				entry.Label = stringValue(key.Key.Label)
				entry.Fingerprint = stringValue(key.Key.Fingerprint)
			}
			if key.Permission != nil {
				entry.Permission = string(*key.Permission)
			}
			converted.AccessKeys = append(converted.AccessKeys, entry)
		}
	}

	return converted
}

// RestrictionsFrom converts a list, preserving order and never returning nil.
func RestrictionsFrom(upstream []openapigenerated.RestRefRestriction) []Restriction {
	converted := make([]Restriction, 0, len(upstream))
	for _, one := range upstream {
		converted = append(converted, RestrictionFrom(one))
	}

	return converted
}
