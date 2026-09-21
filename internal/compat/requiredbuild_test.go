package compat

import (
	"testing"

	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

func TestAsksForRequiredBuildScopeOnlyWhenAnOlderReleaseWouldDiffer(t *testing.T) {
	t.Parallel()

	cases := []struct {
		body map[string]any
		want bool
	}{
		{body: map[string]any{"buildParentKeys": []any{"ci"}}, want: false},
		// What a release without the scope does anyway.
		{body: map[string]any{"requiredForPullRequest": true, "requiredForMergeQueue": false}, want: false},
		{body: map[string]any{"requiredForPullRequest": false}, want: true},
		{body: map[string]any{"requiredForMergeQueue": true}, want: true},
		{body: map[string]any{"requiredForPullRequest": true, "requiredForMergeQueue": true}, want: true},
	}
	for _, testCase := range cases {
		if got := AsksForRequiredBuildScope(testCase.body); got != testCase.want {
			t.Errorf("AsksForRequiredBuildScope(%v) = %t, want %t", testCase.body, got, testCase.want)
		}
	}
}

func TestReportRequiredBuildScopeFillsOnlyWhatWasLeftOut(t *testing.T) {
	t.Parallel()

	stored := false
	checks := []openapigenerated.RestRequiredBuildCondition{
		{},
		{RequiredForPullRequest: &stored},
	}
	if !RequiredBuildScopeUnreported(checks) {
		t.Fatal("a check with neither field is not reported as unreported")
	}

	ReportRequiredBuildScope(checks)

	if forPullRequest, forMergeQueue := checks[0].RequiredForPullRequest, checks[0].RequiredForMergeQueue; forPullRequest == nil || !*forPullRequest || forMergeQueue == nil || *forMergeQueue {
		t.Errorf("the unreported check reads %v/%v, want applied to pull requests and not the merge queue", forPullRequest, forMergeQueue)
	}
	// A scope Bitbucket did report is Bitbucket's, whatever it says.
	if checks[1].RequiredForPullRequest == nil || *checks[1].RequiredForPullRequest || checks[1].RequiredForMergeQueue != nil {
		t.Errorf("the reported scope was changed: %v/%v", checks[1].RequiredForPullRequest, checks[1].RequiredForMergeQueue)
	}
	if RequiredBuildScopeUnreported(checks) {
		t.Error("checks still read as unreported after the scope was filled in")
	}

	untyped := []any{map[string]any{"id": float64(1)}, map[string]any{"id": float64(2), "requiredForMergeQueue": true}}
	ReportRequiredBuildScopeIn(untyped)
	if first := untyped[0].(map[string]any); first["requiredForPullRequest"] != true || first["requiredForMergeQueue"] != false {
		t.Errorf("the untyped unreported check reads %v", first)
	}
	if second := untyped[1].(map[string]any); second["requiredForMergeQueue"] != true || second["requiredForPullRequest"] != nil {
		t.Errorf("the untyped reported scope was changed: %v", second)
	}
}
