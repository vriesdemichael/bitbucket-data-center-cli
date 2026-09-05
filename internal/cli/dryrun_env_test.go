package cli

import "testing"

// configureDryRunEnv points the CLI at a test server with a token.
//
// It is what remains of dryrun_stateful_commands_test.go, which drove every
// stateful dry run against a hand-written Bitbucket and asserted the prediction
// it had itself supplied. All of that is live now:
//
//   - TestLiveDryRunPredictionsReadRealState -- pull requests and code insights
//   - TestLiveGovernanceDryRunPredictionsReadRealState -- reviewer conditions,
//     repository permissions, webhooks, pull-request settings, commit comments
//   - TestLiveResourceDryRunPredictionsReadRealState -- branches, restrictions,
//     build statuses, required checks, projects, repositories, tags
//   - TestLiveDryRunPrechecksRefuseBeforePlanning -- the rule that a dry run
//     checks permission before it plans
//
// Four of its assertions did not survive the move, each because the mock
// supplied both sides of a comparison or both sides of a permission decision:
// an approve preview read off a pull request Bitbucket refuses to create, an
// approver count read from a shape Bitbucket does not send, a condition
// comparison that could never match a real response, and a project delete whose
// no-op branch nothing can reach. The precheck suite was wrong in the opposite
// direction: its blanket 403 asserted refusals for three operations Bitbucket
// lets a reader perform.
func configureDryRunEnv(t *testing.T, serverURL, projectKey, repoSlug string) {
	t.Helper()
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", serverURL)
	t.Setenv("BITBUCKET_PROJECT_KEY", projectKey)
	t.Setenv("BITBUCKET_REPO_SLUG", repoSlug)
	t.Setenv("BITBUCKET_TOKEN", "test-token")
	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("BITBUCKET_PASSWORD", "")
}
