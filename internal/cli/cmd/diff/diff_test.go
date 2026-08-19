package diffcmd_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	diffcmd "github.com/vriesdemichael/bitbucket-server-cli/internal/cli/cmd/diff"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
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

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/1.patch":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("diff --git a/pr.txt b/pr.txt\n--- a/pr.txt\n+++ b/pr.txt\n@@ -1 +1 @@\n-old\n+new\n"))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/diff":
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

func TestDiffRefsPatch(t *testing.T) {
	server := newMockDiffServer(t)
	deps := newTestDependencies(t, server.URL, false)

	cmd := diffcmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"refs", "main", "feature", "--patch"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on diff refs: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "diff --git a/file.txt") {
		t.Fatalf("expected git diff in output, got: %s", out)
	}
}

func TestDiffPR(t *testing.T) {
	server := newMockDiffServer(t)
	deps := newTestDependencies(t, server.URL, false)

	cmd := diffcmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"pr", "1", "--patch"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on diff pr: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "diff --git a/pr.txt") {
		t.Fatalf("expected pr diff in output, got: %s", out)
	}
}

func TestDiffCommit(t *testing.T) {
	server := newMockDiffServer(t)
	deps := newTestDependencies(t, server.URL, false)

	cmd := diffcmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"commit", "sha123"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on diff commit: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "diff --git a/commit.txt") {
		t.Fatalf("expected commit diff in output, got: %s", out)
	}
}
