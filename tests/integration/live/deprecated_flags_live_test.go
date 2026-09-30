//go:build live

package live_test

import (
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/deprecation"
)

// assertDeprecationWarned checks that a command passed a deprecated flag wrote
// the warning the registry holds for it, to stderr.
func assertDeprecationWarned(t *testing.T, stderr, name string) {
	t.Helper()

	entry, registered := deprecation.Named(name)
	if !registered {
		t.Fatalf("%s is not in the deprecation registry", name)
	}
	if !strings.Contains(stderr, entry.Warning()) {
		t.Errorf("stderr lacks the warning for %s: %q", name, stderr)
	}
}
