package buildcmd

import (
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/result"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
	qualityservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/quality"
)

// TestResults is the test tally a build reporter may attach to a status.
type TestResults struct {
	Successful int32 `json:"successful" jsonschema:"Tests that passed."`
	Failed     int32 `json:"failed" jsonschema:"Tests that failed."`
	Skipped    int32 `json:"skipped" jsonschema:"Tests that were skipped."`
}

// BuildStatus is one build result reported against a commit.
type BuildStatus struct {
	Key         string             `json:"key,omitempty" jsonschema:"Build key, unique per commit. This is what bb build get and delete address."`
	State       string             `json:"state,omitempty" jsonschema:"SUCCESSFUL, FAILED, INPROGRESS, CANCELLED or UNKNOWN."`
	URL         string             `json:"url,omitempty" jsonschema:"Link to the build in the reporting system."`
	Name        string             `json:"name,omitempty" jsonschema:"Display name of the build."`
	Description string             `json:"description,omitempty" jsonschema:"Longer description the reporter attached."`
	Ref         string             `json:"ref,omitempty" jsonschema:"Ref the build ran against, when the reporter said."`
	Parent      string             `json:"parent,omitempty" jsonschema:"Key of the build that triggered this one."`
	BuildNumber string             `json:"buildNumber,omitempty" jsonschema:"Reporter's own build number. A string because build numbers are not always integers."`
	Duration    int64              `json:"duration,omitempty" jsonschema:"How long the build took, in milliseconds."`
	CreatedDate int64              `json:"createdDate,omitempty" jsonschema:"When the status was first reported, in milliseconds since the epoch."`
	UpdatedDate int64              `json:"updatedDate,omitempty" jsonschema:"When the status was last updated, in milliseconds since the epoch."`
	Repository  *result.Repository `json:"repository,omitempty" jsonschema:"Repository the build belongs to. Absent on commit-level statuses, which Bitbucket does not scope to a repository."`
	TestResults *TestResults       `json:"testResults,omitempty" jsonschema:"Test tally, when the reporter attached one. Absent is not the same as zero tests."`
}

// CommitBuildStats is the per-state tally for one commit.
//
// commit is carried on the row rather than used as an object key. `bb build
// status stats` accepts one commit or many, and it used to answer with a bare
// object for one and a map keyed by commit for many -- the same command,
// two shapes, decided by how many arguments the caller happened to pass. One
// list covers both, and a list is something --describe can state.
type CommitBuildStats struct {
	Commit     string `json:"commit" jsonschema:"Commit the tally is for, as it was given on the command line."`
	Successful int32  `json:"successful" jsonschema:"Builds reporting SUCCESSFUL."`
	Failed     int32  `json:"failed" jsonschema:"Builds reporting FAILED."`
	InProgress int32  `json:"inProgress" jsonschema:"Builds reporting INPROGRESS."`
	Unknown    int32  `json:"unknown" jsonschema:"Builds reporting UNKNOWN."`
	Cancelled  int32  `json:"cancelled" jsonschema:"Builds reporting CANCELLED."`
}

// StatusChange is what `bb build status set` reports.
type StatusChange struct {
	result.Status
	Commit string `json:"commit" jsonschema:"Commit the build status was set on."`
	Key    string `json:"key" jsonschema:"Build key that was set."`
}

// ScopedStatusChange is what the repository-scoped `bb build set` and
// `bb build delete` report.
type ScopedStatusChange struct {
	result.Status
	Repository result.Repository `json:"repository"`
	Commit     string            `json:"commit" jsonschema:"Commit the build status was set on or removed from."`
	Key        string            `json:"key" jsonschema:"Build key that was set or removed."`
}

// RequiredCheckDeletion is what `bb build required delete` reports.
type RequiredCheckDeletion struct {
	result.Status
	Repository result.Repository `json:"repository"`
	ID         int64             `json:"id" jsonschema:"Identifier of the merge check that was deleted."`
}

