package compat

import (
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// RequiredBuildScope is choosing what a required build applies to: pull
// requests, the merge queue, or both.
//
// It arrived with merge queues in 10.2. An earlier release applies every
// required build to pull requests, has no merge queue to apply one to, and
// answers 200 to requiredForPullRequest and requiredForMergeQueue while
// ignoring both -- so a build created to spare pull requests blocks them.
// Observed on every supported release from 9.2.1 to 10.1.5. From 10.2 a check
// created without a scope applies to both (observed on 10.4.3).
var RequiredBuildScope = Difference{
	What:  "a required build that spares pull requests or applies to the merge queue",
	Since: Release{Major: 10, Minor: 2},
}

// AsksForRequiredBuildScope reports whether a required build body asks for
// something a release without the scope would do differently. Asking for what
// such a release does anyway -- applied to pull requests, not to a merge
// queue -- is not.
func AsksForRequiredBuildScope(body map[string]any) bool {
	return body["requiredForPullRequest"] == false || body["requiredForMergeQueue"] == true
}

// RequiredBuildScopeUnreported reports whether Bitbucket left the scope out of
// any of these checks, which a release with the scope does not do.
func RequiredBuildScopeUnreported(checks []openapigenerated.RestRequiredBuildCondition) bool {
	for _, check := range checks {
		if check.RequiredForPullRequest == nil && check.RequiredForMergeQueue == nil {
			return true
		}
	}

	return false
}

// ReportRequiredBuildScope fills in the scope a release without it enforces --
// on pull requests, not on a merge queue -- wherever Bitbucket left it out, so
// the check reads the way the newest release reports the same behaviour.
func ReportRequiredBuildScope(checks []openapigenerated.RestRequiredBuildCondition) {
	for index := range checks {
		if checks[index].RequiredForPullRequest != nil || checks[index].RequiredForMergeQueue != nil {
			continue
		}
		forPullRequest, forMergeQueue := true, false
		checks[index].RequiredForPullRequest = &forPullRequest
		checks[index].RequiredForMergeQueue = &forMergeQueue
	}
}

// ReportRequiredBuildScopeIn is ReportRequiredBuildScope for checks decoded
// without a type, as one listing is.
func ReportRequiredBuildScopeIn(checks []any) {
	for _, entry := range checks {
		check, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		_, forPullRequest := check["requiredForPullRequest"]
		_, forMergeQueue := check["requiredForMergeQueue"]
		if forPullRequest || forMergeQueue {
			continue
		}
		check["requiredForPullRequest"] = true
		check["requiredForMergeQueue"] = false
	}
}
