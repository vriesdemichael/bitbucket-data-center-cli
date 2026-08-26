package openapi

import (
	"strings"

	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

// Ref matcher types accepted by the default-task API. Anything outside this set
// is rejected with "Unable to get ref matcher for type <value>".
const (
	refMatcherAnyRef  = "ANY_REF"
	refMatcherBranch  = "BRANCH"
	refMatcherPattern = "PATTERN"
)

// anyRefMatcherID is the id that goes with the ANY_REF type. Bitbucket echoes it
// back as ANY_REF_MATCHER_ID, which reads like a type name but is not one:
// sending that value as the type is exactly what the API rejects.
const anyRefMatcherID = "ANY_REF"

// DefaultTaskSourceMatcher is the sourceMatcher field of a default-task request.
type DefaultTaskSourceMatcher = struct {
	DisplayId *string `json:"displayId,omitempty"`
	Id        *string `json:"id,omitempty"`
	Type      *struct {
		Id   openapigenerated.RestDefaultTaskRequestSourceMatcherTypeId `json:"id"`
		Name string                                                     `json:"name"`
	} `json:"type,omitempty"`
}

// DefaultTaskTargetMatcher is the targetMatcher field of a default-task request.
//
// Bitbucket 10.4 replaced the inline schema this used to be with a $ref to
// RestRefMatcher, while leaving the sourceMatcher side inline. The two are
// structurally identical; only the generated name differs.
type DefaultTaskTargetMatcher = openapigenerated.RestRefMatcher

// inferRefMatcher derives the matcher type from the ref itself, because the ref
// already determines it: a glob can only be a pattern and anything else can only
// be a branch. An empty ref means "any ref", which is also what an omitted flag
// has to become -- the API requires both matchers on every default task, so
// there is no way to leave one out.
func inferRefMatcher(ref *string) (id string, typeID string, typeName string) {
	trimmed := ""
	if ref != nil {
		trimmed = strings.TrimSpace(*ref)
	}
	switch {
	case trimmed == "", strings.EqualFold(trimmed, refMatcherAnyRef), strings.EqualFold(trimmed, "any"):
		return anyRefMatcherID, refMatcherAnyRef, "Any branch"
	case strings.Contains(trimmed, "*"):
		return trimmed, refMatcherPattern, "Pattern"
	default:
		return trimmed, refMatcherBranch, "Branch"
	}
}

// NewDefaultTaskSourceMatcher builds the sourceMatcher for the given ref. A nil
// or empty ref means "any ref".
func NewDefaultTaskSourceMatcher(ref *string) *DefaultTaskSourceMatcher {
	id, typeID, typeName := inferRefMatcher(ref)
	return &DefaultTaskSourceMatcher{
		Id:        &id,
		DisplayId: &id,
		Type: &struct {
			Id   openapigenerated.RestDefaultTaskRequestSourceMatcherTypeId `json:"id"`
			Name string                                                     `json:"name"`
		}{
			Id:   openapigenerated.RestDefaultTaskRequestSourceMatcherTypeId(typeID),
			Name: typeName,
		},
	}
}

// NewDefaultTaskTargetMatcher builds the targetMatcher for the given ref. A nil
// or empty ref means "any ref".
func NewDefaultTaskTargetMatcher(ref *string) *DefaultTaskTargetMatcher {
	id, typeID, typeName := inferRefMatcher(ref)
	return &DefaultTaskTargetMatcher{
		Id:        &id,
		DisplayId: &id,
		Type: &struct {
			Id   openapigenerated.RestRefMatcherTypeId `json:"id"`
			Name string                                `json:"name"`
		}{
			Id:   openapigenerated.RestRefMatcherTypeId(typeID),
			Name: typeName,
		},
	}
}
