package branchcmd

import (
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/result"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
	branchservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/branch"
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
	Branches   []Branch          `json:"branches" jsonschema:"Matching branches. Empty rather than absent when nothing matched."`
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

// AccessKey is an SSH key granted an exemption from a branch restriction.
//
// Only the key's identity is published. The upstream object nests the whole
// project and repository the key is scoped to -- several hundred fields deep in
// places -- and none of it says anything about the restriction being described.
type AccessKey struct {
	ID          int32  `json:"id,omitempty" jsonschema:"Access key identifier."`
	Label       string `json:"label,omitempty" jsonschema:"Label the key was registered under."`
	Fingerprint string `json:"fingerprint,omitempty" jsonschema:"Fingerprint of the public key."`
	Permission  string `json:"permission,omitempty" jsonschema:"Access level the key is granted on the repository."`
}

// RestrictionMatcher is which refs a restriction applies to.
//
// Flattened the same way as elsewhere: the upstream nests the matcher kind as
// an object holding an id and a name that always agree.
type RestrictionMatcher struct {
	ID        string `json:"id,omitempty" jsonschema:"Matcher value: a branch name, a pattern, or a model branch id depending on type."`
	DisplayID string `json:"displayId,omitempty" jsonschema:"Human-readable form of the same thing."`
	Type      string `json:"type,omitempty" jsonschema:"BRANCH, PATTERN, MODEL_BRANCH or MODEL_CATEGORY, which decides how id is read."`
}

// Restriction is one branch restriction.
type Restriction struct {
	ID         int32              `json:"id,omitempty" jsonschema:"Restriction identifier, for bb branch restriction get, update and delete."`
	Type       string             `json:"type,omitempty" jsonschema:"What the restriction forbids: read-only, no-deletes, fast-forward-only, pull-request-only or no-creates."`
	Matcher    RestrictionMatcher `json:"matcher,omitzero" jsonschema:"Which refs the restriction applies to."`
	Users      []result.User      `json:"users,omitempty" jsonschema:"Users exempt from the restriction."`
	Groups     []string           `json:"groups,omitempty" jsonschema:"Groups exempt from the restriction."`
	AccessKeys []AccessKey        `json:"accessKeys,omitempty" jsonschema:"SSH access keys exempt from the restriction."`
	Scope      string             `json:"scope,omitempty" jsonschema:"PROJECT when the restriction is inherited from the project, REPOSITORY when set on the repository itself."`
}

// Restrictions is what `bb branch restriction list` returns.
type Restrictions struct {
	Repository   result.Repository `json:"repository"`
	Restrictions []Restriction     `json:"restrictions" jsonschema:"Branch restrictions in scope. Empty rather than absent when there are none."`
}

// SingleRestriction is what `bb branch restriction get`, `create` and `update`
// return.
type SingleRestriction struct {
	Repository  result.Repository `json:"repository"`
	Restriction Restriction       `json:"restriction"`
}

// RestrictionDeletion is what `bb branch restriction delete` reports.
type RestrictionDeletion struct {
	result.Status
	Repository    result.Repository `json:"repository"`
	RestrictionID string            `json:"restrictionId" jsonschema:"Identifier of the restriction that was deleted, as it was given on the command line."`
}

var (
	restrictionTypes  = []string{"read-only", "no-deletes", "fast-forward-only", "pull-request-only", "no-creates"}
	matcherTypes      = []string{"BRANCH", "PATTERN", "MODEL_BRANCH", "MODEL_CATEGORY"}
	restrictionScopes = []string{"PROJECT", "REPOSITORY"}
)

func init() {
	result.Declare("branch list", result.For[Branches](nil))
	result.Declare("branch create", result.For[BranchCreation](nil))
	result.Declare("branch delete", result.For[BranchDeletion](nil))

	result.Declare("branch default get", result.For[DefaultBranch](map[string][]string{"defaultBranch.type": result.RefTypes}))
	result.Declare("branch default set", result.For[DefaultBranchChange](nil))

	result.Declare("branch model inspect", result.For[CommitRefs](map[string][]string{"refs.type": result.RefTypes}))
	result.Declare("branch model update", result.For[DefaultBranchChange](nil))

	listEnums := map[string][]string{
		"restrictions.type":         restrictionTypes,
		"restrictions.matcher.type": matcherTypes,
		"restrictions.scope":        restrictionScopes,
	}
	singleEnums := map[string][]string{
		"restriction.type":         restrictionTypes,
		"restriction.matcher.type": matcherTypes,
		"restriction.scope":        restrictionScopes,
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
		ID:           safeString(upstream.Id),
		DisplayID:    safeString(upstream.DisplayId),
		LatestCommit: safeString(upstream.LatestCommit),
	}
	if upstream.Default != nil {
		converted.Default = *upstream.Default
	}

	return converted
}

// branchesFrom converts a list, preserving order and never returning nil.
func branchesFrom(upstream []openapigenerated.RestBranch) []Branch {
	converted := make([]Branch, 0, len(upstream))
	for _, one := range upstream {
		converted = append(converted, branchFrom(one))
	}

	return converted
}

// restrictionFrom converts one upstream branch restriction.
func restrictionFrom(upstream openapigenerated.RestRefRestriction) Restriction {
	converted := Restriction{
		ID:     safeInt32(upstream.Id),
		Type:   safeString(upstream.Type),
		Groups: safeStringSlice(upstream.Groups),
	}
	if upstream.Matcher != nil {
		converted.Matcher = RestrictionMatcher{
			ID:        safeString(upstream.Matcher.Id),
			DisplayID: safeString(upstream.Matcher.DisplayId),
		}
		if upstream.Matcher.Type != nil {
			converted.Matcher.Type = string(upstream.Matcher.Type.Id)
		}
	}
	if upstream.Scope != nil {
		converted.Scope = string(upstream.Scope.Type)
	}
	if upstream.Users != nil {
		converted.Users = result.RestUsersFrom(*upstream.Users)
	}
	if upstream.AccessKeys != nil {
		converted.AccessKeys = make([]AccessKey, 0, len(*upstream.AccessKeys))
		for _, key := range *upstream.AccessKeys {
			entry := AccessKey{}
			if key.Key != nil {
				entry.ID = safeInt32(key.Key.Id)
				entry.Label = safeString(key.Key.Label)
				entry.Fingerprint = safeString(key.Key.Fingerprint)
			}
			if key.Permission != nil {
				entry.Permission = string(*key.Permission)
			}
			converted.AccessKeys = append(converted.AccessKeys, entry)
		}
	}

	return converted
}

// restrictionsFrom converts a list, preserving order and never returning nil.
func restrictionsFrom(upstream []openapigenerated.RestRefRestriction) []Restriction {
	converted := make([]Restriction, 0, len(upstream))
	for _, one := range upstream {
		converted = append(converted, restrictionFrom(one))
	}

	return converted
}
