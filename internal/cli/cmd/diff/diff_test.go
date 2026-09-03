package diffcmd_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	diffcmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/diff"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

func newMockDiffServer(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		switch {
		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/patch":
			w.Header().Set("Content-Type", "text/plain")
			if r.URL.Query().Get("until") == "sha123" {
				_, _ = w.Write([]byte("diff --git a/commit.txt b/commit.txt\n--- a/commit.txt\n+++ b/commit.txt\n@@ -1 +1 @@\n-old\n+new\n"))
			} else {
				_, _ = w.Write([]byte("diff --git a/file.txt b/file.txt\n--- a/file.txt\n+++ b/file.txt\n@@ -1 +1 @@\n-old\n+new\n"))
			}

		case r.Method == http.MethodGet && strings.Contains(path, "/pull-requests/1.patch"):
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("diff --git a/pr.txt b/pr.txt\n--- a/pr.txt\n+++ b/pr.txt\n@@ -1 +1 @@\n-old\n+new\n"))

		case r.Method == http.MethodGet && strings.Contains(path, "/pull-requests/1/diff-stats-summary"):
			w.Header().Set("Content-Type", "application/json; charset=UTF-8")
			_, _ = w.Write([]byte(`{"filesChanged":1,"totalInsertions":10,"totalDeletions":2}`))

		case r.Method == http.MethodGet && strings.Contains(path, "/pull-requests/1.diff"):
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("diff --git a/pr.txt b/pr.txt\n--- a/pr.txt\n+++ b/pr.txt\n@@ -1 +1 @@\n-old\n+new\n"))

		case r.Method == http.MethodGet && strings.Contains(path, "/pull-requests/1/diff"):
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("diff --git a/pr.txt b/pr.txt\n--- a/pr.txt\n+++ b/pr.txt\n@@ -1 +1 @@\n-old\n+new\n"))

		// Before the compare/diff case below, which matches this path by prefix
		// and used to answer it with a diff payload the summary cannot come from.
		case r.Method == http.MethodGet && strings.Contains(path, "/rest/api/latest/projects/PRJ/repos/demo/compare/diff-stats-summary"):
			w.Header().Set("Content-Type", "application/json; charset=UTF-8")
			_, _ = w.Write([]byte(`{"filesChanged":1,"totalInsertions":1,"totalDeletions":1}`))

		case r.Method == http.MethodGet && strings.Contains(path, "/rest/api/latest/projects/PRJ/repos/demo/compare/diff"):
			w.Header().Set("Content-Type", "application/json; charset=UTF-8")
			_, _ = w.Write([]byte(`{"diffs":[{"destination":{"toString":"file.txt"},"hunks":[{"segments":[{"lines":[{"line":"+new"}]}]}]}]}`))

		case r.Method == http.MethodGet && strings.Contains(path, "/rest/api/latest/projects/PRJ/repos/demo/diff"):
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("diff --git a/commit.txt b/commit.txt\n--- a/commit.txt\n+++ b/commit.txt\n@@ -1 +1 @@\n-old\n+new\n"))

		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	return server
}

func newTestDependencies(t *testing.T, serverURL string, jsonMode bool) diffcmd.Dependencies {
	cfg := config.AppConfig{
		BitbucketURL: serverURL,
		ProjectKey:   "PRJ",
	}
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	return diffcmd.Dependencies{
		JSONEnabled: func() bool { return jsonMode },
		LoadConfig: func() (config.AppConfig, error) {
			return cfg, nil
		},
		LoadConfigAndClient: func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
			client, err := openapi.NewClientWithResponsesFromConfig(cfg)
			if err != nil {
				return config.AppConfig{}, nil, err
			}
			return cfg, client, nil
		},
	}
}

func TestDiffRefsModes(t *testing.T) {
	server := newMockDiffServer(t)

	// 1. --patch mode (human & JSON)
	deps := newTestDependencies(t, server.URL, false)
	cmd := diffcmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"refs", "main", "feature", "--patch"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on diff refs patch: %v", err)
	}
	if !strings.Contains(buf.String(), "diff --git a/file.txt") {
		t.Fatalf("expected git diff in output, got: %s", buf.String())
	}

	depsJSON := newTestDependencies(t, server.URL, true)
	cmd = diffcmd.New(depsJSON)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"refs", "main", "feature", "--patch"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on diff refs patch JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "patch") {
		t.Fatalf("expected patch in JSON output: %s", buf.String())
	}

	// 2. --name-only mode (human & JSON)
	cmd = diffcmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"refs", "main", "feature", "--name-only"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on diff refs name-only: %v", err)
	}
	if !strings.Contains(buf.String(), "file.txt") {
		t.Fatalf("expected file.txt in name-only output: %s", buf.String())
	}

	cmd = diffcmd.New(depsJSON)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"refs", "main", "feature", "--name-only"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on diff refs name-only JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "names") {
		t.Fatalf("expected names in JSON output: %s", buf.String())
	}

	// 3. --stat mode (human & JSON)
	cmd = diffcmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"refs", "main", "feature", "--stat"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on diff refs stat: %v", err)
	}

	cmd = diffcmd.New(depsJSON)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"refs", "main", "feature", "--stat"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on diff refs stat JSON: %v", err)
	}
	if !strings.Contains(buf.String(), `"output": "stat"`) {
		t.Fatalf("expected output=stat in JSON output: %s", buf.String())
	}

	// 4. Default / raw mode with --path
	cmd = diffcmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"refs", "main", "feature", "--path", "commit.txt"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on diff refs raw with path: %v", err)
	}
	if !strings.Contains(buf.String(), "diff --git a/commit.txt") {
		t.Fatalf("expected commit diff in output: %s", buf.String())
	}
}

