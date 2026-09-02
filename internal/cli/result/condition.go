package result

import (
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// Participant is a user or a group named as a default reviewer.
//
// Bitbucket returns both through the same object, and a condition carries them
// in two separate lists, so the shape is shared rather than split.
type Participant struct {
	ID   int64  `json:"id,omitempty" jsonschema:"Identifier, unique among users or among groups."`
	Name string `json:"name,omitempty" jsonschema:"Username for a user, group name for a group."`
}

// There is no display name here on purpose. The endpoint types both lists as
// reviewer groups, which carry a name and no display name, so bb has nothing to
// put in the field -- and a schema that advertises one a caller will never
// receive is worse than not offering it.

// Condition is one default-reviewer condition.
//
// Shared by `bb reviewer condition` and `bb pr default-reviewers`: the two read
// the same Bitbucket object through different endpoints, and the second used to
// publish it raw while the first published a model of it.
type Condition struct {
	ID                int64         `json:"id,omitempty" jsonschema:"Condition identifier, which update and delete address."`
	RequiredApprovals int32         `json:"requiredApprovals" jsonschema:"How many of the named reviewers must approve before the pull request can merge."`
	SourceRefMatcher  RefMatcher    `json:"sourceRefMatcher,omitzero" jsonschema:"Which source branches the condition applies to."`
	TargetRefMatcher  RefMatcher    `json:"targetRefMatcher,omitzero" jsonschema:"Which target branches the condition applies to."`
	Reviewers         []Participant `json:"reviewers,omitempty" jsonschema:"Individual users added as default reviewers."`
	ReviewerGroups    []Participant `json:"reviewerGroups,omitempty" jsonschema:"Groups added as default reviewers."`
	Scope             string        `json:"scope,omitempty" jsonschema:"PROJECT when the condition is inherited from the project, REPOSITORY when it is set on the repository itself."`
}

// ConditionScopes is where a condition was defined.
var ConditionScopes = []string{"PROJECT", "REPOSITORY"}

// ConditionFrom converts one upstream condition.
func ConditionFrom(upstream openapigenerated.RestPullRequestCondition) Condition {
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
			ID:        stringValue(upstream.SourceRefMatcher.Id),
			DisplayID: stringValue(upstream.SourceRefMatcher.DisplayId),
		}
		if upstream.SourceRefMatcher.Type != nil {
			converted.SourceRefMatcher.Type = string(upstream.SourceRefMatcher.Type.Id)
		}
	}
	if upstream.TargetRefMatcher != nil {
		converted.TargetRefMatcher = RefMatcher{
			ID:        stringValue(upstream.TargetRefMatcher.Id),
			DisplayID: stringValue(upstream.TargetRefMatcher.DisplayId),
		}
		if upstream.TargetRefMatcher.Type != nil {
			converted.TargetRefMatcher.Type = string(upstream.TargetRefMatcher.Type.Id)
		}
	}
	if upstream.Reviewers != nil {
		converted.Reviewers = ParticipantsFrom(*upstream.Reviewers)
	}
	if upstream.ReviewerGroups != nil {
		converted.ReviewerGroups = ParticipantsFrom(*upstream.ReviewerGroups)
	}

	return converted
}

// ConditionsFrom converts a list, preserving order and never returning nil.
func ConditionsFrom(upstream []openapigenerated.RestPullRequestCondition) []Condition {
	converted := make([]Condition, 0, len(upstream))
	for _, one := range upstream {
		converted = append(converted, ConditionFrom(one))
	}

	return converted
}

// ParticipantsFrom converts users or groups into the shared participant shape.
func ParticipantsFrom(upstream []openapigenerated.RestReviewerGroup) []Participant {
	converted := make([]Participant, 0, len(upstream))
	for _, one := range upstream {
		participant := Participant{Name: stringValue(one.Name)}
		if one.Id != nil {
			participant.ID = *one.Id
		}
		converted = append(converted, participant)
	}

	return converted
}
