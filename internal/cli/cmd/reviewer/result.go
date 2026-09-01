package reviewercmd

import (
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/result"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

// RefMatcher is which refs a condition applies to.
//
// The upstream object nests the matcher kind as an object with an id and a
// name that always agree. It is flattened to the id, which is the value a
// caller matches on, with the display text kept as the label a human reads.
type RefMatcher struct {
	ID        string `json:"id,omitempty" jsonschema:"Matcher value: a branch name, a pattern, or a model branch id depending on type."`
	DisplayID string `json:"displayId,omitempty" jsonschema:"Human-readable form of the same thing."`
	Type      string `json:"type,omitempty" jsonschema:"ANY_REF, BRANCH, PATTERN or MODEL_BRANCH, which decides how id is read."`
}

// Participant is a user or a group named as a default reviewer.
//
// Bitbucket returns both through the same object, and a condition carries them
// in two separate lists, so the shape is shared rather than split.
type Participant struct {
	ID          int64  `json:"id,omitempty" jsonschema:"Identifier, unique among users or among groups."`
	Name        string `json:"name,omitempty" jsonschema:"Username for a user, group name for a group."`
	DisplayName string `json:"displayName,omitempty" jsonschema:"Human-readable name, when the instance has one."`
}

// Condition is one default-reviewer condition.
type Condition struct {
	ID                int64         `json:"id,omitempty" jsonschema:"Condition identifier, for bb reviewer condition update and delete."`
	RequiredApprovals int32         `json:"requiredApprovals" jsonschema:"How many of the named reviewers must approve before the pull request can merge."`
	SourceRefMatcher  RefMatcher    `json:"sourceRefMatcher,omitzero" jsonschema:"Which source branches the condition applies to."`
	TargetRefMatcher  RefMatcher    `json:"targetRefMatcher,omitzero" jsonschema:"Which target branches the condition applies to."`
	Reviewers         []Participant `json:"reviewers,omitempty" jsonschema:"Individual users added as default reviewers."`
	ReviewerGroups    []Participant `json:"reviewerGroups,omitempty" jsonschema:"Groups added as default reviewers."`
	Scope             string        `json:"scope,omitempty" jsonschema:"PROJECT when the condition is inherited from the project, REPOSITORY when it is set on the repository itself."`
}

// Conditions is what `bb reviewer condition list` returns.
type Conditions struct {
	Conditions []Condition `json:"conditions" jsonschema:"Default-reviewer conditions in scope. Empty rather than absent when there are none."`
}

// ConditionDeletion is what `bb reviewer condition delete` reports.
type ConditionDeletion struct {
	result.Status
	ID string `json:"id" jsonschema:"Identifier of the condition that was deleted."`
}

func init() {
	enums := map[string][]string{
		"conditions.sourceRefMatcher.type": refMatcherTypes,
		"conditions.targetRefMatcher.type": refMatcherTypes,
		"conditions.scope":                 conditionScopes,
	}

	result.Declare("reviewer condition list", result.For[Conditions](enums))
	result.Declare("reviewer condition create", result.For[Condition](map[string][]string{
		"sourceRefMatcher.type": refMatcherTypes,
		"targetRefMatcher.type": refMatcherTypes,
		"scope":                 conditionScopes,
	}))
	result.Declare("reviewer condition update", result.For[Condition](map[string][]string{
		"sourceRefMatcher.type": refMatcherTypes,
		"targetRefMatcher.type": refMatcherTypes,
		"scope":                 conditionScopes,
	}))
	result.Declare("reviewer condition delete", result.For[ConditionDeletion](nil))
}

var (
	refMatcherTypes = []string{"ANY_REF", "BRANCH", "PATTERN", "MODEL_BRANCH", "MODEL_CATEGORY"}
	conditionScopes = []string{"PROJECT", "REPOSITORY"}
)

// conditionFrom converts one upstream condition.
func conditionFrom(upstream openapigenerated.RestPullRequestCondition) Condition {
	converted := Condition{}

	if upstream.Id != nil {
		converted.ID = int64(*upstream.Id)
	}
	if upstream.RequiredApprovals != nil {
		converted.RequiredApprovals = *upstream.RequiredApprovals
	}
	if upstream.Scope != nil {
		converted.Scope = string(upstream.Scope.Type)
	}
	if upstream.SourceRefMatcher != nil {
		converted.SourceRefMatcher = RefMatcher{
			ID:        safeString(upstream.SourceRefMatcher.Id),
			DisplayID: safeString(upstream.SourceRefMatcher.DisplayId),
		}
		if upstream.SourceRefMatcher.Type != nil {
			converted.SourceRefMatcher.Type = string(upstream.SourceRefMatcher.Type.Id)
		}
	}
	if upstream.TargetRefMatcher != nil {
		converted.TargetRefMatcher = RefMatcher{
			ID:        safeString(upstream.TargetRefMatcher.Id),
			DisplayID: safeString(upstream.TargetRefMatcher.DisplayId),
		}
		if upstream.TargetRefMatcher.Type != nil {
			converted.TargetRefMatcher.Type = string(upstream.TargetRefMatcher.Type.Id)
		}
	}
	if upstream.Reviewers != nil {
		converted.Reviewers = participantsFrom(*upstream.Reviewers)
	}
	if upstream.ReviewerGroups != nil {
		converted.ReviewerGroups = participantsFrom(*upstream.ReviewerGroups)
	}

	return converted
}

// conditionsFrom converts a list, preserving order and never returning nil.
func conditionsFrom(upstream []openapigenerated.RestPullRequestCondition) []Condition {
	converted := make([]Condition, 0, len(upstream))
	for _, one := range upstream {
		converted = append(converted, conditionFrom(one))
	}

	return converted
}

// participantsFrom converts users or groups into the shared participant shape.
func participantsFrom(upstream []openapigenerated.RestReviewerGroup) []Participant {
	converted := make([]Participant, 0, len(upstream))
	for _, one := range upstream {
		participant := Participant{Name: safeString(one.Name)}
		if one.Id != nil {
			participant.ID = *one.Id
		}
		converted = append(converted, participant)
	}

	return converted
}

func safeString(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}
