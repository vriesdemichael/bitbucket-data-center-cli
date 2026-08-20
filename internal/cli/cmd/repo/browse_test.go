package repocmd_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	repocmd "github.com/vriesdemichael/bitbucket-server-cli/internal/cli/cmd/repo"
)

func newMockBrowseServer(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		switch {
		case r.Method == http.MethodGet && strings.Contains(path, "/files"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"values":["main.go","README.md","pkg/lib.go"],"isLastPage":true}`))

		case r.Method == http.MethodGet && strings.Contains(path, "/raw/"):
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("package main\n\nfunc main() {}\n"))

		case r.Method == http.MethodGet && strings.Contains(path, "/browse/"):
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Query().Get("blame") == "true" {
				_, _ = w.Write([]byte(`{
					"blame": {"author": {"name": "alice"}},
					"lines": [{"text": "package main"}, {"text": "func main() {}"}]
				}`))
			} else {
				_, _ = w.Write([]byte(`{
					"lines": [{"text": "package main"}, {"text": "func main() {}"}]
				}`))
			}

		case r.Method == http.MethodGet && strings.Contains(path, "/commits"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"values": [
					{"id": "1111111111111111111111111111111111111111", "displayId": "1111111", "message": "Initial commit\nSecond line"}
				],
				"isLastPage": true
			}`))

		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	return server
}

func TestRepoBrowseTree(t *testing.T) {
	server := newMockBrowseServer(t)
	deps := testDeps(server.URL)

	// Human mode tree
	cmd := repocmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"browse", "tree", "pkg", "--repo", "PROJ/repo1", "--at", "main"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on browse tree: %v", err)
	}
	if !strings.Contains(buf.String(), "main.go") {
		t.Fatalf("expected main.go in output, got: %s", buf.String())
	}

	// JSON mode tree
	deps.JSONEnabled = func() bool { return true }
	cmd = repocmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"browse", "tree", "--repo", "PROJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on browse tree JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "files") {
		t.Fatalf("expected files in json output, got: %s", buf.String())
	}
}

func TestRepoBrowseRaw(t *testing.T) {
	server := newMockBrowseServer(t)
	deps := testDeps(server.URL)

	cmd := repocmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"browse", "raw", "main.go", "--repo", "PROJ/repo1", "--at", "main"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on browse raw: %v", err)
	}
	if !strings.Contains(buf.String(), "package main") {
		t.Fatalf("expected 'package main' in output, got: %s", buf.String())
	}
}

func TestRepoBrowseFile(t *testing.T) {
	server := newMockBrowseServer(t)
	deps := testDeps(server.URL)

	// Human mode file
	cmd := repocmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"browse", "file", "main.go", "--repo", "PROJ/repo1", "--at", "main"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on browse file: %v", err)
	}
	if !strings.Contains(buf.String(), "package main") {
		t.Fatalf("expected 'package main' in output, got: %s", buf.String())
	}

	// JSON mode file
	deps.JSONEnabled = func() bool { return true }
	cmd = repocmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"browse", "file", "main.go", "--repo", "PROJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on browse file JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "lines") {
		t.Fatalf("expected lines in json output, got: %s", buf.String())
	}
}

func TestRepoBrowseBlame(t *testing.T) {
	server := newMockBrowseServer(t)
	deps := testDeps(server.URL)

	// Human mode blame
	cmd := repocmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"browse", "blame", "main.go", "--repo", "PROJ/repo1", "--at", "main"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on browse blame: %v", err)
	}
	if !strings.Contains(buf.String(), "alice") {
		t.Fatalf("expected alice in blame output, got: %s", buf.String())
	}

	// JSON mode blame
	deps.JSONEnabled = func() bool { return true }
	cmd = repocmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"browse", "blame", "main.go", "--repo", "PROJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on browse blame JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "blame") {
		t.Fatalf("expected blame in json output, got: %s", buf.String())
	}
}

func TestRepoBrowseHistory(t *testing.T) {
	server := newMockBrowseServer(t)
	deps := testDeps(server.URL)

	// Human mode history
	cmd := repocmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"browse", "history", "main.go", "--repo", "PROJ/repo1", "--limit", "10"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on browse history: %v", err)
	}
	if !strings.Contains(buf.String(), "Initial commit") {
		t.Fatalf("expected Initial commit in history output, got: %s", buf.String())
	}

	// JSON mode history
	deps.JSONEnabled = func() bool { return true }
	cmd = repocmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"browse", "history", "main.go", "--repo", "PROJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on browse history JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "commits") {
		t.Fatalf("expected commits in json output, got: %s", buf.String())
	}
}
