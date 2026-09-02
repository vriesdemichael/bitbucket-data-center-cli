package result

import (
	"encoding/json"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/safederef"

	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// RequiredBuildCheck is one required-builds merge check.
//
// Shared by `bb build required` and `bb repo settings pull-requests
// merge-checks list`: the same Bitbucket object through two endpoints, which
// published two different descriptions of it.
type RequiredBuildCheck struct {
	ID                     int64      `json:"id,omitempty" jsonschema:"Check identifier, which update and delete address."`
	BuildParentKeys        []string   `json:"buildParentKeys,omitempty" jsonschema:"Build keys that must be green before a pull request can merge."`
	RefMatcher             RefMatcher `json:"refMatcher,omitzero" jsonschema:"Which target branches the check is enforced on."`
	ExemptRefMatcher       RefMatcher `json:"exemptRefMatcher,omitzero" jsonschema:"Which source branches are exempt from the check."`
	RequiredForPullRequest bool       `json:"requiredForPullRequest" jsonschema:"Whether the check is enforced on pull requests."`
	RequiredForMergeQueue  bool       `json:"requiredForMergeQueue" jsonschema:"Whether the check is enforced on merge-queue merges."`
}

// RequiredBuildCheckFrom converts one upstream required build condition.
func RequiredBuildCheckFrom(upstream openapigenerated.RestRequiredBuildCondition) RequiredBuildCheck {
	converted := RequiredBuildCheck{}
	if upstream.Id != nil {
		converted.ID = *upstream.Id
	}
	if upstream.BuildParentKeys != nil {
		converted.BuildParentKeys = *upstream.BuildParentKeys
	}
	if upstream.RefMatcher != nil {
		converted.RefMatcher = RefMatcher{
			ID:        safederef.String(upstream.RefMatcher.Id),
			DisplayID: safederef.String(upstream.RefMatcher.DisplayId),
		}
		if upstream.RefMatcher.Type != nil {
			converted.RefMatcher.Type = string(upstream.RefMatcher.Type.Id)
		}
	}
	if upstream.ExemptRefMatcher != nil {
		converted.ExemptRefMatcher = RefMatcher{
			ID:        safederef.String(upstream.ExemptRefMatcher.Id),
			DisplayID: safederef.String(upstream.ExemptRefMatcher.DisplayId),
		}
		if upstream.ExemptRefMatcher.Type != nil {
			converted.ExemptRefMatcher.Type = string(upstream.ExemptRefMatcher.Type.Id)
		}
	}
	if upstream.RequiredForPullRequest != nil {
		converted.RequiredForPullRequest = *upstream.RequiredForPullRequest
	}
	if upstream.RequiredForMergeQueue != nil {
		converted.RequiredForMergeQueue = *upstream.RequiredForMergeQueue
	}

	return converted
}

// RequiredBuildChecksFrom converts a list, preserving order and never returning
// nil.
func RequiredBuildChecksFrom(upstream []openapigenerated.RestRequiredBuildCondition) []RequiredBuildCheck {
	converted := make([]RequiredBuildCheck, 0, len(upstream))
	for _, one := range upstream {
		converted = append(converted, RequiredBuildCheckFrom(one))
	}

	return converted
}

// RequiredBuildCheckFromMap decodes the untyped object some endpoints return.
//
// The create and update calls hand back a map rather than a typed value, so a
// round trip through JSON is the only way to reach the fields.
func RequiredBuildCheckFromMap(payload map[string]any) RequiredBuildCheck {
	if len(payload) == 0 {
		return RequiredBuildCheck{}
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return RequiredBuildCheck{}
	}

	var upstream openapigenerated.RestRequiredBuildCondition
	if err := json.Unmarshal(raw, &upstream); err != nil {
		return RequiredBuildCheck{}
	}

	return RequiredBuildCheckFrom(upstream)
}

// RequiredBuildChecksFromAny decodes a listing that arrives untyped.
//
// Bitbucket answers this endpoint two ways depending on version: a bare array,
// or a paginated object with the checks under values. Both are handled, which
// the human renderer already did by printing whichever it found -- and the JSON
// path did not, because it published whatever arrived.
// Each element is decoded on its own, so one entry Bitbucket sent in an
// unexpected shape costs that entry rather than the whole list. Decoding the
// array in one go made any single surprise indistinguishable from "there are
// none", which is the answer the schema says an empty list means.
func RequiredBuildChecksFromAny(payload any) []RequiredBuildCheck {
	converted := []RequiredBuildCheck{}

	var values []any
	switch typed := payload.(type) {
	case []any:
		values = typed
	case map[string]any:
		if paginated, ok := typed["values"].([]any); ok {
			values = paginated
		}
	}

	for _, value := range values {
		object, ok := value.(map[string]any)
		if !ok {
			continue
		}
		converted = append(converted, RequiredBuildCheckFromMap(object))
	}

	return converted
}
