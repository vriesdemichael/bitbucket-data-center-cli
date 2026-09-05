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
