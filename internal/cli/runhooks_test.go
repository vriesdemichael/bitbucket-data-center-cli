package cli

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
)

// TestARootFlagReachesACommandGroupWithItsOwnHook is #574: bb bulk's own
// PersistentPreRun replaced the root's, so --full-error-body was accepted
// there and silently did nothing.
//
// Not parallel. The flag is process-wide, and a sequential test runs while the
// parallel ones are held back, so no other test sees it switched on.
func TestARootFlagReachesACommandGroupWithItsOwnHook(t *testing.T) {
	t.Setenv("BB_BULK_STATUS_DIR", t.TempDir())
	t.Setenv("BB_CONFIG_PATH", filepath.Join(t.TempDir(), "config.yaml"))
	t.Cleanup(func() { openapi.SetFullUpstreamBodies(false) })

	root := NewRootCommand()
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"bulk", "status", "no-such-operation", "--full-error-body"})
	_ = root.Execute()

	if !openapi.FullUpstreamBodies() {
		t.Fatal("--full-error-body under bb bulk did not take effect")
	}
}
