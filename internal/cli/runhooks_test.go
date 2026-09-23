package cli

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
)

// TestARootFlagReachesACommandGroupWithItsOwnHook is #574: a command group's
// own PersistentPreRun replaced the root's, so --full-error-body was accepted
// under it and silently did nothing.
//
// The group is built here rather than found in the tree. A deprecated group is
// what grows a hook of its own -- it warns its users from one -- and the tree
// has none while nothing is deprecated.
//
// Not parallel. The flag is process-wide, and a sequential test runs while the
// parallel ones are held back, so no other test sees it switched on.
func TestARootFlagReachesACommandGroupWithItsOwnHook(t *testing.T) {
	t.Setenv("BB_CONFIG_PATH", filepath.Join(t.TempDir(), "config.yaml"))
	t.Cleanup(func() { openapi.SetFullUpstreamBodies(false) })

	groupHookRan := false
	group := &cobra.Command{
		Use:              "group",
		PersistentPreRun: func(*cobra.Command, []string) { groupHookRan = true },
	}
	group.AddCommand(&cobra.Command{
		Use:  "leaf",
		RunE: func(*cobra.Command, []string) error { return nil },
	})

	root := NewRootCommand()
	root.AddCommand(group)
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"group", "leaf", "--full-error-body"})
	_ = root.Execute()

	if !groupHookRan {
		t.Fatal("the group's own hook did not run, so this test is not testing a group with one")
	}
	if !openapi.FullUpstreamBodies() {
		t.Fatal("--full-error-body under a group with its own hook did not take effect")
	}
}
