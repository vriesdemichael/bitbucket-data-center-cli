package reviewercmd

import (
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
)

// Conditions is what `bb reviewer condition list` returns.
type Conditions struct {
	Conditions []result.Condition `json:"conditions" jsonschema:"Default-reviewer conditions in scope. Empty rather than absent when there are none."`
}

// ConditionDeletion is what `bb reviewer condition delete` reports.
type ConditionDeletion struct {
	result.Status
	ID string `json:"id" jsonschema:"Identifier of the condition that was deleted."`
}

func init() {
	singleEnums := map[string][]string{
		"sourceRefMatcher.type": result.RefMatcherTypes,
		"targetRefMatcher.type": result.RefMatcherTypes,
		"scope":                 result.ConditionScopes,
	}

	result.Declare("reviewer condition list", result.For[Conditions](map[string][]string{
		"conditions.sourceRefMatcher.type": result.RefMatcherTypes,
		"conditions.targetRefMatcher.type": result.RefMatcherTypes,
		"conditions.scope":                 result.ConditionScopes,
	}))
	result.Declare("reviewer condition create", result.For[result.Condition](singleEnums))
	result.Declare("reviewer condition update", result.For[result.Condition](singleEnums))
	result.Declare("reviewer condition delete", result.For[ConditionDeletion](nil))
}
