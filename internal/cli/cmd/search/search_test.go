package searchcmd

import (
	"bytes"
	"context"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/safederef"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearchReposCommand(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/1.0/repos" {
			http.NotFound(w, r)
			return
		}

		name := r.URL.Query().Get("name")
		if name == "demo" {
			_, _ = w.Write([]byte(`{"values":[{"slug":"demo","name":"Demo","project":{"key":"TEST"}}],"isLastPage":true}`))
			return
		}

		_, _ = w.Write([]byte(`{"values":[],"isLastPage":true}`))
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	cmd := New(Dependencies{
		JSONEnabled: func() bool { return false },
	})

	output := new(bytes.Buffer)
	cmd.SetOut(output)
	cmd.SetArgs([]string{"repos", "demo"})

	err := cmd.ExecuteContext(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if !bytes.Contains(output.Bytes(), []byte("TEST/demo")) || !bytes.Contains(output.Bytes(), []byte("Demo")) {
		t.Fatalf("unexpected output: %s", output.String())
	}
}

func TestSearchCommitsCommand(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Logf("Commit API Request: %s", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/commits" {
			_, _ = w.Write([]byte(`{"values":[{"id":"abcdef","displayId":"abcdef","message":"Fix bug"}],"isLastPage":true}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	cmd := New(Dependencies{
		JSONEnabled: func() bool { return false },
	})

	output := new(bytes.Buffer)
	cmd.SetOut(output)
	cmd.SetArgs([]string{"commits", "--repo", "TEST/demo"})

	err := cmd.ExecuteContext(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if !bytes.Contains(output.Bytes(), []byte("abcdef")) || !bytes.Contains(output.Bytes(), []byte("Fix bug")) {
		t.Fatalf("unexpected output: %s", output.String())
	}
}

func TestSearchPRsCommand(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Logf("PR API Request: %s", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/rest/api/1.0/dashboard/pull-requests" {
			_, _ = w.Write([]byte(`{"values":[{"id":42,"title":"Fix bug","state":"OPEN","open":true,"toRef":{"repository":{"slug":"demo","project":{"key":"TEST"}}}}],"isLastPage":true}`))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/rest/api/latest/projects/TEST/repos/demo/pull-requests") {
			_, _ = w.Write([]byte(`{"values":[{"id":43,"title":"Update docs","state":"OPEN","open":true}],"isLastPage":true}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)

	t.Run("dashboard", func(t *testing.T) {
		cmd := New(Dependencies{
			JSONEnabled: func() bool { return false },
		})
		output := new(bytes.Buffer)
		cmd.SetOut(output)
		cmd.SetArgs([]string{"prs"})

		err := cmd.ExecuteContext(context.Background())
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}

		if !bytes.Contains(output.Bytes(), []byte("[TEST/demo] #42")) || !bytes.Contains(output.Bytes(), []byte("OPEN")) || !bytes.Contains(output.Bytes(), []byte("Fix bug")) {
			t.Fatalf("unexpected output: %s", output.String())
		}
	})

	t.Run("repo", func(t *testing.T) {
		cmd := New(Dependencies{
			JSONEnabled: func() bool { return false },
		})
		output := new(bytes.Buffer)
		cmd.SetOut(output)
		cmd.SetArgs([]string{"prs", "--repo", "TEST/demo"})

		err := cmd.ExecuteContext(context.Background())
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}

		if !bytes.Contains(output.Bytes(), []byte("#43")) || !bytes.Contains(output.Bytes(), []byte("OPEN")) || !bytes.Contains(output.Bytes(), []byte("Update docs")) {
			t.Fatalf("unexpected output: %s", output.String())
		}
	})
}

func TestSearchReposEmptyResult(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"values":[],"isLastPage":true}`))
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "repo")

	cmd := New(Dependencies{
		JSONEnabled: func() bool { return false },
	})

	output := new(bytes.Buffer)
	cmd.SetOut(output)
	cmd.SetArgs([]string{"repos", "notfound"})

	err := cmd.ExecuteContext(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !bytes.Contains(output.Bytes(), []byte("No repositories found")) {
		t.Fatalf("expected empty-state message, got: %s", output.String())
	}
}

func TestSearchDefaultsAndSafeString(t *testing.T) {
	if safederef.String(nil) != "" {
		t.Fatal("expected empty string for safederef.String(nil)")
	}
	s := "test"
	if safederef.String(&s) != "test" {
		t.Fatal("expected test for safederef.String(&s)")
	}

	t.Setenv("BITBUCKET_URL", "http://localhost:7990")
	var deps Dependencies
	d := deps.withDefaults()

	if d.JSONEnabled == nil || d.JSONEnabled() {
		t.Fatal("expected JSONEnabled to default to false")
	}
	if d.WriteJSONList == nil {
		t.Fatal("expected WriteJSONList to default to non-nil")
	}
	if d.LoadConfig != nil {
		cfg, err := d.LoadConfig()
		if err != nil || cfg.BitbucketURL != "http://localhost:7990" {
			t.Fatalf("unexpected LoadConfig: %v", err)
		}
	}
	if d.LoadConfigAndClient != nil {
		cfg, client, err := d.LoadConfigAndClient()
		if err != nil || client == nil || cfg.BitbucketURL != "http://localhost:7990" {
			t.Fatalf("unexpected LoadConfigAndClient: %v", err)
		}
	}
}

func TestSearchJSONAndEmptyStates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "empty") {
			_, _ = w.Write([]byte(`{"values":[],"isLastPage":true}`))
			return
		}
		if strings.Contains(r.URL.Path, "pull-requests") {
			_, _ = w.Write([]byte(`{"values":[{"id":10,"title":"PR Title"}],"isLastPage":true}`))
			return
		}
		if strings.Contains(r.URL.Path, "commits") {
			_, _ = w.Write([]byte(`{"values":[{"id":"c1","displayId":"c1","message":"Commit Msg"}],"isLastPage":true}`))
			return
		}
		if strings.Contains(r.URL.Path, "repos") {
			_, _ = w.Write([]byte(`{"values":[{"slug":"r1","name":"Repo 1","project":{"key":"PRJ"}}],"isLastPage":true}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "repo")

	// JSON mode for repos, commits, prs
	depsJSON := Dependencies{JSONEnabled: func() bool { return true }}

	// search repos JSON
	cmd := New(depsJSON)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"repos", "query"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("unexpected error searching repos JSON: %v", err)
	}

	// search commits JSON
	cmd = New(depsJSON)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"commits", "--repo", "PRJ/repo"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("unexpected error searching commits JSON: %v", err)
	}

	// search prs JSON
	cmd = New(depsJSON)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"prs", "--repo", "PRJ/repo"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("unexpected error searching prs JSON: %v", err)
	}

	// Empty states
	depsHuman := Dependencies{JSONEnabled: func() bool { return false }}

	// commits empty state
	cmd = New(depsHuman)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"commits", "--repo", "PRJ/empty"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("unexpected error on empty commits: %v", err)
	}
	if !strings.Contains(buf.String(), "No commits found") {
		t.Fatalf("expected No commits found in output: %s", buf.String())
	}

	// prs empty state
	cmd = New(depsHuman)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"prs", "--repo", "PRJ/empty"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("unexpected error on empty prs: %v", err)
	}
	if !strings.Contains(buf.String(), "No pull requests found") {
		t.Fatalf("expected No pull requests found in output: %s", buf.String())
	}
}