var buildStates = []string{"SUCCESSFUL", "FAILED", "INPROGRESS", "CANCELLED", "UNKNOWN"}

func init() {
	statusEnums := map[string][]string{"state": buildStates}
	checkEnums := map[string][]string{
		"refMatcher.type":       result.RefMatcherTypes,
		"exemptRefMatcher.type": result.RefMatcherTypes,
	}

	result.Declare("build status set", result.For[StatusChange](nil))
	result.Declare("build status get", result.List[BuildStatus](statusEnums))
	result.Declare("build status stats", result.List[CommitBuildStats](nil))

	result.Declare("build required list", result.List[result.RequiredBuildCheck](checkEnums))
	result.Declare("build required create", result.For[result.RequiredBuildCheck](checkEnums))
	result.Declare("build required update", result.For[result.RequiredBuildCheck](checkEnums))
	result.Declare("build required delete", result.For[RequiredCheckDeletion](nil))

	result.Declare("build set", result.For[ScopedStatusChange](nil))
	result.Declare("build get", result.For[BuildStatus](statusEnums))
	result.Declare("build delete", result.For[ScopedStatusChange](nil))
}

// repositoryOf converts the service reference used throughout this package.
func repositoryOf(repo qualityservice.RepositoryRef) result.Repository {
	return result.Repository{ProjectKey: repo.ProjectKey, Slug: repo.Slug}
}

// buildStatusFrom converts one upstream build status.
func buildStatusFrom(upstream openapigenerated.RestBuildStatus) BuildStatus {
	converted := BuildStatus{
		Key:         safeString(upstream.Key),
		State:       safeStringFromBuildState(upstream.State),
		URL:         safeString(upstream.Url),
		Name:        safeString(upstream.Name),
		Description: safeString(upstream.Description),
		Ref:         safeString(upstream.Ref),
		Parent:      safeString(upstream.Parent),
		BuildNumber: safeString(upstream.BuildNumber),
		Duration:    safeInt64(upstream.Duration),
		CreatedDate: safeInt64(upstream.CreatedDate),
		UpdatedDate: safeInt64(upstream.UpdatedDate),
	}

	projectKey := safeString(upstream.ProjectKey)
	slug := safeString(upstream.RepositorySlug)
	if projectKey != "" || slug != "" {
		converted.Repository = &result.Repository{ProjectKey: projectKey, Slug: slug}
	}

	if upstream.TestResults != nil {
		converted.TestResults = &TestResults{
			Successful: safeInt32(upstream.TestResults.Successful),
			Failed:     safeInt32(upstream.TestResults.Failed),
			Skipped:    safeInt32(upstream.TestResults.Skipped),
		}
	}

	return converted
}

// buildStatusesFrom converts a list, preserving order and never returning nil.
func buildStatusesFrom(upstream []openapigenerated.RestBuildStatus) []BuildStatus {
	converted := make([]BuildStatus, 0, len(upstream))
	for _, one := range upstream {
		converted = append(converted, buildStatusFrom(one))
	}

	return converted
}

// statsFrom converts one commit's tally.
func statsFrom(commit string, upstream openapigenerated.RestBuildStats) CommitBuildStats {
	return CommitBuildStats{
		Commit:     commit,
		Successful: safeInt32(upstream.Successful),
		Failed:     safeInt32(upstream.Failed),
		InProgress: safeInt32(upstream.InProgress),
		Unknown:    safeInt32(upstream.Unknown),
		Cancelled:  safeInt32(upstream.Cancelled),
	}
}

// statsListFrom builds one row per commit the caller asked about, in the order
// they were asked for.
//
// A commit with no tally still gets a row of zeros, because the caller named it
// and a missing row would be indistinguishable from a commit the caller forgot
// to pass.
func statsListFrom(commits []string, tallies map[string]openapigenerated.RestBuildStats) []CommitBuildStats {
	converted := make([]CommitBuildStats, 0, len(commits))
	for _, commit := range commits {
		converted = append(converted, statsFrom(commit, tallies[commit]))
	}

	return converted
}
