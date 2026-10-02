package result

// RefMatcher is which refs a rule applies to.
//
// Bitbucket nests the matcher kind as an object holding an id and a name that
// always agree -- {"id": "BRANCH", "name": "Branch"}. It is flattened to the id,
// which is the value a caller matches on, because publishing an object whose
// two fields are the same fact twice makes a consumer choose between them.
//
// Shared across the rules that use one: default reviewer conditions, default
// tasks, required build checks and branch restrictions. Each had its own copy,
// which is how several descriptions of one Bitbucket object come to disagree.
type RefMatcher struct {
	ID        string `json:"id,omitempty" jsonschema:"Matcher value: a branch name, a pattern, or a model branch id depending on type."`
	DisplayID string `json:"displayId,omitempty" jsonschema:"Human-readable form of the same thing."`
	Type      string `json:"type,omitempty" jsonschema:"ANY_REF, BRANCH, PATTERN, MODEL_BRANCH, MODEL_CATEGORY or DEFAULT_BRANCH, which decides how id is read."`
}

// RefMatcherTypes is the closed set Bitbucket uses. DEFAULT_BRANCH, which
// matches the repository's default branch, is in none of Bitbucket's REST
// documentation, and from 10.2 a required build or a default reviewer
// condition holds it.
//
// Exported because the enum path differs per payload -- the field sits at a
// different depth in each -- so every caller passes it under its own path.
var RefMatcherTypes = []string{"ANY_REF", "BRANCH", "PATTERN", "MODEL_BRANCH", "MODEL_CATEGORY", "DEFAULT_BRANCH"}
