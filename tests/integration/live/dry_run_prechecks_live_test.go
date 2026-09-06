//go:build live

package live_test

import (
	"context"
	"strings"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// TestLiveDryRunPrechecksRefuseBeforePlanning covers the rule that a dry run
// checks permission before it plans anything.
//
// It matters because a preview is what an agent acts on. A dry run that plans
// first and reports "will be created" to somebody who cannot create it has told
// the caller the operation is available; the real attempt then fails, and the
// preview was worse than no preview.
//
// The mocked version answered 403 to the permission lookup and every command
// then failed, which proves the CLI propagates a 403 -- not that Bitbucket sends
// one for these operations, nor that it sends one for the account the caller
// actually has. Here the commands run as a real user holding read and nothing
// more, so the refusal is the server's.
func TestLiveDryRunPrechecksRefuseBeforePlanning(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	commit := repo.CommitIDs[0]

	// A pull request to aim the pull-request previews at, opened by the admin
	// before the CLI changes hands.
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)
	const branch = "feature/precheck"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "precheck.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}
	pullRequestID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
	if err != nil {
		t.Fatalf("create pull request failed: %v", err)
	}

	// Read and nothing else: enough to see the repository, so a refusal is about
	// the operation rather than about not finding it.
	reader, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	if err := harness.grantProjectPermission(ctx, seeded.Key, reader.Username, "PROJECT_READ"); err != nil {
		t.Fatalf("grant project permission failed: %v", err)
	}
	if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, reader.Username,
		openapigenerated.SetPermissionForUserParamsPermissionREPOREAD); err != nil {
		t.Fatalf("grant repository permission failed: %v", err)
	}

	configureLiveCLIEnvForUser(t, harness, seeded.Key, repo.Slug, reader)

	cases := []struct {
		name string
		args []string
	}{
		{"branch create", []string{"branch", "create", "feature/refused", "--start-point", "master"}},
		{"branch default set", []string{"branch", "default", "set", "master"}},
		{"branch model update", []string{"branch", "model", "update", "master"}},
		{"branch restriction create", []string{"branch", "restriction", "create", "--type", "read-only", "--matcher-type", "BRANCH", "--matcher-id", "refs/heads/master"}},
		{"branch restriction delete", []string{"branch", "restriction", "delete", "10"}},
		{"build required create", []string{"build", "required", "create", "--body", `{"buildParentKeys":["ci"],"refMatcher":{"id":"refs/heads/master","type":{"id":"BRANCH"}}}`}},
		{"build required delete", []string{"build", "required", "delete", "5"}},
		{"tag create", []string{"tag", "create", "refused", "--start-point", "master"}},
		{"tag delete", []string{"tag", "delete", "refused"}},
		{"repo permissions users grant", []string{"repo", "settings", "security", "permissions", "users", "grant", reader.Username, "repo_write"}},
		{"repo permissions users revoke", []string{"repo", "settings", "security", "permissions", "users", "revoke", reader.Username}},
		{"repo permissions groups grant", []string{"repo", "settings", "security", "permissions", "groups", "grant", licensedGroup, "repo_read"}},
		{"repo permissions groups revoke", []string{"repo", "settings", "security", "permissions", "groups", "revoke", licensedGroup}},
		{"repo webhook create", []string{"repo", "settings", "workflow", "webhooks", "create", "refused", "http://example.invalid/refused"}},
		{"repo webhook delete", []string{"repo", "settings", "workflow", "webhooks", "delete", "42"}},
		{"repo pr settings update", []string{"repo", "settings", "pull-requests", "update", "--required-all-tasks-complete=true"}},
		{"repo pr settings update approvers", []string{"repo", "settings", "pull-requests", "update-approvers", "--count", "2"}},
		{"repo pr settings set strategy", []string{"repo", "settings", "pull-requests", "set-strategy", "squash"}},
		{"insights report set", []string{"insights", "report", "set", commit, "lint", "--body", `{"title":"Lint","result":"PASS"}`}},
		{"insights report delete", []string{"insights", "report", "delete", commit, "lint"}},
		{"insights annotation add", []string{"insights", "annotation", "add", commit, "lint", "--body", `[{"externalId":"ann1","message":"m","severity":"LOW"}]`}},
		{"repo admin update", []string{"repo", "admin", "update", "--repo", repoRef}},
		{"repo admin delete", []string{"repo", "admin", "delete", "--repo", repoRef}},
		{"reviewer repo create", []string{"reviewer", "condition", "create", `{"requiredApprovals":1}`, "--repo", repoRef}},
		{"reviewer repo delete", []string{"reviewer", "condition", "delete", "1", "--repo", repoRef}},
		{"project update", []string{"project", "update", seeded.Key, "--name", "Refused"}},
		{"project delete", []string{"project", "delete", seeded.Key}},
		{"project users grant", []string{"project", "permissions", "users", "grant", seeded.Key, reader.Username, "PROJECT_READ"}},
		{"project users revoke", []string{"project", "permissions", "users", "revoke", seeded.Key, reader.Username}},
		{"project groups grant", []string{"project", "permissions", "groups", "grant", seeded.Key, licensedGroup, "PROJECT_WRITE"}},
		{"project groups revoke", []string{"project", "permissions", "groups", "revoke", seeded.Key, licensedGroup}},
		{"repo admin create", []string{"repo", "admin", "create", "--project", seeded.Key, "--name", "refused"}},
		{"reviewer project create", []string{"reviewer", "condition", "create", `{"requiredApprovals":1}`, "--project", seeded.Key}},
		{"reviewer project delete", []string{"reviewer", "condition", "delete", "1", "--project", seeded.Key}},
		{"pr merge", []string{"pr", "merge", pullRequestID}},
		{"pr decline", []string{"pr", "decline", pullRequestID}},
		{"pr auto-merge enable", []string{"pr", "auto-merge", "enable", pullRequestID}},
		{"pr auto-merge disable", []string{"pr", "auto-merge", "disable", pullRequestID}},

		// The tiers a unit contract test used to assert by reading the enum
		// passed to an injected checker. What matters to a caller is where the
		// line falls, so it is drawn here instead: refused for a reader below,
		// permitted for a reader in the subtest that follows.
		{"repo label add", []string{"repo", "label", "add", "refused-label"}},
		{"repo label remove", []string{"repo", "label", "remove", "refused-label"}},
		{"repo default-task add", []string{"repo", "default-task", "add", "refused task"}},
		{"repo default-task update", []string{"repo", "default-task", "update", "1", "--description", "refused"}},
		{"repo default-task delete", []string{"repo", "default-task", "delete", "1"}},
		{"repo sync enable", []string{"repo", "sync", "enable"}},
		{"repo sync disable", []string{"repo", "sync", "disable"}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			output, err := executeLiveCLI(t, append([]string{"--json", "--dry-run"}, testCase.args...)...)
			if err == nil {
				t.Fatalf("a read-only account got a preview:\n%s", output)
			}
			if code := apperrors.ExitCode(err); code != 3 {
				t.Fatalf("exit code = %d, want 3 (authorization): %v\noutput: %s", code, err, output)
			}
			// The refusal has to come before the plan, or the caller has already
			// been told the operation is available.
			if strings.Contains(output, `"predictedAction"`) {
				t.Fatalf("the preview was written before the permission check:\n%s", output)
			}
		})
	}

	// The other half of the rule, and the half the mock got wrong.
	//
	// Its blanket 403 made every dry run refuse, so it asserted a refusal for
	// three operations Bitbucket lets a reader perform: commenting on a commit,
	// forking a repository, and resolving a pull request comment. All three
	// succeed for this account -- verified by doing them -- so refusing them
	// would be the opposite failure: telling a caller an operation is closed
	// when it is open.
	t.Run("what a reader may do still previews", func(t *testing.T) {
		permitted := [][]string{
			{"repo", "comment", "create", "--commit", commit, "--text", "a reader may comment"},
			{"repo", "admin", "fork", "--repo", repoRef, "--name", "a-reader-may-fork"},
			// Watching is a preference on your own account, not a change to
			// the repository, so read is the right tier and refusing it would
			// close something Bitbucket leaves open.
			{"repo", "watch"},
			{"repo", "unwatch"},
		}

		for _, args := range permitted {
			output, err := executeLiveCLI(t, append([]string{"--json", "--dry-run"}, args...)...)
			if err != nil {
				t.Errorf("%s was refused to an account that may do it: %v\noutput: %s",
					strings.Join(args, " "), err, output)

				continue
			}
			if !strings.Contains(output, `"predictedAction"`) {
				t.Errorf("%s produced no preview:\n%s", strings.Join(args, " "), output)
			}
		}
	})
}
