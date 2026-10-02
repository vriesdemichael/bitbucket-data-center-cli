package compat

import "testing"

func TestNamesDefaultBranchMatcherReadsOnlyTheNamedMatchers(t *testing.T) {
	t.Parallel()

	defaultBranch := map[string]any{"id": "#", "type": map[string]any{"id": "DEFAULT_BRANCH"}}
	branch := map[string]any{"id": "refs/heads/main", "type": map[string]any{"id": "BRANCH"}}
	cases := []struct {
		body map[string]any
		want bool
	}{
		{body: map[string]any{"refMatcher": branch}, want: false},
		{body: map[string]any{"refMatcher": defaultBranch}, want: true},
		{body: map[string]any{"refMatcher": branch, "exemptRefMatcher": defaultBranch}, want: true},
		{body: map[string]any{"refMatcher": map[string]any{"type": map[string]any{"id": " default_branch "}}}, want: true},
		// A field that was not named is not read.
		{body: map[string]any{"targetMatcher": defaultBranch}, want: false},
		{body: map[string]any{}, want: false},
	}
	for _, testCase := range cases {
		if got := NamesDefaultBranchMatcher(testCase.body, "refMatcher", "exemptRefMatcher"); got != testCase.want {
			t.Errorf("NamesDefaultBranchMatcher(%v) = %t, want %t", testCase.body, got, testCase.want)
		}
	}
}

func TestConditionCheckedDifferentlyOnlyWhenTheReviewersDecideIt(t *testing.T) {
	t.Parallel()

	cases := []struct {
		body map[string]any
		want bool
	}{
		{body: map[string]any{"reviewers": []any{map[string]any{"id": float64(2)}}}, want: false},
		{body: map[string]any{}, want: true},
		{body: map[string]any{"reviewers": []any{}}, want: true},
		{body: map[string]any{"reviewers": []any{map[string]any{"name": "admin"}}}, want: true},
		{body: map[string]any{"reviewers": []any{map[string]any{"id": float64(2)}, map[string]any{"name": "admin"}}}, want: true},
	}
	for _, testCase := range cases {
		if got := ConditionCheckedDifferently(testCase.body); got != testCase.want {
			t.Errorf("ConditionCheckedDifferently(%v) = %t, want %t", testCase.body, got, testCase.want)
		}
	}
}
