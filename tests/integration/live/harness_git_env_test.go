//go:build live

package live_test

import (
	"os"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/git/execgit"
)

// TestScopeFreeEnvStripsRepositoryScope guards against git commands being
// retargeted at whatever repository the process happens to be running under.
//
// Git exports GIT_DIR and friends to the hooks it runs, and honours them over a
// command's working directory. Before these were stripped, running the live
// suite from a pre-push hook seeded fixtures into the developer's own
// repository, committing onto the branch being pushed.
func TestScopeFreeEnvStripsRepositoryScope(t *testing.T) {
	scoped := map[string]string{
		"GIT_DIR":        "/somewhere/else/.git",
		"GIT_INDEX_FILE": "/somewhere/else/.git/index",
		"GIT_WORK_TREE":  "/somewhere/else",
		"GIT_PREFIX":     "sub/dir/",
	}
	for name, value := range scoped {
		t.Setenv(name, value)
	}
	t.Setenv("BB_HARNESS_SENTINEL", "kept")

	filtered := execgit.ScopeFreeEnv()

	for name := range scoped {
		if hasEnvVar(filtered, name) {
			t.Errorf("%s must be stripped so git is scoped by the working directory alone", name)
		}
	}

	if !hasEnvVar(filtered, "BB_HARNESS_SENTINEL") {
		t.Error("unrelated environment variables must be preserved")
	}

	// Not an exact count: the suite runs from a pre-push hook, where git has
	// already exported scoping variables beyond the ones set above.
	if len(filtered) > len(os.Environ())-len(scoped) {
		t.Errorf("expected at least the scoping variables to be removed: got %d of %d", len(filtered), len(os.Environ()))
	}
}

func hasEnvVar(environment []string, name string) bool {
	prefix := name + "="
	for _, entry := range environment {
		if strings.HasPrefix(entry, prefix) {
			return true
		}
	}
	return false
}
