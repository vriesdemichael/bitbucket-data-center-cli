package prcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

type testChecker struct{}

func (testChecker) CheckRepoPermission(ctx context.Context, projectKey, repoSlug string, permission openapigenerated.GetRepositories1ParamsPermission) error {
	return nil
}

// newMockPRServer answers pull-request routes for the tests that still need a
// server to accept a write.
//
// Thirty-seven suites used to hang off it, each asserting that a renderer
// prints what this file wrote. They are live now. What is left needs the
// request to succeed so that something else can be asserted about it -- which
// reviewer names arrived, which id came back -- and for those the payload is
// scaffolding rather than the subject.
func newMockPRServer(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":42,"title":"Test PR","state":"OPEN","open":true,"fromRef":{"displayId":"feature/x"},"toRef":{"displayId":"main"}}]}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42":
			_, _ = w.Write([]byte(`{"id":42,"title":"Test PR","description":"Desc","state":"OPEN","open":true,"version":1,"author":{"user":{"name":"authoruser","displayName":"Author User"}},"fromRef":{"displayId":"feature/x","latestCommit":"c123"},"toRef":{"displayId":"main"},"reviewers":[{"user":{"name":"alice","displayName":"Alice"},"approved":false,"status":"UNAPPROVED"}]}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/settings/reviewer-groups":
			_, _ = w.Write([]byte(`{"values":[{"id":10,"name":"core-team"},{"id":30,"name":"go-team"}]}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/settings/reviewer-groups/10/users":
			_, _ = w.Write([]byte(`[{"name":"bob"},{"name":"charlie"}]`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/settings/reviewer-groups/30/users":
			_, _ = w.Write([]byte(`[{"name":"gopher"}]`))

		case r.Method == http.MethodGet && strings.Contains(path, "/raw/.bitbucket/CODEOWNERS"):
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("*.go @go-team\n"))

		case r.Method == http.MethodGet && (strings.HasSuffix(path, "/diff") || strings.Contains(path, "/diff/")):
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("diff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go\n@@ -1 +1 @@\n"))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/settings/reviewer-groups":
			_, _ = w.Write([]byte(`{"values":[{"id":20,"name":"arch-team"}]}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/settings/reviewer-groups/20":
			_, _ = w.Write([]byte(`{"id":20,"name":"arch-team","users":[{"name":"david"}]}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/users":
			w.Header().Set("X-AUSERNAME", "currentuser")
			_, _ = w.Write([]byte(`{"values":[]}`))

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests":
			var payload struct {
				Title     string `json:"title"`
				Reviewers []struct {
					User struct {
						Name string `json:"name"`
					} `json:"user"`
				} `json:"reviewers"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			var respReviewers []map[string]any
			for _, rev := range payload.Reviewers {
				respReviewers = append(respReviewers, map[string]any{
					"user":   map[string]any{"name": rev.User.Name, "displayName": rev.User.Name},
					"role":   "REVIEWER",
					"status": "UNAPPROVED",
				})
			}
			resp := map[string]any{
				"id":        43,
				"title":     payload.Title,
				"state":     "OPEN",
				"open":      true,
				"fromRef":   map[string]any{"displayId": "feature/y"},
				"toRef":     map[string]any{"displayId": "main"},
				"reviewers": respReviewers,
			}
			_ = json.NewEncoder(w).Encode(resp)

		case r.Method == http.MethodPut && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42":
			_, _ = w.Write([]byte(`{"id":42,"title":"Updated PR","description":"New Desc","state":"OPEN","open":true,"version":2,"fromRef":{"displayId":"feature/x"},"toRef":{"displayId":"main"}}`))

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/merge":
			_, _ = w.Write([]byte(`{"id":42,"title":"Test PR","state":"MERGED","open":false,"closed":true}`))

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/decline":
			_, _ = w.Write([]byte(`{"id":42,"title":"Test PR","state":"DECLINED","open":false,"closed":true}`))

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/reopen":
			_, _ = w.Write([]byte(`{"id":42,"title":"Test PR","state":"OPEN","open":true,"closed":false}`))

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/approve":
			_, _ = w.Write([]byte(`{"id":42,"title":"Test PR","state":"OPEN","open":true,"reviewers":[{"user":{"name":"alice"},"approved":true,"status":"APPROVED"}]}`))

		case r.Method == http.MethodDelete && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/approve":
			_, _ = w.Write([]byte(`{"id":42,"title":"Test PR","state":"OPEN","open":true,"reviewers":[{"user":{"name":"alice"},"approved":false,"status":"UNAPPROVED"}]}`))

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/participants":
			// Bitbucket answers this endpoint with a RestPullRequestParticipant,
			// not with the pull request. Mirroring that here keeps the command
			// honest about where it gets the pull request it reports on.
			var payload struct {
				User struct {
					Name string `json:"name"`
				} `json:"user"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			_, _ = w.Write([]byte(`{"user":{"name":"` + payload.User.Name + `","displayName":"` + payload.User.Name + `"},"role":"REVIEWER","approved":false,"status":"UNAPPROVED"}`))

		case r.Method == http.MethodDelete && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/participants/bob":
			_, _ = w.Write([]byte(`{"id":42,"title":"Test PR","state":"OPEN","open":true}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/activities":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"action":"COMMENTED","comment":{"id":101,"version":1,"text":"Comment 1","state":"OPEN","author":{"name":"alice"}}}]}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/comments":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":101,"version":1,"text":"Comment 1","state":"OPEN","author":{"name":"alice"}}]}`))

		case r.Method == http.MethodGet && strings.Contains(path, "blocker-comments"):
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":103,"version":1,"text":"Blocker comment","severity":"BLOCKER"}]}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/comments/101":
			_, _ = w.Write([]byte(`{"id":101,"version":1,"text":"Comment 1","state":"OPEN","author":{"name":"alice"}}`))

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/comments":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":102,"version":1,"text":"New comment","state":"OPEN","author":{"name":"alice"}}`))

		case r.Method == http.MethodPut && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/comments/101":
			_, _ = w.Write([]byte(`{"id":101,"version":2,"text":"Comment 1","state":"RESOLVED","author":{"name":"alice"}}`))

		case (r.Method == http.MethodPut || r.Method == http.MethodDelete) && strings.Contains(path, "/comment-likes/latest/"):
			_, _ = w.Write([]byte(`{"emoticon":{"value":"thumbsup"}}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/commits":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":"c123","displayId":"c123","message":"Commit 1"}]}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/changes":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"path":{"toString":"file1.go"}}]}`))

		case r.Method == http.MethodGet && (strings.HasSuffix(path, "42.diff") || strings.HasSuffix(path, "/diff")):
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("diff --git a/file1.go b/file1.go\n--- a/file1.go\n+++ b/file1.go\n@@ -1 +1 @@\n-old\n+new\n"))

		case r.Method == http.MethodGet && (strings.HasSuffix(path, "42.patch") || strings.HasSuffix(path, "/patch")):
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("From c123\nSubject: Patch\n---\ndiff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go\n@@ -1 +1 @@\n"))

		case r.Method == http.MethodGet && strings.Contains(path, "diff-stats-summary"):
			// What Bitbucket sends: three totals and no per-file rows. The old
			// fixture invented a files array, so the assertion below tested
			// output the endpoint cannot produce (#526).
			_, _ = w.Write([]byte(`{"filesChanged":1,"totalInsertions":10,"totalDeletions":2}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/auto-merge":
			_, _ = w.Write([]byte(`{"enabled":true,"strategyId":"no-ff"}`))

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/auto-merge":
			_, _ = w.Write([]byte(`{"enabled":true,"strategyId":"no-ff"}`))

		case r.Method == http.MethodDelete && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/auto-merge":
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/watch":
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodDelete && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/watch":
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodGet && path == "/rest/jira/latest/projects/PRJ/repos/demo/pull-requests/42/issues":
			_, _ = w.Write([]byte(`[{"key":"PROJ-123","url":"https://jira.example.com/browse/PROJ-123"}]`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/participants":
			_, _ = w.Write([]byte(`{"values":[{"name":"alice","displayName":"Alice","emailAddress":"alice@example.com","active":true}]}`))

		case r.Method == http.MethodGet && path == "/rest/default-reviewers/latest/projects/PRJ/repos/demo/reviewers":
			_, _ = w.Write([]byte(`[{"id":1,"reviewers":[{"name":"bob","displayName":"Bob"}]}]`))

		case r.Method == http.MethodGet && strings.Contains(path, "/projects/PRJ/repos/demo/pull-requests/42/rebase"):
			_, _ = w.Write([]byte(`{"canRebase":true}`))

		case r.Method == http.MethodGet && strings.Contains(path, "/build-status/"):
			_, _ = w.Write([]byte(`{"values":[{"key":"ci/build","state":"SUCCESSFUL","url":"https://ci.example.com"}]}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42/merge-base":
			_, _ = w.Write([]byte(`{"id":"base123456","displayId":"base123","message":"Base commit"}`))

		case r.Method == http.MethodPost && strings.Contains(path, "/comments/101/apply-suggestion"):
			w.WriteHeader(http.StatusOK)

		case r.Method == http.MethodGet && strings.Contains(path, "/pull-requests/42/review"):
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":101,"text":"Draft comment","author":{"name":"alice"}}]}`))

		case (r.Method == http.MethodPost || r.Method == http.MethodPut) && strings.Contains(path, "/pull-requests/42/review"):
			w.WriteHeader(http.StatusOK)

		case r.Method == http.MethodDelete && strings.Contains(path, "/pull-requests/42/review"):
			w.WriteHeader(http.StatusNoContent)

		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	return server
}

func executePr(t *testing.T, serverURL string, args ...string) (string, error) {
	t.Helper()

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", serverURL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")
	t.Setenv("BITBUCKET_TOKEN", "test-token")

	var jsonFlag bool
	var dryRunFlag bool
	filteredArgs := make([]string, 0, len(args))
	for _, a := range args {
		if a == "--json" {
			jsonFlag = true
		} else if a == "--dry-run" {
			dryRunFlag = true
		} else {
			filteredArgs = append(filteredArgs, a)
		}
	}

	deps := Dependencies{
		JSONEnabled:   func() bool { return jsonFlag },
		DryRunEnabled: func() bool { return dryRunFlag },
		LoadConfig: func() (config.AppConfig, error) {
			return config.LoadFromEnv()
		},
		LoadConfigAndClient: func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
			cfg, err := config.LoadFromEnv()
			if err != nil {
				return config.AppConfig{}, nil, err
			}
			client, err := openapi.NewClientWithResponsesFromConfig(cfg)
			return cfg, client, err
		},
		WriteJSON:     jsonoutput.Write,
		WriteJSONList: jsonoutput.WriteList,
		PermissionChecker: func(client *openapigenerated.ClientWithResponses) PermissionChecker {
			return testChecker{}
		},
	}

	root := New(deps)
	buffer := &bytes.Buffer{}
	root.SetOut(buffer)
	root.SetErr(buffer)

	root.SetArgs(filteredArgs)
	err := root.Execute()
	return buffer.String(), err
}

