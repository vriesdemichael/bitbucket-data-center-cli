package admincmd

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/transport/httpclient"
)

type Dependencies struct {
	JSONEnabled func() bool
	LoadConfig  func() (config.AppConfig, error)
	WriteJSON   func(io.Writer, any) error
}

func (deps *Dependencies) withDefaults() Dependencies {
	d := *deps
	if d.JSONEnabled == nil {
		d.JSONEnabled = func() bool { return false }
	}
	if d.LoadConfig == nil {
		d.LoadConfig = func() (config.AppConfig, error) {
			return config.LoadFromEnv()
		}
	}
	if d.WriteJSON == nil {
		d.WriteJSON = jsonoutput.Write
	}
	return d
}

func New(deps Dependencies) *cobra.Command {
	d := deps.withDefaults()

	adminCmd := &cobra.Command{
		Use:   "admin",
		Short: "Administrative and connectivity checks",
	}

	adminCmd.AddCommand(&cobra.Command{
		Use:   "health",
		Short: "Probe the configured Bitbucket for reachability and authentication",
		Long: "Probe the configured Bitbucket for reachability and authentication.\n\n" +
			"This was previously described as checking \"local stack health\", which it never did: " +
			"it probes whichever host BITBUCKET_URL resolves to, wherever that is. The description " +
			"was wrong, not the command.\n\n" +
			"bb auth status now reports the same thing and more — identity, credential storage, and " +
			"whether git is set up to authenticate through bb — so prefer that. This stays for " +
			"scripts that already call it.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := d.LoadConfig()
			if err != nil {
				return err
			}

			client := httpclient.NewFromConfig(cfg)
			health, err := client.Health(cmd.Context())
			if err != nil {
				return err
			}

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), health)
			}

			if health.Authenticated {
				fmt.Fprintf(cmd.OutOrStdout(), "Bitbucket health: OK (status=%d, auth=ok)\n", health.StatusCode)
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Bitbucket health: OK (status=%d, auth=limited)\n", health.StatusCode)
			return nil
		},
	})

	return adminCmd
}
