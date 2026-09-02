package openapi

import (
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// The closed input sets that more than one command package needs.
//
// A set used by one package stays there (build.buildStates,
// deployment.deploymentStates), and where an output enum already names the
// same closed set the flag uses that one instead -- result.RestrictionTypes
// is both what --type accepts and what comes back on the restriction, which
// is the strongest form of the property #482 asks for.
//
// These three are here because they were about to be written twice: branch and
// project both take a matcher type, pr and repo both anchor a comment to a
// line, and pr and search both filter by pull request state.
var (
	// RestrictionMatcherTypes are what a branch restriction can be matched
	// against.
	//
	// Narrower than result.RefMatcherTypes, which also carries ANY_REF: that
	// value appears on default reviewer and task matchers, and the restriction
	// endpoints do not accept it. The two sets are genuinely different, so
	// this one comes from the generated request parameter rather than from the
	// output enum.
	RestrictionMatcherTypes = []string{
		string(openapigenerated.GetRestrictions1ParamsMatcherTypeBRANCH),
		string(openapigenerated.GetRestrictions1ParamsMatcherTypeMODELBRANCH),
		string(openapigenerated.GetRestrictions1ParamsMatcherTypeMODELCATEGORY),
		string(openapigenerated.GetRestrictions1ParamsMatcherTypePATTERN),
	}

	// RefOrderings are how a branch or tag listing can be sorted.
	RefOrderings = []string{
		string(openapigenerated.ALPHABETICAL),
		string(openapigenerated.MODIFICATION),
	}

	// DiffLineTypes are the sides of a diff an inline comment can anchor to.
	// commentanchor.NormalizeLineType owns the same set for the service layer
	// and still runs; this is the set the flags advertise.
	DiffLineTypes = []string{"ADDED", "REMOVED", "CONTEXT"}

	// PullRequestStateFilters are the values --state takes when listing or
	// searching pull requests. "all" is not a Bitbucket state -- it is the CLI
	// asking for both -- so this set has no generated counterpart.
	PullRequestStateFilters = []string{"open", "closed", "all"}
)
