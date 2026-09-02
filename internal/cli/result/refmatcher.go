package result

// RefMatcher is which refs a rule applies to.
//
// Bitbucket nests the matcher kind as an object holding an id and a name that
// always agree -- {"id": "BRANCH", "name": "Branch"}. It is flattened to the id,
// which is the value a caller matches on, because publishing an object whose
// two fields are the same fact twice makes a consumer choose between them.
//
// Shared across the rules that use one: default reviewer conditions, required
// build checks and branch restrictions. Each had its own copy, which is how
// three descriptions of one Bitbucket object come to disagree.
type RefMatcher struct {
	ID        string `json:"id,omitempty" jsonschema:"Matcher value: a branch name, a pattern, or a model branch id depending on type."`
	DisplayID string `json:"displayId,omitempty" jsonschema:"Human-readable form of the same thing."`
	Type      string `json:"type,omitempty" jsonschema:"ANY_REF, BRANCH, PATTERN, MODEL_BRANCH or MODEL_CATEGORY, which decides how id is read."`
}

// RefMatcherTypes is the closed set Bitbucket uses.
//
// Exported because the enum path differs per payload -- the field sits at a
// different depth in each -- so every caller passes it under its own path.
var RefMatcherTypes = []string{"ANY_REF", "BRANCH", "PATTERN", "MODEL_BRANCH", "MODEL_CATEGORY"}
