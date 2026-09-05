package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

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

// The pull-request and code-insights predictions that were here are live now,
// in TestLiveDryRunPredictionsReadRealState. They are the ones whose answer is
// a property of the server rather than of the command: merging a merged pull
// request, declining a declined one, approving one you have already approved,
// setting a report that already exists.
//
// The mock decided each of those states for itself, and one of them was a state
// Bitbucket does not allow. Its fixture listed the author as an APPROVED
// reviewer and ran the approve preview as them; a real Bitbucket answers
// "Authors may not update their status" and refuses. So the no-op that test
// asserted was read off a pull request that could not exist.
//
// The reviewer conditions, repository permissions, workflow webhooks, pull
// request settings and commit comments went the same way, into
// TestLiveGovernanceDryRunPredictionsReadRealState. Two of those previews were
// broken and the mock could not see it, because it supplied both sides of the
// comparison they make:
//
//   - update-approvers read requiredApprovers as {"enabled", "count"}. Bitbucket
//     sends a plain number, so the count was never known and the preview could
//     not report a no-op at all.
//   - reviewer condition create deep-compared the request against the listing.
//     The server answers with full user objects and a matcher id of its own
//     ("ANY_REF" is stored as "ANY_REF_MATCHER_ID"), so nothing ever matched and
//     an existing condition was predicted as a create.
//
// The branches, branch restrictions, build statuses, required build checks,
// projects, repositories and tags are TestLiveResourceDryRunPredictionsReadReal
// State. One more assertion did not survive contact: `project delete <missing>
// --dry-run` was said to predict no-op, and against a real server it exits 4.
// The code still carries the not-found branch, but nothing reaches it -- the
// admin preflight runs first and asks whether the caller administers a project
// that is not there. The mock made it look reachable by answering the permission
// lookup for every project while 404ing only the project itself.

