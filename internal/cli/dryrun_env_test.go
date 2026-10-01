package cli

import "github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"

// configuredRepository is the configuration a command is handed to reach one
// repository on a server with a token: passed to the root it is built with,
// never published to the process (ADR-082).
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
func configuredRepository(serverURL, projectKey, repoSlug string) config.Overrides {
	return config.Overrides{Host: serverURL, ProjectKey: projectKey, RepoSlug: repoSlug, Token: "test-token"}
}
