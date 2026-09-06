package openapi

import "testing"

func stringPointer(value string) *string { return &value }

func TestNewDefaultTaskSourceMatcherInfersTypeFromRef(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		ref      *string
		wantID   string
		wantType string
	}{
		{name: "nil ref means any ref", ref: nil, wantID: "ANY_REF", wantType: "ANY_REF"},
		{name: "empty ref means any ref", ref: stringPointer(""), wantID: "ANY_REF", wantType: "ANY_REF"},
		{name: "blank ref means any ref", ref: stringPointer("   "), wantID: "ANY_REF", wantType: "ANY_REF"},
		{name: "any ref spelled out", ref: stringPointer("any_ref"), wantID: "ANY_REF", wantType: "ANY_REF"},
		{name: "any shorthand", ref: stringPointer("ANY"), wantID: "ANY_REF", wantType: "ANY_REF"},
		{name: "glob is a pattern", ref: stringPointer("refs/heads/feature/*"), wantID: "refs/heads/feature/*", wantType: "PATTERN"},
		{name: "plain ref is a branch", ref: stringPointer("refs/heads/master"), wantID: "refs/heads/master", wantType: "BRANCH"},
		{name: "surrounding space is trimmed", ref: stringPointer(" refs/heads/master "), wantID: "refs/heads/master", wantType: "BRANCH"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			matcher := NewDefaultTaskSourceMatcher(testCase.ref)
			if matcher.Id == nil || *matcher.Id != testCase.wantID {
				t.Fatalf("id = %v, want %q", matcher.Id, testCase.wantID)
			}
			if matcher.DisplayId == nil || *matcher.DisplayId != testCase.wantID {
				t.Fatalf("displayId = %v, want %q", matcher.DisplayId, testCase.wantID)
			}
			if matcher.Type == nil || string(matcher.Type.Id) != testCase.wantType {
				t.Fatalf("type id = %v, want %q", matcher.Type, testCase.wantType)
			}
			if matcher.Type.Name == "" {
				t.Fatal("type name is required by the schema and must not be empty")
			}
		})
	}
}

// The target matcher is a separate generated type with its own enum, so it needs
// its own check that the type id is spelled the same way.
func TestNewDefaultTaskTargetMatcherInfersTypeFromRef(t *testing.T) {
	t.Parallel()

	if matcher := NewDefaultTaskTargetMatcher(nil); string(matcher.Type.Id) != "ANY_REF" {
		t.Fatalf("nil ref: type id = %q, want ANY_REF", matcher.Type.Id)
	}
	if matcher := NewDefaultTaskTargetMatcher(stringPointer("refs/heads/release/*")); string(matcher.Type.Id) != "PATTERN" {
		t.Fatalf("glob: type id = %q, want PATTERN", matcher.Type.Id)
	}
	if matcher := NewDefaultTaskTargetMatcher(stringPointer("refs/heads/master")); string(matcher.Type.Id) != "BRANCH" {
		t.Fatalf("plain ref: type id = %q, want BRANCH", matcher.Type.Id)
	}
}

// ANY_REF_MATCHER was sent for every matcher until the server rejected it with
// "Unable to get ref matcher for type ANY_REF_MATCHER". It is not in the schema
// enum; it is the shape of the *id* Bitbucket echoes back, not a type.
func TestDefaultTaskMatcherNeverSendsTheRejectedType(t *testing.T) {
	t.Parallel()

	refs := []*string{nil, stringPointer(""), stringPointer("refs/heads/master"), stringPointer("refs/heads/feature/*")}
	for _, ref := range refs {
		if got := string(NewDefaultTaskSourceMatcher(ref).Type.Id); got == "ANY_REF_MATCHER" {
			t.Fatalf("source matcher still sends the rejected type for ref %v", ref)
		}
		if got := string(NewDefaultTaskTargetMatcher(ref).Type.Id); got == "ANY_REF_MATCHER" {
			t.Fatalf("target matcher still sends the rejected type for ref %v", ref)
		}
	}
}