func TestDryRunRepoPermissionPrechecksFailBeforePlanning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/repos" {
			writer.WriteHeader(http.StatusForbidden)
			_, _ = writer.Write([]byte(`{"errors":[{"message":"forbidden"}]}`))
			return
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()

	configureDryRunEnv(t, server.URL, "TEST", "demo")

	testCases := []struct {
		name string
		args []string
	}{
		{name: "branch create", args: []string{"--json", "--dry-run", "branch", "create", "feature/demo", "--start-point", "master"}},
		{name: "branch default set", args: []string{"--json", "--dry-run", "branch", "default", "set", "master"}},
		{name: "branch model update", args: []string{"--json", "--dry-run", "branch", "model", "update", "master"}},
		{name: "branch restriction create", args: []string{"--json", "--dry-run", "branch", "restriction", "create", "--type", "read-only", "--matcher-type", "BRANCH", "--matcher-id", "refs/heads/main"}},
		{name: "branch restriction update", args: []string{"--json", "--dry-run", "branch", "restriction", "update", "10", "--type", "read-only", "--matcher-type", "BRANCH", "--matcher-id", "refs/heads/main"}},
		{name: "branch restriction delete", args: []string{"--json", "--dry-run", "branch", "restriction", "delete", "10"}},
		{name: "build required create", args: []string{"--json", "--dry-run", "build", "required", "create", "--body", `{"buildParentKeys":["ci"]}`}},
		{name: "build required update", args: []string{"--json", "--dry-run", "build", "required", "update", "5", "--body", `{"buildParentKeys":["ci"]}`}},
		{name: "build required delete", args: []string{"--json", "--dry-run", "build", "required", "delete", "5"}},
		{name: "tag create", args: []string{"--json", "--dry-run", "tag", "create", "v1", "--start-point", "master"}},
		{name: "tag delete", args: []string{"--json", "--dry-run", "tag", "delete", "v1"}},
		{name: "repo comment create", args: []string{"--json", "--dry-run", "repo", "comment", "create", "--commit", "abc", "--text", "hello"}},
		{name: "repo comment update", args: []string{"--json", "--dry-run", "repo", "comment", "update", "--commit", "abc", "--id", "1", "--text", "hello"}},
		{name: "repo comment delete", args: []string{"--json", "--dry-run", "repo", "comment", "delete", "--commit", "abc", "--id", "1"}},
		{name: "repo permissions users grant", args: []string{"--json", "--dry-run", "repo", "settings", "security", "permissions", "users", "grant", "alice", "repo_write"}},
		{name: "repo permissions users revoke", args: []string{"--json", "--dry-run", "repo", "settings", "security", "permissions", "users", "revoke", "alice"}},
		{name: "repo permissions groups grant", args: []string{"--json", "--dry-run", "repo", "settings", "security", "permissions", "groups", "grant", "devs", "repo_read"}},
		{name: "repo permissions groups revoke", args: []string{"--json", "--dry-run", "repo", "settings", "security", "permissions", "groups", "revoke", "devs"}},
		{name: "repo webhook create", args: []string{"--json", "--dry-run", "repo", "settings", "workflow", "webhooks", "create", "ci", "http://h"}},
		{name: "repo webhook delete", args: []string{"--json", "--dry-run", "repo", "settings", "workflow", "webhooks", "delete", "42"}},
		{name: "repo pr settings update", args: []string{"--json", "--dry-run", "repo", "settings", "pull-requests", "update", "--required-all-tasks-complete=true"}},
		{name: "repo pr settings update approvers", args: []string{"--json", "--dry-run", "repo", "settings", "pull-requests", "update-approvers", "--count", "2"}},
		{name: "repo pr settings set strategy", args: []string{"--json", "--dry-run", "repo", "settings", "pull-requests", "set-strategy", "squash"}},
		{name: "insights report set", args: []string{"--json", "--dry-run", "insights", "report", "set", "abc", "lint", "--body", `{"title":"Lint","result":"PASS"}`}},
		{name: "insights report delete", args: []string{"--json", "--dry-run", "insights", "report", "delete", "abc", "lint"}},
		{name: "insights annotation add", args: []string{"--json", "--dry-run", "insights", "annotation", "add", "abc", "lint", "--body", `[{"externalId":"ann1","message":"m","severity":"LOW"}]`}},
		{name: "insights annotation delete", args: []string{"--json", "--dry-run", "insights", "annotation", "delete", "abc", "lint", "--external-id", "ann1"}},
		{name: "pr create", args: []string{"--json", "--dry-run", "pr", "create", "--from-ref", "feature/demo", "--to-ref", "master", "--title", "Feature"}},
		{name: "pr update", args: []string{"--json", "--dry-run", "pr", "update", "20", "--title", "Feature", "--version", "1"}},
		{name: "pr merge", args: []string{"--json", "--dry-run", "pr", "merge", "20"}},
		{name: "pr decline", args: []string{"--json", "--dry-run", "pr", "decline", "20"}},
		{name: "pr reopen", args: []string{"--json", "--dry-run", "pr", "reopen", "20"}},
		{name: "pr review approve", args: []string{"--json", "--dry-run", "pr", "review", "approve", "20"}},
		{name: "pr review unapprove", args: []string{"--json", "--dry-run", "pr", "review", "unapprove", "20"}},
		{name: "pr reviewer add", args: []string{"--json", "--dry-run", "pr", "review", "reviewer", "add", "20", "--user", "alice"}},
		{name: "pr comment resolve", args: []string{"--json", "--dry-run", "pr", "comment", "resolve", "20", "100"}},
		{name: "pr comment reopen", args: []string{"--json", "--dry-run", "pr", "comment", "reopen", "20", "100"}},
		{name: "pr reviewer remove", args: []string{"--json", "--dry-run", "pr", "review", "reviewer", "remove", "20", "--user", "alice"}},
		{name: "repo admin fork", args: []string{"--json", "--dry-run", "repo", "admin", "fork", "--repo", "TEST/demo", "--name", "forked"}},
		{name: "repo admin update", args: []string{"--json", "--dry-run", "repo", "admin", "update", "--repo", "TEST/demo"}},
		{name: "repo admin delete", args: []string{"--json", "--dry-run", "repo", "admin", "delete", "--repo", "TEST/demo"}},
		{name: "reviewer repo create", args: []string{"--json", "--dry-run", "reviewer", "condition", "create", `{"requiredApprovals":1}`, "--repo", "TEST/demo"}},
		{name: "reviewer repo update", args: []string{"--json", "--dry-run", "reviewer", "condition", "update", "1", `{"requiredApprovals":1}`, "--repo", "TEST/demo"}},
		{name: "reviewer repo delete", args: []string{"--json", "--dry-run", "reviewer", "condition", "delete", "1", "--repo", "TEST/demo"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			out, err := executeTestCLI(t, testCase.args...)
			if err == nil {
				t.Fatalf("expected authorization error, output=%s", out)
			}
			if apperrors.ExitCode(err) != 3 {
				t.Fatalf("expected exit code 3, got %d err=%v output=%s", apperrors.ExitCode(err), err, out)
			}
			if strings.Contains(out, `"predictedAction"`) {
				t.Fatalf("expected precheck failure before dry-run preview, output=%s", out)
			}
		})
	}
}

