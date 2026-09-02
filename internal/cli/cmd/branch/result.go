package branchcmd

import (
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
