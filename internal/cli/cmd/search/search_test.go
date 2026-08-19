package searchcmd

import (
	"bytes"
	"context"
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