func TestDryRunProjectPermissionPrechecksFailBeforePlanning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/PRJ/permissions/users":
			writer.WriteHeader(http.StatusForbidden)
			_, _ = writer.Write([]byte(`{"errors":[{"message":"forbidden"}]}`))
			return
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/PRJ":
			writer.WriteHeader(http.StatusForbidden)
			_, _ = writer.Write([]byte(`{"errors":[{"message":"forbidden"}]}`))
			return
		case request.Method == http.MethodPost && request.URL.Path == "/rest/api/latest/projects":
			writer.WriteHeader(http.StatusForbidden)
			_, _ = writer.Write([]byte(`{"errors":[{"message":"forbidden"}]}`))
			return
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	configureDryRunEnv(t, server.URL, "TEST", "demo")

	testCases := []struct {
		name string
		args []string
	}{
		{name: "project create", args: []string{"--json", "--dry-run", "project", "create", "PRJ", "--name", "Project"}},
		{name: "project update", args: []string{"--json", "--dry-run", "project", "update", "PRJ", "--name", "Project"}},
		{name: "project delete", args: []string{"--json", "--dry-run", "project", "delete", "PRJ"}},
		{name: "project users grant", args: []string{"--json", "--dry-run", "project", "permissions", "users", "grant", "PRJ", "alice", "PROJECT_READ"}},
		{name: "project users revoke", args: []string{"--json", "--dry-run", "project", "permissions", "users", "revoke", "PRJ", "alice"}},
		{name: "project groups grant", args: []string{"--json", "--dry-run", "project", "permissions", "groups", "grant", "PRJ", "devs", "PROJECT_WRITE"}},
		{name: "project groups revoke", args: []string{"--json", "--dry-run", "project", "permissions", "groups", "revoke", "PRJ", "devs"}},
		{name: "repo admin create", args: []string{"--json", "--dry-run", "repo", "admin", "create", "--project", "PRJ", "--name", "repo"}},
		{name: "reviewer project create", args: []string{"--json", "--dry-run", "reviewer", "condition", "create", `{"requiredApprovals":1}`, "--project", "PRJ"}},
		{name: "reviewer project update", args: []string{"--json", "--dry-run", "reviewer", "condition", "update", "1", `{"requiredApprovals":1}`, "--project", "PRJ"}},
		{name: "reviewer project delete", args: []string{"--json", "--dry-run", "reviewer", "condition", "delete", "1", "--project", "PRJ"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			out, err := executeTestCLI(t, testCase.args...)
			if err == nil {
				t.Fatalf("expected authorization error, output=%s", out)
			}
			if apperrors.ExitCode(err) != 3 {
				t.Fatalf("expected exit code 3, got %d err=%v output=%s", apperrors.ExitCode(err), err, out)
			}
			if strings.Contains(out, `"predictedAction"`) {
				t.Fatalf("expected precheck failure before dry-run preview, output=%s", out)
			}
		})
	}
}