func TestPRDefaultDependencies(t *testing.T) {
	cmd := New(Dependencies{})
	if cmd == nil {
		t.Fatal("expected New to return command with default dependencies")
	}
	checker := nopPermissionChecker{}
	if err := checker.CheckRepoPermission(context.Background(), "PRJ", "demo", openapi.RepoRead); err != nil {
		t.Fatalf("expected nop checker to return nil error: %v", err)
	}
}

// Both spellings are refused before a request exists, so a listener here
// could only hide the refusal not happening.
func TestPRValidationErrors(t *testing.T) {
	server := httptest.NewServer(testsupport.UnreachedHandler(t))
	t.Cleanup(server.Close)

	// --unresolved with conflicting --state
	_, err := executePr(t, server.URL, "comment", "list", "42", "--unresolved", "--state", "resolved")
	if err == nil {
		t.Fatalf("expected error on --unresolved with --state resolved")
	}

	// invalid state
	_, err = executePr(t, server.URL, "comment", "list", "42", "--state", "invalid-state")
	if err == nil {
		t.Fatalf("expected error on invalid comment state")
	}
}

// #506 is live now, in TestLivePRCreateFromAFork.
//
// pr create pre-flighted REPO_WRITE on the repository the pull request targets,
// which a fork contributor does not hold upstream, and had no way to say the
// source branch lives somewhere else -- so the standard contribution flow was
// refused twice over.
//
// Three of the four cases here read the POST body off a mock and checked what
// fromRef carried. Two of them turned on a field being *absent*, which is the
// weakest thing a payload assertion can watch: it passes if the field is
// missing for any reason, including the request never being built the way the
// server needs. The live test creates a real fork, opens the pull request from
// it, and reads back which repository each side points at -- and does the same
// for a pull request that is not from a fork, and for one that names its own
// target as the source.
//
// What is left is the case that never reaches a server.
func TestPRCreateFromAForkRefusesAMalformedRepository(t *testing.T) {
	// A listener that fails the test if it is reached. The kind asserted below
	// is the whole point -- a command that got as far as a request would fail
	// for a different reason and report a different kind.
	guard := httptest.NewServer(testsupport.UnreachedHandler(t))
	t.Cleanup(guard.Close)

	_, err := executePr(t, guard.URL, "create", "--from-ref", "feature/x", "--to-ref", "main",
		"--title", "probe", "--from-repo", "no-slash", "--repo", "GHBS/uploader",
		"--no-default-reviewers", "--no-codeowners")
	if err == nil {
		t.Fatal("a --from-repo without a project was accepted")
	}
	if kind := apperrors.KindOf(err); kind != apperrors.KindValidation {
		t.Errorf("kind = %v, want validation", kind)
	}
}

// Thirty-seven suites are live now rather than here.
//
// Each one drove a pull-request command against newMockPRServer and asserted
// the rendering: TestPRList looked for "#42" in a listing whose only pull
// request this file numbered 42, TestPRMerge for "Merged pull request #42"
// from a handler that answered every merge with MERGED. Command reach is
// still 234/234, so every one of those commands is asserted against a real
// Bitbucket -- there against pull requests the harness created, in both
// output modes, and with the state read back afterwards.
//
// TestPRMergeDryRunReadsMergeability went with them, which is #479. Its
// subject was that the merge preview reads mergeability rather than guessing
// from the state, and it built the vetoes it then required the preview to
// name. TestLivePullRequestMergeability makes a real instance refuse -- a
// required-approver check on a pull request that merges cleanly -- and
// requires "blocked" with a reason; TestLiveDryRunPredictionsReadRealState
// holds the other end, that one nothing stands against is still "update".
