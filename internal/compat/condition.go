package compat

// ConditionReviewerGroups is a default reviewer condition naming reviewer groups.
//
// It arrived in 9.5. An earlier release answers 200 to reviewerGroups and drops
// them, so a condition naming only groups is stored with nobody to add
// (observed on 9.2.1 and 9.4.24; 9.5.2 keeps them).
var ConditionReviewerGroups = Difference{
	What:  "a default reviewer condition naming reviewer groups",
	Since: Release{Major: 9, Minor: 5},
}

// ConditionReviewerChecks is how a default reviewer condition's reviewers are
// checked.
//
// From 9.5 a condition names reviewers or reviewer groups, and a reviewer named
// without an id is answered with a 404 "User with ID -1 does not exist". An
// earlier release stores a condition with no reviewers when it requires an
// approval, refuses one that requires none with "Required approvals must be > 0
// if no reviewers are provided", and refuses a reviewer without an id with a
// 400 "No user exists for identifier -1" (observed on 9.3.2 and 9.4.24; 9.5.2
// checks as 10.5.0 does). bb's dry run predicts what the release does.
var ConditionReviewerChecks = Difference{
	What:  "a default reviewer condition checked for reviewers or reviewer groups",
	Since: Release{Major: 9, Minor: 5},
}

// ConditionCheckedDifferently reports whether a condition body is one a release
// before ConditionReviewerChecks answers differently: it names no reviewers,
// or names one without an id. A body naming reviewers by id is checked the same
// way on every release.
func ConditionCheckedDifferently(body map[string]any) bool {
	reviewers, _ := body["reviewers"].([]any)
	if len(reviewers) == 0 {
		return true
	}
	for _, entry := range reviewers {
		reviewer, _ := entry.(map[string]any)
		if reviewer["id"] == nil {
			return true
		}
	}

	return false
}
