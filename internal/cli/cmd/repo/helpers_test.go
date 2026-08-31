package repocmd

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/permissionchecker"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

func executeTestCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	t.Setenv("NO_COLOR", "1")
	root := NewRootCommand()
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(args)
	err := root.Execute()
	return buf.String(), err
}

func NewRootCommand() *cobra.Command {
	root := &cobra.Command{Use: "bb"}
	jsonFlag := root.PersistentFlags().Bool("json", false, "")
	dryRunFlag := root.PersistentFlags().Bool("dry-run", false, "")
	// Mirrors the real root (internal/cli/root.go). prompt.RequestFor reads this
	// flag off the command, so a harness without it makes the flag untestable and
	// the command behave differently here than it does for a user.
	root.PersistentFlags().Bool("no-input", false, "")
	deps := Dependencies{
		JSONEnabled:       func() bool { return *jsonFlag },
		DryRunEnabled:     func() bool { return *dryRunFlag },
		PermissionChecker: func(c *openapigenerated.ClientWithResponses) PermissionChecker { return permissionchecker.New(c) },
	}
	root.AddCommand(New(deps))
	root.AddCommand(NewClone(deps))
	return root
}
