package repocmd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRepoEditRequiresContentToBeAskedFor covers the fallback that used to
// block.
//
// An empty --content read stdin unconditionally. On a CI runner, where stdin is
// an open pipe with nothing coming, that blocked forever; under an agent it
// returned nothing and committed an empty file over a real one. ADR-073 now
// requires the caller to ask, with --content -.
func TestRepoEditRequiresContentToBeAskedFor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"abc","displayId":"abc"}`))
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_TOKEN", "token")

	t.Run("no content given", func(t *testing.T) {
		_, err := executeTestCLI(t, "repo", "edit", "file.txt",
			"--repo", "PRJ/repo", "--message", "m", "--branch", "main")
		if err == nil {
			t.Fatal("an empty file was written with no content given")
		}
		if !strings.Contains(err.Error(), "--content") {
			t.Errorf("error = %q, want it to name --content", err.Error())
		}
	})

	t.Run("stdin when asked for", func(t *testing.T) {
		root := NewRootCommand()
		root.SetIn(strings.NewReader("body from stdin"))
		out := &strings.Builder{}
		root.SetOut(out)
		root.SetErr(out)
		root.SetArgs([]string{"repo", "edit", "file.txt", "--content", "-",
			"--repo", "PRJ/repo", "--message", "m", "--branch", "main"})

		// The point is that --content - reaches the read rather than being
		// rejected as missing; whether the stub accepts the write is not.
		if err := root.Execute(); err != nil && strings.Contains(err.Error(), "no content given") {
			t.Errorf("--content - was treated as missing content: %v", err)
		}
	})
}
