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
