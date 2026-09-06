package repocmd

import (
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// TestRepoEditRequiresContentToBeAskedFor covers the fallback that used to
// block.
//
// An empty --content read stdin unconditionally. On a CI runner, where stdin is
// an open pipe with nothing coming, that blocked forever; under an agent it
// returned nothing and committed an empty file over a real one. ADR-073 now
// requires the caller to ask, with --content -.
func TestRepoEditRequiresContentToBeAskedFor(t *testing.T) {
	t.Parallel()

	// A closed listener rather than a guarded one: the second case is supposed
	// to reach a request. Both are decided before a reply matters -- one
	// refuses without asking, and the other only has to get past that same
	// refusal, so a request that fails at the transport is a pass.
	setup := testSetup{Host: testsupport.ClosedListenerURL(t), Token: "token"}

	t.Run("no content given", func(t *testing.T) {
		_, err := executeTestCLIWith(t, setup, "repo", "edit", "file.txt",
			"--repo", "PRJ/repo", "--message", "m", "--branch", "main")
		if err == nil {
			t.Fatal("an empty file was written with no content given")
		}
		if !strings.Contains(err.Error(), "--content") {
			t.Errorf("error = %q, want it to name --content", err.Error())
		}
	})

	t.Run("stdin when asked for", func(t *testing.T) {
		reading := setup
		reading.Stdin = strings.NewReader("body from stdin")

		// The point is that --content - reaches the read rather than being
		// rejected as missing; whether the stub accepts the write is not.
		_, err := executeTestCLIWith(t, reading, "repo", "edit", "file.txt", "--content", "-",
			"--repo", "PRJ/repo", "--message", "m", "--branch", "main")
		if err != nil && strings.Contains(err.Error(), "no content given") {
			t.Errorf("--content - was treated as missing content: %v", err)
		}
	})
}