func TestDiffPRModes(t *testing.T) {
	server := newMockDiffServer(t)
	deps := newTestDependencies(t, server.URL, false)
	depsJSON := newTestDependencies(t, server.URL, true)

	// 1. --patch mode (human & JSON)
	cmd := diffcmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"pr", "1", "--patch"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on diff pr: %v", err)
	}
	if !strings.Contains(buf.String(), "diff --git a/pr.txt") {
		t.Fatalf("expected pr diff in output, got: %s", buf.String())
	}

	cmd = diffcmd.New(depsJSON)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"pr", "1", "--patch"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on diff pr JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "patch") {
		t.Fatalf("expected patch in JSON output: %s", buf.String())
	}

	// 2. --name-only mode (human & JSON)
	cmd = diffcmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"pr", "1", "--name-only"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on diff pr name-only: %v", err)
	}
	if !strings.Contains(buf.String(), "pr.txt") {
		t.Fatalf("expected pr.txt in name-only output: %s", buf.String())
	}

	cmd = diffcmd.New(depsJSON)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"pr", "1", "--name-only"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on diff pr name-only JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "names") {
		t.Fatalf("expected names in JSON output: %s", buf.String())
	}

	// 3. --stat mode (human & JSON)
	cmd = diffcmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"pr", "1", "--stat"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on diff pr stat: %v", err)
	}

	cmd = diffcmd.New(depsJSON)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"pr", "1", "--stat"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on diff pr stat JSON: %v", err)
	}
	if !strings.Contains(buf.String(), `"totalInsertions"`) {
		t.Fatalf("expected the stats summary in JSON output: %s", buf.String())
	}

	// 4. Default / raw mode
	cmd = diffcmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"pr", "1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on diff pr raw: %v", err)
	}

	// 5. Resolving via full PR URL
	cmd = diffcmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"pr", server.URL + "/projects/PRJ/repos/demo/pull-requests/1", "--patch"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on diff pr URL: %v", err)
	}
	if !strings.Contains(buf.String(), "diff --git a/pr.txt") {
		t.Fatalf("expected pr diff in URL output, got: %s", buf.String())
	}

	// 6. Resolving via #1
	cmd = diffcmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"pr", "#1", "--patch"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on diff pr #1: %v", err)
	}
	if !strings.Contains(buf.String(), "diff --git a/pr.txt") {
		t.Fatalf("expected pr diff in #1 output, got: %s", buf.String())
	}
}

func TestDiffCommit(t *testing.T) {
	server := newMockDiffServer(t)

	// Human & JSON
	deps := newTestDependencies(t, server.URL, false)
	cmd := diffcmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"commit", "sha123", "--path", "commit.txt"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on diff commit: %v", err)
	}
	if !strings.Contains(buf.String(), "diff --git a/commit.txt") {
		t.Fatalf("expected commit diff in output, got: %s", buf.String())
	}

	depsJSON := newTestDependencies(t, server.URL, true)
	cmd = diffcmd.New(depsJSON)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"commit", "sha123"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on diff commit JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "patch") {
		t.Fatalf("expected patch in JSON output: %s", buf.String())
	}
}

func TestDiffValidationErrors(t *testing.T) {
	server := newMockDiffServer(t)
	deps := newTestDependencies(t, server.URL, false)

	// Mutually exclusive flags in refs
	cmd := diffcmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"refs", "main", "feature", "--patch", "--stat"})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error when multiple diff modes are specified on refs")
	}

	// Mutually exclusive flags in pr
	buf.Reset()
	cmd.SetArgs([]string{"pr", "1", "--patch", "--name-only"})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error when multiple diff modes are specified on pr")
	}

	// Invalid repo selector
	buf.Reset()
	cmd.SetArgs([]string{"refs", "main", "feature", "--repo", "invalid-no-slash"})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error on invalid repo selector")
	}

	// Repo flag passed to PR
	cmd = diffcmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"pr", "1", "--repo", "PRJ/demo"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on pr with --repo flag: %v", err)
	}

	// Defaults coverage
	defaultCmd := diffcmd.New(diffcmd.Dependencies{})
	if defaultCmd == nil {
		t.Fatalf("expected default command to be created")
	}

	repo := "PRJ/repo"
	prAlias := diffcmd.NewDiffPullRequestCommand(diffcmd.Dependencies{}, &repo)
	if prAlias == nil {
		t.Fatalf("expected default pr alias command to be created")
	}
}

func TestDiffDefaults(t *testing.T) {
	t.Setenv("BITBUCKET_URL", "http://localhost:7990")
	var deps diffcmd.Dependencies
	cmd := diffcmd.New(deps)
	if cmd == nil {
		t.Fatal("expected New to succeed with empty deps")
	}
}
