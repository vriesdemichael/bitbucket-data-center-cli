package projectcmd

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/permissionchecker"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

func executeTestCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := NewRootCommand()
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(args)
	err := root.Execute()
	return buf.String(), err
}

func NewRootCommand() *cobra.Command {
	return newRootCommandAt("")
}

// newRootCommandAt is NewRootCommand with the host the configuration names
// passed to the load rather than published to the process. An empty host
// leaves the load as it is with nothing passed.
func newRootCommandAt(host string) *cobra.Command {
	root := &cobra.Command{Use: "bb"}
	jsonFlag := root.PersistentFlags().Bool("json", false, "")
	dryRunFlag := root.PersistentFlags().Bool("dry-run", false, "")
	deps := Dependencies{
		JSONEnabled:   func() bool { return *jsonFlag },
		DryRunEnabled: func() bool { return *dryRunFlag },
		LoadConfig: func() (config.AppConfig, error) {
			return config.LoadWithOverrides(config.Overrides{Host: host})
		},
		PermissionChecker: func(c *openapigenerated.ClientWithResponses) PermissionChecker { return permissionchecker.New(c) },
	}
	root.AddCommand(New(deps))
	return root
}
